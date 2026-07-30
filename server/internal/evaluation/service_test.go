package evaluation

import (
	"context"
	"crypto/sha256"
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

var testSnapshotSourceManifestHash = func() [sha256.Size]byte {
	hash, err := evidenceSourceManifestHash(
		evidenceSnapshotPayloadForMetadata(
			"snapshot_provisional",
			"session-1",
		),
	)
	if err != nil {
		panic(err)
	}
	return hash
}()

var testSnapshotID = deriveEvidenceSnapshotID(
	testOwnerA,
	"session-1",
	ScopeSession,
	testSnapshotSourceManifestHash,
)

func TestServiceCreateCanonicalizesAndDerivesServerIdentity(t *testing.T) {
	repository := &repositoryStub{evaluation: validEvaluation()}
	reader := &evidenceSnapshotReaderStub{}
	service := NewService(repository, reader)
	request := validCreateRequest()
	request.Channels = []Channel{ChannelScene, ChannelCore4D}
	request.Core4DStrategyRef = "core4d/v1"

	_, _, err := service.Create(
		testActorContext(testOwnerA),
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
		testActorContext(testOwnerA),
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

	ownerBSnapshot := validEvidenceSnapshot(testOwnerB)
	reader.snapshot = ownerBSnapshot
	request.InputSnapshotID = ownerBSnapshot.ID
	_, _, err = service.Create(
		testActorContext(testOwnerB),
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

func TestServiceCreateRejectsActorOutsideAuthenticatedContext(t *testing.T) {
	service := NewService(
		&repositoryStub{},
		&evidenceSnapshotReaderStub{},
	)
	for _, ctx := range []context.Context{
		context.Background(),
		testActorContext(testOwnerB),
	} {
		_, _, err := service.Create(
			ctx,
			testActor(testOwnerA),
			validCreateRequest(),
		)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Create error = %v", err)
		}
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
			_, _, err := NewService(
				&repositoryStub{},
				&evidenceSnapshotReaderStub{},
			).Create(
				testActorContext(testOwnerA),
				testActor(testOwnerA),
				request,
			)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Create error = %v", err)
			}
		})
	}
}

func TestServiceCreateRejectsUntrustedEvidenceBeforeWriting(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*EvidenceSnapshot)
		readerErr error
		wantErr   error
	}{
		{
			name:      "snapshot missing",
			readerErr: ErrNotFound,
			wantErr:   ErrNotFound,
		},
		{
			name: "snapshot id mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.ID = "snapshot_other"
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "owner mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.OwnerUserID = testOwnerB
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "session mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.PracticeSessionID = "session-other"
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "revision mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.InputRevision = 2
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "scope mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.Scope = ScopeTurn
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "scene mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.SceneType = SceneIELTSSpeaking
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "payload hash mismatch",
			mutate: func(snapshot *EvidenceSnapshot) {
				snapshot.SnapshotHash[0] ^= 0xff
			},
			wantErr: ErrInvalidRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validEvidenceSnapshot(testOwnerA)
			if test.mutate != nil {
				test.mutate(&snapshot)
			}
			reader := &evidenceSnapshotReaderStub{
				snapshot: snapshot,
				err:      test.readerErr,
			}
			repository := &repositoryStub{}
			_, _, err := NewService(repository, reader).Create(
				testActorContext(testOwnerA),
				testActor(testOwnerA),
				validCreateRequest(),
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Create error = %v, want %v", err, test.wantErr)
			}
			if len(repository.ensureCommands) != 0 {
				t.Fatalf(
					"ledger Ensure called %d times",
					len(repository.ensureCommands),
				)
			}
		})
	}
}

