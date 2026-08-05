// Package handoff defines trusted operations projected from completed Agent
// runs to authenticated clients. Handoffs are never part of model output.
package handoff

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ConfirmPracticePlanType = "confirm_practice_plan"
	PracticePlanReadyStatus = "ready"
	MaxItems                = 4
)

var (
	ErrInvalid  = errors.New("agent handoff: invalid handoff")
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
	)
)

// Item is one server-generated operation attached to an assistant message.
// The first supported operation confirms one exact PracticePlan revision.
type Item struct {
	Type                     string   `json:"type"`
	Label                    string   `json:"label"`
	PracticePlanID           string   `json:"practice_plan_id"`
	PlanRevision             int      `json:"plan_revision"`
	Target                   string   `json:"target"`
	SceneName                string   `json:"scene_name"`
	PracticeExperience       string   `json:"practice_experience"`
	SceneCategory            string   `json:"scene_category"`
	PracticeMode             string   `json:"practice_mode"`
	Roles                    []string `json:"roles"`
	PracticeScope            string   `json:"practice_scope"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int      `json:"min_effective_turns"`
	MaxEffectiveTurns        int      `json:"max_effective_turns"`
	ExecutableStatus         string   `json:"executable_status"`
	ConfirmationPrompt       string   `json:"confirmation_prompt"`
}

func NewConfirmPracticePlan(item Item) (Item, error) {
	item.Type = ConfirmPracticePlanType
	item.Roles = append([]string(nil), item.Roles...)
	if err := Validate(item); err != nil {
		return Item{}, err
	}
	return item, nil
}

func Validate(item Item) error {
	if item.Type != ConfirmPracticePlanType ||
		!validText(item.Label, 100) ||
		!uuidPattern.MatchString(item.PracticePlanID) ||
		item.PlanRevision < 1 ||
		!validText(item.Target, 500) ||
		!validText(item.SceneName, 200) ||
		!validText(item.PracticeExperience, 100) ||
		!validText(item.SceneCategory, 200) ||
		!validText(item.PracticeMode, 100) ||
		!validText(item.PracticeScope, 200) ||
		item.SuggestedDurationSeconds < 1 ||
		item.MinEffectiveTurns < 1 ||
		item.MaxEffectiveTurns < item.MinEffectiveTurns ||
		item.MaxEffectiveTurns > 100 ||
		item.ExecutableStatus != PracticePlanReadyStatus ||
		!validText(item.ConfirmationPrompt, 300) ||
		!validRoles(item.Roles) {
		return ErrInvalid
	}
	return nil
}

func ValidateItems(items []Item) error {
	if len(items) > MaxItems {
		return ErrInvalid
	}
	for _, item := range items {
		if err := Validate(item); err != nil {
			return err
		}
	}
	return nil
}

func CloneItems(items []Item) []Item {
	cloned := make([]Item, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Roles = append([]string(nil), item.Roles...)
	}
	return cloned
}

func validRoles(roles []string) bool {
	if len(roles) < 1 || len(roles) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if !validText(role, 200) {
			return false
		}
		if _, exists := seen[role]; exists {
			return false
		}
		seen[role] = struct{}{}
	}
	return true
}

func validText(value string, maxRunes int) bool {
	return value == strings.TrimSpace(value) && value != "" &&
		utf8.RuneCountInString(value) <= maxRunes
}
