package interaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

func TestPreparedIELTSAnswerUsesFrozenQuestionPosition(t *testing.T) {
	assignment := &practice.IELTSAssignment{Parts: []practice.IELTSPart{
		{TurnBlueprints: []string{"one", "two"}, PreparedAnswers: []practice.IELTSPreparedAnswer{{QuestionPosition: 2, Answer: "my frozen answer", Personalized: true}}},
		{TurnBlueprints: []string{"three"}},
	}}

	answer, ok := preparedIELTSAnswer(assignment, 2, "two")
	if !ok || answer != "my frozen answer" {
		t.Fatalf("preparedIELTSAnswer() = %q, %v", answer, ok)
	}
	if _, ok := preparedIELTSAnswer(assignment, 1, "one"); ok {
		t.Fatal("unprepared question unexpectedly returned an answer")
	}
	if _, ok := preparedIELTSAnswer(assignment, 2, "follow-up"); ok {
		t.Fatal("a follow-up at the same sequence reused another question's answer")
	}
}

func TestQuestionTipPersistsEnglishAndSimplifiedChineseTogether(t *testing.T) {
	store := &questionTipStoreStub{}
	generator := &answerTipGeneratorStub{result: AnswerTipGenerationResult{
		RequestID: "tip-request-1",
		Provider:  "qianwen",
		Model:     "qwen-plus",
		Content:   "  I would explain the goal and my approach.  ",
	}}
	translator := &questionTipTranslatorStub{content: "  我会说明目标和我的方法。  "}
	service, err := NewQuestionTipService(store, generator, translator)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.EnsureQuestionTip(
		context.Background(),
		requestcontext.Actor{UserID: "user-1", SessionID: "auth-session-1"},
		Session{ID: "session-1", QuestionTipsAllowed: true},
		practice.Question{ID: "question-1", SessionID: "session-1", Content: "Tell me about your approach."},
		nil,
		"tip-operation-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if translator.request.Text != "I would explain the goal and my approach." {
		t.Fatalf("translation source=%q", translator.request.Text)
	}
	if store.completed.Content != translator.request.Text ||
		store.completed.Translation != "我会说明目标和我的方法。" {
		t.Fatalf("complete command=%#v", store.completed)
	}
	if result.Content != store.completed.Content || result.Translation != store.completed.Translation {
		t.Fatalf("result=%#v", result)
	}
	if store.failCalls != 0 || generator.calls != 1 || translator.calls != 1 {
		t.Fatalf("calls generator=%d translator=%d fail=%d", generator.calls, translator.calls, store.failCalls)
	}
}

func TestQuestionTipTranslationFailureDoesNotPersistPartialTip(t *testing.T) {
	store := &questionTipStoreStub{}
	generator := &answerTipGeneratorStub{result: AnswerTipGenerationResult{
		RequestID: "tip-request-1", Provider: "qianwen", Model: "qwen-plus",
		Content: "I would give a concise answer.",
	}}
	translationErr := sharedtranslation.NewProviderError(
		sharedtranslation.ProviderErrorUnavailable,
		"translation-request-1",
		errors.New("unavailable"),
	)
	translator := &questionTipTranslatorStub{err: translationErr}
	service, err := NewQuestionTipService(store, generator, translator)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.EnsureQuestionTip(
		context.Background(),
		requestcontext.Actor{UserID: "user-1", SessionID: "auth-session-1"},
		Session{ID: "session-1", QuestionTipsAllowed: true},
		practice.Question{ID: "question-1", SessionID: "session-1", Content: "Answer briefly."},
		nil,
		"tip-operation-1",
	)
	if !errors.Is(err, translationErr) {
		t.Fatalf("error=%v", err)
	}
	if store.failCalls != 1 || store.completeCalls != 0 {
		t.Fatalf("fail calls=%d complete calls=%d", store.failCalls, store.completeCalls)
	}
}

type questionTipStoreStub struct {
	completed     CompleteQuestionTipCommand
	completeCalls int
	failCalls     int
}

func (*questionTipStoreStub) ClaimQuestionTip(_ context.Context, _ Actor, command ClaimQuestionTipCommand) (QuestionTip, error) {
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	return QuestionTip{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", SessionID: command.SessionID,
		QuestionID: command.QuestionID, IdempotencyKey: command.IdempotencyKey,
		Status: QuestionTipProcessing, FencingToken: 1, LeaseAcquired: true,
		LeaseExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (*questionTipStoreStub) GetQuestionTip(context.Context, Actor, string, string) (QuestionTip, error) {
	return QuestionTip{}, ErrPersistenceNotFound
}

func (store *questionTipStoreStub) CompleteQuestionTip(_ context.Context, _ Actor, command CompleteQuestionTipCommand) (QuestionTip, error) {
	store.completeCalls++
	store.completed = command
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	return QuestionTip{
		ID: command.TipID, SessionID: "session-1", QuestionID: "question-1",
		Status: QuestionTipCompleted, FencingToken: command.FencingToken,
		Content: command.Content, Translation: command.Translation,
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}, nil
}

func (store *questionTipStoreStub) FailQuestionTip(context.Context, Actor, FailQuestionTipCommand) error {
	store.failCalls++
	return nil
}

type answerTipGeneratorStub struct {
	result AnswerTipGenerationResult
	err    error
	calls  int
}

func (generator *answerTipGeneratorStub) GenerateAnswerTip(context.Context, AnswerTipGenerationRequest) (AnswerTipGenerationResult, error) {
	generator.calls++
	return generator.result, generator.err
}

type questionTipTranslatorStub struct {
	content string
	err     error
	request sharedtranslation.Request
	calls   int
}

func (translator *questionTipTranslatorStub) Translate(_ context.Context, request sharedtranslation.Request) (string, error) {
	translator.calls++
	translator.request = request
	return translator.content, translator.err
}
