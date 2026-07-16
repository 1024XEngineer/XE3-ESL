// Package conversation owns questions, valid answer turns, transcripts, and
// audio asset metadata for an interview conversation.
package conversation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }
