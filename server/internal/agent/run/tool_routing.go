package run

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
)

const (
	modelToolRoutingVersionV2  = "model-tool-routing-v2"
	reasonModelToolSelection   = "model_tool_selection"
	reasonIELTSCreationRouting = "ielts_creation_routing_guard"
	goalCreateToolName         = "goal.create.v1"
)

var (
	practiceActionPattern = regexp.MustCompile(
		`(?i)(创建|安排|准备|开始|来|做|进行|需要|想要?|帮我|直接).{0,12}(练习|模拟|mock|practice|interview)|` +
			`(练习|模拟|mock|practice).{0,12}(创建|安排|准备|开始|来|做|进行|需要|想要?|帮我|直接)|` +
			`来一场.{0,12}(面试|interview)`,
	)
	explicitGoalPattern = regexp.MustCompile(
		`(?i)(创建|建立|保存|记录|追踪|跟踪|管理).{0,8}(长期)?目标|` +
			`(长期)?目标.{0,8}(创建|建立|保存|记录|追踪|跟踪|管理)|` +
			`(create|save|track).{0,12}goal`,
	)
	upcomingEventPattern = regexp.MustCompile(
		`(?i)(明天|后天|下周|即将|马上|准备要|要参加|有一场).{0,16}(面试|会议|演讲|汇报|interview|meeting|presentation)`,
	)
	internalPracticeIdentifierPattern = regexp.MustCompile(
		`(?i)\s*\((?:scn|role|option)_[a-z0-9_:-]+\)|` +
			`(?:scn|role|option)_[a-z0-9_:-]+`,
	)
)

type modelToolRouting struct {
	Definitions []ToolDefinition
	ToolChoice  ToolChoice
}

func sanitizeUserVisiblePracticeIdentifiers(content string) string {
	return strings.TrimSpace(
		internalPracticeIdentifierPattern.ReplaceAllString(content, ""),
	)
}

// buildModelToolRouting exposes the complete Registry and applies the routing
// choice already decided at the Run boundary.
func buildModelToolRouting(
	registry *capability.Registry,
	logger *slog.Logger,
	runID string,
	choice ToolChoice,
) modelToolRouting {
	if choice.Mode == "" {
		choice = ToolChoice{Mode: ToolChoiceAuto}
	}
	routing := modelToolRouting{ToolChoice: choice}
	if registry == nil {
		return routing
	}
	registered := registry.Definitions()
	routing.Definitions = make([]ToolDefinition, 0, len(registered))
	names := make([]string, 0, len(registered))
	for _, definition := range registered {
		routing.Definitions = append(routing.Definitions, ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
		names = append(names, definition.Name)
	}
	if logger != nil {
		logger.Debug(
			"agent.tools.exposed",
			"run_id", runID,
			"tools", names,
			"tool_count", len(names),
			"routing_version", modelToolRoutingVersionV2,
			"tool_choice_mode", string(routing.ToolChoice.Mode),
			"tool_choice_name", routing.ToolChoice.Name,
		)
	}
	return routing
}

func applyUserIntentToolRouting(
	routing modelToolRouting,
	input string,
) modelToolRouting {
	input = strings.TrimSpace(input)
	if input == "" {
		return routing
	}
	previewExposed := false
	for _, definition := range routing.Definitions {
		if definition.Name == practicePreviewToolName {
			previewExposed = true
			break
		}
	}
	if !previewExposed {
		return routing
	}
	practiceIntent := practiceActionPattern.MatchString(input)
	if !explicitGoalPattern.MatchString(input) &&
		(practiceIntent || upcomingEventPattern.MatchString(input)) {
		filtered := make([]ToolDefinition, 0, len(routing.Definitions))
		for _, definition := range routing.Definitions {
			if definition.Name != goalCreateToolName {
				filtered = append(filtered, definition)
			}
		}
		routing.Definitions = filtered
	}
	if routing.ToolChoice.Mode == ToolChoiceAuto && practiceIntent {
		filtered := make([]ToolDefinition, 0, 1)
		for _, definition := range routing.Definitions {
			if definition.Name == practicePreviewToolName {
				filtered = append(filtered, definition)
			}
		}
		routing.Definitions = filtered
		routing.ToolChoice = ToolChoice{Mode: ToolChoiceRequired}
	}
	return routing
}

func applyModelToolSnapshot(
	manifest *agentcontext.Manifest,
	routing modelToolRouting,
) {
	manifest.ExposedTools = exposedToolNameList(routing.Definitions)
	manifest.ToolSchemaHashes = toolSchemaHashes(routing.Definitions)
}
