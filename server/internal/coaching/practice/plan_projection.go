package practice

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// PlanProjection is the immutable input Practice needs from one confirmed
// Preparation Plan revision. The Preparation adapter maps its editable domain
// models into these Practice-owned values before Session creation.
type PlanProjection struct {
	ID                 string
	OwnerUserID        string
	Version            int
	Preparation        PreparationSnapshot
	SceneSelection     SceneSelection
	SessionPolicy      SessionPolicy
	PracticeObjectives []PracticeObjective
	IELTSAssignment    *IELTSAssignment
}

// PlanProjectionReader is Practice's only Preparation boundary.
type PlanProjectionReader interface {
	ReadExecutablePlan(
		context.Context,
		requestcontext.Actor,
		string,
		int,
	) (PlanProjection, error)
}

type PracticeExperience string

const (
	PracticeExperienceInterview     PracticeExperience = "INTERVIEW"
	PracticeExperienceIELTSSpeaking PracticeExperience = "IELTS_SPEAKING"
	PracticeExperienceWorkplace     PracticeExperience = "WORKPLACE"
	PracticeExperienceLifeAndTravel PracticeExperience = "LIFE_AND_TRAVEL"
)

type SceneCategory string

type SceneStatus string

const SceneStatusActive SceneStatus = "active"

type PracticeMode string

const (
	// MaxPracticeTurns is a generic runtime safety boundary, not an IELTS
	// question-count rule.
	MaxPracticeTurns                        = 64
	PracticeModeFullSimulation PracticeMode = "FULL_SIMULATION"
	PracticeModeFocus          PracticeMode = "FOCUS"
	PracticeModeFullMock       PracticeMode = "FULL_MOCK"
	PracticeModePart1          PracticeMode = "PART_1"
	PracticeModePart2          PracticeMode = "PART_2"
	PracticeModePart3          PracticeMode = "PART_3"
)

type ScenePrompt struct {
	PublicSceneBrief string   `json:"public_scene_brief"`
	PracticeGoal     string   `json:"practice_goal"`
	UserRole         string   `json:"user_role"`
	AIRole           string   `json:"ai_role"`
	PersonaSummary   string   `json:"persona_summary"`
	FocusAreas       []string `json:"focus_areas"`
	TurnBlueprints   []string `json:"turn_blueprints"`
}

type PracticeObjectiveDefinition struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type RoleDefinition struct {
	ID                 string                        `json:"role_definition_id"`
	SceneID            string                        `json:"scene_id"`
	Type               string                        `json:"role_type"`
	DisplayName        string                        `json:"display_name"`
	Responsibilities   string                        `json:"responsibilities"`
	Style              string                        `json:"style"`
	PracticeObjectives []PracticeObjectiveDefinition `json:"practice_objectives"`
	VoiceConfigRef     string                        `json:"voice_config_ref,omitempty"`
}

type PracticeOption struct {
	ID                       string       `json:"practice_option_id"`
	SceneID                  string       `json:"scene_id"`
	RoleDefinitionID         string       `json:"role_definition_id,omitempty"`
	Mode                     PracticeMode `json:"practice_mode"`
	DisplayName              string       `json:"display_name"`
	SuggestedDurationSeconds int          `json:"suggested_duration_seconds"`
	TurnPolicyRef            string       `json:"turn_policy_ref"`
	SessionPolicyRef         string       `json:"session_policy_ref"`
	EvaluationPolicyRef      string       `json:"evaluation_policy_ref"`
}

// SceneDefinition is the exact immutable execution projection frozen from one
// selected Scene version. Only selected roles and the selected option are
// carried into Practice.
type SceneDefinition struct {
	ID              string             `json:"scene_id"`
	Experience      PracticeExperience `json:"practice_experience"`
	Category        SceneCategory      `json:"scene_category"`
	Name            string             `json:"name"`
	Version         int                `json:"scene_version"`
	Status          SceneStatus        `json:"status"`
	Prompt          ScenePrompt        `json:"prompt"`
	Roles           []RoleDefinition   `json:"roles"`
	PracticeOptions []PracticeOption   `json:"practice_options"`
}

type SceneSelection struct {
	Scene            SceneDefinition `json:"scene"`
	SelectedRoleIDs  []string        `json:"selected_role_ids"`
	PracticeOptionID string          `json:"practice_option_id"`
}

func (selection SceneSelection) SelectedRoles() ([]RoleDefinition, error) {
	roles := make([]RoleDefinition, 0, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		found := false
		for _, role := range selection.Scene.Roles {
			if role.ID == roleID {
				roles = append(roles, cloneRoleDefinition(role))
				found = true
				break
			}
		}
		if !found {
			return nil, ErrConflict
		}
	}
	return roles, nil
}

