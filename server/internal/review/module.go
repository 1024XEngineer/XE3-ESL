// Package review owns analysis, feedback, retries, and history projections.
package review

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "review" }
