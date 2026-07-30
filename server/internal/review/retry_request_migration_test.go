package review

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestRetryTurnMigrationFreezesNonEffectiveSameQuestionSaga(
	t *testing.T,
) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000041_speech_feedback_retry_turns.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"conversation_retry_turn_drafts",
		"'answering'",
		"turn_kind",
		"'effective'",
		"'retry'",
		"counts_toward_effective_turn_limit",
		"practice_retry_turn_authorizations",
		"review_speech_feedback_retry_requests",
		"'pending'",
		"'turn_created'",
		"'failed'",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("retry Turn migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"object_key",
		"signed_url",
		"overall_score",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("retry Turn migration contains forbidden %q", forbidden)
		}
	}
}
