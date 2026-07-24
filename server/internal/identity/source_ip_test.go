package identity

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrustedProxyResolverDefaultsToRemoteAddr(t *testing.T) {
	resolver, err := NewTrustedProxyResolver(nil, "")
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("Forwarded", "for=198.51.100.7")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := resolver.Resolve(request); got != "10.0.0.2" {
		t.Fatalf("default source IP = %q", got)
	}
}

func TestXForwardedForStrategyIgnoresClientForwardedHeader(t *testing.T) {
	resolver := mustTrustedProxyResolver(
		t,
		[]string{"10.0.0.0/8"},
		"x-forwarded-for",
	)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("Forwarded", "for=198.51.100.7")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := resolver.Resolve(request); got != "203.0.113.9" {
		t.Fatalf("forged Forwarded overrode configured XFF: %q", got)
	}
}

func TestXForwardedForStrategyPeelsTrustedChainFromRight(t *testing.T) {
	resolver := mustTrustedProxyResolver(
		t,
		[]string{"10.0.0.0/8", "2001:db8:ffff::/48"},
		"x-forwarded-for",
	)
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{
			name:       "untrusted direct peer ignores header",
			remoteAddr: "192.0.2.44:1234",
			xff:        "203.0.113.9",
			want:       "192.0.2.44",
		},
		{
			name:       "unknown client prefix is ignored",
			remoteAddr: "10.0.0.2:443",
			xff:        "unknown, 203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "garbage left of source and trusted chain is ignored",
			remoteAddr: "10.0.0.3:443",
			xff:        "garbage, 203.0.113.9, 10.0.0.2",
			want:       "203.0.113.9",
		},
		{
			name:       "all trusted chain returns leftmost",
			remoteAddr: "10.0.0.3:443",
			xff:        "10.0.0.1, 10.0.0.2",
			want:       "10.0.0.1",
		},
		{
			name:       "malformed required right segment fails closed",
			remoteAddr: "10.0.0.3:443",
			xff:        "203.0.113.9, malformed, 10.0.0.2",
			want:       "10.0.0.3",
		},
		{
			name:       "IPv6 source is normalized",
			remoteAddr: "[2001:db8:ffff::1]:443",
			xff:        "2001:db8::1234",
			want:       "2001:db8::1234",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.xff)
			if got := resolver.Resolve(request); got != test.want {
				t.Fatalf("source IP = %q, want %q", got, test.want)
			}
		})
	}
}

func TestXForwardedForStrategyPreservesMultipleHeaderLineOrder(t *testing.T) {
	resolver := mustTrustedProxyResolver(
		t,
		[]string{"10.0.0.0/8"},
		"x-forwarded-for",
	)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.3:443"
	request.Header.Add("X-Forwarded-For", "unknown, 203.0.113.9")
	request.Header.Add("X-Forwarded-For", "10.0.0.2")
	if got := resolver.Resolve(request); got != "203.0.113.9" {
		t.Fatalf("multiple-line XFF source IP = %q", got)
	}
}

func TestForwardedStrategyIgnoresXForwardedFor(t *testing.T) {
	resolver := mustTrustedProxyResolver(
		t,
		[]string{"10.0.0.0/8"},
		"forwarded",
	)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("Forwarded", `for="[2001:db8::1234]";proto=https`)
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := resolver.Resolve(request); got != "2001:db8::1234" {
		t.Fatalf("configured Forwarded source IP = %q", got)
	}
}

func TestTrustedProxyResolverRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name   string
		cidrs  []string
		header string
	}{
		{name: "CIDR", cidrs: []string{"not-a-cidr"}},
		{name: "header", cidrs: []string{"10.0.0.0/8"}, header: "both"},
		{name: "header without CIDR", header: "x-forwarded-for"},
		{
			name:  "CIDR without header",
			cidrs: []string{"10.0.0.0/8"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewTrustedProxyResolver(test.cidrs, test.header)
			if err == nil {
				t.Fatal("expected invalid trusted proxy configuration")
			}
			if strings.Contains(err.Error(), "10.0.0.0/8") ||
				strings.Contains(err.Error(), test.header) && test.header != "" {
				t.Fatalf("configuration error leaked input: %v", err)
			}
		})
	}
}

func TestTrustedProxyResolverAcceptsCompleteModes(t *testing.T) {
	for _, test := range []struct {
		name   string
		cidrs  []string
		header string
	}{
		{name: "direct"},
		{
			name:   "proxy",
			cidrs:  []string{"10.0.0.0/8"},
			header: "x-forwarded-for",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTrustedProxyResolver(
				test.cidrs,
				test.header,
			); err != nil {
				t.Fatalf("complete configuration rejected: %v", err)
			}
		})
	}
}

func mustTrustedProxyResolver(
	t *testing.T,
	cidrs []string,
	header string,
) *TrustedProxyResolver {
	t.Helper()
	resolver, err := NewTrustedProxyResolver(cidrs, header)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return resolver
}
