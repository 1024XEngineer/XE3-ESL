package practice

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestEvaluationHandoffMigrationAddsDurableDeliveryState(t *testing.T) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000055_practice_evaluation_handoff.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"practice_completed",
		"delivery_status",
		"'pending'",
		"'running'",
		"'delivered'",
		"'failed'",
		"attempt_count",
		"fencing_token",
		"lease_expires_at",
		"available_at",
		"failure_retryable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("Evaluation handoff migration is missing %q", required)
		}
	}
}
