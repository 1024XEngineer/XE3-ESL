// Package assistant 负责跨业务模块编排用户意图，但不拥有各模块的领域数据。
package assistant

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "assistant" }
