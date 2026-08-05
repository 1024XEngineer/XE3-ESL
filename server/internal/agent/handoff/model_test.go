package handoff

import (
	"errors"
	"testing"
)

func TestConfirmPracticePlanRequiresExactExecutablePlan(t *testing.T) {
	item, err := NewConfirmPracticePlan(validConfirmPracticePlan())
	if err != nil {
		t.Fatalf("NewConfirmPracticePlan() error = %v", err)
	}
	if item.Type != ConfirmPracticePlanType || len(item.Roles) != 1 {
		t.Fatalf("item = %#v", item)
	}

	invalid := validConfirmPracticePlan()
	invalid.PlanRevision = 0
	if _, err := NewConfirmPracticePlan(invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid revision error = %v, want ErrInvalid", err)
	}

	invalid = validConfirmPracticePlan()
	invalid.Roles = []string{"面试官", "面试官"}
	if _, err := NewConfirmPracticePlan(invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate roles error = %v, want ErrInvalid", err)
	}
}

func TestCloneItemsDoesNotShareRoles(t *testing.T) {
	original := []Item{validConfirmPracticePlan()}
	cloned := CloneItems(original)
	cloned[0].Roles[0] = "changed"
	if original[0].Roles[0] == "changed" {
		t.Fatal("CloneItems() shared role storage")
	}
}

func TestValidateItemsRejectsMoreThanMessageContractAllows(t *testing.T) {
	items := make([]Item, MaxItems+1)
	for index := range items {
		items[index] = validConfirmPracticePlan()
	}
	if err := ValidateItems(items); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ValidateItems() error = %v, want ErrInvalid", err)
	}
}

func validConfirmPracticePlan() Item {
	return Item{
		Label:                    "确认并开始练习",
		PracticePlanID:           "10000000-0000-4000-8000-000000000001",
		PlanRevision:             1,
		Target:                   "练习清晰表达项目影响力",
		SceneName:                "项目经历深挖",
		PracticeExperience:       "INTERVIEW",
		SceneCategory:            "INTERVIEW_PROFESSIONAL",
		PracticeMode:             "FULL_SIMULATION",
		Roles:                    []string{"面试官"},
		PracticeScope:            "完整模拟",
		SuggestedDurationSeconds: 600,
		MinEffectiveTurns:        3,
		MaxEffectiveTurns:        6,
		ExecutableStatus:         PracticePlanReadyStatus,
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	}
}
