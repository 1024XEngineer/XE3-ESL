package scoring

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

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
	if firstIELTS.MaxAttempts != runtimeMaxAttempts {
		t.Fatalf("IELTS max attempts = %d", firstIELTS.MaxAttempts)
	}
	if firstIELTS.AcousticWaitDuration !=
		IELTSAcousticSnapshotWaitDurationV1 ||
		IELTSAcousticSnapshotWaitDurationV1 != 120*time.Second {
		t.Fatalf(
			"IELTS acoustic wait = %s",
			firstIELTS.AcousticWaitDuration,
		)
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
	if baseGeneral.FullConfigHash == modelGeneral.FullConfigHash ||
		baseGeneral.IELTSFullConfigHash == modelGeneral.IELTSFullConfigHash {
		t.Fatal("general lineage change did not alter full config hash")
	}
}

func TestGeneralSceneNonIELTSHashRemainsLegacyCompatible(t *testing.T) {
	t.Parallel()
	configuration, err := NewConfiguration("qianwen", "qwen-plus", 2048)
	if err != nil {
		t.Fatal(err)
	}
	got, err := generalRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	const wantLegacy = "4bbf71bc42ff998d510f8fee099be231fd22577408e4d457b9f054b7586f4d6a"
	for _, sceneType := range []evaluation.SceneType{
		evaluation.SceneOverseasDaily,
		evaluation.SceneOverseasWorkplace,
	} {
		effective, ok := got.ForScene(sceneType)
		if !ok {
			t.Fatalf("missing configuration for %s", sceneType)
		}
		if encoded := hex.EncodeToString(effective.FullConfigHash[:]); encoded != wantLegacy {
			t.Fatalf("%s hash = %s, want legacy %s", sceneType, encoded, wantLegacy)
		}
	}
	ielts, ok := got.ForScene(evaluation.SceneIELTSSpeaking)
	if !ok || ielts.FullConfigHash != got.IELTSFullConfigHash ||
		hex.EncodeToString(ielts.FullConfigHash[:]) == wantLegacy {
		t.Fatalf("IELTS hash was not isolated: %#v", ielts)
	}
}
