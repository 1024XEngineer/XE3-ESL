package scoring

import "testing"

func TestRuntimeConfigurationsAreDeterministic(t *testing.T) {
	t.Parallel()
	configuration, err := NewConfiguration("qianwen", "qwen-plus", 2048)
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
