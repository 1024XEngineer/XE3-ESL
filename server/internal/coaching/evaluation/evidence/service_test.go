package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestEvidenceSnapshotServiceFreezesOnlyTrustedComposition(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    testOwnerA,
		SessionID: "50000000-0000-4000-8000-000000000005",
	}
	command := validEvidenceSnapshotCommand()
	composer := &snapshotComposerStub{command: command}
	repository := &snapshotRepositoryStub{
		snapshot: snapshotFromCommand(command),
	}
	service := NewEvidenceSnapshotService(composer, repository)
	ctx := requestcontext.WithActor(context.Background(), actor)

	snapshot, replayed, err := service.Freeze(
		ctx,
		actor,
		command.PracticeSessionID,
		command.Scope,
		command.SceneType,
	)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if replayed || snapshot.ID != command.SnapshotID ||
		len(repository.ensureCommands) != 1 {
		t.Fatalf("Freeze result = %#v, replayed = %v", snapshot, replayed)
	}

	untrusted := actor
	untrusted.UserID = "20000000-0000-4000-8000-000000000002"
	if _, _, err := service.Freeze(
		ctx,
		untrusted,
		command.PracticeSessionID,
		command.Scope,
		command.SceneType,
	); !errors.Is(err, evaluation.ErrInvalidRequest) {
		t.Fatalf("untrusted Freeze error = %v", err)
	}
	if len(repository.ensureCommands) != 1 {
		t.Fatal("untrusted Freeze reached the repository")
	}

	composer.command.OwnerUserID = untrusted.UserID
	if _, _, err := service.Freeze(
		ctx,
		actor,
		command.PracticeSessionID,
		command.Scope,
		command.SceneType,
	); !errors.Is(err, evaluation.ErrInvalidRequest) {
		t.Fatalf("spoofed composition error = %v", err)
	}
	if len(repository.ensureCommands) != 1 {
		t.Fatal("spoofed composition reached the repository")
	}
}

type snapshotComposerStub struct {
	command EnsureEvidenceSnapshotCommand
	err     error
}

func (stub *snapshotComposerStub) Compose(
	context.Context,
	requestcontext.Actor,
	string,
	evaluation.Scope,
	evaluation.SceneType,
) (EnsureEvidenceSnapshotCommand, error) {
	return stub.command, stub.err
}

func (stub *snapshotComposerStub) ComposeCompleted(
	context.Context,
	string,
	string,
	evaluation.Scope,
	evaluation.SceneType,
) (EnsureEvidenceSnapshotCommand, error) {
	return stub.command, stub.err
}

type snapshotRepositoryStub struct {
	snapshot       EvidenceSnapshot
	replayed       bool
	err            error
	ensureCommands []EnsureEvidenceSnapshotCommand
}

func (stub *snapshotRepositoryStub) EnsureEvidenceSnapshot(
	_ context.Context,
	command EnsureEvidenceSnapshotCommand,
) (EvidenceSnapshot, bool, error) {
	stub.ensureCommands = append(stub.ensureCommands, command)
	return stub.snapshot, stub.replayed, stub.err
}

func (stub *snapshotRepositoryStub) GetEvidenceSnapshot(
	context.Context,
	string,
	string,
) (EvidenceSnapshot, error) {
	return stub.snapshot, stub.err
}

func snapshotFromCommand(command EnsureEvidenceSnapshotCommand) EvidenceSnapshot {
	payload, err := CanonicalPayload(command.CanonicalPayload)
	if err != nil {
		panic(err)
	}
	return EvidenceSnapshot{
		ID:                 command.SnapshotID,
		OwnerUserID:        command.OwnerUserID,
		PracticeSessionID:  command.PracticeSessionID,
		InputRevision:      2,
		Scope:              command.Scope,
		SceneType:          command.SceneType,
		SourceManifestHash: command.SourceManifestHash,
		SnapshotHash:       sha256.Sum256(payload),
		Payload:            payload,
		CreatedAt:          time.Now().UTC(),
	}
}
