package agentcapability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type portRepositoryStub struct {
	createdRequest string
	searchQuery    goal.SearchQuery
}

type activeGoalLinkerStub struct {
	threadID string
	goalID   string
}

func (stub *activeGoalLinkerStub) SetActiveGoal(
	_ context.Context,
	_ requestcontext.Actor,
	threadID string,
	goalID string,
) (agentconversation.ThreadGoalLink, error) {
	stub.threadID = threadID
	stub.goalID = goalID
	return agentconversation.ThreadGoalLink{
		ThreadID: threadID,
		GoalID:   goalID,
		Active:   true,
	}, nil
}

func (stub *portRepositoryStub) Create(
	context.Context,
	string,
	string,
) (goal.Goal, error) {
	return goal.Goal{}, nil
}

func (stub *portRepositoryStub) CreateIdempotent(
	ctx context.Context,
	ownerID string,
	requestID string,
	title string,
) (goal.Goal, error) {
	stub.createdRequest = requestID
	return goal.Goal{
		ID:        "goal-1",
		OwnerID:   ownerID,
		Title:     title,
		Status:    goal.StatusActive,
		Version:   1,
		UpdatedAt: time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
	}, nil
}

func (stub *portRepositoryStub) ListOwned(
	context.Context,
	string,
) ([]goal.Goal, error) {
	return nil, nil
}

func (stub *portRepositoryStub) SearchOwned(
	ctx context.Context,
	ownerID string,
	query goal.SearchQuery,
) ([]goal.Goal, error) {
	stub.searchQuery = query
	return []goal.Goal{{
		ID:        "goal-1",
		OwnerID:   ownerID,
		Title:     "PM interview",
		Status:    goal.StatusActive,
		Version:   2,
		UpdatedAt: time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
	}}, nil
}

func (stub *portRepositoryStub) FindOwned(
	context.Context,
	string,
	string,
) (goal.Goal, error) {
	return goal.Goal{}, goal.ErrNotFound
}

func (stub *portRepositoryStub) UpdateStatus(
	context.Context,
	string,
	string,
	int64,
	goal.Status,
) (goal.Goal, error) {
	return goal.Goal{}, nil
}

func TestServicePortCreatesGoalWithTrustedRequest(t *testing.T) {
	repository := &portRepositoryStub{}
	linker := &activeGoalLinkerStub{}
	service, err := goal.NewService(repository)
	if err != nil {
		t.Fatalf("goal.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, linker)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	result, err := port.CreateGoal(
		context.Background(),
		portCallContext(),
		GoalCreateInput{Title: "PM interview"},
	)
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if repository.createdRequest != "request-1" ||
		linker.threadID != "thread-1" ||
		linker.goalID != "goal-1" ||
		result.GoalID != "goal-1" ||
		len(result.SourceRefs) != 1 ||
		result.SourceRefs[0].Type != "goal" {
		t.Fatalf("created result = %#v", result)
	}
}

func TestServicePortSearchUsesDefaultLimit(t *testing.T) {
	repository := &portRepositoryStub{}
	service, err := goal.NewService(repository)
	if err != nil {
		t.Fatalf("goal.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, &activeGoalLinkerStub{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	results, err := port.SearchGoals(
		context.Background(),
		portCallContext(),
		GoalSearchInput{Query: "interview"},
	)
	if err != nil {
		t.Fatalf("SearchGoals() error = %v", err)
	}
	if repository.searchQuery.Limit != defaultGoalSearchLimit ||
		len(results) != 1 ||
		results[0].UpdatedAt != "2026-07-29T08:00:00Z" {
		t.Fatalf("search query/result = %#v / %#v", repository.searchQuery, results)
	}
}

func TestServicePortRejectsMissingTrustedRequestID(t *testing.T) {
	repository := &portRepositoryStub{}
	service, err := goal.NewService(repository)
	if err != nil {
		t.Fatalf("goal.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, &activeGoalLinkerStub{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	call := portCallContext()
	call.RequestID = ""
	_, err = port.CreateGoal(
		context.Background(),
		call,
		GoalCreateInput{Title: "PM interview"},
	)
	if !errors.Is(err, capability.ErrExecutionRejected) {
		t.Fatalf("CreateGoal() error = %v, want execution rejected", err)
	}
}

func portCallContext() capability.CallContext {
	return capability.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}
