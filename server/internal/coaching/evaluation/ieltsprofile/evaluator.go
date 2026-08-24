package ieltsprofile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

const (
	StrategyRef     = "ielts-cumulative-profile/v1"
	PipelineVersion = "ielts-cumulative-profile/v1"
	PromptVersion   = "ielts-cumulative-profile/v1"
)

type Evaluator struct{ generator textgeneration.Generator }

func New(generator textgeneration.Generator) (*Evaluator, error) {
	if generator == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Evaluator{generator: generator}, nil
}

func Lineage(provider string, model string) (evaluation.ConfigLineage, error) {
	lineage := evaluation.ConfigLineage{
		SchemaVersion: evaluation.ConfigLineageSchemaVersion,
		StrategyRef:   StrategyRef, PipelineVersion: PipelineVersion,
		PromptVersion: PromptVersion,
		ResultSchema:  evaluation.IELTSCumulativeProfileSchemaVersion,
		Provider:      provider, Model: model,
	}
	if !lineage.Valid() {
		return evaluation.ConfigLineage{}, evaluation.ErrInvalidRequest
	}
	return lineage, nil
}

func (evaluator *Evaluator) EvaluateProfile(
	ctx context.Context,
	_ evaluation.Record,
	snapshot evaluation.IELTSProfileInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	if evaluator == nil || evaluator.generator == nil || ctx == nil ||
		!snapshot.Valid() || !lineage.Valid() || lineage.StrategyRef != StrategyRef {
		return nil, evaluation.ErrInvalidRequest
	}
	questions, turns := profileDelta(snapshot)
	payload, err := json.Marshal(struct {
		Stage           evaluation.IELTSProfileStage         `json:"stage"`
		PreviousProfile *evaluation.IELTSCumulativeProfile   `json:"previous_profile"`
		Questions       []evaluation.SessionEvidenceQuestion `json:"questions"`
		Turns           []evaluation.SessionEvidenceTurn     `json:"turns"`
	}{
		Stage: snapshot.Stage, PreviousProfile: snapshot.PreviousProfile,
		Questions: questions, Turns: turns,
	})
	if err != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	generated, err := evaluator.generator.Generate(ctx, textgeneration.Request{
		SystemPrompt: profileSystemPrompt, UserPrompt: string(payload),
	})
	if err != nil {
		return nil, err
	}
	var provider providerProfile
	if evaluation.DecodeStrict([]byte(generated.Content), &provider) != nil {
		return nil, errors.New("ielts profile: provider response invalid")
	}
	profile := evaluation.IELTSCumulativeProfile{
		SchemaVersion: evaluation.IELTSCumulativeProfileSchemaVersion,
		SessionID:     snapshot.SessionID, CompletedParts: provider.CompletedParts,
		Dimensions: provider.Dimensions,
		Provider:   generated.Provider, Model: generated.Model,
	}
	if !profile.Valid() || !profileMatchesSnapshot(profile, snapshot) {
		return nil, errors.New("ielts profile: normalized response invalid")
	}
	encoded, _, err := evaluation.EncodeStrict(profile)
	return encoded, err
}

type providerProfile struct {
	CompletedParts []int                              `json:"completed_parts"`
	Dimensions     []evaluation.IELTSProfileDimension `json:"dimensions"`
}

func profileDelta(snapshot evaluation.IELTSProfileInputSnapshot) (
	[]evaluation.SessionEvidenceQuestion,
	[]evaluation.SessionEvidenceTurn,
) {
	start := 0
	if snapshot.Stage == evaluation.IELTSProfileStagePart2 &&
		snapshot.PreviousProfile != nil {
		start = snapshot.Part1Boundary
	}
	turns := append([]evaluation.SessionEvidenceTurn(nil), snapshot.Turns[start:]...)
	questionIDs := make(map[string]struct{}, len(turns))
	for _, turn := range turns {
		questionIDs[turn.QuestionID] = struct{}{}
	}
	questions := make([]evaluation.SessionEvidenceQuestion, 0, len(questionIDs))
	for _, question := range snapshot.Questions {
		if _, exists := questionIDs[question.ID]; exists {
			questions = append(questions, question)
		}
	}
	return questions, turns
}

func profileMatchesSnapshot(
	profile evaluation.IELTSCumulativeProfile,
	snapshot evaluation.IELTSProfileInputSnapshot,
) bool {
	wantParts := 1
	if snapshot.Stage == evaluation.IELTSProfileStagePart2 {
		wantParts = 2
	}
	if len(profile.CompletedParts) != wantParts {
		return false
	}
	turns := make(map[string]evaluation.SessionEvidenceTurn, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		turns[turn.ID] = turn
	}
	for _, dimension := range profile.Dimensions {
		for _, observation := range dimension.Observations {
			for _, evidence := range observation.Evidence {
				turn, exists := turns[evidence.TurnID]
				if !exists || strings.Count(turn.Transcript, evidence.Quote) < evidence.Occurrence {
					return false
				}
				part := 1
				for index, candidate := range snapshot.Turns {
					if candidate.ID == evidence.TurnID && index >= snapshot.Part1Boundary {
						part = 2
					}
				}
				if evidence.Part != part {
					return false
				}
			}
		}
	}
	return true
}

const profileSystemPrompt = `You maintain an internal provisional cumulative IELTS Speaking profile. Return one JSON object only with exactly completed_parts and dimensions. completed_parts is [1] for PART_1 and [1,2] for PART_2. dimensions must be ordered FLUENCY_COHERENCE, LEXICAL_RESOURCE, GRAMMATICAL_RANGE_ACCURACY, PRONUNCIATION. Each dimension contains key, provisional_band_low, provisional_band_high, coverage, confidence, observations. Bands use 0.5 increments from 0 to 9 and are provisional ranges, never official Part scores. Each observation contains kind (STRENGTH or IMPROVEMENT), reason_code and one or two evidence items. Evidence contains turn_id, an exact quote, its 1-based occurrence and part. Use at most three observations per dimension. When previous_profile is present, preserve valid earlier evidence and update conclusions using the new Part. Do not mechanically average Parts. Base pronunciation only on acoustic checkpoints; when coverage is insufficient, use a broad low-confidence range and no invented evidence.`

var _ interface {
	EvaluateProfile(context.Context, evaluation.Record, evaluation.IELTSProfileInputSnapshot, evaluation.ConfigLineage) (json.RawMessage, error)
} = (*Evaluator)(nil)
