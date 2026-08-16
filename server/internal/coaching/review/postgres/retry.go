package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/jackc/pgx/v5"
)

type transactionStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type retrySourceReader interface {
	GetRetrySourceInTx(context.Context, pgx.Tx, string, string) (evaluation.RetrySource, error)
}

// retryTurnWriter is owned by this same-database integration adapter. Practice
// exposes only its command value; transaction plumbing does not enter either
// domain package.
type retryTurnWriter interface {
	CreateRetryTurn(
		context.Context,
		pgx.Tx,
		practice.CreateRetryTurnCommand,
	) (practice.Turn, bool, error)
}

type Service struct {
	transactions transactionStarter
	sources      retrySourceReader
	turns        retryTurnWriter
}

func New(
	transactions transactionStarter,
	sources retrySourceReader,
	turns retryTurnWriter,
) (*Service, error) {
	if transactions == nil || sources == nil || turns == nil {
		return nil, review.ErrInvalidRequest
	}
	return &Service{transactions: transactions, sources: sources, turns: turns}, nil
}

func (service *Service) CreateTurn(
	ctx context.Context,
	userID string,
	feedbackItemID string,
	clientRequestID string,
) (practice.Turn, bool, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if service == nil || service.transactions == nil || service.sources == nil ||
		service.turns == nil || ctx == nil || userID == "" ||
		feedbackItemID == "" || clientRequestID == "" {
		return practice.Turn{}, false, review.ErrInvalidRequest
	}
	tx, err := service.transactions.Begin(ctx)
	if err != nil {
		return practice.Turn{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	source, err := service.sources.GetRetrySourceInTx(ctx, tx, userID, feedbackItemID)
	if err != nil {
		return practice.Turn{}, false, err
	}
	if source.Item.RepracticeMode != "SAME_QUESTION" {
		return practice.Turn{}, false, review.ErrRetryUnavailable
	}
	var snapshot evaluation.SpeechInputSnapshot
	if evaluation.DecodeStrict(source.Evaluation.InputSnapshot, &snapshot) != nil ||
		!snapshot.Valid(evaluation.KindPracticeTurnFeedback) ||
		snapshot.EvidenceRefID != source.Evaluation.SourceID {
		return practice.Turn{}, false, review.ErrRetryUnavailable
	}
	turn, replayed, err := service.turns.CreateRetryTurn(
		ctx,
		tx,
		practice.CreateRetryTurnCommand{
			UserID:          userID,
			SessionID:       source.Evaluation.ContextID,
			OriginalTurnID:  source.Evaluation.SourceID,
			QuestionID:      snapshot.QuestionID,
			ClientRequestID: scopedRequestID(feedbackItemID, clientRequestID),
		},
	)
	if err != nil {
		return practice.Turn{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return practice.Turn{}, false, err
	}
	return turn, replayed, nil
}

func scopedRequestID(feedbackItemID string, clientRequestID string) string {
	digest := sha256.Sum256([]byte(
		"review-feedback-retry/v1\x00" + feedbackItemID + "\x00" + clientRequestID,
	))
	return "review-feedback-retry-v1-" + hex.EncodeToString(digest[:])
}

var _ review.RetryTurnCreator = (*Service)(nil)
