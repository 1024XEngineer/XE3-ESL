package sessionevaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

func TestInvalidProviderReportRemainsRetryableAtTheJobBoundary(t *testing.T) {
	var failure interface {
		StableCategory() string
		Retryable() bool
		EvaluationNormalizeReason() string
	}
	if !errors.As(ErrProviderResponse, &failure) ||
		failure.StableCategory() != "PROVIDER_RESPONSE_INVALID" ||
		failure.EvaluationNormalizeReason() != "normalized_report_invalid" ||
		!failure.Retryable() {
		t.Fatalf("provider response failure = %#v", failure)
	}
}

func TestInsufficientProviderReportClearsScoresAndPriorityActions(t *testing.T) {
	score := 7.0
	snapshot := sessionSnapshotFixture()
	generated := mustProviderResult(t, providerReport{
		ScoreabilityStatus: report.ReportScoreabilityInsufficient,
		Summary:            "The available evidence is insufficient.",
		Dimensions: []providerDimension{
			providerDimensionFixture("FLUENCY_COHERENCE", &score),
		},
		PriorityActions: []providerPriorityAction{{
			DimensionKey: "UNKNOWN", ImprovementIndex: 99,
		}},
	})

	formal, err := normalizeProviderReport(
		generated, snapshot, evaluation.SceneIELTSSpeaking,
		[]string{"FLUENCY_COHERENCE"}, report.ReportScaleIELTSBand,
		"qianwen", "qwen-plus", fullRawProviderEvidencePolicy(snapshot),
	)
	if err != nil {
		t.Fatalf("normalizeProviderReport() error = %v", err)
	}
	for _, dimension := range formal.Dimensions {
		if dimension.Score != nil {
			t.Fatalf("INSUFFICIENT dimension retained score: %#v", dimension)
		}
	}
	if len(formal.PriorityActions) != 0 {
		t.Fatalf("INSUFFICIENT priority actions = %#v", formal.PriorityActions)
	}
}

func TestInsufficientProviderReportDoesNotFabricateInvalidEvidence(t *testing.T) {
	score := 7.0
	snapshot := sessionSnapshotFixture()
	dimension := providerDimensionFixture("FLUENCY_COHERENCE", &score)
	dimension.Strengths = []providerFinding{{
		Message: "Unsupported strength",
		Evidence: []providerEvidence{{
			TurnID: snapshot.Turns[0].ID,
			Quote:  "provider-invented quote", Occurrence: 1,
		}},
	}}
	_, err := normalizeProviderReport(
		mustProviderResult(t, providerReport{
			ScoreabilityStatus: report.ReportScoreabilityInsufficient,
			Summary:            "The available evidence is insufficient.", Dimensions: []providerDimension{dimension},
			PriorityActions: []providerPriorityAction{},
		}),
		snapshot, evaluation.SceneIELTSSpeaking,
		[]string{"FLUENCY_COHERENCE"}, report.ReportScaleIELTSBand,
		"qianwen", "qwen-plus", fullRawProviderEvidencePolicy(snapshot),
	)
	if !errors.Is(err, ErrProviderResponse) ||
		normalizeReasonFromError(err) != normalizeReasonEvidenceInvalid {
		t.Fatalf("invalid INSUFFICIENT evidence error = %v", err)
	}
}

func TestProvisionalProviderReportRemainsFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		report     providerReport
		wantReason normalizeReason
	}{
		{
			name: "invalid score",
			report: func() providerReport {
				score := 9.5
				return providerReport{
					ScoreabilityStatus: report.ReportScoreabilityProvisional,
					Summary:            "Provisional report.",
					Dimensions: []providerDimension{
						providerDimensionFixture("FLUENCY_COHERENCE", &score),
					},
					PriorityActions: []providerPriorityAction{},
				}
			}(),
			wantReason: normalizeReasonNormalizedReportInvalid,
		},
		{
			name: "invalid priority action",
			report: func() providerReport {
				score := 7.0
				return providerReport{
					ScoreabilityStatus: report.ReportScoreabilityProvisional,
					Summary:            "Provisional report.",
					Dimensions: []providerDimension{
						providerDimensionFixture("FLUENCY_COHERENCE", &score),
					},
					PriorityActions: []providerPriorityAction{{
						DimensionKey: "FLUENCY_COHERENCE", ImprovementIndex: 1,
					}},
				}
			}(),
			wantReason: normalizeReasonPriorityActionInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := sessionSnapshotFixture()
			_, err := normalizeProviderReport(
				mustProviderResult(t, test.report),
				snapshot, evaluation.SceneIELTSSpeaking,
				[]string{"FLUENCY_COHERENCE"}, report.ReportScaleIELTSBand,
				"qianwen", "qwen-plus", fullRawProviderEvidencePolicy(snapshot),
			)
			if !errors.Is(err, ErrProviderResponse) ||
				normalizeReasonFromError(err) != test.wantReason {
				t.Fatalf("normalizeProviderReport() error = %v, reason = %q", err, normalizeReasonFromError(err))
			}
		})
	}
}

