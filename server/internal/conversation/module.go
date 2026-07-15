// Package conversation owns questions, turns, transcripts, and media capability ports.
package conversation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }
