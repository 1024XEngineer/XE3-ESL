package capability

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var ErrDuplicateTool = errors.New("agent tool: duplicate tool")

type Registry struct {
	tools map[string]Tool
}

// ExposureRequest is the trusted per-run identity available before model
// tool selection. Business tools may use it to enforce a blocking exposure
// policy without receiving or trusting model-authored authorization fields.
type ExposureRequest struct {
	Actor          requestcontext.Actor
	ThreadID       string
	RunID          string
	InputMessageID string
}

type ExposureDecision struct {
	Expose        bool
	Require       bool
	Authorization json.RawMessage
	AuditLabel    string
	Instruction   string
	InputSchema   map[string]any
}

type ExposureAuthorizer interface {
	AuthorizeExposure(
		context.Context,
		ExposureRequest,
	) (ExposureDecision, error)
}

type ExposurePlan struct {
	Definitions    []Definition
	RequiredTool   string
	Authorizations map[string]json.RawMessage
	AuditLabels    map[string]string
	InputSchemas   map[string]map[string]any
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

// ResolveExposure runs blocking, tool-owned authorization before definitions
// are sent to the model. A required decision is unique and fail-closed.
func (registry *Registry) ResolveExposure(
	ctx context.Context,
	request ExposureRequest,
) (ExposurePlan, error) {
	if registry == nil || ctx == nil {
		return ExposurePlan{}, ErrExecutionRejected
	}
	definitions := registry.Definitions()
	plan := ExposurePlan{
		Definitions:    make([]Definition, 0, len(definitions)),
		Authorizations: make(map[string]json.RawMessage),
		AuditLabels:    make(map[string]string),
		InputSchemas:   make(map[string]map[string]any),
	}
	for _, definition := range definitions {
		tool, found := registry.Get(definition.Name)
		if !found {
			return ExposurePlan{}, ErrInvalidDefinition
		}
		authorizer, guarded := tool.(ExposureAuthorizer)
		if !guarded {
			plan.Definitions = append(plan.Definitions, definition)
			continue
		}
		if !request.Actor.Valid() || request.ThreadID == "" ||
			request.RunID == "" || request.InputMessageID == "" {
			return ExposurePlan{}, ErrExecutionRejected
		}
		decision, err := authorizer.AuthorizeExposure(ctx, request)
		if err != nil {
			return ExposurePlan{}, err
		}
		if decision.AuditLabel != "" {
			plan.AuditLabels[definition.Name] = decision.AuditLabel
		}
		if !decision.Expose {
			if decision.Require || len(decision.Authorization) != 0 ||
				decision.Instruction != "" || decision.InputSchema != nil {
				return ExposurePlan{}, ErrExecutionRejected
			}
			continue
		}
		if len(decision.Authorization) != 0 {
			if !json.Valid(decision.Authorization) {
				return ExposurePlan{}, ErrExecutionRejected
			}
			plan.Authorizations[definition.Name] = append(
				json.RawMessage(nil),
				decision.Authorization...,
			)
		}
		if decision.Require {
			if plan.RequiredTool != "" {
				return ExposurePlan{}, ErrExecutionRejected
			}
			plan.RequiredTool = definition.Name
		}
		if decision.Instruction != "" {
			definition.Description += " " + decision.Instruction
		}
		if decision.InputSchema != nil {
			definition.InputSchema = cloneSchemaMap(decision.InputSchema)
			if err := ValidateDefinition(definition); err != nil {
				return ExposurePlan{}, ErrExecutionRejected
			}
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.InputSchemas[definition.Name] = cloneSchemaMap(definition.InputSchema)
	}
	return plan, nil
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
