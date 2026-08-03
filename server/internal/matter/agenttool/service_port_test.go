package agenttool

import (
	"context"
	"errors"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type portRepositoryStub struct {
	createdRequest string
	searchQuery    matter.SearchQuery
}

type activeMatterLinkerStub struct {
	threadID string
	matterID string
}

func (stub *activeMatterLinkerStub) SetActiveMatter(
	_ context.Context,
	_ requestcontext.Actor,
	threadID string,
	matterID string,
) (agentconversation.ThreadMatterLink, error) {
	stub.threadID = threadID
	stub.matterID = matterID
	return agentconversation.ThreadMatterLink{
		ThreadID: threadID,
		MatterID: matterID,
		Active:   true,
	}, nil
}

func (stub *portRepositoryStub) Create(
	context.Context,
	string,
	string,
) (matter.Matter, error) {
	return matter.Matter{}, nil
}

func (stub *portRepositoryStub) CreateIdempotent(
	ctx context.Context,
	ownerID string,
	requestID string,
	title string,
) (matter.Matter, error) {
	stub.createdRequest = requestID
	return matter.Matter{
		ID:        "matter-1",
		OwnerID:   ownerID,
		Title:     title,
		Status:    matter.StatusActive,
		Version:   1,
		UpdatedAt: time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
	}, nil
}

func (stub *portRepositoryStub) ListOwned(
	context.Context,
	string,
) ([]matter.Matter, error) {
	return nil, nil
}

func (stub *portRepositoryStub) SearchOwned(
	ctx context.Context,
	ownerID string,
	query matter.SearchQuery,
) ([]matter.Matter, error) {
	stub.searchQuery = query
	return []matter.Matter{{
		ID:        "matter-1",
		OwnerID:   ownerID,
		Title:     "PM interview",
		Status:    matter.StatusActive,
		Version:   2,
		UpdatedAt: time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
	}}, nil
}

func (stub *portRepositoryStub) FindOwned(
	context.Context,
	string,
	string,
) (matter.Matter, error) {
	return matter.Matter{}, matter.ErrNotFound
}

func (stub *portRepositoryStub) UpdateStatus(
	context.Context,
	string,
	string,
	int64,
	matter.Status,
) (matter.Matter, error) {
	return matter.Matter{}, nil
}

func TestServicePortCreatesMatterWithTrustedRequest(t *testing.T) {
	repository := &portRepositoryStub{}
	linker := &activeMatterLinkerStub{}
	service, err := matter.NewService(repository)
	if err != nil {
		t.Fatalf("matter.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, linker)
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	result, err := port.CreateScenario(
		context.Background(),
		portCallContext(),
		ScenarioCreateInput{Title: "PM interview"},
	)
	if err != nil {
		t.Fatalf("CreateScenario() error = %v", err)
	}
	if repository.createdRequest != "request-1" ||
		linker.threadID != "thread-1" ||
		linker.matterID != "matter-1" ||
		result.MatterID != "matter-1" ||
		len(result.SourceRefs) != 1 ||
		result.SourceRefs[0].Type != "matter" {
		t.Fatalf("created result = %#v", result)
	}
}

func TestServicePortSearchUsesDefaultLimit(t *testing.T) {
	repository := &portRepositoryStub{}
	service, err := matter.NewService(repository)
	if err != nil {
		t.Fatalf("matter.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, &activeMatterLinkerStub{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	results, err := port.SearchScenarios(
		context.Background(),
		portCallContext(),
		ScenarioSearchInput{Query: "interview"},
	)
	if err != nil {
		t.Fatalf("SearchScenarios() error = %v", err)
	}
	if repository.searchQuery.Limit != defaultMatterSearchLimit ||
		len(results) != 1 ||
		results[0].UpdatedAt != "2026-07-29T08:00:00Z" {
		t.Fatalf("search query/result = %#v / %#v", repository.searchQuery, results)
	}
}

func TestServicePortRejectsMissingTrustedRequestID(t *testing.T) {
	repository := &portRepositoryStub{}
	service, err := matter.NewService(repository)
	if err != nil {
		t.Fatalf("matter.NewService() error = %v", err)
	}
	port, err := NewServicePort(service, &activeMatterLinkerStub{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	call := portCallContext()
	call.RequestID = ""
	_, err = port.CreateScenario(
		context.Background(),
		call,
		ScenarioCreateInput{Title: "PM interview"},
	)
	if !errors.Is(err, tool.ErrExecutionRejected) {
		t.Fatalf("CreateScenario() error = %v, want execution rejected", err)
	}
}

func portCallContext() tool.CallContext {
	return tool.CallContext{
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
