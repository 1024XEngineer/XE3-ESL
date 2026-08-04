package evaluation

import (
	"bytes"
	"encoding/json"
	"slices"
)

const (
	IELTSSpeakingReportSchemaVersion  = "ielts-speaking-report/v1"
	IELTSSpeakingReportDisclaimerCode = "AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS"
	IELTSSpeakingReportDisclaimer     = "AI 练习估分，非 IELTS 官方成绩。"
	IELTSSpeakingOverallNotAvailable  = "NOT_AVAILABLE"
	IELTSTargetPlanNotConfigured      = "NOT_CONFIGURED"
)

type IELTSSpeakingReport struct {
	SchemaVersion      string                           `json:"schema_version"`
	DisclaimerCode     string                           `json:"disclaimer_code"`
	Disclaimer         string                           `json:"disclaimer"`
	ScoreabilityStatus IELTSSpeakingScoreabilityStatus  `json:"scoreability_status"`
	GateStatus         IELTSSpeakingGateStatus          `json:"gate_status"`
	Criteria           []IELTSSpeakingReportCriterion   `json:"criteria"`
	SpeakingOverall    IELTSSpeakingReportOverall       `json:"speaking_overall"`
	PartReviews        []IELTSSpeakingReportPartReview  `json:"part_reviews"`
	Questions          []IELTSSpeakingReportQuestion    `json:"questions"`
	TargetPlan         IELTSSpeakingReportTargetPlan    `json:"target_plan"`
	PriorityActions    []IELTSSpeakingReportPriorityRef `json:"priority_actions"`
}

type IELTSSpeakingReportCriterion struct {
	CriterionID     IELTSCriterion                  `json:"criterion_id"`
	Scoreability    IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	Gate            IELTSSpeakingGateStatus         `json:"gate_status"`
	EstimatedBand   *int                            `json:"estimated_band,omitempty"`
	BandDescriptor  string                          `json:"band_descriptor,omitempty"`
	Coverage        float64                         `json:"coverage"`
	Confidence      float64                         `json:"confidence"`
	ReasonCodes     []IELTSSpeakingReasonCode       `json:"reason_codes"`
	EvidenceRefIDs  []string                        `json:"evidence_ref_ids"`
	Strengths       []IELTSSpeakingShadowFinding    `json:"strengths"`
	Improvements    []IELTSSpeakingShadowFinding    `json:"improvements"`
	UpgradeExamples []IELTSSpeakingShadowFinding    `json:"upgrade_examples"`
}

type IELTSSpeakingReportOverall struct {
	Status string `json:"status"`
}

type IELTSSpeakingReportPartReview struct {
	PartID                   IELTSPart `json:"part_id"`
	QuestionIndexes          []int     `json:"question_indexes"`
	EvidenceRefIDs           []string  `json:"evidence_ref_ids"`
	StrengthFindingIDs       []string  `json:"strength_finding_ids"`
	ImprovementFindingIDs    []string  `json:"improvement_finding_ids"`
	UpgradeExampleFindingIDs []string  `json:"upgrade_example_finding_ids"`
}

type IELTSSpeakingReportQuestion struct {
	QuestionID          string                                      `json:"question_id"`
	PartID              IELTSPart                                   `json:"part_id"`
	Index               int                                         `json:"index"`
	QuestionText        string                                      `json:"question_text"`
	OpportunityStatus   IELTSSpeakingOpportunityStatus              `json:"opportunity_status"`
	AssessmentStatus    IELTSSpeakingAssessmentStatus               `json:"assessment_status"`
	ConfirmedTranscript string                                      `json:"confirmed_transcript,omitempty"`
	ResponseTurnID      string                                      `json:"response_turn_id,omitempty"`
	EvidenceRefIDs      []string                                    `json:"evidence_ref_ids"`
	CriterionFindings   []IELTSSpeakingQuestionCriterionFindingRefs `json:"criterion_findings"`
}

type IELTSSpeakingReportTargetPlan struct {
	Status string `json:"status"`
}

type IELTSSpeakingReportPriorityRef struct {
	CriterionID IELTSCriterion `json:"criterion_id"`
	FindingID   string         `json:"finding_id"`
}

