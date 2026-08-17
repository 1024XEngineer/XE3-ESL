package run

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	runPersistenceTimeout    = 5 * time.Second
	maxPersistedTokenCount   = 1<<31 - 1
	defaultMaxIterations     = 3
	MaxToolCallsPerRun       = 4
	maxWriteCallsPerRun      = 1
	defaultToolTimeout       = 5 * time.Second
	defaultLoopTimeout       = 25 * time.Second
	defaultMaxToolResult     = 16 * 1024
	streamReplayPollInterval = 100 * time.Millisecond
	toolSchemaVersionV1      = "tool-schema-v1"
	maxImagesPerMessage      = 4
	finalResponseToolName    = "agent.final_response.v1"
)

type Service struct {
	repository       Repository
	messages         MessageReader
	imageSubmissions ImageSubmissionRepository
	assembler        *agentcontext.Assembler
	generator        TextGenerator
	configuration    Configuration
	registry         *capability.Registry
	executor         *capability.Executor
	loopLimits       LoopLimits
	logger           *slog.Logger
	logOptions       LogOptions
}

type LoopLimits struct {
	MaxIterations      int
	MaxToolCalls       int
	ToolTimeout        time.Duration
	LoopTimeout        time.Duration
	MaxToolResultBytes int
}

type LogOptions struct {
	LogUserInput    bool
	LogToolPayloads bool
	InputPreviewMax int
}

type Option func(*Service) error

