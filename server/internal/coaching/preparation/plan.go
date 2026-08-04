package preparation

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrPlanInvalid             = errors.New("preparation: invalid practice plan request")
	ErrPlanNotFound            = errors.New("preparation: practice plan not found")
	ErrPlanConflict            = errors.New("preparation: practice plan conflict")
	ErrPlanIdempotencyConflict = errors.New("preparation: practice plan idempotency conflict")
	ErrPlanRepository          = errors.New("preparation: practice plan repository failure")
)

type PlanStatus string

const (
	PlanStatusReady    PlanStatus = "ready"
	PlanStatusArchived PlanStatus = "archived"
)

type EarlyCompletionRule string

const EarlyCompletionCoverageSatisfiedAfterCheckpoint EarlyCompletionRule = "COVERAGE_SATISFIED_AFTER_CHECKPOINT"

// GoalSnapshot freezes only the Goal identity needed to explain why a Plan
// was created. Goal remains optional and owns its live lifecycle separately.
type GoalSnapshot struct {
	ID      string `json:"goal_id"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}

// SessionPolicy is the complete, frozen execution policy for one Plan
// revision. Objectives are held once on PracticePlan rather than duplicated
// inside this value.
type SessionPolicy struct {
	SuggestedDurationSeconds int                 `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int                 `json:"min_effective_turns"`
	MaxEffectiveTurns        int                 `json:"max_effective_turns"`
	CoverageCheckpointTurn   int                 `json:"coverage_checkpoint_turn"`
	MaxFollowUpsPerQuestion  int                 `json:"max_follow_ups_per_question"`
	EarlyCompletionRule      EarlyCompletionRule `json:"early_completion_rule"`
}

type PracticeObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

// IELTSQuestionSelection is the explicit question-bank choice required when
// creating an IELTS Plan. Preparation resolves it once and persists only the
// resulting immutable assignment.
type IELTSQuestionSelection struct {
	Mode         scene.IELTSPracticeMode `json:"mode"`
	Part1SetID   string                  `json:"part_1_set_id,omitempty"`
	TopicGroupID string                  `json:"topic_group_id,omitempty"`
}

// IELTSAssignmentSnapshot is the complete IELTS question assignment frozen
// into a Plan revision. Practice copies this value without consulting the
// live Scene catalog.
type IELTSAssignmentSnapshot struct {
	BankID         string                  `json:"bank_id"`
	Season         string                  `json:"season"`
	Mode           scene.IELTSPracticeMode `json:"mode"`
	Part1SetID     string                  `json:"part_1_set_id,omitempty"`
	TopicGroupID   string                  `json:"topic_group_id,omitempty"`
	TopicTitle     string                  `json:"topic_title,omitempty"`
	Part2CueCard   string                  `json:"part_2_cue_card,omitempty"`
	Part1Questions int                     `json:"part_1_questions"`
	Part2Questions int                     `json:"part_2_questions"`
	Part3Questions int                     `json:"part_3_questions"`
	TurnBlueprints []string                `json:"turn_blueprints"`
}

// PracticePlan is Preparation's single executable-plan authority. Every
// revision contains complete immutable Goal, material, Scene, policy, and
// objective values; consumers never reinterpret live source records.
type PracticePlan struct {
	ID                  string                   `json:"practice_plan_id"`
	UserID              string                   `json:"user_id"`
	SourceThreadID      string                   `json:"source_thread_id,omitempty"`
	GoalSnapshot        *GoalSnapshot            `json:"goal_snapshot,omitempty"`
	PreparationSnapshot Snapshot                 `json:"preparation_snapshot"`
	SceneSelection      scene.SelectionSnapshot  `json:"scene_selection"`
	SessionPolicy       SessionPolicy            `json:"session_policy"`
	PracticeObjectives  []PracticeObjective      `json:"practice_objectives"`
	IELTSAssignment     *IELTSAssignmentSnapshot `json:"ielts_assignment,omitempty"`
	Revision            int                      `json:"plan_revision"`
	Status              PlanStatus               `json:"practice_plan_status"`
	CreatedAt           time.Time                `json:"created_at"`
	UpdatedAt           time.Time                `json:"updated_at"`
}

