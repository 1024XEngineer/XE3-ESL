package ieltsprofile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

func TestPart2EvaluationSendsPreviousProfileAndOnlyPart2Delta(t *testing.T) {
	generator := &profileGeneratorFake{}
	evaluator, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := profileSnapshotFixture()

	if _, err := evaluator.EvaluateProfile(
		context.Background(), evaluation.Record{}, snapshot, lineage,
	); err != nil {
		t.Fatalf("EvaluateProfile() error = %v", err)
	}
	if strings.Contains(generator.last.UserPrompt, "PART_ONE_PRIVATE_TRANSCRIPT") ||
		!strings.Contains(generator.last.UserPrompt, "PART_TWO_NEW_TRANSCRIPT") ||
		!strings.Contains(generator.last.UserPrompt, `"previous_profile"`) {
		t.Fatalf("Part 2 incremental payload = %s", generator.last.UserPrompt)
	}
}

func TestProfileEvaluationRepairsInvalidHalfBand(t *testing.T) {
	generator := &sequencedProfileGenerator{bands: []float64{6.3, 6.5}}
	evaluator, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	result, err := evaluator.EvaluateProfile(
		context.Background(), evaluation.Record{}, profileSnapshotFixture(), lineage,
	)
	if err != nil {
		t.Fatalf("EvaluateProfile() error = %v", err)
	}
	if generator.calls != 2 || !strings.Contains(
		generator.requests[1].UserPrompt, "Every provisional band must be one of",
	) {
		t.Fatalf("repair requests = %#v", generator.requests)
	}
	var profile evaluation.IELTSCumulativeProfile
	if evaluation.DecodeStrict(result, &profile) != nil || !profile.Valid() ||
		profile.Dimensions[0].ProvisionalBandLow != 6.5 {
		t.Fatalf("repaired profile = %#v", profile)
	}
}

func TestProfileEvaluationRejectsRepeatedInvalidHalfBands(t *testing.T) {
	generator := &sequencedProfileGenerator{bands: []float64{6.3, 6.3}}
	evaluator, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := evaluator.EvaluateProfile(
		context.Background(), evaluation.Record{}, profileSnapshotFixture(), lineage,
	); err == nil || generator.calls != 2 {
		t.Fatalf("EvaluateProfile() error = %v, calls = %d", err, generator.calls)
	}
}

type profileGeneratorFake struct{ last textgeneration.Request }

func (generator *profileGeneratorFake) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.last = request
	dimensions := make([]map[string]any, 0, 4)
	for _, key := range []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	} {
		dimensions = append(dimensions, map[string]any{
			"key": key, "provisional_band_low": 6.0,
			"provisional_band_high": 7.0, "coverage": 0.7,
			"confidence": 0.6, "observations": []any{},
		})
	}
	content, err := json.Marshal(map[string]any{
		"completed_parts": []int{1, 2}, "dimensions": dimensions,
	})
	return textgeneration.Result{
		RequestID: "profile-request-1", Content: string(content),
		Provider: "qianwen", Model: "qwen-plus",
	}, err
}

func profileSnapshotFixture() evaluation.IELTSProfileInputSnapshot {
	now := time.Now().UTC()
	sessionID := "20000000-0000-4000-8000-000000000001"
	profile := &evaluation.IELTSCumulativeProfile{
		SchemaVersion: evaluation.IELTSCumulativeProfileSchemaVersion,
		SessionID:     sessionID, CompletedParts: []int{1},
		Dimensions: profileDimensions(), Provider: "qianwen", Model: "qwen-plus",
	}
	return evaluation.IELTSProfileInputSnapshot{
		SchemaVersion: evaluation.IELTSProfileInputSchemaVersion,
		SessionID:     sessionID, SessionVersion: 3,
		Stage: evaluation.IELTSProfileStagePart2, CompletedAt: now,
		Part1Boundary: 1, Part2Boundary: 2,
		AcousticCapability:   evaluation.AcousticCapabilityNotConfigured,
		DependencyResolution: evaluation.IELTSProfileDependencyResolved,
		PreviousProfile:      profile,
		Questions: []evaluation.SessionEvidenceQuestion{
			{ID: "40000000-0000-4000-8000-000000000001", Position: 1,
				Text: "Part 1", SpeakerParticipantID: "assistant",
				AddresseeParticipantIDs: []string{"learner"}},
			{ID: "40000000-0000-4000-8000-000000000002", Position: 2,
				Text: "Part 2", SpeakerParticipantID: "assistant",
				AddresseeParticipantIDs: []string{"learner"}},
		},
		Turns: []evaluation.SessionEvidenceTurn{
			{ID: "30000000-0000-4000-8000-000000000001", Position: 1,
				QuestionID:              "40000000-0000-4000-8000-000000000001",
				RespondentParticipantID: "learner", Transcript: "PART_ONE_PRIVATE_TRANSCRIPT",
				Effective: true, ConfirmedAt: now},
			{ID: "30000000-0000-4000-8000-000000000002", Position: 2,
				QuestionID:              "40000000-0000-4000-8000-000000000002",
				RespondentParticipantID: "learner", Transcript: "PART_TWO_NEW_TRANSCRIPT",
				Effective: true, ConfirmedAt: now},
		},
	}
}

func profileDimensions() []evaluation.IELTSProfileDimension {
	result := make([]evaluation.IELTSProfileDimension, 0, 4)
	for _, key := range []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	} {
		result = append(result, evaluation.IELTSProfileDimension{
			Key: key, ProvisionalBandLow: 6, ProvisionalBandHigh: 7,
			Coverage: 0.5, Confidence: 0.5,
			Observations: []evaluation.IELTSProfileObservation{},
		})
	}
	return result
}

var _ textgeneration.Generator = (*profileGeneratorFake)(nil)

type sequencedProfileGenerator struct {
	bands    []float64
	calls    int
	requests []textgeneration.Request
}

func (generator *sequencedProfileGenerator) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.requests = append(generator.requests, request)
	band := generator.bands[generator.calls]
	generator.calls++
	dimensions := make([]map[string]any, 0, 4)
	for _, key := range []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	} {
		dimensions = append(dimensions, map[string]any{
			"key": key, "provisional_band_low": band,
			"provisional_band_high": band, "coverage": 0.7,
			"confidence": 0.6, "observations": []any{},
		})
	}
	content, err := json.Marshal(map[string]any{
		"completed_parts": []int{1, 2}, "dimensions": dimensions,
	})
	return textgeneration.Result{
		RequestID: "profile-request", Content: string(content),
		Provider: "qianwen", Model: "qwen-plus",
	}, err
}

var _ textgeneration.Generator = (*sequencedProfileGenerator)(nil)