func NewService(
	repository Repository,
	messages MessageReader,
	assembler *agentcontext.Assembler,
	generator TextGenerator,
	configuration Configuration,
	options ...Option,
) (*Service, error) {
	if repository == nil || messages == nil || assembler == nil || generator == nil ||
		!configuration.Valid() {
		return nil, errors.New("agent: run dependency or configuration is invalid")
	}
	service := &Service{
		repository:    repository,
		messages:      messages,
		assembler:     assembler,
		generator:     generator,
		configuration: configuration,
		loopLimits:    defaultLoopLimits(),
		logOptions:    normalizeLogOptions(LogOptions{}),
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("agent: run option is invalid")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func WithToolRegistry(registry *capability.Registry) Option {
	return func(service *Service) error {
		if registry == nil {
			return errors.New("agent: tool registry is required")
		}
		service.registry = registry
		service.executor = capability.NewExecutorWithLogger(
			registry,
			service.logger,
			service.logOptions.LogToolPayloads,
		)
		return nil
	}
}

func WithRunLogger(logger *slog.Logger) Option {
	return func(service *Service) error {
		if logger == nil {
			return errors.New("agent: run logger is required")
		}
		service.logger = logger
		if service.registry != nil {
			service.executor = capability.NewExecutorWithLogger(
				service.registry,
				logger,
				service.logOptions.LogToolPayloads,
			)
		}
		return nil
	}
}

func WithLogOptions(options LogOptions) Option {
	return func(service *Service) error {
		service.logOptions = normalizeLogOptions(options)
		if service.registry != nil {
			service.executor = capability.NewExecutorWithLogger(
				service.registry,
				service.logger,
				service.logOptions.LogToolPayloads,
			)
		}
		return nil
	}
}

func WithLoopLimits(limits LoopLimits) Option {
	return func(service *Service) error {
		normalized := normalizeLoopLimits(limits)
		if normalized.MaxIterations <= 0 ||
			normalized.MaxToolCalls <= 0 ||
			normalized.MaxToolCalls > MaxToolCallsPerRun ||
			normalized.ToolTimeout <= 0 ||
			normalized.LoopTimeout <= 0 ||
			normalized.MaxToolResultBytes <= 0 ||
			normalized.MaxToolResultBytes > 64*1024 {
			return errors.New("agent: loop limits are invalid")
		}
		service.loopLimits = normalized
		return nil
	}
}

func WithImageSubmissions(repository ImageSubmissionRepository) Option {
	return func(service *Service) error {
		if repository == nil {
			return errors.New("agent: image submission repository is required")
		}
		service.imageSubmissions = repository
		return nil
	}
}

func (service *Service) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
) (Submission, error) {
	if !actor.Valid() || !ValidUUID(threadID) {
		return Submission{}, ErrNotFound
	}
	if !conversation.ValidClientMessageID(clientMessageID) ||
		!conversation.ValidMessageContent(content) {
		return Submission{}, ErrInvalidRequest
	}
	submission, err := service.repository.CreateInitial(
		ctx,
		actor.UserID,
		threadID,
		clientMessageID,
		content,
		service.configuration,
	)
	if err != nil {
		return Submission{}, err
	}
	submission.Run, err = service.process(ctx, actor, submission.Run, nil)
	if err != nil {
		return Submission{}, err
	}
	return submission, nil
}

func (service *Service) SubmitTextStream(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
	observer StreamObserver,
) (Submission, error) {
	if observer == nil {
		return Submission{}, ErrInvalidRequest
	}
	if !actor.Valid() || !ValidUUID(threadID) {
		return Submission{}, ErrNotFound
	}
	if !conversation.ValidClientMessageID(clientMessageID) ||
		!conversation.ValidMessageContent(content) {
		return Submission{}, ErrInvalidRequest
	}
	submission, err := service.repository.CreateInitial(
		ctx, actor.UserID, threadID, clientMessageID, content,
		service.configuration,
	)
	if err != nil {
		return Submission{}, err
	}
	if err := observer.OnInputCommitted(ctx, submission); err != nil {
		return Submission{}, err
	}
	submission.Run, err = service.process(ctx, actor, submission.Run, observer)
	if err == nil {
		submission.Run, err = service.waitForTerminalRun(
			ctx,
			actor.UserID,
			submission.Run,
		)
	}
	return submission, err
}

func (service *Service) SubmitWithImages(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
	imageAssetIDs []string,
) (Submission, error) {
	if service == nil || service.imageSubmissions == nil {
		return Submission{}, ErrInvalidRequest
	}
	if !actor.Valid() || !ValidUUID(threadID) {
		return Submission{}, ErrNotFound
	}
	if !conversation.ValidClientMessageID(clientMessageID) ||
		!conversation.ValidMessageContent(content) ||
		!validImageAssetIDs(imageAssetIDs) {
		return Submission{}, ErrInvalidRequest
	}
	submission, err := service.imageSubmissions.CreateInitialWithImages(
		ctx,
		actor.UserID,
		threadID,
		clientMessageID,
		content,
		imageAssetIDs,
		service.configuration,
	)
	if err != nil {
		return Submission{}, err
	}
	submission.Run, err = service.process(ctx, actor, submission.Run, nil)
	if err != nil {
		return Submission{}, err
	}
	return submission, nil
}

func validImageAssetIDs(imageAssetIDs []string) bool {
	if len(imageAssetIDs) < 1 ||
		len(imageAssetIDs) > maxImagesPerMessage {
		return false
	}
	seen := make(map[string]struct{}, len(imageAssetIDs))
	for _, assetID := range imageAssetIDs {
		if !ValidUUID(assetID) {
			return false
		}
		if _, found := seen[assetID]; found {
			return false
		}
		seen[assetID] = struct{}{}
	}
	return true
}

func (service *Service) RetryText(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
	retryClientID string,
) (Retry, error) {
	if !actor.Valid() || !ValidUUID(runID) {
		return Retry{}, ErrNotFound
	}
	if !conversation.ValidClientMessageID(retryClientID) {
		return Retry{}, ErrInvalidRequest
	}
	retry, err := service.repository.CreateRetry(
		ctx,
		actor.UserID,
		runID,
		retryClientID,
		service.configuration,
	)
	if err != nil {
		return Retry{}, err
	}
	retry.Run, err = service.process(ctx, actor, retry.Run, nil)
	if err != nil {
		return Retry{}, err
	}
	return retry, nil
}

func (service *Service) RetryTextStream(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
	retryClientID string,
	observer StreamObserver,
) (Retry, error) {
	if observer == nil {
		return Retry{}, ErrInvalidRequest
	}
	if !actor.Valid() || !ValidUUID(runID) {
		return Retry{}, ErrNotFound
	}
	if !conversation.ValidClientMessageID(retryClientID) {
		return Retry{}, ErrInvalidRequest
	}
	retry, err := service.repository.CreateRetry(
		ctx, actor.UserID, runID, retryClientID, service.configuration,
	)
	if err != nil {
		return Retry{}, err
	}
	userMessage, err := service.messages.FindMessage(
		ctx,
		actor.UserID,
		retry.Run.ThreadID,
		retry.Run.InputMessageID,
	)
	if err != nil {
		return Retry{}, err
	}
	if err := observer.OnInputCommitted(ctx, Submission{
		Run: retry.Run, UserMessage: userMessage, Created: retry.Created,
	}); err != nil {
		return Retry{}, err
	}
	retry.Run, err = service.process(ctx, actor, retry.Run, observer)
	if err == nil {
		retry.Run, err = service.waitForTerminalRun(
			ctx,
			actor.UserID,
			retry.Run,
		)
	}
	return retry, err
}

func (service *Service) waitForTerminalRun(
	ctx context.Context,
	ownerID string,
	run Run,
) (Run, error) {
	if run.Status == StatusCompleted || run.Status == StatusFailed {
		return run, nil
	}
	ticker := time.NewTicker(streamReplayPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return Run{}, ctx.Err()
		case <-ticker.C:
			current, err := service.repository.Find(ctx, ownerID, run.ID)
			if err != nil {
				return Run{}, err
			}
			switch current.Status {
			case StatusCompleted, StatusFailed:
				return current, nil
			case StatusPending, StatusRunning:
				continue
			default:
				return Run{}, agentcontext.ErrInvalidContext
			}
		}
	}
}

func (service *Service) GetRun(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) (Run, error) {
	if !actor.Valid() || !ValidUUID(runID) {
		return Run{}, ErrNotFound
	}
	return service.repository.Find(ctx, actor.UserID, runID)
}

func (service *Service) GetClientActions(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) ([]agentclientaction.Action, error) {
	if !actor.Valid() || !ValidUUID(runID) {
		return nil, ErrNotFound
	}
	if _, err := service.repository.Find(
		ctx,
		actor.UserID,
		runID,
	); err != nil {
		return nil, err
	}
	return service.repository.ListClientActions(ctx, actor.UserID, runID)
}

func (service *Service) RecoverInterrupted(
	ctx context.Context,
) (int64, error) {
	return service.repository.RecoverInterrupted(ctx)
}