func ProjectIELTSSpeakingReport(
	snapshot EvidenceSnapshot,
	result IELTSSpeakingShadowResult,
) (IELTSSpeakingReport, error) {
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		return IELTSSpeakingReport{}, err
	}
	var evidence evidencePayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&evidence) != nil ||
		ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingReport{},
			ErrInvalidIELTSSpeakingShadow
	}
	turnsByID := make(
		map[string]evidenceConfirmedTurn,
		len(evidence.ConfirmedTurns),
	)
	for _, turn := range evidence.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}

	report := IELTSSpeakingReport{
		SchemaVersion:      IELTSSpeakingReportSchemaVersion,
		DisclaimerCode:     IELTSSpeakingReportDisclaimerCode,
		Disclaimer:         IELTSSpeakingReportDisclaimer,
		ScoreabilityStatus: result.Scoreability,
		GateStatus:         result.Gate,
		Criteria: make(
			[]IELTSSpeakingReportCriterion,
			len(result.Criteria),
		),
		SpeakingOverall: IELTSSpeakingReportOverall{
			Status: IELTSSpeakingOverallNotAvailable,
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
	for index, question := range result.QuestionResults {
		opportunity := evidence.OpportunityManifest[index]
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
		if question.AssessmentStatus == IELTSAssessmentAssessed {
			turn, exists := turnsByID[question.ResponseTurnID]
			if !exists ||
				turn.QuestionID != question.QuestionID ||
				turn.Sequence != question.Index {
				return IELTSSpeakingReport{},
					ErrInvalidIELTSSpeakingShadow
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
			ErrInvalidIELTSSpeakingShadow
	}
	return report, nil
}

func projectIELTSSpeakingReportCriterion(
	source IELTSSpeakingShadowCriterionResult,
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
		Coverage:        source.Coverage,
		Confidence:      source.Confidence,
		ReasonCodes:     slices.Clone(source.ReasonCodes),
		EvidenceRefIDs:  slices.Clone(source.EvidenceRefIDs),
		Strengths:       cloneIELTSFindings(source.Strengths),
		Improvements:    cloneIELTSFindings(source.Improvements),
		UpgradeExamples: cloneIELTSFindings(source.UpgradeExamples),
	}
}

func cloneIELTSFindings(
	source []IELTSSpeakingShadowFinding,
) []IELTSSpeakingShadowFinding {
	result := make([]IELTSSpeakingShadowFinding, len(source))
	for index, finding := range source {
		result[index] = finding
		result[index].Evidence = slices.Clone(finding.Evidence)
	}
	return result
}

func cloneIELTSQuestionCriterionRefs(
	source []IELTSSpeakingQuestionCriterionFindingRefs,
) []IELTSSpeakingQuestionCriterionFindingRefs {
	result := make(
		[]IELTSSpeakingQuestionCriterionFindingRefs,
		len(source),
	)
	for index, refs := range source {
		result[index] = IELTSSpeakingQuestionCriterionFindingRefs{
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
	part IELTSPart,
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
		len(report.Criteria) != len(ieltsCriterionOrder) ||
		report.SpeakingOverall.Status !=
			IELTSSpeakingOverallNotAvailable ||
		len(report.PartReviews) != len(ieltsPartOrder) ||
		len(report.Questions) != ieltsQuestionCount ||
		report.TargetPlan.Status != IELTSTargetPlanNotConfigured ||
		report.PriorityActions == nil ||
		len(report.PriorityActions) > 3 {
		return false
	}
	findings := make(
		map[IELTSCriterion]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	improvements := make(
		map[IELTSCriterion]map[string]struct{},
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
			values      []IELTSSpeakingShadowFinding
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
			question.PartID != ieltsPartForQuestionIndex(index+1) ||
			!validInterviewText(
				question.QuestionText,
				interviewShadowMaximumInputString,
			) ||
			question.EvidenceRefIDs == nil ||
			len(question.CriterionFindings) !=
				len(ieltsCriterionOrder) {
			return false
		}
		if question.AssessmentStatus == IELTSAssessmentAssessed {
			if question.OpportunityStatus !=
				IELTSOpportunityProvided ||
				!validIdentifier(question.ResponseTurnID) ||
				!validInterviewText(
					question.ConfirmedTranscript,
					interviewShadowMaximumInputString,
				) ||
				len(question.EvidenceRefIDs) != 1 {
				return false
			}
		} else if question.AssessmentStatus !=
			IELTSAssessmentNotAssessed ||
			question.OpportunityStatus !=
				IELTSOpportunityNotProvided ||
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

func validIELTSRootGate(
	scoreability IELTSSpeakingScoreabilityStatus,
	gate IELTSSpeakingGateStatus,
) bool {
	return (scoreability == IELTSSpeakingScoreabilityProvisional &&
		gate == IELTSSpeakingGateFeedbackOnly) ||
		(scoreability == IELTSSpeakingScoreabilityInsufficient &&
			gate == IELTSSpeakingGateBlocked)
}

func validIELTSReportCriterion(
	root IELTSSpeakingScoreabilityStatus,
	criterion IELTSSpeakingReportCriterion,
) bool {
	if !validIELTSRootGate(
		criterion.Scoreability,
		criterion.Gate,
	) ||
		criterion.Coverage < 0 || criterion.Coverage > 1 ||
		criterion.Confidence != 0 ||
		criterion.ReasonCodes == nil ||
		criterion.EvidenceRefIDs == nil ||
		criterion.Strengths == nil ||
		criterion.Improvements == nil ||
		criterion.UpgradeExamples == nil {
		return false
	}
	if root == IELTSSpeakingScoreabilityInsufficient &&
		criterion.Scoreability !=
			IELTSSpeakingScoreabilityInsufficient {
		return false
	}
	if criterion.Scoreability ==
		IELTSSpeakingScoreabilityInsufficient {
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
	case IELTSCriterionFC:
		return criterion.EstimatedBand == nil &&
			criterion.BandDescriptor == ""
	case IELTSCriterionLR, IELTSCriterionGRA:
		if criterion.EstimatedBand == nil {
			return false
		}
		_, descriptor, valid := mapIELTSBand(
			criterion.CriterionID,
			*criterion.EstimatedBand,
		)
		return valid && criterion.BandDescriptor == descriptor
	case IELTSCriterionPR:
		return false
	default:
		return false
	}
}

func ieltsQuestionIndexes(part IELTSPart) []int {
	switch part {
	case IELTSPart1:
		return []int{1, 2, 3, 4, 5, 6, 7, 8}
	case IELTSPart2:
		return []int{9}
	case IELTSPart3:
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
