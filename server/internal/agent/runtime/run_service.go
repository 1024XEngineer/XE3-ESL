package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/command"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	runPersistenceTimeout  = 5 * time.Second
	maxPersistedTokenCount = 1<<31 - 1
	defaultMaxIterations   = 3
	defaultMaxToolCalls    = 4
	defaultMaxWriteCalls   = 1
	defaultToolTimeout     = 5 * time.Second
	defaultLoopTimeout     = 25 * time.Second
	defaultMaxToolResult   = 16 * 1024
	loopFallbackID         = "agent-loop-fallback"
	toolSchemaVersionV1    = "tool-schema-v1"
)

type RunService struct {
	repository    RunRepository
	multimodal    core.MultimodalRunRepository
	assembler     *ContextAssembler
	generator     ai.TextGenerator
	configuration RunConfiguration
	registry      *tool.Registry
	executor      *tool.Executor
	loopLimits    LoopLimits
	commands      *command.Router
	logger        *slog.Logger
	logOptions    LogOptions
}

type LoopLimits struct {
	MaxIterations      int
	MaxToolCalls       int
	MaxWriteToolCalls  int
	ToolTimeout        time.Duration
	LoopTimeout        time.Duration
	MaxToolResultBytes int
}

type LogOptions struct {
	LogUserInput    bool
	LogToolPayloads bool
	InputPreviewMax int
}

type RunServiceOption func(*RunService) error

