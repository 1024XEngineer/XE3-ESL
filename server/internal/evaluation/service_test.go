package evaluation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testOwnerA = "10000000-0000-4000-8000-000000000001"
	testOwnerB = "20000000-0000-4000-8000-000000000002"
	testEvalID = "30000000-0000-4000-8000-000000000003"
	testRevID  = "40000000-0000-4000-8000-000000000004"
)

func TestServiceCreateCanonicalizesAndDerivesServerIdentity(t *testing.T) {
	repository := &repositoryStub{evaluation: validEvaluation()}
	service := NewService(repository)
	request := validCreateRequest()
	request.Channels = []Channel{ChannelScene, ChannelCore4D}
	request.Core4DStrategyRef = "core4d/v1"

	_, _, err := service.Create(
		context.Background(),
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first := repository.ensureCommands[0]
	if got := first.Input.Config.Channels; len(got) != 2 ||
		got[0] != ChannelCore4D || got[1] != ChannelScene {
		t.Fatalf("canonical channels = %#v", got)
	}
	if first.OwnerUserID != testOwnerA {
		t.Fatalf("owner = %q, want trusted actor", first.OwnerUserID)
	}

	request.ClientRequestID = "another-trace"
	_, _, err = service.Create(
		context.Background(),
		testActor(testOwnerA),
		request,
	)
	if err != nil {
		t.Fatalf("replayed Create: %v", err)
	}
	second := repository.ensureCommands[1]
	if first.RootIdempotencyKey != second.RootIdempotencyKey ||
		first.RootFingerprint != second.RootFingerprint ||
		first.RevisionFingerprint != second.RevisionFingerprint {
		t.Fatal("client request ID changed server idempotency identity")
	}

	_, _, err = service.Create(
		context.Background(),
		testActor(testOwnerB),
		request,
	)
	if err != nil {
		t.Fatalf("other-owner Create: %v", err)
	}
	third := repository.ensureCommands[2]
	if first.RootIdempotencyKey == third.RootIdempotencyKey {
		t.Fatal("root idempotency identity is not owner scoped")
	}
}

func TestServiceRejectsInvalidPolicyShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreateRequest)
	}{
		{
			name: "duplicate channel",
			mutate: func(request *CreateRequest) {
				request.Channels = []Channel{ChannelScene, ChannelScene}
			},
		},
		{
			name: "missing scene strategy",
			mutate: func(request *CreateRequest) {
				request.SceneStrategyRef = ""
			},
		},
		{
			name: "unexpected core strategy",
			mutate: func(request *CreateRequest) {
				request.Core4DStrategyRef = "core/v1"
			},
		},
		{
			name: "invalid pipeline",
			mutate: func(request *CreateRequest) {
				request.PipelineVersion = "invalid pipeline"
			},
		},
		{
			name: "invalid snapshot",
			mutate: func(request *CreateRequest) {
				request.InputSnapshotID = "../snapshot"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validCreateRequest()
			test.mutate(&request)
			_, _, err := NewService(&repositoryStub{}).Create(
				context.Background(),
				testActor(testOwnerA),
				request,
			)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Create error = %v", err)
			}
		})
	}
}

func TestServiceReevaluateAndGetBindTrustedOwner(t *testing.T) {
	repository := &repositoryStub{evaluation: validEvaluation()}
	service := NewService(repository)
	request := ReevaluateRequest{
		Channels:         []Channel{ChannelScene},
		SceneStrategyRef: "interview/v2",
		PipelineVersion:  "pipeline/v2",
		ClientRequestID:  "trace-2",
	}
	_, _, err := service.Reevaluate(
		context.Background(),
		testActor(testOwnerA),
		testEvalID,
		request,
	)
	if err != nil {
		t.Fatalf("Reevaluate: %v", err)
	}
	if got := repository.reevaluateCommands[0]; got.OwnerUserID != testOwnerA ||
		got.EvaluationID != testEvalID ||
		got.Config.ClientRequestID != "trace-2" {
		t.Fatalf("Reevaluate command = %#v", got)
	}
	_, err = service.Get(
		context.Background(),
		testActor(testOwnerB),
		testEvalID,
	)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if repository.getOwner != testOwnerB {
		t.Fatalf("Get owner = %q", repository.getOwner)
	}
}

func TestChannelKeysAreRevisionChannelAndStrategyScoped(t *testing.T) {
	var root [32]byte
	root[0] = 1
	first := deriveChannelKey(root, 1, ChannelScene, "scene/v1")
	tests := [][32]byte{
		deriveChannelKey(root, 2, ChannelScene, "scene/v1"),
		deriveChannelKey(root, 1, ChannelCore4D, "scene/v1"),
		deriveChannelKey(root, 1, ChannelScene, "scene/v2"),
	}
	for index, candidate := range tests {
		if candidate == first {
			t.Fatalf("channel key variant %d collided", index)
		}
	}
}

type repositoryStub struct {
	evaluation         Evaluation
	ensureCommands     []EnsureCommand
	reevaluateCommands []ReevaluateCommand
	getOwner           string
}

func (r *repositoryStub) Ensure(
	_ context.Context,
	command EnsureCommand,
) (Evaluation, bool, error) {
	r.ensureCommands = append(r.ensureCommands, command)
	return r.evaluation, false, nil
}

func (r *repositoryStub) Reevaluate(
	_ context.Context,
	command ReevaluateCommand,
) (Evaluation, bool, error) {
	r.reevaluateCommands = append(r.reevaluateCommands, command)
	return r.evaluation, false, nil
}

func (r *repositoryStub) Get(
	_ context.Context,
	ownerUserID string,
	_ string,
) (Evaluation, error) {
	r.getOwner = ownerUserID
	return r.evaluation, nil
}

func validCreateRequest() CreateRequest {
	return CreateRequest{
		PracticeSessionID: "session-1",
		InputSnapshotID:   "snapshot-1",
		InputRevision:     1,
		Scope:             ScopeSession,
		SceneType:         SceneInterview,
		Channels:          []Channel{ChannelScene},
		SceneStrategyRef:  "interview/v1",
		PipelineVersion:   "pipeline/v1",
		ClientRequestID:   "trace-1",
	}
}

func testActor(owner string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    owner,
		SessionID: "50000000-0000-4000-8000-000000000005",
	}
}

func validEvaluation() Evaluation {
	now := time.Now().UTC()
	return Evaluation{
		ID:                testEvalID,
		OwnerUserID:       testOwnerA,
		PracticeSessionID: "session-1",
		InputSnapshotID:   "snapshot-1",
		InputRevision:     1,
		Scope:             ScopeSession,
		SceneType:         SceneInterview,
		CreatedAt:         now,
		Revision: Revision{
			ID:               testRevID,
			EvaluationID:     testEvalID,
			OwnerUserID:      testOwnerA,
			Number:           1,
			Channels:         []Channel{ChannelScene},
			SceneStrategyRef: "interview/v1",
			PipelineVersion:  "pipeline/v1",
			SchemaVersion:    SchemaVersion,
			Status:           StatusQueued,
			ClientRequestID:  "trace-1",
			CreatedAt:        now,
			UpdatedAt:        now,
		},
	}
}
