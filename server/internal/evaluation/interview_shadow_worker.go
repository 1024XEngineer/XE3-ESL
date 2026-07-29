package evaluation

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const maxInterviewShadowSweepLimit = 20

var interviewShadowFailureCodePattern = regexp.MustCompile(
	`^[a-z][a-z0-9_.:-]{0,127}$`,
)

type InterviewShadowRuntimeConfiguration struct {
	MaxAttempts     int
	LeaseDuration   time.Duration
	StrategyRef     string
	PipelineVersion string
	FullConfigHash  [sha256.Size]byte
	PromptVersion   string
	Provider        string
	Model           string
}

func (configuration InterviewShadowRuntimeConfiguration) Valid() bool {
	return configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.StrategyRef == InterviewShadowStrategyRef &&
		configuration.PipelineVersion == InterviewShadowPipelineVersion &&
		nonZeroDigest(configuration.FullConfigHash) &&
		configuration.PromptVersion == InterviewShadowPromptVersion &&
		validRuntimeLineage(configuration.Provider) &&
		validRuntimeLineage(configuration.Model)
}

type InterviewShadowClaim struct {
	OutboxID             string
	ModuleRunID          string
	EvaluationID         string
	EvaluationRevisionID string
	OwnerUserID          string
	Revision             int
	StrategyRef          string
	PipelineVersion      string
	AttemptCount         int
	FencingToken         int64
	LeaseExpiresAt       time.Time
	FullConfigHash       [sha256.Size]byte
	PromptVersion        string
	Provider             string
	Model                string
	Snapshot             EvidenceSnapshot
}

func (claim InterviewShadowClaim) Valid() bool {
	return validUUID(claim.OutboxID) &&
		validUUID(claim.ModuleRunID) &&
		validUUID(claim.EvaluationID) &&
		validUUID(claim.EvaluationRevisionID) &&
		validUUID(claim.OwnerUserID) &&
		claim.Revision >= 1 &&
		claim.StrategyRef == InterviewShadowStrategyRef &&
		claim.PipelineVersion == InterviewShadowPipelineVersion &&
		claim.AttemptCount >= 1 &&
		claim.FencingToken >= 1 &&
		!claim.LeaseExpiresAt.IsZero() &&
		nonZeroDigest(claim.FullConfigHash) &&
		claim.PromptVersion == InterviewShadowPromptVersion &&
		validRuntimeLineage(claim.Provider) &&
		validRuntimeLineage(claim.Model) &&
		claim.Snapshot.Valid() &&
		claim.Snapshot.OwnerUserID == claim.OwnerUserID &&
		claim.Snapshot.Scope == ScopeSession &&
		claim.Snapshot.SceneType == SceneInterview
}

type InterviewShadowFailure struct {
	Code      string
	Retryable bool
}

func (failure InterviewShadowFailure) Valid() bool {
	return interviewShadowFailureCodePattern.MatchString(failure.Code)
}

type InterviewShadowRuntimeStatus string

const (
	InterviewShadowRuntimePending InterviewShadowRuntimeStatus = "PENDING"
	InterviewShadowRuntimeRunning InterviewShadowRuntimeStatus = "RUNNING"
	InterviewShadowRuntimeReady   InterviewShadowRuntimeStatus = "READY"
	InterviewShadowRuntimeFailed  InterviewShadowRuntimeStatus = "FAILED"
)

type InterviewShadowReadState struct {
	ModuleStatus   InterviewShadowRuntimeStatus
	FullConfigHash [sha256.Size]byte
	Result         *InterviewShadowResult
	Failure        *InterviewShadowFailure
}

func (state InterviewShadowReadState) Valid(snapshot *EvidenceSnapshot) bool {
	switch state.ModuleStatus {
	case InterviewShadowRuntimePending, InterviewShadowRuntimeRunning:
		return state.Result == nil && state.Failure == nil
	case InterviewShadowRuntimeReady:
		return snapshot != nil &&
			nonZeroDigest(state.FullConfigHash) &&
			state.Result != nil &&
			state.Failure == nil &&
			ValidateInterviewShadowResult(*snapshot, *state.Result) == nil
	case InterviewShadowRuntimeFailed:
		return state.Result == nil &&
			nonZeroDigest(state.FullConfigHash) &&
			state.Failure != nil &&
			state.Failure.Valid()
	default:
		return false
	}
}

type InterviewShadowRuntimeRepository interface {
	ClaimInterviewShadow(
		context.Context,
		InterviewShadowRuntimeConfiguration,
	) (InterviewShadowClaim, bool, error)
	CompleteInterviewShadow(
		context.Context,
		InterviewShadowClaim,
		InterviewShadowResult,
	) error
	FailInterviewShadow(
		context.Context,
		InterviewShadowClaim,
		InterviewShadowFailure,
		InterviewShadowRuntimeConfiguration,
	) (InterviewShadowRuntimeStatus, error)
	GetInterviewShadowState(
		context.Context,
		string,
		string,
		string,
	) (InterviewShadowReadState, error)
}

type InterviewShadowSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