func TestIELTSRepairReceivesBoundedNormalizeReason(t *testing.T) {
	generator := &repairReportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, sessionSnapshotFixture(), lineages.IELTS,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	if len(generator.requests) != 2 {
		t.Fatalf("generation requests = %d", len(generator.requests))
	}
	var repair struct {
		Violation   normalizeReason `json:"violation"`
		Instruction string          `json:"instruction"`
	}
	if err := json.Unmarshal([]byte(generator.requests[1].UserPrompt), &repair); err != nil {
		t.Fatal(err)
	}
	if repair.Violation != normalizeReasonPriorityActionInvalid ||
		!repair.Violation.valid() ||
		!strings.Contains(repair.Instruction, "For INSUFFICIENT, every score must be null") ||
		!strings.Contains(repair.Instruction, "existing improvement in the same dimension") {
		t.Fatalf("repair contract = %#v", repair)
	}
}

func TestIELTSEvaluatorUsesAssessedAcousticsForPronunciation(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshotFixture()
	snapshot.AcousticCapability = evaluation.AcousticCapabilityEnabled
	pronunciation := 84.0
	snapshot.Turns[0].AudioAssetID = "audio-1"
	snapshot.Turns[0].Acoustic = &evaluation.AcousticCheckpoint{
		Status:          evaluation.AcousticAssessed,
		Pronunciation:   &pronunciation,
		Provider:        "xfyun-ise",
		ProviderSession: "ise-session-1",
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	)
	if err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var formal report.FormalReport
	if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
		t.Fatal(err)
	}
	if len(formal.Dimensions) != 4 ||
		formal.Dimensions[3].Key != "PRONUNCIATION" ||
		formal.Dimensions[3].Score == nil ||
		*formal.Dimensions[3].Score != 7 {
		t.Fatalf("pronunciation dimension = %#v", formal.Dimensions)
	}
	if len(formal.Questions) != 1 || formal.Questions[0].Text != snapshot.Questions[0].Text ||
		formal.Questions[0].Answer == nil ||
		formal.Questions[0].Answer.TurnID != snapshot.Turns[0].ID ||
		formal.Questions[0].Answer.Transcript != snapshot.Turns[0].Transcript {
		t.Fatalf("question evidence projection = %#v", formal.Questions)
	}
	if !strings.Contains(generator.last.UserPrompt, `"acoustic":{"status":"ASSESSED"`) ||
		!strings.Contains(generator.last.SystemPrompt, "never on transcript spelling") {
		t.Fatalf("IELTS request omitted acoustic evidence: %#v", generator.last)
	}
}

func TestIELTSEvaluatorMarksPronunciationNotAssessedWhenCapabilityDisabled(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, sessionSnapshotFixture(),
		lineages.IELTS,
	)
	if err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var formal report.FormalReport
	if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
		t.Fatal(err)
	}
	pronunciation := formal.Dimensions[len(formal.Dimensions)-1]
	if pronunciation.Key != "PRONUNCIATION" || pronunciation.Score != nil ||
		len(pronunciation.ReasonCodes) != 1 ||
		pronunciation.ReasonCodes[0] != "ACOUSTIC_ASSESSMENT_NOT_CONFIGURED" {
		t.Fatalf("pronunciation dimension = %#v", pronunciation)
	}
}

func TestIELTSEvaluatorUsesCumulativeProfileAndOnlyPart3RawEvidence(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := resolvedFullMockSnapshot()

	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var payload resolvedIELTSProviderInputV4
	if err := evaluation.DecodeStrict(
		json.RawMessage(generator.last.UserPrompt), &payload,
	); err != nil {
		t.Fatalf("decode resolved v4 payload: %v: %s", err, generator.last.UserPrompt)
	}
	if lineages.IELTS.PromptVersion != ieltsPromptVersionV4 ||
		payload.SchemaVersion != ieltsInputSchemaVersionV4 ||
		payload.EvidenceMode != ieltsInputCumulativeParts12PlusPart3 ||
		len(payload.CumulativeProfile.CompletedParts) != 2 ||
		len(payload.Questions) != 1 || len(payload.Turns) != 1 ||
		payload.Turns[0].Transcript != "PART_THREE_RAW_TRANSCRIPT" ||
		strings.Contains(generator.last.UserPrompt, "PART_ONE_HIDDEN_RAW") ||
		strings.Contains(generator.last.UserPrompt, "PART_TWO_RAW_TRANSCRIPT") {
		t.Fatalf("resolved v4 payload = %#v: %s", payload, generator.last.UserPrompt)
	}
}

