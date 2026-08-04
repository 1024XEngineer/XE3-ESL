package report

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

const (
	IELTSSpeakingReportSchemaVersion  = "ielts-speaking-report/v1"
	IELTSSpeakingReportDisclaimerCode = "AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS"
	IELTSSpeakingReportDisclaimer     = "AI 练习估分，非 IELTS 官方成绩。"
	IELTSSpeakingOverallAvailable     = "AVAILABLE"
	IELTSSpeakingOverallNotAvailable  = "NOT_AVAILABLE"
	IELTSTargetPlanNotConfigured      = "NOT_CONFIGURED"
)

var (
	ieltsCriterionOrder = scoring.IELTSCriteria()
	ieltsPartOrder      = scoring.IELTSParts()
)

type IELTSSpeakingReport struct {
	SchemaVersion      string                                  `json:"schema_version"`
	DisclaimerCode     string                                  `json:"disclaimer_code"`
	Disclaimer         string                                  `json:"disclaimer"`
	ScoreabilityStatus scoring.IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	GateStatus         scoring.IELTSSpeakingGateStatus         `json:"gate_status"`
	TestSummary        IELTSSpeakingReportTestSummary          `json:"test_summary"`
	Criteria           []IELTSSpeakingReportCriterion          `json:"criteria"`
	SpeakingOverall    IELTSSpeakingReportOverall              `json:"speaking_overall"`
	PartReviews        []IELTSSpeakingReportPartReview         `json:"part_reviews"`
	Questions          []IELTSSpeakingReportQuestion           `json:"questions"`
	TargetPlan         IELTSSpeakingReportTargetPlan           `json:"target_plan"`
	PriorityActions    []IELTSSpeakingReportPriorityRef        `json:"priority_actions"`
}

type IELTSSpeakingReportTestSummary struct {
	Part1Topic          string `json:"part_1_topic"`
	Part2Topic          string `json:"part_2_topic"`
	Part3Topic          string `json:"part_3_topic"`
	QuestionCount       int    `json:"question_count"`
	AnsweredCount       int    `json:"answered_count"`
	RecordingDurationMS int64  `json:"recording_duration_ms"`
}

type IELTSSpeakingReportCriterion struct {
	CriterionID     scoring.IELTSCriterion                  `json:"criterion_id"`
	Scoreability    scoring.IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	Gate            scoring.IELTSSpeakingGateStatus         `json:"gate_status"`
	EstimatedBand   *int                                    `json:"estimated_band,omitempty"`
	BandDescriptor  string                                  `json:"band_descriptor,omitempty"`
	Explanation     string                                  `json:"explanation"`
	Coverage        float64                                 `json:"coverage"`
	Confidence      float64                                 `json:"confidence"`
	ReasonCodes     []scoring.IELTSSpeakingReasonCode       `json:"reason_codes"`
	EvidenceRefIDs  []string                                `json:"evidence_ref_ids"`
	Strengths       []scoring.IELTSSpeakingShadowFinding    `json:"strengths"`
	Improvements    []scoring.IELTSSpeakingShadowFinding    `json:"improvements"`
	UpgradeExamples []scoring.IELTSSpeakingShadowFinding    `json:"upgrade_examples"`
}

type IELTSSpeakingReportOverall struct {
	Status        string   `json:"status"`
	EstimatedBand *float64 `json:"estimated_band,omitempty"`
	Explanation   string   `json:"explanation"`
}

type IELTSSpeakingReportPartReview struct {
	PartID                   scoring.IELTSPart `json:"part_id"`
	QuestionIndexes          []int             `json:"question_indexes"`
	EvidenceRefIDs           []string          `json:"evidence_ref_ids"`
	StrengthFindingIDs       []string          `json:"strength_finding_ids"`
	ImprovementFindingIDs    []string          `json:"improvement_finding_ids"`
	UpgradeExampleFindingIDs []string          `json:"upgrade_example_finding_ids"`
}

type IELTSSpeakingReportQuestion struct {
	QuestionID          string                                              `json:"question_id"`
	PartID              scoring.IELTSPart                                   `json:"part_id"`
	Index               int                                                 `json:"index"`
	QuestionText        string                                              `json:"question_text"`
	OpportunityStatus   scoring.IELTSSpeakingOpportunityStatus              `json:"opportunity_status"`
	AssessmentStatus    scoring.IELTSSpeakingAssessmentStatus               `json:"assessment_status"`
	ConfirmedTranscript string                                              `json:"confirmed_transcript,omitempty"`
	ResponseTurnID      string                                              `json:"response_turn_id,omitempty"`
	EvidenceRefIDs      []string                                            `json:"evidence_ref_ids"`
	CriterionFindings   []scoring.IELTSSpeakingQuestionCriterionFindingRefs `json:"criterion_findings"`
}

type IELTSSpeakingReportTargetPlan struct {
	Status string `json:"status"`
}

type IELTSSpeakingReportPriorityRef struct {
	CriterionID scoring.IELTSCriterion `json:"criterion_id"`
	FindingID   string                 `json:"finding_id"`
}

func ProjectIELTSSpeakingReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.IELTSSpeakingShadowResult,
) (IELTSSpeakingReport, error) {
	if err := scoring.ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		return IELTSSpeakingReport{}, err
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil ||
		ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingReport{},
			scoring.ErrInvalidIELTSSpeakingShadow
	}
	turnsByID := make(
		map[string]evidence.ConfirmedTurn,
		len(payload.ConfirmedTurns),
	)
	for _, turn := range payload.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}

	report := IELTSSpeakingReport{
		SchemaVersion:      IELTSSpeakingReportSchemaVersion,
		DisclaimerCode:     IELTSSpeakingReportDisclaimerCode,
		Disclaimer:         IELTSSpeakingReportDisclaimer,
		ScoreabilityStatus: result.Scoreability,
		GateStatus:         result.Gate,
		TestSummary:        projectIELTSSpeakingTestSummary(payload),
		Criteria: make(
			[]IELTSSpeakingReportCriterion,
			len(result.Criteria),
		),
		SpeakingOverall: IELTSSpeakingReportOverall{
			Status:      IELTSSpeakingOverallNotAvailable,
			Explanation: "当前证据还不能同时支持四项评分，因此暂不计算口语总分。",
		},
		PartReviews: make(
			[]IELTSSpeakingReportPartReview,
			len(ieltsPartOrder),
		),
		Questions: make(
			[]IELTSSpeakingReportQuestion,
			len(result.QuestionResults),
		),
		TargetPlan: IELTSSpeakingReportTargetPlan{
			Status: IELTSTargetPlanNotConfigured,
		},
		PriorityActions: []IELTSSpeakingReportPriorityRef{},
	}
	for index, criterion := range result.Criteria {
		report.Criteria[index] =
			projectIELTSSpeakingReportCriterion(criterion)
		for _, finding := range criterion.Improvements {
			if len(report.PriorityActions) == 3 {
				break
			}
			report.PriorityActions = append(
				report.PriorityActions,
				IELTSSpeakingReportPriorityRef{
					CriterionID: criterion.CriterionID,
					FindingID:   finding.ID,
				},
			)
		}
	}
	if overall, available := projectIELTSSpeakingOverall(report.Criteria); available {
		report.SpeakingOverall = overall
	}
	for index, question := range result.QuestionResults {
		opportunity := payload.OpportunityManifest[index]
		projected := IELTSSpeakingReportQuestion{
			QuestionID:        question.QuestionID,
			PartID:            question.PartID,
			Index:             question.Index,
			QuestionText:      opportunity.QuestionText,
			OpportunityStatus: question.OpportunityStatus,
			AssessmentStatus:  question.AssessmentStatus,
			ResponseTurnID:    question.ResponseTurnID,
			EvidenceRefIDs: slices.Clone(
				question.EvidenceRefIDs,
			),
			CriterionFindings: cloneIELTSQuestionCriterionRefs(
				question.CriterionFindings,
			),
		}
		if question.AssessmentStatus == scoring.IELTSAssessmentAssessed {
			turn, exists := turnsByID[question.ResponseTurnID]
			if !exists ||
				turn.QuestionID != question.QuestionID ||
				turn.Sequence != question.Index {
				return IELTSSpeakingReport{},
					scoring.ErrInvalidIELTSSpeakingShadow
			}
			projected.ConfirmedTranscript = turn.Transcript.Text
		}
		report.Questions[index] = projected
	}
	for index, part := range ieltsPartOrder {
		report.PartReviews[index] =
			projectIELTSSpeakingPartReview(
				part,
				report.Questions,
			)
	}
	if !report.Valid() {
		return IELTSSpeakingReport{},
			scoring.ErrInvalidIELTSSpeakingShadow
	}
	return report, nil
}

