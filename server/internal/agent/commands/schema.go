// Package commands 把显式斜杠命令映射成 Agent 工具调用。
package commands

import (
	"encoding/json"
	"errors"
	"strings"

	agenttools "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tools"
)

var (
	ErrInvalidDefinition = errors.New("agent command: invalid definition")
	ErrDuplicateCommand  = errors.New("agent command: duplicate command")
	ErrUnknownCommand    = errors.New("agent command: unknown command")
	ErrInvalidCommand    = errors.New("agent command: invalid command")
)

type InputBuilder func(args string) (json.RawMessage, error)

type Definition struct {
	Name        string
	Aliases     []string
	Description string
	ToolName    string
	BuildInput  InputBuilder
}

type Parsed struct {
	CommandName string
	Args        string
	Invocation  agenttools.Invocation
}

// ValidateDefinition 检查命令定义是否完整，能否注册。
func ValidateDefinition(definition Definition) error {
	if strings.TrimSpace(definition.Name) == "" ||
		strings.TrimSpace(definition.Description) == "" ||
		strings.TrimSpace(definition.ToolName) == "" ||
		definition.BuildInput == nil {
		return ErrInvalidDefinition
	}
	for _, alias := range definition.Aliases {
		if strings.TrimSpace(alias) == "" {
			return ErrInvalidDefinition
		}
	}
	return nil
}

// JSONObjectInput 把命令参数编码成工具调用需要的 JSON 对象。
func JSONObjectInput(fields map[string]any) (json.RawMessage, error) {
	if fields == nil {
		fields = map[string]any{}
	}
	return json.Marshal(fields)
}
