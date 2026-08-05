package practice

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// PlanProjection is the immutable input Practice needs from one confirmed
// Preparation Plan revision. The Preparation adapter maps its editable domain
// models into these Practice-owned values before Session creation.
type PlanProjection struct {
	ID                 string
	OwnerUserID        string
	Revision           int
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

type SceneFamily string

const (
	SceneFamilyInterview SceneFamily = "INTERVIEW"
	SceneFamilyExam      SceneFamily = "EXAM"
	SceneFamilyWorkplace SceneFamily = "WORKPLACE"
	SceneFamilyDaily     SceneFamily = "DAILY"
)

type SceneModel string

const (
	SceneModelProjectExperienceDeepDive    SceneModel = "PROJECT_EXPERIENCE_DEEP_DIVE"
	SceneModelInterviewBasicDialogue       SceneModel = "INTERVIEW_BASIC_DIALOGUE"
	SceneModelIELTSSpeakingPart1           SceneModel = "IELTS_SPEAKING_PART_1"
	SceneModelIELTSSpeakingPart2           SceneModel = "IELTS_SPEAKING_PART_2"
	SceneModelIELTSSpeakingPart3           SceneModel = "IELTS_SPEAKING_PART_3"
	SceneModelIELTSSpeakingFullMock        SceneModel = "IELTS_SPEAKING_FULL_MOCK"
	SceneModelExamBasicDialogue            SceneModel = "EXAM_BASIC_DIALOGUE"
	SceneModelProgressAndRiskUpdate        SceneModel = "PROGRESS_AND_RISK_UPDATE"
	SceneModelWorkplaceBasicDialogue       SceneModel = "WORKPLACE_BASIC_DIALOGUE"
	SceneModelHotelCheckinAndIssueHandling SceneModel = "HOTEL_CHECKIN_AND_ISSUE_HANDLING"
	SceneModelDailyBasicDialogue           SceneModel = "DAILY_BASIC_DIALOGUE"
)

type SceneStatus string

const SceneStatusActive SceneStatus = "active"

type PracticeOptionType string

const (
	PracticeOptionFullSimulation PracticeOptionType = "FULL_SIMULATION"
	PracticeOptionFocus          PracticeOptionType = "FOCUS"
)

type ScenePrompt struct {
	PublicSceneBrief         string   `json:"public_scene_brief"`
	PracticeGoal             string   `json:"practice_goal"`
	UserRole                 string   `json:"user_role"`
	AIRole                   string   `json:"ai_role"`
	PersonaSummary           string   `json:"persona_summary"`
	FocusAreas               []string `json:"focus_areas"`
	TurnBlueprints           []string `json:"turn_blueprints"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
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
	ID               string             `json:"practice_option_id"`
	SceneID          string             `json:"scene_id"`
	RoleDefinitionID string             `json:"role_definition_id,omitempty"`
	Type             PracticeOptionType `json:"practice_option_type"`
	DisplayName      string             `json:"display_name"`
}

// SceneDefinition is the exact immutable execution projection frozen from one
// selected Scene version. Only selected roles and the selected option are
// carried into Practice.
type SceneDefinition struct {
	ID                  string           `json:"scene_id"`
	Family              SceneFamily      `json:"scene_family"`
	Model               SceneModel       `json:"scene_model"`
	Name                string           `json:"name"`
	Version             int              `json:"scene_version"`
	Status              SceneStatus      `json:"status"`
	TurnPolicyRef       string           `json:"turn_policy_ref"`
	SessionPolicyRef    string           `json:"session_policy_ref"`
	EvaluationPolicyRef string           `json:"evaluation_policy_ref"`
	Prompt              ScenePrompt      `json:"prompt"`
	Roles               []RoleDefinition `json:"roles"`
	PracticeOptions     []PracticeOption `json:"practice_options"`
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

type ResumeRevisionSnapshot struct {
	ResumeID string         `json:"resume_id"`
	Revision int64          `json:"revision"`
	Material ResumeMaterial `json:"material"`
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
	ID                                 string                  `json:"preparation_snapshot_id"`
	SourceProfileID                    string                  `json:"source_profile_id"`
	SourceVersion                      int                     `json:"source_version"`
	SourceJobTargetID                  string                  `json:"source_job_target_id,omitempty"`
	SourceJobTargetConfirmationVersion int                     `json:"source_job_target_confirmation_version,omitempty"`
	JobTargetInputSnapshot             *JobTargetInput         `json:"job_target_input_snapshot,omitempty"`
	JobTargetCandidateSnapshot         *JobTargetCandidate     `json:"job_target_candidate_snapshot,omitempty"`
	ResumeSnapshot                     *ResumeRevisionSnapshot `json:"resume_snapshot,omitempty"`
	JobDescriptionSnapshot             string                  `json:"job_description_snapshot,omitempty"`
	BackgroundSnapshot                 string                  `json:"background_snapshot"`
	CreatedAt                          time.Time               `json:"created_at"`
}

type EarlyCompletionRule string

const EarlyCompletionCoverageSatisfiedAfterCheckpoint EarlyCompletionRule = "COVERAGE_SATISFIED_AFTER_CHECKPOINT"

// SessionPolicy is Practice's frozen progression policy. RetryAllowed is a
// concrete value projected once at Session creation; runtime code never
// infers it again from Scene family or model.
type SessionPolicy struct {
	SuggestedDurationSeconds   int                 `json:"suggested_duration_seconds"`
	MinEffectiveTurns          int                 `json:"min_effective_turns"`
	MaxEffectiveTurns          int                 `json:"max_effective_turns"`
	CoverageCheckpointTurn     int                 `json:"coverage_checkpoint_turn"`
	MaxFollowUpsPerQuestion    int                 `json:"max_follow_ups_per_question"`
	EarlyCompletionRule        EarlyCompletionRule `json:"early_completion_rule"`
	RetryAllowed               bool                `json:"retry_allowed"`
	QuestionTranslationAllowed bool                `json:"question_translation_allowed"`
}

type PracticeObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type IELTSPracticeMode string

const (
	IELTSPracticeModeFullMock IELTSPracticeMode = "FULL_MOCK"
	IELTSPracticeModePart1    IELTSPracticeMode = "PART_1"
	IELTSPracticeModePart2    IELTSPracticeMode = "PART_2"
	IELTSPracticeModePart3    IELTSPracticeMode = "PART_3"
)

type IELTSAssignment struct {
	BankID         string            `json:"bank_id"`
	Season         string            `json:"season"`
	Mode           IELTSPracticeMode `json:"mode"`
	Part1SetID     string            `json:"part_1_set_id,omitempty"`
	TopicGroupID   string            `json:"topic_group_id,omitempty"`
	TopicTitle     string            `json:"topic_title,omitempty"`
	Part2CueCard   string            `json:"part_2_cue_card,omitempty"`
	Part1Questions int               `json:"part_1_questions"`
	Part2Questions int               `json:"part_2_questions"`
	Part3Questions int               `json:"part_3_questions"`
	TurnBlueprints []string          `json:"turn_blueprints"`
}
