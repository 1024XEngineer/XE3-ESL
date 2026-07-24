package identity

import (
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyResolverUsesHeadersOnlyForTrustedImmediatePeer(t *testing.T) {
	resolver, err := NewTrustedProxyResolver([]string{
		"10.0.0.0/8",
		"2001:db8:ffff::/48",
	})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		xff        string
		want       string
	}{
		{
			name:       "direct ignores forged header",
			remoteAddr: "192.0.2.44:1234",
			xff:        "203.0.113.9",
			want:       "192.0.2.44",
		},
		{
			name:       "trusted proxy xff",
			remoteAddr: "10.0.0.2:443",
			xff:        "203.0.113.9, 10.0.0.1",
			want:       "203.0.113.9",
		},
		{
			name:       "forwarded takes precedence and normalizes IPv6",
			remoteAddr: "[2001:db8:ffff::1]:443",
			forwarded:  `for="[2001:db8::1234]";proto=https`,
			xff:        "203.0.113.9",
			want:       "2001:db8::1234",
		},
		{
			name:       "malformed trusted header falls back to peer",
			remoteAddr: "10.0.0.2:443",
			forwarded:  "for=unknown",
			xff:        "203.0.113.9",
			want:       "10.0.0.2",
		},
		{
			name:       "malformed xff falls back to peer",
			remoteAddr: "10.0.0.2:443",
			xff:        "unknown",
			want:       "10.0.0.2",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.remoteAddr
			if test.forwarded != "" {
				request.Header.Set("Forwarded", test.forwarded)
			}
			if test.xff != "" {
				request.Header.Set("X-Forwarded-For", test.xff)
			}
			if got := resolver.Resolve(request); got != test.want {
				t.Fatalf("source IP = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTrustedProxyResolverRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewTrustedProxyResolver([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected invalid CIDR to fail startup")
	}
}
