// Package practice owns practice plans, sessions, and policy snapshots.
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }
