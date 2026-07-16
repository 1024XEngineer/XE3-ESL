// Package assistant coordinates user intents across the public services exposed
// by SpeakUp's business modules. It does not own their domain data.
package assistant

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "assistant" }