type CreatePlanRequest struct {
	SourceThreadID        string                  `json:"source_thread_id,omitempty"`
	GoalID                string                  `json:"goal_id,omitempty"`
	PreparationSnapshotID string                  `json:"preparation_snapshot_id"`
	SceneID               string                  `json:"scene_id"`
	SceneVersion          int                     `json:"scene_version"`
	SelectedRoleIDs       []string                `json:"selected_role_ids"`
	PracticeOptionID      string                  `json:"practice_option_id"`
	MaxEffectiveTurns     int                     `json:"max_effective_turns,omitempty"`
	IELTSSelection        *IELTSQuestionSelection `json:"ielts_selection,omitempty"`
}

type RevisePlanRequest struct {
	ExpectedPlanRevision int      `json:"expected_plan_revision"`
	SelectedRoleIDs      []string `json:"selected_role_ids"`
	PracticeOptionID     string   `json:"practice_option_id"`
	MaxEffectiveTurns    int      `json:"max_effective_turns"`
}

// SourceThread is the only Thread value Preparation freezes. Thread content
// and lifecycle stay owned by Agent.
type SourceThread struct {
	ID string
}

// SourceThreadReader is implemented by the caller that owns Agent Threads.
// It validates ownership independently from optional Goal validation.
type SourceThreadReader interface {
	ReadOwnedThread(
		context.Context,
		requestcontext.Actor,
		string,
	) (SourceThread, error)
}

type CreatePlanCommand struct {
	PlanID              string
	SourceThreadID      string
	GoalSnapshot        *GoalSnapshot
	PreparationSnapshot Snapshot
	SceneSelection      scene.SelectionSnapshot
	SessionPolicy       SessionPolicy
	PracticeObjectives  []PracticeObjective
	IELTSAssignment     *IELTSAssignmentSnapshot
	Intent              IdempotencyIntent
}

type RevisePlanCommand struct {
	PlanID               string
	ExpectedPlanRevision int
	SceneSelection       scene.SelectionSnapshot
	SessionPolicy        SessionPolicy
	PracticeObjectives   []PracticeObjective
	IELTSAssignment      *IELTSAssignmentSnapshot
	Intent               IdempotencyIntent
}

// PlanRepository persists a complete ready revision atomically with its
// idempotency result. Revision updates use compare-and-swap and append a new
// immutable revision; they never overwrite a prior revision.
type PlanRepository interface {
	ReplayPlan(
		context.Context,
		requestcontext.Actor,
		IdempotencyIntent,
	) (plan PracticePlan, found bool, err error)
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		CreatePlanCommand,
	) (plan PracticePlan, replayed bool, err error)
	ReadCurrentPlan(
		context.Context,
		requestcontext.Actor,
		string,
	) (PracticePlan, error)
	RevisePlan(
		context.Context,
		requestcontext.Actor,
		RevisePlanCommand,
	) (plan PracticePlan, replayed bool, err error)
	// ReadExecutablePlan succeeds only when exactRevision is the Plan's
	// current revision and its status is ready.
	ReadExecutablePlan(
		context.Context,
		requestcontext.Actor,
		string,
		int,
	) (PracticePlan, error)
}

// PlanReader is the only Preparation capability Practice needs.
type PlanReader interface {
	ReadExecutablePlan(
		context.Context,
		requestcontext.Actor,
		string,
		int,
	) (PracticePlan, error)
}

// PolicyResolver resolves one explicitly registered Scene policy reference.
// It never infers policy from Scene family or model.
type PolicyResolver interface {
	ResolveSessionPolicy(
		scene.SceneDefinition,
		scene.PracticeOption,
	) (SessionPolicy, error)
}
