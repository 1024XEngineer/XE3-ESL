package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"regexp"
)

const sessionTokenEntropyBytes = 32

var sessionTokenPattern = regexp.MustCompile(`^sess_[A-Za-z0-9._~+/-]+={0,}$`)

type OpaqueSessionTokens struct {
	random io.Reader
}

func NewOpaqueSessionTokens(random io.Reader) *OpaqueSessionTokens {
	if random == nil {
		random = rand.Reader
	}
	return &OpaqueSessionTokens{random: random}
}

func (t *OpaqueSessionTokens) Generate() (string, []byte, error) {
	entropy := make([]byte, sessionTokenEntropyBytes)
	if _, err := io.ReadFull(t.random, entropy); err != nil {
		return "", nil, err
	}
	raw := "sess_" + base64.RawURLEncoding.EncodeToString(entropy)
	return raw, t.Digest(raw), nil
}

func (*OpaqueSessionTokens) Digest(raw string) []byte {
	digest := sha256.Sum256([]byte(raw))
	return digest[:]
}

func (*OpaqueSessionTokens) ValidWireFormat(raw string) bool {
	return len(raw) >= 6 && len(raw) <= 512 && sessionTokenPattern.MatchString(raw)
}
