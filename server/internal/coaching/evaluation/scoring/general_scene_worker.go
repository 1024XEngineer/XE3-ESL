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
	LoadGeneralSceneAtomicResults(
		context.Context,
		GeneralSceneClaim,
	) ([]GeneralSceneAtomicResult, error)
	RecordGeneralSceneAtomicAttempt(
		context.Context,
		GeneralSceneClaim,
		GeneralSceneAtomicAttempt,
	) error
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
	if claim.SceneType == evaluation.SceneIELTSSpeaking {
		return worker.processIELTSAtomicClaim(ctx, claim)
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

func (worker *GeneralSceneWorker) processIELTSAtomicClaim(
	ctx context.Context,
	claim GeneralSceneClaim,
) (GeneralSceneRuntimeStatus, error) {
	keys, insufficient, err := generalSceneAtomicPlan(claim.Snapshot)
	if err != nil {
		return worker.recordFailure(ctx, claim, err)
	}
	if insufficient != nil {
		if err := worker.repository.CompleteGeneralScene(
			ctx,
			claim,
			*insufficient,
		); err != nil {
			return "", err
		}
		return GeneralSceneRuntimeReady, nil
	}
	stored, err := worker.repository.LoadGeneralSceneAtomicResults(ctx, claim)
	if err != nil {
		return "", err
	}
	ready := make(map[GeneralSceneAtomicKey]GeneralSceneAtomicResult, len(stored))
	expected := make(map[GeneralSceneAtomicKey]struct{}, len(keys))
	for _, key := range keys {
		expected[key] = struct{}{}
	}
	for _, atom := range stored {
		if _, exists := expected[atom.Key]; !exists ||
			ValidateGeneralSceneAtomicResult(claim.Snapshot, atom) != nil ||
			atom.Provider.Provider != claim.Provider ||
			atom.Provider.Model != claim.Model {
			return worker.recordFailure(ctx, claim, ErrInvalidGeneralSceneResult)
		}
		if _, duplicate := ready[atom.Key]; duplicate {
			return worker.recordFailure(ctx, claim, ErrInvalidGeneralSceneResult)
		}
		ready[atom.Key] = atom
	}
	missing := make([]GeneralSceneAtomicKey, 0, len(keys)-len(ready))
	for _, key := range keys {
		if _, exists := ready[key]; !exists {
			missing = append(missing, key)
		}
	}
	type atomicOutcome struct {
		key    GeneralSceneAtomicKey
		result GeneralSceneAtomicResult
		err    error
	}
	outcomes := make(chan atomicOutcome, len(missing))
	for _, key := range missing {
		key := key
		go func() {
			result, evaluateErr := worker.engine.EvaluateAtomic(
				ctx,
				claim.Snapshot,
				key,
			)
			outcomes <- atomicOutcome{key: key, result: result, err: evaluateErr}
		}()
	}
	failures := make(map[GeneralSceneAtomicKey]error)
	var recordErr error
	for range missing {
		outcome := <-outcomes
		if outcome.err == nil &&
			(outcome.result.Provider.Provider != claim.Provider ||
				outcome.result.Provider.Model != claim.Model) {
			outcome.err = ErrInvalidGeneralSceneResult
		}
		attempt := GeneralSceneAtomicAttempt{
			Key:          outcome.key,
			AttemptCount: claim.AttemptCount,
		}
		if outcome.err == nil {
			attempt.Status = GeneralSceneAtomicAttemptReady
			attempt.Result = &outcome.result
		} else {
			failure := classifyGeneralSceneFailure(outcome.err)
			attempt.Status = GeneralSceneAtomicAttemptFailed
			attempt.Failure = &failure
			failures[outcome.key] = outcome.err
		}
		if err := worker.repository.RecordGeneralSceneAtomicAttempt(
			ctx,
			claim,
			attempt,
		); err != nil && recordErr == nil {
			recordErr = err
		}
		if outcome.err == nil {
			ready[outcome.key] = outcome.result
		}
	}
	if recordErr != nil {
		return "", recordErr
	}
	if len(failures) > 0 {
		var first error
		for _, key := range keys {
			cause, exists := failures[key]
			if !exists {
				continue
			}
			if first == nil {
				first = cause
			}
			if !classifyGeneralSceneFailure(cause).Retryable {
				first = cause
				break
			}
		}
		return worker.recordFailure(ctx, claim, first)
	}
	atoms := make([]GeneralSceneAtomicResult, len(keys))
	for index, key := range keys {
		atom, exists := ready[key]
		if !exists {
			return worker.recordFailure(ctx, claim, ErrInvalidGeneralSceneResult)
		}
		atoms[index] = atom
	}
	result, err := AggregateGeneralSceneAtoms(claim.Snapshot, atoms)
	if err != nil {
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
