package agentcapability

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	ShowToolName   = "coaching.profile.show.v1"
	UpdateToolName = "coaching.profile.update.v1"
	ForgetToolName = "coaching.profile.forget.v1"
	MemoryToolName = "coaching.profile.memory.v1"
)

type Service interface {
	Get(context.Context, requestcontext.Actor) (coachingprofile.Profile, error)
	Update(
		context.Context,
		requestcontext.Actor,
		coachingprofile.UpdateCommand,
	) (coachingprofile.Profile, error)
}

type TrustedInputReader interface {
	FindMessage(
		context.Context,
		string,
		string,
		string,
	) (conversation.Message, error)
}

type ShowTool struct {
	service Service
}

func NewShowTool(service Service) ShowTool {
	return ShowTool{service: service}
}

func (tool ShowTool) Definition() capability.Definition {
	return capability.Definition{
		Name: ShowToolName,
		Description: "Show the current user's saved coaching profile only when " +
			"they ask what is remembered or when their stable background is " +
			"directly needed. It uses the authenticated user and accepts no IDs.",
		InputSchema: capability.ObjectSchema(map[string]any{}, nil),
		ReadOnly:    true,
		Risk:        capability.RiskReadOnly,
	}
}

func (tool ShowTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.service == nil || capability.ValidateJSONObject(input) != nil {
		return capability.Result{}, capability.ErrInvalidInput
	}
	item, err := tool.service.Get(ctx, call.Actor)
	if err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Content: map[string]any{
		"coaching_profile": agentVisibleProfile(item),
	}}, nil
}

type UpdateTool struct {
	service  Service
	messages TrustedInputReader
}

func NewUpdateTool(service Service, messages TrustedInputReader) UpdateTool {
	return UpdateTool{service: service, messages: messages}
}

func (tool UpdateTool) Definition() capability.Definition {
	return capability.Definition{
		Name: UpdateToolName,
		Description: "Save one or more allowed stable coaching-profile fields " +
			"from the authenticated user's current message. Use only for an " +
			"explicit current fact or a durable remember/preference instruction; " +
			"never use role-play, hypothetical, historical, third-party, inferred, " +
			"or one-turn details. Evidence must be an exact current-message excerpt.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"patch":    profilePatchSchema(),
			"evidence": evidenceSchema(),
			"source_type": capability.StringEnumSchema(
				"Why the current user explicitly authorized this update.",
				string(coachingprofile.SourceExplicitCurrentFact),
				string(coachingprofile.SourceExplicitRememberInstruction),
			),
		}, []string{"patch", "evidence", "source_type"}),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (tool UpdateTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.service == nil || tool.messages == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	var request updateRequest
	if json.Unmarshal(input, &request) != nil {
		return capability.Result{}, capability.ErrInvalidInput
	}
	patch, fields, ok := request.Patch.domain()
	if !ok || !request.Evidence.matches(fields) {
		return capability.Result{}, capability.ErrInvalidInput
	}
	message, err := trustedInput(ctx, tool.messages, call)
	if err != nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	evidence := request.Evidence.forFields(fields)
	if !validUpdateEvidence(message.Content, evidence, fields, request.SourceType) {
		return capability.Result{}, capability.ErrInvalidInput
	}
	current, err := tool.service.Get(ctx, call.Actor)
	if err != nil {
		return capability.Result{}, err
	}
	if !current.MemoryEnabled {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	updated, err := tool.service.Update(ctx, call.Actor, coachingprofile.UpdateCommand{
		ExpectedVersion: current.Version,
		Patch:           patch,
		SourceType:      request.SourceType,
		SourceMessageID: call.InputMessageID,
	})
	if err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Content: map[string]any{
		"status":         "updated",
		"updated_fields": fields,
		"version":        updated.Version,
		"memory_enabled": updated.MemoryEnabled,
	}}, nil
}

type ForgetTool struct {
	service  Service
	messages TrustedInputReader
}

// MemoryTool is the conversational privacy control for Coaching Profile use.
// It deliberately does not share UpdateTool's field patch contract: disabling,
// re-enabling and optional atomic clearing remain available even while memory
// is disabled.
type MemoryTool struct {
	service  Service
	messages TrustedInputReader
}

func NewMemoryTool(service Service, messages TrustedInputReader) MemoryTool {
	return MemoryTool{service: service, messages: messages}
}

