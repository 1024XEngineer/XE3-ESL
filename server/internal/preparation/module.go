// Package preparation owns scenarios, roles, and confirmed background snapshots.
package preparation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "preparation" }
