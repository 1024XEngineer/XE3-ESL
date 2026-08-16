package context

import (
	"strings"
	"testing"
)

func TestSelectCoachingProfileContextSelectsNonEmptyCore(t *testing.T) {
	contribution := CoachingProfileContribution{
		Content: "<coaching_user_profile>{&quot;form_of_address&quot;:&quot;Alex&quot;}</coaching_user_profile>.",
		Enabled: true,
		Version: 4,
	}
	if !contribution.Valid() {
		t.Fatal("non-empty core contribution must be valid")
	}
	content, status := selectCoachingProfileContext(
		"system",
		contribution,
		4096,
	)
	if status != coachingProfileContextSelected ||
		!strings.Contains(content, "form_of_address") ||
		!strings.HasPrefix(content, "system ") {
		t.Fatalf("content=%q status=%q", content, status)
	}
}
