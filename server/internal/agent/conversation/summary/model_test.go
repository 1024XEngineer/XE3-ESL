package summary

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"
)

func TestContentValidation(t *testing.T) {
	valid := validContent()
	if !valid.Valid() {
		t.Fatal("valid summary content was rejected")
	}

	tests := map[string]Content{
		"nil section": func() Content {
			value := valid
			value.Decisions = nil
			return value
		}(),
		"all empty": {
			Goals: []string{}, Background: []string{}, Progress: []string{},
			Decisions: []string{}, OpenQuestions: []string{}, NextSteps: []string{},
		},
		"blank item": func() Content {
			value := valid
			value.Goals = []string{" "}
			return value
		}(),
		"untrimmed item": func() Content {
			value := valid
			value.Goals = []string{" pass the interview "}
			return value
		}(),
		"oversized item": func() Content {
			value := valid
			value.Goals = []string{strings.Repeat("界", MaxItemRunes+1)}
			return value
		}(),
		"too many section items": func() Content {
			value := valid
			value.Goals = make([]string, MaxItemsPerSection+1)
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

func TestCreateCheckpointCommandValidation(t *testing.T) {
	valid := validCommand()
	if !valid.Valid() {
		t.Fatal("valid summary command was rejected")
	}

	tests := map[string]CreateCheckpointCommand{
		"first starts after one": func() CreateCheckpointCommand {
			value := valid
			value.SourceFromSequence = 2
			return value
		}(),
		"previous missing for continuation": func() CreateCheckpointCommand {
			value := valid
			value.SourceFromSequence = 2
			value.CoveredThroughSequence = 3
			return value
		}(),
		"coverage before source": func() CreateCheckpointCommand {
			value := valid
			value.CoveredThroughSequence = 0
			return value
		}(),
		"invalid policy version": func() CreateCheckpointCommand {
			value := valid
			value.PolicyVersion = "policy version"
			return value
		}(),
		"zero checksum": func() CreateCheckpointCommand {
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

func TestCheckpointValidationRequiresCreationTime(t *testing.T) {
	command := validCommand()
	checkpoint := Checkpoint{
		ID:      "40000000-0000-4000-8000-000000000001",
		OwnerID: command.OwnerID, ThreadID: command.ThreadID,
		SourceFromSequence:     command.SourceFromSequence,
		CoveredThroughSequence: command.CoveredThroughSequence,
		Content:                command.Content, PolicyVersion: command.PolicyVersion,
		PromptVersion: command.PromptVersion, Provider: command.Provider,
		Model: command.Model, SourceChecksum: command.SourceChecksum,
		CreatedAt: time.Now().UTC(),
	}
	if !checkpoint.Valid() {
		t.Fatal("valid checkpoint was rejected")
	}
	checkpoint.CreatedAt = time.Time{}
	if checkpoint.Valid() {
		t.Fatal("checkpoint without creation time was accepted")
	}
}

func validContent() Content {
	return Content{
		Goals:      []string{"Pass an English product interview"},
		Background: []string{}, Progress: []string{}, Decisions: []string{},
		OpenQuestions: []string{}, NextSteps: []string{},
	}
}

func validCommand() CreateCheckpointCommand {
	return CreateCheckpointCommand{
		OwnerID:            "10000000-0000-4000-8000-000000000001",
		ThreadID:           "20000000-0000-4000-8000-000000000001",
		SourceFromSequence: 1, CoveredThroughSequence: 1,
		Content: validContent(), PolicyVersion: "summary-policy-v1",
		PromptVersion: "summary-prompt-v1", Provider: "qwen", Model: "qwen-plus",
		SourceChecksum: sha256.Sum256([]byte("source")),
	}
}
