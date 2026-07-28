package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
)

type voiceHostResolverFunc func(
	context.Context,
	string,
) ([]net.IPAddr, error)

func (resolve voiceHostResolverFunc) LookupIPAddr(
	ctx context.Context,
	host string,
) ([]net.IPAddr, error) {
	return resolve(ctx, host)
}

type voiceRoundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip voiceRoundTripperFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return roundTrip(request)
}

func TestSignedVoiceAudioLoaderRequiresExplicitHostAndPublicResolution(
	t *testing.T,
) {
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSignedVoiceAudioLoader(
		store,
		&http.Client{Timeout: time.Second},
		t.TempDir(),
		nil,
	); err == nil {
		t.Fatal("loader accepted an empty signed-URL host allowlist")
	}

	tests := []struct {
		name      string
		url       string
		allowed   string
		addresses []net.IPAddr
	}{
		{
			name:      "host outside allowlist",
			url:       "https://attacker.example/audio.wav",
			allowed:   "trusted.example",
			addresses: publicVoiceTestAddress(),
		},
		{
			name:      "userinfo",
			url:       "https://user@trusted.example/audio.wav",
			allowed:   "trusted.example",
			addresses: publicVoiceTestAddress(),
		},
		{
			name:      "loopback",
			url:       "https://trusted.example/audio.wav",
			allowed:   "trusted.example",
			addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
		},
		{
			name:      "private",
			url:       "https://trusted.example/audio.wav",
			allowed:   "trusted.example",
			addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.9")}},
		},
		{
			name:      "link local",
			url:       "https://trusted.example/audio.wav",
			allowed:   "trusted.example",
			addresses: []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			loader := newVoiceSourceTestLoader(
				t,
				store,
				testCase.url,
				testCase.allowed,
				testCase.addresses,
				func(*http.Request) (*http.Response, error) {
					requests.Add(1)
					return nil, context.Canceled
				},
			)
			if _, err := loader.LoadVoiceAudio(
				context.Background(),
				voiceSourceTestCandidate(),
			); err != ErrRepository {
				t.Fatalf("LoadVoiceAudio() error = %v", err)
			}
			if requests.Load() != 0 {
				t.Fatal("unsafe signed URL reached the HTTP transport")
			}
		})
	}
}

func TestSignedVoiceAudioLoaderAllowsExactPublicHostAndRejectsRedirect(
	t *testing.T,
) {
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	audio := voiceTestWAV(0x62)
	success := newVoiceSourceTestLoader(
		t,
		store,
		"https://trusted.example/audio.wav?signature=opaque",
		"trusted.example",
		publicVoiceTestAddress(),
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"audio/wav"},
				},
				Body: io.NopCloser(bytes.NewReader(audio)),
			}, nil
		},
	)
	source, err := success.LoadVoiceAudio(
		context.Background(),
		voiceSourceTestCandidate(),
	)
	if err != nil {
		t.Fatalf("LoadVoiceAudio() error = %v", err)
	}
	_ = source.Close()

	var requests atomic.Int32
	redirect := newVoiceSourceTestLoader(
		t,
		store,
		"https://trusted.example/audio.wav",
		"trusted.example",
		publicVoiceTestAddress(),
		func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusFound,
				Header: http.Header{
					"Location": []string{
						"https://169.254.169.254/latest/meta-data",
					},
				},
				Body: io.NopCloser(bytes.NewReader(nil)),
			}, nil
		},
	)
	if _, err := redirect.LoadVoiceAudio(
		context.Background(),
		voiceSourceTestCandidate(),
	); err != ErrRepository {
		t.Fatalf("redirect LoadVoiceAudio() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("redirect transport calls = %d, want 1", requests.Load())
	}
}

func newVoiceSourceTestLoader(
	t *testing.T,
	store *objectfake.Store,
	signedURL string,
	allowedHost string,
	addresses []net.IPAddr,
	roundTrip voiceRoundTripperFunc,
) *SignedVoiceAudioLoader {
	t.Helper()
	loader, err := newSignedVoiceAudioLoader(
		signedVoiceStore{
			Store: store,
			result: objectstore.SignedGetResult{
				URL:       signedURL,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		},
		&http.Client{
			Timeout:   time.Second,
			Transport: roundTrip,
		},
		t.TempDir(),
		[]string{allowedHost},
		voiceHostResolverFunc(func(
			context.Context,
			string,
		) ([]net.IPAddr, error) {
			return addresses, nil
		}),
	)
	if err != nil {
		t.Fatalf("newSignedVoiceAudioLoader() error = %v", err)
	}
	return loader
}

func voiceSourceTestCandidate() VoiceCandidate {
	audio := voiceTestWAV(0x62)
	checksum := sha256.Sum256(audio)
	return VoiceCandidate{
		ObjectKey:      "audio/v1/agent/test.wav",
		ContentType:    "audio/wav",
		Size:           int64(len(audio)),
		ChecksumSHA256: hex.EncodeToString(checksum[:]),
		Duration:       100 * time.Millisecond,
		SampleRate:     16_000,
	}
}

func publicVoiceTestAddress() []net.IPAddr {
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}
}
