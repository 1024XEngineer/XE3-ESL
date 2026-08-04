package memorysource

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestAgentStableProfileReaderPreservesFields(t *testing.T) {
	t.Parallel()
	actor := stableProfileActor()
	now := time.Now().UTC()
	delegate := &recordingStableProfileReader{
		items: []memory.Memory{{
			ID:            "10000000-0000-4000-8000-000000000001",
			OwnerID:       actor.UserID,
			Type:          memory.TypeProfile,
			CanonicalKey:  memory.CanonicalProfilePreferredName,
			Content:       "小花",
			Scope:         memory.ScopeUser,
			Status:        memory.StatusActive,
			Version:       3,
			PolicyVersion: "memory-policy-v2",
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}
	adapter, err := NewStableProfileReader(delegate)
	if err != nil {
		t.Fatalf("newAgentStableProfileReader: %v", err)
	}
	request := agentcontext.StableProfileReadRequest{Actor: actor}
	items, err := adapter.ReadStableProfile(context.Background(), request)
	if err != nil {
		t.Fatalf("ReadStableProfile: %v", err)
	}
	if delegate.actor != actor {
		t.Fatalf("domain actor = %#v", delegate.actor)
	}
	if len(items) != 1 ||
		items[0].MemoryID != delegate.items[0].ID ||
		items[0].MemoryVersion != delegate.items[0].Version ||
		items[0].CanonicalKey != delegate.items[0].CanonicalKey ||
		items[0].Type != string(delegate.items[0].Type) ||
		items[0].Content != delegate.items[0].Content ||
		items[0].Scope != string(delegate.items[0].Scope) ||
		!items[0].Valid() {
		t.Fatalf("Agent Stable Profile = %#v", items)
	}
}

func TestAgentStableProfileReaderAcceptsEmptyProfile(t *testing.T) {
	t.Parallel()
	adapter, err := NewStableProfileReader(
		&recordingStableProfileReader{items: []memory.Memory{}},
	)
	if err != nil {
		t.Fatalf("newAgentStableProfileReader: %v", err)
	}
	items, err := adapter.ReadStableProfile(
		context.Background(),
		agentcontext.StableProfileReadRequest{Actor: stableProfileActor()},
	)
	if err != nil {
		t.Fatalf("ReadStableProfile: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty Stable Profile = %#v", items)
	}
}

func TestAgentStableProfileReaderRejectsInvalidDomainResult(t *testing.T) {
	t.Parallel()
	actor := stableProfileActor()
	now := time.Now().UTC()
	adapter, err := NewStableProfileReader(
		&recordingStableProfileReader{items: []memory.Memory{{
			ID:            "10000000-0000-4000-8000-000000000001",
			OwnerID:       actor.UserID,
			Type:          memory.TypeProfile,
			CanonicalKey:  "profile.unapproved",
			Content:       "must not pass",
			Scope:         memory.ScopeUser,
			Status:        memory.StatusActive,
			Version:       1,
			PolicyVersion: "memory-policy-v2",
			CreatedAt:     now,
			UpdatedAt:     now,
		}}},
	)
	if err != nil {
		t.Fatalf("newAgentStableProfileReader: %v", err)
	}
	if _, err := adapter.ReadStableProfile(
		context.Background(),
		agentcontext.StableProfileReadRequest{Actor: actor},
	); !errors.Is(err, memory.ErrRepository) {
		t.Fatalf("invalid domain result error = %v", err)
	}
}

func TestAgentStableProfileReaderRequiresDependencyAndPropagatesFailure(
	t *testing.T,
) {
	t.Parallel()
	if adapter, err := NewStableProfileReader(nil); err == nil ||
		adapter != nil {
		t.Fatalf("nil adapter = %#v, %v", adapter, err)
	}
	dependencyError := errors.New("profile database unavailable")
	adapter, err := NewStableProfileReader(
		&recordingStableProfileReader{err: dependencyError},
	)
	if err != nil {
		t.Fatalf("newAgentStableProfileReader: %v", err)
	}
	if _, err := adapter.ReadStableProfile(
		context.Background(),
		agentcontext.StableProfileReadRequest{Actor: stableProfileActor()},
	); !errors.Is(err, dependencyError) {
		t.Fatalf("ReadStableProfile error = %v", err)
	}
	if _, err := adapter.ReadStableProfile(
		context.Background(),
		agentcontext.StableProfileReadRequest{},
	); !errors.Is(err, memory.ErrInvalidArgument) {
		t.Fatalf("invalid request error = %v", err)
	}
}

type recordingStableProfileReader struct {
	actor requestcontext.Actor
	items []memory.Memory
	err   error
}

func (reader *recordingStableProfileReader) ListStableProfile(
	_ context.Context,
	actor requestcontext.Actor,
) ([]memory.Memory, error) {
	reader.actor = actor
	return append([]memory.Memory(nil), reader.items...), reader.err
}

func stableProfileActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "20000000-0000-4000-8000-000000000001",
		SessionID: "30000000-0000-4000-8000-000000000001",
	}
}
