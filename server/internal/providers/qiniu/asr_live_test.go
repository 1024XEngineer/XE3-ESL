package qiniu

import (
	"context"
	"os"
	"testing"
	"unicode/utf8"

	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

func TestLiveQiniuSpeechRecognition(t *testing.T) {
	if os.Getenv("QINIU_ASR_LIVE_TEST") != "1" {
		t.Skip(
			"set QINIU_ASR_LIVE_TEST=1 and export the Qiniu ASR variables " +
				"to run; real requests may incur charges",
		)
	}
	configuration, err := platformconfig.LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load Qiniu ASR configuration: %v", err)
	}
	if configuration.Provider != platformconfig.SpeechProviderQiniu {
		t.Fatalf("speech recognition provider = %q, want qiniu", configuration.Provider)
	}
	client, err := newASR(ASRConfig{
		BaseURL: configuration.BaseURL,
		Model:   configuration.Model,
		Timeout: configuration.Timeout,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qiniu ASR client: %v", err)
	}
	fixtures := []struct {
		name        string
		environment string
	}{
		{name: "english", environment: "QINIU_ASR_LIVE_TEST_AUDIO_EN"},
		{name: "chinese", environment: "QINIU_ASR_LIVE_TEST_AUDIO_ZH"},
		{name: "mixed", environment: "QINIU_ASR_LIVE_TEST_AUDIO_MIXED"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := os.Getenv(fixture.environment)
			if path == "" {
				t.Fatalf("%s is required for live acceptance", fixture.environment)
			}
			audio := captureQiniuLiveAudio(t, path)
			defer audio.Close()
			result, err := client.transcribeWAV(context.Background(), audio, nil)
			if err != nil {
				t.Fatalf("live Qiniu ASR failed: %v", err)
			}
			if result.transcript == "" {
				t.Fatal("live Qiniu ASR returned an empty transcript")
			}
			t.Logf(
				"Qiniu ASR success: language_case=%s request_id=%s "+
					"transcript_nonempty=true transcript_characters=%d "+
					"audio_seconds=%d fixture_bytes=%d",
				fixture.name,
				result.id,
				utf8.RuneCountInString(result.transcript),
				result.audioSeconds,
				audio.Size(),
			)
		})
	}
}

func captureQiniuLiveAudio(
	t *testing.T,
	path string,
) platformmedia.ManagedAudioSource {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open Qiniu ASR live fixture: %v", err)
	}
	defer file.Close()
	audio, err := platformmedia.CaptureTemporaryAudio(
		t.TempDir(),
		platformmedia.ContentTypeWAV,
		file,
	)
	if err != nil {
		t.Fatalf("validate Qiniu ASR live fixture: %v", err)
	}
	if audio.SampleRate() != qiniuASRSampleRate {
		audio.Close()
		t.Fatalf("Qiniu ASR live fixture sample rate = %d, want %d", audio.SampleRate(), qiniuASRSampleRate)
	}
	return audio
}
