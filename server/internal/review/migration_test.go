package review

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestScenarioReviewMigrationFreezesContextAndPreciseEvidence(t *testing.T) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000025_review_scenario_policies.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"evaluation_context",
		"summary_eligibility",
		"target_kind",
		"feedback_item",
		"anchor_kind",
		"exact_quote",
		"start_utf8_byte",
		"end_utf8_byte",
		"insufficient_evidence",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
}
