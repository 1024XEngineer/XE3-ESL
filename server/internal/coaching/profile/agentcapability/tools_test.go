package agentcapability

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	toolUserID    = "10000000-0000-4000-8000-000000000001"
	toolThreadID  = "20000000-0000-4000-8000-000000000001"
	toolMessageID = "30000000-0000-4000-8000-000000000001"
)

func TestUpdateToolPatchesMultipleFieldsFromCurrentTrustedMessage(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(true)}
	reader := &messageReaderStub{message: trustedMessage(
		"我是产品经理，请叫我小林。",
	)}
	tool := NewUpdateTool(service, reader)
	result, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "patch":{"occupation":"产品经理","form_of_address":"小林"},
      "evidence":{"occupation":"我是产品经理","form_of_address":"请叫我小林"},
      "source_type":"explicit_current_fact"
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(service.command.Patch.Fields(), []coachingprofile.Field{
		coachingprofile.FieldFormOfAddress,
		coachingprofile.FieldOccupation,
	}) || service.command.SourceMessageID != toolMessageID ||
		service.command.SourceType != coachingprofile.SourceExplicitCurrentFact {
		t.Fatalf("update command = %#v", service.command)
	}
	if result.Content["version"] != int64(1) {
		t.Fatalf("result = %#v", result.Content)
	}
	if reader.ownerID != toolUserID || reader.threadID != toolThreadID ||
		reader.messageID != toolMessageID {
		t.Fatalf("trusted lookup = %#v", reader)
	}
}

func TestUpdateToolAcceptsExplicitDurablePreferenceWithoutKeywordDictionary(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(true)}
	reader := &messageReaderStub{message: trustedMessage("我偏好详细解释")}
	tool := NewUpdateTool(service, reader)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "patch":{"response_detail":"DETAILED"},
      "evidence":{"response_detail":"我偏好详细解释"},
      "source_type":"explicit_current_fact"
    }`))
	if err != nil {
		t.Fatalf("durable preference rejected: %v", err)
	}
}

func TestUpdateToolRejectsEvidenceNotInCurrentTrustedMessage(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(true)}
	reader := &messageReaderStub{message: trustedMessage("我是设计师")}
	tool := NewUpdateTool(service, reader)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "patch":{"occupation":"医生"},
      "evidence":{"occupation":"我是医生"},
      "source_type":"explicit_current_fact"
    }`))
	if !errors.Is(err, capability.ErrInvalidInput) || service.updateCalls != 0 {
		t.Fatalf("evidence error = %v, updates = %d", err, service.updateCalls)
	}
}

func TestUpdateToolRejectsMessageOutsideAuthenticatedActor(t *testing.T) {
	message := trustedMessage("我是设计师")
	message.OwnerID = "10000000-0000-4000-8000-000000000099"
	service := &serviceStub{profile: logicalProfile(true)}
	tool := NewUpdateTool(service, &messageReaderStub{message: message})
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "patch":{"occupation":"设计师"},
      "evidence":{"occupation":"我是设计师"},
      "source_type":"explicit_current_fact"
    }`))
	if !errors.Is(err, capability.ErrExecutionRejected) || service.updateCalls != 0 {
		t.Fatalf("actor isolation error = %v, updates = %d", err, service.updateCalls)
	}
}

func TestUpdateToolRejectsWhenMemoryDisabled(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(false)}
	tool := NewUpdateTool(
		service,
		&messageReaderStub{message: trustedMessage("我是设计师")},
	)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "patch":{"occupation":"设计师"},
      "evidence":{"occupation":"我是设计师"},
      "source_type":"explicit_current_fact"
    }`))
	if !errors.Is(err, capability.ErrExecutionRejected) || service.updateCalls != 0 {
		t.Fatalf("disabled update error = %v, updates = %d", err, service.updateCalls)
	}
}

func TestForgetToolWorksWhileMemoryDisabled(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(false)}
	tool := NewForgetTool(
		service,
		&messageReaderStub{message: trustedMessage("请忘掉我的职业")},
	)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "scope":"fields","fields":["occupation"],"evidence":"请忘掉我的职业"
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if service.updateCalls != 1 ||
		!reflect.DeepEqual(service.command.ForgetFields, []coachingprofile.Field{
			coachingprofile.FieldOccupation,
		}) {
		t.Fatalf("forget command = %#v", service.command)
	}
}