func TestIELTSEvaluatorV4ResolvedRejectsPart12RawEvidenceOutsideProfile(t *testing.T) {
	snapshot := resolvedFullMockSnapshot()
	for _, test := range []struct {
		name     string
		evidence providerEvidence
	}{
		{
			name: "Part 1 raw outside profile",
			evidence: providerEvidence{
				TurnID: snapshot.Turns[0].ID, Quote: "PART_ONE_HIDDEN_RAW", Occurrence: 1,
			},
		},
		{
			name: "Part 2 raw outside profile",
			evidence: providerEvidence{
				TurnID: snapshot.Turns[1].ID, Quote: "PART_TWO_RAW_TRANSCRIPT", Occurrence: 1,
			},
		},
		{
			name: "profile quote with different occurrence",
			evidence: providerEvidence{
				TurnID: snapshot.Turns[0].ID, Quote: "PROFILE_ALLOWED_QUOTE", Occurrence: 2,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			generator := &reportGeneratorFake{evidence: &test.evidence}
			evaluators, err := New(generator)
			if err != nil {
				t.Fatal(err)
			}
			lineages, err := Lineages("qianwen", "qwen-plus")
			if err != nil {
				t.Fatal(err)
			}

			_, err = evaluators.EvaluateIELTS(
				context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
			)
			if !errors.Is(err, ErrProviderResponse) || generator.calls != 2 {
				t.Fatalf(
					"EvaluateIELTS() error = %v, generator calls = %d",
					err, generator.calls,
				)
			}
		})
	}
}

func TestIELTSEvaluatorV4ResolvedAcceptsOnlyProfileOrPart3Evidence(t *testing.T) {
	snapshot := resolvedFullMockSnapshot()
	tests := []struct {
		name      string
		evidence  providerEvidence
		turnIndex int
	}{
		{
			name: "cumulative profile evidence",
			evidence: providerEvidence{
				TurnID: snapshot.Turns[0].ID, Quote: "PROFILE_ALLOWED_QUOTE", Occurrence: 1,
			}, turnIndex: 0,
		},
		{
			name: "Part 3 raw evidence",
			evidence: providerEvidence{
				TurnID: snapshot.Turns[2].ID, Quote: "THREE_RAW", Occurrence: 1,
			}, turnIndex: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := &reportGeneratorFake{evidence: &test.evidence}
			evaluators, err := New(generator)
			if err != nil {
				t.Fatal(err)
			}
			lineages, err := Lineages("qianwen", "qwen-plus")
			if err != nil {
				t.Fatal(err)
			}

			encoded, err := evaluators.EvaluateIELTS(
				context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
			)
			if err != nil {
				t.Fatalf("EvaluateIELTS() error = %v", err)
			}
			var formal report.FormalReport
			if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
				t.Fatal(err)
			}
			got := formal.Dimensions[0].Strengths[0].Evidence[0]
			wantStart := strings.Index(
				snapshot.Turns[test.turnIndex].Transcript, test.evidence.Quote,
			)
			if got.TurnID != test.evidence.TurnID ||
				got.OriginalExcerpt != test.evidence.Quote || got.StartUTF8Byte != wantStart {
				t.Fatalf("normalized evidence = %#v, want start %d", got, wantStart)
			}
		})
	}
}

func TestIELTSEvaluatorV4FallbackKeepsFullRawEvidence(t *testing.T) {
	snapshot := sessionSnapshotFixture()
	generator := &reportGeneratorFake{evidence: &providerEvidence{
		TurnID: snapshot.Turns[0].ID, Quote: "small engineering", Occurrence: 1,
	}}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
}

