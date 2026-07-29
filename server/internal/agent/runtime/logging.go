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
	case reasonModelToolSelection:
		if decision == "tool_call" {
			return "模型根据全量工具描述自主选择了工具。"
		}
		return "模型查看全量工具描述后选择直接回答。"
	case reasonExplicitCommand:
		return "用户输入匹配显式命令，按命令声明的工具执行。"
	case "budget_exhausted":
		return "本轮达到 Agent Loop 工具调用或迭代预算，返回稳定降级回复。"
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
