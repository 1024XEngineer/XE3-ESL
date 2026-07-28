package tools

import (
	"errors"
	"sort"
)

var ErrDuplicateTool = errors.New("agent tool: duplicate tool")

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(items ...Tool) (*Registry, error) {
	registry := &Registry{tools: make(map[string]Tool, len(items))}
	for _, item := range items {
		if err := registry.Register(item); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (registry *Registry) Register(tool Tool) error {
	if registry == nil || tool == nil {
		return ErrInvalidDefinition
	}
	definition := tool.Definition()
	if err := ValidateDefinition(definition); err != nil {
		return err
	}
	if registry.tools == nil {
		registry.tools = make(map[string]Tool)
	}
	if _, exists := registry.tools[definition.Name]; exists {
		return ErrDuplicateTool
	}
	registry.tools[definition.Name] = tool
	return nil
}

func (registry *Registry) Get(name string) (Tool, bool) {
	if registry == nil {
		return nil, false
	}
	tool, ok := registry.tools[name]
	return tool, ok
}

func (registry *Registry) Definitions() []Definition {
	if registry == nil {
		return nil
	}
	definitions := make([]Definition, 0, len(registry.tools))
	for _, tool := range registry.tools {
		definitions = append(definitions, tool.Definition())
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Name < definitions[right].Name
	})
	return definitions
}
