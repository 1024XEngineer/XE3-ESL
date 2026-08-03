package voice

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	questionTipLease      = 45 * time.Second
	questionTipPoll       = 75 * time.Millisecond
	questionTipMaxRunes   = 800
	questionTipMaxHistory = 6
)

type QuestionTipResult struct {
	ID         string
	SessionID  string
	QuestionID string
	Content    string
	CreatedAt  time.Time
}

type QuestionTipPort interface {
	EnsureQuestionTip(
		context.Context,
		requestcontext.Actor,
		Session,
		practice.Question,
		[]TurnExchange,
		string,
	) (QuestionTipResult, error)
}

type QuestionTipService struct {
	store     QuestionTipStore
	generator AnswerTipGenerator
}

func NewQuestionTipService(
	store QuestionTipStore,
	generator AnswerTipGenerator,
) (*QuestionTipService, error) {
	if store == nil || generator == nil {
		return nil, ErrInvalidRequest
	}
	return &QuestionTipService{store: store, generator: generator}, nil
}

func (service *QuestionTipService) EnsureQuestionTip(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
	question practice.Question,
	history []TurnExchange,
	idempotencyKey string,
) (QuestionTipResult, error) {
	if service == nil || service.store == nil || service.generator == nil ||
		!actor.Valid() || session.ID == "" || session.SceneFamily != "INTERVIEW" ||
		question.ID == "" || question.SessionID != session.ID ||
		strings.TrimSpace(question.Content) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return QuestionTipResult{}, ErrInvalidRequest
	}
	storedActor := persistenceActor(actor)
	for {
		tip, err := service.store.ClaimQuestionTip(
			ctx,
			storedActor,
			ClaimQuestionTipCommand{
				SessionID:      session.ID,
				QuestionID:     question.ID,
				IdempotencyKey: idempotencyKey,
				LeaseDuration:  questionTipLease,
			},
		)
		if err != nil {
			return QuestionTipResult{}, mapPersistenceError(err)
		}
		if tip.Status == QuestionTipCompleted {
			return mapQuestionTip(tip)
		}
		if tip.LeaseAcquired {
			return service.generateQuestionTip(
				ctx,
				storedActor,
				tip,
				session,
				question,
				history,
			)
		}
		select {
		case <-ctx.Done():
			return QuestionTipResult{}, ctx.Err()
		case <-time.After(questionTipPoll):
		}
		persisted, err := service.store.GetQuestionTip(
			ctx,
			storedActor,
			session.ID,
			question.ID,
		)
		if err != nil {
			return QuestionTipResult{}, mapPersistenceError(err)
		}
		if persisted.Status == QuestionTipCompleted {
			return mapQuestionTip(persisted)
		}
	}
}

func (service *QuestionTipService) generateQuestionTip(
	ctx context.Context,
	actor Actor,
	tip QuestionTip,
	session Session,
	question practice.Question,
	history []TurnExchange,
) (QuestionTipResult, error) {
	result, err := service.generator.GenerateAnswerTip(
		ctx,
		questionTipRequest(session, question, history),
	)
	content := strings.TrimSpace(result.Content)
	if err != nil || content == "" || utf8.RuneCountInString(content) > questionTipMaxRunes {
		failureCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			2*time.Second,
		)
		defer cancel()
		_ = service.store.FailQuestionTip(
			failureCtx,
			actor,
			FailQuestionTipCommand{
				TipID:              tip.ID,
				FencingToken:       tip.FencingToken,
				DeletionGeneration: tip.DeletionGeneration,
			},
		)
		if err != nil {
			return QuestionTipResult{}, err
		}
		return QuestionTipResult{}, ErrInvalidContext
	}
	completed, err := service.store.CompleteQuestionTip(
		ctx,
		actor,
		CompleteQuestionTipCommand{
			TipID:              tip.ID,
			FencingToken:       tip.FencingToken,
			DeletionGeneration: tip.DeletionGeneration,
			Content:            content,
			Provider:           result.Provider,
			Model:              result.Model,
			ProviderRequestID:  result.RequestID,
		},
	)
	if err != nil {
		return QuestionTipResult{}, mapPersistenceError(err)
	}
	return mapQuestionTip(completed)
}

func questionTipRequest(
	session Session,
	question practice.Question,
	history []TurnExchange,
) AnswerTipGenerationRequest {
	var prior strings.Builder
	start := len(history) - questionTipMaxHistory
	if start < 0 {
		start = 0
	}
	for _, exchange := range history[start:] {
		fmt.Fprintf(
			&prior,
			"Interviewer: %s\nCandidate: %s\n",
			exchange.Question.Content,
			exchange.Turn.AnswerText,
		)
	}
	return AnswerTipGenerationRequest{
		SystemPrompt: `You help an English learner answer an interview question aloud. ` +
			`Return only one short natural English sample answer of 2 to 4 sentences, ` +
			`roughly 15 to 30 seconds when spoken. Use simple, speakable language. ` +
			`Use conversation facts only when explicitly provided. Never invent names, employers, ` +
			`metrics, credentials, or personal experiences; use safe generic phrasing instead. ` +
			`Do not add headings, coaching notes, placeholders, quotation marks, or markdown.`,
		UserPrompt: fmt.Sprintf(
			"Interview context: %s\nPractice goal: %s\nPrior conversation:\n%sCurrent question: %s",
			session.Prompt.PublicSceneBrief,
			session.Prompt.PracticeGoal,
			prior.String(),
			question.Content,
		),
	}
}

func mapQuestionTip(tip QuestionTip) (QuestionTipResult, error) {
	if tip.Status != QuestionTipCompleted || tip.ID == "" ||
		tip.SessionID == "" || tip.QuestionID == "" ||
		strings.TrimSpace(tip.Content) == "" || tip.CreatedAt.IsZero() {
		return QuestionTipResult{}, ErrInvalidContext
	}
	return QuestionTipResult{
		ID:         tip.ID,
		SessionID:  tip.SessionID,
		QuestionID: tip.QuestionID,
		Content:    tip.Content,
		CreatedAt:  tip.CreatedAt,
	}, nil
}

var _ QuestionTipPort = (*QuestionTipService)(nil)
