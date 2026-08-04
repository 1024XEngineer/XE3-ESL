package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type EnsureCommand struct {
	OwnerUserID         string
	RootIdempotencyKey  [sha256.Size]byte
	RootFingerprint     [sha256.Size]byte
	RevisionFingerprint [sha256.Size]byte
	Input               createInput
}

type ReevaluateCommand struct {
	OwnerUserID         string
	EvaluationID        string
	RevisionFingerprint [sha256.Size]byte
	Config              revisionConfig
}

type Repository interface {
	Ensure(
		ctx context.Context,
		command EnsureCommand,
	) (Evaluation, bool, error)
	Reevaluate(
		ctx context.Context,
		command ReevaluateCommand,
	) (Evaluation, bool, error)
	Get(
		ctx context.Context,
		ownerUserID string,
		evaluationID string,
	) (Evaluation, error)
}

type EvidenceSnapshotComposer interface {
	Compose(
		ctx context.Context,
		actor requestcontext.Actor,
		practiceSessionID string,
		scope Scope,
		sceneType SceneType,
	) (EnsureEvidenceSnapshotCommand, error)
	ComposeCompleted(
		ctx context.Context,
		ownerUserID string,
		practiceSessionID string,
		scope Scope,
		sceneType SceneType,
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
	scope Scope,
	sceneType SceneType,
) (EvidenceSnapshot, bool, error) {
	if s == nil || s.composer == nil || s.repository == nil ||
		ctx == nil || !validActor(actor) {
		return EvidenceSnapshot{}, false, ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return EvidenceSnapshot{}, false, ErrInvalidRequest
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
		return EvidenceSnapshot{}, false, ErrInvalidRequest
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
		return EvidenceSnapshot{}, false, ErrInvalidRequest
	}
	return snapshot, replayed, nil
}

func (s *EvidenceSnapshotService) FreezeCompleted(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
	scope Scope,
	sceneType SceneType,
) (EvidenceSnapshot, bool, error) {
	if s == nil || s.composer == nil || s.repository == nil ||
		ctx == nil || !validUUID(ownerUserID) {
		return EvidenceSnapshot{}, false, ErrInvalidRequest
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
		return EvidenceSnapshot{}, false, ErrInvalidRequest
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
		return EvidenceSnapshot{}, false, ErrInvalidRequest
	}
	return snapshot, replayed, nil
}

type Service struct {
	repository        Repository
	evidenceSnapshots EvidenceSnapshotReader
}

func NewService(
	repository Repository,
	evidenceSnapshots EvidenceSnapshotReader,
) *Service {
	return &Service{
		repository:        repository,
		evidenceSnapshots: evidenceSnapshots,
	}
}

func (s *Service) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	request CreateRequest,
) (Evaluation, bool, error) {
	if s == nil || s.repository == nil || s.evidenceSnapshots == nil ||
		ctx == nil || !validActor(actor) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return Evaluation{}, false, ErrInvalidRequest
	}
	return s.create(ctx, actor.UserID, request)
}

func (s *Service) CreateCompleted(
	ctx context.Context,
	ownerUserID string,
	request CreateRequest,
) (Evaluation, bool, error) {
	if s == nil || s.repository == nil || s.evidenceSnapshots == nil ||
		ctx == nil || !validUUID(ownerUserID) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	return s.create(ctx, ownerUserID, request)
}

func (s *Service) create(
	ctx context.Context,
	ownerUserID string,
	request CreateRequest,
) (Evaluation, bool, error) {
	input, err := normalizeCreate(request)
	if err != nil {
		return Evaluation{}, false, err
	}
	snapshot, err := s.evidenceSnapshots.GetEvidenceSnapshot(
		ctx,
		ownerUserID,
		input.InputSnapshotID,
	)
	if err != nil {
		return Evaluation{}, false, err
	}
	if !snapshot.Valid() ||
		snapshot.ID != input.InputSnapshotID ||
		snapshot.OwnerUserID != ownerUserID ||
		snapshot.PracticeSessionID != input.PracticeSessionID ||
		snapshot.InputRevision != input.InputRevision ||
		snapshot.Scope != input.Scope ||
		snapshot.SceneType != input.SceneType {
		return Evaluation{}, false, ErrInvalidRequest
	}
	rootFingerprint, err := digest(struct {
		OwnerUserID string      `json:"owner_user_id"`
		Input       createInput `json:"input"`
	}{
		OwnerUserID: ownerUserID,
		Input:       input,
	})
	if err != nil {
		return Evaluation{}, false, err
	}
	rootKey := sha256.Sum256(append(
		[]byte("evaluation-root:v1\x00"),
		rootFingerprint[:]...,
	))
	revisionFingerprint, err := digest(input.Config)
	if err != nil {
		return Evaluation{}, false, err
	}
	return s.repository.Ensure(ctx, EnsureCommand{
		OwnerUserID:         ownerUserID,
		RootIdempotencyKey:  rootKey,
		RootFingerprint:     rootFingerprint,
		RevisionFingerprint: revisionFingerprint,
		Input:               input,
	})
}

func (s *Service) Reevaluate(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
	request ReevaluateRequest,
) (Evaluation, bool, error) {
	if s == nil || s.repository == nil || ctx == nil ||
		!validActor(actor) || !validUUID(evaluationID) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	config, err := normalizeReevaluation(request)
	if err != nil {
		return Evaluation{}, false, err
	}
	configFingerprint, err := digest(config)
	if err != nil {
		return Evaluation{}, false, err
	}
	revisionFingerprint := sha256.Sum256(append(
		[]byte("evaluation-revision:reevaluate:v1\x00"),
		configFingerprint[:]...,
	))
	return s.repository.Reevaluate(ctx, ReevaluateCommand{
		OwnerUserID:         actor.UserID,
		EvaluationID:        evaluationID,
		RevisionFingerprint: revisionFingerprint,
		Config:              config,
	})
}

func (s *Service) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
) (Evaluation, error) {
	if s == nil || s.repository == nil || ctx == nil ||
		!validActor(actor) || !validUUID(evaluationID) {
		return Evaluation{}, ErrInvalidRequest
	}
	return s.repository.Get(ctx, actor.UserID, evaluationID)
}

func digest(value any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [sha256.Size]byte{}, errors.Join(ErrInvalidRequest, err)
	}
	return sha256.Sum256(encoded), nil
}
