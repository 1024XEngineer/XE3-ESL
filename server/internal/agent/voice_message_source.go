package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const voiceObjectReadTimeout = 30 * time.Second

// SignedVoiceAudioLoader reads a private object through a short-lived server
// capability and recreates a validated provider source. Signed URLs and object
// bytes remain inside the server.
type SignedVoiceAudioLoader struct {
	store            objectstore.Store
	client           *http.Client
	scratchDirectory string
	allowedHosts     map[string]struct{}
	resolver         voiceHostResolver
	now              func() time.Time
}

type voiceHostResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func NewSignedVoiceAudioLoader(
	store objectstore.Store,
	client *http.Client,
	scratchDirectory string,
	allowedHosts []string,
) (*SignedVoiceAudioLoader, error) {
	return newSignedVoiceAudioLoader(
		store,
		client,
		scratchDirectory,
		allowedHosts,
		net.DefaultResolver,
	)
}

func newSignedVoiceAudioLoader(
	store objectstore.Store,
	client *http.Client,
	scratchDirectory string,
	allowedHosts []string,
	resolver voiceHostResolver,
) (*SignedVoiceAudioLoader, error) {
	if nilVoiceDependency(store) {
		return nil, errors.New("agent: voice object store is required")
	}
	if client == nil {
		client = &http.Client{
			Timeout: voiceObjectReadTimeout,
		}
	}
	if client.Timeout <= 0 || resolver == nil {
		return nil, errors.New("agent: voice object client timeout is required")
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	trustedHosts := make(map[string]struct{}, len(allowedHosts))
	for _, allowedHost := range allowedHosts {
		host, ok := canonicalVoiceAllowedHost(allowedHost)
		if !ok {
			return nil, errors.New(
				"agent: voice object allowed host is invalid",
			)
		}
		trustedHosts[host] = struct{}{}
	}
	if len(trustedHosts) == 0 {
		return nil, errors.New(
			"agent: voice object allowed host is required",
		)
	}
	if safeClient.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = voiceObjectDialContext(
			trustedHosts,
			resolver,
		)
		safeClient.Transport = transport
	}
	return &SignedVoiceAudioLoader{
		store:            store,
		client:           &safeClient,
		scratchDirectory: strings.TrimSpace(scratchDirectory),
		allowedHosts:     trustedHosts,
		resolver:         resolver,
		now:              func() time.Time { return time.Now().UTC() },
	}, nil
}

func voiceObjectDialContext(
	allowedHosts map[string]struct{},
	resolver voiceHostResolver,
) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return func(
		ctx context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !trustedVoiceDialHost(allowedHosts, host, port) {
			return nil, ErrRepository
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrRepository
		}
		for _, resolved := range addresses {
			if !publicVoiceObjectAddress(resolved.IP) {
				return nil, ErrRepository
			}
		}
		var dialErr error
		for _, resolved := range addresses {
			connection, err := dialer.DialContext(
				ctx,
				network,
				net.JoinHostPort(resolved.IP.String(), port),
			)
			if err == nil {
				return connection, nil
			}
			dialErr = err
		}
		if dialErr != nil {
			return nil, dialErr
		}
		return nil, ErrRepository
	}
}

func trustedVoiceDialHost(
	allowedHosts map[string]struct{},
	hostname string,
	port string,
) bool {
	hostname = strings.ToLower(hostname)
	if port == "443" {
		if _, trusted := allowedHosts[hostname]; trusted {
			return true
		}
	}
	hostPort := net.JoinHostPort(hostname, port)
	_, trusted := allowedHosts[strings.ToLower(hostPort)]
	return trusted
}

func (loader *SignedVoiceAudioLoader) LoadVoiceAudio(
	ctx context.Context,
	candidate VoiceCandidate,
) (platformmedia.ManagedAudioSource, error) {
	if ctx == nil ||
		candidate.ObjectKey == "" ||
		candidate.ContentType != platformmedia.ContentTypeWAV ||
		candidate.Size <= 0 ||
		candidate.ChecksumSHA256 == "" {
		return nil, ErrInvalidRequest
	}
	signed, err := loader.store.SignedGet(ctx, candidate.ObjectKey)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(signed.URL)
	now := loader.now()
	if err != nil ||
		target.Scheme != "https" ||
		target.Host == "" ||
		target.User != nil ||
		target.Fragment != "" ||
		signed.ExpiresAt.IsZero() ||
		!signed.ExpiresAt.After(now) ||
		signed.ExpiresAt.After(now.Add(defaultVoicePlaybackTTL)) {
		return nil, ErrRepository
	}
	if err := loader.validateSignedTarget(ctx, target); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		target.String(),
		nil,
	)
	if err != nil {
		return nil, ErrRepository
	}
	request.Header.Set("Accept", platformmedia.ContentTypeWAV)
	response, err := loader.client.Do(request)
	if err != nil {
		return nil, ErrRepository
	}
	if response == nil || response.Body == nil {
		return nil, ErrRepository
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrRepository
	}
	body := io.LimitReader(response.Body, platformmedia.MaxAudioBytes+1)
	audio, err := platformmedia.CaptureTemporaryAudio(
		loader.scratchDirectory,
		response.Header.Get("Content-Type"),
		body,
	)
	if err != nil {
		return nil, ErrRepository
	}
	checksum, err := voiceAudioChecksum(audio)
	if err != nil ||
		!sameVoiceUpload(candidate, audio, checksum) {
		_ = audio.Close()
		return nil, ErrRepository
	}
	return audio, nil
}

func (loader *SignedVoiceAudioLoader) validateSignedTarget(
	ctx context.Context,
	target *url.URL,
) error {
	if loader == nil || ctx == nil || target == nil ||
		target.User != nil || target.Fragment != "" ||
		!strings.EqualFold(target.Scheme, "https") {
		return ErrRepository
	}
	host, ok := canonicalVoiceAllowedHost(target.Host)
	if !ok {
		return ErrRepository
	}
	if _, trusted := loader.allowedHosts[host]; !trusted {
		return ErrRepository
	}
	addresses, err := loader.resolver.LookupIPAddr(
		ctx,
		target.Hostname(),
	)
	if err != nil || len(addresses) == 0 {
		return ErrRepository
	}
	for _, address := range addresses {
		if !publicVoiceObjectAddress(address.IP) {
			return ErrRepository
		}
	}
	return nil
}

func canonicalVoiceAllowedHost(raw string) (string, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw ||
		strings.ContainsAny(raw, "/?#") {
		return "", false
	}
	parsed, err := url.Parse("https://" + raw)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", false
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.HasSuffix(hostname, ".") ||
		strings.ContainsAny(hostname, "\r\n\t ") {
		return "", false
	}
	if port := parsed.Port(); port != "" {
		number, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || number == 0 {
			return "", false
		}
	}
	return strings.ToLower(parsed.Host), true
}

func publicVoiceObjectAddress(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() ||
		address.IsPrivate() ||
		address.IsLoopback() ||
		address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() ||
		address.IsMulticast() ||
		address.IsUnspecified() {
		return false
	}
	for _, prefix := range nonPublicVoiceObjectPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var nonPublicVoiceObjectPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"),
}

var _ VoiceAudioSourceLoader = (*SignedVoiceAudioLoader)(nil)