func projectIELTSSpeakingOverall(
	criteria []IELTSSpeakingReportCriterion,
) (IELTSSpeakingReportOverall, bool) {
	if len(criteria) != len(ieltsCriterionOrder) {
		return IELTSSpeakingReportOverall{}, false
	}
	total := 0
	for index, criterion := range criteria {
		if criterion.CriterionID != ieltsCriterionOrder[index] ||
			criterion.EstimatedBand == nil {
			return IELTSSpeakingReportOverall{}, false
		}
		total += *criterion.EstimatedBand
	}
	band := math.Round((float64(total)/float64(len(criteria)))*2) / 2
	return IELTSSpeakingReportOverall{
		Status:        IELTSSpeakingOverallAvailable,
		EstimatedBand: &band,
		Explanation:   "口语总分按四项练习估分等权平均，并四舍五入到最近的 0.5 分。",
	}, true
}

func projectIELTSSpeakingReportCriterion(
	source scoring.IELTSSpeakingShadowCriterionResult,
) IELTSSpeakingReportCriterion {
	var estimatedBand *int
	if source.EstimatedBand != nil {
		value := *source.EstimatedBand
		estimatedBand = &value
	}
	return IELTSSpeakingReportCriterion{
		CriterionID:     source.CriterionID,
		Scoreability:    source.Scoreability,
		Gate:            source.Gate,
		EstimatedBand:   estimatedBand,
		BandDescriptor:  source.BandDescriptor,
		Explanation:     ieltsCriterionExplanation(source),
		Coverage:        source.Coverage,
		Confidence:      source.Confidence,
		ReasonCodes:     slices.Clone(source.ReasonCodes),
		EvidenceRefIDs:  slices.Clone(source.EvidenceRefIDs),
		Strengths:       cloneIELTSFindings(source.Strengths),
		Improvements:    cloneIELTSFindings(source.Improvements),
		UpgradeExamples: cloneIELTSFindings(source.UpgradeExamples),
	}
}

func projectIELTSSpeakingTestSummary(
	payload evidence.SnapshotPayload,
) IELTSSpeakingReportTestSummary {
	summary := IELTSSpeakingReportTestSummary{
		QuestionCount: len(payload.OpportunityManifest),
	}
	for _, turn := range payload.ConfirmedTurns {
		summary.AnsweredCount++
		summary.RecordingDurationMS += turn.Audio.DurationMS
	}
	if len(payload.OpportunityManifest) >= scoring.IELTSQuestionCount {
		summary.Part1Topic = payload.OpportunityManifest[0].QuestionText
		summary.Part2Topic = payload.OpportunityManifest[8].QuestionText
		summary.Part3Topic = payload.OpportunityManifest[9].QuestionText
	}
	return summary
}

func ieltsCriterionExplanation(
	criterion scoring.IELTSSpeakingShadowCriterionResult,
) string {
	if criterion.Scoreability == scoring.IELTSSpeakingScoreabilityInsufficient {
		if criterion.CriterionID == scoring.IELTSCriterionPR {
			return "本次缺少可用于整场评分的可信发音工件，因此不展示发音分数。"
		}
		return "本次可确认的回答证据不足，因此不展示该维度分数。"
	}
	if criterion.BandDescriptor != "" {
		return criterion.BandDescriptor
	}
	switch criterion.CriterionID {
	case scoring.IELTSCriterionFC:
		return "根据已确认回答评估观点衔接与话题展开；缺少完整时序证据时只提供定性反馈。"
	case scoring.IELTSCriterionLR:
		return "根据已确认回答评估词汇范围、准确性、搭配和改述能力。"
	case scoring.IELTSCriterionGRA:
		return "根据已确认回答评估句式范围、复杂结构控制和语法准确性。"
	case scoring.IELTSCriterionPR:
		return "根据录音声学证据评估可理解度、音段、重音、节奏与语调。"
	default:
		return ""
	}
}

