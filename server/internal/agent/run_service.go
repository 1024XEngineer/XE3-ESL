package agent

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	runPersistenceTimeout  = 5 * time.Second
	maxPersistedTokenCount = 1<<31 - 1
)

type RunService struct {
	repository    RunRepository
	assembler     *ContextAssembler
	generator     ai.TextGenerator
	configuration RunConfiguration
}

func NewRunService(
	repository RunRepository,
	assembler *ContextAssembler,
	generator ai.TextGenerator,
	configuration RunConfiguration,
) (*RunService, error) {
	if repository == nil || assembler == nil || generator == nil ||
		!validRunConfiguration(configuration) {
		return nil, errors.New("agent: run dependency or configuration is invalid")
	}
	return &RunService{
		repository:    repository,
		assembler:     assembler,
		generator:     generator,
		configuration: configuration,
	}, nil
}

func (service *RunService) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
) (RunSubmission, error) {
	if !actor.Valid() || !validUUID(threadID) {
		return RunSubmission{}, ErrNotFound
	}
	if !clientMessageIDPattern.MatchString(clientMessageID) ||
		!validMessageContent(content) {
		return RunSubmission{}, ErrInvalidRequest
	}
	submission, err := service.repository.CreateInitialRun(
		ctx,
		actor.UserID,
		threadID,
		clientMessageID,
		content,
		service.configuration,
	)
	if err != nil {
		return RunSubmission{}, err
	}
	submission.Run, err = service.process(ctx, actor, submission.Run)
	if err != nil {
		return RunSubmission{}, err
	}
	return submission, nil
}

func (service *RunService) RetryText(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
	retryClientID string,
) (RunRetry, error) {
	if !actor.Valid() || !validUUID(runID) {
		return RunRetry{}, ErrNotFound
	}
	if !clientMessageIDPattern.MatchString(retryClientID) {
		return RunRetry{}, ErrInvalidRequest
	}
	retry, err := service.repository.CreateRetryRun(
		ctx,
		actor.UserID,
		runID,
		retryClientID,
		service.configuration,
	)
	if err != nil {
		return RunRetry{}, err
	}
	retry.Run, err = service.process(ctx, actor, retry.Run)
	if err != nil {
		return RunRetry{}, err
	}
	return retry, nil
}

func (service *RunService) GetRun(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) (Run, error) {
	if !actor.Valid() || !validUUID(runID) {
		return Run{}, ErrNotFound
	}
	return service.repository.FindRun(ctx, actor.UserID, runID)
}

func (service *RunService) GetContextManifest(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) (ContextManifest, error) {
	if !actor.Valid() || !validUUID(runID) {
		return ContextManifest{}, ErrNotFound
	}
	if _, err := service.repository.FindRun(
		ctx,
		actor.UserID,
		runID,
	); err != nil {
		return ContextManifest{}, err
	}
	return service.repository.FindContextManifest(ctx, actor.UserID, runID)
}

func (service *RunService) RecoverInterruptedRuns(
	ctx context.Context,
) (int64, error) {
	return service.repository.RecoverInterruptedRuns(ctx)
}

func (service *RunService) process(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
) (Run, error) {
	if run.Status != RunStatusPending {
		return run, nil
	}
	claimed, acquired, err := service.repository.ClaimRun(
		ctx,
		actor.UserID,
		run.ID,
	)
	if err != nil {
		return Run{}, err
	}
	if !acquired {
		return claimed, nil
	}
	if !runConfigurationMatches(claimed, service.configuration) {
		return service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			RunFailureConfigurationDrift,
			true,
		)
	}

	manifest, request, err := service.assembler.Assemble(
		ctx,
		actor,
		claimed,
		service.configuration,
	)
	if err != nil {
		kind := RunFailureInternal
		retryable := true
		if errors.Is(err, ErrInvalidContext) {
			kind = RunFailureInvalidContext
			retryable = false
		}
		return service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
	}
	if _, err := service.repository.SaveContextManifest(
		ctx,
		manifest,
	); err != nil {
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			RunFailureInternal,
			true,
		)
		if failErr != nil {
			return Run{}, err
		}
		return failed, nil
	}

	result, err := service.generator.Generate(ctx, request)
	if err != nil {
		kind, retryable := generationFailure(err)
		return service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
	}
	if !validTextResult(result) ||
		result.Provider != service.configuration.Provider ||
		result.Model != service.configuration.Model ||
		result.Usage.OutputTokens > service.configuration.MaxOutputTokens {
		return service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			string(ai.ErrorInvalidResponse),
			ai.ErrorInvalidResponse.Retryable(),
		)
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.CompleteRun(
		persistContext,
		actor.UserID,
		claimed.ID,
		claimed.WorkerLeaseToken,
		result.Content,
		result,
	)
}

func (service *RunService) persistFailure(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	kind string,
	retryable bool,
) (Run, error) {
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.FailRun(
		persistContext,
		ownerID,
		runID,
		workerLeaseToken,
		kind,
		retryable,
	)
}

func runPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		runPersistenceTimeout,
	)
}

func generationFailure(err error) (string, bool) {
	var generationError *ai.GenerationError
	if errors.As(err, &generationError) {
		return string(generationError.Kind), generationError.Retryable()
	}
	return string(ai.ErrorProviderUnavailable), true
}

func validTextResult(result ai.TextResult) bool {
	return modelPattern.MatchString(result.ID) &&
		providerPattern.MatchString(result.Provider) &&
		modelPattern.MatchString(result.Model) &&
		(result.FinishReason == "stop" || result.FinishReason == "length") &&
		validTokenUsage(result.Usage) &&
		validMessageContent(result.Content)
}

func validTokenUsage(usage ai.TokenUsage) bool {
	return usage.InputTokens >= 0 &&
		usage.InputTokens <= maxPersistedTokenCount &&
		usage.OutputTokens >= 0 &&
		usage.OutputTokens <= maxPersistedTokenCount &&
		usage.TotalTokens >= usage.InputTokens &&
		usage.TotalTokens <= maxPersistedTokenCount &&
		usage.TotalTokens-usage.InputTokens == usage.OutputTokens
}

func runConfigurationMatches(
	run Run,
	configuration RunConfiguration,
) bool {
	return run.RequestedProvider == configuration.Provider &&
		run.RequestedModel == configuration.Model &&
		run.MaxOutputTokens == configuration.MaxOutputTokens &&
		run.MaxInputCharacters == configuration.MaxInputCharacters
}
