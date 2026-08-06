// Package model owns Preparation domain values shared by application services,
// strategies, transports, and persistence adapters.
package model

type PreparationKind string

const (
	PreparationKindInterview PreparationKind = "interview"
	PreparationKindScenario  PreparationKind = "scenario"
)

func (kind PreparationKind) Valid() bool {
	return kind == PreparationKindInterview || kind == PreparationKindScenario
}
