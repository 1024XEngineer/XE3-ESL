package evaluation

import (
	"context"
	"crypto/sha256"
	"regexp"
	"strings"
	"time"
)

var durableSceneJobFailureCodePattern = regexp.MustCompile(
	`^[a-z][a-z0-9_.:-]{0,127}$`,
)

type durableSceneJobSpec struct {
	sceneType       SceneType
	strategyRef     string
	pipelineVersion string
	promptVersion   string
	resultTable     string
}

func (spec durableSceneJobSpec) valid() bool {
	if !validSceneType(spec.sceneType) ||
		!validVersion(spec.strategyRef) ||
		!validVersion(spec.pipelineVersion) ||
		!validVersion(spec.promptVersion) {
		return false
	}
	switch spec.resultTable {
	case "evaluation_interview_scene_results",
		"evaluation_ielts_speaking_scene_results":
		return true
	default:
		return false
	}
}

type durableSceneJobConfiguration struct {
	MaxAttempts     int
	LeaseDuration   time.Duration
	StrategyRef     string
	PipelineVersion string
	FullConfigHash  [sha256.Size]byte
	PromptVersion   string
	Provider        string
	Model           string
}

func (configuration durableSceneJobConfiguration) valid(
	spec durableSceneJobSpec,
) bool {
	return spec.valid() &&
		configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.StrategyRef == spec.strategyRef &&
		configuration.PipelineVersion == spec.pipelineVersion &&
		nonZeroDigest(configuration.FullConfigHash) &&
		configuration.PromptVersion == spec.promptVersion &&
		validRuntimeLineage(configuration.Provider) &&
		validRuntimeLineage(configuration.Model)
}

type durableSceneJobClaim struct {
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

func (claim durableSceneJobClaim) valid(
	spec durableSceneJobSpec,
) bool {
	return spec.valid() &&
		validUUID(claim.OutboxID) &&
		validUUID(claim.ModuleRunID) &&
		validUUID(claim.EvaluationID) &&
		validUUID(claim.EvaluationRevisionID) &&
		validUUID(claim.OwnerUserID) &&
		claim.Revision >= 1 &&
		claim.StrategyRef == spec.strategyRef &&
		claim.PipelineVersion == spec.pipelineVersion &&
		claim.AttemptCount >= 1 &&
		claim.FencingToken >= 1 &&
		!claim.LeaseExpiresAt.IsZero() &&
		nonZeroDigest(claim.FullConfigHash) &&
		claim.PromptVersion == spec.promptVersion &&
		validRuntimeLineage(claim.Provider) &&
		validRuntimeLineage(claim.Model) &&
		claim.Snapshot.Valid() &&
		claim.Snapshot.OwnerUserID == claim.OwnerUserID &&
		claim.Snapshot.Scope == ScopeSession &&
		claim.Snapshot.SceneType == spec.sceneType
}

type durableSceneJobFailure struct {
	Code      string
	Retryable bool
}

func (failure durableSceneJobFailure) valid() bool {
	return durableSceneJobFailureCodePattern.MatchString(failure.Code)
}

type durableSceneJobStatus string

const (
	durableSceneJobPending durableSceneJobStatus = "PENDING"
	durableSceneJobRunning durableSceneJobStatus = "RUNNING"
	durableSceneJobReady   durableSceneJobStatus = "READY"
	durableSceneJobFailed  durableSceneJobStatus = "FAILED"
)

type durableSceneJobReadState struct {
	ModuleStatus   durableSceneJobStatus
	FullConfigHash [sha256.Size]byte
	ResultPayload  []byte
	Failure        *durableSceneJobFailure
}

type durableSceneJobSweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

func processDurableSceneJobs(
	ctx context.Context,
	limit int,
	claimNext func(context.Context) (durableSceneJobClaim, bool, error),
	process func(
		context.Context,
		durableSceneJobClaim,
	) (durableSceneJobStatus, error),
) (durableSceneJobSweepResult, error) {
	if ctx == nil || claimNext == nil || process == nil ||
		limit < 1 || limit > 20 {
		return durableSceneJobSweepResult{}, ErrInvalidRequest
	}
	var sweep durableSceneJobSweepResult
	for sweep.Claimed < limit {
		if err := ctx.Err(); err != nil {
			return sweep, err
		}
		claim, acquired, err := claimNext(ctx)
		if err != nil {
			return sweep, err
		}
		if !acquired {
			return sweep, nil
		}
		sweep.Claimed++
		status, err := process(ctx, claim)
		if err != nil {
			return sweep, err
		}
		switch status {
		case durableSceneJobReady:
			sweep.Completed++
		case durableSceneJobPending:
			sweep.Retried++
		case durableSceneJobFailed:
			sweep.Failed++
		default:
			return sweep, ErrInvalidRequest
		}
	}
	return sweep, nil
}

func durableSceneJobConfigurationMatchesClaim(
	claim durableSceneJobClaim,
	configuration durableSceneJobConfiguration,
	spec durableSceneJobSpec,
) bool {
	return claim.valid(spec) &&
		configuration.valid(spec) &&
		claim.StrategyRef == configuration.StrategyRef &&
		claim.PipelineVersion == configuration.PipelineVersion &&
		claim.FullConfigHash == configuration.FullConfigHash &&
		claim.PromptVersion == configuration.PromptVersion &&
		claim.Provider == configuration.Provider &&
		claim.Model == configuration.Model &&
		claim.AttemptCount <= configuration.MaxAttempts
}

func validRuntimeLineage(value string) bool {
	return strings.TrimSpace(value) == value &&
		value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00')
}
