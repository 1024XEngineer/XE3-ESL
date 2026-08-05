package scoring

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"slices"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestGeneralSceneEngineProducesGroundedResult(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneOverseasDaily,
		scene.PracticeExperienceRoleplay,
		scene.SceneCategoryRoleplayTravel,
		scene.PracticeModeFullSimulation,
		"I need to change my room because the air conditioner is not working.",
	)
	provider := &generalSceneProviderStub{}
	engine := NewGeneralSceneEngine(provider)
	result, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 ||
		provider.input.SceneType != evaluation.SceneOverseasDaily ||
		provider.input.PracticeExperience != string(scene.PracticeExperienceRoleplay) ||
		provider.input.SceneCategory != string(scene.SceneCategoryRoleplayTravel) ||
		provider.input.PracticeMode != string(scene.PracticeModeFullSimulation) ||
		result.ScoreabilityStatus != GeneralSceneScoreabilityProvisional ||
		len(result.Dimensions) != 4 || len(result.PriorityActions) != 3 ||
		result.Provider == nil {
		t.Fatalf("provider=%#v result=%#v", provider, result)
	}
	for _, dimension := range result.Dimensions {
		if dimension.Score == nil || len(dimension.EvidenceRefs) != 1 ||
			len(dimension.Improvements) != 1 ||
			dimension.Confidence != 0.6 {
			t.Fatalf("dimension = %#v", dimension)
		}
	}
}

func TestGeneralSceneEngineDoesNotCallProviderForInsufficientEvidence(
	t *testing.T,
) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneOverseasWorkplace,
		scene.PracticeExperienceRoleplay,
		scene.SceneCategoryRoleplayWorkplace,
		scene.PracticeModeFullSimulation,
		"Okay.",
	)
	provider := &generalSceneProviderStub{}
	result, err := NewGeneralSceneEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 ||
		result.ScoreabilityStatus != GeneralSceneScoreabilityInsufficient ||
		result.Provider != nil || len(result.PriorityActions) != 0 {
		t.Fatalf("provider calls=%d result=%#v", provider.calls, result)
	}
	for _, dimension := range result.Dimensions {
		if dimension.Score != nil || len(dimension.EvidenceRefs) != 0 {
			t.Fatalf("insufficient dimension = %#v", dimension)
		}
	}
}

func TestGeneralSceneEngineRejectsUngroundedProviderQuote(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		evaluation.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I usually read a book after work because it helps me relax.",
	)
	provider := &generalSceneProviderStub{mutate: func(
		payload *generalSceneProviderPayload,
	) {
		payload.Dimensions[0].Improvements[0].Evidence[0].Quote =
			"This text never appeared."
	}}
	_, err := NewGeneralSceneEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if !errors.Is(err, ErrInvalidGeneralSceneResult) {
		t.Fatalf("error = %v", err)
	}
}

type generalSceneProviderStub struct {
	input  GeneralSceneProviderInput
	calls  int
	mutate func(*generalSceneProviderPayload)
}

func (provider *generalSceneProviderStub) AnalyzeGeneralScene(
	_ context.Context,
	input GeneralSceneProviderInput,
) (GeneralSceneProviderResult, error) {
	provider.input = input
	provider.calls++
	payload := validGeneralSceneProviderPayload(input)
	if provider.mutate != nil {
		provider.mutate(&payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return GeneralSceneProviderResult{}, err
	}
	return GeneralSceneProviderResult{
		Payload:   encoded,
		Provider:  "qianwen",
		Model:     "qwen-plus",
		RequestID: "provider-request-1",
	}, nil
}

func validGeneralSceneProviderPayload(
	input GeneralSceneProviderInput,
) generalSceneProviderPayload {
	var response *GeneralSceneResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil {
			response = opportunity.Response
			break
		}
	}
	if response == nil {
		panic("general Scene provider fixture requires a response")
	}
	dimensions := make(
		[]generalSceneProviderDimension,
		0,
		len(input.AssessableDimensions),
	)
	for index, dimension := range input.AssessableDimensions {
		template, ok := generalSceneTemplate(
			dimension,
			generalSceneImprovement,
		)
		if !ok {
			panic("general Scene provider fixture requires a template")
		}
		dimensions = append(dimensions, generalSceneProviderDimension{
			DimensionID: dimension,
			Score:       60 + index*10,
			Strengths:   []generalSceneProviderFinding{},
			Improvements: []generalSceneProviderFinding{{
				TemplateID: template.ID,
				Evidence: []generalSceneProviderAnchor{{
					EvidenceRefID: response.EvidenceRefID,
					Quote:         response.Transcript,
					Occurrence:    1,
				}},
			}},
			Examples: []generalSceneProviderFinding{},
		})
	}
	return generalSceneProviderPayload{
		SchemaVersion: GeneralSceneProviderSchemaVersion,
		Dimensions:    dimensions,
	}
}

