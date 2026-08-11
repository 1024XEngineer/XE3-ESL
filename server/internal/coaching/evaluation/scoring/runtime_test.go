package scoring

import "testing"

func TestRuntimeConfigurationsAreDeterministic(t *testing.T) {
	t.Parallel()
	configuration, err := NewConfiguration(
		"qiniu",
		"moonshotai/kimi-k2.6",
		2048,
	)
	if err != nil {
		t.Fatal(err)
	}

	firstInterview, err := interviewRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	secondInterview, err := interviewRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if firstInterview != secondInterview || !firstInterview.Valid() {
		t.Fatalf("interview runtime configuration is unstable: %#v %#v", firstInterview, secondInterview)
	}
	if firstInterview.MaxAttempts != runtimeMaxAttempts {
		t.Fatalf("interview max attempts = %d", firstInterview.MaxAttempts)
	}

	firstIELTS, err := ieltsRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	secondIELTS, err := ieltsRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if firstIELTS != secondIELTS || !firstIELTS.Valid() {
		t.Fatalf("IELTS runtime configuration is unstable: %#v %#v", firstIELTS, secondIELTS)
	}
	if firstIELTS.MaxAttempts != 10 {
		t.Fatalf("IELTS max attempts = %d", firstIELTS.MaxAttempts)
	}

	firstGeneral, err := generalRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	secondGeneral, err := generalRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeneral != secondGeneral || !firstGeneral.Valid() {
		t.Fatalf("general runtime configuration is unstable: %#v %#v", firstGeneral, secondGeneral)
	}
	if firstGeneral.MaxAttempts != runtimeMaxAttempts {
		t.Fatalf("general max attempts = %d", firstGeneral.MaxAttempts)
	}
}

func TestIELTSAcousticWaitBudgetPreservesProviderAttempts(t *testing.T) {
	t.Parallel()
	configuration, err := NewConfiguration("qiniu", "qwen-plus", 2048)
	if err != nil {
		t.Fatal(err)
	}
	ielts, err := ieltsRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	waitAttempts := ieltsAcousticWaitAttemptLimit(ielts.MaxAttempts)
	if waitAttempts != 7 {
		t.Fatalf("IELTS acoustic wait attempts = %d", waitAttempts)
	}
	if providerAttempts := ielts.MaxAttempts - waitAttempts; providerAttempts < ieltsProviderAttemptReserve {
		t.Fatalf("IELTS provider attempts = %d", providerAttempts)
	}
}

func TestRuntimeConfigurationRejectsPathLikeModelID(t *testing.T) {
	t.Parallel()
	if _, err := NewConfiguration(
		"qiniu",
		"moonshotai//kimi-k2.6",
		2048,
	); err == nil {
		t.Fatal("path-like model ID was accepted")
	}
}

func TestRuntimeLineageChangesHash(t *testing.T) {
	t.Parallel()
	base, err := NewConfiguration("qianwen", "qwen-plus", 2048)
	if err != nil {
		t.Fatal(err)
	}
	changedModel, err := NewConfiguration("qianwen", "qwen-max", 2048)
	if err != nil {
		t.Fatal(err)
	}
	changedBudget, err := NewConfiguration("qianwen", "qwen-plus", 2049)
	if err != nil {
		t.Fatal(err)
	}

	baseInterview, _ := interviewRuntimeConfiguration(base)
	modelInterview, _ := interviewRuntimeConfiguration(changedModel)
	budgetInterview, _ := interviewRuntimeConfiguration(changedBudget)
	if baseInterview.FullConfigHash == modelInterview.FullConfigHash ||
		baseInterview.FullConfigHash == budgetInterview.FullConfigHash {
		t.Fatal("interview lineage change did not alter full config hash")
	}

	baseIELTS, _ := ieltsRuntimeConfiguration(base)
	modelIELTS, _ := ieltsRuntimeConfiguration(changedModel)
	if baseIELTS.FullConfigHash == modelIELTS.FullConfigHash {
		t.Fatal("IELTS lineage change did not alter full config hash")
	}

	baseGeneral, _ := generalRuntimeConfiguration(base)
	modelGeneral, _ := generalRuntimeConfiguration(changedModel)
	if baseGeneral.FullConfigHash == modelGeneral.FullConfigHash {
		t.Fatal("general lineage change did not alter full config hash")
	}
}
