package practice

type CoverageLevel string

const (
	CoverageCovered   CoverageLevel = "covered"
	CoveragePartial   CoverageLevel = "partial"
	CoverageUncovered CoverageLevel = "uncovered"
)

type NextAction string

const (
	NextActionFollowUpCurrent     NextAction = "FOLLOW_UP_CURRENT"
	NextActionMoveToNextObjective NextAction = "MOVE_TO_NEXT_OBJECTIVE"
	NextActionCompleteSession     NextAction = "COMPLETE_SESSION"
)

const EndReasonCoverageSatisfiedAtCheckpoint = "COVERAGE_SATISFIED_AT_CHECKPOINT"

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
