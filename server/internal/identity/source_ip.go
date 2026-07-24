package identity

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

type SourceIPResolver interface {
	Resolve(*http.Request) string
}

type directSourceIPResolver struct{}

func (directSourceIPResolver) Resolve(request *http.Request) string {
	address, ok := parseIP(request.RemoteAddr)
	if !ok {
		return request.RemoteAddr
	}
	return address.String()
}

type TrustedProxyResolver struct {
	trusted []netip.Prefix
}

func NewTrustedProxyResolver(cidrs []string) (*TrustedProxyResolver, error) {
	trusted := make([]netip.Prefix, 0, len(cidrs))
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, errors.New("identity: invalid trusted proxy CIDR")
		}
		trusted = append(trusted, prefix.Masked())
	}
	return &TrustedProxyResolver{trusted: trusted}, nil
}

func (r *TrustedProxyResolver) Resolve(request *http.Request) string {
	peer, ok := parseIP(request.RemoteAddr)
	if !ok || !r.isTrusted(peer) {
		return directSourceIPResolver{}.Resolve(request)
	}

	var chain []netip.Addr
	if forwarded := request.Header.Values("Forwarded"); len(forwarded) > 0 {
		chain, ok = forwardedChain(forwarded)
		if !ok {
			return peer.String()
		}
	} else {
		chain, ok = xForwardedForChain(request.Header.Values("X-Forwarded-For"))
	}
	if !ok || len(chain) == 0 {
		return peer.String()
	}
	chain = append(chain, peer)
	for index := len(chain) - 1; index >= 0; index-- {
		if !r.isTrusted(chain[index]) {
			return chain[index].String()
		}
	}
	return chain[0].String()
}

func (r *TrustedProxyResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func forwardedChain(values []string) ([]netip.Addr, bool) {
	if len(values) == 0 {
		return nil, true
	}
	var result []netip.Addr
	for _, value := range values {
		for _, element := range strings.Split(value, ",") {
			var found bool
			for _, parameter := range strings.Split(element, ";") {
				name, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if !ok || !strings.EqualFold(name, "for") {
					continue
				}
				if found {
					return nil, false
				}
				found = true
				raw = strings.TrimSpace(raw)
				if strings.HasPrefix(raw, `"`) {
					unquoted, err := strconv.Unquote(raw)
					if err != nil {
						return nil, false
					}
					raw = unquoted
				}
				address, ok := parseIP(raw)
				if !ok {
					return nil, false
				}
				result = append(result, address)
			}
			if !found {
				return nil, false
			}
		}
	}
	return result, true
}

func xForwardedForChain(values []string) ([]netip.Addr, bool) {
	var result []netip.Addr
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			address, ok := parseIP(strings.TrimSpace(raw))
			if !ok {
				return nil, false
			}
			result = append(result, address)
		}
	}
	return result, true
}

func parseIP(raw string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}