func TestForgetToolPreservesTrustedInputFailureCategory(t *testing.T) {
	message := trustedMessage("请忘掉我的职业")
	message.OwnerID = "10000000-0000-4000-8000-000000000099"
	service := &serviceStub{profile: logicalProfile(true)}
	tool := NewForgetTool(service, &messageReaderStub{message: message})
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "scope":"fields","fields":["occupation"],"evidence":"请忘掉我的职业"
    }`))
	if !errors.Is(err, capability.ErrExecutionRejected) || service.updateCalls != 0 {
		t.Fatalf("trusted input error = %v, updates = %d", err, service.updateCalls)
	}
}

func TestMemoryToolDisablesAndClearsProfileAtomically(t *testing.T) {
	item := logicalProfile(true)
	item.Data.Occupation = "产品经理"
	service := &serviceStub{profile: item}
	reader := &messageReaderStub{message: trustedMessage("关闭记忆并清空资料")}
	tool := NewMemoryTool(service, reader)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "enabled":false,"clear_profile":true,"evidence":"关闭记忆并清空资料"
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if service.updateCalls != 1 || !service.command.ClearProfile ||
		service.command.MemoryEnabled == nil || *service.command.MemoryEnabled ||
		service.command.SourceType != coachingprofile.SourceUserSetting ||
		service.command.SourceMessageID != "" {
		t.Fatalf("memory command = %#v", service.command)
	}
	if reader.messageID != toolMessageID || reader.ownerID != toolUserID {
		t.Fatalf("trusted lookup = %#v", reader)
	}
}

func TestMemoryToolReEnablesMemoryWhileDisabled(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(false)}
	tool := NewMemoryTool(
		service,
		&messageReaderStub{message: trustedMessage("重新开启记忆")},
	)
	_, err := tool.Execute(context.Background(), toolCall(), json.RawMessage(`{
      "enabled":true,"evidence":"重新开启记忆"
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if service.updateCalls != 1 || service.command.MemoryEnabled == nil ||
		!*service.command.MemoryEnabled || service.command.ClearProfile {
		t.Fatalf("memory command = %#v", service.command)
	}
}

func TestMemoryToolRejectsClearWhileEnablingOrUntrustedEvidence(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"clear while enabling": json.RawMessage(`{
          "enabled":true,"clear_profile":true,"evidence":"重新开启记忆"
        }`),
		"untrusted evidence": json.RawMessage(`{
          "enabled":false,"evidence":"关闭记忆"
        }`),
	} {
		t.Run(name, func(t *testing.T) {
			service := &serviceStub{profile: logicalProfile(true)}
			tool := NewMemoryTool(
				service,
				&messageReaderStub{message: trustedMessage("保留现状")},
			)
			_, err := tool.Execute(context.Background(), toolCall(), raw)
			if !errors.Is(err, capability.ErrInvalidInput) || service.updateCalls != 0 {
				t.Fatalf("error = %v updates = %d", err, service.updateCalls)
			}
		})
	}
}

func TestUpdateToolSchemaStripsModelSuppliedIdentityAndUnknownFields(t *testing.T) {
	service := &serviceStub{profile: logicalProfile(true)}
	reader := &messageReaderStub{message: trustedMessage("我是设计师")}
	tool := NewUpdateTool(service, reader)
	registry, err := capability.NewRegistry(tool)
	if err != nil {
		t.Fatal(err)
	}
	_, err = capability.NewExecutor(registry).Execute(
		context.Background(),
		toolCall(),
		capability.Invocation{Name: UpdateToolName, Input: json.RawMessage(`{
		"user_id":"attacker","source_message_id":"attacker-message",
		"patch":{"occupation":"设计师","owner_user_id":"attacker"},
        "evidence":{"occupation":"我是设计师"},
        "source_type":"explicit_current_fact"
      }`)},
	)
	if err != nil || service.updateCalls != 1 ||
		service.command.SourceMessageID != toolMessageID ||
		reader.ownerID != toolUserID {
		t.Fatalf(
			"filtered identity error = %v, command = %#v, reader owner = %q",
			err,
			service.command,
			reader.ownerID,
		)
	}
}

func toolCall() capability.CallContext {
	return capability.CallContext{
		Actor:    requestcontext.Actor{UserID: toolUserID, SessionID: "session-1"},
		ThreadID: toolThreadID, RunID: "run-1", InputMessageID: toolMessageID,
		ToolCallID: "call-1", RequestID: "request-1",
	}
}

func trustedMessage(content string) conversation.Message {
	return conversation.Message{
		ID: toolMessageID, OwnerID: toolUserID, ThreadID: toolThreadID,
		Role: conversation.MessageRoleUser, Content: content,
	}
}

func logicalProfile(enabled bool) coachingprofile.Profile {
	item := coachingprofile.Empty(toolUserID)
	item.MemoryEnabled = enabled
	return item
}

type serviceStub struct {
	profile     coachingprofile.Profile
	command     coachingprofile.UpdateCommand
	updateCalls int
}

func (service *serviceStub) Get(
	context.Context,
	requestcontext.Actor,
) (coachingprofile.Profile, error) {
	return service.profile, nil
}

func (service *serviceStub) Update(
	_ context.Context,
	_ requestcontext.Actor,
	command coachingprofile.UpdateCommand,
) (coachingprofile.Profile, error) {
	service.updateCalls++
	service.command = command
	service.profile.Version++
	return service.profile, nil
}

type messageReaderStub struct {
	message   conversation.Message
	ownerID   string
	threadID  string
	messageID string
}

func (reader *messageReaderStub) FindMessage(
	_ context.Context,
	ownerID string,
	threadID string,
	messageID string,
) (conversation.Message, error) {
	reader.ownerID = ownerID
	reader.threadID = threadID
	reader.messageID = messageID
	return reader.message, nil
}
