package protocol

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

func TestValidateTranscriptionRequest(t *testing.T) {
	t.Parallel()

	valid := TranscriptionRequest{Audio: speechTestAudio{
		mediaType:  platformmedia.ContentTypeWAV,
		size:       100,
		duration:   time.Second,
		sampleRate: 16_000,
	}}
	if err := ValidateTranscriptionRequest(valid); err != nil {
		t.Fatalf("valid transcription request rejected: %v", err)
	}
	if err := ValidateTranscriptionRequest(TranscriptionRequest{}); err == nil {
		t.Fatal("missing audio was accepted")
	}
}

func TestValidateSynthesisRequest(t *testing.T) {
	t.Parallel()

	if err := ValidateSynthesisRequest(SynthesisRequest{Text: "Repeat after me."}); err != nil {
		t.Fatalf("valid synthesis request rejected: %v", err)
	}
	tests := map[string]string{
		"blank":         " \n\t",
		"invalid UTF-8": string([]byte{0xff}),
		"too long":      strings.Repeat("a", MaxSynthesisTextRunes+1),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateSynthesisRequest(SynthesisRequest{Text: text}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSpeechErrorHasStableSafeSemantics(t *testing.T) {
	t.Parallel()

	cause := context.DeadlineExceeded
	err := NewSpeechError(
		SpeechOperationTranscription,
		ErrorTimeout,
		0,
		"RequestTimeOut",
		"request-safe-id",
		cause,
	)
	if got := err.Error(); got != "speech transcription failed: timeout" {
		t.Fatalf("unexpected safe error string: %q", got)
	}
	if !err.Retryable() {
		t.Fatal("timeout must be retryable")
	}
	if !errors.Is(err, cause) {
		t.Fatal("speech error must retain the machine-readable safe cause")
	}
}

type speechTestAudio struct {
	mediaType  string
	size       int64
	duration   time.Duration
	sampleRate int
}

func (audio speechTestAudio) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio")), nil
}

func (audio speechTestAudio) MediaType() string       { return audio.mediaType }
func (audio speechTestAudio) Size() int64             { return audio.size }
func (audio speechTestAudio) Duration() time.Duration { return audio.duration }
func (audio speechTestAudio) SampleRate() int         { return audio.sampleRate }