type InterviewShadowWorker struct {
	repository    InterviewShadowRuntimeRepository
	engine        *InterviewShadowEngine
	configuration InterviewShadowRuntimeConfiguration
}

func NewInterviewShadowWorker(
	repository InterviewShadowRuntimeRepository,
	engine *InterviewShadowEngine,
	configuration InterviewShadowRuntimeConfiguration,
) (*InterviewShadowWorker, error) {
	if repository == nil || engine == nil || !configuration.Valid() {
		return nil, ErrInvalidRequest
	}
	return &InterviewShadowWorker{
		repository:    repository,
		engine:        engine,
		configuration: configuration,
	}, nil
}

func (worker *InterviewShadowWorker) ProcessPending(
	ctx context.Context,
	limit int,
) (InterviewShadowSweepResult, error) {
	if worker == nil || worker.repository == nil || worker.engine == nil ||
		ctx == nil || limit < 1 || limit > maxInterviewShadowSweepLimit {
		return InterviewShadowSweepResult{}, ErrInvalidRequest
	}
	var sweep InterviewShadowSweepResult
	for sweep.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return sweep, err
		}
		claim, acquired, err := worker.repository.ClaimInterviewShadow(
			ctx,
			worker.configuration,
		)
		if err != nil {
			return sweep, err
		}
		if !acquired {
			return sweep, nil
		}
		sweep.Claimed++
		status, err := worker.processClaim(ctx, claim)
		if err != nil {
			return sweep, err
		}
		switch status {
		case InterviewShadowRuntimeReady:
			sweep.Completed++
		case InterviewShadowRuntimePending:
			sweep.Retried++
		case InterviewShadowRuntimeFailed:
			sweep.Failed++
		default:
			return sweep, ErrInvalidRequest
		}
	}
	return sweep, nil
}

func (worker *InterviewShadowWorker) processClaim(
	ctx context.Context,
	claim InterviewShadowClaim,
) (InterviewShadowRuntimeStatus, error) {
	if !claim.Valid() || !claimMatchesRuntime(
		claim,
		worker.configuration,
	) {
		return "", ErrInvalidRequest
	}
	result, err := worker.engine.Evaluate(ctx, claim.Snapshot)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if result.Provider != nil &&
		(result.Provider.Provider != claim.Provider ||
			result.Provider.Model != claim.Model) {
		return worker.recordFailure(
			ctx,
			claim,
			ErrInvalidInterviewShadow,
		)
	}
	if err := ValidateInterviewShadowResult(
		claim.Snapshot,
		result,
	); err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if err := worker.repository.CompleteInterviewShadow(
		ctx,
		claim,
		result,
	); err != nil {
		return "", err
	}
	return InterviewShadowRuntimeReady, nil
}

func (worker *InterviewShadowWorker) recordFailure(
	ctx context.Context,
	claim InterviewShadowClaim,
	cause error,
) (InterviewShadowRuntimeStatus, error) {
	failure := classifyInterviewShadowFailure(cause)
	status, err := worker.repository.FailInterviewShadow(
		ctx,
		claim,
		failure,
		worker.configuration,
	)
	if err != nil {
		return "", err
	}
	if status != InterviewShadowRuntimePending &&
		status != InterviewShadowRuntimeFailed {
		return "", ErrInvalidRequest
	}
	return status, nil
}

func claimMatchesRuntime(
	claim InterviewShadowClaim,
	configuration InterviewShadowRuntimeConfiguration,
) bool {
	return claim.StrategyRef == configuration.StrategyRef &&
		claim.PipelineVersion == configuration.PipelineVersion &&
		claim.FullConfigHash == configuration.FullConfigHash &&
		claim.PromptVersion == configuration.PromptVersion &&
		claim.Provider == configuration.Provider &&
		claim.Model == configuration.Model &&
		claim.AttemptCount <= configuration.MaxAttempts
}

func classifyInterviewShadowFailure(cause error) InterviewShadowFailure {
	switch {
	case errors.Is(cause, ErrInvalidInterviewShadow),
		errors.Is(cause, ErrInvalidRequest):
		return InterviewShadowFailure{
			Code:      "provider_invalid_response",
			Retryable: false,
		}
	case errors.Is(cause, context.Canceled):
		return InterviewShadowFailure{
			Code:      "provider_canceled",
			Retryable: true,
		}
	case errors.Is(cause, context.DeadlineExceeded):
		return InterviewShadowFailure{
			Code:      "provider_timeout",
			Retryable: true,
		}
	}
	var generationError *ai.GenerationError
	if errors.As(cause, &generationError) {
		code := strings.ToLower(generationError.StableCategory())
		code = strings.NewReplacer("-", "_", " ", "_").Replace(code)
		if !interviewShadowFailureCodePattern.MatchString(code) {
			code = "provider_error"
		}
		return InterviewShadowFailure{
			Code:      code,
			Retryable: generationError.Retryable(),
		}
	}
	return InterviewShadowFailure{
		Code:      "dependency_error",
		Retryable: true,
	}
}

func validRuntimeLineage(value string) bool {
	return strings.TrimSpace(value) == value &&
		value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00')
}
