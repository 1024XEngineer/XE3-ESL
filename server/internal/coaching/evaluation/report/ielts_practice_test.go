package report

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestProjectIELTSSpeakingPracticeReportSeparatesPart2AndPart3(t *testing.T) {
	t.Parallel()
	snapshot := ieltsPracticeReportTestSnapshot(t, "PART_2")
	provider := &ieltsPracticeReportProvider{}
	engine := scoring.NewGeneralSceneEngine(provider)
	atoms := make([]scoring.GeneralSceneAtomicResult, 0, 8)
	for _, part := range []scoring.IELTSPart{scoring.IELTSPart2, scoring.IELTSPart3} {
		for _, dimension := range scoring.GeneralSceneDimensions() {
			atom, err := engine.EvaluateAtomic(
				context.Background(),
				snapshot,
				scoring.GeneralSceneAtomicKey{Part: part, Dimension: dimension},
			)
			if err != nil {
				t.Fatalf("evaluate IELTS practice atom: %v", err)
			}
			atoms = append(atoms, atom)
		}
	}
	result, err := scoring.AggregateGeneralSceneAtoms(snapshot, atoms)
	if err != nil {
		t.Fatalf("evaluate IELTS practice fixture: %v", err)
	}
	for _, dimension := range result.Dimensions {
		if len(dimension.Improvements) != 2 || len(dimension.Examples) != 2 {
			t.Fatalf("cross-Part dimension = %#v", dimension)
		}
	}
	formal, err := ProjectGeneralSceneFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("project IELTS practice FormalReport: %v", err)
	}
	if formal.DetailSchema != IELTSSpeakingPracticeReportSchemaVersion {
		t.Fatalf("detail schema = %q", formal.DetailSchema)
	}
	if formal.Summary != "本次 IELTS 专项练习已形成分段复盘，可按对应 Part 继续练习。" {
		t.Fatalf("summary = %q", formal.Summary)
	}
	var detail IELTSSpeakingPracticeReport
	if err := json.Unmarshal(formal.Detail, &detail); err != nil {
		t.Fatalf("decode IELTS practice detail: %v", err)
	}
	if !detail.Valid() ||
		detail.ReportScope != IELTSSpeakingPracticeReportPart23 ||
		!slices.Equal(detail.AvailableSections, []scoring.IELTSPart{
			scoring.IELTSPart2,
			scoring.IELTSPart3,
		}) || len(detail.SectionReviews) != 2 {
		t.Fatalf("practice detail = %#v", detail)
	}
	part2 := detail.SectionReviews[0]
	part3 := detail.SectionReviews[1]
	if len(part2.QuestionIndexes) != 1 ||
		len(part3.QuestionIndexes) != ieltsReportQuestionCount-
			ieltsReportPart1QuestionCount-ieltsReportPart2QuestionCount ||
		len(part2.StrengthFindingIDs) != len(result.Dimensions) ||
		len(part2.ImprovementFindingIDs) != len(result.Dimensions) ||
		len(part2.UpgradeExampleFindingIDs) != len(result.Dimensions) ||
		len(part3.StrengthFindingIDs) != len(result.Dimensions) ||
		len(part3.ImprovementFindingIDs) != len(result.Dimensions) ||
		len(part3.UpgradeExampleFindingIDs) != len(result.Dimensions) {
		t.Fatalf("section reviews = %#v", detail.SectionReviews)
	}
}

func TestProjectIELTSSpeakingPracticeReportScopesStandaloneParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode    string
		scope   IELTSSpeakingPracticeReportScope
		section scoring.IELTSPart
	}{
		{mode: "PART_1", scope: IELTSSpeakingPracticeReportPart1, section: scoring.IELTSPart1},
		{mode: "PART_3", scope: IELTSSpeakingPracticeReportPart3, section: scoring.IELTSPart3},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			snapshot := ieltsPracticeReportTestSnapshot(t, test.mode)
			result, err := scoring.NewGeneralSceneEngine(
				&ieltsPracticeReportProvider{},
			).Evaluate(context.Background(), snapshot)
			if err != nil {
				t.Fatalf("evaluate IELTS practice fixture: %v", err)
			}
			detail, err := ProjectIELTSSpeakingPracticeReport(snapshot, result)
			if err != nil {
				t.Fatalf("project IELTS practice detail: %v", err)
			}
			if !detail.Valid() || detail.ReportScope != test.scope ||
				!slices.Equal(detail.AvailableSections, []scoring.IELTSPart{test.section}) ||
				len(detail.SectionReviews) != 1 {
				t.Fatalf("practice detail = %#v", detail)
			}
		})
	}
}

func TestProjectIELTSSpeakingBandPracticeReportKeepsBandScale(t *testing.T) {
	t.Parallel()
	snapshot := ieltsPracticeReportTestSnapshot(t, "PART_1")
	result, err := scoring.NewIELTSSpeakingShadowEngine(
		&ieltsBandPracticeReportProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("evaluate IELTS Band practice fixture: %v", err)
	}
	formal, err := ProjectIELTSFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("project IELTS Band practice FormalReport: %v", err)
	}
	if formal.DetailSchema != IELTSSpeakingPracticeReportSchemaVersion ||
		len(formal.Dimensions) != len(scoring.IELTSCriteria()) {
		t.Fatalf("formal = %#v", formal)
	}
	for _, dimension := range formal.Dimensions {
		if dimension.Scale != ReportScaleIELTSBand {
			t.Fatalf("dimension = %#v", dimension)
		}
	}
	var detail IELTSSpeakingPracticeReport
	if err := json.Unmarshal(formal.Detail, &detail); err != nil ||
		!detail.Valid() || detail.ReportScope != IELTSSpeakingPracticeReportPart1 {
		t.Fatalf("detail = %#v; error = %v", detail, err)
	}
}

type ieltsBandPracticeReportProvider struct{}

func (*ieltsBandPracticeReportProvider) AnalyzeIELTSCriterion(
	_ context.Context,
	request scoring.IELTSSpeakingCriterionProviderRequest,
) (scoring.IELTSSpeakingShadowProviderResult, error) {
	input := request.Input
	criterion := input.AssessableCriteria[0]
	response := input.Questions[0].Response
	if response == nil {
		panic("IELTS Band report fixture requires an answered opportunity")
	}
	prefix := map[scoring.IELTSCriterion]string{
		scoring.IELTSCriterionFC:  "ielts.fc",
		scoring.IELTSCriterionLR:  "ielts.lr",
		scoring.IELTSCriterionGRA: "ielts.gra",
		scoring.IELTSCriterionPR:  "ielts.pr",
	}[criterion]
	criterionPayload := map[string]any{
		"criterion_id": criterion,
		"strengths": []any{map[string]any{
			"template_id": prefix + ".strength.v1",
			"evidence": []any{map[string]any{
				"evidence_ref_id": response.EvidenceRefID,
				"quote":           response.Transcript,
				"occurrence":      1,
			}},
		}},
		"improvements":     []any{},
		"upgrade_examples": []any{},
	}
	if len(input.RubricDescriptors) == 1 {
		criterionPayload["rubric_descriptor"] =
			input.RubricDescriptors[0].Descriptors[5].ID
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": scoring.IELTSSpeakingShadowProviderSchemaVersion,
		"criteria":       []any{criterionPayload},
	})
	return scoring.IELTSSpeakingShadowProviderResult{
		Payload: payload, Provider: "qianwen", Model: "qwen-plus",
		RequestID: "band-" + string(criterion),
	}, err
}

type ieltsPracticeReportProvider struct{}

