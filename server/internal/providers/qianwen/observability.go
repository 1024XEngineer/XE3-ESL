package qianwen

import (
	"context"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func recordTextCall(
	recorder providerobservability.Recorder,
	provider string,
	startedAt time.Time,
	usage protocol.TokenUsage,
	err error,
) {
	if recorder == nil {
		return
	}
	recorder.Record(providerobservability.Observation{
		Provider:   observedTextProvider(provider),
		Capability: providerobservability.CapabilityTextGeneration,
		Duration:   time.Since(startedAt),
		ErrorKind:  observedErrorKind(err),
		Usage: providerobservability.Usage{
			Tokens: float64(usage.TotalTokens),
		},
	})
}

func recordSpeechCall(
	recorder providerobservability.Recorder,
	capability providerobservability.Capability,
	startedAt time.Time,
	usage protocol.SpeechUsage,
	err error,
	characters int,
) {
	if recorder == nil {
		return
	}
	if usage.Characters > 0 {
		characters = usage.Characters
	}
	recorder.Record(providerobservability.Observation{
		Provider:   providerobservability.ProviderQianwen,
		Capability: capability,
		Duration:   time.Since(startedAt),
		ErrorKind:  observedErrorKind(err),
		Usage: providerobservability.Usage{
			Tokens:       float64(usage.TotalTokens),
			AudioSeconds: float64(usage.AudioSeconds),
			Characters:   float64(characters),
		},
	})
}

func observedTextProvider(provider string) providerobservability.Provider {
	switch provider {
	case providerName:
		return providerobservability.ProviderQianwen
	case qiniuProviderName:
		return providerobservability.ProviderQiniu
	default:
		panic("qianwen: unregistered observable text provider")
	}
}

func observedErrorKind(err error) providerobservability.ErrorKind {
	if err == nil {
		return providerobservability.ErrorNone
	}
	var generationError *protocol.GenerationError
	if errors.As(err, &generationError) {
		return observedStableErrorKind(string(generationError.Kind))
	}
	var speechError *protocol.SpeechError
	if errors.As(err, &speechError) {
		return observedStableErrorKind(string(speechError.Kind))
	}
	var stableError interface{ StableCategory() string }
	if errors.As(err, &stableError) {
		return observedStableErrorKind(stableError.StableCategory())
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return providerobservability.ErrorTimeout
	case errors.Is(err, context.Canceled):
		return providerobservability.ErrorCancelled
	default:
		return providerobservability.ErrorProviderUnavailable
	}
}

func observedStableErrorKind(kind string) providerobservability.ErrorKind {
	switch protocol.ErrorKind(kind) {
	case protocol.ErrorInvalidRequest:
		return providerobservability.ErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return providerobservability.ErrorConfiguration
	case protocol.ErrorAuthentication:
		return providerobservability.ErrorAuthentication
	case protocol.ErrorAuthorization:
		return providerobservability.ErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return providerobservability.ErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return providerobservability.ErrorRateLimited
	case protocol.ErrorTimeout:
		return providerobservability.ErrorTimeout
	case protocol.ErrorInvalidResponse:
		return providerobservability.ErrorInvalidResponse
	case protocol.ErrorCancelled:
		return providerobservability.ErrorCancelled
	default:
		return providerobservability.ErrorProviderUnavailable
	}
}

func downstreamSpeechCallbackError(
	operation protocol.SpeechOperation,
	err error,
) error {
	if err == nil {
		return nil
	}
	return protocol.NewSpeechError(
		operation,
		protocol.ErrorCancelled,
		0,
		"",
		"",
		err,
	)
}

func downstreamSpeechConsumer(consume func([]byte) error) func([]byte) error {
	if consume == nil {
		return nil
	}
	return func(audio []byte) error {
		return downstreamSpeechCallbackError(
			protocol.SpeechOperationSynthesis,
			consume(audio),
		)
	}
}

type observedAssistantSpeechSession struct {
	delegate   agentconversation.AssistantSpeechSession
	recorder   providerobservability.Recorder
	startedAt  time.Time
	characters int
	recorded   sync.Once
	state      sync.Mutex
	finished   bool
}

func (session *observedAssistantSpeechSession) AppendText(text string) error {
	err := session.delegate.AppendText(text)
	if err != nil {
		session.finish(err)
		return err
	}
	session.state.Lock()
	session.characters += utf8.RuneCountInString(text)
	session.state.Unlock()
	return nil
}

func (session *observedAssistantSpeechSession) Finish() error {
	session.state.Lock()
	session.finished = true
	session.state.Unlock()
	err := session.delegate.Finish()
	session.finish(err)
	return err
}

func (session *observedAssistantSpeechSession) Close() error {
	err := session.delegate.Close()
	session.state.Lock()
	finished := session.finished
	session.state.Unlock()
	if !finished {
		session.finish(context.Canceled)
		return err
	}
	session.finish(err)
	return err
}

func (session *observedAssistantSpeechSession) finish(err error) {
	session.recorded.Do(func() {
		characters := 0
		kind := observedErrorKind(err)
		if kind == providerobservability.ErrorNone ||
			kind == providerobservability.ErrorInvalidResponse {
			session.state.Lock()
			characters = session.characters
			session.state.Unlock()
		}
		recordSpeechCall(
			session.recorder,
			providerobservability.CapabilitySpeechSynthesis,
			session.startedAt,
			protocol.SpeechUsage{},
			err,
			characters,
		)
	})
}

func observedSynthesisCharacters(text string, err error) int {
	kind := observedErrorKind(err)
	if kind != providerobservability.ErrorNone &&
		kind != providerobservability.ErrorInvalidResponse {
		return 0
	}
	return utf8.RuneCountInString(text)
}

var _ agentconversation.AssistantSpeechSession = (*observedAssistantSpeechSession)(nil)
