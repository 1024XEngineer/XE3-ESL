package conversation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const audioAssetRandomIDBytes = 16

// SecureAudioAssetIDGenerator creates opaque, unguessable identifiers for
// AudioAsset metadata and the corresponding private object key.
type SecureAudioAssetIDGenerator struct{}

var _ AudioAssetIDGenerator = SecureAudioAssetIDGenerator{}

func (SecureAudioAssetIDGenerator) NewID() (string, error) {
	random := make([]byte, audioAssetRandomIDBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate audio asset ID: %w", err)
	}
	return "audio_" + hex.EncodeToString(random), nil
}