func cloneIELTSFindings(
	source []scoring.IELTSSpeakingShadowFinding,
) []scoring.IELTSSpeakingShadowFinding {
	result := make([]scoring.IELTSSpeakingShadowFinding, len(source))
	for index, finding := range source {
		result[index] = finding
		result[index].Evidence = slices.Clone(finding.Evidence)
	}
	return result
}

func cloneIELTSQuestionCriterionRefs(
	source []scoring.IELTSSpeakingQuestionCriterionFindingRefs,
) []scoring.IELTSSpeakingQuestionCriterionFindingRefs {
	result := make(
		[]scoring.IELTSSpeakingQuestionCriterionFindingRefs,
		len(source),
	)
	for index, refs := range source {
		result[index] = scoring.IELTSSpeakingQuestionCriterionFindingRefs{
			CriterionID: refs.CriterionID,
			StrengthFindingIDs: slices.Clone(
				refs.StrengthFindingIDs,
			),
			ImprovementFindingIDs: slices.Clone(
				refs.ImprovementFindingIDs,
			),
			UpgradeExampleFindingIDs: slices.Clone(
				refs.UpgradeExampleFindingIDs,
			),
		}
	}
	return result
}

func projectIELTSSpeakingPartReview(
	part scoring.IELTSPart,
	questions []IELTSSpeakingReportQuestion,
) IELTSSpeakingReportPartReview {
	result := IELTSSpeakingReportPartReview{
		PartID:                   part,
		QuestionIndexes:          []int{},
		EvidenceRefIDs:           []string{},
		StrengthFindingIDs:       []string{},
		ImprovementFindingIDs:    []string{},
		UpgradeExampleFindingIDs: []string{},
	}
	evidenceSet := make(map[string]struct{})
	strengthSet := make(map[string]struct{})
	improvementSet := make(map[string]struct{})
	upgradeSet := make(map[string]struct{})
	for _, question := range questions {
		if question.PartID != part {
			continue
		}
		result.QuestionIndexes = append(
			result.QuestionIndexes,
			question.Index,
		)
		for _, refID := range question.EvidenceRefIDs {
			evidenceSet[refID] = struct{}{}
		}
		for _, refs := range question.CriterionFindings {
			appendUniqueStrings(
				&result.StrengthFindingIDs,
				strengthSet,
				refs.StrengthFindingIDs,
			)
			appendUniqueStrings(
				&result.ImprovementFindingIDs,
				improvementSet,
				refs.ImprovementFindingIDs,
			)
			appendUniqueStrings(
				&result.UpgradeExampleFindingIDs,
				upgradeSet,
				refs.UpgradeExampleFindingIDs,
			)
		}
	}
	for refID := range evidenceSet {
		result.EvidenceRefIDs = append(result.EvidenceRefIDs, refID)
	}
	slices.Sort(result.EvidenceRefIDs)
	return result
}

func appendUniqueStrings(
	target *[]string,
	seen map[string]struct{},
	values []string,
) {
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		*target = append(*target, value)
	}
}

