package evaluation

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestProjectInterviewFormalReportPreservesCanonicalDetail(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepare Interview Shadow: %v", err)
	}
	payload, err := json.Marshal(validInterviewProviderPayloadValue(prepared.input))
	if err != nil {
		t.Fatalf("marshal Provider payload: %v", err)
	}
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{payload: payload},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	report, err := ProjectInterviewFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewFormalReport: %v", err)
	}
	if !report.Valid() ||
		report.SceneType != SceneInterview ||
		report.SchemaVersion != FormalReportSchemaVersion ||
		report.DetailSchema != InterviewReportSchemaVersion ||
		len(report.Dimensions) != len(result.Dimensions) {
		t.Fatalf("formal report = %#v", report)
	}
	var detail InterviewReport
	if err := json.Unmarshal(report.Detail, &detail); err != nil {
		t.Fatalf("decode Interview detail: %v", err)
	}
	if !detail.Valid() || len(detail.Questions) != 1 {
		t.Fatalf("Interview detail = %#v", detail)
	}
}

func TestProjectIELTSFormalReportPreservesCanonicalDetail(t *testing.T) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	result, err := NewIELTSSpeakingShadowEngine(
		&ieltsProviderStub{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	report, err := ProjectIELTSFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSFormalReport: %v", err)
	}
	if !report.Valid() ||
		report.SceneType != SceneIELTSSpeaking ||
		report.DetailSchema != IELTSSpeakingReportSchemaVersion ||
		len(report.Dimensions) != len(result.Criteria) {
		t.Fatalf("formal report = %#v", report)
	}
	var detail IELTSSpeakingReport
	if err := json.Unmarshal(report.Detail, &detail); err != nil {
		t.Fatalf("decode IELTS detail: %v", err)
	}
	if !detail.Valid() || len(detail.Questions) != ieltsQuestionCount {
		t.Fatalf("IELTS detail = %#v", detail)
	}
}

func TestFormalReportRejectsNonFiniteNumbersAndNonObjectDetail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*FormalReport)
	}{
		{
			name: "NaN score",
			mutate: func(report *FormalReport) {
				value := math.NaN()
				report.Dimensions[0].Score = &value
			},
		},
		{
			name: "infinite coverage",
			mutate: func(report *FormalReport) {
				report.Dimensions[0].Coverage = math.Inf(1)
			},
		},
		{
			name: "non-object detail",
			mutate: func(report *FormalReport) {
				report.Detail = json.RawMessage(`[]`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := validFormalReportForValidation()
			test.mutate(&report)
			if report.Valid() {
				t.Fatal("invalid canonical report was accepted")
			}
		})
	}
}

func TestFormalReportPriorityActionTargetsItsDimensionImprovement(
	t *testing.T,
) {
	t.Parallel()
	report := validFormalReportForValidation()
	report.Dimensions[0].Strengths = report.Dimensions[0].Improvements
	report.Dimensions[0].Improvements = []ReportFinding{}
	if report.Valid() {
		t.Fatal("priority action targeting a strength was accepted")
	}

	report = validFormalReportForValidation()
	report.Dimensions = append(report.Dimensions, ReportDimension{
		Key:          "INTERVIEW_EVIDENCE",
		Score:        report.Dimensions[0].Score,
		Scale:        ReportScalePercentage100,
		Coverage:     1,
		Confidence:   0.8,
		ReasonCodes:  []string{},
		EvidenceRefs: []string{},
		Strengths:    []ReportFinding{},
		Improvements: []ReportFinding{},
		Examples:     []ReportFinding{},
	})
	report.PriorityActions[0].DimensionKey = "INTERVIEW_EVIDENCE"
	if report.Valid() {
		t.Fatal("cross-dimension priority action was accepted")
	}
}

func validFormalReportForValidation() FormalReport {
	score := 80.0
	finding := ReportFinding{
		ID:         "finding_1",
		Message:    "回答结构需要更清楚。",
		Suggestion: "使用 STAR 结构。",
		Evidence:   []ReportEvidence{},
	}
	return FormalReport{
		SchemaVersion:      FormalReportSchemaVersion,
		SceneType:          SceneInterview,
		SceneModel:         "PROJECT_EXPERIENCE_DEEP_DIVE",
		ScoreabilityStatus: ReportScoreabilityProvisional,
		Summary:            "本次回答已经形成可复盘的文本反馈。",
		Dimensions: []ReportDimension{{
			Key:          "INTERVIEW_STRUCTURE",
			Score:        &score,
			Scale:        ReportScalePercentage100,
			Coverage:     1,
			Confidence:   0.8,
			ReasonCodes:  []string{},
			EvidenceRefs: []string{},
			Strengths:    []ReportFinding{},
			Improvements: []ReportFinding{finding},
			Examples:     []ReportFinding{},
		}},
		PriorityActions: []ReportPriorityAction{{
			DimensionKey: "INTERVIEW_STRUCTURE",
			FindingID:    finding.ID,
		}},
		DetailSchema: "interview-report/v1",
		Detail:       json.RawMessage(`{"schema_version":"interview-report/v1"}`),
	}
}

func TestAggregateLearningProfileDimensionTracksTrendAndRecurringIssues(t *testing.T) {
	t.Parallel()
	newest := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	contributions := []learningProfileContribution{
		{
			EvaluationID:         "10000000-0000-4000-8000-000000000001",
			EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
			Scale:                ReportScalePercentage100,
			Score:                80,
			Confidence:           0.8,
			Issues: []learningProfileContributionIssue{{
				Key:   "issue:clarity",
				Label: "表达需要更具体",
			}},
			CreatedAt: newest,
		},
		{
			EvaluationID:         "30000000-0000-4000-8000-000000000003",
			EvaluationRevisionID: "40000000-0000-4000-8000-000000000004",
			Scale:                ReportScalePercentage100,
			Score:                70,
			Confidence:           0.6,
			Issues: []learningProfileContributionIssue{{
				Key:   "issue:clarity",
				Label: "表达不够具体",
			}},
			CreatedAt: newest.Add(-24 * time.Hour),
		},
	}

	profile, err := aggregateLearningProfileDimension(
		"interview.specificity",
		contributions,
	)
	if err != nil {
		t.Fatalf("aggregate Learning Profile: %v", err)
	}
	if profile.EstimatedValue != 75.714 ||
		profile.Confidence != 0.7 ||
		profile.Trend != LearningProfileTrendImproving ||
		len(profile.RecurringIssues) != 1 ||
		profile.RecurringIssues[0].Count != 2 ||
		profile.RecurringIssues[0].Label != "表达需要更具体" ||
		len(profile.SourceEvaluations) != 2 ||
		!profile.UpdatedAt.Equal(newest) {
		t.Fatalf("Learning Profile = %#v", profile)
	}
}

func TestFormalReportMigrationKeepsProfilePayloadBounded(t *testing.T) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000056_evaluation_reports_learning_profile.up.sql",
	)
	if err != nil {
		t.Fatalf("read formal report migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE evaluation_formal_reports",
		"CREATE TABLE learning_profile_contributions",
		"CREATE TABLE learning_profile_dimensions",
		"source_evaluation_refs",
		"strategy_version",
		"jsonb_array_length(source_evaluation_refs) BETWEEN 1 AND 20",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("formal report migration is missing %q", required)
		}
	}
}
