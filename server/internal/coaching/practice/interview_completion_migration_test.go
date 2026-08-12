package practice

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestInterviewUserControlledSessionMigrationVersionsPoliciesSafely(
	t *testing.T,
) {
	t.Parallel()
	upContent, err := migrations.Files.ReadFile(
		"000093_interview_user_controlled_sessions.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	downContent, err := migrations.Files.ReadFile(
		"000093_interview_user_controlled_sessions.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}

	upSQL := strings.ToLower(string(upContent))
	for _, required := range []string{
		"interview.user_controlled.session.v1",
		"interview.project_deep_dive.user_controlled.session.v1",
		"'completion_mode', 'user_controlled'",
		"'max_effective_turns', 0",
		"not exists",
		"from practice_sessions",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("up migration is missing %q", required)
		}
	}

	downSQL := strings.ToLower(string(downContent))
	for _, required := range []string{
		"interview.practice.session.v1",
		"interview.project_deep_dive.session.v1",
		"'completion_mode', 'turn_limited'",
	} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("down migration is missing %q", required)
		}
	}
}
