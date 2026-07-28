package voice

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const (
	runPersistenceTimeout = 5 * time.Second
)

func validUUID(value string) bool {
	return core.ValidUUID(value)
}

func validMessageContent(value string) bool {
	return core.ValidMessageContent(value)
}

func validClientMessageID(value string) bool {
	return core.ValidClientMessageID(value)
}

func validRunConfiguration(configuration RunConfiguration) bool {
	return core.ValidRunConfiguration(configuration)
}

func validVoiceIdempotencyKey(value string) bool {
	return value == strings.TrimSpace(value) &&
		len(value) >= 8 &&
		len(value) <= 128 &&
		!strings.ContainsAny(value, "\r\n\x00")
}

func validVoiceTranscription(result ai.TranscriptionResult) bool {
	return core.ValidModelID(result.ID) &&
		core.ValidProviderID(result.Provider) &&
		core.ValidModelID(result.Model) &&
		core.ValidMessageContent(result.Transcript) &&
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