func (report IELTSSpeakingReport) Valid() bool {
	if report.SchemaVersion != IELTSSpeakingReportSchemaVersion ||
		report.DisclaimerCode !=
			IELTSSpeakingReportDisclaimerCode ||
		report.Disclaimer != IELTSSpeakingReportDisclaimer ||
		!validIELTSRootGate(
			report.ScoreabilityStatus,
			report.GateStatus,
		) ||
		!validIELTSSpeakingTestSummary(report.TestSummary) ||
		len(report.Criteria) != len(ieltsCriterionOrder) ||
		!validIELTSSpeakingOverall(report.SpeakingOverall, report.Criteria) ||
		report.SpeakingOverall.Explanation == "" ||
		len(report.PartReviews) != len(ieltsPartOrder) ||
		len(report.Questions) != scoring.IELTSQuestionCount ||
		report.TargetPlan.Status != IELTSTargetPlanNotConfigured ||
		report.PriorityActions == nil ||
		len(report.PriorityActions) > 3 {
		return false
	}
	findings := make(
		map[scoring.IELTSCriterion]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	improvements := make(
		map[scoring.IELTSCriterion]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	for index, criterion := range report.Criteria {
		if criterion.CriterionID != ieltsCriterionOrder[index] ||
			!validIELTSReportCriterion(
				report.ScoreabilityStatus,
				criterion,
			) {
			return false
		}
		findings[criterion.CriterionID] = make(map[string]struct{})
		improvements[criterion.CriterionID] =
			make(map[string]struct{})
		for _, collection := range []struct {
			values      []scoring.IELTSSpeakingShadowFinding
			improvement bool
		}{
			{criterion.Strengths, false},
			{criterion.Improvements, true},
			{criterion.UpgradeExamples, false},
		} {
			for _, finding := range collection.values {
				if _, duplicate :=
					findings[criterion.CriterionID][finding.ID]; duplicate {
					return false
				}
				findings[criterion.CriterionID][finding.ID] =
					struct{}{}
				if collection.improvement {
					improvements[criterion.CriterionID][finding.ID] =
						struct{}{}
				}
			}
		}
	}
	for index, question := range report.Questions {
		if !validIdentifier(question.QuestionID) ||
			question.Index != index+1 ||
			question.PartID != scoring.IELTSPartForQuestionIndex(index+1) ||
			!validReportText(
				question.QuestionText,
				reportMaximumInputString,
			) ||
			question.EvidenceRefIDs == nil ||
			len(question.CriterionFindings) !=
				len(ieltsCriterionOrder) {
			return false
		}
		if question.AssessmentStatus == scoring.IELTSAssessmentAssessed {
			if question.OpportunityStatus !=
				scoring.IELTSOpportunityProvided ||
				!validIdentifier(question.ResponseTurnID) ||
				!validReportText(
					question.ConfirmedTranscript,
					reportMaximumInputString,
				) ||
				len(question.EvidenceRefIDs) != 1 {
				return false
			}
		} else if question.AssessmentStatus !=
			scoring.IELTSAssessmentNotAssessed ||
			question.OpportunityStatus !=
				scoring.IELTSOpportunityNotProvided ||
			question.ResponseTurnID != "" ||
			question.ConfirmedTranscript != "" ||
			len(question.EvidenceRefIDs) != 0 {
			return false
		}
		for criterionIndex, refs := range question.CriterionFindings {
			if refs.CriterionID !=
				ieltsCriterionOrder[criterionIndex] ||
				!validStringList(refs.StrengthFindingIDs, 3) ||
				!validStringList(refs.ImprovementFindingIDs, 3) ||
				!validStringList(
					refs.UpgradeExampleFindingIDs,
					3,
				) {
				return false
			}
		}
	}
	for index, part := range report.PartReviews {
		if part.PartID != ieltsPartOrder[index] ||
			!slices.Equal(
				part.QuestionIndexes,
				ieltsQuestionIndexes(part.PartID),
			) ||
			!validStringList(part.EvidenceRefIDs, 14) ||
			!validStringList(part.StrengthFindingIDs, 36) ||
			!validStringList(part.ImprovementFindingIDs, 36) ||
			!validStringList(
				part.UpgradeExampleFindingIDs,
				36,
			) {
			return false
		}
	}
	for _, priority := range report.PriorityActions {
		if _, exists :=
			improvements[priority.CriterionID][priority.FindingID]; !exists {
			return false
		}
	}
	return true
}

func validIELTSSpeakingOverall(
	overall IELTSSpeakingReportOverall,
	criteria []IELTSSpeakingReportCriterion,
) bool {
	if overall.Explanation == "" {
		return false
	}
	expected, available := projectIELTSSpeakingOverall(criteria)
	if !available {
		return overall.Status == IELTSSpeakingOverallNotAvailable &&
			overall.EstimatedBand == nil
	}
	return overall.Status == IELTSSpeakingOverallAvailable &&
		overall.EstimatedBand != nil &&
		*overall.EstimatedBand == *expected.EstimatedBand
}

func validIELTSSpeakingTestSummary(
	summary IELTSSpeakingReportTestSummary,
) bool {
	return summary.QuestionCount == scoring.IELTSQuestionCount &&
		summary.AnsweredCount >= 0 &&
		summary.AnsweredCount <= summary.QuestionCount &&
		summary.RecordingDurationMS >= 0 &&
		validReportText(summary.Part1Topic, reportMaximumInputString) &&
		validReportText(summary.Part2Topic, reportMaximumInputString) &&
		validReportText(summary.Part3Topic, reportMaximumInputString)
}

func validIELTSRootGate(
	scoreability scoring.IELTSSpeakingScoreabilityStatus,
	gate scoring.IELTSSpeakingGateStatus,
) bool {
	return (scoreability == scoring.IELTSSpeakingScoreabilityProvisional &&
		gate == scoring.IELTSSpeakingGateFeedbackOnly) ||
		(scoreability == scoring.IELTSSpeakingScoreabilityInsufficient &&
			gate == scoring.IELTSSpeakingGateBlocked)
}

func validIELTSReportCriterion(
	root scoring.IELTSSpeakingScoreabilityStatus,
	criterion IELTSSpeakingReportCriterion,
) bool {
	if !validIELTSRootGate(
		criterion.Scoreability,
		criterion.Gate,
	) ||
		criterion.Explanation == "" ||
		criterion.Coverage < 0 || criterion.Coverage > 1 ||
		criterion.Confidence != 0 ||
		criterion.ReasonCodes == nil ||
		criterion.EvidenceRefIDs == nil ||
		criterion.Strengths == nil ||
		criterion.Improvements == nil ||
		criterion.UpgradeExamples == nil {
		return false
	}
	if root == scoring.IELTSSpeakingScoreabilityInsufficient &&
		criterion.Scoreability !=
			scoring.IELTSSpeakingScoreabilityInsufficient {
		return false
	}
	if criterion.Scoreability ==
		scoring.IELTSSpeakingScoreabilityInsufficient {
		return criterion.EstimatedBand == nil &&
			criterion.BandDescriptor == "" &&
			len(criterion.EvidenceRefIDs) == 0 &&
			len(criterion.Strengths) == 0 &&
			len(criterion.Improvements) == 0 &&
			len(criterion.UpgradeExamples) == 0 &&
			len(criterion.ReasonCodes) == 1
	}
	if len(criterion.EvidenceRefIDs) == 0 ||
		len(criterion.Strengths)+len(criterion.Improvements) == 0 {
		return false
	}
	switch criterion.CriterionID {
	case scoring.IELTSCriterionFC:
		if criterion.EstimatedBand == nil {
			return criterion.BandDescriptor == ""
		}
		fallthrough
	case scoring.IELTSCriterionLR, scoring.IELTSCriterionGRA,
		scoring.IELTSCriterionPR:
		if criterion.EstimatedBand == nil {
			return false
		}
		_, descriptor, valid := scoring.MapIELTSBand(
			criterion.CriterionID,
			*criterion.EstimatedBand,
		)
		return valid && criterion.BandDescriptor == descriptor
	default:
		return false
	}
}

func ieltsQuestionIndexes(part scoring.IELTSPart) []int {
	switch part {
	case scoring.IELTSPart1:
		return []int{1, 2, 3, 4, 5, 6, 7, 8}
	case scoring.IELTSPart2:
		return []int{9}
	case scoring.IELTSPart3:
		return []int{10, 11, 12, 13, 14}
	default:
		return []int{}
	}
}

func validStringList(values []string, maximum int) bool {
	if values == nil || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validIdentifier(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
