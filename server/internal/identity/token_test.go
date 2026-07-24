package identity

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestOpaqueSessionTokenIsDeterministicWithInjectedRandomSource(t *testing.T) {
	tokens := NewOpaqueSessionTokens(bytes.NewReader(make([]byte, 32)))
	raw, digest, err := tokens.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if raw != "sess_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("unexpected token: %q", raw)
	}
	if hex.EncodeToString(digest) !=
		"7f283130533b1378553599db97a56e061fffae72d28af83a02366530113ae5a2" {
		t.Fatalf("unexpected digest: %x", digest)
	}
	if !tokens.ValidWireFormat(raw) {
		t.Fatal("generated token does not satisfy Bearer wire format")
	}
}

func TestOpaqueSessionTokenRejectsUnsafeWireValues(t *testing.T) {
	tokens := NewOpaqueSessionTokens(nil)
	for _, raw := range []string{
		"",
		"abc123==",
		"sess_",
		"sess_contains space",
		"sess_line\nbreak",
		"sess_padding=inside",
	} {
		if tokens.ValidWireFormat(raw) {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}
