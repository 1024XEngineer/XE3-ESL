package voice

import "testing"

func TestValidModelIDAcceptsProviderQualifiedModel(t *testing.T) {
	if !ValidModelID("qwen/qwen3.7-plus") {
		t.Fatal("provider-qualified model ID was rejected")
	}
	if ValidModelID("qwen//qwen3.7-plus") {
		t.Fatal("invalid provider-qualified model ID was accepted")
	}
}
