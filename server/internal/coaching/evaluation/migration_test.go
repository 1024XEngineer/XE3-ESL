package evaluation

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestEvaluationMigrationFreezesFactsAndMinimizesOutbox(t *testing.T) {
	content, err := migrations.Files.ReadFile(
		"000024_evaluation_ledger.up.sql",
	)
	if err != nil {
		t.Fatalf("read Evaluation migration: %v", err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table evaluation_ledgers",
		"create table evaluation_revisions",
		"create table evaluation_revision_states",
		"create table evaluation_outbox",
		"evaluation_revisions_immutable",
		"evaluation_assert_revision_chain",
		"evaluation_outbox_revision_channel_unique",
		"evaluation_outbox_channel_key_unique",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"transcript",
		"audio_url",
		"provider_key",
		"system_prompt",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("migration contains forbidden sensitive field %q", forbidden)
		}
	}
}
