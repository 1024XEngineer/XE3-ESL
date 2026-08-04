package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const EvaluationContextSchemaVersion = "evaluation-context.v1"

var ErrInvalidEvaluationContext = errors.New("review: invalid evaluation context")

type EvaluationContext struct {
	SchemaVersion        string                `json:"schema_version"`
	ContextType          EvaluationContextType `json:"context_type"`
	SceneKey             string                `json:"scene_key"`
	SceneID              string                `json:"scene_id"`
	SceneVersion         int                   `json:"scene_version"`
	PracticeOptionType   string                `json:"practice_option_type"`
	DifficultyRef        string                `json:"difficulty_ref"`
	AssistanceRef        string                `json:"assistance_ref"`
	TurnPolicyRef        string                `json:"turn_policy_ref"`
	SessionPolicyRef     string                `json:"session_policy_ref"`
	SceneSpecificContext SceneSpecificContext  `json:"scene_specific_context"`
}

type SceneSpecificContext struct {
	Type      EvaluationContextType          `json:"type"`
	Interview *InterviewProjectDeepDiveV1    `json:"interview_project_deep_dive,omitempty"`
	IELTS     *IELTSSpeakingPart2V1          `json:"ielts_speaking_part2,omitempty"`
	Workplace *WorkplaceProgressRiskUpdateV1 `json:"workplace_progress_risk_update,omitempty"`
	Daily     *DailyHotelCheckinIssueV1      `json:"daily_hotel_checkin_issue,omitempty"`
	Generic   *GenericPracticeV1             `json:"generic_practice,omitempty"`
}

type InterviewProjectDeepDiveV1 struct {
	Version       string   `json:"version"`
	ProjectBrief  string   `json:"project_brief"`
	CandidateRole string   `json:"candidate_role"`
	FocusPoints   []string `json:"focus_points"`
}

type IELTSSpeakingPart2V1 struct {
	Version          string   `json:"version"`
	CueCardTopic     string   `json:"cue_card_topic"`
	CueCardPoints    []string `json:"cue_card_points"`
	StrictSimulation bool     `json:"strict_simulation"`
}

type WorkplaceProgressRiskUpdateV1 struct {
	Version          string   `json:"version"`
	InitiativeBrief  string   `json:"initiative_brief"`
	Audience         string   `json:"audience"`
	ExpectedSections []string `json:"expected_sections"`
}

type DailyHotelCheckinIssueV1 struct {
	Version          string `json:"version"`
	ReservationBrief string `json:"reservation_brief"`
	Issue            string `json:"issue"`
	DesiredOutcome   string `json:"desired_outcome"`
}

type GenericPracticeV1 struct {
	Version      string `json:"version"`
	PracticeGoal string `json:"practice_goal"`
}

func (c EvaluationContext) CanonicalJSON(
	registry *PolicyRegistry,
) ([]byte, error) {
	if err := c.Validate(registry); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return nil, errors.Join(ErrInvalidEvaluationContext, err)
	}
	return encoded, nil
}

func (c EvaluationContext) Fingerprint(
	registry *PolicyRegistry,
) (string, error) {
	encoded, err := c.CanonicalJSON(registry)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (c EvaluationContext) Validate(registry *PolicyRegistry) error {
	if c.SchemaVersion != EvaluationContextSchemaVersion ||
		!validEvaluationContextType(c.ContextType) ||
		!validContextIdentifier(c.SceneKey, 128) ||
		!validContextIdentifier(c.SceneID, 128) ||
		c.SceneVersion < 1 ||
		!validContextIdentifier(c.PracticeOptionType, 64) ||
		!validContextIdentifier(c.DifficultyRef, 128) ||
		!validContextIdentifier(c.AssistanceRef, 128) ||
		!validContextIdentifier(c.TurnPolicyRef, 128) ||
		!validContextIdentifier(c.SessionPolicyRef, 128) ||
		c.SceneSpecificContext.Type != c.ContextType ||
		c.SceneSpecificContext.validate() != nil {
		return ErrInvalidEvaluationContext
	}
	if _, err := registry.Resolve(
		c.TurnPolicyRef,
		PolicyScopeTurn,
		c.ContextType,
	); err != nil {
		return ErrInvalidEvaluationContext
	}
	if _, err := registry.Resolve(
		c.SessionPolicyRef,
		PolicyScopeSession,
		c.ContextType,
	); err != nil {
		return ErrInvalidEvaluationContext
	}
	return nil
}

func (c SceneSpecificContext) validate() error {
	variants := 0
	for _, present := range []bool{
		c.Interview != nil,
		c.IELTS != nil,
		c.Workplace != nil,
		c.Daily != nil,
		c.Generic != nil,
	} {
		if present {
			variants++
		}
	}
	if variants != 1 {
		return ErrInvalidEvaluationContext
	}
	switch c.Type {
	case ContextInterviewProjectDeepDive:
		if c.Interview == nil ||
			c.Interview.Version != "interview.project_deep_dive.v1" ||
			!validContextText(c.Interview.ProjectBrief, 4096) ||
			!validContextText(c.Interview.CandidateRole, 256) ||
			!validContextStrings(c.Interview.FocusPoints, 1, 12, 256) {
			return ErrInvalidEvaluationContext
		}
	case ContextIELTSSpeakingPart2:
		if c.IELTS == nil ||
			c.IELTS.Version != "ielts.speaking_part2.v1" ||
			!validContextText(c.IELTS.CueCardTopic, 2048) ||
			!validContextStrings(c.IELTS.CueCardPoints, 1, 12, 512) {
			return ErrInvalidEvaluationContext
		}
	case ContextWorkplaceProgressRisk:
		if c.Workplace == nil ||
			c.Workplace.Version !=
				"workplace.progress_risk_update.v1" ||
			!validContextText(c.Workplace.InitiativeBrief, 4096) ||
			!validContextText(c.Workplace.Audience, 256) ||
			!validContextStrings(
				c.Workplace.ExpectedSections,
				1,
				12,
				256,
			) {
			return ErrInvalidEvaluationContext
		}
	case ContextDailyHotelCheckin:
		if c.Daily == nil ||
			c.Daily.Version != "daily.hotel_checkin_issue.v1" ||
			!validContextText(c.Daily.ReservationBrief, 2048) ||
			!validContextText(c.Daily.Issue, 2048) ||
			!validContextText(c.Daily.DesiredOutcome, 2048) {
			return ErrInvalidEvaluationContext
		}
	case ContextGenericPractice:
		if c.Generic == nil ||
			c.Generic.Version != "generic.practice.v1" ||
			!validContextText(c.Generic.PracticeGoal, 2048) {
			return ErrInvalidEvaluationContext
		}
	default:
		return ErrInvalidEvaluationContext
	}
	return nil
}

func validContextIdentifier(value string, maximumBytes int) bool {
	return validContextText(value, maximumBytes) &&
		!strings.ContainsAny(value, "\r\n\t")
}

func validContextText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		len(value) <= maximumBytes &&
		strings.TrimSpace(value) != ""
}

func validContextStrings(
	values []string,
	minimum int,
	maximum int,
	maximumItemBytes int,
) bool {
	if len(values) < minimum || len(values) > maximum {
		return false
	}
	for _, value := range values {
		if !validContextText(value, maximumItemBytes) {
			return false
		}
	}
	return true
}
