package commands

import (
	"encoding/json"

	agenttools "github.com/1024XEngineer/XE3-ESL/server/internal/agent/tools"
)

// agentInvocation wraps a parsed command target as an Agent tool invocation.
func agentInvocation(name string, input json.RawMessage) agenttools.Invocation {
	return agenttools.Invocation{Name: name, Input: input}
}
