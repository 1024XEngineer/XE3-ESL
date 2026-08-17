package scene

import "testing"

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
