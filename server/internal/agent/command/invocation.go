package command

import (
	"encoding/json"

	agenttool "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

// agentInvocation 把解析出的命令目标包装成 Agent 工具调用。
func agentInvocation(name string, input json.RawMessage) agenttool.Invocation {
	return agenttool.Invocation{Name: name, Input: input}
}
