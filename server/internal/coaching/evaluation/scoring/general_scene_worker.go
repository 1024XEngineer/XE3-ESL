package scoring

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

var generalSceneTypes = [...]evaluation.SceneType{
	evaluation.SceneIELTSSpeaking,
	evaluation.SceneOverseasDaily,
	evaluation.SceneOverseasWorkplace,
}

type GeneralSceneRuntimeConfiguration struct {
	MaxAttempts     int
	LeaseDuration   time.Duration
	StrategyRef     string
	PipelineVersion string
	FullConfigHash  [sha256.Size]byte
	PromptVersion   string
	Provider        string
	Model           string
}

func (configuration GeneralSceneRuntimeConfiguration) Valid() bool {
	spec, ok := generalSceneDurableJobSpec(evaluation.SceneIELTSSpeaking)
	return ok && durableConfigurationFromGeneralScene(configuration).valid(spec)
}

type GeneralSceneClaim struct {
	OutboxID             string
	ModuleRunID          string
	EvaluationID         string
	EvaluationRevisionID string
	OwnerUserID          string
	Revision             int
	SceneType            evaluation.SceneType
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
}

func (claim GeneralSceneClaim) Valid() bool {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	return ok && claim.SceneType == claim.Snapshot.SceneType &&
		durableClaimFromGeneralScene(claim).valid(spec)
}

type GeneralSceneFailure struct {
	Code      string
	Retryable bool
}

func (failure GeneralSceneFailure) Valid() bool {
	return durableSceneJobFailure(failure).valid()
}

type GeneralSceneRuntimeStatus string

const (
	GeneralSceneRuntimePending GeneralSceneRuntimeStatus = "PENDING"
	GeneralSceneRuntimeRunning GeneralSceneRuntimeStatus = "RUNNING"
	GeneralSceneRuntimeReady   GeneralSceneRuntimeStatus = "READY"
	GeneralSceneRuntimeFailed  GeneralSceneRuntimeStatus = "FAILED"
)

type GeneralSceneRuntimeRepository interface {
	ClaimGeneralScene(
		context.Context,
		evaluation.SceneType,
		GeneralSceneRuntimeConfiguration,
	) (GeneralSceneClaim, bool, error)
	CompleteGeneralScene(
		context.Context,
		GeneralSceneClaim,
		GeneralSceneResult,
	) error
	FailGeneralScene(
		context.Context,
		GeneralSceneClaim,
		GeneralSceneFailure,
		GeneralSceneRuntimeConfiguration,
	) (GeneralSceneRuntimeStatus, error)
}

type GeneralSceneSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

type GeneralSceneWorker struct {
	mu            sync.Mutex
	repository    GeneralSceneRuntimeRepository
	engine        *GeneralSceneEngine
	configuration GeneralSceneRuntimeConfiguration
	nextScene     int
}

func NewGeneralSceneWorker(
	repository GeneralSceneRuntimeRepository,
	engine *GeneralSceneEngine,
	configuration GeneralSceneRuntimeConfiguration,
) (*GeneralSceneWorker, error) {
	if repository == nil || engine == nil || !configuration.Valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	return &GeneralSceneWorker{
		repository:    repository,
		engine:        engine,
		configuration: configuration,
	}, nil
}

func (worker *GeneralSceneWorker) ProcessPending(
	ctx context.Context,
	limit int,
) (GeneralSceneSweepResult, error) {
	if worker == nil || worker.repository == nil || worker.engine == nil ||
		ctx == nil || limit < 1 || limit > 20 {
		return GeneralSceneSweepResult{}, evaluation.ErrInvalidRequest
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	var sweep GeneralSceneSweepResult
	for sweep.Claimed < limit {
		claim, acquired, err := worker.claimNext(ctx)
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
		case GeneralSceneRuntimeReady:
			sweep.Completed++
		case GeneralSceneRuntimePending:
			sweep.Retried++
		case GeneralSceneRuntimeFailed:
			sweep.Failed++
		default:
			return sweep, evaluation.ErrInvalidRequest
		}
	}
	return sweep, nil
}

func (worker *GeneralSceneWorker) claimNext(
	ctx context.Context,
) (GeneralSceneClaim, bool, error) {
	for offset := range len(generalSceneTypes) {
		index := (worker.nextScene + offset) % len(generalSceneTypes)
		sceneType := generalSceneTypes[index]
		claim, acquired, err := worker.repository.ClaimGeneralScene(
			ctx,
			sceneType,
			worker.configuration,
		)
		if err != nil {
			return GeneralSceneClaim{}, false, err
		}
		if acquired {
			worker.nextScene = (index + 1) % len(generalSceneTypes)
			return claim, true, nil
		}
	}
	return GeneralSceneClaim{}, false, nil
}

func (worker *GeneralSceneWorker) processClaim(
	ctx context.Context,
	claim GeneralSceneClaim,
) (GeneralSceneRuntimeStatus, error) {
	if !claim.Valid() || !generalSceneClaimMatchesRuntime(
		claim,
		worker.configuration,
	) {
		return "", evaluation.ErrInvalidRequest
	}
	result, err := worker.engine.Evaluate(ctx, claim.Snapshot)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if result.Provider != nil &&
		(result.Provider.Provider != claim.Provider ||
			result.Provider.Model != claim.Model) {
		return worker.recordFailure(ctx, claim, ErrInvalidGeneralSceneResult)
	}
	if err := ValidateGeneralSceneResult(claim.Snapshot, result); err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if err := worker.repository.CompleteGeneralScene(ctx, claim, result); err != nil {
		return "", err
	}
	return GeneralSceneRuntimeReady, nil
}

func (worker *GeneralSceneWorker) recordFailure(
	ctx context.Context,
	claim GeneralSceneClaim,
	cause error,
) (GeneralSceneRuntimeStatus, error) {
	failure := classifyGeneralSceneFailure(cause)
	if rejectionStage := generalSceneProviderRejectionStage(cause); rejectionStage != "" {
		slog.Warn(
			"general Scene provider response rejected",
			"failure_code",
			failure.Code,
			"rejection_stage",
			rejectionStage,
		)
	}
	status, err := worker.repository.FailGeneralScene(
		ctx,
		claim,
		failure,
		worker.configuration,
	)
	if err != nil {
		return "", err
	}
	if status != GeneralSceneRuntimePending &&
		status != GeneralSceneRuntimeFailed {
		return "", evaluation.ErrInvalidRequest
	}
	return status, nil
}

func generalSceneProviderRejectionStage(cause error) string {
	switch {
	case errors.Is(cause, errGeneralSceneProviderInvalidJSON):
		return "json_decode"
	case errors.Is(cause, errGeneralSceneProviderSchemaMismatch):
		return "schema_validation"
	case errors.Is(cause, ErrInvalidGeneralSceneResult):
		return "semantic_validation"
	case errors.Is(cause, evaluation.ErrInvalidRequest):
		return "request_validation"
	default:
		return ""
	}
}

func generalSceneClaimMatchesRuntime(
	claim GeneralSceneClaim,
	configuration GeneralSceneRuntimeConfiguration,
) bool {
	spec, ok := generalSceneDurableJobSpec(claim.SceneType)
	return ok && durableSceneJobConfigurationMatchesClaim(
		durableClaimFromGeneralScene(claim),
		durableConfigurationFromGeneralScene(configuration),
		spec,
	)
}

func classifyGeneralSceneFailure(cause error) GeneralSceneFailure {
	switch {
	case errors.Is(cause, ErrInvalidGeneralSceneResult):
		return GeneralSceneFailure{
			Code:      "provider_invalid_response",
			Retryable: true,
		}
	case errors.Is(cause, evaluation.ErrInvalidRequest):
		return GeneralSceneFailure{
			Code:      "invalid_evidence",
			Retryable: false,
		}
	case errors.Is(cause, context.Canceled):
		return GeneralSceneFailure{
			Code:      "provider_canceled",
			Retryable: true,
		}
	case errors.Is(cause, context.DeadlineExceeded):
		return GeneralSceneFailure{
			Code:      "provider_timeout",
			Retryable: true,
		}
	}
	var generationError GenerationFailure
	if errors.As(cause, &generationError) {
		code := strings.ToLower(generationError.StableCategory())
		code = strings.NewReplacer("-", "_", " ", "_").Replace(code)
		if !durableSceneJobFailureCodePattern.MatchString(code) {
			code = "provider_error"
		}
		return GeneralSceneFailure{
			Code:      code,
			Retryable: generationError.Retryable(),
		}
	}
	return GeneralSceneFailure{Code: "dependency_error", Retryable: true}
}