func generalSceneTestSnapshot(
	t *testing.T,
	sceneType evaluation.SceneType,
	experience scene.PracticeExperience,
	category scene.SceneCategory,
	mode scene.PracticeMode,
	transcript string,
) evidence.EvidenceSnapshot {
	t.Helper()
	source := interviewShadowTestSnapshot(
		t,
		transcript,
		interviewFollowUpNone,
	)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(source.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.PracticeExperience = string(experience)
	payload.PracticeContext.SceneCategory = string(category)
	payload.PracticeContext.PracticeMode = string(mode)
	payload.PracticeContext.PracticeOption.Mode = string(mode)
	payload.PracticeContext.EvaluationPolicyRef = "general.scene.evaluation.v1"
	if experience == scene.PracticeExperienceIELTSSpeaking {
		assignment := &evidence.IELTSAssignment{
			BankID: "ielts-bank-1",
			Season: "2026-05",
			Mode:   string(mode),
		}
		switch mode {
		case scene.PracticeModePart1:
			assignment.Parts = []evidence.IELTSAssignmentPart{{
				Part:           string(scene.PracticeModePart1),
				SourceID:       "part-1-set-1",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints),
			}}
		case scene.PracticeModePart2:
			payload.PracticeContext.TaskBlueprints = append(
				payload.PracticeContext.TaskBlueprints,
				"Discuss the topic more broadly.",
			)
			assignment.Parts = []evidence.IELTSAssignmentPart{
				{
					Part:       string(scene.PracticeModePart2),
					SourceID:   "topic-group-1",
					TopicTitle: "Fixture topic",
					CueCard:    "Discuss the fixture topic.",
					TurnBlueprints: slices.Clone(
						payload.PracticeContext.TaskBlueprints[:1],
					),
				},
				{
					Part:       string(scene.PracticeModePart3),
					SourceID:   "topic-group-1",
					TopicTitle: "Fixture topic",
					TurnBlueprints: slices.Clone(
						payload.PracticeContext.TaskBlueprints[1:],
					),
				},
			}
		case scene.PracticeModePart3:
			assignment.Parts = []evidence.IELTSAssignmentPart{{
				Part:           string(scene.PracticeModePart3),
				SourceID:       "topic-group-1",
				TopicTitle:     "Fixture topic",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints),
			}}
		}
		payload.PracticeContext.IELTSAssignment = assignment
	}
	payload.PracticeContext.Scene.ID = "scene-general-1"
	payload.PracticeContext.Preparation.BackgroundSnapshotHash =
		evidenceTextHash(evidenceTestPreparationBackground)
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sourceManifestHash, err := evidence.SourceManifestHash(provisional)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID := evidence.DeriveSnapshotID(
		testOwnerA,
		"practice-session-1",
		evaluation.ScopeSession,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		turn := payload.ConfirmedTurns[index]
		payload.EvidenceRefs[index].SnapshotID = snapshotID
		payload.EvidenceRefs[index].EvidenceRefID = evidence.StableRefID(
			snapshotID,
			turn.TurnID,
			turn.Transcript.EvidenceVersion,
			turn.Audio.ChecksumSHA256,
		)
	}
	canonical, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := evidence.EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              evaluation.ScopeSession,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("general Scene evidence.EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}
