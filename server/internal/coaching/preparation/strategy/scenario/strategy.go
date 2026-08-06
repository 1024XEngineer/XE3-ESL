// Package scenario implements Preparation resolution for ordinary roleplay.
package scenario

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
)

const maxFieldLength = 16 * 1024

type Strategy struct{}

func New() *Strategy { return &Strategy{} }

func (*Strategy) Kind() model.PreparationKind {
	return model.PreparationKindScenario
}

func (*Strategy) Resolve(
	ctx context.Context,
	command port.ResolveCommand,
) (model.ResolvedContext, error) {
	if ctx == nil || !command.Actor.Valid() || !command.Input.ValidShape() ||
		command.Input.Kind != model.PreparationKindScenario {
		return model.ResolvedContext{}, model.ErrInvalidContext
	}
	input := *command.Input.Scenario
	resolved := model.ScenarioContextSnapshot{
		Situation:          fallback(input.Situation, command.ScenarioDefaults.Situation),
		UserRole:           fallback(input.UserRole, command.ScenarioDefaults.UserRole),
		CounterpartRole:    fallback(input.CounterpartRole, command.ScenarioDefaults.CounterpartRole),
		Goal:               fallback(input.Goal, command.ScenarioDefaults.Goal),
		CounterpartPersona: fallback(input.CounterpartPersona, command.ScenarioDefaults.CounterpartPersona),
	}
	if !validText(resolved.Situation) || !validText(resolved.UserRole) ||
		!validText(resolved.CounterpartRole) || !validText(resolved.Goal) ||
		!validText(resolved.CounterpartPersona) {
		return model.ResolvedContext{}, model.ErrInvalidContext
	}
	return model.ResolvedContext{
		Kind:     model.PreparationKindScenario,
		Scenario: &resolved,
	}, nil
}

func fallback(value string, defaultValue string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(defaultValue)
}

func validText(value string) bool {
	return value != "" && utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= maxFieldLength &&
		!strings.ContainsRune(value, '\x00')
}

var _ port.PreparationStrategy = (*Strategy)(nil)
