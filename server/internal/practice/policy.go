package practice

type PracticeMode string

const (
	PracticeModeFullSimulation  PracticeMode = "full_simulation"
	PracticeModeFocusedPractice PracticeMode = "focused_practice"
)

type CoverageLevel string

const (
	CoverageCovered   CoverageLevel = "covered"
	CoveragePartial   CoverageLevel = "partial"
	CoverageUncovered CoverageLevel = "uncovered"
)

type EarlyCompletionRule string

const (
	EarlyCompletionObjectivesCovered                EarlyCompletionRule = "objectives_covered"
	EarlyCompletionCoverageSatisfiedAfterCheckpoint EarlyCompletionRule = "COVERAGE_SATISFIED_AFTER_CHECKPOINT"
)

type NextAction string

const (
	NextActionFollowUpCurrent     NextAction = "FOLLOW_UP_CURRENT"
	NextActionMoveToNextObjective NextAction = "MOVE_TO_NEXT_OBJECTIVE"
	NextActionCompleteSession     NextAction = "COMPLETE_SESSION"
)

type PracticeSessionPolicy struct {
	Mode                     PracticeMode        `json:"mode,omitempty"`
	SuggestedDurationSeconds int                 `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int                 `json:"min_effective_turns"`
	MaxEffectiveTurns        int                 `json:"max_effective_turns"`
	CoverageCheckpointTurn   int                 `json:"coverage_checkpoint_turn"`
	MaxFollowUpsPerQuestion  int                 `json:"max_follow_ups_per_question"`
	TargetObjectives         []PracticeObjective `json:"target_objectives"`
	EarlyCompletionRule      EarlyCompletionRule `json:"early_completion_rule"`
}

type ObjectiveCoverage struct {
	ObjectiveID string        `json:"objective_id"`
	Level       CoverageLevel `json:"coverage_level"`
}

type TurnOutcome struct {
	TurnID                        string
	SessionID                     string
	IsRetry                       bool
	AnswerValid                   bool
	ObjectiveCoverage             []ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}