func (tool MemoryTool) Definition() capability.Definition {
	return capability.Definition{
		Name: MemoryToolName,
		Description: "Turn Coaching Profile memory on or off only when the " +
			"authenticated user explicitly asks in the current message. When " +
			"turning it off, clear_profile may atomically erase all saved profile " +
			"fields. Evidence must be an exact current-message excerpt. This does " +
			"not delete conversations, practice, or reports.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Whether Coaching Profile memory should be enabled.",
			},
			"clear_profile": map[string]any{
				"type": "boolean",
				"description": "Erase all saved profile fields atomically; " +
					"allowed only while disabling memory.",
			},
			"evidence": capability.TextSchema(
				"Exact current-message excerpt requesting this privacy change.",
				coachingprofile.MaxEvidenceRunes,
			),
		}, []string{"enabled", "evidence"}),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (tool MemoryTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.service == nil || tool.messages == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	var request memoryRequest
	if json.Unmarshal(input, &request) != nil || !request.valid() {
		return capability.Result{}, capability.ErrInvalidInput
	}
	message, err := trustedInput(ctx, tool.messages, call)
	if err != nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	if !strings.Contains(message.Content, request.Evidence) {
		return capability.Result{}, capability.ErrInvalidInput
	}
	current, err := tool.service.Get(ctx, call.Actor)
	if err != nil {
		return capability.Result{}, err
	}
	enabled := *request.Enabled
	if current.MemoryEnabled == enabled &&
		(!request.ClearProfile || current.Data.Empty()) {
		return capability.Result{Content: map[string]any{
			"status":          "unchanged",
			"memory_enabled":  current.MemoryEnabled,
			"profile_cleared": current.Data.Empty(),
			"version":         current.Version,
		}}, nil
	}
	updated, err := tool.service.Update(ctx, call.Actor, coachingprofile.UpdateCommand{
		ExpectedVersion: current.Version,
		ClearProfile:    request.ClearProfile,
		MemoryEnabled:   &enabled,
		SourceType:      coachingprofile.SourceUserSetting,
	})
	if err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Content: map[string]any{
		"status":          "updated",
		"memory_enabled":  updated.MemoryEnabled,
		"profile_cleared": request.ClearProfile,
		"version":         updated.Version,
	}}, nil
}

func NewForgetTool(service Service, messages TrustedInputReader) ForgetTool {
	return ForgetTool{service: service, messages: messages}
}

func (tool ForgetTool) Definition() capability.Definition {
	return capability.Definition{
		Name: ForgetToolName,
		Description: "Forget selected saved coaching-profile fields or clear " +
			"the entire profile when the authenticated user explicitly asks. " +
			"Evidence must be an exact excerpt from the current user message. " +
			"This never deletes conversations, practice, or reports.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"scope": capability.StringEnumSchema(
				"Whether to forget selected fields or the whole coaching profile.",
				"fields", "all",
			),
			"fields": map[string]any{
				"type":        "array",
				"description": "Allowed fields to forget; empty only when scope is all.",
				"items":       fieldSchema("One allowed field."),
				"minItems":    0,
				"maxItems":    len(coachingprofile.Fields()),
			},
			"evidence": capability.TextSchema(
				"Exact current-message excerpt containing the forget request.",
				coachingprofile.MaxEvidenceRunes,
			),
		}, []string{"scope", "fields", "evidence"}),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (tool ForgetTool) Execute(
	ctx context.Context,
	call capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if tool.service == nil || tool.messages == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	var request forgetRequest
	if json.Unmarshal(input, &request) != nil || !request.valid() {
		return capability.Result{}, capability.ErrInvalidInput
	}
	message, err := trustedInput(ctx, tool.messages, call)
	if err != nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	if !strings.Contains(message.Content, request.Evidence) {
		return capability.Result{}, capability.ErrInvalidInput
	}
	current, err := tool.service.Get(ctx, call.Actor)
	if err != nil {
		return capability.Result{}, err
	}
	fields := append([]coachingprofile.Field(nil), request.Fields...)
	updated, err := tool.service.Update(ctx, call.Actor, coachingprofile.UpdateCommand{
		ExpectedVersion: current.Version,
		ForgetFields:    fields,
		ClearProfile:    request.Scope == "all",
		SourceType:      coachingprofile.SourceUserSetting,
	})
	if err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Content: map[string]any{
		"status":           "forgotten",
		"cleared_all":      request.Scope == "all",
		"forgotten_fields": fields,
		"version":          updated.Version,
		"memory_enabled":   updated.MemoryEnabled,
	}}, nil
}

