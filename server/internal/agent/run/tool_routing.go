package run

import (
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
)

const (
	modelToolRoutingVersionV3 = "model-tool-routing-v3"
	reasonModelToolSelection  = "model_tool_selection"
)

type modelToolRouting struct {
	Definitions []ToolDefinition
	ToolChoice  ToolChoice
}

// buildModelToolRouting exposes the complete Registry without interpreting
// user text or business tool names.
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
			"routing_version", modelToolRoutingVersionV3,
			"tool_choice_mode", string(routing.ToolChoice.Mode),
			"tool_choice_name", routing.ToolChoice.Name,
		)
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
