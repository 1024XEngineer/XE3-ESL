package agentconversation

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestReaderProjectsOnlyConversationGoalState(t *testing.T) {
	const goalID = "10000000-0000-4000-8000-000000000001"
	reader, err := New(goalReaderStub{item: goal.Goal{
		ID:     goalID,
		Status: goal.StatusActive,
	}})
	if err != nil {
		t.Fatal(err)
	}

	state, found, err := reader.ReadGoalState(
		context.Background(),
		requestcontext.Actor{UserID: goalID, SessionID: "session"},
		goalID,
	)
	if err != nil || !found || state.ID != goalID || !state.Active {
		t.Fatalf("ReadGoalState() = %#v, %v, %v", state, found, err)
	}
}

func TestReaderMapsMissingGoalToNotFoundProjection(t *testing.T) {
	reader, err := New(goalReaderStub{err: goal.ErrNotFound})
	if err != nil {
		t.Fatal(err)
	}

	state, found, err := reader.ReadGoalState(
		context.Background(),
		requestcontext.Actor{UserID: "owner", SessionID: "session"},
		"missing",
	)
	if err != nil || found || state.ID != "" || state.Active {
		t.Fatalf("ReadGoalState() = %#v, %v, %v", state, found, err)
	}
}

type goalReaderStub struct {
	item goal.Goal
	err  error
}

func (stub goalReaderStub) ReadOwned(
	context.Context,
	requestcontext.Actor,
	string,
) (goal.Goal, error) {
	return stub.item, stub.err
}
