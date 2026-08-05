package scene

import (
	"errors"
	"testing"
)

type testEvaluationPolicyValidator map[string]bool

func (validator testEvaluationPolicyValidator) ValidateEvaluationPolicyReference(
	reference string,
) error {
	if validator[reference] {
		return nil
	}
	return errors.New("evaluation policy reference unavailable")
}

func testPolicyValidator() testEvaluationPolicyValidator {
	return testEvaluationPolicyValidator{
		"interview.shadow.evaluation.v1":         true,
		"ielts.speaking_practice.evaluation.v1":  true,
		"ielts.speaking_full_mock.evaluation.v1": true,
		"workplace.general.evaluation.v1":        true,
		"daily.general.evaluation.v1":            true,
		"disabled.fixture.evaluation.v1":         false,
	}
}

const (
	testSceneID       = "scn_test_interview"
	testRoleID        = "role_test_interviewer"
	testSecondRoleID  = "role_test_hr"
	testFullOptionID  = "option_test_full"
	testFocusOptionID = "option_test_focus"
)

func testSceneDefinition() SceneDefinition {
	return SceneDefinition{
		ID:                  testSceneID,
		Family:              SceneFamilyInterview,
		Model:               SceneModelProjectExperienceDeepDive,
		Name:                "Test interview",
		Version:             1,
		Status:              SceneStatusActive,
		TurnPolicyRef:       "interview.test.turn.v1",
		SessionPolicyRef:    "interview.test.session.v1",
		EvaluationPolicyRef: "interview.shadow.evaluation.v1",
		Prompt: ScenePrompt{
			PublicSceneBrief:         "Explain one project and its outcome.",
			PracticeGoal:             "Present evidence and trade-offs clearly.",
			UserRole:                 "Candidate",
			AIRole:                   "Interviewer",
			PersonaSummary:           "An evidence-seeking technical interviewer.",
			FocusAreas:               []string{"evidence", "tradeoff"},
			TurnBlueprints:           []string{"Ask for evidence", "Probe one trade-off"},
			SuggestedDurationSeconds: 600,
		},
		Roles: []RoleDefinition{
			{
				ID:               testRoleID,
				SceneID:          testSceneID,
				Type:             "TECHNICAL_INTERVIEWER",
				DisplayName:      "Technical interviewer",
				Responsibilities: "Probe technical evidence.",
				Style:            "Precise.",
				PracticeObjectives: []PracticeObjectiveDefinition{
					{ID: "evidence", Description: "Support claims with concrete evidence."},
				},
				DisplayOrder: 10,
			},
		},
		PracticeOptions: []PracticeOption{
			{
				ID:           testFullOptionID,
				SceneID:      testSceneID,
				Type:         PracticeOptionFullSimulation,
				DisplayName:  "Full simulation",
				DisplayOrder: 10,
			},
			{
				ID:               testFocusOptionID,
				SceneID:          testSceneID,
				RoleDefinitionID: testRoleID,
				Type:             PracticeOptionFocus,
				DisplayName:      "Technical focus",
				DisplayOrder:     20,
			},
		},
		DisplayOrder: 10,
	}
}

func mustTestCatalog(t *testing.T, definitions ...SceneDefinition) *Catalog {
	t.Helper()
	if len(definitions) == 0 {
		definitions = []SceneDefinition{testSceneDefinition()}
	}
	catalog, err := NewCatalog(definitions, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	return catalog
}

func reparentTestScene(definition *SceneDefinition) {
	for index := range definition.Roles {
		definition.Roles[index].ID = definition.ID + "-role-" + string(rune('a'+index))
		definition.Roles[index].SceneID = definition.ID
	}
	for index := range definition.PracticeOptions {
		definition.PracticeOptions[index].ID = definition.ID + "-option-" + string(rune('a'+index))
		definition.PracticeOptions[index].SceneID = definition.ID
		if definition.PracticeOptions[index].Type == PracticeOptionFocus {
			definition.PracticeOptions[index].RoleDefinitionID = definition.Roles[index-1].ID
		}
	}
}
