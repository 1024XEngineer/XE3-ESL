package preparation

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type SourceThread struct{ ID string }

type SourceThreadReader interface {
	ReadOwnedThread(context.Context, requestcontext.Actor, string) (SourceThread, error)
}

var (
	ErrPlanInvalid             = errors.New("preparation: invalid practice plan request")
	ErrPlanNotFound            = errors.New("preparation: practice plan not found")
	ErrPlanConflict            = errors.New("preparation: practice plan conflict")
	ErrPlanIdempotencyConflict = errors.New("preparation: practice plan idempotency conflict")
	ErrPlanRepository          = errors.New("preparation: practice plan repository failure")
)

type PlanStatus string

const (
	PlanStatusDraft    PlanStatus = "draft"
	PlanStatusReady    PlanStatus = "ready"
	PlanStatusArchived PlanStatus = "archived"
)

type EarlyCompletionRule string

const EarlyCompletionCoverageSatisfiedAfterCheckpoint EarlyCompletionRule = "COVERAGE_SATISFIED_AFTER_CHECKPOINT"

type CompletionMode string

const (
	CompletionModeTurnLimited    CompletionMode = "TURN_LIMITED"
	CompletionModeUserControlled CompletionMode = "USER_CONTROLLED"
)

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

type IELTSQuestionSelection struct {
	Part1SetID   string `json:"part_1_set_id,omitempty"`
	TopicGroupID string `json:"topic_group_id,omitempty"`
	CueCardType  string `json:"cue_card_type,omitempty"`
}

type IELTSAssignmentSnapshot struct {
	BankID string                        `json:"bank_id"`
	Season string                        `json:"season"`
	Mode   scene.PracticeMode            `json:"mode"`
	Parts  []IELTSAssignmentPartSnapshot `json:"parts"`
}

type IELTSAssignmentPartSnapshot struct {
	Part            scene.PracticeMode            `json:"part"`
	SourceID        string                        `json:"source_id"`
	TopicTitle      string                        `json:"topic_title,omitempty"`
	CueCard         string                        `json:"cue_card,omitempty"`
	TurnBlueprints  []string                      `json:"turn_blueprints"`
	PreparedAnswers []IELTSPreparedAnswerSnapshot `json:"prepared_answers,omitempty"`
}

type IELTSPreparedAnswerSnapshot struct {
	QuestionPosition int    `json:"question_position"`
	Answer           string `json:"answer"`
	Personalized     bool   `json:"personalized"`
}

type IELTSPreparedAnswerRequest struct {
	BankID           string             `json:"bank_id"`
	Part             scene.PracticeMode `json:"part"`
	SourceID         string             `json:"source_id"`
	QuestionPosition int                `json:"question_position"`
	Answer           string             `json:"answer"`
	Personalized     bool               `json:"personalized"`
}

// Snapshot is an immutable value embedded in PracticePlan and later copied
// into PracticeSession.plan_snapshot. It has no identity or table of its own.
type Snapshot struct {
	BackgroundSummary string                        `json:"background_summary,omitempty"`
	Interview         *InterviewPreparationSnapshot `json:"interview,omitempty"`
}

type PracticePlan struct {
	ID                  string                   `json:"practice_plan_id"`
	UserID              string                   `json:"user_id"`
	SourceThreadID      string                   `json:"source_thread_id,omitempty"`
	PreparationSnapshot Snapshot                 `json:"preparation_snapshot"`
	SceneSelection      scene.SelectionSnapshot  `json:"scene_selection"`
	SessionPolicy       SessionPolicy            `json:"session_policy"`
	PracticeObjectives  []PracticeObjective      `json:"practice_objectives"`
	IELTSAssignment     *IELTSAssignmentSnapshot `json:"ielts_assignment,omitempty"`
	Version             int                      `json:"version"`
	Status              PlanStatus               `json:"practice_plan_status"`
	CreatedAt           time.Time                `json:"created_at"`
	UpdatedAt           time.Time                `json:"updated_at"`
}

type PracticePlanSummary struct {
	ID                       string                   `json:"practice_plan_id"`
	Version                  int                      `json:"version"`
	Status                   PlanStatus               `json:"practice_plan_status"`
	PracticeExperience       scene.PracticeExperience `json:"practice_experience"`
	SceneName                string                   `json:"scene_name"`
	PracticeScope            string                   `json:"practice_scope"`
	JobTitle                 string                   `json:"job_title,omitempty"`
	PracticeObjectives       []string                 `json:"practice_objectives"`
	ResumeUsed               bool                     `json:"resume_used"`
	SuggestedDurationSeconds int                      `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int                      `json:"min_effective_turns"`
	MaxEffectiveTurns        int                      `json:"max_effective_turns"`
	CreatedAt                time.Time                `json:"created_at"`
	UpdatedAt                time.Time                `json:"updated_at"`
}

type CreatePlanRequest struct {
	SourceThreadID           string                       `json:"source_thread_id,omitempty"`
	BackgroundSummary        string                       `json:"background_summary,omitempty"`
	InterviewPreparationID   string                       `json:"interview_preparation_id,omitempty"`
	ExpectedInterviewVersion int                          `json:"expected_interview_version,omitempty"`
	SceneID                  string                       `json:"scene_id"`
	SceneVersion             int                          `json:"scene_version"`
	SelectedRoleIDs          []string                     `json:"selected_role_ids"`
	PracticeOptionID         string                       `json:"practice_option_id"`
	MaxEffectiveTurns        int                          `json:"max_effective_turns,omitempty"`
	IELTSSelection           *IELTSQuestionSelection      `json:"ielts_selection,omitempty"`
	IELTSPreparedAnswers     []IELTSPreparedAnswerRequest `json:"ielts_prepared_answers,omitempty"`
}

type ConfirmPlanRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

type CreatePlanCommand struct {
	PlanID              string
	SourceThreadID      string
	PreparationSnapshot Snapshot
	SceneSelection      scene.SelectionSnapshot
	SessionPolicy       SessionPolicy
	PracticeObjectives  []PracticeObjective
	IELTSAssignment     *IELTSAssignmentSnapshot
	Status              PlanStatus
	ClientRequestID     string
	RequestFingerprint  [32]byte
}

type ConfirmPlanCommand struct {
	PlanID             string
	ExpectedVersion    int
	ClientRequestID    string
	RequestFingerprint [32]byte
}

type PlanRepository interface {
	CreatePlan(context.Context, requestcontext.Actor, CreatePlanCommand) (PracticePlan, bool, error)
	ReadCurrentPlan(context.Context, requestcontext.Actor, string) (PracticePlan, error)
	ListCurrentPlans(context.Context, requestcontext.Actor, scene.PracticeExperience) ([]PracticePlan, error)
	ArchivePlan(context.Context, requestcontext.Actor, string) error
	ConfirmPlan(context.Context, requestcontext.Actor, ConfirmPlanCommand) (PracticePlan, bool, error)
	ReadExecutablePlan(context.Context, requestcontext.Actor, string, int) (PracticePlan, error)
}

type PlanReader interface {
	ReadExecutablePlan(context.Context, requestcontext.Actor, string, int) (PracticePlan, error)
}

type PolicyResolver interface {
	ResolveSessionPolicy(scene.SceneDefinition, scene.PracticeOption, int) (SessionPolicy, error)
}
