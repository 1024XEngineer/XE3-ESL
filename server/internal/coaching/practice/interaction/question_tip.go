package interaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

const (
	questionTipLease      = 45 * time.Second
	questionTipRenewEvery = questionTipLease / 3
	questionTipRenewWait  = 2 * time.Second
	questionTipPoll       = 75 * time.Millisecond
	questionTipMaxRunes   = 2000
	questionTipMaxHistory = 6
)

type QuestionTipResult struct {
	ID          string
	SessionID   string
	QuestionID  string
	Content     string
	Translation string
	CreatedAt   time.Time
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
	store              QuestionTipStore
	generator          AnswerTipGenerator
	translator         sharedtranslation.Translator
	leaseDuration      time.Duration
	leaseRenewInterval time.Duration
	leaseRenewTimeout  time.Duration
}

func NewQuestionTipService(
	store QuestionTipStore,
	generator AnswerTipGenerator,
	translator sharedtranslation.Translator,
) (*QuestionTipService, error) {
	if store == nil || generator == nil || translator == nil {
		return nil, ErrInvalidRequest
	}
	return &QuestionTipService{
		store: store, generator: generator, translator: translator,
		leaseDuration: questionTipLease, leaseRenewInterval: questionTipRenewEvery,
		leaseRenewTimeout: questionTipRenewWait,
	}, nil
}

func (service *QuestionTipService) EnsureQuestionTip(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
	question practice.Question,
	history []TurnExchange,
	idempotencyKey string,
) (QuestionTipResult, error) {
	if service == nil || service.store == nil || service.generator == nil || service.translator == nil ||
		service.leaseDuration <= 0 || service.leaseRenewInterval <= 0 ||
		service.leaseRenewInterval >= service.leaseDuration || service.leaseRenewTimeout <= 0 ||
		!actor.Valid() || session.ID == "" || !session.QuestionTipsAllowed ||
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
				LeaseDuration:  service.leaseDuration,
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
	prepared, err := service.prepareQuestionTipWithLease(
		ctx,
		actor,
		tip,
		func(workCtx context.Context) (preparedQuestionTip, error) {
			return service.prepareQuestionTip(workCtx, tip, session, question, history)
		},
	)
	if err != nil {
		var renewalError *questionTipLeaseRenewalError
		if !errors.As(err, &renewalError) {
			service.failQuestionTip(ctx, actor, tip)
		}
		return QuestionTipResult{}, err
	}
	completed, err := service.store.CompleteQuestionTip(
		ctx,
		actor,
		CompleteQuestionTipCommand{
			TipID:              tip.ID,
			FencingToken:       tip.FencingToken,
			DeletionGeneration: tip.DeletionGeneration,
			Content:            prepared.content,
			Translation:        prepared.translation,
			Provider:           prepared.provider,
			Model:              prepared.model,
			ProviderRequestID:  prepared.providerRequestID,
		},
	)
	if err != nil {
		return QuestionTipResult{}, mapPersistenceError(err)
	}
	return mapQuestionTip(completed)
}

type preparedQuestionTip struct {
	content           string
	translation       string
	provider          string
	model             string
	providerRequestID string
}

func (service *QuestionTipService) prepareQuestionTip(
	ctx context.Context,
	tip QuestionTip,
	session Session,
	question practice.Question,
	history []TurnExchange,
) (preparedQuestionTip, error) {
	if answer, ok := preparedIELTSAnswer(
		session.IELTSAssignment,
		question.Sequence,
		question.Content,
	); ok {
		return service.translateQuestionTip(ctx, answer, "practice-plan", "prepared-answer-v1", tip.ID)
	}
	result, err := service.generator.GenerateAnswerTip(
		ctx,
		questionTipRequest(session, question, history),
	)
	content := strings.TrimSpace(result.Content)
	if err != nil || content == "" ||
		utf8.RuneCountInString(content) > questionTipMaxRunes ||
		!validVoiceIdentifier(result.Provider) ||
		!modelid.Valid(result.Model) ||
		!validVoiceIdentifier(result.RequestID) {
		if err != nil {
			return preparedQuestionTip{}, err
		}
		return preparedQuestionTip{}, ErrInvalidContext
	}
	return service.translateQuestionTip(ctx, content, result.Provider, result.Model, result.RequestID)
}

