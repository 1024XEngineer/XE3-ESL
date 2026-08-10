package postgres

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestInterviewAnswerAssessmentMigrationPersistsBoundedAuthority(t *testing.T) {
	up, err := migrations.Files.ReadFile(
		"000086_interview_answer_assessments.up.sql",
	)
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile(
		"000086_interview_answer_assessments.down.sql",
	)
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN answer_assessment jsonb",
		"ADD COLUMN assessment_policy_version text",
		"ADD COLUMN advance_authorized boolean",
		"ADD COLUMN dialogue_act text",
		"jsonb_typeof(answer_assessment) = 'object'",
		"FROM pg_constraint",
		"pg_get_constraintdef(oid)",
		"ALTER TABLE practice_turn_results DROP CONSTRAINT %I",
		"effective_turns >= 0 AND effective_turns <= round_number",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Fatalf("up migration missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"DROP COLUMN advance_authorized",
		"DROP COLUMN assessment_policy_version",
		"DROP COLUMN answer_assessment",
		"DROP COLUMN dialogue_act",
		"cannot roll back interview answer assessments while non-counting turn results exist",
		"effective_turns = round_number",
	} {
		if !strings.Contains(string(down), fragment) {
			t.Fatalf("down migration missing %q", fragment)
		}
	}
}
