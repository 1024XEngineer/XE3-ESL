package scoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

type IELTSSpeakingShadowRuntimeConfiguration struct {
	MaxAttempts          int
	LeaseDuration        time.Duration
	AcousticWaitDuration time.Duration
	StrategyRef          string
	PipelineVersion      string
	FullConfigHash       [sha256.Size]byte
	PromptVersion        string
	Provider             string
	Model                string
}

func (configuration IELTSSpeakingShadowRuntimeConfiguration) Valid() bool {
	return configuration.AcousticWaitDuration >= time.Second &&
		configuration.AcousticWaitDuration <= 10*time.Minute &&
		durableConfigurationFromIELTS(configuration).valid(
			ieltsDurableSceneJobSpec,
		)
}

type IELTSSpeakingShadowClaim struct {
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
	Snapshot             evidence.EvidenceSnapshot
	AcousticSnapshot     IELTSAcousticSnapshot
	InputBundleHash      [sha256.Size]byte
}

func (claim IELTSSpeakingShadowClaim) Valid() bool {
	return !claim.AcousticSnapshot.CreatedAt.IsZero() &&
		claim.AcousticSnapshot.ValidFor(claim.Snapshot) &&
		claim.InputBundleHash == IELTSAcousticInputBundleHash(
			claim.Snapshot,
			claim.AcousticSnapshot,
		) && durableClaimFromIELTS(claim).valid(ieltsDurableSceneJobSpec)
}

type IELTSSpeakingShadowFailure struct {
	Code      string
	Retryable bool
}

func (failure IELTSSpeakingShadowFailure) Valid() bool {
	return durableSceneJobFailure{
		Code:      failure.Code,
		Retryable: failure.Retryable,
	}.valid()
}

type IELTSSpeakingShadowRuntimeStatus string

const (
	IELTSSpeakingShadowRuntimePending IELTSSpeakingShadowRuntimeStatus = "PENDING"
	IELTSSpeakingShadowRuntimeRunning IELTSSpeakingShadowRuntimeStatus = "RUNNING"
	IELTSSpeakingShadowRuntimeReady   IELTSSpeakingShadowRuntimeStatus = "READY"
	IELTSSpeakingShadowRuntimeFailed  IELTSSpeakingShadowRuntimeStatus = "FAILED"
)

type IELTSSpeakingShadowReadState struct {
	ModuleStatus   IELTSSpeakingShadowRuntimeStatus
	FullConfigHash [sha256.Size]byte
	Result         *IELTSSpeakingShadowResult
	Failure        *IELTSSpeakingShadowFailure
}

func (state IELTSSpeakingShadowReadState) Valid(
	snapshot *evidence.EvidenceSnapshot,
) bool {
	switch state.ModuleStatus {
	case IELTSSpeakingShadowRuntimePending,
		IELTSSpeakingShadowRuntimeRunning:
		return state.Result == nil && state.Failure == nil
	case IELTSSpeakingShadowRuntimeReady:
		return snapshot != nil &&
			nonZeroDigest(state.FullConfigHash) &&
			state.Result != nil &&
			state.Failure == nil &&
			ValidateIELTSSpeakingShadowResult(
				*snapshot,
				*state.Result,
			) == nil
	case IELTSSpeakingShadowRuntimeFailed:
		return state.Result == nil &&
			nonZeroDigest(state.FullConfigHash) &&
			state.Failure != nil &&
			state.Failure.Valid()
	default:
		return false
	}
}

type IELTSSpeakingShadowRuntimeRepository interface {
	ClaimIELTSSpeakingShadow(
		context.Context,
		IELTSSpeakingShadowRuntimeConfiguration,
	) (IELTSSpeakingShadowClaim, bool, error)
	CompleteIELTSSpeakingShadow(
		context.Context,
		IELTSSpeakingShadowClaim,
		IELTSSpeakingShadowResult,
	) error
	FailIELTSSpeakingShadow(
		context.Context,
		IELTSSpeakingShadowClaim,
		IELTSSpeakingShadowFailure,
		IELTSSpeakingShadowRuntimeConfiguration,
	) (IELTSSpeakingShadowRuntimeStatus, error)
	GetIELTSSpeakingShadowState(
		context.Context,
		string,
		string,
		string,
	) (IELTSSpeakingShadowReadState, error)
}

type IELTSSpeakingShadowSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

type IELTSSpeakingShadowWorker struct {
	repository    IELTSSpeakingShadowRuntimeRepository
	engine        *IELTSSpeakingShadowEngine
	configuration IELTSSpeakingShadowRuntimeConfiguration
	freezer       *IELTSAcousticSnapshotFreezer
}

func NewIELTSSpeakingShadowWorker(
	repository IELTSSpeakingShadowRuntimeRepository,
	engine *IELTSSpeakingShadowEngine,
	configuration IELTSSpeakingShadowRuntimeConfiguration,
) (*IELTSSpeakingShadowWorker, error) {
	if repository == nil || engine == nil || !configuration.Valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	return &IELTSSpeakingShadowWorker{
		repository:    repository,
		engine:        engine,
		configuration: configuration,
	}, nil
}

func NewIELTSSpeakingShadowWorkerWithAcousticSnapshots(
	repository interface {
		IELTSSpeakingShadowRuntimeRepository
		IELTSAcousticSnapshotRepository
	},
	engine *IELTSSpeakingShadowEngine,
	configuration IELTSSpeakingShadowRuntimeConfiguration,
	source IELTSSpeakingAcousticSource,
) (*IELTSSpeakingShadowWorker, error) {
	worker, err := NewIELTSSpeakingShadowWorker(
		repository,
		engine,
		configuration,
	)
	if err != nil {
		return nil, err
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		configuration.AcousticWaitDuration,
	)
	if err != nil {
		return nil, err
	}
	worker.freezer = freezer
	return worker, nil
}

func (worker *IELTSSpeakingShadowWorker) ProcessPending(
	ctx context.Context,
	limit int,
) (IELTSSpeakingShadowSweepResult, error) {
	if worker == nil || worker.repository == nil ||
		worker.engine == nil || ctx == nil {
		return IELTSSpeakingShadowSweepResult{}, evaluation.ErrInvalidRequest
	}
	var freezerErr error
	if worker.freezer != nil {
		_, freezerErr = worker.freezer.ProcessPending(ctx, limit)
		if errors.Is(freezerErr, context.Canceled) ||
			errors.Is(freezerErr, context.DeadlineExceeded) || ctx.Err() != nil {
			return IELTSSpeakingShadowSweepResult{}, freezerErr
		}
	}
	sweep, err := processDurableSceneJobs(
		ctx,
		limit,
		func(
			claimContext context.Context,
		) (durableSceneJobClaim, bool, error) {
			claim, acquired, claimErr :=
				worker.repository.ClaimIELTSSpeakingShadow(
					claimContext,
					worker.configuration,
				)
			return durableClaimFromIELTS(claim),
				acquired,
				claimErr
		},
		func(
			processContext context.Context,
			claim durableSceneJobClaim,
		) (durableSceneJobStatus, error) {
			status, processErr := worker.processClaim(
				processContext,
				ieltsClaimFromDurable(claim),
			)
			return durableSceneJobStatus(status), processErr
		},
	)
	result := IELTSSpeakingShadowSweepResult{
		Claimed:   sweep.Claimed,
		Completed: sweep.Completed,
		Retried:   sweep.Retried,
		Failed:    sweep.Failed,
	}
	if err != nil {
		return result, err
	}
	return result, freezerErr
}

func (worker *IELTSSpeakingShadowWorker) processClaim(
	ctx context.Context,
	claim IELTSSpeakingShadowClaim,
) (IELTSSpeakingShadowRuntimeStatus, error) {
	if !claim.Valid() ||
		!ieltsClaimMatchesRuntime(
			claim,
			worker.configuration,
		) {
		return "", evaluation.ErrInvalidRequest
	}
	result, err := worker.engine.EvaluateWithAcousticSnapshot(
		ctx,
		claim.Snapshot,
		claim.AcousticSnapshot,
	)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if result.Provider != nil &&
		(result.Provider.Provider != claim.Provider ||
			result.Provider.Model != claim.Model) {
		return worker.recordFailure(
			ctx,
			claim,
			ErrInvalidIELTSSpeakingShadow,
		)
	}
	if err := ValidateIELTSSpeakingShadowResult(
		claim.Snapshot,
		result,
	); err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if err := worker.repository.CompleteIELTSSpeakingShadow(
		ctx,
		claim,
		result,
	); err != nil {
		return "", err
	}
	return IELTSSpeakingShadowRuntimeReady, nil
}