func TestEvidenceSnapshotServiceFreezesOnlyTrustedComposition(t *testing.T) {
	actor := testActor(testOwnerA)
	command := validEvidenceCommand(
		testOwnerA,
		"session-1",
		ScopeSession,
		SceneInterview,
	)
	composer := &evidenceSnapshotComposerStub{command: command}
	repository := &evidenceSnapshotRepositoryStub{
		snapshot: evidenceSnapshotFromCommand(command),
	}
	service := NewEvidenceSnapshotService(composer, repository)
	ctx := requestcontext.WithActor(context.Background(), actor)

	snapshot, replayed, err := service.Freeze(
		ctx,
		actor,
		"session-1",
		ScopeSession,
		SceneInterview,
	)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if replayed || snapshot.ID != command.SnapshotID ||
		len(repository.ensureCommands) != 1 {
		t.Fatalf("Freeze result = %#v, replayed = %v", snapshot, replayed)
	}

	untrusted := testActor(testOwnerB)
	if _, _, err := service.Freeze(
		ctx,
		untrusted,
		"session-1",
		ScopeSession,
		SceneInterview,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("untrusted Freeze error = %v", err)
	}
	if len(repository.ensureCommands) != 1 {
		t.Fatal("untrusted Freeze reached the repository")
	}

	composer.command.OwnerUserID = testOwnerB
	if _, _, err := service.Freeze(
		ctx,
		actor,
		"session-1",
		ScopeSession,
		SceneInterview,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("spoofed composition error = %v", err)
	}
	if len(repository.ensureCommands) != 1 {
		t.Fatal("spoofed composition reached the repository")
	}
}

