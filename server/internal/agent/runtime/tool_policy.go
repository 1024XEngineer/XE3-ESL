package runtime

import (
	"log/slog"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const toolPolicyVersionV1 = "tool-policy-v1"

const (
	toolScenarioCreate = "scenario.create.v1"
	toolScenarioSearch = "scenario.search.v1"
	toolReviewSearch   = "review.search.v1"
	toolReviewGet      = "review.get.v1"
	toolMaterialSearch = "material.search.v1"
	toolMistakeSearch  = "mistake.search.v1"
)

type PolicyContext struct {
	Actor             requestcontext.Actor
	ThreadID          string
	ActiveMatterID    string
	EntryPoint        string
	ExplicitToolName  string
	ConfirmedActions  []string
	AvailableFeatures map[string]bool
	Intent            IntentDecision
}

type PolicyDecision struct {
	Policy        tool.Policy
	AllowedTools  []string
	BlockedTools  []BlockedTool
	PolicyVersion string
	ReasonCode    string
}

type BlockedTool struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type ToolPolicyBuilder struct {
	registry *tool.Registry
	logger   *slog.Logger
}

func NewToolPolicyBuilder(
	registry *tool.Registry,
	logger *slog.Logger,
) ToolPolicyBuilder {
	return ToolPolicyBuilder{registry: registry, logger: logger}
}

func (builder ToolPolicyBuilder) Build(
	runID string,
	context PolicyContext,
) PolicyDecision {
	decision := PolicyDecision{
		Policy: tool.Policy{
			AllowWrites:    context.Intent.Mode != IntentDirectOnly,
			ConfirmedNames: confirmedToolNames(context.ConfirmedActions),
		},
		PolicyVersion: toolPolicyVersionV1,
		ReasonCode:    context.Intent.ReasonCode,
	}
	if !context.Actor.Valid() || context.ThreadID == "" {
		decision.Policy.AllowWrites = false
		decision.Policy.AllowedNames = []string{}
		decision.ReasonCode = ReasonPolicyRejected
		builder.logDecision(runID, decision)
		return decision
	}
	if context.Intent.Mode == IntentDirectOnly && context.ExplicitToolName == "" {
		decision.AllowedTools = nil
		decision.BlockedTools = builder.blockAll(ReasonDirectLanguageHelp)
		builder.logDecision(runID, decision)
		return decision
	}
	if context.ExplicitToolName != "" {
		decision.Policy.AllowedNames = []string{context.ExplicitToolName}
		decision.ReasonCode = ReasonExplicitCommand
	} else {
		decision.Policy.AllowedNames = candidateToolNames(
			builder.registry,
			context,
		)
	}
	decision.AllowedTools, decision.BlockedTools = builder.selectNames(
		decision.Policy,
		context.AvailableFeatures,
	)
	builder.logDecision(runID, decision)
	return decision
}

func (builder ToolPolicyBuilder) blockAll(reason string) []BlockedTool {
	if builder.registry == nil {
		return nil
	}
	definitions := builder.registry.Definitions()
	blocked := make([]BlockedTool, 0, len(definitions))
	for _, definition := range definitions {
		blocked = append(blocked, BlockedTool{
			Name:   definition.Name,
			Reason: reason,
		})
	}
	return blocked
}

func (builder ToolPolicyBuilder) selectNames(
	policy tool.Policy,
	features map[string]bool,
) ([]string, []BlockedTool) {
	if builder.registry == nil {
		return nil, nil
	}
	definitions := builder.registry.Definitions()
	allowed := make([]string, 0, len(definitions))
	blocked := make([]BlockedTool, 0)
	for _, definition := range definitions {
		if !featureEnabled(features, definition.Name) {
			blocked = append(blocked, BlockedTool{
				Name:   definition.Name,
				Reason: ReasonToolUnavailable,
			})
			continue
		}
		if policy.Allows(definition) {
			allowed = append(allowed, definition.Name)
			continue
		}
		blocked = append(blocked, BlockedTool{
			Name:   definition.Name,
			Reason: ReasonPolicyRejected,
		})
	}
	return allowed, blocked
}

func (builder ToolPolicyBuilder) logDecision(
	runID string,
	decision PolicyDecision,
) {
	if builder.logger == nil {
		return
	}
	builder.logger.Debug(
		"agent.routing.candidates",
		"run_id", runID,
		"allowed_tools", decision.AllowedTools,
		"blocked_tools", decision.BlockedTools,
		"policy_version", decision.PolicyVersion,
		"reason_code", decision.ReasonCode,
	)
}

func candidateToolNames(
	registry *tool.Registry,
	context PolicyContext,
) []string {
	if registry == nil {
		return nil
	}
	candidates := reasonCandidateToolNames(context.Intent.ReasonCode)
	if len(candidates) == 0 && len(context.AvailableFeatures) == 0 {
		return nil
	}
	names := make([]string, 0)
	if len(candidates) == 0 {
		for _, definition := range registry.Definitions() {
			if context.ActiveMatterID != "" &&
				definition.Name == toolScenarioSearch {
				continue
			}
			if featureEnabled(context.AvailableFeatures, definition.Name) {
				names = append(names, definition.Name)
			}
		}
		return names
	}
	for _, name := range candidates {
		if context.ActiveMatterID != "" && name == toolScenarioSearch {
			continue
		}
		if _, ok := registry.Get(name); !ok {
			continue
		}
		if featureEnabled(context.AvailableFeatures, name) {
			names = append(names, name)
		}
	}
	return names
}

func reasonCandidateToolNames(reason string) []string {
	switch reason {
	case ReasonDirectLanguageHelp:
		return nil
	case ReasonNewRealWorldScenario:
		return []string{toolScenarioCreate, toolScenarioSearch, toolMaterialSearch}
	case ReasonExistingScenarioRef:
		return []string{toolScenarioSearch}
	case ReasonHistoricalReviewRequest:
		return []string{toolReviewSearch, toolReviewGet}
	case ReasonMaterialContextRequest:
		return []string{toolMaterialSearch, toolScenarioSearch, toolScenarioCreate}
	case ReasonHistoricalMistakeRequest:
		return []string{toolMistakeSearch}
	default:
		return nil
	}
}

func featureEnabled(features map[string]bool, name string) bool {
	if len(features) == 0 {
		return true
	}
	return features[name]
}

func confirmedToolNames(actions []string) []string {
	if len(actions) == 0 {
		return nil
	}
	names := make([]string, 0, len(actions))
	seen := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		if action == "" {
			continue
		}
		if _, exists := seen[action]; exists {
			continue
		}
		seen[action] = struct{}{}
		names = append(names, action)
	}
	return names
}