func (worker *IELTSSpeakingShadowWorker) recordFailure(
	ctx context.Context,
	claim IELTSSpeakingShadowClaim,
	cause error,
) (IELTSSpeakingShadowRuntimeStatus, error) {
	failure := classifyIELTSSpeakingShadowFailure(cause)
	if rejectionStage := ieltsProviderRejectionStage(cause); rejectionStage != "" {
		slog.Warn(
			"IELTS Speaking provider response rejected",
			"failure_code",
			failure.Code,
			"rejection_stage",
			rejectionStage,
		)
	}
	status, err := worker.repository.FailIELTSSpeakingShadow(
		ctx,
		claim,
		failure,
		worker.configuration,
	)
	if err != nil {
		return "", err
	}
	if status != IELTSSpeakingShadowRuntimePending &&
		status != IELTSSpeakingShadowRuntimeFailed {
		return "", evaluation.ErrInvalidRequest
	}
	return status, nil
}

func ieltsProviderRejectionStage(cause error) string {
	switch {
	case errors.Is(cause, errIELTSSpeakingProviderInvalidJSON):
		return "json_decode"
	case errors.Is(cause, errIELTSSpeakingProviderSchemaMismatch):
		return "schema_validation"
	case errors.Is(cause, ErrInvalidIELTSSpeakingShadow):
		return "semantic_validation"
	case errors.Is(cause, evaluation.ErrInvalidRequest):
		return "request_validation"
	default:
		return ""
	}
}

func ieltsClaimMatchesRuntime(
	claim IELTSSpeakingShadowClaim,
	configuration IELTSSpeakingShadowRuntimeConfiguration,
) bool {
	return durableSceneJobConfigurationMatchesClaim(
		durableClaimFromIELTS(claim),
		durableConfigurationFromIELTS(configuration),
		ieltsDurableSceneJobSpec,
	)
}

func classifyIELTSSpeakingShadowFailure(
	cause error,
) IELTSSpeakingShadowFailure {
	switch {
	case errors.Is(cause, errIELTSSpeakingProviderInvalidJSON):
		return IELTSSpeakingShadowFailure{
			Code:      "provider_invalid_json",
			Retryable: false,
		}
	case errors.Is(cause, errIELTSSpeakingProviderSchemaMismatch):
		return IELTSSpeakingShadowFailure{
			Code:      "provider_schema_mismatch",
			Retryable: false,
		}
	case errors.Is(cause, ErrInvalidIELTSSpeakingShadow),
		errors.Is(cause, evaluation.ErrInvalidRequest):
		return IELTSSpeakingShadowFailure{
			Code:      "provider_invalid_response",
			Retryable: false,
		}
	case errors.Is(cause, context.Canceled):
		return IELTSSpeakingShadowFailure{
			Code:      "provider_canceled",
			Retryable: true,
		}
	case errors.Is(cause, context.DeadlineExceeded):
		return IELTSSpeakingShadowFailure{
			Code:      "provider_timeout",
			Retryable: true,
		}
	}
	var generationError GenerationFailure
	if errors.As(cause, &generationError) {
		code := strings.ToLower(generationError.StableCategory())
		code = strings.NewReplacer("-", "_", " ", "_").Replace(code)
		if code == "timeout" {
			code = "provider_timeout"
		}
		if !durableSceneJobFailureCodePattern.MatchString(code) {
			code = "provider_error"
		}
		return IELTSSpeakingShadowFailure{
			Code:      code,
			Retryable: generationError.Retryable(),
		}
	}
	return IELTSSpeakingShadowFailure{
		Code:      "dependency_error",
		Retryable: true,
	}
}

func DecodeIELTSSpeakingShadowResult(
	payload []byte,
) (IELTSSpeakingShadowResult, error) {
	var result IELTSSpeakingShadowResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil ||
		ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingShadowResult{},
			ErrInvalidIELTSSpeakingShadow
	}
	return result, nil
}
