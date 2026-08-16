package sessionevaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

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

type reportGeneratorFake struct {
	last textgeneration.Request
}

func (generator *reportGeneratorFake) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.last = request
	var input struct {
		DimensionKeys []string `json:"dimension_keys"`
	}
	if err := json.Unmarshal([]byte(request.UserPrompt), &input); err != nil {
		return textgeneration.Result{}, err
	}
	dimensions := make([]map[string]any, len(input.DimensionKeys))
	for index, key := range input.DimensionKeys {
		dimensions[index] = map[string]any{
			"key": key, "score": 7.0, "coverage": 1.0, "confidence": 0.8,
			"reason_codes": []string{}, "strengths": []any{},
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
