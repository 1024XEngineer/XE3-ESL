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

type trustedProxyHeader uint8

const (
	trustedProxyHeaderNone trustedProxyHeader = iota
	trustedProxyHeaderXForwardedFor
	trustedProxyHeaderForwarded
)

type TrustedProxyResolver struct {
	trusted []netip.Prefix
	header  trustedProxyHeader
}

func NewTrustedProxyResolver(
	cidrs []string,
	headerName string,
) (*TrustedProxyResolver, error) {
	trusted := make([]netip.Prefix, 0, len(cidrs))
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, errors.New("identity: invalid trusted proxy CIDR")
		}
		trusted = append(trusted, prefix.Masked())
	}

	var header trustedProxyHeader
	switch strings.ToLower(strings.TrimSpace(headerName)) {
	case "":
		header = trustedProxyHeaderNone
	case "x-forwarded-for":
		header = trustedProxyHeaderXForwardedFor
	case "forwarded":
		header = trustedProxyHeaderForwarded
	default:
		return nil, errors.New("identity: invalid trusted proxy header")
	}
	if (header == trustedProxyHeaderNone) != (len(trusted) == 0) {
		return nil, errors.New("identity: incomplete trusted proxy configuration")
	}
	return &TrustedProxyResolver{trusted: trusted, header: header}, nil
}

func (r *TrustedProxyResolver) Resolve(request *http.Request) string {
	peer, ok := parseIP(request.RemoteAddr)
	if !ok || !r.isTrusted(peer) || r.header == trustedProxyHeaderNone {
		return directSourceIPResolver{}.Resolve(request)
	}

	switch r.header {
	case trustedProxyHeaderXForwardedFor:
		return r.resolveXForwardedFor(
			peer,
			request.Header.Values("X-Forwarded-For"),
		)
	case trustedProxyHeaderForwarded:
		return r.resolveForwarded(peer, request.Header.Values("Forwarded"))
	default:
		return peer.String()
	}
}

// resolveXForwardedFor peels the proxy-appended chain from right to left.
// Once the first valid non-trusted address is found, client-controlled values
// further left are irrelevant and are deliberately not parsed.
func (r *TrustedProxyResolver) resolveXForwardedFor(
	peer netip.Addr,
	values []string,
) string {
	elements := commaSeparatedElements(values)
	if len(elements) == 0 {
		return peer.String()
	}
	var leftmost netip.Addr
	for index := len(elements) - 1; index >= 0; index-- {
		address, ok := parseIP(strings.TrimSpace(elements[index]))
		if !ok {
			return peer.String()
		}
		leftmost = address
		if !r.isTrusted(address) {
			return address.String()
		}
	}
	return leftmost.String()
}

func (r *TrustedProxyResolver) resolveForwarded(
	peer netip.Addr,
	values []string,
) string {
	elements := commaSeparatedElements(values)
	if len(elements) == 0 {
		return peer.String()
	}
	var leftmost netip.Addr
	for index := len(elements) - 1; index >= 0; index-- {
		address, ok := forwardedAddress(elements[index])
		if !ok {
			return peer.String()
		}
		leftmost = address
		if !r.isTrusted(address) {
			return address.String()
		}
	}
	return leftmost.String()
}

func commaSeparatedElements(values []string) []string {
	var elements []string
	for _, value := range values {
		elements = append(elements, strings.Split(value, ",")...)
	}
	return elements
}

func forwardedAddress(element string) (netip.Addr, bool) {
	var rawAddress string
	found := false
	for _, parameter := range strings.Split(element, ";") {
		name, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if !ok || !strings.EqualFold(name, "for") {
			continue
		}
		if found {
			return netip.Addr{}, false
		}
		found = true
		rawAddress = strings.TrimSpace(raw)
	}
	if !found || rawAddress == "" {
		return netip.Addr{}, false
	}
	if strings.HasPrefix(rawAddress, `"`) {
		unquoted, err := strconv.Unquote(rawAddress)
		if err != nil {
			return netip.Addr{}, false
		}
		rawAddress = unquoted
	}
	return parseIP(rawAddress)
}

func (r *TrustedProxyResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
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
