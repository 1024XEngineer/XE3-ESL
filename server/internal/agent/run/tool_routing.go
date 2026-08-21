package run

import (
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
)

const (
	modelToolRoutingVersionV3 = "model-tool-routing-v3"
	reasonModelToolSelection  = "model_tool_selection"
	reasonDomainTurnCompleted = "domain_turn_completed"
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
	var definitions []capability.Definition
	if registry != nil {
		definitions = registry.Definitions()
	}
	return buildModelToolRoutingDefinitions(definitions, logger, runID, choice)
}

func buildModelToolRoutingDefinitions(
	definitions []capability.Definition,
	logger *slog.Logger,
	runID string,
	choice ToolChoice,
) modelToolRouting {
	if choice.Mode == "" {
		choice = ToolChoice{Mode: ToolChoiceAuto}
	}
	routing := modelToolRouting{ToolChoice: choice}
	if len(definitions) == 0 {
		return routing
	}
	routing.Definitions = make([]ToolDefinition, 0, len(definitions))
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
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
