package postgres

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestDurableSceneJobConfigurationAcceptsQualifiedModelID(t *testing.T) {
	t.Parallel()
	spec := durableSceneJobSpec{
		sceneType:       evaluation.SceneInterview,
		strategyRef:     "interview/v1",
		pipelineVersion: "pipeline/v1",
		promptVersion:   "prompt/v1",
		resultTable:     "evaluation_interview_scene_results",
	}
	configuration := durableSceneJobConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Minute,
		StrategyRef:     spec.strategyRef,
		PipelineVersion: spec.pipelineVersion,
		FullConfigHash:  sha256.Sum256([]byte("configuration")),
		PromptVersion:   spec.promptVersion,
		Provider:        "qiniu",
		Model:           "moonshotai/kimi-k2.6",
	}
	if !configuration.valid(spec) {
		t.Fatal("qualified model ID was rejected")
	}
	configuration.Model = "moonshotai//kimi-k2.6"
	if configuration.valid(spec) {
		t.Fatal("path-like model ID was accepted")
	}
}
