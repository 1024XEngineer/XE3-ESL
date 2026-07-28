package runtime

import (
	"log/slog"
	"strings"
)

const intentGuardVersionV1 = "intent-guard-v1"

type IntentMode string

const (
	IntentDirectOnly   IntentMode = "direct_only"
	IntentToolEligible IntentMode = "tool_eligible"
)

const (
	ReasonDirectLanguageHelp       = "direct_language_help"
	ReasonNewRealWorldScenario     = "new_real_world_scenario"
	ReasonExistingScenarioRef      = "existing_scenario_reference"
	ReasonHistoricalReviewRequest  = "historical_review_request"
	ReasonMaterialContextRequest   = "material_context_request"
	ReasonHistoricalMistakeRequest = "historical_mistake_request"
	ReasonMissingRequiredContext   = "missing_required_context"
	ReasonExplicitCommand          = "explicit_command"
	ReasonToolUnavailable          = "tool_unavailable"
	ReasonPolicyRejected           = "policy_rejected"
)

type IntentDecision struct {
	Mode         IntentMode
	ReasonCode   string
	GuardVersion string
}

type IntentGuard struct {
	logger *slog.Logger
}

func NewIntentGuard(logger *slog.Logger) IntentGuard {
	return IntentGuard{logger: logger}
}

// Guard only classifies high-confidence language-help requests as direct.
// Ambiguous or contextual requests remain tool-eligible for model routing.
func (guard IntentGuard) Guard(runID string, input string) IntentDecision {
	decision := guardDecision(input)
	if guard.logger != nil {
		guard.logger.Debug(
			"agent.intent.guarded",
			"run_id", runID,
			"mode", string(decision.Mode),
			"reason_code", decision.ReasonCode,
			"guard_version", decision.GuardVersion,
		)
	}
	return decision
}

func guardDecision(input string) IntentDecision {
	text := normalizeIntentText(input)
	decision := IntentDecision{
		Mode:         IntentToolEligible,
		ReasonCode:   ReasonMissingRequiredContext,
		GuardVersion: intentGuardVersionV1,
	}
	if text == "" || hasContextReference(text) {
		return decision
	}
	switch {
	case hasAny(text, materialSignals):
		decision.ReasonCode = ReasonMaterialContextRequest
	case hasAny(text, reviewSignals):
		decision.ReasonCode = ReasonHistoricalReviewRequest
	case hasAny(text, mistakeHistorySignals):
		decision.ReasonCode = ReasonHistoricalMistakeRequest
	case hasAny(text, scenarioSignals):
		decision.ReasonCode = ReasonNewRealWorldScenario
	case isDirectLanguageHelp(text):
		decision.Mode = IntentDirectOnly
		decision.ReasonCode = ReasonDirectLanguageHelp
	}
	return decision
}

func isDirectLanguageHelp(text string) bool {
	return hasAny(text, directLanguageSignals) &&
		!hasAny(text, businessStateSignals)
}

func normalizeIntentText(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

func hasContextReference(text string) bool {
	return hasAny(text, contextReferenceSignals)
}

func hasAny(text string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

var directLanguageSignals = []string{
	"翻译",
	"怎么说",
	"怎么表达",
	"润色",
	"改写",
	"委婉",
	"礼貌",
	"自然",
	"专业",
	"polish",
	"translate",
	"rephrase",
	"grammar",
	"有什么问题",
	"什么意思",
	"哪里不对",
	"语法",
	"表达",
}

var contextReferenceSignals = []string{
	"上次",
	"那个",
	"刚才",
	"继续",
	"第一条",
	"上一轮",
	"之前",
	"last time",
	"previous",
	"continue",
}

var scenarioSignals = []string{
	"面试",
	"会议",
	"客户",
	"演讲",
	"场景",
	"准备",
	"interview",
	"meeting",
	"client",
	"presentation",
}

var reviewSignals = []string{
	"评价",
	"复盘",
	"review",
	"feedback",
}

var materialSignals = []string{
	"简历",
	"履历",
	"jd",
	"岗位",
	"resume",
	"material",
}

var mistakeHistorySignals = []string{
	"错题",
	"历史错误",
	"以前的错误",
	"mistake",
}

var businessStateSignals = append(
	append(append([]string{}, scenarioSignals...), reviewSignals...),
	append(materialSignals, mistakeHistorySignals...)...,
)
