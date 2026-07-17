// Package practice 管理练习计划、场次和推进策略
package practice

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "practice" }
