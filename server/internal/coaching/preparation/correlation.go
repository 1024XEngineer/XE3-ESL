package preparation

import (
	"crypto/rand"
	"encoding/hex"
)

func newPreparationCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_preparation_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}

// NewCorrelationID supplies opaque error correlation IDs to transport
// adapters without exposing generator details.
func NewCorrelationID() string {
	return newPreparationCorrelationID()
}