func TestServiceReevaluateAndGetBindTrustedOwner(t *testing.T) {
	repository := &repositoryStub{evaluation: validEvaluation()}
	service := NewService(repository, &evidenceSnapshotReaderStub{})
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

func TestServiceReevaluateDomainSeparatesCreateAndIgnoresTraceAndOwner(t *testing.T) {
	repository := &repositoryStub{evaluation: validEvaluation()}
	service := NewService(repository, &evidenceSnapshotReaderStub{})
	createRequest := validCreateRequest()
	if _, _, err := service.Create(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		createRequest,
	); err != nil {
		t.Fatalf("Create: %v", err)
	}
	request := ReevaluateRequest{
		Channels:          createRequest.Channels,
		SceneStrategyRef:  createRequest.SceneStrategyRef,
		Core4DStrategyRef: createRequest.Core4DStrategyRef,
		PipelineVersion:   createRequest.PipelineVersion,
		ClientRequestID:   "reevaluate-first-trace",
	}
	if _, _, err := service.Reevaluate(
		context.Background(),
		testActor(testOwnerA),
		testEvalID,
		request,
	); err != nil {
		t.Fatalf("Reevaluate: %v", err)
	}
	createFingerprint := repository.ensureCommands[0].RevisionFingerprint
	first := repository.reevaluateCommands[0]
	if first.RevisionFingerprint == createFingerprint {
		t.Fatal("Reevaluate fingerprint collided with Create fingerprint")
	}

	request.ClientRequestID = "reevaluate-retry-trace"
	if _, _, err := service.Reevaluate(
		context.Background(),
		testActor(testOwnerA),
		testEvalID,
		request,
	); err != nil {
		t.Fatalf("retry Reevaluate: %v", err)
	}
	retry := repository.reevaluateCommands[1]
	if retry.RevisionFingerprint != first.RevisionFingerprint {
		t.Fatal("client request ID changed Reevaluate fingerprint")
	}

	if _, _, err := service.Reevaluate(
		context.Background(),
		testActor(testOwnerB),
		testEvalID,
		request,
	); err != nil {
		t.Fatalf("other-owner Reevaluate: %v", err)
	}
	otherOwner := repository.reevaluateCommands[2]
	if otherOwner.OwnerUserID != testOwnerB {
		t.Fatalf("other-owner command = %#v", otherOwner)
	}
	if otherOwner.RevisionFingerprint != first.RevisionFingerprint {
		t.Fatal("owner changed Reevaluate revision intent fingerprint")
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

type evidenceSnapshotReaderStub struct {
	snapshot EvidenceSnapshot
	err      error
	getCalls int
}

func (r *evidenceSnapshotReaderStub) GetEvidenceSnapshot(
	_ context.Context,
	ownerUserID string,
	_ string,
) (EvidenceSnapshot, error) {
	r.getCalls++
	if r.err != nil {
		return EvidenceSnapshot{}, r.err
	}
	if r.snapshot.ID != "" {
		return r.snapshot, nil
	}
	return validEvidenceSnapshot(ownerUserID), nil
}

type evidenceSnapshotComposerStub struct {
	command EnsureEvidenceSnapshotCommand
	err     error
}

func (c *evidenceSnapshotComposerStub) Compose(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	_ Scope,
	_ SceneType,
) (EnsureEvidenceSnapshotCommand, error) {
	return c.command, c.err
}

type evidenceSnapshotRepositoryStub struct {
	snapshot       EvidenceSnapshot
	replayed       bool
	err            error
	ensureCommands []EnsureEvidenceSnapshotCommand
}

func (r *evidenceSnapshotRepositoryStub) EnsureEvidenceSnapshot(
	_ context.Context,
	command EnsureEvidenceSnapshotCommand,
) (EvidenceSnapshot, bool, error) {
	r.ensureCommands = append(r.ensureCommands, command)
	return r.snapshot, r.replayed, r.err
}

func (r *evidenceSnapshotRepositoryStub) GetEvidenceSnapshot(
	_ context.Context,
	_ string,
	_ string,
) (EvidenceSnapshot, error) {
	return r.snapshot, r.err
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
		InputSnapshotID:   testSnapshotID,
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

func testActorContext(owner string) context.Context {
	return requestcontext.WithActor(
		context.Background(),
		testActor(owner),
	)
}

func validEvaluation() Evaluation {
	now := time.Now().UTC()
	return Evaluation{
		ID:                testEvalID,
		OwnerUserID:       testOwnerA,
		PracticeSessionID: "session-1",
		InputSnapshotID:   testSnapshotID,
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

func validEvidenceSnapshot(ownerUserID string) EvidenceSnapshot {
	sourceManifestHash := testSnapshotSourceManifestHash
	snapshotID := deriveEvidenceSnapshotID(
		ownerUserID,
		"session-1",
		ScopeSession,
		sourceManifestHash,
	)
	payload, err := canonicalEvidencePayload(
		evidenceSnapshotPayloadForMetadata(snapshotID, "session-1"),
	)
	if err != nil {
		panic(err)
	}
	return EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        ownerUserID,
		PracticeSessionID:  "session-1",
		InputRevision:      1,
		Scope:              ScopeSession,
		SceneType:          SceneInterview,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(payload),
		Payload:            payload,
		CreatedAt:          time.Now().UTC(),
	}
}

func validEvidenceCommand(
	ownerUserID string,
	practiceSessionID string,
	scope Scope,
	sceneType SceneType,
) EnsureEvidenceSnapshotCommand {
	provisionalPayload := evidenceSnapshotPayloadForMetadata(
		"snapshot_provisional",
		practiceSessionID,
	)
	sourceManifestHash, err := evidenceSourceManifestHash(provisionalPayload)
	if err != nil {
		panic(err)
	}
	snapshotID := deriveEvidenceSnapshotID(
		ownerUserID,
		practiceSessionID,
		scope,
		sourceManifestHash,
	)
	return EnsureEvidenceSnapshotCommand{
		SnapshotID:         snapshotID,
		OwnerUserID:        ownerUserID,
		PracticeSessionID:  practiceSessionID,
		Scope:              scope,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		CanonicalPayload: evidenceSnapshotPayloadForMetadata(
			snapshotID,
			practiceSessionID,
		),
	}
}

func evidenceSnapshotFromCommand(
	command EnsureEvidenceSnapshotCommand,
) EvidenceSnapshot {
	payload, err := canonicalEvidencePayload(command.CanonicalPayload)
	if err != nil {
		panic(err)
	}
	return EvidenceSnapshot{
		ID:                 command.SnapshotID,
		OwnerUserID:        command.OwnerUserID,
		PracticeSessionID:  command.PracticeSessionID,
		InputRevision:      1,
		Scope:              command.Scope,
		SceneType:          command.SceneType,
		SourceManifestHash: command.SourceManifestHash,
		SnapshotHash:       sha256.Sum256(payload),
		Payload:            payload,
		CreatedAt:          time.Now().UTC(),
	}
}
