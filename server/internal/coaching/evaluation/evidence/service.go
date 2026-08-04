package evidence

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type EvidenceSnapshotComposer interface {
	Compose(
		ctx context.Context,
		actor requestcontext.Actor,
		practiceSessionID string,
		scope evaluation.Scope,
		sceneType evaluation.SceneType,
	) (EnsureEvidenceSnapshotCommand, error)
	ComposeCompleted(
		ctx context.Context,
		ownerUserID string,
		practiceSessionID string,
		scope evaluation.Scope,
		sceneType evaluation.SceneType,
	) (EnsureEvidenceSnapshotCommand, error)
}

type EvidenceSnapshotReader interface {
	GetEvidenceSnapshot(
		ctx context.Context,
		ownerUserID string,
		snapshotID string,
	) (EvidenceSnapshot, error)
}

type EvidenceSnapshotService struct {
	composer   EvidenceSnapshotComposer
	repository EvidenceSnapshotRepository
}

func NewEvidenceSnapshotService(
	composer EvidenceSnapshotComposer,
	repository EvidenceSnapshotRepository,
) *EvidenceSnapshotService {
	return &EvidenceSnapshotService{
		composer:   composer,
		repository: repository,
	}
}

func (s *EvidenceSnapshotService) Freeze(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
	scope evaluation.Scope,
	sceneType evaluation.SceneType,
) (EvidenceSnapshot, bool, error) {
	if s == nil || s.composer == nil || s.repository == nil ||
		ctx == nil || !validActor(actor) {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	command, err := s.composer.Compose(
		ctx,
		actor,
		practiceSessionID,
		scope,
		sceneType,
	)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	command, err = normalizeEvidenceSnapshotCommand(command)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	if command.OwnerUserID != actor.UserID ||
		command.PracticeSessionID != practiceSessionID ||
		command.Scope != scope ||
		command.SceneType != sceneType {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	snapshot, replayed, err := s.repository.EnsureEvidenceSnapshot(ctx, command)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	if !snapshot.Valid() ||
		snapshot.ID != command.SnapshotID ||
		snapshot.OwnerUserID != command.OwnerUserID ||
		snapshot.PracticeSessionID != command.PracticeSessionID ||
		snapshot.Scope != command.Scope ||
		snapshot.SceneType != command.SceneType ||
		snapshot.SourceManifestHash != command.SourceManifestHash {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	return snapshot, replayed, nil
}

func (s *EvidenceSnapshotService) FreezeCompleted(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
	scope evaluation.Scope,
	sceneType evaluation.SceneType,
) (EvidenceSnapshot, bool, error) {
	if s == nil || s.composer == nil || s.repository == nil ||
		ctx == nil || !validUUID(ownerUserID) {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	command, err := s.composer.ComposeCompleted(
		ctx,
		ownerUserID,
		practiceSessionID,
		scope,
		sceneType,
	)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	command, err = normalizeEvidenceSnapshotCommand(command)
	if err != nil || command.OwnerUserID != ownerUserID ||
		command.PracticeSessionID != practiceSessionID ||
		command.Scope != scope || command.SceneType != sceneType {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	snapshot, replayed, err := s.repository.EnsureEvidenceSnapshot(ctx, command)
	if err != nil {
		return EvidenceSnapshot{}, false, err
	}
	if !snapshot.Valid() || snapshot.ID != command.SnapshotID ||
		snapshot.OwnerUserID != command.OwnerUserID ||
		snapshot.PracticeSessionID != command.PracticeSessionID ||
		snapshot.Scope != command.Scope ||
		snapshot.SceneType != command.SceneType ||
		snapshot.SourceManifestHash != command.SourceManifestHash {
		return EvidenceSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	return snapshot, replayed, nil
}
