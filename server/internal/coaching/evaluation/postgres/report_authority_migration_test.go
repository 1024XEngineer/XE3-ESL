package postgres

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestReportAuthorityMigrationRemovesSupersededReviewScoringStore(
	t *testing.T,
) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000058_evaluation_report_authority.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"drop table review_evidence",
		"drop table review_generation_attempts",
		"drop table reviews",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("report authority migration is missing %q", required)
		}
	}
	for _, retained := range []string{
		"review_deletion_fences",
		"review_repractice_requests",
		"evaluation_formal_reports",
	} {
		if strings.Contains(sql, "drop table "+retained) {
			t.Errorf("report authority migration removes %q", retained)
		}
	}
}
