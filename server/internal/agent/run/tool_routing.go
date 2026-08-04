package run

import (
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	modelToolRoutingVersionV1 = "model-tool-routing-v1"
	reasonModelToolSelection  = "model_tool_selection"
)

type modelToolRouting struct {
	Definitions []ai.ToolDefinition
	ToolChoice  ai.ToolChoice
}

// buildModelToolRouting 将 Registry 中的全部工具交给模型自主选择。
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

func applyModelToolSnapshot(
	manifest *core.ContextManifest,
	routing modelToolRouting,
) {
	manifest.ExposedTools = exposedToolNameList(routing.Definitions)
	manifest.ToolSchemaHashes = toolSchemaHashes(routing.Definitions)
}
