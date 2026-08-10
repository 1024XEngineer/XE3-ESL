package run

import "testing"

func TestModelAndOpaqueIdentifiersHaveDistinctRules(t *testing.T) {
	if !ValidModelID("moonshotai/kimi-k2.6") {
		t.Fatal("provider-qualified model ID should be valid")
	}
	if ValidOpaqueID("provider/completion") {
		t.Fatal("opaque provider result ID must not accept model path syntax")
	}
	if !ValidOpaqueID("completion-1") {
		t.Fatal("ordinary opaque provider result ID should be valid")
	}
}
