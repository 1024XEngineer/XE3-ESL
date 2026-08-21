package report

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

const FormalReportSchemaVersion = "evaluation-report/v2"

type ReportScoreability string

const (
	ReportScoreabilityProvisional  ReportScoreability = "PROVISIONAL"
	ReportScoreabilityInsufficient ReportScoreability = "INSUFFICIENT"
)

type ReportScoreScale string

const (
	ReportScalePercentage100 ReportScoreScale = "PERCENTAGE_100"
	ReportScaleIELTSBand     ReportScoreScale = "IELTS_BAND_9"
)

// FormalReport is the only user-facing session report. Scene-specific
// evaluators share this envelope instead of persisting parallel report models.
type FormalReport struct {
	SchemaVersion      string                 `json:"schema_version"`
	SceneType          evaluation.SceneType   `json:"scene_type"`
	PracticeExperience string                 `json:"practice_experience"`
	SceneCategory      string                 `json:"scene_category"`
	PracticeMode       string                 `json:"practice_mode"`
	ScoreabilityStatus ReportScoreability     `json:"scoreability_status"`
	Summary            string                 `json:"summary"`
	Questions          []ReportQuestion       `json:"questions"`
	Dimensions         []ReportDimension      `json:"dimensions"`
	PriorityActions    []ReportPriorityAction `json:"priority_actions"`
}

// ReportQuestion is the deterministic Practice evidence projection shown in a
// session report. Evaluators never generate or rewrite Questions or Answers.
type ReportQuestion struct {
	ID               string        `json:"question_id"`
	Position         int           `json:"position"`
	ParentQuestionID string        `json:"parent_question_id,omitempty"`
	Text             string        `json:"text"`
	Answer           *ReportAnswer `json:"answer"`
}

type ReportAnswer struct {
	TurnID     string `json:"turn_id"`
	Transcript string `json:"transcript"`
}

type ReportDimension struct {
	Key          string           `json:"key"`
	Score        *float64         `json:"score"`
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
	ReportID          string       `json:"report_id"`
	EvaluationID      string       `json:"evaluation_id"`
	OwnerUserID       string       `json:"-"`
	PracticeSessionID string       `json:"practice_session_id"`
	Report            FormalReport `json:"report"`
	CreatedAt         time.Time    `json:"created_at"`
}

func (value FormalReport) Valid() bool {
	if value.SchemaVersion != FormalReportSchemaVersion ||
		!validSceneType(value.SceneType) ||
		!validIdentifier(value.PracticeExperience) ||
		!validIdentifier(value.SceneCategory) ||
		!validIdentifier(value.PracticeMode) ||
		(value.ScoreabilityStatus != ReportScoreabilityProvisional &&
			value.ScoreabilityStatus != ReportScoreabilityInsufficient) ||
		!validText(value.Summary, 2048) || len(value.Questions) == 0 ||
		len(value.Questions) > 128 || len(value.Dimensions) == 0 ||
		len(value.Dimensions) > 8 || value.PriorityActions == nil ||
		len(value.PriorityActions) > 5 {
		return false
	}
	questionIDs := make(map[string]struct{}, len(value.Questions))
	positions := make(map[int]struct{}, len(value.Questions))
	answerTurnIDs := make(map[string]struct{}, len(value.Questions))
	for _, question := range value.Questions {
		if !question.valid() {
			return false
		}
		if _, duplicate := questionIDs[question.ID]; duplicate {
			return false
		}
		if _, duplicate := positions[question.Position]; duplicate {
			return false
		}
		questionIDs[question.ID] = struct{}{}
		positions[question.Position] = struct{}{}
		if question.Answer != nil {
			if _, duplicate := answerTurnIDs[question.Answer.TurnID]; duplicate {
				return false
			}
			answerTurnIDs[question.Answer.TurnID] = struct{}{}
		}
	}
	for _, question := range value.Questions {
		if question.ParentQuestionID != "" {
			if _, exists := questionIDs[question.ParentQuestionID]; !exists ||
				question.ParentQuestionID == question.ID {
				return false
			}
		}
	}
	dimensions := make(map[string]struct{}, len(value.Dimensions))
	improvements := make(map[string]string)
	findings := make(map[string]struct{})
	for _, dimension := range value.Dimensions {
		if !dimension.valid(value.ScoreabilityStatus) {
			return false
		}
		if _, duplicate := dimensions[dimension.Key]; duplicate {
			return false
		}
		dimensions[dimension.Key] = struct{}{}
		for _, collection := range [][]ReportFinding{
			dimension.Strengths,
			dimension.Improvements,
			dimension.Examples,
		} {
			for _, finding := range collection {
				for _, evidence := range finding.Evidence {
					if _, exists := answerTurnIDs[evidence.TurnID]; !exists {
						return false
					}
				}
				if _, duplicate := findings[finding.ID]; duplicate {
					return false
				}
				findings[finding.ID] = struct{}{}
			}
		}
		for _, finding := range dimension.Improvements {
			improvements[finding.ID] = dimension.Key
		}
	}
	actions := make(map[string]struct{}, len(value.PriorityActions))
	for _, action := range value.PriorityActions {
		if improvements[action.FindingID] != action.DimensionKey {
			return false
		}
		key := action.DimensionKey + "\x00" + action.FindingID
		if _, duplicate := actions[key]; duplicate {
			return false
		}
		actions[key] = struct{}{}
	}
	return true
}