func (*ieltsPracticeReportProvider) AnalyzeGeneralScene(
	_ context.Context,
	input scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	payload := generalSceneReportProviderPayload{
		SchemaVersion: scoring.GeneralSceneProviderSchemaVersion,
		Dimensions: make(
			[]generalSceneReportProviderDimension,
			0,
			len(input.AssessableDimensions),
		),
	}
	for index, dimension := range input.AssessableDimensions {
		first := input.Opportunities[0].Response
		last := input.Opportunities[len(input.Opportunities)-1].Response
		if first == nil || last == nil {
			panic("IELTS practice report fixture requires answered opportunities")
		}
		payload.Dimensions = append(payload.Dimensions,
			generalSceneReportProviderDimension{
				DimensionID: dimension,
				Score:       60 + index*10,
				Strengths: []generalSceneReportProviderFinding{{
					TemplateID: string(dimension) + ":STRENGTH:v1",
					Evidence: []generalSceneReportProviderAnchor{{
						EvidenceRefID: first.EvidenceRefID,
						Quote:         first.Transcript,
						Occurrence:    1,
					}},
				}},
				Improvements: []generalSceneReportProviderFinding{
					{
						TemplateID: string(dimension) + ":IMPROVEMENT:v1",
						Evidence: []generalSceneReportProviderAnchor{{
							EvidenceRefID: first.EvidenceRefID,
							Quote:         first.Transcript,
							Occurrence:    1,
						}},
					},
					{
						TemplateID: string(dimension) + ":IMPROVEMENT:v1",
						Evidence: []generalSceneReportProviderAnchor{{
							EvidenceRefID: last.EvidenceRefID,
							Quote:         last.Transcript,
							Occurrence:    1,
						}},
					},
				},
				Examples: []generalSceneReportProviderFinding{{
					TemplateID: string(dimension) + ":RECOMMENDED_EXAMPLE:v1",
					Evidence: []generalSceneReportProviderAnchor{
						{
							EvidenceRefID: first.EvidenceRefID,
							Quote:         first.Transcript,
							Occurrence:    1,
						},
						{
							EvidenceRefID: last.EvidenceRefID,
							Quote:         last.Transcript,
							Occurrence:    1,
						},
					},
				}},
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

func (*ieltsPracticeReportProvider) AnalyzeGeneralSceneAtom(
	_ context.Context,
	input scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	var response *scoring.GeneralSceneResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil {
			response = opportunity.Response
			break
		}
	}
	if response == nil || len(input.AssessableDimensions) != 1 {
		return scoring.GeneralSceneProviderResult{}, errors.New("missing atomic response")
	}
	dimension := input.AssessableDimensions[0]
	anchor := generalSceneReportProviderAnchor{
		EvidenceRefID: response.EvidenceRefID,
		Quote:         response.Transcript,
		Occurrence:    1,
	}
	payload := struct {
		SchemaVersion string                              `json:"schema_version"`
		Dimension     generalSceneReportProviderDimension `json:"dimension"`
	}{
		SchemaVersion: scoring.GeneralSceneAtomicProviderSchemaVersion,
		Dimension: generalSceneReportProviderDimension{
			DimensionID: dimension,
			Score:       70,
			Strengths: []generalSceneReportProviderFinding{{
				TemplateID: string(dimension) + ":STRENGTH:v1",
				Evidence:   []generalSceneReportProviderAnchor{anchor},
			}},
			Improvements: []generalSceneReportProviderFinding{{
				TemplateID: string(dimension) + ":IMPROVEMENT:v1",
				Evidence:   []generalSceneReportProviderAnchor{anchor},
			}},
			Examples: []generalSceneReportProviderFinding{{
				TemplateID: string(dimension) + ":RECOMMENDED_EXAMPLE:v1",
				Evidence:   []generalSceneReportProviderAnchor{anchor},
			}},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return scoring.GeneralSceneProviderResult{}, err
	}
	return scoring.GeneralSceneProviderResult{
		Payload: encoded, Provider: "qianwen", Model: "qwen-plus",
		RequestID: "atomic-" + string(input.EvaluationSection) + "-" + string(dimension),
	}, nil
}

func ieltsPracticeReportTestSnapshot(
	t *testing.T,
	mode string,
) evidence.EvidenceSnapshot {
	t.Helper()
	full := ieltsReportTestSnapshot(t, ieltsReportQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(full.Payload, &payload); err != nil {
		t.Fatalf("decode full IELTS fixture: %v", err)
	}
	start := 0
	end := ieltsReportPart1QuestionCount
	parts := payload.PracticeContext.IELTSAssignment.Parts[:1]
	switch mode {
	case "PART_1":
	case "PART_2":
		start = ieltsReportPart1QuestionCount
		end = ieltsReportQuestionCount
		parts = payload.PracticeContext.IELTSAssignment.Parts[1:]
	case "PART_3":
		start = ieltsReportPart1QuestionCount + ieltsReportPart2QuestionCount
		end = ieltsReportQuestionCount
		parts = payload.PracticeContext.IELTSAssignment.Parts[2:]
	default:
		t.Fatalf("unsupported IELTS practice mode %q", mode)
	}
	payload.PracticeContext.PracticeMode = mode
	payload.PracticeContext.PracticeOption.Mode = mode
	payload.PracticeContext.EvaluationPolicyRef =
		scoring.IELTSSpeakingPracticeEvaluationPolicyRef
	payload.PracticeContext.TaskBlueprints = slices.Clone(
		payload.PracticeContext.TaskBlueprints[start:end],
	)
	payload.PracticeContext.IELTSAssignment.Mode = mode
	payload.PracticeContext.IELTSAssignment.Parts = slices.Clone(parts)
	payload.OpportunityManifest = slices.Clone(payload.OpportunityManifest[start:end])
	payload.ConfirmedTurns = slices.Clone(payload.ConfirmedTurns[start:end])
	payload.EvidenceRefs = slices.Clone(payload.EvidenceRefs[start:end])
	payload.ProviderLineage.ASR = slices.Clone(payload.ProviderLineage.ASR[start:end])
	payload.VersionManifest.TurnEvidence = slices.Clone(
		payload.VersionManifest.TurnEvidence[start:end],
	)
	for index := range payload.OpportunityManifest {
		payload.OpportunityManifest[index].Sequence = index + 1
		payload.ConfirmedTurns[index].Sequence = index + 1
	}
	return rebuildReportTestSnapshot(t, payload, evaluation.SceneIELTSSpeaking)
}
