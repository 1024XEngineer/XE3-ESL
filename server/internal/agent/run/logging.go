package run

import (
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

func toolCallNames(calls []ModelToolCall) []string {
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
	case reasonModelToolSelection:
		if decision == "tool_call" {
			return "模型根据全量工具描述自主选择了工具。"
		}
		return "模型查看全量工具描述后选择直接回答。"
	case FailureToolIterationBudgetExhausted:
		return "本轮已达到 Agent Loop 工具迭代预算。"
	case FailureToolCallBudgetExhausted:
		return "本轮已达到 Agent Loop 工具调用预算。"
	case reasonDomainTurnCompleted:
		return "领域工具已完成本轮目标，直接提交规范回复与客户端动作。"
	case FailureDuplicateToolCall:
		return "模型重复提交了同一 ToolCall ID，本轮已停止执行。"
	default:
		return "模型根据工具描述决定直接回答、追问或调用工具。"
	}
}

func durationSince(startedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt)
}

func (service *Service) logAdvisoryStreamFailure(
	run Run,
	step ToolStep,
	event string,
) {
	if service == nil || service.logger == nil {
		return
	}
	service.logger.Warn(
		"agent.stream.advisory_delivery_failed",
		slog.String("run_id", run.ID),
		slog.String("thread_id", run.ThreadID),
		slog.String("tool_call_id", step.ID),
		slog.String("tool_name", step.Name),
		slog.String("event", event),
	)
}