func NewRunService(
	repository RunRepository,
	assembler *ContextAssembler,
	generator ai.TextGenerator,
	configuration RunConfiguration,
	options ...RunServiceOption,
) (*RunService, error) {
	if repository == nil || assembler == nil || generator == nil ||
		!core.ValidRunConfiguration(configuration) {
		return nil, errors.New("agent: run dependency or configuration is invalid")
	}
	service := &RunService{
		repository:    repository,
		assembler:     assembler,
		generator:     generator,
		configuration: configuration,
		loopLimits:    defaultLoopLimits(),
		logOptions:    normalizeLogOptions(LogOptions{}),
	}
	if multimodal, ok := repository.(core.MultimodalRunRepository); ok {
		service.multimodal = multimodal
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

func WithToolRegistry(registry *tool.Registry) RunServiceOption {
	return func(service *RunService) error {
		if registry == nil {
			return errors.New("agent: tool registry is required")
		}
		service.registry = registry
		service.executor = tool.NewExecutorWithLogger(
			registry,
			service.logger,
			service.logOptions.LogToolPayloads,
		)
		return nil
	}
}

func WithRunLogger(logger *slog.Logger) RunServiceOption {
	return func(service *RunService) error {
		if logger == nil {
			return errors.New("agent: run logger is required")
		}
		service.logger = logger
		if service.registry != nil {
			service.executor = tool.NewExecutorWithLogger(
				service.registry,
				logger,
				service.logOptions.LogToolPayloads,
			)
		}
		return nil
	}
}

func WithLogOptions(options LogOptions) RunServiceOption {
	return func(service *RunService) error {
		service.logOptions = normalizeLogOptions(options)
		if service.registry != nil {
			service.executor = tool.NewExecutorWithLogger(
				service.registry,
				service.logger,
				service.logOptions.LogToolPayloads,
			)
		}
		return nil
	}
}

func WithLoopLimits(limits LoopLimits) RunServiceOption {
	return func(service *RunService) error {
		normalized := normalizeLoopLimits(limits)
		if normalized.MaxIterations <= 0 ||
			normalized.MaxToolCalls <= 0 ||
			normalized.MaxWriteToolCalls < 0 ||
			normalized.ToolTimeout <= 0 ||
			normalized.LoopTimeout <= 0 ||
			normalized.MaxToolResultBytes <= 0 {
			return errors.New("agent: loop limits are invalid")
		}
		service.loopLimits = normalized
		return nil
	}
}

func WithCommandRouter(router *command.Router) RunServiceOption {
	return func(service *RunService) error {
		if router == nil {
			return errors.New("agent: command router is required")
		}
		service.commands = router
		return nil
	}
}

func (service *RunService) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
) (RunSubmission, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return RunSubmission{}, ErrNotFound
	}
	if !core.ValidClientMessageID(clientMessageID) ||
		!core.ValidMessageContent(content) {
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

func (service *RunService) SubmitMultimodal(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	clientMessageID string,
	content string,
	imageAssetIDs []string,
) (RunSubmission, error) {
	if !actor.Valid() || !core.ValidUUID(threadID) {
		return RunSubmission{}, ErrNotFound
	}
	if service.multimodal == nil ||
		!core.ValidClientMessageID(clientMessageID) ||
		!core.ValidMessageContent(content) ||
		!validMultimodalImageIDs(imageAssetIDs) {
		return RunSubmission{}, ErrInvalidRequest
	}
	submission, err := service.multimodal.CreateInitialMultimodalRun(
		ctx,
		actor.UserID,
		threadID,
		clientMessageID,
		content,
		imageAssetIDs,
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

func validMultimodalImageIDs(imageAssetIDs []string) bool {
	if len(imageAssetIDs) < 1 ||
		len(imageAssetIDs) > core.MaxImagesPerMessage {
		return false
	}
	seen := make(map[string]struct{}, len(imageAssetIDs))
	for _, assetID := range imageAssetIDs {
		if !core.ValidUUID(assetID) {
			return false
		}
		if _, found := seen[assetID]; found {
			return false
		}
		seen[assetID] = struct{}{}
	}
	return true
}

func (service *RunService) RetryText(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
	retryClientID string,
) (RunRetry, error) {
	if !actor.Valid() || !core.ValidUUID(runID) {
		return RunRetry{}, ErrNotFound
	}
	if !core.ValidClientMessageID(retryClientID) {
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
	if !actor.Valid() || !core.ValidUUID(runID) {
		return Run{}, ErrNotFound
	}
	return service.repository.FindRun(ctx, actor.UserID, runID)
}

func (service *RunService) GetContextManifest(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) (ContextManifest, error) {
	if !actor.Valid() || !core.ValidUUID(runID) {
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

func (service *RunService) GetToolCalls(
	ctx context.Context,
	actor requestcontext.Actor,
	runID string,
) ([]ToolCallRecord, error) {
	if !actor.Valid() || !core.ValidUUID(runID) {
		return nil, ErrNotFound
	}
	if _, err := service.repository.FindRun(
		ctx,
		actor.UserID,
		runID,
	); err != nil {
		return nil, err
	}
	return service.repository.ListToolCalls(ctx, actor.UserID, runID)
}

func (service *RunService) RecoverInterruptedRuns(
	ctx context.Context,
) (int64, error) {
	return service.repository.RecoverInterruptedRuns(ctx)
}

// ProcessPending resumes a Run that was atomically created by another
// Agent-owned input workflow, such as voice-candidate confirmation. It does
// not create or mutate the input Message itself.
func (service *RunService) ProcessPending(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
) (Run, error) {
	if ctx == nil || !actor.Valid() ||
		run.OwnerID != actor.UserID ||
		!core.ValidUUID(run.ID) ||
		!core.ValidUUID(run.ThreadID) ||
		!core.ValidUUID(run.InputMessageID) {
		return Run{}, ErrInvalidRequest
	}
	return service.process(ctx, actor, run)
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
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			RunFailureConfigurationDrift,
			true,
		)
		service.logRunFailed(
			claimed,
			RunFailureConfigurationDrift,
			true,
			claimed.StartedAt,
		)
		return failed, failErr
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
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
		service.logRunFailed(claimed, kind, retryable, claimed.StartedAt)
		return failed, failErr
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

	result, err := service.generate(ctx, actor, claimed, manifest, request)
	if err != nil {
		kind, retryable := generationFailure(err)
		failed, failErr := service.persistFailure(
			ctx,
			actor.UserID,
			claimed.ID,
			claimed.WorkerLeaseToken,
			kind,
			retryable,
		)
		service.logRunFailed(claimed, kind, retryable, claimed.StartedAt)
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
			string(ai.ErrorInvalidResponse),
			ai.ErrorInvalidResponse.Retryable(),
		)
		service.logRunFailed(
			claimed,
			string(ai.ErrorInvalidResponse),
			ai.ErrorInvalidResponse.Retryable(),
			claimed.StartedAt,
		)
		return failed, failErr
	}
	persistContext, cancel := runPersistenceContext(ctx)
	defer cancel()
	completed, err := service.repository.CompleteRun(
		persistContext,
		actor.UserID,
		claimed.ID,
		claimed.WorkerLeaseToken,
		result.Content,
		result,
	)
	return completed, err
}

func (service *RunService) generate(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	manifest ContextManifest,
	request ai.TextRequest,
) (ai.TextResult, error) {
	if service.registry == nil || service.executor == nil {
		return service.generator.Generate(ctx, request)
	}
	loopCtx, cancel := context.WithTimeout(ctx, service.loopLimits.LoopTimeout)
	defer cancel()
	startedAt := time.Now()

	input := lastUserContent(request)
	service.logRunReceived(run, input, request)
	routing := buildModelToolRouting(service.registry, service.logger, run.ID)
	parsed, explicitCommand, err := service.parseCommand(input)
	if err != nil {
		return fallbackResult(
			service.configuration,
			"我暂时无法识别这条命令，请换成自然语言告诉我你想做什么。",
		), nil
	}

	// 自然语言请求始终拿到 Registry 的全量工具，是否调用完全由模型判断。
	request.Tools = routing.Definitions
	request.ToolChoice = routing.ToolChoice
	applyModelToolSnapshot(&manifest, routing)
	if err := service.saveContextToolSnapshot(loopCtx, manifest); err != nil {
		return ai.TextResult{}, err
	}
	exposed := exposedToolNames(request.Tools)
	toolCalls := 0
	writeCalls := 0
	toolIterations := 0
	modelIterations := 0
	seenToolCallIDs := make(map[string]struct{})
	finalDecision := "direct_response"
	if explicitCommand {
		// 显式命令只负责预先确定第一个工具，执行结果仍进入统一有界循环。
		commandCall := ai.ToolCall{
			ID:        "command-call",
			Name:      parsed.Invocation.Name,
			Arguments: parsed.Invocation.Input,
		}
		if err := service.saveToolCallProposed(loopCtx, run, commandCall); err != nil {
			return ai.TextResult{}, err
		}
		toolMessage, err := service.executeToolCall(
			loopCtx,
			actor,
			run,
			commandCall,
		)
		if err != nil {
			return ai.TextResult{}, err
		}
		request.Messages = append(request.Messages, ai.TextMessage{
			Role:      ai.TextRoleAssistant,
			ToolCalls: []ai.ToolCall{commandCall},
		}, toolMessage)
		seenToolCallIDs[commandCall.ID] = struct{}{}
		toolCalls = 1
		toolIterations = 1
		if definition, ok := service.toolDefinition(commandCall.Name); ok &&
			!definition.ReadOnly {
			writeCalls = 1
		}
		finalDecision = "tool_call_then_response"
	}
	for {
		service.logLoopIteration(run, modelIterations, toolCalls)
		result, err := service.generator.Generate(loopCtx, request)
		if err != nil {
			return ai.TextResult{}, err
		}
		modelIterations++
		if !validLoopTextResult(result) ||
			result.Provider != service.configuration.Provider ||
			result.Model != service.configuration.Model ||
			result.Usage.OutputTokens > service.configuration.MaxOutputTokens {
			return result, nil
		}
		if len(result.ToolCalls) == 0 {
			service.logRoutingDecision(
				run,
				finalDecision,
				nil,
				reasonModelToolSelection,
				reasonSummary(reasonModelToolSelection, finalDecision),
				modelIterations,
			)
			service.logRunCompleted(
				run,
				finalDecision,
				modelIterations,
				toolCalls,
				startedAt,
				result.Content,
			)
			return result, nil
		}
		selected := toolCallNames(result.ToolCalls)
		finalDecision = "tool_call"
		service.logRoutingDecision(
			run,
			finalDecision,
			selected,
			reasonModelToolSelection,
			reasonSummary(reasonModelToolSelection, finalDecision),
			modelIterations,
		)
		// MaxIterations 表示最多执行多少轮工具；达到上限后仍允许模型生成一次最终回复。
		if toolIterations >= service.loopLimits.MaxIterations {
			return service.loopBudgetFallback(
				run,
				"这次对话需要更多步骤才能完成，我先停在这里。请把最重要的一步单独发给我。",
				modelIterations,
				toolCalls,
				startedAt,
			), nil
		}
		if toolCalls+len(result.ToolCalls) > service.loopLimits.MaxToolCalls {
			service.logRoutingDecision(
				run,
				"budget_exhausted",
				selected,
				"budget_exhausted",
				reasonSummary("budget_exhausted", "budget_exhausted"),
				modelIterations,
			)
			result := fallbackResult(
				service.configuration,
				"这次需要调用的工具太多了，我先暂停一下。请把需求拆成一个更具体的问题。",
			)
			service.logRunCompleted(
				run,
				"budget_exhausted",
				modelIterations,
				toolCalls,
				startedAt,
				result.Content,
			)
			return result, nil
		}
		if duplicateID := repeatedToolCallID(
			result.ToolCalls,
			seenToolCallIDs,
		); duplicateID != "" {
			result := fallbackResult(
				service.configuration,
				"模型重复提交了同一个工具调用，我已停止执行以避免重复操作。",
			)
			service.logRunCompleted(
				run,
				"duplicate_tool_call",
				modelIterations,
				toolCalls,
				startedAt,
				result.Content,
			)
			return result, nil
		}
		// 先检查整批调用，防止同一批写操作只执行一部分后才发现预算不足。
		if writeCalls+service.writeToolCallCount(result.ToolCalls) >
			service.loopLimits.MaxWriteToolCalls {
			result := fallbackResult(
				service.configuration,
				"这次已经达到写操作上限，我先不继续执行新的写操作。",
			)
			service.logRunCompleted(
				run,
				"budget_exhausted",
				modelIterations,
				toolCalls,
				startedAt,
				result.Content,
			)
			return result, nil
		}
		request.Messages = append(request.Messages, ai.TextMessage{
			Content:   result.Content,
			Role:      ai.TextRoleAssistant,
			ToolCalls: result.ToolCalls,
		})
		toolIterations++
		toolCalls += len(result.ToolCalls)
		for _, call := range result.ToolCalls {
			seenToolCallIDs[call.ID] = struct{}{}
			if err := service.saveToolCallProposed(
				loopCtx,
				run,
				call,
			); err != nil {
				return ai.TextResult{}, err
			}
			if !toolExposed(exposed, call.Name) {
				if err := service.markToolCallRejected(
					loopCtx,
					run,
					call.ID,
					"unknown_tool",
				); err != nil {
					return ai.TextResult{}, err
				}
				request.Messages = append(
					request.Messages,
					toolFailureMessage(call.ID, tool.ErrUnknownTool),
				)
				continue
			}
			definition, ok := service.toolDefinition(call.Name)
			if !ok {
				if err := service.markToolCallRejected(
					loopCtx,
					run,
					call.ID,
					"unknown_tool",
				); err != nil {
					return ai.TextResult{}, err
				}
				request.Messages = append(
					request.Messages,
					toolFailureMessage(call.ID, tool.ErrUnknownTool),
				)
				continue
			}
			if !definition.ReadOnly {
				writeCalls++
			}
			toolMessage, err := service.executeToolCall(
				loopCtx,
				actor,
				run,
				call,
			)
			if err != nil {
				return ai.TextResult{}, err
			}
			request.Messages = append(request.Messages, toolMessage)
		}
		request.ToolChoice = ai.ToolChoice{Mode: ai.ToolChoiceAuto}
		finalDecision = "tool_call_then_response"
	}
}

func (service *RunService) executeToolCall(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	call ai.ToolCall,
) (ai.TextMessage, error) {
	toolCtx, cancel := context.WithTimeout(ctx, service.loopLimits.ToolTimeout)
	defer cancel()
	requestID := toolCallRequestID(run.ID, call.ID)
	if service.repository != nil {
		if _, err := service.repository.MarkToolCallRunning(
			toolCtx,
			actor.UserID,
			run.ID,
			call.ID,
			requestID,
		); err != nil {
			return ai.TextMessage{}, err
		}
	}
	result, err := service.executor.Execute(
		toolCtx,
		tool.CallContext{
			Actor:      actor,
			ThreadID:   run.ThreadID,
			RunID:      run.ID,
			ToolCallID: call.ID,
			RequestID:  requestID,
		},
		tool.Invocation{Name: call.Name, Input: call.Arguments},
	)
	if err != nil {
		if service.repository != nil {
			persistCtx, persistCancel := runPersistenceContext(ctx)
			defer persistCancel()
			if _, persistErr := service.repository.MarkToolCallFailed(
				persistCtx,
				actor.UserID,
				run.ID,
				call.ID,
				core.ToolCallStatusFailed,
				tool.ErrorCategory(err),
			); persistErr != nil {
				return ai.TextMessage{}, persistErr
			}
		}
		// 工具自身失败属于模型可处理结果，回填稳定分类后让模型决定重试、换工具或解释。
		return toolFailureMessage(call.ID, err), nil
	}
	content, err := marshalToolResult(result, service.loopLimits.MaxToolResultBytes)
	if err != nil {
		if service.repository != nil {
			persistCtx, persistCancel := runPersistenceContext(ctx)
			defer persistCancel()
			if _, persistErr := service.repository.MarkToolCallFailed(
				persistCtx,
				actor.UserID,
				run.ID,
				call.ID,
				core.ToolCallStatusFailed,
				"internal",
			); persistErr != nil {
				return ai.TextMessage{}, persistErr
			}
		}
		return toolFailureMessage(call.ID, err), nil
	}
	if service.repository != nil {
		persistCtx, persistCancel := runPersistenceContext(ctx)
		defer persistCancel()
		if _, err := service.repository.MarkToolCallSucceeded(
			persistCtx,
			actor.UserID,
			run.ID,
			call.ID,
			json.RawMessage(content),
			toolSourceRefs(result.SourceRefs),
		); err != nil {
			return ai.TextMessage{}, err
		}
	}
	return ai.TextMessage{
		Role:       ai.TextRoleTool,
		Content:    content,
		ToolCallID: call.ID,
	}, nil
}

func (service *RunService) saveToolCallProposed(
	ctx context.Context,
	run Run,
	call ai.ToolCall,
) error {
	if service.repository == nil {
		return nil
	}
	_, err := service.repository.SaveToolCallProposed(
		ctx,
		core.ToolCallRecord{
			ID:            call.ID,
			RunID:         run.ID,
			OwnerID:       run.OwnerID,
			ThreadID:      run.ThreadID,
			Name:          call.Name,
			SchemaVersion: toolSchemaVersionV1,
			Input:         call.Arguments,
			Status:        core.ToolCallStatusProposed,
		},
	)
	return err
}

func (service *RunService) markToolCallRejected(
	ctx context.Context,
	run Run,
	toolCallID string,
	errorCategory string,
) error {
	if service.repository == nil {
		return nil
	}
	_, err := service.repository.MarkToolCallFailed(
		ctx,
		run.OwnerID,
		run.ID,
		toolCallID,
		core.ToolCallStatusRejected,
		errorCategory,
	)
	return err
}

func (service *RunService) saveContextToolSnapshot(
	ctx context.Context,
	manifest ContextManifest,
) error {
	if service.repository == nil {
		return nil
	}
	_, err := service.repository.SaveContextToolSnapshot(ctx, manifest)
	return err
}

func toolSourceRefs(refs []tool.SourceRef) []core.ToolSourceRef {
	result := make([]core.ToolSourceRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, core.ToolSourceRef{
			Type: ref.Type,
			ID:   ref.ID,
		})
	}
	return result
}

func exposedToolNameList(definitions []ai.ToolDefinition) []string {
	if len(definitions) == 0 {
		return nil
	}
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func toolSchemaHashes(definitions []ai.ToolDefinition) map[string]string {
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

func (service *RunService) logRunReceived(
	run Run,
	input string,
	request ai.TextRequest,
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

func textRequestImageCount(request ai.TextRequest) int {
	count := 0
	for _, message := range request.Messages {
		for _, part := range message.ContentParts {
			if part.Kind == ai.ContentPartImageURL {
				count++
			}
		}
	}
	return count
}

func (service *RunService) logLoopIteration(
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

func (service *RunService) logRoutingDecision(
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

func (service *RunService) logRunCompleted(
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

func (service *RunService) logRunFailed(
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

func validFinalTextResult(result ai.TextResult) bool {
	return core.ValidModelID(result.ID) &&
		core.ValidProviderID(result.Provider) &&
		core.ValidModelID(result.Model) &&
		(result.FinishReason == "stop" || result.FinishReason == "length") &&
		validTokenUsage(result.Usage) &&
		core.ValidMessageContent(result.Content) &&
		len(result.ToolCalls) == 0
}

func validLoopTextResult(result ai.TextResult) bool {
	if !core.ValidModelID(result.ID) ||
		!core.ValidProviderID(result.Provider) ||
		!core.ValidModelID(result.Model) ||
		!validTokenUsage(result.Usage) {
		return false
	}
	switch result.FinishReason {
	case "stop", "length":
		return core.ValidMessageContent(result.Content) &&
			len(result.ToolCalls) == 0
	case "tool_calls":
		if len(result.ToolCalls) == 0 {
			return false
		}
		for _, call := range result.ToolCalls {
			if err := ai.ValidateToolCall(call); err != nil {
				return false
			}
		}
		return true
	default:
		return false
	}
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

func defaultLoopLimits() LoopLimits {
	return LoopLimits{
		MaxIterations:      defaultMaxIterations,
		MaxToolCalls:       defaultMaxToolCalls,
		MaxWriteToolCalls:  defaultMaxWriteCalls,
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
	if limits.MaxWriteToolCalls == 0 {
		limits.MaxWriteToolCalls = defaults.MaxWriteToolCalls
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

func (service *RunService) parseCommand(input string) (command.Parsed, bool, error) {
	if service.commands == nil {
		return command.Parsed{}, false, nil
	}
	return service.commands.Parse(input)
}

func (service *RunService) toolDefinition(name string) (tool.Definition, bool) {
	if service.registry == nil {
		return tool.Definition{}, false
	}
	registered, ok := service.registry.Get(name)
	if !ok {
		return tool.Definition{}, false
	}
	return registered.Definition(), true
}

// toolCallRequestID 在同一 Run 重放同一个 Tool Call 时保持不变，供写工具做幂等去重。
func toolCallRequestID(runID string, toolCallID string) string {
	return runID + "-" + toolCallID
}

func (service *RunService) writeToolCallCount(
	calls []ai.ToolCall,
) int {
	count := 0
	for _, call := range calls {
		definition, ok := service.toolDefinition(call.Name)
		if ok && !definition.ReadOnly {
			count++
		}
	}
	return count
}

func (service *RunService) loopBudgetFallback(
	run Run,
	content string,
	modelIterations int,
	toolCalls int,
	startedAt time.Time,
) ai.TextResult {
	service.logRoutingDecision(
		run,
		"budget_exhausted",
		nil,
		"budget_exhausted",
		reasonSummary("budget_exhausted", "budget_exhausted"),
		modelIterations,
	)
	result := fallbackResult(service.configuration, content)
	service.logRunCompleted(
		run,
		"budget_exhausted",
		modelIterations,
		toolCalls,
		startedAt,
		result.Content,
	)
	return result
}

// repeatedToolCallID 同时检查本批和前序批次，防止模型复用 ID 导致重复执行。
func repeatedToolCallID(
	calls []ai.ToolCall,
	seen map[string]struct{},
) string {
	current := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if _, ok := seen[call.ID]; ok {
			return call.ID
		}
		if _, ok := current[call.ID]; ok {
			return call.ID
		}
		current[call.ID] = struct{}{}
	}
	return ""
}

// toolFailureMessage 只向模型暴露稳定错误语义，不泄漏数据库或业务内部错误文本。
func toolFailureMessage(toolCallID string, err error) ai.TextMessage {
	category := tool.ErrorCategory(err)
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
			"retryable": tool.RetryableError(err),
		},
	})
	return ai.TextMessage{
		Role:       ai.TextRoleTool,
		Content:    string(raw),
		ToolCallID: toolCallID,
	}
}

func lastUserContent(request ai.TextRequest) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		if request.Messages[index].Role == ai.TextRoleUser {
			return request.Messages[index].Content
		}
	}
	return ""
}

func marshalToolResult(result tool.Result, maxBytes int) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if len(raw) <= maxBytes {
		return string(raw), nil
	}
	return `{"error":{"category":"internal","message":"tool result too large"}}`, nil
}

func exposedToolNames(definitions []ai.ToolDefinition) map[string]struct{} {
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

func fallbackResult(
	configuration RunConfiguration,
	content string,
) ai.TextResult {
	return ai.TextResult{
		ID:           loopFallbackID,
		Provider:     configuration.Provider,
		Model:        configuration.Model,
		Content:      content,
		FinishReason: "stop",
		Usage:        ai.TokenUsage{},
	}
}
