package practice

import (
	"errors"
	"slices"
)

// 决定场次的回合预算和覆盖检查时机
type PracticeMode string

const (
	PracticeModeFullSimulation  PracticeMode = "full_simulation"
	PracticeModeFocusedPractice PracticeMode = "focused_practice"
)

// 覆盖程度由对话分析按单个训练目标给出
type CoverageLevel string

const (
	CoverageCovered   CoverageLevel = "covered"
	CoveragePartial   CoverageLevel = "partial"
	CoverageUncovered CoverageLevel = "uncovered"
)

// 固化创建场次时采用的提前结束条件
type EarlyCompletionRule string

const (
	EarlyCompletionObjectivesCovered EarlyCompletionRule = "objectives_covered"
)

// 下一轮对话只接受 Practice 输出的这组动作
type NextAction string

const (
	NextActionFollowUpCurrent     NextAction = "FOLLOW_UP_CURRENT"
	NextActionMoveToNextObjective NextAction = "MOVE_TO_NEXT_OBJECTIVE"
	NextActionCompleteSession     NextAction = "COMPLETE_SESSION"
)

var (
	ErrPracticeSessionPolicyInvalid = errors.New("practice_session_policy_invalid")
	ErrTurnOutcomeInvalid           = errors.New("turn_outcome_invalid")
)

// 策略在场次创建时冻结，EvaluatePolicy 不读取外部状态
type PracticeSessionPolicy struct {
	mode                     PracticeMode
	suggestedDurationSeconds int
	minEffectiveTurns        int
	maxEffectiveTurns        int
	coverageCheckpointTurn   int
	maxFollowUpsPerQuestion  int
	targetObjectiveIDs       []string
	earlyCompletionRule      EarlyCompletionRule
}

// 不同练习模式使用固定回合预算，未知模式或空目标会被拒绝
func NewPracticeSessionPolicy(mode PracticeMode, targetObjectiveIDs []string) (PracticeSessionPolicy, error) {
	if !hasNonEmptyValues(targetObjectiveIDs) {
		return PracticeSessionPolicy{}, ErrPracticeSessionPolicyInvalid
	}

	policy := PracticeSessionPolicy{
		mode:                    mode,
		maxFollowUpsPerQuestion: 1,
		targetObjectiveIDs:      cloneStrings(targetObjectiveIDs),
		earlyCompletionRule:     EarlyCompletionObjectivesCovered,
	}
	switch mode {
	case PracticeModeFullSimulation:
		policy.suggestedDurationSeconds = 600
		policy.minEffectiveTurns = 4
		policy.maxEffectiveTurns = 6
		policy.coverageCheckpointTurn = 4
	case PracticeModeFocusedPractice:
		policy.suggestedDurationSeconds = 300
		policy.minEffectiveTurns = 2
		policy.maxEffectiveTurns = 3
		policy.coverageCheckpointTurn = 2
	default:
		return PracticeSessionPolicy{}, ErrPracticeSessionPolicyInvalid
	}
	return policy, nil
}

func (p PracticeSessionPolicy) Mode() PracticeMode { return p.mode }

func (p PracticeSessionPolicy) SuggestedDurationSeconds() int { return p.suggestedDurationSeconds }

func (p PracticeSessionPolicy) MinEffectiveTurns() int { return p.minEffectiveTurns }

func (p PracticeSessionPolicy) MaxEffectiveTurns() int { return p.maxEffectiveTurns }

func (p PracticeSessionPolicy) CoverageCheckpointTurn() int { return p.coverageCheckpointTurn }

func (p PracticeSessionPolicy) MaxFollowUpsPerQuestion() int { return p.maxFollowUpsPerQuestion }

// 返回副本，避免调用方修改已冻结的推进策略
func (p PracticeSessionPolicy) TargetObjectiveIDs() []string {
	return cloneStrings(p.targetObjectiveIDs)
}

func (p PracticeSessionPolicy) EarlyCompletionRule() EarlyCompletionRule {
	return p.earlyCompletionRule
}

type ObjectiveCoverage struct {
	ObjectiveID string
	Level       CoverageLevel
}

// 只接受已通过对话模块有效性判断的回合结果
type TurnOutcome struct {
	TurnID                        string
	SessionID                     string
	AnswerValid                   bool
	ObjectiveCoverage             []ObjectiveCoverage
	FollowUpGap                   bool
	FollowUpCount                 int
	CompletedPrimaryQuestionCount int
}

type ProgressInput struct {
	EffectiveTurnCount   int
	RemainingTimeSeconds int
	Outcome              TurnOutcome
}

// 纯计算，相同策略和进度始终得到相同动作
func EvaluatePolicy(policy PracticeSessionPolicy, progress ProgressInput) (NextAction, error) {
	if !policy.valid() {
		return "", ErrPracticeSessionPolicyInvalid
	}
	if !progress.valid() {
		return "", ErrTurnOutcomeInvalid
	}
	if progress.EffectiveTurnCount >= policy.maxEffectiveTurns {
		return NextActionCompleteSession, nil
	}
	if progress.EffectiveTurnCount >= policy.coverageCheckpointTurn && progress.RemainingTimeSeconds <= 0 {
		return NextActionCompleteSession, nil
	}
	if progress.Outcome.FollowUpGap && progress.Outcome.FollowUpCount < policy.maxFollowUpsPerQuestion {
		return NextActionFollowUpCurrent, nil
	}
	if progress.EffectiveTurnCount >= policy.coverageCheckpointTurn && allObjectivesCovered(policy.targetObjectiveIDs, progress.Outcome.ObjectiveCoverage) {
		return NextActionCompleteSession, nil
	}
	return NextActionMoveToNextObjective, nil
}

func (p PracticeSessionPolicy) valid() bool {
	return (p.mode == PracticeModeFullSimulation || p.mode == PracticeModeFocusedPractice) &&
		p.suggestedDurationSeconds > 0 && p.minEffectiveTurns > 0 && p.maxEffectiveTurns >= p.minEffectiveTurns &&
		p.coverageCheckpointTurn >= p.minEffectiveTurns && p.coverageCheckpointTurn <= p.maxEffectiveTurns &&
		p.maxFollowUpsPerQuestion >= 0 && hasNonEmptyValues(p.targetObjectiveIDs) &&
		p.earlyCompletionRule == EarlyCompletionObjectivesCovered
}

func (p ProgressInput) valid() bool {
	return p.EffectiveTurnCount > 0 && p.Outcome.TurnID != "" && p.Outcome.SessionID != "" &&
		p.Outcome.AnswerValid && p.Outcome.FollowUpCount >= 0 && p.Outcome.CompletedPrimaryQuestionCount >= 0
}

func allObjectivesCovered(targetIDs []string, coverage []ObjectiveCoverage) bool {
	covered := make(map[string]bool, len(coverage))
	for _, item := range coverage {
		if item.ObjectiveID != "" && item.Level == CoverageCovered {
			covered[item.ObjectiveID] = true
		}
	}
	for _, targetID := range targetIDs {
		if !covered[targetID] {
			return false
		}
	}
	return true
}

func hasNonEmptyValues(values []string) bool {
	return len(values) > 0 && !slices.Contains(values, "")
}
