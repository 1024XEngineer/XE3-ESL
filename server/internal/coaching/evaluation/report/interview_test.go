package report

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestProjectInterviewReportUsesFrozenQuestionsAndEvidence(t *testing.T) {
	t.Parallel()
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpUnanswered,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{
		ImprovementDimensions: 3,
	})

	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	if !report.Valid() ||
		report.SchemaVersion != InterviewReportSchemaVersion ||
		report.ScoreabilityStatus != scoring.InterviewScoreabilityProvisional ||
		report.GateStatus != scoring.InterviewGateFeedbackOnly ||
		report.ReadinessLevel != scoring.InterviewReadinessNotAssessed ||
		len(report.Dimensions) != 5 ||
		len(report.Questions) != 2 ||
		len(report.PriorityActions) != 3 {
		t.Fatalf("report = %#v", report)
	}
	for _, dimension := range report.Dimensions {
		if dimension.ScoreabilityStatus == scoring.InterviewScoreabilityProvisional {
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
		second.OpportunityStatus != scoring.InterviewOpportunityNotProvided ||
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
	first := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	second := interviewReportTestSnapshot(
		t,
		"I led a different migration.",
		interviewReportFollowUpNone,
	)
	result := interviewReportTestResult(t, first, interviewReportProviderOptions{})
	if _, err := ProjectInterviewReport(second, result); err == nil {
		t.Fatal("cross-snapshot result was projected")
	}
}

func TestInterviewReportRejectsNonCanonicalPriorityActions(t *testing.T) {
	t.Parallel()
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{})
	report, err := ProjectInterviewReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewReport: %v", err)
	}
	report.PriorityActions = []InterviewReportPriorityRef{{
		DimensionID: scoring.InterviewDimensionRelevance,
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
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{})
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
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{})
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
	blockedSnapshot := interviewReportTestSnapshot(
		t,
		"Yes.",
		interviewReportFollowUpNone,
	)
	blockedResult := interviewReportTestResult(
		t,
		blockedSnapshot,
		interviewReportProviderOptions{},
	)
	blockedReport, err := ProjectInterviewReport(
		blockedSnapshot,
		blockedResult,
	)
	if err != nil {
		t.Fatalf("blocked report: %v", err)
	}

	provisionalSnapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	provisionalResult := interviewReportTestResult(
		t,
		provisionalSnapshot,
		interviewReportProviderOptions{},
	)
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
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpAnswered,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{
		AddFollowUpEvidence: true,
	})
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
