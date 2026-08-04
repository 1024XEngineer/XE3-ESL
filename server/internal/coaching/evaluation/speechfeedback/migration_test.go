package speechfeedback

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestSpeechFeedbackMigrationKeepsTypedVoiceSourcesAndFencing(
	t *testing.T,
) {
	t.Parallel()
	base, err := migrations.Files.ReadFile(
		"000040_review_speech_feedback.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := migrations.Files.ReadFile(
		"000053_evaluation_speech_feedback_authority.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	baseSQL := strings.ToLower(string(base))
	sql := baseSQL + "\n" + strings.ToLower(string(authority))
	for _, required := range []string{
		"evaluation_speech_feedback_turn_snapshots",
		"evaluation_speech_feedbacks",
		"evaluation_speech_feedback_items",
		"conversation_turn",
		"agent_voice_message",
		"conversation_transcript",
		"agent_transcript",
		"source_digest",
		"deletion_generation",
		"fencing_token",
		"lease_expires_at",
		"provisional",
		"insufficient",
		"provider_unavailable",
		"provider_response_invalid",
		"processing_timeout",
		"internal_processing_error",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("SpeechFeedback migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"overall_score",
		"profile_update_eligible",
		"object_key",
		"signed_url",
	} {
		if strings.Contains(baseSQL, forbidden) {
			t.Errorf(
				"SpeechFeedback migration contains forbidden %q",
				forbidden,
			)
		}
	}
}
