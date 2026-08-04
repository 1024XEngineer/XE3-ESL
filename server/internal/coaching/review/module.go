// Package review owns evaluation report history, explanation, and repractice.
package review

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "review" }
