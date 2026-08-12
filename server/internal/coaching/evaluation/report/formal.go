package report

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

const FormalReportSchemaVersion = "evaluation-report/v1"

type ReportScoreability string

const (
	ReportScoreabilityProvisional  ReportScoreability = "PROVISIONAL"
	ReportScoreabilityInsufficient ReportScoreability = "INSUFFICIENT"
)

type ReportScoreScale string

const (
	ReportScalePercentage100 ReportScoreScale = "PERCENTAGE_100"
	ReportScaleIELTSBand     ReportScoreScale = "IELTS_BAND"
)

type FormalReport struct {
	SchemaVersion      string                 `json:"schema_version"`
	SceneType          evaluation.SceneType   `json:"scene_type"`
	PracticeExperience string                 `json:"practice_experience"`
	SceneCategory      string                 `json:"scene_category"`
	PracticeMode       string                 `json:"practice_mode"`
	ScoreabilityStatus ReportScoreability     `json:"scoreability_status"`
	Summary            string                 `json:"summary"`
	Dimensions         []ReportDimension      `json:"dimensions"`
	PriorityActions    []ReportPriorityAction `json:"priority_actions"`
	DetailSchema       string                 `json:"detail_schema"`
	Detail             json.RawMessage        `json:"detail"`
}

type ReportDimension struct {
	Key          string           `json:"key"`
	Score        *float64         `json:"score,omitempty"`
	Scale        ReportScoreScale `json:"scale"`
	Coverage     float64          `json:"coverage"`
	Confidence   float64          `json:"confidence"`
	ReasonCodes  []string         `json:"reason_codes"`
	EvidenceRefs []string         `json:"evidence_ref_ids"`
	Strengths    []ReportFinding  `json:"strengths"`
	Improvements []ReportFinding  `json:"improvements"`
	Examples     []ReportFinding  `json:"recommended_examples"`
}

type ReportFinding struct {
	ID         string           `json:"finding_id"`
	Message    string           `json:"message"`
	Suggestion string           `json:"suggestion,omitempty"`
	Evidence   []ReportEvidence `json:"evidence"`
}

type ReportEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	TurnID          string `json:"turn_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

type ReportPriorityAction struct {
	DimensionKey string `json:"dimension_key"`
	FindingID    string `json:"finding_id"`
}

type StoredFormalReport struct {
	ReportID             string       `json:"report_id"`
	EvaluationID         string       `json:"evaluation_id"`
	EvaluationRevisionID string       `json:"evaluation_revision_id"`
	OwnerUserID          string       `json:"-"`
	PracticeSessionID    string       `json:"practice_session_id"`
	Revision             int          `json:"revision"`
	Report               FormalReport `json:"report"`
	CreatedAt            time.Time    `json:"created_at"`
}

func ProjectInterviewFormalReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.InterviewShadowResult,
) (FormalReport, error) {
	detail, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		return FormalReport{}, err
	}
	practiceContext, err := reportPracticeContext(snapshot)
	if err != nil {
		return FormalReport{}, err
	}
	report := FormalReport{
		SchemaVersion:      FormalReportSchemaVersion,
		SceneType:          evaluation.SceneInterview,
		PracticeExperience: practiceContext.PracticeExperience,
		SceneCategory:      practiceContext.SceneCategory,
		PracticeMode:       practiceContext.PracticeMode,
		ScoreabilityStatus: ReportScoreabilityProvisional,
		Summary:            "本次练习已形成面试表达评估，可按优先行动继续复练。",
		Dimensions:         make([]ReportDimension, len(detail.Dimensions)),
		PriorityActions:    make([]ReportPriorityAction, len(detail.PriorityActions)),
		DetailSchema:       detail.SchemaVersion,
	}
	if detail.ScoreabilityStatus == scoring.InterviewScoreabilityInsufficient {
		report.ScoreabilityStatus = ReportScoreabilityInsufficient
		report.Summary = "本次练习的有效证据不足，暂不形成能力结论。"
	}
	for index, dimension := range detail.Dimensions {
		projected := ReportDimension{
			Key:          string(dimension.DimensionID),
			Scale:        ReportScalePercentage100,
			Coverage:     dimension.Coverage,
			Confidence:   dimension.Confidence,
			ReasonCodes:  stringValues(dimension.ReasonCodes),
			EvidenceRefs: slices.Clone(dimension.EvidenceRefIDs),
			Strengths:    interviewReportFindings(dimension.Strengths),
			Improvements: interviewReportFindings(dimension.Improvements),
			Examples: interviewReportFindings(
				dimension.RecommendedExpressions,
			),
		}
		if dimension.Score != nil {
			value := float64(*dimension.Score)
			projected.Score = &value
		}
		report.Dimensions[index] = projected
	}
	for index, action := range detail.PriorityActions {
		report.PriorityActions[index] = ReportPriorityAction{
			DimensionKey: string(action.DimensionID),
			FindingID:    action.FindingID,
		}
	}
	report.Detail, err = json.Marshal(detail)
	if err != nil || !report.Valid() {
		return FormalReport{}, evaluation.ErrInvalidRequest
	}
	return report, nil
}

func ProjectIELTSFormalReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.IELTSSpeakingShadowResult,
) (FormalReport, error) {
	practiceContext, err := reportPracticeContext(snapshot)
	if err != nil {
		return FormalReport{}, err
	}
	var detail any
	if practiceContext.PracticeMode == "FULL_MOCK" {
		detail, err = ProjectIELTSSpeakingReport(snapshot, result)
	} else {
		detail, err = ProjectIELTSSpeakingBandPracticeReport(snapshot, result)
	}
	if err != nil {
		return FormalReport{}, err
	}
	report := FormalReport{
		SchemaVersion:      FormalReportSchemaVersion,
		SceneType:          evaluation.SceneIELTSSpeaking,
		PracticeExperience: practiceContext.PracticeExperience,
		SceneCategory:      practiceContext.SceneCategory,
		PracticeMode:       practiceContext.PracticeMode,
		ScoreabilityStatus: ReportScoreabilityProvisional,
		Summary:            "本次练习已形成 IELTS 口语评估，可按优先行动继续复练。",
		Dimensions:         make([]ReportDimension, len(result.Criteria)),
		PriorityActions:    []ReportPriorityAction{},
	}
	switch value := detail.(type) {
	case IELTSSpeakingReport:
		report.DetailSchema = value.SchemaVersion
	case IELTSSpeakingPracticeReport:
		report.DetailSchema = value.SchemaVersion
	default:
		return FormalReport{}, evaluation.ErrInvalidRequest
	}
	if result.Scoreability == scoring.IELTSSpeakingScoreabilityInsufficient {
		report.ScoreabilityStatus = ReportScoreabilityInsufficient
		report.Summary = "本次练习的有效证据不足，暂不形成能力结论。"
	}
	for index, criterion := range result.Criteria {
		projected := ReportDimension{
			Key:          string(criterion.CriterionID),
			Scale:        ReportScaleIELTSBand,
			Coverage:     criterion.Coverage,
			Confidence:   criterion.Confidence,
			ReasonCodes:  stringValues(criterion.ReasonCodes),
			EvidenceRefs: slices.Clone(criterion.EvidenceRefIDs),
			Strengths:    ieltsReportFindings(criterion.Strengths),
			Improvements: ieltsReportFindings(criterion.Improvements),
			Examples:     ieltsReportFindings(criterion.UpgradeExamples),
		}
		if criterion.EstimatedBand != nil {
			value := float64(*criterion.EstimatedBand)
			projected.Score = &value
		}
		report.Dimensions[index] = projected
		for _, finding := range criterion.Improvements {
			if len(report.PriorityActions) == 3 {
				break
			}
			report.PriorityActions = append(report.PriorityActions, ReportPriorityAction{
				DimensionKey: string(criterion.CriterionID),
				FindingID:    finding.ID,
			})
		}
	}
	report.Detail, err = json.Marshal(detail)
	if err != nil || !report.Valid() {
		return FormalReport{}, evaluation.ErrInvalidRequest
	}
	return report, nil
}

func (report FormalReport) Valid() bool {
	if report.SchemaVersion != FormalReportSchemaVersion ||
		!validSceneType(report.SceneType) ||
		!validIdentifier(report.PracticeExperience) ||
		!validIdentifier(report.SceneCategory) ||
		!validIdentifier(report.PracticeMode) ||
		(report.ScoreabilityStatus != ReportScoreabilityProvisional &&
			report.ScoreabilityStatus != ReportScoreabilityInsufficient) ||
		strings.TrimSpace(report.Summary) == "" ||
		len(report.Summary) > 2048 || len(report.Dimensions) == 0 ||
		len(report.Dimensions) > 16 || report.PriorityActions == nil ||
		len(report.PriorityActions) > 5 ||
		!validVersion(report.DetailSchema) || len(report.Detail) == 0 ||
		len(report.Detail) > 256*1024 {
		return false
	}
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(report.Detail, &detail); err != nil || detail == nil {
		return false
	}
	seen := make(map[string]struct{}, len(report.Dimensions))
	findings := make(map[string]struct{})
	improvementDimensions := make(map[string]string)
	for _, dimension := range report.Dimensions {
		if !dimension.valid(report.ScoreabilityStatus) {
			return false
		}
		if _, exists := seen[dimension.Key]; exists {
			return false
		}
		seen[dimension.Key] = struct{}{}
		for _, collection := range [][]ReportFinding{
			dimension.Strengths,
			dimension.Improvements,
			dimension.Examples,
		} {
			for _, finding := range collection {
				if _, exists := findings[finding.ID]; exists {
					return false
				}
				findings[finding.ID] = struct{}{}
			}
		}
		for _, finding := range dimension.Improvements {
			improvementDimensions[finding.ID] = dimension.Key
		}
	}
	actions := make(map[string]struct{}, len(report.PriorityActions))
	for _, action := range report.PriorityActions {
		if _, exists := seen[action.DimensionKey]; !exists {
			return false
		}
		if improvementDimensions[action.FindingID] != action.DimensionKey {
			return false
		}
		key := action.DimensionKey + "\x00" + action.FindingID
		if _, exists := actions[key]; exists {
			return false
		}
		actions[key] = struct{}{}
	}
	return true
}

func (report StoredFormalReport) Valid() bool {
	return validUUID(report.ReportID) &&
		validUUID(report.EvaluationID) &&
		validUUID(report.EvaluationRevisionID) &&
		validUUID(report.OwnerUserID) &&
		validIdentifier(report.PracticeSessionID) &&
		report.Revision >= 1 && report.Report.Valid() &&
		!report.CreatedAt.IsZero()
}

func (dimension ReportDimension) valid(
	reportScoreability ReportScoreability,
) bool {
	if !validVersion(dimension.Key) ||
		(dimension.Scale != ReportScalePercentage100 &&
			dimension.Scale != ReportScaleIELTSBand) ||
		math.IsNaN(dimension.Coverage) ||
		math.IsInf(dimension.Coverage, 0) ||
		dimension.Coverage < 0 || dimension.Coverage > 1 ||
		math.IsNaN(dimension.Confidence) ||
		math.IsInf(dimension.Confidence, 0) ||
		dimension.Confidence < 0 || dimension.Confidence > 1 ||
		dimension.ReasonCodes == nil || dimension.EvidenceRefs == nil ||
		dimension.Strengths == nil || dimension.Improvements == nil ||
		dimension.Examples == nil {
		return false
	}
	if reportScoreability == ReportScoreabilityInsufficient &&
		dimension.Score != nil {
		return false
	}
	if dimension.Score != nil {
		if math.IsNaN(*dimension.Score) || math.IsInf(*dimension.Score, 0) {
			return false
		}
		switch dimension.Scale {
		case ReportScalePercentage100:
			if *dimension.Score < 0 || *dimension.Score > 100 {
				return false
			}
		case ReportScaleIELTSBand:
			if *dimension.Score < 0 || *dimension.Score > 9 {
				return false
			}
		}
	}
	for _, collection := range [][]ReportFinding{
		dimension.Strengths,
		dimension.Improvements,
		dimension.Examples,
	} {
		if len(collection) > 5 {
			return false
		}
		for _, finding := range collection {
			if !finding.valid() {
				return false
			}
		}
	}
	return true
}

func (finding ReportFinding) valid() bool {
	if !validVersion(finding.ID) || strings.TrimSpace(finding.Message) == "" ||
		len(finding.Message) > 2048 || len(finding.Suggestion) > 2048 ||
		finding.Evidence == nil || len(finding.Evidence) > 8 {
		return false
	}
	for _, evidence := range finding.Evidence {
		if !validIdentifier(evidence.EvidenceRefID) ||
			!validIdentifier(evidence.TurnID) ||
			evidence.StartUTF8Byte < 0 ||
			evidence.EndUTF8Byte <= evidence.StartUTF8Byte ||
			strings.TrimSpace(evidence.OriginalExcerpt) == "" {
			return false
		}
	}
	return true
}

func reportPracticeContext(
	snapshot evidence.EvidenceSnapshot,
) (evidence.PracticeContext, error) {
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil ||
		payload.PracticeContext.PracticeExperience == "" ||
		payload.PracticeContext.SceneCategory == "" ||
		payload.PracticeContext.PracticeMode == "" {
		return evidence.PracticeContext{}, evaluation.ErrInvalidRequest
	}
	return payload.PracticeContext, nil
}

func interviewReportFindings(
	items []InterviewReportFinding,
) []ReportFinding {
	result := make([]ReportFinding, len(items))
	for index, item := range items {
		evidence := make([]ReportEvidence, len(item.Evidence))
		for evidenceIndex, itemEvidence := range item.Evidence {
			evidence[evidenceIndex] = ReportEvidence(itemEvidence)
		}
		result[index] = ReportFinding{
			ID:         item.FindingID,
			Message:    item.Message,
			Suggestion: item.Suggestion,
			Evidence:   evidence,
		}
	}
	return result
}

func ieltsReportFindings(
	items []scoring.IELTSSpeakingShadowFinding,
) []ReportFinding {
	result := make([]ReportFinding, len(items))
	for index, item := range items {
		evidence := make([]ReportEvidence, len(item.Evidence))
		for evidenceIndex, itemEvidence := range item.Evidence {
			evidence[evidenceIndex] = ReportEvidence(itemEvidence)
		}
		result[index] = ReportFinding{
			ID:         item.ID,
			Message:    item.Message,
			Suggestion: item.Suggestion,
			Evidence:   evidence,
		}
	}
	return result
}

func stringValues[T ~string](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}
