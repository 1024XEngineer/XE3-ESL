package postgres

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestValidCreateSessionCommandRequiresFrozenPlanIdentity(t *testing.T) {
	t.Parallel()
	command := practice.CreateSessionCommand{
		SessionID:       "22222222-2222-4222-8222-222222222222",
		PlanID:          "11111111-1111-4111-8111-111111111111",
		PlanVersion:     3,
		ClientRequestID: "session-create-0001",
		Snapshot: practice.SessionSnapshot{
			SessionID:   "22222222-2222-4222-8222-222222222222",
			PlanVersion: 3,
			Participants: []practice.Participant{{
				ID:        "33333333-3333-4333-8333-333333333333",
				SessionID: "22222222-2222-4222-8222-222222222222", Role: "LEARNER",
			}},
		},
	}
	if !validCreateSessionCommand(command) {
		t.Fatal("valid frozen Session command was rejected")
	}
	for name, mutate := range map[string]func(*practice.CreateSessionCommand){
		"plan version": func(value *practice.CreateSessionCommand) {
			value.Snapshot.PlanVersion++
		},
		"session identity": func(value *practice.CreateSessionCommand) {
			value.Snapshot.SessionID = "different-session"
		},
		"participants": func(value *practice.CreateSessionCommand) {
			value.Snapshot.Participants = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := command
			invalid.Snapshot.Participants = append(
				[]practice.Participant(nil), command.Snapshot.Participants...,
			)
			mutate(&invalid)
			if validCreateSessionCommand(invalid) {
				t.Fatal("invalid frozen Session command was accepted")
			}
		})
	}
}

func TestTransitionStatusKeepsCompletionRulesInPractice(t *testing.T) {
	t.Parallel()
	if _, _, err := transitionStatus(
		practice.SessionInProgress,
		practice.SessionComplete,
		2,
		3,
	); err == nil {
		t.Fatal("Session completed below its minimum effective Turn count")
	}
	status, reason, err := transitionStatus(
		practice.SessionInProgress,
		practice.SessionEndEarly,
		2,
		3,
	)
	if err != nil || status != practice.SessionEndedEarly || reason == "" {
		t.Fatalf("end early = (%q,%q,%v)", status, reason, err)
	}
}
