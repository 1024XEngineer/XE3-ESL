package report

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestProjectInterviewFormalReportPreservesCanonicalDetail(t *testing.T) {
	t.Parallel()
	snapshot := interviewReportTestSnapshot(
		t,
		"I led a careful migration.",
		interviewReportFollowUpNone,
	)
	result := interviewReportTestResult(t, snapshot, interviewReportProviderOptions{})

	report, err := ProjectInterviewFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectInterviewFormalReport: %v", err)
	}
	if !report.Valid() ||
		report.SceneType != evaluation.SceneInterview ||
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
	snapshot := ieltsReportTestSnapshot(t, scoring.IELTSQuestionCount)
	result := ieltsReportTestResult(t, snapshot)

	report, err := ProjectIELTSFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSFormalReport: %v", err)
	}
	if !report.Valid() ||
		report.SceneType != evaluation.SceneIELTSSpeaking ||
		report.DetailSchema != IELTSSpeakingReportSchemaVersion ||
		len(report.Dimensions) != len(result.Criteria) {
		t.Fatalf("formal report = %#v", report)
	}
	var detail IELTSSpeakingReport
	if err := json.Unmarshal(report.Detail, &detail); err != nil {
		t.Fatalf("decode IELTS detail: %v", err)
	}
	if !detail.Valid() || len(detail.Questions) != scoring.IELTSQuestionCount {
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
		SceneType:          evaluation.SceneInterview,
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
