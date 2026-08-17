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
	if len(definitions) != 25 {
		t.Fatalf("built-in Scene count = %d, want 25", len(definitions))
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

func TestBuiltinCatalogSplitsClientDelayAndRequirementClarification(t *testing.T) {
	catalog, err := NewBuiltinCatalog(testPolicyValidator())
	if err != nil {
		t.Fatalf("NewBuiltinCatalog() error = %v", err)
	}
	tests := []struct {
		id, name, userRole, aiRole, goal, firstTurn string
	}{
		{"scn_workplace_client_delay", "客户延期沟通", "项目负责人", "客户代表", "说明延期原因、当前状态和业务影响，提出替代方案并确认新的交付时间。", "询问当前交付状态，以及为什么不能按原计划完成"},
		{"scn_workplace_requirement_clarification", "客户需求澄清", "解决方案负责人", "客户业务负责人", "澄清业务目标、范围、优先级、时间和验收标准。", "给出一个较模糊的业务需求，并邀请用户进一步提问"},
	}
	for _, test := range tests {
		definition, err := catalog.GetScene(context.Background(), test.id)
		if err != nil {
			t.Fatalf("GetScene(%s) error = %v", test.id, err)
		}
		if definition.Name != test.name || definition.Prompt.UserRole != test.userRole ||
			definition.Prompt.AIRole != test.aiRole || definition.Prompt.PracticeGoal != test.goal ||
			definition.Prompt.TurnBlueprints[0] != test.firstTurn || len(definition.PracticeOptions) != 2 {
			t.Fatalf("Scene(%s) = %#v", test.id, definition)
		}
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