func TestIELTSEvaluatorUsesTaggedFullRawFallbackPayload(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshotFixture()
	snapshot.Questions = append(snapshot.Questions, evaluation.SessionEvidenceQuestion{
		ID: "40000000-0000-4000-8000-000000000002", Position: 2,
		Text: "Unused question", SpeakerParticipantID: "assistant-1",
		AddresseeParticipantIDs: []string{"user-1"},
	})
	snapshot.Turns = append(snapshot.Turns, evaluation.SessionEvidenceTurn{
		ID: "30000000-0000-4000-8000-000000000002", Position: 2,
		QuestionID:              "40000000-0000-4000-8000-000000000002",
		RespondentParticipantID: "user-1", Transcript: "INEFFECTIVE_RAW_TRANSCRIPT",
		Effective: false, ConfirmedAt: snapshot.CompletedAt,
	})

	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var payload fallbackIELTSProviderInputV4
	if err := evaluation.DecodeStrict(
		json.RawMessage(generator.last.UserPrompt), &payload,
	); err != nil {
		t.Fatalf("decode fallback v4 payload: %v: %s", err, generator.last.UserPrompt)
	}
	if payload.SchemaVersion != ieltsInputSchemaVersionV4 ||
		payload.EvidenceMode != ieltsInputFullRawFallback ||
		len(payload.Questions) != 2 || len(payload.Turns) != 1 ||
		payload.Turns[0].Transcript != snapshot.Turns[0].Transcript ||
		strings.Contains(generator.last.UserPrompt, "INEFFECTIVE_RAW_TRANSCRIPT") ||
		strings.Contains(generator.last.UserPrompt, `"cumulative_profile"`) ||
		strings.Contains(generator.last.UserPrompt, `"part_3_effective_turns"`) {
		t.Fatalf("fallback v4 payload = %#v: %s", payload, generator.last.UserPrompt)
	}
}

func TestIELTSEvaluatorPreservesHistoricalV3FallbackContract(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	lineage := lineages.IELTS
	lineage.PromptVersion = ieltsPromptVersionV3

	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, sessionSnapshotFixture(), lineage,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var payload providerInput
	if err := evaluation.DecodeStrict(
		json.RawMessage(generator.last.UserPrompt), &payload,
	); err != nil {
		t.Fatalf("decode historical v3 payload: %v: %s", err, generator.last.UserPrompt)
	}
	if generator.last.SystemPrompt != ieltsSystemPromptV3 ||
		strings.Contains(generator.last.UserPrompt, `"schema_version"`) ||
		strings.Contains(generator.last.UserPrompt, `"evidence_mode"`) ||
		len(payload.Questions) != 1 || len(payload.Turns) != 1 {
		t.Fatalf("historical v3 contract changed: %#v: %s", payload, generator.last.UserPrompt)
	}
}

func TestIELTSEvaluatorPreservesHistoricalV3ResolvedContract(t *testing.T) {
	snapshot := resolvedFullMockSnapshot()
	generator := &reportGeneratorFake{evidence: &providerEvidence{
		TurnID: snapshot.Turns[0].ID, Quote: "PART_ONE_HIDDEN_RAW", Occurrence: 1,
	}}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	lineage := lineages.IELTS
	lineage.PromptVersion = ieltsPromptVersionV3

	if _, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineage,
	); err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var payload incrementalIELTSProviderInput
	if err := evaluation.DecodeStrict(
		json.RawMessage(generator.last.UserPrompt), &payload,
	); err != nil {
		t.Fatalf("decode historical resolved v3 payload: %v: %s", err, generator.last.UserPrompt)
	}
	if generator.last.SystemPrompt != ieltsSystemPromptV3 ||
		len(payload.CumulativeProfile.CompletedParts) != 2 ||
		len(payload.Questions) != 1 || len(payload.Turns) != 1 {
		t.Fatalf("historical resolved v3 contract changed: %#v", payload)
	}
}

func cumulativeProfileDimensions() []evaluation.IELTSProfileDimension {
	result := make([]evaluation.IELTSProfileDimension, 0, 4)
	for _, key := range []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	} {
		result = append(result, evaluation.IELTSProfileDimension{
			Key: key, ProvisionalBandLow: 6, ProvisionalBandHigh: 7,
			Coverage: 0.6, Confidence: 0.6,
			Observations: []evaluation.IELTSProfileObservation{},
		})
	}
	return result
}

