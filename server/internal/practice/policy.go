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

// PracticeSessionPolicy 随场次快照冻结，后续推进不能改读计划的最新配置
// 时长使用秒，Turn 上限只统计被 Practice 接受的有效结果
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

// TurnOutcome 是 Conversation 完成一轮处理后交给 Practice 的推进依据
// TurnID 用于识别重放，同一个 Turn 必须得到相同的 NextAction
type TurnOutcome struct {
	TurnID                        string
	SessionID                     string
	AnswerValid                   bool
	ObjectiveCoverage             []ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}
