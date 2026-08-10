package title

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
)

const (
	MaxTitleRunes            = 32
	MaxTitleBytes            = 128
	MinTitleWords            = 2
	MaxTitleWords            = 12
	DefaultWorkerMaxAttempts = 3
)

var (
	versionPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
	)
	providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	failurePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

func validModelID(value string) bool {
	return modelid.Valid(value)
}

func ValidTitle(value string) bool {
	if !utf8.ValidString(value) ||
		value != strings.TrimSpace(value) ||
		len(value) > MaxTitleBytes {
		return false
	}
	runes := utf8.RuneCountInString(value)
	if runes < 2 || runes > MaxTitleRunes {
		return false
	}
	for _, value := range value {
		if unicode.IsControl(value) || unicode.IsSymbol(value) ||
			strings.ContainsRune("#*_~`[]{}<>\\|", value) ||
			strings.ContainsRune("\"'“”‘’：:", value) {
			return false
		}
	}
	if strings.ContainsRune(".!?。！？;；,，", []rune(value)[runes-1]) {
		return false
	}
	words, usesUnsegmentedScript := titleWordCount(value)
	return words >= MinTitleWords &&
		(usesUnsegmentedScript || words <= MaxTitleWords)
}

func titleWordCount(value string) (int, bool) {
	words := 0
	inAlphabeticWord := false
	usesUnsegmentedScript := false
	for _, value := range value {
		switch {
		case unicode.Is(unicode.Han, value) ||
			unicode.Is(unicode.Hiragana, value) ||
			unicode.Is(unicode.Katakana, value) ||
			unicode.Is(unicode.Hangul, value):
			words++
			inAlphabeticWord = false
			usesUnsegmentedScript = true
		case unicode.IsLetter(value) || unicode.IsDigit(value):
			if !inAlphabeticWord {
				words++
				inAlphabeticWord = true
			}
		default:
			inAlphabeticWord = false
		}
	}
	return words, usesUnsegmentedScript
}

func ValidFailureKind(value string) bool {
	return failurePattern.MatchString(value)
}

type Configuration struct {
	PromptVersion string
	Provider      string
	Model         string
}

func (configuration Configuration) Valid() bool {
	return versionPattern.MatchString(configuration.PromptVersion) &&
		providerPattern.MatchString(configuration.Provider) &&
		validModelID(configuration.Model)
}

type WorkerConfiguration struct {
	LeaseDuration time.Duration
	MaxAttempts   int
	Generation    Configuration
}

func (configuration WorkerConfiguration) Valid() bool {
	return configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.Generation.Valid()
}

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

func (status JobStatus) Valid() bool {
	switch status {
	case JobPending, JobRunning, JobCompleted, JobFailed:
		return true
	default:
		return false
	}
}

type Job struct {
	SourceRunID      string
	OwnerID          string
	ThreadID         string
	UserMessage      string
	AssistantMessage string
	Status           JobStatus
	AttemptCount     int
	LeaseToken       string
	LeaseExpiresAt   time.Time
	NextAttemptAt    time.Time
	PromptVersion    string
	Provider         string
	Model            string
	FailureKind      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      time.Time
}

type JobClaim struct {
	Job
}

func (claim JobClaim) Valid() bool {
	return conversation.ValidUUID(claim.SourceRunID) &&
		conversation.ValidUUID(claim.OwnerID) &&
		conversation.ValidUUID(claim.ThreadID) &&
		conversation.ValidMessageContent(claim.UserMessage) &&
		conversation.ValidMessageContent(claim.AssistantMessage) &&
		claim.Status == JobRunning &&
		claim.AttemptCount > 0 &&
		conversation.ValidUUID(claim.LeaseToken) &&
		!claim.LeaseExpiresAt.IsZero() &&
		versionPattern.MatchString(claim.PromptVersion) &&
		providerPattern.MatchString(claim.Provider) &&
		validModelID(claim.Model)
}

type JobRepository interface {
	ClaimJob(
		context.Context,
		WorkerConfiguration,
	) (JobClaim, bool, error)
	CompleteJob(context.Context, JobClaim, string) (Job, error)
	FailJob(
		context.Context,
		JobClaim,
		string,
		bool,
		WorkerConfiguration,
	) (Job, error)
}

type TitleGenerator interface {
	GenerateTitle(context.Context, JobClaim) (string, error)
}

type SweepResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

type Processor interface {
	ProcessPending(context.Context, int) (SweepResult, error)
}
