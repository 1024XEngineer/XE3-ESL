package voice

import (
	"regexp"
	"testing"
)

func TestSecureAudioAssetIDGeneratorProducesOpaqueUniqueIDs(t *testing.T) {
	t.Parallel()

	generator := SecureAudioAssetIDGenerator{}
	pattern := regexp.MustCompile(`^audio_[0-9a-f]{32}$`)
	seen := make(map[string]struct{}, 256)
	for range 256 {
		id, err := generator.NewID()
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("NewID() = %q, want opaque audio ID", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("NewID() produced duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}
