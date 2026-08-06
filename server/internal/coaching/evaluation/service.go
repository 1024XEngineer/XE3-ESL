package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type EnsureCommand struct {
	OwnerUserID         string
	RootIdempotencyKey  [sha256.Size]byte
	RootFingerprint     [sha256.Size]byte
	RevisionFingerprint [sha256.Size]byte
	Input               CreateInput
}

func (command EnsureCommand) Valid() bool {
	return validUUID(command.OwnerUserID) && command.Input.Valid()
}

type ReevaluateCommand struct {
	OwnerUserID         string
	EvaluationID        string
	RevisionFingerprint [sha256.Size]byte
	Config              RevisionConfig
}

func (command ReevaluateCommand) Valid() bool {
	return validUUID(command.OwnerUserID) &&
		validUUID(command.EvaluationID) &&
		command.Config.Valid()
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

type EvidenceReference struct {
	ID                string
	OwnerUserID       string
	PracticeSessionID string
	InputRevision     int
	Scope             Scope
	SceneType         SceneType
}

func (reference EvidenceReference) Valid() bool {
	return validIdentifier(reference.ID) &&
		validUUID(reference.OwnerUserID) &&
		validIdentifier(reference.PracticeSessionID) &&
		reference.InputRevision > 0 &&
		validScope(reference.Scope) &&
		validSceneType(reference.SceneType)
}

type EvidenceReader interface {
	GetEvaluationEvidence(
		context.Context,
		string,
		string,
	) (EvidenceReference, error)
}

type Service struct {
	repository        Repository
	evidenceSnapshots EvidenceReader
}

func NewService(
	repository Repository,
	evidenceSnapshots EvidenceReader,
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
	snapshot, err := s.evidenceSnapshots.GetEvaluationEvidence(
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
		Input       CreateInput `json:"input"`
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

func DeriveChannelKey(
	rootKey [sha256.Size]byte,
	revision int,
	channel Channel,
	strategyRef string,
) [sha256.Size]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("evaluation-channel:v1\x00"))
	_, _ = hasher.Write(rootKey[:])
	var revisionBytes [8]byte
	binary.BigEndian.PutUint64(revisionBytes[:], uint64(revision))
	_, _ = hasher.Write(revisionBytes[:])
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(channel))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(strategyRef))
	var result [sha256.Size]byte
	copy(result[:], hasher.Sum(nil))
	return result
}
