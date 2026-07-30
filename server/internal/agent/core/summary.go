package core

import (
	"context"
	"crypto/sha256"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxSummaryItemsPerSection = 20
	MaxSummaryItems           = 60
	MaxSummaryItemRunes       = 512
	MaxSummaryItemBytes       = 2048
	MaxSummarySourceMessages  = 100
	MaxSummarySourceRunes     = 64000
)

var summaryVersionPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
)

func ValidSummaryVersion(value string) bool {
	return summaryVersionPattern.MatchString(value)
}

// ThreadSummaryContent is the bounded, structured state carried between
// conversation context windows.
type ThreadSummaryContent struct {
	Goals         []string `json:"goals"`
	Background    []string `json:"background"`
	Progress      []string `json:"progress"`
	Decisions     []string `json:"decisions"`
	OpenQuestions []string `json:"open_questions"`
	NextSteps     []string `json:"next_steps"`
}

func (content ThreadSummaryContent) Valid() bool {
	sections := [][]string{
		content.Goals,
		content.Background,
		content.Progress,
		content.Decisions,
		content.OpenQuestions,
		content.NextSteps,
	}
	total := 0
	for _, section := range sections {
		if section == nil || len(section) > MaxSummaryItemsPerSection {
			return false
		}
		total += len(section)
		for _, item := range section {
			if !validSummaryItem(item) {
				return false
			}
		}
	}
	return total > 0 && total <= MaxSummaryItems
}

func validSummaryItem(value string) bool {
	return utf8.ValidString(value) &&
		len(value) <= MaxSummaryItemBytes &&
		utf8.RuneCountInString(value) <= MaxSummaryItemRunes &&
		!strings.ContainsRune(value, '\x00') &&
		value == strings.TrimSpace(value) &&
		value != ""
}

type ThreadSummaryCheckpoint struct {
	ID                     string
	OwnerID                string
	ThreadID               string
	PreviousCheckpointID   string
	SourceFromSequence     int64
	CoveredThroughSequence int64
	Content                ThreadSummaryContent
	PolicyVersion          string
	PromptVersion          string
	Provider               string
	Model                  string
	SourceChecksum         [sha256.Size]byte
	CreatedAt              time.Time
}

func (checkpoint ThreadSummaryCheckpoint) Valid() bool {
	return ValidUUID(checkpoint.ID) &&
		validSummaryCheckpointFields(
			checkpoint.OwnerID,
			checkpoint.ThreadID,
			checkpoint.PreviousCheckpointID,
			checkpoint.SourceFromSequence,
			checkpoint.CoveredThroughSequence,
			checkpoint.Content,
			checkpoint.PolicyVersion,
			checkpoint.PromptVersion,
			checkpoint.Provider,
			checkpoint.Model,
			checkpoint.SourceChecksum,
		) &&
		!checkpoint.CreatedAt.IsZero()
}

type CreateThreadSummaryCheckpointCommand struct {
	OwnerID                string
	ThreadID               string
	PreviousCheckpointID   string
	SourceFromSequence     int64
	CoveredThroughSequence int64
	Content                ThreadSummaryContent
	PolicyVersion          string
	PromptVersion          string
	Provider               string
	Model                  string
	SourceChecksum         [sha256.Size]byte
}

func (command CreateThreadSummaryCheckpointCommand) Valid() bool {
	return validSummaryCheckpointFields(
		command.OwnerID,
		command.ThreadID,
		command.PreviousCheckpointID,
		command.SourceFromSequence,
		command.CoveredThroughSequence,
		command.Content,
		command.PolicyVersion,
		command.PromptVersion,
		command.Provider,
		command.Model,
		command.SourceChecksum,
	)
}

func validSummaryCheckpointFields(
	ownerID string,
	threadID string,
	previousCheckpointID string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
	content ThreadSummaryContent,
	policyVersion string,
	promptVersion string,
	provider string,
	model string,
	sourceChecksum [sha256.Size]byte,
) bool {
	previousValid := previousCheckpointID == "" ||
		ValidUUID(previousCheckpointID)
	sequenceValid := sourceFromSequence >= 1 &&
		coveredThroughSequence >= sourceFromSequence &&
		((previousCheckpointID == "" && sourceFromSequence == 1) ||
			(previousCheckpointID != "" && sourceFromSequence > 1))
	return ValidUUID(ownerID) &&
		ValidUUID(threadID) &&
		previousValid &&
		sequenceValid &&
		content.Valid() &&
		ValidSummaryVersion(policyVersion) &&
		ValidSummaryVersion(promptVersion) &&
		ValidProviderID(provider) &&
		ValidModelID(model) &&
		sourceChecksum != [sha256.Size]byte{}
}

type ThreadSummaryCheckpointRepository interface {
	CreateSummaryCheckpoint(
		context.Context,
		CreateThreadSummaryCheckpointCommand,
	) (ThreadSummaryCheckpoint, error)
	FindLatestSummaryCheckpoint(
		ctx context.Context,
		ownerID string,
		threadID string,
		maxSequence int64,
	) (ThreadSummaryCheckpoint, error)
}
