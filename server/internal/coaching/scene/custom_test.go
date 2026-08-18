package scene

import "testing"

func TestCustomSceneCompilerCompilesScenarioOnlyIntentExplicitly(t *testing.T) {
	spec, err := (CustomSceneCompiler{}).Compile(CustomSceneDraft{
		Scenario:       "在国外宠物店沟通鹦鹉寄养",
		ExperienceHint: PracticeExperienceLifeAndTravel,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if spec.Scenario != "在国外宠物店沟通鹦鹉寄养" ||
		spec.UserRole != "场景中的英语学习者" ||
		spec.AIRole != "场景中的对话方" ||
		spec.PracticeGoal !=
			"用英语清晰、自然地完成“在国外宠物店沟通鹦鹉寄养”场景中的沟通" {
		t.Fatalf("compiled spec = %#v", spec)
	}
}

func TestCustomSceneCompilerPreservesUserAuthoredFacts(t *testing.T) {
	draft := CustomSceneDraft{
		Scenario:       "在展会上介绍机器人",
		UserRole:       "销售",
		AIRole:         "潜在客户",
		PracticeGoal:   "说明产品价值",
		ExperienceHint: PracticeExperienceWorkplace,
	}
	spec, err := (CustomSceneCompiler{}).Compile(draft)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if spec.UserRole != draft.UserRole || spec.AIRole != draft.AIRole ||
		spec.PracticeGoal != draft.PracticeGoal {
		t.Fatalf("compiled spec = %#v", spec)
	}
}

func TestNewCustomSelectionBuildsPlanScopedExecutableSnapshot(t *testing.T) {
	selection, err := NewCustomSelection("11111111-1111-4111-8111-111111111111", CustomSceneSpec{
		Scenario:       "在国外展会上介绍工业机器人",
		UserRole:       "参展公司的销售",
		AIRole:         "对价格和安全性有疑虑的潜在客户",
		PracticeGoal:   "介绍产品价值并回应客户疑虑",
		ExperienceHint: PracticeExperienceWorkplace,
	})
	if err != nil {
		t.Fatalf("NewCustomSelection() error = %v", err)
	}
	if selection.Source.Type != SceneSourceCustom ||
		selection.Source.SceneID != "" || selection.Source.SceneVersion != 0 ||
		selection.Scene.Key != "custom:11111111-1111-4111-8111-111111111111" ||
		selection.Scene.Revision != 1 ||
		selection.Scene.Experience != PracticeExperienceWorkplace ||
		selection.Scene.Category != SceneCategoryWorkplaceGeneral ||
		selection.Scene.Prompt.PracticeGoal != "介绍产品价值并回应客户疑虑" {
		t.Fatalf("selection = %#v", selection)
	}
	option, err := selection.PracticeOption()
	if err != nil || option.EvaluationPolicyRef != "workplace.general.evaluation.v1" {
		t.Fatalf("PracticeOption() = %#v, %v", option, err)
	}
}

func TestNewCustomSelectionRejectsSpecializedAndIncompleteSpecs(t *testing.T) {
	valid := CustomSceneSpec{
		Scenario:       "场景",
		UserRole:       "用户",
		AIRole:         "对话方",
		PracticeGoal:   "完成目标",
		ExperienceHint: PracticeExperienceLifeAndTravel,
	}
	for name, mutate := range map[string]func(*CustomSceneSpec){
		"missing role": func(spec *CustomSceneSpec) { spec.UserRole = "" },
		"interview": func(spec *CustomSceneSpec) {
			spec.ExperienceHint = PracticeExperienceInterview
		},
		"ielts": func(spec *CustomSceneSpec) {
			spec.ExperienceHint = PracticeExperienceIELTSSpeaking
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid
			mutate(&spec)
			if _, err := NewCustomSelection("11111111-1111-4111-8111-111111111111", spec); err != ErrCustomSceneInvalid {
				t.Fatalf("NewCustomSelection() error = %v", err)
			}
		})
	}
}