func TestIELTSEvaluatorDoesNotScorePronunciationBelowMinimumCoverage(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshotFixture()
	snapshot.AcousticCapability = evaluation.AcousticCapabilityEnabled
	pronunciationScore := 86.0
	snapshot.Turns[0].AudioAssetID = "audio-1"
	snapshot.Turns[0].Acoustic = &evaluation.AcousticCheckpoint{
		Status:          evaluation.AcousticAssessed,
		Pronunciation:   &pronunciationScore,
		Provider:        "xfyun-ise",
		ProviderSession: "ise-session-1",
	}
	snapshot.Questions = append(snapshot.Questions, evaluation.SessionEvidenceQuestion{
		ID: "40000000-0000-4000-8000-000000000002", Position: 2, Text: "What would you improve?",
		SpeakerParticipantID:    "assistant-1",
		AddresseeParticipantIDs: []string{"user-1"},
	})
	snapshot.Turns = append(snapshot.Turns, evaluation.SessionEvidenceTurn{
		ID: "30000000-0000-4000-8000-000000000002", Position: 2,
		QuestionID:              "40000000-0000-4000-8000-000000000002",
		RespondentParticipantID: "user-1",
		Transcript:              "I would explain the impact more clearly.",
		Effective:               true,
		ConfirmedAt:             snapshot.CompletedAt,
		AudioAssetID:            "audio-2",
		Acoustic: &evaluation.AcousticCheckpoint{
			Status: evaluation.AcousticNotAssessed,
			Reason: "ACOUSTIC_ASSESSMENT_FAILED",
		},
	})

	encoded, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	)
	if err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var formal report.FormalReport
	if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
		t.Fatal(err)
	}
	pronunciation := formal.Dimensions[len(formal.Dimensions)-1]
	if len(formal.Dimensions) != 4 || pronunciation.Key != "PRONUNCIATION" ||
		pronunciation.Score != nil || len(pronunciation.ReasonCodes) != 1 ||
		pronunciation.ReasonCodes[0] != "ACOUSTIC_ASSESSMENT_FAILED" {
		t.Fatalf("pronunciation dimension = %#v", pronunciation)
	}
	if strings.Contains(generator.last.UserPrompt, `"PRONUNCIATION"`) {
		t.Fatalf("provider was asked to infer pronunciation: %s", generator.last.UserPrompt)
	}
}

func TestIELTSEvaluatorScoresPronunciationWithSufficientPartialCoverage(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshotFixture()
	snapshot.AcousticCapability = evaluation.AcousticCapabilityEnabled
	pronunciationScore := 86.0
	for index := 0; index < 3; index++ {
		if index > 0 {
			questionID := fmt.Sprintf(
				"40000000-0000-4000-8000-%012d", index+1,
			)
			turnID := fmt.Sprintf(
				"30000000-0000-4000-8000-%012d", index+1,
			)
			snapshot.Questions = append(
				snapshot.Questions,
				evaluation.SessionEvidenceQuestion{
					ID: questionID, Position: index + 1,
					Text: "Tell me more.", SpeakerParticipantID: "assistant-1",
					AddresseeParticipantIDs: []string{"user-1"},
				},
			)
			snapshot.Turns = append(snapshot.Turns, evaluation.SessionEvidenceTurn{
				ID: turnID, Position: index + 1, QuestionID: questionID,
				RespondentParticipantID: "user-1",
				Transcript:              "I can explain this with another example.",
				Effective:               true, ConfirmedAt: snapshot.CompletedAt,
			})
		}
		turn := &snapshot.Turns[index]
		turn.AudioAssetID = fmt.Sprintf("audio-%d", index+1)
		if index < 2 {
			turn.Acoustic = &evaluation.AcousticCheckpoint{
				Status: evaluation.AcousticAssessed, Pronunciation: &pronunciationScore,
				Provider:        "xfyun-ise",
				ProviderSession: fmt.Sprintf("ise-session-%d", index+1),
			}
		} else {
			turn.Acoustic = &evaluation.AcousticCheckpoint{
				Status: evaluation.AcousticNotAssessed,
				Reason: "ACOUSTIC_ASSESSMENT_FAILED",
			}
		}
	}

	encoded, err := evaluators.EvaluateIELTS(
		context.Background(), evaluation.Record{}, snapshot, lineages.IELTS,
	)
	if err != nil {
		t.Fatalf("EvaluateIELTS() error = %v", err)
	}
	var formal report.FormalReport
	if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
		t.Fatal(err)
	}
	pronunciation := formal.Dimensions[len(formal.Dimensions)-1]
	if pronunciation.Key != "PRONUNCIATION" || pronunciation.Score == nil ||
		*pronunciation.Score != 7 || pronunciation.Coverage != 2.0/3.0 ||
		!slices.Contains(pronunciation.ReasonCodes, "PARTIAL_ACOUSTIC_COVERAGE") {
		t.Fatalf("pronunciation dimension = %#v", pronunciation)
	}
}

