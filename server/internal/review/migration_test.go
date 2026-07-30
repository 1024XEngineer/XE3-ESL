package review

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestScenarioReviewMigrationFreezesContextAndPreciseEvidence(t *testing.T) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000028_review_scenario_policies.up.sql",
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

func TestSpeechFeedbackMigrationKeepsTypedVoiceSourcesAndFencing(
	t *testing.T,
) {
	t.Parallel()
	content, err := migrations.Files.ReadFile(
		"000040_review_speech_feedback.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"review_speech_feedback_turn_snapshots",
		"review_speech_feedbacks",
		"review_speech_feedback_items",
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
		if strings.Contains(sql, forbidden) {
			t.Errorf(
				"SpeechFeedback migration contains forbidden %q",
				forbidden,
			)
		}
	}
}
