package transport

import (
	"reflect"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const maxAgentPageSize = agentconversation.MaxPageSize

func validUUID(value string) bool {
	return agentconversation.ValidUUID(value)
}

func nilVoiceDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