// ProcessPending resumes a Run that was atomically created by another
// Agent-owned input workflow, such as voice-candidate confirmation. It does
// not create or mutate the input Message itself.
func (service *Service) ProcessPending(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
) (Run, error) {
	if ctx == nil || !actor.Valid() ||
		run.OwnerID != actor.UserID ||
		!ValidUUID(run.ID) ||
		!ValidUUID(run.ThreadID) ||
		!ValidUUID(run.InputMessageID) {
		return Run{}, ErrInvalidRequest
	}
	return service.process(ctx, actor, run, nil)
}

// ProcessPendingStream resumes a Run created by another Agent input workflow
// while forwarding typed tool steps and the committed Assistant Output to that
// workflow's live response.
func (service *Service) ProcessPendingStream(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	observer StreamObserver,
) (Run, error) {
	if ctx == nil || observer == nil || !actor.Valid() ||
		run.OwnerID != actor.UserID ||
		!ValidUUID(run.ID) ||
		!ValidUUID(run.ThreadID) ||
		!ValidUUID(run.InputMessageID) {
		return Run{}, ErrInvalidRequest
	}
	processed, err := service.process(ctx, actor, run, observer)
	if err == nil {
		processed, err = service.waitForTerminalRun(
			ctx,
			actor.UserID,
			processed,
		)
	}
	return processed, err
}

func (service *Service) process(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	observer StreamObserver,
) (Run, error) {
	if run.Status != StatusPending {
		return run, nil
	}
	claimed, acquired, err := service.repository.Claim(
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
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			FailureConfigurationDrift,
			true,
		)
		service.logRunFailed(
			claimed,
			FailureConfigurationDrift,
			true,
			claimed.StartedAt,
		)
		return failed, failErr
	}

	manifest, modelInput, err := service.assembler.Assemble(
		ctx,
		actor,
		agentcontext.AssembleCommand{
			RunID:              claimed.ID,
			OwnerID:            claimed.OwnerID,
			ThreadID:           claimed.ThreadID,
			InputMessageID:     claimed.InputMessageID,
			RunCreatedAt:       claimed.CreatedAt.UTC(),
			Provider:           service.configuration.Provider,
			Model:              service.configuration.Model,
			MaxOutputTokens:    service.configuration.MaxOutputTokens,
			MaxInputCharacters: service.configuration.MaxInputCharacters,
		},
	)
	if err != nil {
		kind := FailureInternal
		retryable := true
		switch {
		case errors.Is(err, agentcontext.ErrInvalidContext):
			kind = FailureInvalidContext
			retryable = false
		}
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
		service.logRunFailed(claimed, kind, retryable, claimed.StartedAt)
		service.logGenerationFailureDetail(claimed, err)
		return failed, failErr
	}
	if manifest.CoachingProfileContextStatus ==
		agentcontext.CoachingProfileContextUnavailableError {
		service.logContextDegradation(
			claimed,
			"coaching_profile",
			manifest.CoachingProfileContextStatus,
		)
	}
	request, err := textRequestFromContext(modelInput)
	if err != nil {
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			FailureInvalidContext,
			false,
		)
		service.logRunFailed(
			claimed,
			FailureInvalidContext,
			false,
			claimed.StartedAt,
		)
		return failed, failErr
	}
	outputID, err := service.repository.NewAssistantMessageID()
	if err != nil || !ValidUUID(outputID) {
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			FailureInternal,
			true,
		)
		return failed, failErr
	}
	output := AssistantOutput{ID: outputID, RunID: claimed.ID}
	var result TextResult
	var finalDeltas []string
	if observer == nil {
		result, err = service.generate(
			ctx,
			actor,
			claimed,
			manifest,
			request,
		)
	} else {
		result, finalDeltas, err = service.generateObserved(
			ctx,
			actor,
			claimed,
			manifest,
			request,
			observer,
			output,
		)
	}
	if err != nil {
		kind, retryable := classifyRunFailure(err)
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
		service.logRunFailed(claimed, kind, retryable, claimed.StartedAt)
		service.logGenerationFailureDetail(claimed, err)
		return failed, failErr
	}
	if !validFinalTextResult(result) ||
		result.Provider != service.configuration.Provider ||
		result.Model != service.configuration.Model ||
		result.Usage.OutputTokens > service.configuration.MaxOutputTokens {
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			string(ErrorInvalidResponse),
			ErrorInvalidResponse.Retryable(),
		)
		service.logRunFailed(
			claimed,
			string(ErrorInvalidResponse),
			ErrorInvalidResponse.Retryable(),
			claimed.StartedAt,
		)
		return failed, failErr
	}
	output.Content = result.Content
	if observer != nil {
		if len(finalDeltas) == 0 || joinedTextDeltas(finalDeltas) != result.Content {
			failed, failErr := service.persistFailure(
				ctx,
				actor.UserID,
				claimed.ID,
				claimed.WorkerLeaseToken,
				string(ErrorInvalidResponse),
				ErrorInvalidResponse.Retryable(),
			)
			return failed, failErr
		}
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	completed, err := service.repository.Complete(
		persistContext,
		actor.UserID,
		claimed.ID,
		claimed.WorkerLeaseToken,
		output,
		result,
	)
	if err != nil || observer == nil {
		return completed, err
	}
	if err := observer.OnAssistantOutputCompleted(ctx, output); err != nil {
		return completed, err
	}
	return completed, nil
}

