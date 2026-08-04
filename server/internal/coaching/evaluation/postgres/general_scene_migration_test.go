package postgres

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestGeneralSceneMigrationExtendsEvaluationRuntimeAuthority(t *testing.T) {
	t.Parallel()
	up, err := migrations.Files.ReadFile(
		"000059_evaluation_general_scene_runtime.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile(
		"000059_evaluation_general_scene_runtime.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	for _, required := range []string{
		"evaluation_general_scene_results",
		"general-scene-evaluation/v1",
		"overseas_daily_life",
		"overseas_workplace",
		"evaluation_interview_result_refs_are_consistent",
		"reject_evaluation_general_scene_result_mutation",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("general Scene migration is missing %q", required)
		}
	}
	if !strings.Contains(
		strings.ToLower(string(down)),
		"drop table if exists evaluation_general_scene_results",
	) {
		t.Fatal("general Scene migration does not restore the prior runtime")
	}
}
