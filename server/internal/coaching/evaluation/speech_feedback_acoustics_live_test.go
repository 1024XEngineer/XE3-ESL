package evaluation_test

import (
	"context"
	"os"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestSpeechFeedbackAcousticsLive(t *testing.T) {
	if os.Getenv("XFYUN_ISE_LIVE_TEST") != "1" {
		t.Skip("set XFYUN_ISE_LIVE_TEST=1 to call iFlytek ISE")
	}
	audioPath := os.Getenv("XFYUN_ISE_LIVE_TEST_AUDIO")
	referenceText := os.Getenv("XFYUN_ISE_LIVE_TEST_TEXT")
	if audioPath == "" || referenceText == "" {
		t.Fatal("live ISE audio and text are required")
	}
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read live audio: %v", err)
	}
	configuration, err := config.LoadISE()
	if err != nil {
		t.Fatalf("load ISE config: %v", err)
	}
	evaluator, err := xfyun.NewEvaluator(
		xfyun.ISEConfig{
			Endpoint: configuration.Endpoint,
			Timeout:  configuration.Timeout,
		},
		configuration.AppID.Reveal(),
		configuration.APIKey.Reveal(),
		configuration.APISecret.Reveal(),
	)
	if err != nil {
		t.Fatalf("create ISE evaluator: %v", err)
	}
	provider, err := evaluation.NewXFYUNSpeechFeedbackAcousticProvider(
		liveSpeechFeedbackAudioReader{audio: audio},
		evaluator,
	)
	if err != nil {
		t.Fatalf("create acoustic provider: %v", err)
	}
	evidence, err := provider.EvaluateSpeechFeedbackAcoustics(
		context.Background(),
		evaluation.SpeechFeedbackAcousticInput{
			OwnerUserID:       "f475b521-a96f-44be-b447-8b85bed7e6e9",
			AudioAssetID:      "live_fixture",
			AudioAssetVersion: 1,
			AudioChecksum:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ConfirmedText:     referenceText,
		},
	)
	if err != nil {
		t.Fatalf("evaluate live acoustics: %v", err)
	}
	t.Logf(
		"ISE success: accuracy=%.2f fluency=%.2f integrity=%.2f category=%s fields=%d",
		*evidence.Assessment.AccuracyScore,
		*evidence.Assessment.FluencyScore,
		*evidence.Assessment.IntegrityScore,
		evidence.Assessment.Category,
		len(evidence.AvailableFields),
	)
}

type liveSpeechFeedbackAudioReader struct {
	audio []byte
}

func (reader liveSpeechFeedbackAudioReader) ReadSpeechFeedbackAudio(
	context.Context,
	string,
	string,
	string,
	string,
) ([]byte, error) {
	return reader.audio, nil
}