func (service *Service) generate(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	manifest agentcontext.Manifest,
	request TextRequest,
) (TextResult, error) {
	result, _, err := service.generateObserved(
		ctx, actor, run, manifest, request, nil, AssistantOutput{},
	)
	return result, err
}

func (service *Service) generateObserved(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	manifest agentcontext.Manifest,
	request TextRequest,
	observer StreamObserver,
	output AssistantOutput,
) (TextResult, []string, error) {
	if service.registry == nil || service.executor == nil {
		if err := service.repository.SaveContextSnapshot(
			ctx, run.OwnerID, run.ID, run.WorkerLeaseToken, manifest,
		); err != nil {
			return TextResult{}, nil, err
		}
		return service.generateFinalOutput(ctx, request, observer, output)
	}

	loopCtx, cancel := context.WithTimeout(ctx, service.loopLimits.LoopTimeout)
	defer cancel()
	startedAt := time.Now()
	service.logRunReceived(run, lastUserContent(request), request)

	routing := buildModelToolRouting(
		service.registry,
		service.logger,
		run.ID,
		ToolChoice{Mode: ToolChoiceRequired},
	)
	routing.Definitions = append(
		routing.Definitions,
		finalResponseToolDefinition(),
	)
	request.Tools = routing.Definitions
	request.ToolChoice = routing.ToolChoice
	applyModelToolSnapshot(&manifest, routing)
	if err := service.repository.SaveContextSnapshot(
		loopCtx, run.OwnerID, run.ID, run.WorkerLeaseToken, manifest,
	); err != nil {
		return TextResult{}, nil, err
	}

	exposed := exposedToolNames(request.Tools)
	seenToolCallIDs := make(map[string]struct{})
	toolCalls := 0
	writeCalls := 0
	toolIterations := 0
	modelIterations := 0
	finalDecision := "direct_response"

	for {
		service.logLoopIteration(run, modelIterations, toolCalls)
		result, err := service.generateModel(loopCtx, request, nil)
		if err != nil {
			return TextResult{}, nil, err
		}
		modelIterations++
		if !validLoopTextResult(result) ||
			result.Provider != service.configuration.Provider ||
			result.Model != service.configuration.Model ||
			result.Usage.OutputTokens > service.configuration.MaxOutputTokens {
			service.logInvalidModelResult(run, result)
			return result, nil, nil
		}
		if len(result.ToolCalls) == 0 {
			service.logInvalidModelResult(run, result)
			return TextResult{}, nil, NewGenerationError(
				ErrorInvalidResponse,
				0,
				"",
				"",
				errors.New("agent: planning response did not choose a tool or final output"),
			)
		}
		if finalResponseRequested(result.ToolCalls) {
			if !validFinalResponseRequest(result.ToolCalls) {
				service.logInvalidModelResult(run, result)
				return TextResult{}, nil, NewGenerationError(
					ErrorInvalidResponse,
					0,
					"",
					"",
					errors.New("agent: final response control call is invalid"),
				)
			}
			service.logRoutingDecision(
				run,
				finalDecision,
				nil,
				reasonModelToolSelection,
				reasonSummary(reasonModelToolSelection, finalDecision),
				modelIterations,
			)
			finalResult, finalDeltas, finalErr := service.generateFinalOutput(
				loopCtx,
				request,
				observer,
				output,
			)
			if finalErr != nil {
				return TextResult{}, nil, finalErr
			}
			modelIterations++
			service.logRunCompleted(
				run,
				finalDecision,
				modelIterations,
				toolCalls,
				startedAt,
				finalResult.Content,
			)
			return finalResult, finalDeltas, nil
		}

		selected := toolCallNames(result.ToolCalls)
		service.logRoutingDecision(
			run,
			"tool_call",
			selected,
			reasonModelToolSelection,
			reasonSummary(reasonModelToolSelection, "tool_call"),
			modelIterations,
		)
		if toolIterations >= service.loopLimits.MaxIterations {
			return TextResult{}, nil, service.stopLoop(
				run,
				FailureToolIterationBudgetExhausted,
				selected,
				modelIterations,
			)
		}
		if toolCalls+len(result.ToolCalls) > service.loopLimits.MaxToolCalls {
			return TextResult{}, nil, service.stopLoop(
				run,
				FailureToolCallBudgetExhausted,
				selected,
				modelIterations,
			)
		}
		if hasRepeatedToolCallID(result.ToolCalls, seenToolCallIDs) {
			return TextResult{}, nil, service.stopLoop(
				run,
				FailureDuplicateToolCall,
				selected,
				modelIterations,
			)
		}

		pendingWriteCalls := service.writeToolCallCount(result.ToolCalls)
		if writeCalls+pendingWriteCalls > maxWriteCallsPerRun {
			return TextResult{}, nil, service.stopLoop(
				run,
				FailureWriteToolCallBudgetExhausted,
				selected,
				modelIterations,
			)
		}
		writeCalls += pendingWriteCalls
		request.Messages = append(request.Messages, TextMessage{
			Content:   result.Content,
			Role:      TextRoleAssistant,
			ToolCalls: result.ToolCalls,
		})
		toolIterations++
		toolCalls += len(result.ToolCalls)

		for _, call := range result.ToolCalls {
			seenToolCallIDs[call.ID] = struct{}{}
			if err := service.saveToolCallProposed(loopCtx, run, call); err != nil {
				return TextResult{}, nil, err
			}
			step := ToolStep{ID: call.ID, RunID: run.ID, Name: call.Name}
			if observer != nil {
				if err := observer.OnToolStarted(loopCtx, step); err != nil {
					return TextResult{}, nil, err
				}
			}
			if !toolExposed(exposed, call.Name) {
				if err := service.markToolCallRejected(
					loopCtx,
					run,
					call.ID,
					"unknown_tool",
				); err != nil {
					return TextResult{}, nil, err
				}
				if observer != nil {
					if err := observer.OnToolFailed(loopCtx, step); err != nil {
						return TextResult{}, nil, err
					}
				}
				request.Messages = append(
					request.Messages,
					toolFailureMessage(call.ID, capability.ErrUnknownTool),
				)
				continue
			}
			toolMessage, status, err := service.executeToolCall(
				loopCtx,
				actor,
				run,
				call,
			)
			if err != nil {
				if observer != nil {
					_ = observer.OnToolFailed(loopCtx, step)
				}
				return TextResult{}, nil, err
			}
			if observer != nil {
				var eventErr error
				if status == ToolCallSucceeded {
					eventErr = observer.OnToolCompleted(loopCtx, step)
				} else {
					eventErr = observer.OnToolFailed(loopCtx, step)
				}
				if eventErr != nil {
					return TextResult{}, nil, eventErr
				}
			}
			request.Messages = append(request.Messages, toolMessage)
		}
		finalDecision = "tool_call_then_response"
	}
}

func finalResponseToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: finalResponseToolName,
		Description: "Choose this control tool only when no further domain tool is " +
			"needed and the assistant is ready to produce its final user-visible response. " +
			"Set decision to respond and do not include the response text in this call; " +
			"the server starts a separate tool-disabled final output phase.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"decision": capability.StringEnumSchema(
				"Enter the final user-visible response phase.",
				"respond",
			),
		}, []string{"decision"}),
	}
}

func finalResponseRequested(calls []ModelToolCall) bool {
	for _, call := range calls {
		if call.Name == finalResponseToolName {
			return true
		}
	}
	return false
}

func validFinalResponseRequest(calls []ModelToolCall) bool {
	if len(calls) != 1 || calls[0].Name != finalResponseToolName {
		return false
	}
	var input map[string]any
	if json.Unmarshal(calls[0].Arguments, &input) != nil || len(input) != 1 {
		return false
	}
	decision, ok := input["decision"].(string)
	return ok && decision == "respond"
}

func (service *Service) generateFinalOutput(
	ctx context.Context,
	request TextRequest,
	observer StreamObserver,
	output AssistantOutput,
) (TextResult, []string, error) {
	request.Tools = nil
	request.ToolChoice = ToolChoice{Mode: ToolChoiceNone}
	if observer == nil {
		result, err := service.generateModel(ctx, request, nil)
		return result, nil, err
	}
	if !ValidUUID(output.ID) || output.RunID == "" {
		return TextResult{}, nil, NewGenerationError(
			ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("agent: final output identity is invalid"),
		)
	}
	if err := ValidateTextRequest(request); err != nil {
		return TextResult{}, nil, NewGenerationError(
			ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	streaming, ok := service.generator.(StreamingTextGenerator)
	if !ok {
		return TextResult{}, nil, NewGenerationError(
			ErrorConfiguration,
			0,
			"",
			"",
			errors.New("agent: configured text generator does not support streaming"),
		)
	}
	stream := &assistantOutputDeltaStream{observer: observer, output: output}
	if err := stream.start(ctx); err != nil {
		return TextResult{}, nil, streamObserverGenerationError(err)
	}
	result, err := streaming.GenerateStream(ctx, request, stream)
	if err != nil {
		return TextResult{}, nil, err
	}
	return result, stream.deltas, nil
}

func (service *Service) generateModel(
	ctx context.Context,
	request TextRequest,
	observer TextDeltaObserver,
) (TextResult, error) {
	if err := ValidateTextRequest(request); err != nil {
		return TextResult{}, NewGenerationError(
			ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	if observer == nil {
		return service.generator.Generate(ctx, request)
	}
	streaming, ok := service.generator.(StreamingTextGenerator)
	if !ok {
		return TextResult{}, NewGenerationError(
			ErrorConfiguration,
			0,
			"",
			"",
			errors.New("agent: configured text generator does not support streaming"),
		)
	}
	return streaming.GenerateStream(ctx, request, observer)
}

type assistantOutputDeltaStream struct {
	observer StreamObserver
	output   AssistantOutput
	sequence int
	deltas   []string
}

func (stream *assistantOutputDeltaStream) start(ctx context.Context) error {
	return stream.observer.OnAssistantOutputStarted(ctx, AssistantOutput{
		ID: stream.output.ID, RunID: stream.output.RunID,
	})
}

func (stream *assistantOutputDeltaStream) OnTextDelta(
	ctx context.Context,
	delta string,
) error {
	if delta == "" {
		return nil
	}
	stream.sequence++
	if err := stream.observer.OnAssistantOutputDelta(
		ctx,
		AssistantOutputDelta{
			OutputID: stream.output.ID,
			RunID:    stream.output.RunID,
			Sequence: stream.sequence,
			Delta:    delta,
		},
	); err != nil {
		return streamObserverGenerationError(err)
	}
	stream.deltas = append(stream.deltas, delta)
	return nil
}

func streamObserverGenerationError(err error) *GenerationError {
	return NewGenerationError(ErrorCancelled, 0, "", "", err)
}

func joinedTextDeltas(deltas []string) string {
	var joined []byte
	for _, delta := range deltas {
		joined = append(joined, delta...)
	}
	return string(joined)
}

func (service *Service) executeToolCall(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	call ModelToolCall,
) (TextMessage, ToolCallStatus, error) {
	toolCtx, cancel := context.WithTimeout(ctx, service.loopLimits.ToolTimeout)
	defer cancel()
	effect := service.registry.InvocationEffect(capability.Invocation{
		Name: call.Name, Input: call.Arguments,
	})
	requestID := toolCallRequestID(run, call, effect.MayWrite())
	if _, err := service.repository.StartToolCall(
		toolCtx,
		actor.UserID,
		run.ID,
		run.WorkerLeaseToken,
		call.ID,
		requestID,
	); err != nil {
		return TextMessage{}, ToolCallFailed, err
	}
	result, err := service.executor.Execute(
		toolCtx,
		capability.CallContext{
			Actor:          actor,
			ThreadID:       run.ThreadID,
			RunID:          run.ID,
			InputMessageID: run.InputMessageID,
			ToolCallID:     call.ID,
			RequestID:      requestID,
		},
		capability.Invocation{Name: call.Name, Input: call.Arguments},
	)
	if err != nil {
		persistCtx, persistCancel := runPersistenceContext(ctx)
		defer persistCancel()
		if _, persistErr := service.repository.FailToolCall(
			persistCtx,
			actor.UserID,
			run.ID,
			run.WorkerLeaseToken,
			call.ID,
			ToolCallFailed,
			capability.ErrorCategory(err),
		); persistErr != nil {
			return TextMessage{}, ToolCallFailed, persistErr
		}
		// 工具自身失败属于模型可处理结果，回填稳定分类后让模型决定重试、换工具或解释。
		return toolFailureMessage(call.ID, err), ToolCallFailed, nil
	}
	content, err := marshalToolResult(result, service.loopLimits.MaxToolResultBytes)
	if err != nil {
		persistCtx, persistCancel := runPersistenceContext(ctx)
		defer persistCancel()
		if _, persistErr := service.repository.FailToolCall(
			persistCtx,
			actor.UserID,
			run.ID,
			run.WorkerLeaseToken,
			call.ID,
			ToolCallFailed,
			"internal",
		); persistErr != nil {
			return TextMessage{}, ToolCallFailed, persistErr
		}
		return toolFailureMessage(call.ID, err), ToolCallFailed, nil
	}
	persistCtx, persistCancel := runPersistenceContext(ctx)
	defer persistCancel()
	if _, err := service.repository.CompleteToolCall(
		persistCtx,
		actor.UserID,
		run.ID,
		run.WorkerLeaseToken,
		call.ID,
		json.RawMessage(content),
		toolSourceRefs(result.SourceRefs),
		result.ClientActions,
	); err != nil {
		return TextMessage{}, ToolCallFailed, err
	}
	return TextMessage{
		Role:       TextRoleTool,
		Content:    content,
		ToolCallID: call.ID,
	}, ToolCallSucceeded, nil
}

func (service *Service) saveToolCallProposed(
	ctx context.Context,
	run Run,
	call ModelToolCall,
) error {
	_, err := service.repository.ProposeToolCall(
		ctx,
		ToolCall{
			ID:            call.ID,
			RunID:         run.ID,
			OwnerID:       run.OwnerID,
			ThreadID:      run.ThreadID,
			Name:          call.Name,
			SchemaVersion: toolSchemaVersionV1,
			Input:         call.Arguments,
			Status:        ToolCallProposed,
		},
		run.WorkerLeaseToken,
	)
	return err
}

func (service *Service) markToolCallRejected(
	ctx context.Context,
	run Run,
	toolCallID string,
	errorCategory string,
) error {
	_, err := service.repository.FailToolCall(
		ctx,
		run.OwnerID,
		run.ID,
		run.WorkerLeaseToken,
		toolCallID,
		ToolCallRejected,
		errorCategory,
	)
	return err
}

func toolSourceRefs(refs []capability.SourceRef) []ToolSourceRef {
	result := make([]ToolSourceRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ToolSourceRef{
			Type: ref.Type,
			ID:   ref.ID,
		})
	}
	return result
}

func exposedToolNameList(definitions []ToolDefinition) []string {
	if len(definitions) == 0 {
		return nil
	}
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func toolSchemaHashes(definitions []ToolDefinition) map[string]string {
	if len(definitions) == 0 {
		return nil
	}
	hashes := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		raw, err := json.Marshal(definition.InputSchema)
		if err != nil {
			hashes[definition.Name] = "sha256:error"
			continue
		}
		sum := sha256.Sum256(raw)
		hashes[definition.Name] = fmt.Sprintf(
			"sha256:%x",
			sum[:],
		)
	}
	return hashes
}

func (service *Service) logRunReceived(
	run Run,
	input string,
	request TextRequest,
) {
	if service.logger == nil {
		return
	}
	imageCount := textRequestImageCount(request)
	attrs := []any{
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"message_id", run.InputMessageID,
		"input_length", utf8.RuneCountInString(input),
		"image_count", imageCount,
		"estimated_visual_tokens", imageCount * 2048,
	}
	if service.logOptions.LogUserInput {
		attrs = append(
			attrs,
			"input_preview",
			inputPreview(input, service.logOptions.InputPreviewMax),
		)
	}
	service.logger.Info("agent.run.received", attrs...)
}

func (service *Service) logContextDegradation(
	run Run,
	source string,
	status string,
) {
	if service.logger == nil {
		return
	}
	service.logger.Warn(
		"agent.context.degraded",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"source", source,
		"status", status,
	)
}

func textRequestImageCount(request TextRequest) int {
	count := 0
	for _, message := range request.Messages {
		for _, part := range message.ContentParts {
			if part.Kind == ContentPartImageURL {
				count++
			}
		}
	}
	return count
}

func (service *Service) logLoopIteration(
	run Run,
	iteration int,
	toolCallCount int,
) {
	if service.logger == nil {
		return
	}
	service.logger.Debug(
		"agent.loop.iteration",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"iteration", iteration+1,
		"tool_call_count", toolCallCount,
		"remaining_budget", service.loopLimits.MaxToolCalls-toolCallCount,
	)
}

func (service *Service) logRoutingDecision(
	run Run,
	decision string,
	selectedTools []string,
	reasonCode string,
	summary string,
	iteration int,
) {
	if service.logger == nil {
		return
	}
	service.logger.Info(
		"agent.routing.decision",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"decision", decision,
		"selected_tools", selectedTools,
		"reason_code", reasonCode,
		"reason_summary", truncateString(summary, 200),
		"iteration", iteration,
	)
}

func (service *Service) logRunCompleted(
	run Run,
	decision string,
	iterations int,
	toolCallCount int,
	startedAt time.Time,
	output string,
) {
	if service.logger == nil {
		return
	}
	service.logger.Info(
		"agent.run.completed",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"decision", decision,
		"iterations", iterations,
		"tool_call_count", toolCallCount,
		"duration_ms", durationSince(startedAt).Milliseconds(),
		"output_length", utf8.RuneCountInString(output),
	)
}

func (service *Service) logRunFailed(
	run Run,
	kind string,
	retryable bool,
	startedAt time.Time,
) {
	if service.logger == nil {
		return
	}
	service.logger.Error(
		"agent.run.failed",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"failure_category", kind,
		"retryable", retryable,
		"duration_ms", durationSince(startedAt).Milliseconds(),
	)
}

func (service *Service) logGenerationFailureDetail(run Run, err error) {
	if service.logger == nil || err == nil {
		return
	}
	cause := err
	for errors.Unwrap(cause) != nil {
		cause = errors.Unwrap(cause)
	}
	service.logger.Error(
		"agent.run.generation_failed",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"detail", cause.Error(),
	)
}

func (service *Service) logInvalidModelResult(
	run Run,
	result TextResult,
) {
	if service.logger == nil {
		return
	}
	toolCallsValid := true
	for _, call := range result.ToolCalls {
		if ValidateToolCall(call) != nil {
			toolCallsValid = false
			break
		}
	}
	service.logger.Error(
		"agent.loop.invalid_model_result",
		"run_id", run.ID,
		"thread_id", run.ThreadID,
		"id_valid", ValidOpaqueID(result.ID),
		"provider", result.Provider,
		"provider_matches", result.Provider == service.configuration.Provider,
		"model", result.Model,
		"model_matches", result.Model == service.configuration.Model,
		"finish_reason", result.FinishReason,
		"content_length", utf8.RuneCountInString(result.Content),
		"tool_call_count", len(result.ToolCalls),
		"tool_calls_valid", toolCallsValid,
		"input_tokens", result.Usage.InputTokens,
		"output_tokens", result.Usage.OutputTokens,
		"total_tokens", result.Usage.TotalTokens,
		"output_token_limit", service.configuration.MaxOutputTokens,
	)
}

func (service *Service) persistFailure(
	ctx context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	kind string,
	retryable bool,
) (Run, error) {
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	return service.repository.Fail(
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

func classifyRunFailure(err error) (string, bool) {
	var terminal *loopFailure
	if errors.As(err, &terminal) {
		return terminal.kind, false
	}
	var generationError *GenerationError
	if errors.As(err, &generationError) {
		return string(generationError.Kind), generationError.Retryable()
	}
	return string(ErrorProviderUnavailable), true
}

func validFinalTextResult(result TextResult) bool {
	return ValidOpaqueID(result.ID) &&
		ValidProviderID(result.Provider) &&
		ValidModelID(result.Model) &&
		(result.FinishReason == "stop" || result.FinishReason == "length") &&
		validTokenUsage(result.Usage) &&
		conversation.ValidMessageContent(result.Content) &&
		len(result.ToolCalls) == 0
}

func validLoopTextResult(result TextResult) bool {
	if !ValidOpaqueID(result.ID) ||
		!ValidProviderID(result.Provider) ||
		!ValidModelID(result.Model) ||
		!validTokenUsage(result.Usage) {
		return false
	}
	switch result.FinishReason {
	case "stop", "length":
		return conversation.ValidMessageContent(result.Content) &&
			len(result.ToolCalls) == 0
	case "tool_calls":
		if len(result.ToolCalls) == 0 {
			return false
		}
		for _, call := range result.ToolCalls {
			if err := ValidateToolCall(call); err != nil {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validTokenUsage(usage TokenUsage) bool {
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
	configuration Configuration,
) bool {
	return run.RequestedProvider == configuration.Provider &&
		run.RequestedModel == configuration.Model &&
		run.MaxOutputTokens == configuration.MaxOutputTokens &&
		run.MaxInputCharacters == configuration.MaxInputCharacters
}

func defaultLoopLimits() LoopLimits {
	return LoopLimits{
		MaxIterations:      defaultMaxIterations,
		MaxToolCalls:       MaxToolCallsPerRun,
		ToolTimeout:        defaultToolTimeout,
		LoopTimeout:        defaultLoopTimeout,
		MaxToolResultBytes: defaultMaxToolResult,
	}
}

func normalizeLoopLimits(limits LoopLimits) LoopLimits {
	defaults := defaultLoopLimits()
	if limits.MaxIterations == 0 {
		limits.MaxIterations = defaults.MaxIterations
	}
	if limits.MaxToolCalls == 0 {
		limits.MaxToolCalls = defaults.MaxToolCalls
	}
	if limits.ToolTimeout == 0 {
		limits.ToolTimeout = defaults.ToolTimeout
	}
	if limits.LoopTimeout == 0 {
		limits.LoopTimeout = defaults.LoopTimeout
	}
	if limits.MaxToolResultBytes == 0 {
		limits.MaxToolResultBytes = defaults.MaxToolResultBytes
	}
	return limits
}

// toolCallRequestID keeps every write tool stable across retries of the same
// trusted input Message. Read calls remain unique to one Run and model call.
func toolCallRequestID(run Run, call ModelToolCall, mayWrite bool) string {
	seed := run.ID + "\x00" + call.ID
	prefix := "tool-read-"
	if mayWrite {
		seed = run.InputMessageID + "\x00" + call.Name
		prefix = "tool-write-"
	}
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%s%x", prefix, sum[:])
}

func (service *Service) writeToolCallCount(
	calls []ModelToolCall,
) int {
	count := 0
	for _, call := range calls {
		effect := service.registry.InvocationEffect(capability.Invocation{
			Name:  call.Name,
			Input: call.Arguments,
		})
		if effect.MayWrite() {
			count++
		}
	}
	return count
}

func (service *Service) stopLoop(
	run Run,
	kind string,
	selectedTools []string,
	modelIterations int,
) error {
	service.logRoutingDecision(
		run,
		"failed",
		selectedTools,
		kind,
		reasonSummary(kind, "failed"),
		modelIterations,
	)
	return &loopFailure{kind: kind}
}

// hasRepeatedToolCallID 同时检查本批和前序批次，防止模型复用 ID 导致重复执行。
func hasRepeatedToolCallID(
	calls []ModelToolCall,
	seen map[string]struct{},
) bool {
	current := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if _, ok := seen[call.ID]; ok {
			return true
		}
		if _, ok := current[call.ID]; ok {
			return true
		}
		current[call.ID] = struct{}{}
	}
	return false
}

// toolFailureMessage 只向模型暴露稳定错误语义，不泄漏数据库或业务内部错误文本。
func toolFailureMessage(toolCallID string, err error) TextMessage {
	category := capability.ErrorCategory(err)
	message := "tool execution failed"
	switch category {
	case "invalid_input":
		message = "tool arguments are invalid"
	case "unknown_tool":
		message = "tool is not registered"
	case "execution_rejected":
		message = "tool execution was rejected"
	}
	raw, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"category":  category,
			"message":   message,
			"retryable": capability.RetryableError(err),
		},
	})
	return TextMessage{
		Role:       TextRoleTool,
		Content:    string(raw),
		ToolCallID: toolCallID,
	}
}

func lastUserContent(request TextRequest) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		if request.Messages[index].Role == TextRoleUser {
			return request.Messages[index].Content
		}
	}
	return ""
}

// providerToolResult is the only Tool result projection sent back to the
// model. SourceRefs remain separate persistence metadata and must never enter
// the Provider conversation.
type providerToolResult struct {
	Content map[string]any `json:"content"`
}

func marshalToolResult(result capability.Result, maxBytes int) (string, error) {
	raw, err := json.Marshal(providerToolResult{Content: result.Content})
	if err != nil {
		return "", err
	}
	if len(raw) <= maxBytes {
		return string(raw), nil
	}
	return `{"error":{"category":"internal","message":"tool result too large"}}`, nil
}

func exposedToolNames(definitions []ToolDefinition) map[string]struct{} {
	names := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		names[definition.Name] = struct{}{}
	}
	return names
}

func toolExposed(exposed map[string]struct{}, name string) bool {
	_, ok := exposed[name]
	return ok
}
