package commands

import (
	"encoding/json"

	agenttools "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tools"
)

// agentInvocation 把解析出的命令目标包装成 Agent 工具调用。
func agentInvocation(name string, input json.RawMessage) agenttools.Invocation {
	return agenttools.Invocation{Name: name, Input: input}
}
