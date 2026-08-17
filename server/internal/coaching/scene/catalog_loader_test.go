package scene

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuiltinCatalogLoadsVersionedRepositoryContent(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if len(definitions) != 24 {
		t.Fatalf("built-in Scene count = %d, want 24", len(definitions))
	}
	ieltsScene, err := catalog.GetScene(
		context.Background(),
		"scn_ielts_speaking",
	)
	if err != nil {
		t.Fatalf("GetScene(IELTS) error = %v", err)
	}
	if ieltsScene.Experience != PracticeExperienceIELTSSpeaking ||
		len(ieltsScene.PracticeOptions) != 4 {
		t.Fatalf("IELTS Scene = %#v", ieltsScene)
	}
	interview, err := catalog.GetScene(
		context.Background(),
		"scn_interview_behavioral",
	)
	if err != nil {
		t.Fatalf("GetScene(interview) error = %v", err)
	}
	if got := interview.PracticeOptions[0].SessionPolicyRef; got != "interview.user_controlled.session.v1" {
		t.Fatalf("interview Session Policy = %q", got)
	}
}

func TestBuiltinCatalogOmitsSocialInvitation(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	if _, err := catalog.GetScene(context.Background(), "scn_daily_social_invitation"); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("GetScene(social invitation) error = %v", err)
	}
}

func TestBuiltinCatalogDefinesDistinctRentalScenes(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	tests := []struct {
		sceneID        string
		name           string
		userRole       string
		aiRole         string
		practiceGoal   string
		firstBlueprint string
		roleID         string
		fullOptionID   string
		focusOptionID  string
	}{
		{
			sceneID:        "scn_daily_rental_viewing",
			name:           "看房与租赁咨询",
			userRole:       "准租客",
			aiRole:         "房产中介",
			practiceGoal:   "了解房屋条件、租金费用、租期和入住要求，判断房屋是否合适。",
			firstBlueprint: "欢迎用户看房，并询问最关注的房屋信息",
			roleID:         "role_daily_rental_viewing_counterpart",
			fullOptionID:   "option_daily_rental_viewing_full",
			focusOptionID:  "option_daily_rental_viewing_focus",
		},
		{
			sceneID:        "scn_daily_rental_maintenance",
			name:           "租房报修",
			userRole:       "租客",
			aiRole:         "物业工作人员",
			practiceGoal:   "准确描述故障及影响，协商维修时间、进入方式、责任和后续安排。",
			firstBlueprint: "确认报修请求，并请租客描述具体故障",
			roleID:         "role_daily_rental_maintenance_counterpart",
			fullOptionID:   "option_daily_rental_maintenance_full",
			focusOptionID:  "option_daily_rental_maintenance_focus",
		},
	}
	for _, test := range tests {
		t.Run(test.sceneID, func(t *testing.T) {
			definition, err := catalog.GetScene(context.Background(), test.sceneID)
			if err != nil {
				t.Fatalf("GetScene() error = %v", err)
			}
			if definition.Name != test.name ||
				definition.Prompt.UserRole != test.userRole ||
				definition.Prompt.AIRole != test.aiRole ||
				definition.Prompt.PracticeGoal != test.practiceGoal ||
				len(definition.Prompt.TurnBlueprints) != 4 ||
				definition.Prompt.TurnBlueprints[0] != test.firstBlueprint {
				t.Fatalf("rental Scene = %#v", definition)
			}
			for _, selection := range []struct {
				optionID string
				mode     PracticeMode
			}{
				{optionID: test.fullOptionID, mode: PracticeModeFullSimulation},
				{optionID: test.focusOptionID, mode: PracticeModeFocus},
			} {
				snapshot, resolveErr := catalog.ResolveSelection(
					context.Background(),
					test.sceneID,
					1,
					[]string{test.roleID},
					selection.optionID,
				)
				if resolveErr != nil {
					t.Fatalf("ResolveSelection(%s) error = %v", selection.mode, resolveErr)
				}
				if snapshot.Scene.ID != test.sceneID ||
					snapshot.PracticeOptionID != selection.optionID {
					t.Fatalf("selection = %#v", snapshot)
				}
			}
		})
	}
}

func TestLoadCatalogRejectsUnknownSchemaAndFields(t *testing.T) {
	valid, err := json.Marshal(catalogDocument{
		SchemaVersion: 1,
		Scenes:        []SceneDefinition{testSceneDefinition()},
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	for _, document := range []string{
		`{"schema_version":2,"scenes":[]}`,
		`{"schema_version":1,"scenes":[],"unknown":true}`,
		strings.Replace(string(valid), `"scene_id"`, `"unknown":true,"scene_id"`, 1),
	} {
		if _, err := LoadCatalog(
			strings.NewReader(document),
			testPolicyValidator(),
		); !errors.Is(err, ErrCatalogDefinitionInvalid) {
			t.Fatalf("LoadCatalog() error = %v", err)
		}
	}
}

func TestLoadCatalogRejectsInvalidOrDuplicateResourceIdentity(t *testing.T) {
	tests := map[string]func(*catalogDocument){
		"scene id characters": func(document *catalogDocument) {
			document.Scenes[0].ID = "scene/invalid"
		},
		"role id characters": func(document *catalogDocument) {
			document.Scenes[0].Roles[0].ID = "role invalid"
		},
		"option id characters": func(document *catalogDocument) {
			document.Scenes[0].PracticeOptions[0].ID = "option\x00invalid"
		},
		"zero scene version": func(document *catalogDocument) {
			document.Scenes[0].Version = 0
		},
		"invalid turn policy reference": func(document *catalogDocument) {
			document.Scenes[0].PracticeOptions[0].TurnPolicyRef =
				"bad ref.turn.v1"
		},
		"invalid session policy reference": func(document *catalogDocument) {
			document.Scenes[0].PracticeOptions[0].SessionPolicyRef =
				"../bad.session.v1"
		},
		"invalid evaluation policy reference": func(document *catalogDocument) {
			document.Scenes[0].PracticeOptions[0].EvaluationPolicyRef =
				"bad/ref.evaluation.v1"
		},
		"duplicate scene": func(document *catalogDocument) {
			document.Scenes = append(document.Scenes, document.Scenes[0])
		},
		"duplicate role": func(document *catalogDocument) {
			document.Scenes[0].Roles = append(
				document.Scenes[0].Roles,
				document.Scenes[0].Roles[0],
			)
		},
		"duplicate option": func(document *catalogDocument) {
			document.Scenes[0].PracticeOptions = append(
				document.Scenes[0].PracticeOptions,
				document.Scenes[0].PracticeOptions[0],
			)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			document := catalogDocument{
				SchemaVersion: 1,
				Scenes:        []SceneDefinition{testSceneDefinition()},
			}
			mutate(&document)
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			if _, err := LoadCatalog(
				strings.NewReader(string(encoded)),
				testPolicyValidator(),
			); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("LoadCatalog() error = %v", err)
			}
		})
	}
}
