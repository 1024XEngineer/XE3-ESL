package voice

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
)

const runPersistenceTimeout = 5 * time.Second

func ValidUUID(value string) bool {
	return conversation.ValidUUID(value)
}

func ValidClientMessageID(value string) bool {
	return conversation.ValidClientMessageID(value)
}

func ValidMessageContent(value string) bool {
	return conversation.ValidMessageContent(value)
}

func ValidProviderID(value string) bool {
	return run.ValidProviderID(value)
}

func ValidModelID(value string) bool {
	return run.ValidModelID(value)
}

func validConfiguration(configuration run.Configuration) bool {
	return run.ValidConfiguration(configuration)
}

func validIdempotencyKey(value string) bool {
	return value == strings.TrimSpace(value) &&
		len(value) >= 8 &&
		len(value) <= 128 &&
		!strings.ContainsAny(value, "\r\n\x00")
}

func ValidTranscription(result TranscriptionResult) bool {
	return ValidModelID(result.ID) &&
		ValidProviderID(result.Provider) &&
		ValidModelID(result.Model) &&
		ValidMessageContent(result.Transcript) &&
		len(result.Language) <= 64 &&
		len(result.Emotion) <= 64 &&
		len(result.FinishReason) <= 64
}

func runPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		runPersistenceTimeout,
	)
}

func nilDependency(value any) bool {
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
