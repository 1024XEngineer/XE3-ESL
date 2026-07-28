package commands

import "strings"

type Router struct {
	registry *Registry
}

func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
}

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

func splitCommand(body string) (string, string) {
	parts := strings.Fields(body)
	if len(parts) == 0 {
		return "", ""
	}
	name := parts[0]
	args := strings.TrimSpace(strings.TrimPrefix(body, name))
	return name, args
}
