package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestGeneralSceneEngineProducesGroundedFormalReport(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		SceneOverseasDaily,
		scene.SceneFamilyDaily,
		scene.SceneModelHotelCheckinAndIssueHandling,
		"I need to change my room because the air conditioner is not working.",
	)
	provider := &generalSceneProviderStub{}
	engine := NewGeneralSceneEngine(provider)
	result, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 ||
		provider.input.SceneType != SceneOverseasDaily ||
		provider.input.SceneModel != string(
			scene.SceneModelHotelCheckinAndIssueHandling,
		) || result.ScoreabilityStatus != ReportScoreabilityProvisional ||
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
	report, err := ProjectGeneralSceneFormalReport(snapshot, result)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() || report.SceneType != SceneOverseasDaily ||
		report.SceneModel != string(
			scene.SceneModelHotelCheckinAndIssueHandling,
		) {
		t.Fatalf("report = %#v", report)
	}
}

func TestGeneralSceneEngineDoesNotCallProviderForInsufficientEvidence(
	t *testing.T,
) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		SceneOverseasWorkplace,
		scene.SceneFamilyWorkplace,
		scene.SceneModelWorkplaceBasicDialogue,
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
		result.ScoreabilityStatus != ReportScoreabilityInsufficient ||
		result.Provider != nil || len(result.PriorityActions) != 0 {
		t.Fatalf("provider calls=%d result=%#v", provider.calls, result)
	}
	for _, dimension := range result.Dimensions {
		if dimension.Score != nil || len(dimension.EvidenceRefs) != 0 {
			t.Fatalf("insufficient dimension = %#v", dimension)
		}
	}
	report, err := ProjectGeneralSceneFormalReport(snapshot, result)
	if err != nil || report.ScoreabilityStatus != ReportScoreabilityInsufficient {
		t.Fatalf("report=%#v error=%v", report, err)
	}
}

func TestGeneralSceneEngineRejectsUngroundedProviderQuote(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		SceneIELTSSpeaking,
		scene.SceneFamilyExam,
		scene.SceneModelIELTSSpeakingPart1,
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

func TestGeneralSceneRejectsIELTSFullMockSpecializedModel(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneTestSnapshot(
		t,
		SceneIELTSSpeaking,
		scene.SceneFamilyExam,
		scene.SceneModelIELTSSpeakingFullMock,
		"I usually read books after work because they help me relax.",
	)
	_, err := NewGeneralSceneEngine(&generalSceneProviderStub{}).Evaluate(
		context.Background(),
		snapshot,
	)
	if !errors.Is(err, ErrInvalidRequest) {
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
	sceneType SceneType,
	family scene.SceneFamily,
	model scene.SceneModel,
	transcript string,
) EvidenceSnapshot {
	t.Helper()
	source := interviewShadowTestSnapshot(
		t,
		transcript,
		interviewFollowUpNone,
	)
	var payload evidencePayload
	if err := json.Unmarshal(source.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.SceneFamily = string(family)
	payload.PracticeContext.SceneModel = string(model)
	payload.PracticeContext.Scene.ID = "scene-general-1"
	payload.PracticeContext.Preparation.BackgroundSnapshotHash =
		evidenceTextHash(evidenceTestPreparationBackground)
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sourceManifestHash, err := evidenceSourceManifestHash(provisional)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID := deriveEvidenceSnapshotID(
		testOwnerA,
		"practice-session-1",
		ScopeSession,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		turn := payload.ConfirmedTurns[index]
		payload.EvidenceRefs[index].SnapshotID = snapshotID
		payload.EvidenceRefs[index].EvidenceRefID = stableEvidenceRefID(
			snapshotID,
			turn.TurnID,
			turn.Transcript.EvidenceVersion,
			turn.Audio.ChecksumSHA256,
		)
	}
	canonical, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              ScopeSession,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("general Scene EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}
