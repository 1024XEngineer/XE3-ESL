package report

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestProjectGeneralSceneFormalReportPreservesGroundedResult(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneReportTestSnapshot(
		t,
		"I need to change my room because the air conditioner is not working.",
	)
	result, err := scoring.NewGeneralSceneEngine(
		&generalSceneReportProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("evaluate general Scene fixture: %v", err)
	}
	formal, err := ProjectGeneralSceneFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("project general Scene report: %v", err)
	}
	if !formal.Valid() ||
		formal.SceneType != evaluation.SceneOverseasDaily ||
		formal.PracticeExperience != string(scene.PracticeExperienceRoleplay) ||
		formal.SceneCategory != string(scene.SceneCategoryRoleplayTravel) ||
		formal.PracticeMode != string(scene.PracticeModeFullSimulation) ||
		formal.ScoreabilityStatus != ReportScoreabilityProvisional ||
		len(formal.Dimensions) != len(scoring.GeneralSceneDimensions()) {
		t.Fatalf("formal report = %#v", formal)
	}
}

func TestProjectGeneralSceneFormalReportPreservesInsufficientStatus(t *testing.T) {
	t.Parallel()
	snapshot := generalSceneReportTestSnapshot(t, "Okay.")
	result, err := scoring.NewGeneralSceneEngine(
		&generalSceneReportProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("evaluate insufficient general Scene fixture: %v", err)
	}
	formal, err := ProjectGeneralSceneFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("project insufficient general Scene report: %v", err)
	}
	if !formal.Valid() ||
		formal.ScoreabilityStatus != ReportScoreabilityInsufficient {
		t.Fatalf("formal report = %#v", formal)
	}
}

type generalSceneReportProvider struct{}

type generalSceneReportProviderPayload struct {
	SchemaVersion string                                `json:"schema_version"`
	Dimensions    []generalSceneReportProviderDimension `json:"dimensions"`
}

type generalSceneReportProviderDimension struct {
	DimensionID  scoring.GeneralSceneDimension       `json:"dimension_id"`
	Score        int                                 `json:"score"`
	Strengths    []generalSceneReportProviderFinding `json:"strengths"`
	Improvements []generalSceneReportProviderFinding `json:"improvements"`
	Examples     []generalSceneReportProviderFinding `json:"recommended_examples"`
}

type generalSceneReportProviderFinding struct {
	TemplateID string                             `json:"template_id"`
	Evidence   []generalSceneReportProviderAnchor `json:"evidence"`
}

type generalSceneReportProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

func (*generalSceneReportProvider) AnalyzeGeneralScene(
	_ context.Context,
	input scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	response := input.Opportunities[0].Response
	if response == nil {
		panic("general Scene report fixture requires one response")
	}
	payload := generalSceneReportProviderPayload{
		SchemaVersion: scoring.GeneralSceneProviderSchemaVersion,
		Dimensions: make(
			[]generalSceneReportProviderDimension,
			0,
			len(input.AssessableDimensions),
		),
	}
	for index, dimension := range input.AssessableDimensions {
		payload.Dimensions = append(
			payload.Dimensions,
			generalSceneReportProviderDimension{
				DimensionID: dimension,
				Score:       60 + index*10,
				Strengths:   []generalSceneReportProviderFinding{},
				Improvements: []generalSceneReportProviderFinding{{
					TemplateID: string(dimension) + ":IMPROVEMENT:v1",
					Evidence: []generalSceneReportProviderAnchor{{
						EvidenceRefID: response.EvidenceRefID,
						Quote:         response.Transcript,
						Occurrence:    1,
					}},
				}},
				Examples: []generalSceneReportProviderFinding{},
			},
		)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return scoring.GeneralSceneProviderResult{}, err
	}
	return scoring.GeneralSceneProviderResult{
		Payload: raw, Provider: "provider", Model: "model", RequestID: "request-1",
	}, nil
}

func generalSceneReportTestSnapshot(
	t *testing.T,
	transcript string,
) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(reportTestEvidencePayload(), &payload); err != nil {
		t.Fatalf("decode general Scene report fixture: %v", err)
	}
	payload.PracticeContext.PracticeExperience =
		string(scene.PracticeExperienceRoleplay)
	payload.PracticeContext.SceneCategory =
		string(scene.SceneCategoryRoleplayTravel)
	payload.PracticeContext.PracticeMode =
		string(scene.PracticeModeFullSimulation)
	payload.PracticeContext.PracticeOption.Mode =
		string(scene.PracticeModeFullSimulation)
	payload.PracticeContext.EvaluationPolicyRef =
		"general.scene.evaluation.v1"
	payload.PracticeContext.Scene.ID = "scene-general-1"
	payload.PracticeContext.Preparation.BackgroundSnapshotHash =
		reportTestTextHash(reportTestPreparationBackground)
	payload.ConfirmedTurns[0].Transcript.Text = transcript
	payload.EvidenceRefs[0].TranscriptSpan.EndUTF8Byte = len(transcript)
	return rebuildReportTestSnapshot(
		t,
		payload,
		evaluation.SceneOverseasDaily,
	)
}
