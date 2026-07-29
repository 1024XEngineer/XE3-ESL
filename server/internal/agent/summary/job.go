package summary

import (
	"context"
	"regexp"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
)

const (
	TriggerPolicyV1          = "summary-trigger-v1"
	DefaultTriggerMessages   = int64(40)
	DefaultRetainedMessages  = int64(12)
	DefaultWorkerMaxAttempts = 3
)

var summaryJobFailurePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobRunning    JobStatus = "running"
	JobCompleted  JobStatus = "completed"
	JobSkipped    JobStatus = "skipped"
	JobSuperseded JobStatus = "superseded"
	JobFailed     JobStatus = "failed"
)

func (status JobStatus) Valid() bool {
	switch status {
	case JobPending,
		JobRunning,
		JobCompleted,
		JobSkipped,
		JobSuperseded,
		JobFailed:
		return true
	default:
		return false
	}
}

type WorkerConfiguration struct {
	TriggerPolicyVersion string
	TriggerMessages      int64
	RetainRecentMessages int64
	LeaseDuration        time.Duration
	MaxAttempts          int
	Summary              Configuration
}

func (configuration WorkerConfiguration) Valid() bool {
	return core.ValidSummaryVersion(configuration.TriggerPolicyVersion) &&
		configuration.TriggerMessages > configuration.RetainRecentMessages &&
		configuration.TriggerMessages <= 1000 &&
		configuration.RetainRecentMessages >= 1 &&
		configuration.RetainRecentMessages <
			int64(core.MaxSummarySourceMessages) &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.Summary.Valid()
}

type Job struct {
	SourceRunID             string
	OwnerID                 string
	ThreadID                string
	ObservedThroughSequence int64
	SourceCompletedAt       time.Time
	Status                  JobStatus
	AttemptCount            int
	LeaseToken              string
	LeaseExpiresAt          time.Time
	NextAttemptAt           time.Time
	TriggerPolicyVersion    string
	SummaryPolicyVersion    string
	PromptVersion           string
	Provider                string
	Model                   string
	TargetCoveredThrough    int64
	CheckpointID            string
	OutcomeReason           string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	CompletedAt             time.Time
}

type JobClaim struct {
	Job
}

func (claim JobClaim) Valid() bool {
	return core.ValidUUID(claim.SourceRunID) &&
		core.ValidUUID(claim.OwnerID) &&
		core.ValidUUID(claim.ThreadID) &&
		claim.ObservedThroughSequence >= 1 &&
		!claim.SourceCompletedAt.IsZero() &&
		claim.Status == JobRunning &&
		claim.AttemptCount > 0 &&
		core.ValidUUID(claim.LeaseToken) &&
		!claim.LeaseExpiresAt.IsZero() &&
		core.ValidSummaryVersion(claim.TriggerPolicyVersion) &&
		core.ValidSummaryVersion(claim.SummaryPolicyVersion) &&
		core.ValidSummaryVersion(claim.PromptVersion) &&
		core.ValidProviderID(claim.Provider) &&
		core.ValidModelID(claim.Model)
}

type JobRepository interface {
	ClaimSummaryJob(
		context.Context,
		WorkerConfiguration,
	) (JobClaim, bool, error)
	CompleteSummaryJob(
		context.Context,
		JobClaim,
		int64,
		core.ThreadSummaryCheckpoint,
	) (Job, error)
	FinishSummaryJob(
		context.Context,
		JobClaim,
		JobStatus,
		int64,
		string,
	) (Job, error)
	FailSummaryJob(
		context.Context,
		JobClaim,
		int64,
		string,
		bool,
		WorkerConfiguration,
	) (Job, error)
}

type CheckpointGenerator interface {
	GenerateCheckpoint(
		context.Context,
		GenerateCheckpointCommand,
	) (core.ThreadSummaryCheckpoint, error)
}

type SweepResult struct {
	Claimed    int
	Completed  int
	Retried    int
	Skipped    int
	Superseded int
	Failed     int
}

type Processor interface {
	ProcessPending(context.Context, int) (SweepResult, error)
}
