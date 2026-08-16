package summary

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

func TestWorkerTriggersOnOneMaximumLengthMessage(t *testing.T) {
	t.Parallel()
	repository := &workerRepositoryStub{
		claim: workerClaim(1),
		messages: []conversation.Message{
			summaryMessage(1, strings.Repeat("界", conversation.MaxMessageContentRunes)),
		},
	}
	worker := newTestWorker(t, repository)
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed != 1 || repository.completedThrough != 1 || repository.skipped {
		t.Fatalf("result=%#v repository=%#v", result, repository)
	}
}

func TestWorkerSkipsHistoryBelowDerivedBudget(t *testing.T) {
	t.Parallel()
	repository := &workerRepositoryStub{
		claim:    workerClaim(1),
		messages: []conversation.Message{summaryMessage(1, "Hello")},
	}
	worker := newTestWorker(t, repository)
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || !repository.skipped || repository.completedThrough != 0 {
		t.Fatalf("result=%#v repository=%#v", result, repository)
	}
}

func TestWorkerBudgetComesFromContextConfiguration(t *testing.T) {
	t.Parallel()
	configuration := testWorkerConfiguration()
	if configuration.TriggerCharacters() != 4000 ||
		configuration.RetainCharacters() != 2000 {
		t.Fatalf("trigger=%d retain=%d", configuration.TriggerCharacters(), configuration.RetainCharacters())
	}
}

func TestWorkerBoundsTwoHundredMaximumMessagesByActualModelInput(t *testing.T) {
	t.Parallel()
	messages := make([]conversation.Message, MaxSourceMessages)
	for index := range messages {
		messages[index] = summaryMessage(
			int64(index+1),
			strings.Repeat("界", conversation.MaxMessageContentRunes),
		)
	}
	repository := &workerRepositoryStub{
		claim:    workerClaim(MaxSourceMessages + 1),
		messages: messages,
	}
	generator := &recordingContentGenerator{}
	worker, err := NewWorker(repository, generator, testWorkerConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed != 1 || repository.completedThrough < 1 ||
		repository.completedThrough >= MaxSourceMessages {
		t.Fatalf("result=%#v repository=%#v", result, repository)
	}
	payload, err := encodeGenerationPayload(generator.command)
	if err != nil {
		t.Fatal(err)
	}
	actual := utf8.RuneCountInString(summarySystemPrompt) + utf8.RuneCount(payload)
	if actual > testWorkerConfiguration().MaxContextCharacters {
		t.Fatalf("Summary input characters = %d", actual)
	}
}

func TestWorkerFailsExplicitlyWhenOneMessageCannotFitModelInput(t *testing.T) {
	t.Parallel()
	repository := &workerRepositoryStub{
		claim: workerClaim(1),
		messages: []conversation.Message{
			summaryMessage(1, strings.Repeat("\\", conversation.MaxMessageContentRunes)),
		},
	}
	configuration := testWorkerConfiguration()
	configuration.MaxContextCharacters = 5000
	worker, err := NewWorker(repository, contentGeneratorStub{}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || repository.failureKind != "source_exceeds_budget" ||
		repository.failureRetryable {
		t.Fatalf("result=%#v repository=%#v", result, repository)
	}
}

func newTestWorker(t *testing.T, repository *workerRepositoryStub) *Worker {
	t.Helper()
	worker, err := NewWorker(repository, contentGeneratorStub{}, testWorkerConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func testWorkerConfiguration() WorkerConfiguration {
	return WorkerConfiguration{
		MaxContextCharacters: 12000,
		LeaseDuration:        time.Minute,
		MaxAttempts:          3,
		Generation:           Configuration{Provider: "qianwen", Model: "qwen-plus"},
	}
}

func workerClaim(target int64) Claim {
	return Claim{
		OwnerID:        "10000000-0000-4000-8000-000000000001",
		ThreadID:       "20000000-0000-4000-8000-000000000001",
		TargetSequence: target,
		AttemptCount:   1,
		LeaseToken:     "40000000-0000-4000-8000-000000000001",
		LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
	}
}

type contentGeneratorStub struct{}

func (contentGeneratorStub) Generate(
	context.Context,
	GenerateCommand,
) (Content, error) {
	return validContent(), nil
}

type recordingContentGenerator struct {
	command GenerateCommand
}

func (generator *recordingContentGenerator) Generate(
	_ context.Context,
	command GenerateCommand,
) (Content, error) {
	generator.command = command
	return validContent(), nil
}

type workerRepositoryStub struct {
	claim            Claim
	claimed          bool
	messages         []conversation.Message
	completedThrough int64
	skipped          bool
	failureKind      string
	failureRetryable bool
}

func (repository *workerRepositoryStub) FindSummary(
	context.Context, string, string, int64,
) (State, error) {
	return State{}, conversation.ErrNotFound
}

func (repository *workerRepositoryStub) ListMessagesForSummary(
	context.Context, string, string, int64, int64,
) ([]conversation.Message, error) {
	return append([]conversation.Message(nil), repository.messages...), nil
}

func (repository *workerRepositoryStub) Claim(
	context.Context,
	WorkerConfiguration,
) (Claim, bool, error) {
	if repository.claimed {
		return Claim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *workerRepositoryStub) Complete(
	_ context.Context,
	_ Claim,
	through int64,
	_ Content,
) error {
	repository.completedThrough = through
	return nil
}

func (repository *workerRepositoryStub) Skip(context.Context, Claim) error {
	repository.skipped = true
	return nil
}

func (repository *workerRepositoryStub) Fail(
	_ context.Context,
	_ Claim,
	kind string,
	retryable bool,
	_ WorkerConfiguration,
) (bool, error) {
	repository.failureKind = kind
	repository.failureRetryable = retryable
	return false, nil
}
