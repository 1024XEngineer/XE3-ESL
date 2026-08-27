package evaluation

import "testing"

func TestConfigLineageAcceptsProviderQualifiedModel(t *testing.T) {
	lineage := ConfigLineage{
		SchemaVersion: ConfigLineageSchemaVersion,
		StrategyRef:   "strategy/v1", PipelineVersion: "pipeline/v1",
		PromptVersion: "prompt/v1", ResultSchema: "result/v1",
		Provider: "qiniu", Model: "qwen/qwen3.7-max",
	}
	if !lineage.Valid() {
		t.Fatal("provider-qualified model lineage was rejected")
	}
	lineage.Model = "qwen//qwen3.7-max"
	if lineage.Valid() {
		t.Fatal("invalid provider-qualified model lineage was accepted")
	}
}

func TestIELTSCumulativeProfileAcceptsProviderQualifiedModel(t *testing.T) {
	profile := IELTSCumulativeProfile{
		SchemaVersion:  IELTSCumulativeProfileSchemaVersion,
		SessionID:      "20000000-0000-4000-8000-000000000001",
		CompletedParts: []int{1}, Provider: "qiniu", Model: "qwen/qwen3.7-max",
		Dimensions: []IELTSProfileDimension{
			{Key: "FLUENCY_COHERENCE", ProvisionalBandLow: 6, ProvisionalBandHigh: 6.5, Observations: []IELTSProfileObservation{}},
			{Key: "LEXICAL_RESOURCE", ProvisionalBandLow: 6, ProvisionalBandHigh: 6.5, Observations: []IELTSProfileObservation{}},
			{Key: "GRAMMATICAL_RANGE_ACCURACY", ProvisionalBandLow: 6, ProvisionalBandHigh: 6.5, Observations: []IELTSProfileObservation{}},
			{Key: "PRONUNCIATION", ProvisionalBandLow: 6, ProvisionalBandHigh: 6.5, Observations: []IELTSProfileObservation{}},
		},
	}
	if !profile.Valid() {
		t.Fatal("provider-qualified cumulative profile model was rejected")
	}
}