func (selection SceneSelection) PracticeOption() (PracticeOption, error) {
	for _, option := range selection.Scene.PracticeOptions {
		if option.ID == selection.PracticeOptionID {
			return option, nil
		}
	}
	return PracticeOption{}, ErrConflict
}

type JobTargetInput struct {
	Source              string `json:"source"`
	JobTitle            string `json:"job_title,omitempty"`
	JobDescription      string `json:"job_description,omitempty"`
	Company             string `json:"company,omitempty"`
	Seniority           string `json:"seniority,omitempty"`
	CandidateBackground string `json:"candidate_background,omitempty"`
	PracticeFocus       string `json:"practice_focus,omitempty"`
}

type ResumeWorkExperience struct {
	Company      string   `json:"company"`
	Position     string   `json:"position"`
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

type ResumeProjectExperience struct {
	ProjectName  string   `json:"project_name"`
	Role         string   `json:"role"`
	Description  string   `json:"description"`
	Technologies []string `json:"technologies"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

type ResumeEducationExperience struct {
	School    string `json:"school"`
	Major     string `json:"major"`
	Degree    string `json:"degree"`
	GPA       string `json:"gpa,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

type ResumeMaterial struct {
	TargetPosition       string                      `json:"target_position"`
	ProfessionalSummary  string                      `json:"professional_summary"`
	WorkExperiences      []ResumeWorkExperience      `json:"work_experiences"`
	ProjectExperiences   []ResumeProjectExperience   `json:"project_experiences"`
	EducationExperiences []ResumeEducationExperience `json:"education_experiences"`
	Skills               []string                    `json:"skills"`
	Awards               []string                    `json:"awards"`
}

type JobTargetCatalogRecommendation struct {
	SceneID          string   `json:"scene_id"`
	SceneVersion     int      `json:"scene_version"`
	SelectedRoleIDs  []string `json:"selected_role_ids"`
	PracticeOptionID string   `json:"practice_option_id"`
}

type JobTargetCandidate struct {
	Source                string                         `json:"source"`
	GeneralAdviceOnly     bool                           `json:"general_advice_only"`
	JobTitle              string                         `json:"job_title"`
	Seniority             string                         `json:"seniority"`
	Responsibilities      []string                       `json:"responsibilities"`
	CoreSkills            []string                       `json:"core_skills"`
	CommunicationFocus    []string                       `json:"communication_focus"`
	PracticeGoals         []string                       `json:"practice_goals"`
	ScopeNotice           string                         `json:"scope_notice"`
	CatalogRecommendation JobTargetCatalogRecommendation `json:"catalog_recommendation"`
}

type PreparationSnapshot struct {
	InterviewPreparationID      string              `json:"interview_preparation_id,omitempty"`
	InterviewPreparationVersion int                 `json:"interview_preparation_version,omitempty"`
	JobTargetInputSnapshot      *JobTargetInput     `json:"job_target_input_snapshot,omitempty"`
	JobTargetCandidateSnapshot  *JobTargetCandidate `json:"job_target_candidate_snapshot,omitempty"`
	ResumeMaterial              *ResumeMaterial     `json:"resume_material,omitempty"`
	BackgroundSnapshot          string              `json:"background_snapshot"`
}

type EarlyCompletionRule string

const EarlyCompletionCoverageSatisfiedAfterCheckpoint EarlyCompletionRule = "COVERAGE_SATISFIED_AFTER_CHECKPOINT"

type CompletionMode string

const (
	CompletionModeTurnLimited    CompletionMode = "TURN_LIMITED"
	CompletionModeUserControlled CompletionMode = "USER_CONTROLLED"
)

// SessionPolicy is Practice's frozen progression policy. RetryAllowed is a
// concrete value projected once at Session creation; runtime code never
// infers it again from Scene family or model.
type SessionPolicy struct {
	CompletionMode             CompletionMode      `json:"completion_mode"`
	SuggestedDurationSeconds   int                 `json:"suggested_duration_seconds"`
	MinEffectiveTurns          int                 `json:"min_effective_turns"`
	MaxEffectiveTurns          int                 `json:"max_effective_turns"`
	CoverageCheckpointTurn     int                 `json:"coverage_checkpoint_turn"`
	MaxFollowUpsPerQuestion    int                 `json:"max_follow_ups_per_question"`
	EarlyCompletionRule        EarlyCompletionRule `json:"early_completion_rule"`
	RetryAllowed               bool                `json:"retry_allowed"`
	QuestionTranslationAllowed bool                `json:"question_translation_allowed"`
	QuestionTipsAllowed        bool                `json:"question_tips_allowed"`
	SpeechFeedbackAllowed      bool                `json:"speech_feedback_allowed"`
}

type PracticeObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type IELTSAssignment struct {
	BankID string       `json:"bank_id"`
	Season string       `json:"season"`
	Mode   PracticeMode `json:"mode"`
	Parts  []IELTSPart  `json:"parts"`
}

type IELTSPart struct {
	Part            PracticeMode          `json:"part"`
	SourceID        string                `json:"source_id"`
	TopicTitle      string                `json:"topic_title,omitempty"`
	CueCard         string                `json:"cue_card,omitempty"`
	TurnBlueprints  []string              `json:"turn_blueprints"`
	PreparedAnswers []IELTSPreparedAnswer `json:"prepared_answers,omitempty"`
}

type IELTSPreparedAnswer struct {
	QuestionPosition int    `json:"question_position"`
	Answer           string `json:"answer"`
	Personalized     bool   `json:"personalized"`
}

// ValidIELTSAssignment validates the frozen Part composition and its exact
// ordered projection into the Scene prompt.
func ValidIELTSAssignment(
	assignment *IELTSAssignment,
	mode PracticeMode,
	promptTurnBlueprints []string,
) bool {
	if assignment == nil || assignment.Mode != mode ||
		!validIELTSResourceID(assignment.BankID) ||
		!validIELTSText(assignment.Season) {
		return false
	}
	expected := []PracticeMode(nil)
	switch mode {
	case PracticeModeFullMock:
		expected = []PracticeMode{
			PracticeModePart1,
			PracticeModePart2,
			PracticeModePart3,
		}
	case PracticeModePart1:
		expected = []PracticeMode{PracticeModePart1}
	case PracticeModePart2:
		expected = []PracticeMode{PracticeModePart2}
	case PracticeModePart3:
		expected = []PracticeMode{PracticeModePart3}
	default:
		return false
	}
	if len(assignment.Parts) != len(expected) {
		return false
	}
	blueprints := IELTSAssignmentTurnBlueprints(assignment)
	if len(blueprints) == 0 || len(blueprints) > MaxPracticeTurns ||
		!equalPracticeStrings(blueprints, promptTurnBlueprints) {
		return false
	}
	for index, part := range assignment.Parts {
		if part.Part != expected[index] ||
			!validIELTSResourceID(part.SourceID) ||
			len(part.TurnBlueprints) == 0 {
			return false
		}
		for _, blueprint := range part.TurnBlueprints {
			if !validIELTSText(blueprint) {
				return false
			}
		}
		seenAnswers := make(map[int]struct{}, len(part.PreparedAnswers))
		for _, answer := range part.PreparedAnswers {
			if answer.QuestionPosition < 1 || answer.QuestionPosition > len(part.TurnBlueprints) ||
				!validIELTSPreparedAnswer(answer.Answer) {
				return false
			}
			if _, duplicate := seenAnswers[answer.QuestionPosition]; duplicate {
				return false
			}
			seenAnswers[answer.QuestionPosition] = struct{}{}
		}
		switch part.Part {
		case PracticeModePart1:
			if part.TopicTitle != "" || part.CueCard != "" {
				return false
			}
		case PracticeModePart2:
			if !validIELTSText(part.TopicTitle) ||
				!validIELTSText(part.CueCard) ||
				len(part.TurnBlueprints) != 1 {
				return false
			}
		case PracticeModePart3:
			if !validIELTSText(part.TopicTitle) || part.CueCard != "" {
				return false
			}
		}
	}
	parts := assignment.Parts
	if len(parts) >= 2 &&
		parts[len(parts)-2].Part == PracticeModePart2 &&
		(parts[len(parts)-2].SourceID != parts[len(parts)-1].SourceID ||
			parts[len(parts)-2].TopicTitle != parts[len(parts)-1].TopicTitle) {
		return false
	}
	return true
}

func validIELTSPreparedAnswer(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && utf8.RuneCountInString(value) <= 2000 &&
		!strings.ContainsRune(value, '\x00')
}

func IELTSAssignmentTurnBlueprints(assignment *IELTSAssignment) []string {
	if assignment == nil {
		return nil
	}
	var result []string
	for _, part := range assignment.Parts {
		result = append(result, part.TurnBlueprints...)
	}
	return result
}

func validIELTSResourceID(value string) bool {
	return value != "" && len(value) <= 128 &&
		strings.TrimSpace(value) == value
}

func validIELTSText(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func equalPracticeStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
