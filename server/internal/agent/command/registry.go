package command

import "sort"

type Registry struct {
	commands map[string]Definition
}

// NewRegistry 创建命令注册表，并注册传入的命令定义。
func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{commands: make(map[string]Definition)}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register 把一个命令定义及其别名加入注册表。
func (registry *Registry) Register(definition Definition) error {
	if registry == nil {
		return ErrInvalidDefinition
	}
	if err := ValidateDefinition(definition); err != nil {
		return err
	}
	if registry.commands == nil {
		registry.commands = make(map[string]Definition)
	}
	names := append([]string{definition.Name}, definition.Aliases...)
	for _, name := range names {
		if _, exists := registry.commands[name]; exists {
			return ErrDuplicateCommand
		}
	}
	for _, name := range names {
		registry.commands[name] = definition
	}
	return nil
}

// Get 按命令名或别名查找命令定义。
func (registry *Registry) Get(name string) (Definition, bool) {
	if registry == nil {
		return Definition{}, false
	}
	definition, ok := registry.commands[name]
	return definition, ok
}

// Definitions 按稳定顺序返回去重后的主命令定义。
func (registry *Registry) Definitions() []Definition {
	if registry == nil {
		return nil
	}
	seen := make(map[string]struct{})
	definitions := make([]Definition, 0)
	for _, definition := range registry.commands {
		if _, exists := seen[definition.Name]; exists {
			continue
		}
		seen[definition.Name] = struct{}{}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Name < definitions[right].Name
	})
	return definitions
}