func TestGeneralEvaluatorRejectsPolicyCategoryMismatch(t *testing.T) {
	evaluators, err := New(&reportGeneratorFake{})
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshotFixture()
	snapshot.EvaluationPolicyRef = evaluation.DailyEvaluationPolicyRef
	snapshot.SceneCategory = "WORKPLACE_GENERAL"
	if _, err := evaluators.EvaluateGeneral(
		context.Background(), evaluation.Record{}, snapshot, lineages.General,
	); err != evaluation.ErrInvalidRequest {
		t.Fatalf("EvaluateGeneral() error = %v", err)
	}
}

func TestSessionEvaluationPromptsRequireChineseFeedbackAndOriginalEvidence(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		version string
	}{
		{name: "interview", prompt: interviewSystemPromptV2, version: interviewPromptVersionV2},
		{name: "IELTS", prompt: ieltsSystemPromptV4, version: ieltsPromptVersionV4},
		{name: "general", prompt: generalSystemPromptV2, version: generalPromptVersionV2},
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{
		"interview": lineages.Interview.PromptVersion,
		"IELTS":     lineages.IELTS.PromptVersion,
		"general":   lineages.General.PromptVersion,
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, requirement := range []string{
				"summary and every finding message in Simplified Chinese",
				"suggestions for strengths and improvements in Simplified Chinese",
				"directly reusable English expression in suggestion",
				"question, answer, and evidence quote in its original language",
			} {
				if !strings.Contains(test.prompt, requirement) {
					t.Fatalf("prompt omitted language requirement %q: %s", requirement, test.prompt)
				}
			}
			if versions[test.name] != test.version {
				t.Fatalf("PromptVersion = %q, want %q", versions[test.name], test.version)
			}
		})
	}
}

func TestSessionEvaluationPromptsFollowRecordedVersion(t *testing.T) {
	tests := []struct {
		name      string
		v1Version string
		v1Prompt  string
		v2Version string
		v2Prompt  string
	}{
		{
			name: "interview", v1Version: interviewPromptVersionV1,
			v1Prompt: interviewSystemPromptV1, v2Version: interviewPromptVersionV2,
			v2Prompt: interviewSystemPromptV2,
		},
		{
			name: "IELTS", v1Version: ieltsPromptVersionV1,
			v1Prompt: ieltsSystemPromptV1, v2Version: ieltsPromptVersionV2,
			v2Prompt: ieltsSystemPromptV2,
		},
		{
			name: "general", v1Version: generalPromptVersionV1,
			v1Prompt: generalSystemPromptV1, v2Version: generalPromptVersionV2,
			v2Prompt: generalSystemPromptV2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v1, err := selectReportPrompt(
				test.v1Version, test.v1Version, test.v1Prompt,
				test.v2Version, test.v2Prompt,
			)
			if err != nil || v1.system != test.v1Prompt ||
				v1.insufficientSummary != "There is not enough confirmed practice evidence to produce a reliable evaluation." {
				t.Fatalf("v1 prompt = %#v, error = %v", v1, err)
			}
			v2, err := selectReportPrompt(
				test.v2Version, test.v1Version, test.v1Prompt,
				test.v2Version, test.v2Prompt,
			)
			if err != nil || v2.system != test.v2Prompt ||
				v2.insufficientSummary != "本次练习的有效证据不足，暂时无法形成可靠的评估结论。" {
				t.Fatalf("v2 prompt = %#v, error = %v", v2, err)
			}
			if _, err := selectReportPrompt(
				"unknown/v1", test.v1Version, test.v1Prompt,
				test.v2Version, test.v2Prompt,
			); err != evaluation.ErrInvalidRequest {
				t.Fatalf("unknown version error = %v", err)
			}
		})
	}
}

func TestInterviewEvaluatorUsesRecordedV1Prompt(t *testing.T) {
	generator := &reportGeneratorFake{}
	evaluators, err := New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	lineages.Interview.PromptVersion = interviewPromptVersionV1
	snapshot := sessionSnapshotFixture()
	snapshot.EvaluationPolicyRef = evaluation.InterviewEvaluationPolicyRef
	snapshot.PracticeExperience = "INTERVIEW"
	snapshot.SceneCategory = "INTERVIEW_RECRUITER"
	snapshot.PracticeMode = "FULL_SIMULATION"
	if _, err := evaluators.EvaluateInterview(
		context.Background(), evaluation.Record{}, snapshot, lineages.Interview,
	); err != nil {
		t.Fatalf("EvaluateInterview() error = %v", err)
	}
	if generator.last.SystemPrompt != interviewSystemPromptV1 {
		t.Fatal("v1 lineage did not use the v1 interview prompt")
	}
}

