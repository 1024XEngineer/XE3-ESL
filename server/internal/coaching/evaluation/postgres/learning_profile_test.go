package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/learningprofile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestAggregateLearningProfileDimensionTracksTrendAndRecurringIssues(t *testing.T) {
	t.Parallel()
	newest := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	contributions := []learningProfileContribution{
		{
			EvaluationID:         "10000000-0000-4000-8000-000000000001",
			EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
			Scale:                report.ReportScalePercentage100,
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
			Scale:                report.ReportScalePercentage100,
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
		profile.Trend != learningprofile.TrendImproving ||
		len(profile.RecurringIssues) != 1 ||
		profile.RecurringIssues[0].Count != 2 ||
		profile.RecurringIssues[0].Label != "表达需要更具体" ||
		len(profile.SourceEvaluations) != 2 ||
		!profile.UpdatedAt.Equal(newest) {
		t.Fatalf("Learning Profile = %#v", profile)
	}
}

func TestLearningProfileMigrationKeepsProfilePayloadBounded(t *testing.T) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000056_evaluation_reports_learning_profile.up.sql",
	)
	if err != nil {
		t.Fatalf("read Learning Profile migration: %v", err)
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
			t.Errorf("Learning Profile migration is missing %q", required)
		}
	}
}
