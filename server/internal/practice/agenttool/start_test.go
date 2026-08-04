package agenttool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type startApplicationStub struct {
	calls        int
	confirmation practice.StartConfirmation
	key          string
	result       practice.ConfirmAndStartResult
	err          error
}

func (stub *startApplicationStub) ConfirmAndStartPractice(
	_ context.Context,
	_ requestcontext.Actor,
	key string,
	confirmation practice.StartConfirmation,
) (practice.ConfirmAndStartResult, error) {
	stub.calls++
	stub.key = key
	stub.confirmation = confirmation
	return stub.result, stub.err
}

func TestStartToolRejectsModelSelfConfirmation(t *testing.T) {
	application := &startApplicationStub{}
	port, err := NewStartServicePort(application)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewStartTool(port).Execute(
		context.Background(),
		startCallContext(),
		json.RawMessage(`{
			"practice_plan_id":"plan-1",
			"expected_plan_revision":1,
			"user_confirmed":true
		}`),
	)
	if err != nil || result.Content["status"] != "confirmation_required" {
		t.Fatalf("untrusted Start = (%#v, %v)", result, err)
	}
	if application.calls != 0 {
		t.Fatalf("untrusted Start application calls = %d", application.calls)
	}
}

func TestStartToolUsesMatchingTrustedConfirmation(t *testing.T) {
	application := &startApplicationStub{
		result: practice.ConfirmAndStartResult{
			Bootstrap: persistence.ContextSessionBootstrap{
				Session: persistence.ContextSession{
					ID:     "practice-session-1",
					PlanID: "plan-1",
					Status: persistence.ContextSessionStarting,
				},
				Snapshot: persistence.ContextSessionSnapshot{
					PlanRevision: 2,
				},
			},
			Replayed: true,
		},
	}
	port, err := NewStartServicePort(application)
	if err != nil {
		t.Fatal(err)
	}
	call := startCallContext()
	call.Confirmation = &tool.TrustedConfirmation{
		Kind:       practiceStartConfirmationKind,
		ResourceID: "plan-1",
		Revision:   2,
	}
	result, err := NewStartTool(port).Execute(
		context.Background(),
		call,
		json.RawMessage(`{
			"practice_plan_id":"plan-1",
			"expected_plan_revision":2,
			"user_confirmed":true
		}`),
	)
	if err != nil || result.Content["status"] != "started" ||
		result.Content["practice_session_id"] != "practice-session-1" ||
		len(result.SourceRefs) != 2 {
		t.Fatalf("trusted Start = (%#v, %v)", result, err)
	}
	if application.calls != 1 || application.key != call.RequestID ||
		application.confirmation.AgentThreadID != call.ThreadID {
		t.Fatalf("trusted delegation = %#v", application)
	}
}

func TestStartToolRejectsMismatchedTrustedRevision(t *testing.T) {
	application := &startApplicationStub{}
	port, err := NewStartServicePort(application)
	if err != nil {
		t.Fatal(err)
	}
	call := startCallContext()
	call.Confirmation = &tool.TrustedConfirmation{
		Kind:       practiceStartConfirmationKind,
		ResourceID: "plan-1",
		Revision:   1,
	}
	result, err := NewStartTool(port).Execute(
		context.Background(),
		call,
		json.RawMessage(`{
			"practice_plan_id":"plan-1",
			"expected_plan_revision":2,
			"user_confirmed":true
		}`),
	)
	if err != nil || result.Content["status"] != "confirmation_required" ||
		application.calls != 0 {
		t.Fatalf("mismatched Start = (%#v, %v)", result, err)
	}
}

func startCallContext() tool.CallContext {
	return tool.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		ThreadID:  "thread-1",
		RequestID: "request-1",
	}
}
