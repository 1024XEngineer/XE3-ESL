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

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	request CreateRequest,
) (Evaluation, bool, error) {
	if s == nil || s.repository == nil || ctx == nil || !validActor(actor) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	input, err := normalizeCreate(request)
	if err != nil {
		return Evaluation{}, false, err
	}
	rootFingerprint, err := digest(struct {
		OwnerUserID string      `json:"owner_user_id"`
		Input       createInput `json:"input"`
	}{
		OwnerUserID: actor.UserID,
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
		OwnerUserID:         actor.UserID,
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
	revisionFingerprint, err := digest(config)
	if err != nil {
		return Evaluation{}, false, err
	}
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