type updateRequest struct {
	Patch      patchInput                 `json:"patch"`
	Evidence   evidenceInput              `json:"evidence"`
	SourceType coachingprofile.SourceType `json:"source_type"`
}

type patchInput struct {
	FormOfAddress       *string                         `json:"form_of_address"`
	Occupation          *string                         `json:"occupation"`
	ProfessionalContext *string                         `json:"professional_context"`
	NativeLanguage      *string                         `json:"native_language"`
	ExplanationLanguage *string                         `json:"explanation_language"`
	ResponseDetail      *coachingprofile.ResponseDetail `json:"response_detail"`
	Interests           *[]string                       `json:"interests"`
}

func (input patchInput) domain() (coachingprofile.DataPatch, []coachingprofile.Field, bool) {
	patch := coachingprofile.DataPatch{
		FormOfAddress:       input.FormOfAddress,
		Occupation:          input.Occupation,
		ProfessionalContext: input.ProfessionalContext,
		NativeLanguage:      input.NativeLanguage,
		ExplanationLanguage: input.ExplanationLanguage,
		ResponseDetail:      input.ResponseDetail,
		Interests:           input.Interests,
	}
	fields := patch.Fields()
	if len(fields) == 0 {
		return coachingprofile.DataPatch{}, nil, false
	}
	_, valid := patch.Apply(coachingprofile.Data{})
	return patch, fields, valid
}

type evidenceInput struct {
	FormOfAddress       *string `json:"form_of_address"`
	Occupation          *string `json:"occupation"`
	ProfessionalContext *string `json:"professional_context"`
	NativeLanguage      *string `json:"native_language"`
	ExplanationLanguage *string `json:"explanation_language"`
	ResponseDetail      *string `json:"response_detail"`
	Interests           *string `json:"interests"`
}

func (input evidenceInput) matches(fields []coachingprofile.Field) bool {
	values := input.values()
	if len(values) != len(fields) {
		return false
	}
	for _, field := range fields {
		value, found := values[field]
		if !found || !coachingprofile.ValidEvidence(value) {
			return false
		}
	}
	return true
}

func (input evidenceInput) forFields(fields []coachingprofile.Field) map[coachingprofile.Field]string {
	values := input.values()
	result := make(map[coachingprofile.Field]string, len(fields))
	for _, field := range fields {
		result[field] = values[field]
	}
	return result
}

func (input evidenceInput) values() map[coachingprofile.Field]string {
	result := make(map[coachingprofile.Field]string)
	add := func(field coachingprofile.Field, value *string) {
		if value != nil {
			result[field] = *value
		}
	}
	add(coachingprofile.FieldFormOfAddress, input.FormOfAddress)
	add(coachingprofile.FieldOccupation, input.Occupation)
	add(coachingprofile.FieldProfessionalContext, input.ProfessionalContext)
	add(coachingprofile.FieldNativeLanguage, input.NativeLanguage)
	add(coachingprofile.FieldExplanationLanguage, input.ExplanationLanguage)
	add(coachingprofile.FieldResponseDetail, input.ResponseDetail)
	add(coachingprofile.FieldInterests, input.Interests)
	return result
}

type forgetRequest struct {
	Scope    string                  `json:"scope"`
	Fields   []coachingprofile.Field `json:"fields"`
	Evidence string                  `json:"evidence"`
}

type memoryRequest struct {
	Enabled      *bool  `json:"enabled"`
	ClearProfile bool   `json:"clear_profile"`
	Evidence     string `json:"evidence"`
}

func (request memoryRequest) valid() bool {
	return request.Enabled != nil &&
		!(*request.Enabled && request.ClearProfile) &&
		coachingprofile.ValidEvidence(request.Evidence)
}

