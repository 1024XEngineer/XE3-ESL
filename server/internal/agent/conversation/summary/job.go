package summary

import (
	"context"
	"regexp"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
)

const DefaultWorkerMaxAttempts = 3

var (
	failurePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type Configuration struct {
	Provider string
	Model    string
}

func (configuration Configuration) Valid() bool {
	return providerPattern.MatchString(configuration.Provider) &&
		modelid.Valid(configuration.Model)
}

type WorkerConfiguration struct {
	MaxContextCharacters int
	LeaseDuration        time.Duration
	MaxAttempts          int
	Generation           Configuration
}

func (configuration WorkerConfiguration) Valid() bool {
	return configuration.MaxContextCharacters >= 5000 &&
		configuration.MaxContextCharacters <= 1_000_000 &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.MaxAttempts >= 1 && configuration.MaxAttempts <= 10 &&
		configuration.Generation.Valid()
}

// Both thresholds derive from Agent Run's actual Context limit. There is no
// second environment setting that can drift from the assembler budget.
func (configuration WorkerConfiguration) TriggerCharacters() int {
	return configuration.MaxContextCharacters / 3
}

func (configuration WorkerConfiguration) RetainCharacters() int {
	return configuration.MaxContextCharacters / 6
}

type Claim struct {
	OwnerID        string
	ThreadID       string
	TargetSequence int64
	AttemptCount   int
	LeaseToken     string
	LeaseExpiresAt time.Time
}

func (claim Claim) Valid() bool {
	return conversation.ValidUUID(claim.OwnerID) &&
		conversation.ValidUUID(claim.ThreadID) &&
		claim.TargetSequence >= 1 && claim.AttemptCount > 0 &&
		conversation.ValidUUID(claim.LeaseToken) &&
		!claim.LeaseExpiresAt.IsZero()
}

type SweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Skipped   int
	Failed    int
}

type Processor interface {
	ProcessPending(context.Context, int) (SweepResult, error)
}
