package runtime

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const defaultInputPreviewMax = 500

func normalizeLogOptions(options LogOptions) LogOptions {
	if options.InputPreviewMax <= 0 {
		options.InputPreviewMax = defaultInputPreviewMax
	}
	return options
}

func inputPreview(input string, limit int) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(input))
	clean = strings.Join(strings.Fields(clean), " ")
	return truncateString(clean, limit)
}

func truncateString(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func toolCallNames(calls []ai.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name != "" {
			names = append(names, call.Name)
		}
	}
	return names
}

func reasonSummary(reasonCode string, decision string) string {
	switch reasonCode {
	case ReasonDirectLanguageHelp:
		return "用户请求属于表达、翻译、润色或语法帮助，本轮直接回答。"
	case ReasonNewRealWorldScenario:
		if decision == "tool_call" {
			return "用户提到新的现实英语使用场景，模型选择调用场景相关工具。"
		}
		return "用户提到新的现实英语使用场景，需结合确认和候选工具继续判断。"
	case ReasonExistingScenarioRef:
		return "用户使用上下文指代表达，优先查找已有场景而不是重复创建。"
	case ReasonHistoricalReviewRequest:
		return "用户请求历史评价或复盘，候选工具聚焦 Review 查询。"
	case ReasonMaterialContextRequest:
		return "用户希望结合简历、JD 或材料上下文，候选工具聚焦材料检索。"
	case ReasonHistoricalMistakeRequest:
		return "用户请求历史错题或错误记录，候选工具聚焦错题检索。"
	case ReasonExplicitCommand:
		return "用户输入匹配显式命令，按命令声明的工具执行。"
	case ReasonToolUnavailable:
		return "候选工具当前未注册或功能开关不可用。"
	case ReasonPolicyRejected:
		return "服务端策略拒绝本轮工具调用。"
	case "budget_exhausted":
		return "本轮达到 Agent Loop 工具调用或迭代预算，返回稳定降级回复。"
	default:
		return "意图不够明确，模型在服务端策略允许范围内决定直接回答、追问或调用工具。"
	}
}

func durationSince(startedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt)
}
