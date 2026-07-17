// Package conversation 负责问题、有效回答、转录和音频资产元数据。
package conversation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "conversation" }
