package commands

import "strings"

type Router struct {
	registry *Registry
}

// NewRouter creates a router that parses slash commands with the given registry.
func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
}

// Parse converts a slash command into a tool invocation and ignores non-command input.
func (router *Router) Parse(input string) (Parsed, bool, error) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return Parsed{}, false, nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if body == "" {
		return Parsed{}, true, ErrInvalidCommand
	}
	name, args := splitCommand(body)
	definition, ok := router.registry.Get(name)
	if !ok {
		return Parsed{}, true, ErrUnknownCommand
	}
	raw, err := definition.BuildInput(args)
	if err != nil {
		return Parsed{}, true, err
	}
	return Parsed{
		CommandName: definition.Name,
		Args:        args,
		Invocation:  agentInvocation(definition.ToolName, raw),
	}, true, nil
}

// splitCommand separates a slash-command body into command name and raw arguments.
func splitCommand(body string) (string, string) {
	parts := strings.Fields(body)
	if len(parts) == 0 {
		return "", ""
	}
	name := parts[0]
	args := strings.TrimSpace(strings.TrimPrefix(body, name))
	return name, args
}
