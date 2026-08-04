package bootstrap

import (
	"testing"
	"time"
)

func TestInterviewShadowRuntimeConfigurationIsDeterministic(t *testing.T) {
	t.Parallel()
	configuration := EvaluationConfiguration{
		Provider:        "qianwen",
		Model:           "qwen-plus",
		MaxOutputTokens: 2048,
		LeaseDuration:   30 * time.Second,
		MaxAttempts:     3,
	}
	first, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Valid() {
		t.Fatalf("runtime configuration is unstable: %#v %#v", first, second)
	}
	configuration.Model = "qwen-max"
	changed, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("model change did not alter full config hash")
	}
	configuration.Model = "qwen-plus"
	configuration.MaxOutputTokens++
	changed, err = interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("output budget change did not alter full config hash")
	}
}

func TestIELTSSpeakingShadowRuntimeConfigurationIsDeterministic(
	t *testing.T,
) {
	t.Parallel()
	configuration := EvaluationConfiguration{
		Provider:        "qianwen",
		Model:           "qwen-plus",
		MaxOutputTokens: 2048,
		LeaseDuration:   30 * time.Second,
		MaxAttempts:     3,
	}
	first, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Valid() {
		t.Fatalf("runtime configuration is unstable: %#v %#v", first, second)
	}
	configuration.Model = "qwen-max"
	changed, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("model change did not alter full config hash")
	}
}

func TestGeneralSceneRuntimeConfigurationIsDeterministic(t *testing.T) {
	t.Parallel()
	configuration := EvaluationConfiguration{
		Provider:        "qianwen",
		Model:           "qwen-plus",
		MaxOutputTokens: 2048,
		LeaseDuration:   30 * time.Second,
		MaxAttempts:     3,
	}
	first, err := generalSceneRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generalSceneRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Valid() {
		t.Fatalf("runtime configuration is unstable: %#v %#v", first, second)
	}
	configuration.Model = "qwen-max"
	changed, err := generalSceneRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("model change did not alter full config hash")
	}
}
