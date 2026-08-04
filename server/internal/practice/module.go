// Package practice owns Practice Sessions and their frozen execution state.
package practice

import "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }

type CreateSessionRequest struct {
	ExpectedPlanRevision int  `json:"expected_plan_revision"`
	UserConfirmed        bool `json:"user_confirmed"`
}

type StartConfirmation struct {
	AgentThreadID        string
	PracticePlanID       string
	ExpectedPlanRevision int
}

type ConfirmAndStartResult struct {
	Bootstrap      persistence.ContextSessionBootstrap
	Replayed       bool
	ActiveConflict bool
}
