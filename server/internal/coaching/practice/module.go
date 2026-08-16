// Package practice owns Practice Sessions and their frozen execution state.
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreateSessionRequest struct {
	ExpectedPlanVersion int `json:"expected_plan_version"`
}
