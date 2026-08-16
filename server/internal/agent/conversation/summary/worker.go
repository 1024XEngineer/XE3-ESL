package summary

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const maxSummarySweepLimit = 20

type ContentGenerator interface {
	Generate(context.Context, GenerateCommand) (Content, error)
}

type Worker struct {
	repository    Repository
	generator     ContentGenerator
	configuration WorkerConfiguration
}

func NewWorker(
	repository Repository,
	generator ContentGenerator,
	configuration WorkerConfiguration,
) (*Worker, error) {
	if repository == nil || generator == nil || !configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	return &Worker{
		repository:    repository,
		generator:     generator,
		configuration: configuration,
	}, nil
}

func (worker *Worker) ProcessPending(
	ctx context.Context,
	limit int,
) (SweepResult, error) {
	if ctx == nil || limit < 1 || limit > maxSummarySweepLimit {
		return SweepResult{}, ErrInvalidArgument
	}
	var result SweepResult
	for result.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claim, found, err := worker.repository.Claim(ctx, worker.configuration)
		if err != nil {
			return result, err
		}
		if !found {
			return result, nil
		}
		result.Claimed++
		outcome, err := worker.process(ctx, claim)
		if err != nil {
			return result, err
		}
		switch outcome {
		case "completed":
			result.Completed++
		case "retried":
			result.Retried++
		case "skipped":
			result.Skipped++
		case "failed":
			result.Failed++
		default:
			return result, conversation.ErrRepository
		}
	}
	return result, nil
}

func (worker *Worker) process(ctx context.Context, claim Claim) (string, error) {
	var previous *State
	state, err := worker.repository.FindSummary(
		ctx, claim.OwnerID, claim.ThreadID, claim.TargetSequence,
	)
	if err == nil {
		if !state.Valid() || state.OwnerID != claim.OwnerID ||
			state.ThreadID != claim.ThreadID ||
			state.ThroughSequence >= claim.TargetSequence {
			return worker.recordFailure(ctx, claim, ErrInvalidResponse)
		}
		previous = &state
	} else if !errors.Is(err, conversation.ErrNotFound) {
		return worker.recordFailure(ctx, claim, err)
	}
	fromSequence := int64(1)
	if previous != nil {
		fromSequence = previous.ThroughSequence + 1
	}
	messages, err := worker.repository.ListMessagesForSummary(
		ctx,
		claim.OwnerID,
		claim.ThreadID,
		fromSequence,
		claim.TargetSequence,
	)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	coveredCount := selectCoverage(
		messages,
		claim.TargetSequence,
		worker.configuration.TriggerCharacters(),
		worker.configuration.RetainCharacters(),
	)
	if coveredCount == 0 {
		if err := worker.repository.Skip(ctx, claim); err != nil {
			return "", err
		}
		return "skipped", nil
	}
	coveredCount = selectSourceWithinBudget(
		previous,
		messages[:coveredCount],
		worker.configuration.MaxContextCharacters,
	)
	if coveredCount == 0 {
		return worker.recordFailure(ctx, claim, ErrSourceExceedsBudget)
	}
	source := messages[:coveredCount]
	content, err := worker.generator.Generate(ctx, GenerateCommand{
		Previous:           previous,
		Messages:           source,
		MaxInputCharacters: worker.configuration.MaxContextCharacters,
	})
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if !content.Valid() {
		return worker.recordFailure(ctx, claim, ErrInvalidResponse)
	}
	if err := worker.repository.Complete(
		ctx,
		claim,
		source[len(source)-1].Sequence,
		content,
	); err != nil {
		return "", err
	}
	return "completed", nil
}

// selectSourceWithinBudget advances the oldest continuous prefix that fits
// the real model input after the fixed system prompt, previous Summary and JSON
// framing are encoded. It never truncates a Message or skips a fact in front
// of the persisted waterline.
func selectSourceWithinBudget(
	previous *State,
	messages []conversation.Message,
	maxInputCharacters int,
) int {
	covered := 0
	for count := 1; count <= len(messages); count++ {
		command := GenerateCommand{
			Previous:           previous,
			Messages:           messages[:count],
			MaxInputCharacters: maxInputCharacters,
		}
		payload, err := encodeGenerationPayload(command)
		if err != nil || utf8.RuneCountInString(summarySystemPrompt)+
			utf8.RuneCount(payload) > maxInputCharacters {
			break
		}
		covered = count
	}
	return covered
}

// selectCoverage keeps a recent character window verbatim. If the repository
// returned a capped backlog, it advances one bounded batch even when messages
// are individually tiny because message framing also consumes model context.
func selectCoverage(
	messages []conversation.Message,
	targetSequence int64,
	triggerCharacters int,
	retainCharacters int,
) int {
	if len(messages) == 0 || triggerCharacters < 1 || retainCharacters < 1 {
		return 0
	}
	truncatedBacklog := messages[len(messages)-1].Sequence < targetSequence
	total := 0
	for _, message := range messages {
		total += utf8.RuneCountInString(message.Content)
	}
	if !truncatedBacklog && total < triggerCharacters {
		return 0
	}
	limit := min(len(messages), MaxSourceMessages)
	if truncatedBacklog {
		return limit
	}
	retained := 0
	covered := len(messages)
	for covered > 0 {
		runes := utf8.RuneCountInString(messages[covered-1].Content)
		if retained+runes > retainCharacters {
			break
		}
		retained += runes
		covered--
	}
	if covered > MaxSourceMessages {
		covered = MaxSourceMessages
	}
	return covered
}

func (worker *Worker) recordFailure(
	ctx context.Context,
	claim Claim,
	cause error,
) (string, error) {
	kind, retryable := classifyFailure(cause)
	retry, err := worker.repository.Fail(
		ctx, claim, kind, retryable, worker.configuration,
	)
	if err != nil {
		return "", err
	}
	if retry {
		return "retried", nil
	}
	return "failed", nil
}

func classifyFailure(cause error) (string, bool) {
	switch {
	case errors.Is(cause, context.Canceled):
		return "canceled", true
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout", true
	case errors.Is(cause, ErrInvalidArgument),
		errors.Is(cause, conversation.ErrInvalidRequest):
		return "invalid_argument", false
	case errors.Is(cause, ErrSourceExceedsBudget):
		return "source_exceeds_budget", false
	case errors.Is(cause, ErrInvalidResponse):
		return "invalid_response", false
	case errors.Is(cause, conversation.ErrNotFound):
		return "source_not_found", false
	case errors.Is(cause, conversation.ErrConflict):
		return "concurrent_update", true
	}
	var generationError GenerationFailure
	if errors.As(cause, &generationError) {
		kind := generationError.StableCategory()
		if !failurePattern.MatchString(kind) {
			kind = "provider_error"
		}
		return kind, generationError.Retryable()
	}
	return "dependency", true
}

var _ Processor = (*Worker)(nil)
