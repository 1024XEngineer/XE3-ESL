package memory

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"
)

const (
	testOwnerID  = "10000000-0000-4000-8000-000000000001"
	testMemoryID = "20000000-0000-4000-8000-000000000001"
	testGoalID   = "30000000-0000-4000-8000-000000000001"
	testSourceID = "40000000-0000-4000-8000-000000000001"
)

func TestCreateCommandValidation(t *testing.T) {
	t.Parallel()

	valid := validCreateCommand()
	tests := []struct {
		name   string
		mutate func(*CreateCommand)
	}{
		{
			name: "unknown memory type",
			mutate: func(command *CreateCommand) {
				command.Type = Type("personality")
			},
		},
		{
			name: "invalid canonical key",
			mutate: func(command *CreateCommand) {
				command.CanonicalKey = "Career Role"
			},
		},
		{
			name: "blank content",
			mutate: func(command *CreateCommand) {
				command.Content = " "
			},
		},
		{
			name: "user scope with goal",
			mutate: func(command *CreateCommand) {
				command.GoalID = testGoalID
			},
		},
		{
			name: "goal scope without goal",
			mutate: func(command *CreateCommand) {
				command.Scope = ScopeGoal
			},
		},
		{
			name: "invalid policy version",
			mutate: func(command *CreateCommand) {
				command.PolicyVersion = "v1 latest"
			},
		},
		{
			name: "unknown source type",
			mutate: func(command *CreateCommand) {
				command.Source.Type = SourceType("chat")
			},
		},
		{
			name: "invalid source version",
			mutate: func(command *CreateCommand) {
				command.Source.Version = 0
			},
		},
	}

	if !valid.Valid() {
		t.Fatal("valid CreateCommand was rejected")
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := valid
			test.mutate(&command)
			if command.Valid() {
				t.Fatal("invalid CreateCommand was accepted")
			}
		})
	}
}

func TestMemoryLifecycleValidation(t *testing.T) {
	t.Parallel()

	createdAt := time.Now().UTC()
	item := Memory{
		ID:            testMemoryID,
		OwnerID:       testOwnerID,
		Type:          TypeProfile,
		CanonicalKey:  "career.role",
		Content:       "Java backend engineer",
		Scope:         ScopeUser,
		Status:        StatusActive,
		Version:       1,
		PolicyVersion: "memory-v1",
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}
	if !item.Valid() {
		t.Fatal("valid active Memory was rejected")
	}

	inactivatedAt := createdAt.Add(time.Second)
	item.Status = StatusInactive
	item.Version = 2
	item.UpdatedAt = inactivatedAt
	item.InactivatedAt = &inactivatedAt
	if !item.Valid() {
		t.Fatal("valid inactive Memory was rejected")
	}

	item.Content = strings.Repeat("界", maxContentRunes+1)
	if item.Valid() {
		t.Fatal("oversized Memory content was accepted")
	}
}

func TestScopeFilterValidation(t *testing.T) {
	t.Parallel()

	if !(ScopeFilter{Scope: ScopeUser, Limit: 10}).Valid() {
		t.Fatal("valid user ScopeFilter was rejected")
	}
	if !(ScopeFilter{
		Scope:  ScopeGoal,
		GoalID: testGoalID,
		Limit:  100,
	}).Valid() {
		t.Fatal("valid goal ScopeFilter was rejected")
	}
	for _, filter := range []ScopeFilter{
		{Scope: ScopeGoal, Limit: 10},
		{Scope: ScopeUser, GoalID: testGoalID, Limit: 10},
		{Scope: ScopeUser, Limit: 0},
		{Scope: ScopeUser, Limit: 101},
	} {
		if filter.Valid() {
			t.Fatalf("invalid ScopeFilter was accepted: %#v", filter)
		}
	}
}

func validCreateCommand() CreateCommand {
	return CreateCommand{
		Type:          TypeProfile,
		CanonicalKey:  "career.role",
		Content:       "Java backend engineer",
		Scope:         ScopeUser,
		PolicyVersion: "memory-v1",
		Source: SourceInput{
			Type:     SourceAgentRun,
			SourceID: testSourceID,
			Version:  1,
			Checksum: sha256.Sum256([]byte("source")),
		},
	}
}
