package runtime

import (
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	modelToolRoutingVersionV1 = "model-tool-routing-v1"
	intentGuardDisabled       = "disabled"
	intentModeModelSelection  = "model_tool_selection"
	reasonModelToolSelection  = "model_tool_selection"
	reasonExplicitCommand     = "explicit_command"
)

type modelToolRouting struct {
	Definitions []ai.ToolDefinition
	Policy      tool.Policy
	ToolChoice  ai.ToolChoice
}

// buildModelToolRouting 将 Registry 中的全部工具交给模型自主选择。
// Policy 只保证模型不能执行未注册工具，不再承担权限、确认或关键词路由职责。
func buildModelToolRouting(
	registry *tool.Registry,
	logger *slog.Logger,
	runID string,
) modelToolRouting {
	routing := modelToolRouting{
		ToolChoice: ai.ToolChoice{Mode: ai.ToolChoiceAuto},
	}
	if registry == nil {
		return routing
	}
	registered := registry.Definitions()
	routing.Definitions = make([]ai.ToolDefinition, 0, len(registered))
	names := make([]string, 0, len(registered))
	for _, definition := range registered {
		routing.Definitions = append(routing.Definitions, ai.ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
		names = append(names, definition.Name)
	}
	routing.Policy = tool.Policy{
		AllowedNames:   append([]string(nil), names...),
		AllowWrites:    true,
		ConfirmedNames: append([]string(nil), names...),
	}
	if logger != nil {
		logger.Debug(
			"agent.tools.exposed",
			"run_id", runID,
			"tools", names,
			"tool_count", len(names),
			"routing_version", modelToolRoutingVersionV1,
			"tool_choice_mode", string(routing.ToolChoice.Mode),
		)
	}
	return routing
}

// commandExecutionPolicy 只让显式命令执行它已经解析出的那个工具。
// 命令执行结束后的模型请求仍会看到全部工具，并继续使用 auto 模式。
func commandExecutionPolicy(name string) tool.Policy {
	return tool.Policy{
		AllowedNames:   []string{name},
		AllowWrites:    true,
		ConfirmedNames: []string{name},
	}
}

func applyModelToolSnapshot(
	manifest *ContextManifest,
	routing modelToolRouting,
	reason string,
) {
	manifest.ExposedTools = exposedToolNameList(routing.Definitions)
	manifest.BlockedTools = nil
	manifest.IntentMode = intentModeModelSelection
	manifest.IntentReasonCode = reason
	manifest.IntentGuardVersion = intentGuardDisabled
	manifest.ToolPolicyVersion = modelToolRoutingVersionV1
	manifest.ToolSchemaHashes = toolSchemaHashes(routing.Definitions)
}
