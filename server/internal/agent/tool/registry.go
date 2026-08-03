package tool

import (
	"errors"
	"sort"
)

var ErrDuplicateTool = errors.New("agent tool: duplicate tool")

type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建工具注册表，并注册传入的工具。
func NewRegistry(items ...Tool) (*Registry, error) {
	registry := &Registry{tools: make(map[string]Tool, len(items))}
	for _, item := range items {
		if err := registry.Register(item); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register 校验工具定义后，把一个工具实现加入注册表。
func (registry *Registry) Register(tool Tool) error {
	if registry == nil || tool == nil {
		return ErrInvalidDefinition
	}
	definition := tool.Definition()
	if err := ValidateDefinition(definition); err != nil {
		return err
	}
	if definition.ReadOnly {
		if _, conditional := tool.(InvocationEffectClassifier); conditional {
			return ErrInvalidDefinition
		}
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

// Get 按稳定工具名查找已注册工具。
func (registry *Registry) Get(name string) (Tool, bool) {
	if registry == nil {
		return nil, false
	}
	tool, ok := registry.tools[name]
	return tool, ok
}

// InvocationEffect is the authoritative write-effect classification for one
// registered invocation. Anything unknown or invalid fails closed as a
// possible write; Executor remains responsible for returning execution errors.
func (registry *Registry) InvocationEffect(
	invocation Invocation,
) InvocationEffect {
	registered, ok := registry.Get(invocation.Name)
	if !ok {
		return InvocationEffectMayWrite
	}
	definition := registered.Definition()
	normalizedInput, err := NormalizeInput(
		definition.InputSchema,
		invocation.Input,
	)
	if err != nil {
		return InvocationEffectMayWrite
	}
	if definition.ReadOnly {
		return InvocationEffectReadOnly
	}
	classifier, conditional := registered.(InvocationEffectClassifier)
	if !conditional {
		return InvocationEffectMayWrite
	}
	effect, err := classifier.ClassifyInvocationEffect(normalizedInput)
	if err != nil || !validInvocationEffect(effect) {
		return InvocationEffectMayWrite
	}
	return effect
}

// Definitions 按稳定顺序返回所有工具定义，供模型侧暴露使用。
func (registry *Registry) Definitions() []Definition {
	if registry == nil {
		return nil
	}
	definitions := make([]Definition, 0, len(registry.tools))
	for _, tool := range registry.tools {
		definitions = append(definitions, cloneDefinition(tool.Definition()))
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Name < definitions[right].Name
	})
	return definitions
}

func cloneDefinition(definition Definition) Definition {
	definition.InputSchema = cloneSchemaMap(definition.InputSchema)
	return definition
}

func validInvocationEffect(effect InvocationEffect) bool {
	return effect == InvocationEffectReadOnly ||
		effect == InvocationEffectMayWrite
}

func cloneSchemaMap(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	cloned := make(map[string]any, len(schema))
	for key, value := range schema {
		cloned[key] = cloneSchemaValue(value)
	}
	return cloned
}

func cloneSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneSchemaMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneSchemaValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
