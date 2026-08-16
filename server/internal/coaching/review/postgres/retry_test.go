package postgres

import "testing"

func TestScopedRequestIDIsStableAndFeedbackItemScoped(t *testing.T) {
	first := scopedRequestID(
		"10000000-0000-4000-8000-000000000001",
		"mobile-request-1",
	)
	replay := scopedRequestID(
		"10000000-0000-4000-8000-000000000001",
		"mobile-request-1",
	)
	otherItem := scopedRequestID(
		"10000000-0000-4000-8000-000000000002",
		"mobile-request-1",
	)
	if first != replay {
		t.Fatalf("same feedback item replay changed request ID: %q != %q", first, replay)
	}
	if first == otherItem {
		t.Fatalf("different feedback items shared request ID: %q", first)
	}
	if len(first) > 128 {
		t.Fatalf("scoped request ID is too long: %d", len(first))
	}
}
