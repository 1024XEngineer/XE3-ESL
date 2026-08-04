// Package practice owns Practice Sessions and their frozen execution state.
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreateSessionRequest struct {
	ExpectedPlanRevision int  `json:"expected_plan_revision"`
	UserConfirmed        bool `json:"user_confirmed"`
}
