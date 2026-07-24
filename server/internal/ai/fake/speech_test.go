package fake

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

func TestSpeechRecognizerReturnsDeterministicResult(t *testing.T) {
	t.Parallel()

	expected := ai.TranscriptionResult{
		ID:         "fake-asr-1",
		Provider:   "fake",
		Model:      "deterministic",
		Transcript: "A stable transcript.",
	}
	recognizer := NewSpeechRecognizer(expected)
	request := validFakeTranscriptionRequest()

	first, err := recognizer.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatalf("first transcription failed: %v", err)
	}
	second, err := recognizer.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatalf("second transcription failed: %v", err)
	}
	if first != expected || second != expected {
		t.Fatalf("fake result changed: first=%#v second=%#v", first, second)
	}
}

func TestSpeechSynthesizerReturnsDeterministicMetadata(t *testing.T) {
	t.Parallel()

	audio := &fakeManagedAudio{}
	expected := ai.SynthesisResult{
		RequestID: "fake-tts-1",
		Provider:  "fake",
		Model:     "deterministic",
		AudioID:   "audio-fake-1",
		Audio:     audio,
	}
	synthesizer := NewSpeechSynthesizer(expected)

	first, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "Repeat after me."},
	)
	if err != nil {
		t.Fatalf("first synthesis failed: %v", err)
	}
	second, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "Repeat after me."},
	)
	if err != nil {
		t.Fatalf("second synthesis failed: %v", err)
	}
	if first.RequestID != expected.RequestID ||
		second.RequestID != expected.RequestID ||
		first.Audio != audio ||
		second.Audio != audio {
		t.Fatalf("fake result changed: first=%#v second=%#v", first, second)
	}
}

func TestFakeSpeechProvidersRespectCancellationAndValidation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewSpeechRecognizer(ai.TranscriptionResult{}).Transcribe(
		ctx,
		ai.TranscriptionRequest{},
	)
	assertFakeSpeechError(t, err, ai.ErrorCancelled)

	_, err = NewSpeechSynthesizer(ai.SynthesisResult{}).Synthesize(
		context.Background(),
		ai.SynthesisRequest{},
	)
	assertFakeSpeechError(t, err, ai.ErrorInvalidRequest)
}

func TestFakeSpeechProvidersCanFailDeterministically(t *testing.T) {
	t.Parallel()

	expected := errors.New("controlled failure")
	_, err := NewFailingSpeechRecognizer(expected).Transcribe(
		context.Background(),
		validFakeTranscriptionRequest(),
	)
	if !errors.Is(err, expected) {
		t.Fatalf("expected controlled ASR error, got %v", err)
	}
	_, err = NewFailingSpeechSynthesizer(expected).Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "question"},
	)
	if !errors.Is(err, expected) {
		t.Fatalf("expected controlled TTS error, got %v", err)
	}
}

func validFakeTranscriptionRequest() ai.TranscriptionRequest {
	return ai.TranscriptionRequest{Audio: fakeAudioSource{}}
}

func assertFakeSpeechError(t *testing.T, err error, kind ai.ErrorKind) {
	t.Helper()
	var speechError *ai.SpeechError
	if !errors.As(err, &speechError) || speechError.Kind != kind {
		t.Fatalf("expected %s speech error, got %v", kind, err)
	}
}

type fakeAudioSource struct{}

func (fakeAudioSource) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio")), nil
}

func (fakeAudioSource) MediaType() string       { return platformmedia.ContentTypeWAV }
func (fakeAudioSource) Size() int64             { return 5 }
func (fakeAudioSource) Duration() time.Duration { return time.Second }
func (fakeAudioSource) SampleRate() int         { return 16_000 }

type fakeManagedAudio struct {
	fakeAudioSource
}

func (*fakeManagedAudio) Close() error { return nil }
