package agentcontext

import (
	"context"
	"testing"

	agentctx "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestReaderProjectsOnlyAgentContextFields(t *testing.T) {
	const goalID = "10000000-0000-4000-8000-000000000001"
	reader, err := New(goalReaderStub{item: goal.Goal{
		ID:      goalID,
		Title:   "Backend interview",
		Status:  goal.StatusActive,
		Version: 3,
	}})
	if err != nil {
		t.Fatal(err)
	}

	projection, found, err := reader.ReadGoalContext(
		context.Background(),
		requestcontext.Actor{UserID: goalID, SessionID: "session"},
		goalID,
	)
	want := agentctx.GoalContext{
		ID:      goalID,
		Title:   "Backend interview",
		Version: 3,
		Active:  true,
	}
	if err != nil || !found || projection != want {
		t.Fatalf("ReadGoalContext() = %#v, %v, %v", projection, found, err)
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
