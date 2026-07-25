package qianwen

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

const defaultASRLiveFixture = "testdata/asr-live-fixture.wav"

func TestASRLiveFixturePassesUploadBoundary(t *testing.T) {
	audio := captureLiveASRFixture(t, defaultASRLiveFixture)
	defer audio.Close()

	if audio.MediaType() != platformmedia.ContentTypeWAV ||
		audio.SampleRate() != 16_000 ||
		audio.Duration() <= 0 ||
		audio.Duration() > 60*time.Second {
		t.Fatalf(
			"unexpected ASR fixture metadata: type=%q rate=%d duration=%s",
			audio.MediaType(),
			audio.SampleRate(),
			audio.Duration(),
		)
	}
}

func TestLiveSpeechRecognition(t *testing.T) {
	if os.Getenv("QIANWEN_ASR_LIVE_TEST") != "1" {
		t.Skip("set QIANWEN_ASR_LIVE_TEST=1 and the ASR environment variables to run")
	}
	requireConfirmedVoiceFreeQuota(t)
	audioPath := os.Getenv("QIANWEN_ASR_LIVE_TEST_AUDIO")
	if audioPath == "" {
		audioPath = defaultASRLiveFixture
	}
	audio := captureLiveASRFixture(t, audioPath)
	defer audio.Close()

	cfg, err := platformconfig.LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load speech recognition config: %v", err)
	}
	recognizer, err := NewRecognizer(ASRConfig{
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: cfg.Timeout,
	}, cfg.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qianwen recognizer: %v", err)
	}
	result, err := recognizer.Transcribe(
		context.Background(),
		ai.TranscriptionRequest{Audio: audio},
	)
	if err != nil {
		t.Fatalf("live Qianwen ASR failed: %s", safeLiveSpeechError(err))
	}
	if result.Transcript == "" {
		t.Fatal("live Qianwen ASR returned an empty transcript")
	}
	t.Logf(
		"ASR success: provider=%s model=%s request_id=%s "+
			"transcript_nonempty=%t transcript_characters=%d "+
			"usage_audio_seconds=%d fixture_type=%s fixture_bytes=%d "+
			"fixture_rate_hz=%d fixture_duration=%s",
		result.Provider,
		result.Model,
		result.ID,
		result.Transcript != "",
		utf8.RuneCountInString(result.Transcript),
		result.Usage.AudioSeconds,
		audio.MediaType(),
		audio.Size(),
		audio.SampleRate(),
		audio.Duration(),
	)
}

func TestLiveSpeechSynthesis(t *testing.T) {
	if os.Getenv("QIANWEN_TTS_LIVE_TEST") != "1" {
		t.Skip("set QIANWEN_TTS_LIVE_TEST=1 and the TTS environment variables to run")
	}
	requireConfirmedVoiceFreeQuota(t)
	cfg, err := platformconfig.LoadSpeechSynthesis()
	if err != nil {
		t.Fatalf("load speech synthesis config: %v", err)
	}
	synthesizer, err := NewSynthesizer(TTSConfig{
		BaseURL:       cfg.BaseURL,
		Model:         cfg.Model,
		Voice:         cfg.Voice,
		LanguageHint:  cfg.LanguageHint,
		Timeout:       cfg.Timeout,
		TempDirectory: cfg.TempDirectory,
	}, cfg.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qianwen synthesizer: %v", err)
	}
	result, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "Please repeat after me."},
	)
	if err != nil {
		t.Fatalf("live Qianwen TTS failed: %s", safeLiveSpeechError(err))
	}
	if result.Audio == nil {
		t.Fatal("live Qianwen TTS returned no managed audio")
	}
	defer result.Audio.Close()
	reader, err := result.Audio.Open()
	if err != nil {
		t.Fatalf("open live Qianwen TTS audio: %v", err)
	}
	defer reader.Close()
	buffer := make([]byte, 12)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatalf("read live Qianwen TTS audio: %v", err)
	}
	if string(buffer[:4]) != "RIFF" || string(buffer[8:12]) != "WAVE" {
		t.Fatal("live Qianwen TTS output is not a validated WAV")
	}
	t.Logf(
		"TTS success: provider=%s model=%s request_id=%s audio_id=%s "+
			"audio_type=%s audio_bytes=%d audio_rate_hz=%d "+
			"audio_duration=%s usage_characters=%d",
		result.Provider,
		result.Model,
		result.RequestID,
		result.AudioID,
		result.Audio.MediaType(),
		result.Audio.Size(),
		result.Audio.SampleRate(),
		result.Audio.Duration(),
		result.Usage.Characters,
	)
}

func safeLiveSpeechError(err error) string {
	var speechError *ai.SpeechError
	if !errors.As(err, &speechError) {
		return "unexpected_error"
	}
	cause := errors.Unwrap(speechError)
	causeText := ""
	if cause != nil {
		causeText = cause.Error()
	}
	return "operation=" + string(speechError.Operation) +
		" kind=" + string(speechError.Kind) +
		" status=" + strconv.Itoa(speechError.StatusCode) +
		" provider_code=" + speechError.ProviderCode +
		" request_id=" + speechError.RequestID +
		" cause=" + causeText
}

func TestSafeLiveSpeechErrorRedactsUnexpectedError(t *testing.T) {
	const sensitive = "must-not-appear"
	got := safeLiveSpeechError(errors.New(sensitive))
	if got != "unexpected_error" || strings.Contains(got, sensitive) {
		t.Fatalf("unexpected error was not redacted: %q", got)
	}
}

func captureLiveASRFixture(
	t *testing.T,
	audioPath string,
) *platformmedia.TemporaryAudio {
	t.Helper()
	input, err := os.Open(audioPath)
	if err != nil {
		t.Fatalf("open live ASR test audio: %v", err)
	}
	defer input.Close()
	audio, err := platformmedia.CaptureTemporaryAudio(
		t.TempDir(),
		platformmedia.ContentTypeWAV,
		input,
	)
	if err != nil {
		t.Fatalf("validate live ASR test audio: %v", err)
	}
	return audio
}

func requireConfirmedVoiceFreeQuota(t *testing.T) {
	t.Helper()
	if os.Getenv("QIANWEN_VOICE_FREE_QUOTA_ONLY_CONFIRMED") != "1" {
		t.Fatal(
			"confirm the console's free-quota-only protection, then set " +
				"QIANWEN_VOICE_FREE_QUOTA_ONLY_CONFIRMED=1",
		)
	}
}
