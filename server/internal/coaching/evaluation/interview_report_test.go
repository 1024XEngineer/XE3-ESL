package evaluation

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestProjectInterviewReportUsesFrozenQuestionsAndEvidence(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpUnanswered,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepare Interview Shadow: %v", err)
	}
	payload := validInterviewProviderPayloadValue(prepared.input)
	for index := range payload.Dimensions {
		if index >= 3 {
			break
		}
		dimension := &payload.Dimensions[index]
		template, ok := interviewShadowFeedbackTemplate(
			dimension.DimensionID,
			interviewFindingImprovement,
		)
		if !ok {
			t.Fatalf("missing improvement template for %q", dimension.DimensionID)
		}
		dimension.Improvements = []interviewProviderFinding{{
			TemplateID: template.ID,
			Evidence:   dimension.Strengths[0].Evidence,
		}}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Provider payload: %v", err)
	}
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{payload: encoded},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	if !report.Valid() ||
		report.SchemaVersion != InterviewReportSchemaVersion ||
		report.ScoreabilityStatus != InterviewScoreabilityProvisional ||
		report.GateStatus != InterviewGateFeedbackOnly ||
		report.ReadinessLevel != InterviewReadinessNotAssessed ||
		len(report.Dimensions) != 5 ||
		len(report.Questions) != 2 ||
		len(report.PriorityActions) != 3 {
		t.Fatalf("report = %#v", report)
	}
	for _, dimension := range report.Dimensions {
		if dimension.ScoreabilityStatus == InterviewScoreabilityProvisional {
			if dimension.Score == nil || *dimension.Score != 75 {
				t.Fatalf("dimension score = %#v", dimension.Score)
			}
		} else if dimension.Score != nil {
			t.Fatalf("blocked dimension exposes score %#v", dimension.Score)
		}
	}
	first := report.Questions[0]
	if first.QuestionText != "Tell me about a migration you led." ||
		first.ConfirmedTranscript != "I led a careful migration." ||
		first.AssessmentStatus != InterviewAssessmentAssessed ||
		first.ResponseTurnID != "turn-1" ||
		len(first.EvidenceRefIDs) != 1 ||
		len(first.DimensionFindings) != 5 {
		t.Fatalf("first question = %#v", first)
	}
	second := report.Questions[1]
	if second.QuestionText != "What changed after the migration?" ||
		second.OpportunityStatus != InterviewOpportunityNotProvided ||
		second.AssessmentStatus != InterviewAssessmentNotAssessed ||
		second.ConfirmedTranscript != "" ||
		second.ResponseTurnID != "" ||
		len(second.EvidenceRefIDs) != 0 {
		t.Fatalf("unanswered question = %#v", second)
	}
	for index, action := range report.PriorityActions {
		if action.DimensionID != interviewDimensionOrder[index] ||
			action.FindingID !=
				report.Dimensions[index].Improvements[0].FindingID {
			t.Errorf("priority action %d = %#v", index, action)
		}
	}

	wire, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	for _, forbidden := range []string{
		"snapshot_id",
		"provider_lineage",
		"raw",
		"display",
		"overall",
		"total",
		"weights",
		"probe_weight",
	} {
		if jsonContainsExactKey(decoded, forbidden) {
			t.Errorf("report contains forbidden key %q: %s", forbidden, wire)
		}
	}
}

func TestProjectInterviewReportRejectsCrossSnapshotResult(t *testing.T) {
	t.Parallel()
	first := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	second := interviewShadowTestSnapshot(
		t,
		"I led a different migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), first)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if _, err := ProjectInterviewReport(second, result); err == nil {
		t.Fatal("cross-snapshot result was projected")
	}
}

func TestInterviewReportRejectsNonCanonicalPriorityActions(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	report.PriorityActions = []InterviewReportPriorityRef{{
		DimensionID: InterviewDimensionRelevance,
		FindingID:   report.Dimensions[0].Strengths[0].FindingID,
	}}
	if report.Valid() {
		t.Fatal("strength was accepted as a priority improvement")
	}
	if reflect.DeepEqual(report.PriorityActions, []InterviewReportPriorityRef{}) {
		t.Fatal("test did not mutate priority actions")
	}
}

func TestInterviewReportRejectsCrossKindQuestionFindingReference(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	ref := report.Questions[0].DimensionFindings[0].
		StrengthFindingIDs[0]
	report.Questions[0].DimensionFindings[0].
		StrengthFindingIDs = []string{}
	report.Questions[0].DimensionFindings[0].
		RecommendedExpressionFindingIDs = []string{ref}
	if report.Valid() {
		t.Fatal("strength finding was accepted as a recommended expression")
	}
}

func TestInterviewReportRejectsDimensionEvidenceUnionMismatch(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	report.Dimensions[0].EvidenceRefIDs = []string{}
	if report.Valid() {
		t.Fatal("dimension accepted evidence refs that omit finding evidence")
	}
}

func TestInterviewReportRejectsProvisionalDimensionUnderBlockedRoot(
	t *testing.T,
) {
	t.Parallel()
	blockedSnapshot := interviewShadowTestSnapshot(
		t,
		"Yes.",
		interviewFollowUpNone,
	)
	blockedResult, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), blockedSnapshot)
	if err != nil {
		t.Fatalf("blocked Evaluate: %v", err)
	}
	blockedReport, err := ProjectInterviewReport(
		blockedSnapshot,
		blockedResult,
	)
	if err != nil {
		t.Fatalf("blocked report: %v", err)
	}

	provisionalSnapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	provisionalResult, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), provisionalSnapshot)
	if err != nil {
		t.Fatalf("provisional Evaluate: %v", err)
	}
	provisionalReport, err := ProjectInterviewReport(
		provisionalSnapshot,
		provisionalResult,
	)
	if err != nil {
		t.Fatalf("provisional report: %v", err)
	}
	blockedReport.Dimensions[0] = provisionalReport.Dimensions[0]
	if blockedReport.Valid() {
		t.Fatal("blocked root accepted a provisional dimension")
	}
}

func TestInterviewReportQuestionFindingRequiresEvidenceIntersection(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpAnswered,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepare Interview Shadow: %v", err)
	}
	payload := validInterviewProviderPayloadValue(prepared.input)
	followUp := prepared.input.Opportunities[1].Response
	if followUp == nil {
		t.Fatal("fixture has no follow-up response")
	}
	payload.Dimensions[0].Strengths[0].Evidence = append(
		payload.Dimensions[0].Strengths[0].Evidence,
		interviewProviderAnchor{
			EvidenceRefID: followUp.EvidenceRefID,
			Quote:         followUp.Transcript,
			Occurrence:    1,
		},
	)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Provider payload: %v", err)
	}
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{payload: encoded},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	finding := report.Dimensions[0].Strengths[0]
	if len(finding.Evidence) != 2 ||
		len(report.Questions[0].DimensionFindings[0].
			StrengthFindingIDs) != 1 ||
		len(report.Questions[1].DimensionFindings[0].
			StrengthFindingIDs) != 1 ||
		!report.Valid() {
		t.Fatalf("multi-question finding report = %#v", report)
	}

	report.Questions[0].EvidenceRefIDs = []string{
		"evidence_ref_unrelated",
	}
	if report.Valid() {
		t.Fatal("question accepted a finding without evidence intersection")
	}
}
