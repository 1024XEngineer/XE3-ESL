package summary

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const (
	MaxItemsPerSection = 20
	MaxItems           = 60
	MaxItemRunes       = 512
	MaxItemBytes       = 2048
	MaxSourceMessages  = 100
	MaxSourceRunes     = 64000
)

var versionPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var modelPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

func ValidVersion(value string) bool {
	return versionPattern.MatchString(value)
}

type Content struct {
	Goals         []string `json:"goals"`
	Background    []string `json:"background"`
	Progress      []string `json:"progress"`
	Decisions     []string `json:"decisions"`
	OpenQuestions []string `json:"open_questions"`
	NextSteps     []string `json:"next_steps"`
}

func (content Content) Valid() bool {
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
		if section == nil || len(section) > MaxItemsPerSection {
			return false
		}
		total += len(section)
		for _, item := range section {
			if !validItem(item) {
				return false
			}
		}
	}
	return total > 0 && total <= MaxItems
}

func validItem(value string) bool {
	return utf8.ValidString(value) &&
		len(value) <= MaxItemBytes &&
		utf8.RuneCountInString(value) <= MaxItemRunes &&
		!strings.ContainsRune(value, '\x00') &&
		value == strings.TrimSpace(value) &&
		value != ""
}

type Checkpoint struct {
	ID                     string
	OwnerID                string
	ThreadID               string
	PreviousCheckpointID   string
	SourceFromSequence     int64
	CoveredThroughSequence int64
	Content                Content
	PolicyVersion          string
	PromptVersion          string
	Provider               string
	Model                  string
	SourceChecksum         [sha256.Size]byte
	CreatedAt              time.Time
}

func (checkpoint Checkpoint) Valid() bool {
	return conversation.ValidUUID(checkpoint.ID) &&
		validCheckpointFields(
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

type CreateCheckpointCommand struct {
	OwnerID                string
	ThreadID               string
	PreviousCheckpointID   string
	SourceFromSequence     int64
	CoveredThroughSequence int64
	Content                Content
	PolicyVersion          string
	PromptVersion          string
	Provider               string
	Model                  string
	SourceChecksum         [sha256.Size]byte
}

func (command CreateCheckpointCommand) Valid() bool {
	return validCheckpointFields(
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

func validCheckpointFields(
	ownerID string,
	threadID string,
	previousCheckpointID string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
	content Content,
	policyVersion string,
	promptVersion string,
	provider string,
	model string,
	sourceChecksum [sha256.Size]byte,
) bool {
	previousValid := previousCheckpointID == "" ||
		conversation.ValidUUID(previousCheckpointID)
	sequenceValid := sourceFromSequence >= 1 &&
		coveredThroughSequence >= sourceFromSequence &&
		((previousCheckpointID == "" && sourceFromSequence == 1) ||
			(previousCheckpointID != "" && sourceFromSequence > 1))
	return conversation.ValidUUID(ownerID) &&
		conversation.ValidUUID(threadID) &&
		previousValid &&
		sequenceValid &&
		content.Valid() &&
		ValidVersion(policyVersion) &&
		ValidVersion(promptVersion) &&
		providerPattern.MatchString(provider) &&
		modelPattern.MatchString(model) &&
		sourceChecksum != [sha256.Size]byte{}
}
