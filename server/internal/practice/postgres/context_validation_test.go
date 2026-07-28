package postgres

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestValidCreatePlanCommandAcceptsOptionalMatter(t *testing.T) {
	command := persistence.CreatePlanCommand{
		PlanID:                    "plan-1",
		AgentThreadID:             "thread-1",
		ScenarioDefinitionID:      "scenario-1",
		ScenarioDefinitionVersion: 1,
		ScenarioType:              persistence.ScenarioFamilyDaily,
		ScenarioModel: persistence.
			ScenarioModelHotelCheckinAndIssueHandling,
		ScenarioConfigID:      "config-1",
		ScenarioConfigVersion: 1,
		PreparationProfileID:  "profile-1",
		SelectedRoleIDs:       []string{"role-1"},
		Intent: persistence.ContextIdempotencyIntent{
			Method:        "POST",
			CanonicalPath: "/v1/practice-plans",
			Key:           "plan-key-1",
		},
	}
	if !validCreatePlanCommand(command) {
		t.Fatal("Matter-free scenario Plan command was rejected")
	}

	command.MatterID = "matter-1"
	if !validCreatePlanCommand(command) {
		t.Fatal("Matter-backed scenario Plan command was rejected")
	}
}
