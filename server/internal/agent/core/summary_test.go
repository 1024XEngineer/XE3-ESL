package core

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"
)

func TestThreadSummaryContentValidation(t *testing.T) {
	valid := validSummaryContent()
	if !valid.Valid() {
		t.Fatal("valid summary content was rejected")
	}

	tests := map[string]ThreadSummaryContent{
		"nil section": func() ThreadSummaryContent {
			value := valid
			value.Decisions = nil
			return value
		}(),
		"all empty": {
			Goals:         []string{},
			Background:    []string{},
			Progress:      []string{},
			Decisions:     []string{},
			OpenQuestions: []string{},
			NextSteps:     []string{},
		},
		"blank item": func() ThreadSummaryContent {
			value := valid
			value.Goals = []string{" "}
			return value
		}(),
		"untrimmed item": func() ThreadSummaryContent {
			value := valid
			value.Goals = []string{" pass the interview "}
			return value
		}(),
		"oversized item": func() ThreadSummaryContent {
			value := valid
			value.Goals = []string{strings.Repeat("界", MaxSummaryItemRunes+1)}
			return value
		}(),
		"too many section items": func() ThreadSummaryContent {
			value := valid
			value.Goals = make([]string, MaxSummaryItemsPerSection+1)
			for index := range value.Goals {
				value.Goals[index] = "goal"
			}
			return value
		}(),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if content.Valid() {
				t.Fatal("invalid summary content was accepted")
			}
		})
	}
}

func TestCreateThreadSummaryCheckpointCommandValidation(t *testing.T) {
	valid := validSummaryCommand()
	if !valid.Valid() {
		t.Fatal("valid summary command was rejected")
	}

	tests := map[string]CreateThreadSummaryCheckpointCommand{
		"first starts after one": func() CreateThreadSummaryCheckpointCommand {
			value := valid
			value.SourceFromSequence = 2
			return value
		}(),
		"previous missing for continuation": func() CreateThreadSummaryCheckpointCommand {
			value := valid
			value.SourceFromSequence = 2
			value.CoveredThroughSequence = 3
			return value
		}(),
		"coverage before source": func() CreateThreadSummaryCheckpointCommand {
			value := valid
			value.CoveredThroughSequence = 0
			return value
		}(),
		"invalid policy version": func() CreateThreadSummaryCheckpointCommand {
			value := valid
			value.PolicyVersion = "policy version"
			return value
		}(),
		"zero checksum": func() CreateThreadSummaryCheckpointCommand {
			value := valid
			value.SourceChecksum = [sha256.Size]byte{}
			return value
		}(),
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			if command.Valid() {
				t.Fatal("invalid summary command was accepted")
			}
		})
	}

	continuation := valid
	continuation.PreviousCheckpointID = "30000000-0000-4000-8000-000000000001"
	continuation.SourceFromSequence = 2
	continuation.CoveredThroughSequence = 3
	if !continuation.Valid() {
		t.Fatal("valid continuation command was rejected")
	}
}

func TestThreadSummaryCheckpointValidationRequiresPersistenceFields(t *testing.T) {
	command := validSummaryCommand()
	checkpoint := ThreadSummaryCheckpoint{
		ID:                     "40000000-0000-4000-8000-000000000001",
		OwnerID:                command.OwnerID,
		ThreadID:               command.ThreadID,
		SourceFromSequence:     command.SourceFromSequence,
		CoveredThroughSequence: command.CoveredThroughSequence,
		Content:                command.Content,
		PolicyVersion:          command.PolicyVersion,
		PromptVersion:          command.PromptVersion,
		Provider:               command.Provider,
		Model:                  command.Model,
		SourceChecksum:         command.SourceChecksum,
		CreatedAt:              time.Now().UTC(),
	}
	if !checkpoint.Valid() {
		t.Fatal("valid checkpoint was rejected")
	}
	checkpoint.CreatedAt = time.Time{}
	if checkpoint.Valid() {
		t.Fatal("checkpoint without creation time was accepted")
	}
}

func validSummaryContent() ThreadSummaryContent {
	return ThreadSummaryContent{
		Goals:         []string{"Pass an English product interview"},
		Background:    []string{},
		Progress:      []string{},
		Decisions:     []string{},
		OpenQuestions: []string{},
		NextSteps:     []string{},
	}
}

func validSummaryCommand() CreateThreadSummaryCheckpointCommand {
	return CreateThreadSummaryCheckpointCommand{
		OwnerID:                "10000000-0000-4000-8000-000000000001",
		ThreadID:               "20000000-0000-4000-8000-000000000001",
		SourceFromSequence:     1,
		CoveredThroughSequence: 1,
		Content:                validSummaryContent(),
		PolicyVersion:          "summary-policy-v1",
		PromptVersion:          "summary-prompt-v1",
		Provider:               "qwen",
		Model:                  "qwen-plus",
		SourceChecksum:         sha256.Sum256([]byte("source")),
	}
}