func (question ReportQuestion) valid() bool {
	return validUUID(question.ID) && question.Position > 0 &&
		(question.ParentQuestionID == "" || validUUID(question.ParentQuestionID)) &&
		validText(question.Text, 16*1024) &&
		(question.Answer == nil || question.Answer.valid())
}

func (answer ReportAnswer) valid() bool {
	return validUUID(answer.TurnID) && validText(answer.Transcript, 64*1024)
}

func (value StoredFormalReport) Valid() bool {
	return validUUID(value.ReportID) && value.ReportID == value.EvaluationID &&
		validUUID(value.OwnerUserID) && validUUID(value.PracticeSessionID) &&
		value.Report.Valid() && !value.CreatedAt.IsZero()
}

func (dimension ReportDimension) valid(scoreability ReportScoreability) bool {
	if !validVersion(dimension.Key) ||
		(dimension.Scale != ReportScalePercentage100 &&
			dimension.Scale != ReportScaleIELTSBand) ||
		!validRatio(dimension.Coverage) || !validRatio(dimension.Confidence) ||
		dimension.ReasonCodes == nil || dimension.EvidenceRefs == nil ||
		dimension.Strengths == nil || dimension.Improvements == nil ||
		dimension.Examples == nil || len(dimension.ReasonCodes) > 8 ||
		len(dimension.EvidenceRefs) > 128 {
		return false
	}
	if scoreability == ReportScoreabilityInsufficient && dimension.Score != nil {
		return false
	}
	if dimension.Score != nil {
		if math.IsNaN(*dimension.Score) || math.IsInf(*dimension.Score, 0) {
			return false
		}
		if dimension.Scale == ReportScalePercentage100 &&
			(*dimension.Score < 0 || *dimension.Score > 100) {
			return false
		}
		if dimension.Scale == ReportScaleIELTSBand &&
			(*dimension.Score < 0 || *dimension.Score > 9 ||
				math.Mod(*dimension.Score*2, 1) != 0) {
			return false
		}
	}
	for _, reason := range dimension.ReasonCodes {
		if !validIdentifier(reason) {
			return false
		}
	}
	for _, evidenceRef := range dimension.EvidenceRefs {
		if !validUUID(evidenceRef) {
			return false
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
	if !validVersion(finding.ID) || !validText(finding.Message, 2048) ||
		(finding.Suggestion != "" && !validText(finding.Suggestion, 2048)) ||
		finding.Evidence == nil || len(finding.Evidence) > 8 {
		return false
	}
	for _, item := range finding.Evidence {
		if !validUUID(item.EvidenceRefID) ||
			!validUUID(item.TurnID) || item.EvidenceRefID != item.TurnID ||
			item.StartUTF8Byte < 0 || item.EndUTF8Byte <= item.StartUTF8Byte ||
			item.EndUTF8Byte-item.StartUTF8Byte != len(item.OriginalExcerpt) ||
			!validText(item.OriginalExcerpt, 16*1024) {
			return false
		}
	}
	return true
}

func validRatio(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) &&
		value != "" && len(value) <= maximumBytes &&
		!strings.ContainsRune(value, '\x00')
}