func (request forgetRequest) valid() bool {
	if !coachingprofile.ValidEvidence(request.Evidence) {
		return false
	}
	if request.Scope == "all" {
		return len(request.Fields) == 0
	}
	if request.Scope != "fields" || len(request.Fields) == 0 {
		return false
	}
	seen := make(map[coachingprofile.Field]struct{}, len(request.Fields))
	for _, field := range request.Fields {
		if !field.Valid() {
			return false
		}
		if _, duplicate := seen[field]; duplicate {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func trustedInput(
	ctx context.Context,
	reader TrustedInputReader,
	call capability.CallContext,
) (conversation.Message, error) {
	if !call.Actor.Valid() || call.InputMessageID == "" {
		return conversation.Message{}, capability.ErrExecutionRejected
	}
	message, err := reader.FindMessage(
		ctx,
		call.Actor.UserID,
		call.ThreadID,
		call.InputMessageID,
	)
	if err != nil || message.ID != call.InputMessageID ||
		message.OwnerID != call.Actor.UserID || message.ThreadID != call.ThreadID ||
		message.Role != conversation.MessageRoleUser {
		return conversation.Message{}, capability.ErrExecutionRejected
	}
	return message, nil
}

func validUpdateEvidence(
	message string,
	evidence map[coachingprofile.Field]string,
	fields []coachingprofile.Field,
	source coachingprofile.SourceType,
) bool {
	// This boundary proves provenance only: every excerpt came from the trusted
	// current user message. Mapping free-form language to the fixed profile
	// value is intentionally governed by the tool instruction and Agent evals;
	// a keyword dictionary or second model call would create a competing policy.
	if source != coachingprofile.SourceExplicitRememberInstruction &&
		source != coachingprofile.SourceExplicitCurrentFact {
		return false
	}
	for _, field := range fields {
		excerpt := evidence[field]
		if !strings.Contains(message, excerpt) {
			return false
		}
	}
	return true
}

func profilePatchSchema() map[string]any {
	return capability.ObjectSchema(map[string]any{
		"form_of_address": capability.TextSchema(
			"Exact user-requested form of address.",
			coachingprofile.MaxFormOfAddressRunes,
		),
		"occupation": capability.TextSchema(
			"The user's explicitly stated current occupation.",
			coachingprofile.MaxOccupationRunes,
		),
		"professional_context": capability.TextSchema(
			"A concise, explicitly stated professional context; never a resume or JD.",
			coachingprofile.MaxProfessionalContextRunes,
		),
		"native_language": capability.TextSchema(
			"The user's explicitly stated native language.",
			coachingprofile.MaxLanguageRunes,
		),
		"explanation_language": capability.TextSchema(
			"The user's durable preferred explanation language.",
			coachingprofile.MaxLanguageRunes,
		),
		"response_detail": capability.StringEnumSchema(
			"Durable response detail preference.",
			string(coachingprofile.ResponseConcise),
			string(coachingprofile.ResponseBalanced),
			string(coachingprofile.ResponseDetailed),
		),
		"interests": map[string]any{
			"type":        "array",
			"description": "The user's explicitly stated durable interests.",
			"items": capability.TextSchema(
				"One interest.", coachingprofile.MaxInterestRunes,
			),
			"minItems": 1,
			"maxItems": coachingprofile.MaxInterests,
		},
	}, nil)
}

func evidenceSchema() map[string]any {
	properties := make(map[string]any, len(coachingprofile.Fields()))
	for _, field := range coachingprofile.Fields() {
		properties[string(field)] = capability.TextSchema(
			"Exact current-message excerpt supporting this field.",
			coachingprofile.MaxEvidenceRunes,
		)
	}
	return capability.ObjectSchema(properties, nil)
}

func fieldSchema(description string) map[string]any {
	values := make([]string, 0, len(coachingprofile.Fields()))
	for _, field := range coachingprofile.Fields() {
		values = append(values, string(field))
	}
	return capability.StringEnumSchema(description, values...)
}

func agentVisibleProfile(item coachingprofile.Profile) map[string]any {
	sources := make(map[string]any, len(item.FieldSources))
	for field, source := range item.FieldSources {
		sources[string(field)] = map[string]any{
			"type":        source.Type,
			"recorded_at": source.RecordedAt.Format(time.RFC3339Nano),
		}
	}
	return map[string]any{
		"memory_enabled": item.MemoryEnabled,
		"profile":        item.Data,
		"field_sources":  sources,
		"version":        item.Version,
	}
}

var (
	_ capability.Tool = ShowTool{}
	_ capability.Tool = UpdateTool{}
	_ capability.Tool = ForgetTool{}
	_ capability.Tool = MemoryTool{}
)
