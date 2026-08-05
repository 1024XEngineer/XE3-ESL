package review

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidReview           = errors.New("review: invalid request")
	ErrReviewNotFound          = errors.New("review: report not found")
	ErrAccountDeleted          = errors.New("review: account data deleted")
	ErrDeletionGenerationStale = errors.New(
		"review: deletion generation stale",
	)
)

var reviewUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
)

var reviewVersionPattern = regexp.MustCompile(
	`^[A-Za-z][A-Za-z0-9._:/-]{0,127}$`,
)

type Actor struct {
	UserID string
}

func (actor Actor) validate() error {
	if !validUUID(actor.UserID) {
		return ErrInvalidReview
	}
	return nil
}

type Report struct {
	ID                   string
	EvaluationID         string
	EvaluationRevisionID string
	OwnerUserID          string
	PracticeSessionID    string
	Revision             int
	SchemaVersion        string
	SceneType            string
	PracticeExperience   string
	SceneCategory        string
	PracticeMode         string
	ScoreabilityStatus   string
	Summary              string
	Dimensions           []ReportDimension
	PriorityActions      []ReportPriorityAction
	DetailSchema         string
	Detail               json.RawMessage
	CreatedAt            time.Time
}

type ReportDimension struct {
	Key          string          `json:"key"`
	Score        *float64        `json:"score,omitempty"`
	Scale        string          `json:"scale"`
	Coverage     float64         `json:"coverage"`
	Confidence   float64         `json:"confidence"`
	ReasonCodes  []string        `json:"reason_codes"`
	EvidenceRefs []string        `json:"evidence_ref_ids"`
	Strengths    []ReportFinding `json:"strengths"`
	Improvements []ReportFinding `json:"improvements"`
	Examples     []ReportFinding `json:"recommended_examples"`
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

func (report Report) Valid() bool {
	if !validUUID(report.ID) || !validUUID(report.EvaluationID) ||
		!validUUID(report.EvaluationRevisionID) ||
		!validUUID(report.OwnerUserID) ||
		!validReviewText(report.PracticeSessionID, 128) ||
		report.Revision < 1 || !reviewVersionPattern.MatchString(report.SchemaVersion) ||
		!reviewVersionPattern.MatchString(report.SceneType) ||
		!validReviewText(report.PracticeExperience, 128) ||
		!validReviewText(report.SceneCategory, 128) ||
		!validReviewText(report.PracticeMode, 128) ||
		(report.ScoreabilityStatus != "PROVISIONAL" &&
			report.ScoreabilityStatus != "INSUFFICIENT") ||
		!validReviewText(report.Summary, 2048) ||
		report.Dimensions == nil || len(report.Dimensions) == 0 ||
		len(report.Dimensions) > 16 || report.PriorityActions == nil ||
		len(report.PriorityActions) > 5 ||
		!reviewVersionPattern.MatchString(report.DetailSchema) ||
		len(report.Detail) == 0 || len(report.Detail) > 256*1024 ||
		report.CreatedAt.IsZero() {
		return false
	}
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(report.Detail, &detail); err != nil || detail == nil {
		return false
	}
	dimensionKeys := make(map[string]struct{}, len(report.Dimensions))
	findingIDs := make(map[string]struct{})
	improvementDimensions := make(map[string]string)
	for _, dimension := range report.Dimensions {
		if !reviewVersionPattern.MatchString(dimension.Key) ||
			(dimension.Scale != "PERCENTAGE_100" &&
				dimension.Scale != "IELTS_BAND") ||
			math.IsNaN(dimension.Coverage) ||
			math.IsInf(dimension.Coverage, 0) ||
			dimension.Coverage < 0 || dimension.Coverage > 1 ||
			math.IsNaN(dimension.Confidence) ||
			math.IsInf(dimension.Confidence, 0) ||
			dimension.Confidence < 0 || dimension.Confidence > 1 ||
			dimension.ReasonCodes == nil || dimension.EvidenceRefs == nil ||
			dimension.Strengths == nil || dimension.Improvements == nil ||
			dimension.Examples == nil ||
			len(dimension.Strengths) > 5 ||
			len(dimension.Improvements) > 5 ||
			len(dimension.Examples) > 5 {
			return false
		}
		if _, duplicate := dimensionKeys[dimension.Key]; duplicate {
			return false
		}
		dimensionKeys[dimension.Key] = struct{}{}
		if report.ScoreabilityStatus == "INSUFFICIENT" &&
			dimension.Score != nil {
			return false
		}
		if dimension.Score != nil {
			if math.IsNaN(*dimension.Score) || math.IsInf(*dimension.Score, 0) {
				return false
			}
			switch dimension.Scale {
			case "PERCENTAGE_100":
				if *dimension.Score < 0 || *dimension.Score > 100 {
					return false
				}
			case "IELTS_BAND":
				if *dimension.Score < 0 || *dimension.Score > 9 {
					return false
				}
			}
		}
		collections := [][]ReportFinding{
			dimension.Strengths,
			dimension.Improvements,
			dimension.Examples,
		}
		for _, collection := range collections {
			for _, finding := range collection {
				if !validReportFinding(finding) {
					return false
				}
				if _, duplicate := findingIDs[finding.ID]; duplicate {
					return false
				}
				findingIDs[finding.ID] = struct{}{}
			}
		}
		for _, finding := range dimension.Improvements {
			improvementDimensions[finding.ID] = dimension.Key
		}
	}
	actions := make(map[string]struct{}, len(report.PriorityActions))
	for _, action := range report.PriorityActions {
		if improvementDimensions[action.FindingID] != action.DimensionKey {
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

type DeleteUserReviewsCommand struct {
	UserID             string
	DeletionGeneration int64
}

func validUUID(value string) bool {
	return reviewUUIDPattern.MatchString(value)
}

func validReportFinding(finding ReportFinding) bool {
	if !reviewVersionPattern.MatchString(finding.ID) ||
		!validReviewText(finding.Message, 2048) ||
		len(finding.Suggestion) > 2048 || finding.Evidence == nil ||
		len(finding.Evidence) > 8 {
		return false
	}
	for _, evidence := range finding.Evidence {
		if !validReviewText(evidence.EvidenceRefID, 128) ||
			!validReviewText(evidence.TurnID, 128) ||
			evidence.StartUTF8Byte < 0 ||
			evidence.EndUTF8Byte <= evidence.StartUTF8Byte ||
			!validReviewText(evidence.OriginalExcerpt, 2048) {
			return false
		}
	}
	return true
}

func validReviewText(value string, maximumBytes int) bool {
	return strings.TrimSpace(value) == value && value != "" &&
		len(value) <= maximumBytes && !strings.ContainsRune(value, '\x00')
}