func (service *QuestionTipService) translateQuestionTip(
	ctx context.Context,
	content, provider, model, providerRequestID string,
) (preparedQuestionTip, error) {
	translation, err := service.translator.Translate(ctx, sharedtranslation.Request{Text: content})
	translation = strings.TrimSpace(translation)
	if err != nil || translation == "" || utf8.RuneCountInString(translation) > questionTipMaxRunes {
		if err != nil {
			return preparedQuestionTip{}, err
		}
		return preparedQuestionTip{}, ErrInvalidContext
	}
	return preparedQuestionTip{
		content: content, translation: translation, provider: provider,
		model: model, providerRequestID: providerRequestID,
	}, nil
}

type questionTipLeaseRenewalError struct {
	cause error
}

func (err *questionTipLeaseRenewalError) Error() string {
	if err == nil || err.cause == nil {
		return "question tip lease renewal failed"
	}
	return err.cause.Error()
}

func (err *questionTipLeaseRenewalError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (service *QuestionTipService) prepareQuestionTipWithLease(
	ctx context.Context,
	actor Actor,
	tip QuestionTip,
	prepare func(context.Context) (preparedQuestionTip, error),
) (preparedQuestionTip, error) {
	workCtx, cancelWork := context.WithCancel(ctx)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go service.maintainQuestionTipLease(
		heartbeatCtx,
		cancelWork,
		heartbeatDone,
		actor,
		tip,
	)

	prepared, prepareErr := prepare(workCtx)
	cancelHeartbeat()
	heartbeatErr := <-heartbeatDone
	cancelWork()
	if heartbeatErr != nil {
		return preparedQuestionTip{}, heartbeatErr
	}
	return prepared, prepareErr
}

func (service *QuestionTipService) maintainQuestionTipLease(
	ctx context.Context,
	cancelWork context.CancelFunc,
	done chan<- error,
	actor Actor,
	tip QuestionTip,
) {
	ticker := time.NewTicker(service.leaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, service.leaseRenewTimeout)
			err := service.store.RenewQuestionTipLease(
				renewCtx,
				actor,
				RenewQuestionTipLeaseCommand{
					TipID: tip.ID, FencingToken: tip.FencingToken,
					DeletionGeneration: tip.DeletionGeneration,
					LeaseDuration:      service.leaseDuration,
				},
			)
			cancel()
			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				done <- nil
				return
			}
			cancelWork()
			done <- &questionTipLeaseRenewalError{cause: mapPersistenceError(err)}
			return
		}
	}
}

func (service *QuestionTipService) failQuestionTip(ctx context.Context, actor Actor, tip QuestionTip) {
	failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = service.store.FailQuestionTip(failureCtx, actor, FailQuestionTipCommand{
		TipID: tip.ID, FencingToken: tip.FencingToken,
		DeletionGeneration: tip.DeletionGeneration,
	})
}

func preparedIELTSAnswer(
	assignment *practice.IELTSAssignment,
	sequence int,
	question string,
) (string, bool) {
	if assignment == nil || sequence < 1 || strings.TrimSpace(question) == "" {
		return "", false
	}
	remaining := sequence
	for _, part := range assignment.Parts {
		if remaining > len(part.TurnBlueprints) {
			remaining -= len(part.TurnBlueprints)
			continue
		}
		if part.TurnBlueprints[remaining-1] != question {
			return "", false
		}
		for _, answer := range part.PreparedAnswers {
			if answer.QuestionPosition == remaining {
				return answer.Answer, true
			}
		}
		return "", false
	}
	return "", false
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
		strings.TrimSpace(tip.Content) == "" || strings.TrimSpace(tip.Translation) == "" || tip.CreatedAt.IsZero() {
		return QuestionTipResult{}, ErrInvalidContext
	}
	return QuestionTipResult{
		ID:          tip.ID,
		SessionID:   tip.SessionID,
		QuestionID:  tip.QuestionID,
		Content:     tip.Content,
		Translation: tip.Translation,
		CreatedAt:   tip.CreatedAt,
	}, nil
}

var _ QuestionTipPort = (*QuestionTipService)(nil)
