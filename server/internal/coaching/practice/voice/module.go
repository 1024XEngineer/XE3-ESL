// Package voice owns Practice Voice sessions, rounds, transcription,
// confirmation, recordings, and question playback.
package voice

// Module advertises the frozen public health-module identifier. Production
// routes are registered by the HTTP child package through Bootstrap.
type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }
