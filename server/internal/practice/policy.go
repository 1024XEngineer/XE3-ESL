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

const EarlyCompletionObjectivesCovered EarlyCompletionRule = "objectives_covered"

type NextAction string

const (
	NextActionFollowUpCurrent     NextAction = "FOLLOW_UP_CURRENT"
	NextActionMoveToNextObjective NextAction = "MOVE_TO_NEXT_OBJECTIVE"
	NextActionCompleteSession     NextAction = "COMPLETE_SESSION"
)

type PracticeSessionPolicy struct {
	Mode                     PracticeMode
	SuggestedDurationSeconds int
	MinEffectiveTurns        int
	MaxEffectiveTurns        int
	CoverageCheckpointTurn   int
	MaxFollowUpsPerQuestion  int
	TargetObjectiveIDs       []string
	EarlyCompletionRule      EarlyCompletionRule
}

type ObjectiveCoverage struct {
	ObjectiveID string
	Level       CoverageLevel
}

type TurnOutcome struct {
	TurnID                        string
	SessionID                     string
	AnswerValid                   bool
	ObjectiveCoverage             []ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}