func TestInsufficientReportFollowsRecordedVersionAndPreservesOriginalText(t *testing.T) {
	snapshot := sessionSnapshotFixture()
	snapshot.Turns[0].Effective = false
	evaluators, err := New(&reportGeneratorFake{})
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name: "v1", version: ieltsPromptVersionV1,
			want: "There is not enough confirmed practice evidence to produce a reliable evaluation.",
		},
		{
			name: "v2", version: ieltsPromptVersionV2,
			want: "本次练习的有效证据不足，暂时无法形成可靠的评估结论。",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lineage := lineages.IELTS
			lineage.PromptVersion = test.version
			encoded, err := evaluators.EvaluateIELTS(
				context.Background(), evaluation.Record{}, snapshot, lineage,
			)
			if err != nil {
				t.Fatalf("EvaluateIELTS() error = %v", err)
			}
			var formal report.FormalReport
			if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
				t.Fatal(err)
			}
			if formal.Summary != test.want {
				t.Fatalf("Summary = %q", formal.Summary)
			}
			if len(formal.Questions) != 1 || formal.Questions[0].Text != snapshot.Questions[0].Text {
				t.Fatalf("question projection = %#v", formal.Questions)
			}
			if formal.Questions[0].Answer != nil {
				t.Fatalf("ineffective answer must not be projected: %#v", formal.Questions[0].Answer)
			}
		})
	}
}

type reportGeneratorFake struct {
	last     textgeneration.Request
	evidence *providerEvidence
	calls    int
}

type repairReportGeneratorFake struct {
	requests []textgeneration.Request
}

func (generator *repairReportGeneratorFake) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.requests = append(generator.requests, request)
	var dimensionKeys []string
	if len(generator.requests) == 1 {
		var input struct {
			DimensionKeys []string `json:"dimension_keys"`
		}
		if err := json.Unmarshal([]byte(request.UserPrompt), &input); err != nil {
			return textgeneration.Result{}, err
		}
		dimensionKeys = input.DimensionKeys
	} else {
		var repair struct {
			Input struct {
				DimensionKeys []string `json:"dimension_keys"`
			} `json:"input"`
		}
		if err := json.Unmarshal([]byte(request.UserPrompt), &repair); err != nil {
			return textgeneration.Result{}, err
		}
		dimensionKeys = repair.Input.DimensionKeys
	}
	score := 7.0
	dimensions := make([]providerDimension, len(dimensionKeys))
	for index, key := range dimensionKeys {
		dimensions[index] = providerDimensionFixture(key, &score)
	}
	actions := []providerPriorityAction{}
	if len(generator.requests) == 1 {
		actions = append(actions, providerPriorityAction{
			DimensionKey: dimensionKeys[0], ImprovementIndex: 1,
		})
	}
	content, err := json.Marshal(providerReport{
		ScoreabilityStatus: report.ReportScoreabilityProvisional,
		Summary:            "Provisional report.",
		Dimensions:         dimensions,
		PriorityActions:    actions,
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: fmt.Sprintf("request-%d", len(generator.requests)),
		Content:   string(content), Provider: "qianwen", Model: "qwen-plus",
	}, nil
}

func providerDimensionFixture(key string, score *float64) providerDimension {
	return providerDimension{
		Key: key, Score: score, Coverage: 1, Confidence: 0.8,
		ReasonCodes: []string{}, Strengths: []providerFinding{},
		Improvements: []providerFinding{}, Examples: []providerFinding{},
	}
}

func mustProviderResult(t *testing.T, value providerReport) textgeneration.Result {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return textgeneration.Result{
		RequestID: "request-1", Content: string(content),
		Provider: "qianwen", Model: "qwen-plus",
	}
}

func (generator *reportGeneratorFake) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.calls++
	generator.last = request
	var input struct {
		DimensionKeys []string `json:"dimension_keys"`
	}
	if err := json.Unmarshal([]byte(request.UserPrompt), &input); err != nil {
		return textgeneration.Result{}, err
	}
	dimensions := make([]map[string]any, len(input.DimensionKeys))
	for index, key := range input.DimensionKeys {
		strengths := []any{}
		if index == 0 && generator.evidence != nil {
			strengths = []any{map[string]any{
				"message": "有明确证据。", "suggestion": "继续保持。",
				"evidence": []providerEvidence{*generator.evidence},
			}}
		}
		dimensions[index] = map[string]any{
			"key": key, "score": 7.0, "coverage": 1.0, "confidence": 0.8,
			"reason_codes": []string{}, "strengths": strengths,
			"improvements": []any{}, "recommended_examples": []any{},
		}
	}
	content, err := json.Marshal(map[string]any{
		"scoreability_status": "PROVISIONAL",
		"summary":             "Evidence-bound feedback is ready.",
		"dimensions":          dimensions,
		"priority_actions":    []any{},
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: "request-1",
		Content:   string(content),
		Provider:  "qianwen",
		Model:     "qwen-plus",
	}, nil
}

