package qianwen

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

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
		t.Fatalf("live Qianwen ASR failed: %v", err)
	}
	if result.Transcript == "" {
		t.Fatal("live Qianwen ASR returned an empty transcript")
	}
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
		t.Fatalf("live Qianwen TTS failed: %v", err)
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
