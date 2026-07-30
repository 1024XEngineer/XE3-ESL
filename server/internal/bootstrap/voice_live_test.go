package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

func TestLiveVoiceReviewGeneration(t *testing.T) {
	if os.Getenv("QIANWEN_LIVE_TEST") != "1" {
		t.Skip(
			"set QIANWEN_LIVE_TEST=1 and the Qianwen environment variables " +
				"to run; the real request may incur charges",
		)
	}
	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	provider, err := NewTextGenerator(configuration)
	if err != nil {
		t.Fatalf("create text generator: %v", err)
	}
	generator := &voiceReviewGenerator{
		generator: provider,
		timeout:   configuration.Timeout,
	}
	generated, err := generator.GenerateReview(
		context.Background(),
		review.ReviewGenerationInput{
			ReviewID:              "live-review",
			ImplementationVersion: voiceReviewImplementation,
			Source:                bootstrapReviewSource(t),
		},
	)
	if err != nil {
		t.Fatalf("live voice Review generation: %v", err)
	}
	if len(generated.Result.Conclusions) != 5 ||
		len(generated.EvidenceLinks) < len(generated.Result.Conclusions) {
		t.Fatalf(
			"live voice Review shape: conclusions=%d evidence_links=%d",
			len(generated.Result.Conclusions),
			len(generated.EvidenceLinks),
		)
	}
}
