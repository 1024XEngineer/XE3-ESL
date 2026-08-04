package voice

import "testing"

func TestCandidateCleanupClaimAllowsConfirmedMessageAudio(t *testing.T) {
	claim := CleanupClaim{
		Kind:         CleanupCandidate,
		OwnerID:      "10000000-0000-4000-8000-000000000001",
		CandidateID:  "20000000-0000-4000-8000-000000000001",
		AudioID:      "30000000-0000-4000-8000-000000000001",
		ObjectKey:    "audio/v1/agent/confirmed-input.wav",
		FencingToken: 1,
	}
	if !claim.Valid() {
		t.Fatalf("confirmed candidate cleanup claim = %#v, want valid", claim)
	}
}
