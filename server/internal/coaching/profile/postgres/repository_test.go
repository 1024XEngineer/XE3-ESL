package postgres

import (
	"errors"
	"testing"

	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
)

func TestStrictDecodeRejectsUnknownProfileField(t *testing.T) {
	var data coachingprofile.Data
	err := strictDecode(
		[]byte(`{"occupation":"designer","current_goal":"forbidden"}`),
		&data,
	)
	if err == nil {
		t.Fatal("unknown profile field was accepted")
	}
}

func TestStrictDecodeRejectsUnknownNestedSourceFieldAndTrailingJSON(t *testing.T) {
	var sources map[coachingprofile.Field]coachingprofile.FieldSource
	if err := strictDecode([]byte(`{
      "occupation":{
        "type":"user_setting",
        "recorded_at":"2026-08-15T08:00:00Z",
        "evidence":"must-not-persist"
      }
    }`), &sources); err == nil {
		t.Fatal("unknown field source member was accepted")
	}
	if err := strictDecode([]byte(`{} {}`), &sources); err == nil ||
		errors.Is(err, coachingprofile.ErrNotFound) {
		t.Fatalf("trailing JSON error = %v", err)
	}
}