func resolvedFullMockSnapshot() evaluation.SessionInputSnapshot {
	snapshot := sessionSnapshotFixture()
	now := snapshot.CompletedAt
	snapshot.PlanSnapshot = json.RawMessage(`{"ielts_assignment":{"parts":[{"turn_blueprints":["p1"]},{"turn_blueprints":["p2"]},{"turn_blueprints":["p3"]}]}}`)
	snapshot.Questions[0].Text = "Part 1 question"
	snapshot.Turns[0].Transcript = "PROFILE_ALLOWED_QUOTE PART_ONE_HIDDEN_RAW"
	for index, transcript := range []string{"PART_TWO_RAW_TRANSCRIPT", "PART_THREE_RAW_TRANSCRIPT"} {
		position := index + 2
		questionID := fmt.Sprintf("40000000-0000-4000-8000-%012d", position)
		turnID := fmt.Sprintf("30000000-0000-4000-8000-%012d", position)
		snapshot.Questions = append(snapshot.Questions, evaluation.SessionEvidenceQuestion{
			ID: questionID, Position: position, Text: fmt.Sprintf("Part %d question", position),
			SpeakerParticipantID: "assistant-1", AddresseeParticipantIDs: []string{"user-1"},
		})
		snapshot.Turns = append(snapshot.Turns, evaluation.SessionEvidenceTurn{
			ID: turnID, Position: position, QuestionID: questionID,
			RespondentParticipantID: "user-1", Transcript: transcript,
			Effective: true, ConfirmedAt: now,
		})
	}
	dimensions := cumulativeProfileDimensions()
	dimensions[0].Observations = []evaluation.IELTSProfileObservation{{
		Kind: "STRENGTH", ReasonCode: "PROFILE_EVIDENCE",
		Evidence: []evaluation.IELTSProfileEvidence{{
			TurnID: snapshot.Turns[0].ID, Quote: "PROFILE_ALLOWED_QUOTE",
			Occurrence: 1, Part: 1,
		}},
	}}
	snapshot.CumulativeProfile = &evaluation.IELTSCumulativeProfile{
		SchemaVersion: evaluation.IELTSCumulativeProfileSchemaVersion,
		SessionID:     snapshot.SessionID, CompletedParts: []int{1, 2},
		Dimensions: dimensions, Provider: "qianwen", Model: "qwen-plus",
	}
	snapshot.ProfileResolution = evaluation.IELTSFinalProfileResolved
	return snapshot
}

func sessionSnapshotFixture() evaluation.SessionInputSnapshot {
	now := time.Now().UTC()
	return evaluation.SessionInputSnapshot{
		SchemaVersion:       evaluation.SessionInputSchemaVersion,
		SessionID:           "20000000-0000-4000-8000-000000000001",
		SessionVersion:      1,
		EvaluationPolicyRef: evaluation.IELTSSpeakingFullMockEvaluationPolicyRef,
		PracticeExperience:  "IELTS_SPEAKING",
		SceneCategory:       "IELTS_SPEAKING",
		PracticeMode:        "FULL_MOCK",
		ProfileResolution:   evaluation.IELTSFinalProfileFallback,
		CompletedAt:         now,
		AcousticCapability:  evaluation.AcousticCapabilityNotConfigured,
		PlanSnapshot:        json.RawMessage(`{}`),
		Participants:        json.RawMessage(`[]`),
		Questions: []evaluation.SessionEvidenceQuestion{{
			ID: "40000000-0000-4000-8000-000000000001", Position: 1, Text: "Tell me about your work.",
			SpeakerParticipantID:    "assistant-1",
			AddresseeParticipantIDs: []string{"user-1"},
		}},
		Turns: []evaluation.SessionEvidenceTurn{{
			ID: "30000000-0000-4000-8000-000000000001", Position: 1,
			QuestionID:              "40000000-0000-4000-8000-000000000001",
			RespondentParticipantID: "user-1",
			Transcript:              "I work with a small engineering team.", Effective: true,
			ConfirmedAt: now,
		}},
	}
}

var _ textgeneration.Generator = (*reportGeneratorFake)(nil)
var _ textgeneration.Generator = (*repairReportGeneratorFake)(nil)
