package model

// ContextInput is a tagged union. Exactly one payload must match Kind.
// Transport DTOs are converted to this value before entering the service layer.
type ContextInput struct {
	Kind      PreparationKind
	Interview *InterviewContextInput
	Scenario  *ScenarioContextInput
}

func (input ContextInput) ValidShape() bool {
	switch input.Kind {
	case PreparationKindInterview:
		return input.Interview != nil && input.Scenario == nil
	case PreparationKindScenario:
		return input.Interview == nil && input.Scenario != nil
	default:
		return false
	}
}

// ResolvedContext is safe to freeze into a Preparation Snapshot. External
// references have been ownership- and version-checked by the selected strategy.
type ResolvedContext struct {
	Kind      PreparationKind           `json:"kind"`
	Interview *InterviewContextSnapshot `json:"interview,omitempty"`
	Scenario  *ScenarioContextSnapshot  `json:"scenario,omitempty"`
}

func (context ResolvedContext) ValidShape() bool {
	switch context.Kind {
	case PreparationKindInterview:
		return context.Interview != nil && context.Scenario == nil
	case PreparationKindScenario:
		return context.Interview == nil && context.Scenario != nil
	default:
		return false
	}
}
