package voice_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/gin-gonic/gin"
)

type audioAssetHTTPServiceStub struct {
	playbackResult objectstore.SignedGetResult
	playbackErr    error
	deleteResult   practiceinput.AudioAsset
	deleteErr      error
	playbackCalls  []audioAssetHTTPCall
	deleteCalls    []audioAssetHTTPCall
}

type audioAssetHTTPCall struct {
	actor   practiceinput.AudioAssetActor
	assetID string
}

func (stub *audioAssetHTTPServiceStub) Playback(
	_ context.Context,
	actor practiceinput.AudioAssetActor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	stub.playbackCalls = append(stub.playbackCalls, audioAssetHTTPCall{
		actor:   actor,
		assetID: assetID,
	})
	return stub.playbackResult, stub.playbackErr
}

func (stub *audioAssetHTTPServiceStub) Delete(
	_ context.Context,
	actor practiceinput.AudioAssetActor,
	assetID string,
) (practiceinput.AudioAsset, error) {
	stub.deleteCalls = append(stub.deleteCalls, audioAssetHTTPCall{
		actor:   actor,
		assetID: assetID,
	})
	return stub.deleteResult, stub.deleteErr
}

func TestAudioAssetRoutesRequireAuthenticatedActor(t *testing.T) {
	service := &audioAssetHTTPServiceStub{}
	router := newAudioAssetHTTPRouter(t, service, func(*http.Request) (
		practiceinput.AudioAssetActor,
		bool,
	) {
		return practiceinput.AudioAssetActor{}, false
	})

	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/v1/audio-assets/asset-1/playback"},
		{method: http.MethodDelete, path: "/v1/audio-assets/asset-1"},
	} {
		t.Run(test.method, func(t *testing.T) {
			response := serveAudioAssetRequest(router, test.method, test.path)

			assertAudioAssetHTTPError(
				t,
				response,
				http.StatusUnauthorized,
				"authentication_required",
				"Authentication is required.",
			)
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
		})
	}

	if len(service.playbackCalls) != 0 || len(service.deleteCalls) != 0 {
		t.Fatal("unauthenticated request reached application service")
	}
}

func TestAudioAssetRoutesRejectIdentifierOutsideContractBeforeService(t *testing.T) {
	for _, route := range []struct {
		name   string
		method string
		suffix string
	}{
		{
			name:   "playback",
			method: http.MethodGet,
			suffix: "/playback",
		},
		{
			name:   "delete",
			method: http.MethodDelete,
		},
	} {
		t.Run(route.name, func(t *testing.T) {
			for name, encodedID := range map[string]string{
				"over 128 bytes": strings.Repeat("a", 129),
				"multibyte over 128 bytes": strings.Repeat(
					"界",
					43,
				),
				"leading whitespace":  "%20asset-1",
				"trailing whitespace": "asset-1%20",
			} {
				t.Run(name, func(t *testing.T) {
					service := &audioAssetHTTPServiceStub{}
					router := newAudioAssetHTTPRouter(t, service, trustedAlice)

					response := serveAudioAssetRequest(
						router,
						route.method,
						"/v1/audio-assets/"+encodedID+route.suffix,
					)

					assertAudioAssetHTTPError(
						t,
						response,
						http.StatusBadRequest,
						"invalid_request",
						"Request validation failed.",
					)
					if len(service.playbackCalls) != 0 || len(service.deleteCalls) != 0 {
						t.Fatal("invalid request reached application service")
					}
				})
			}
		})
	}
}

func TestAudioAssetRoutesAcceptExact128ByteIdentifier(t *testing.T) {
	const exactIDBytes = 128
	assetID := strings.Repeat("a", exactIDBytes)
	service := &audioAssetHTTPServiceStub{
		deleteResult: practiceinput.AudioAsset{
			ID:     assetID,
			Status: practiceinput.AudioAssetDeleted,
		},
	}
	router := newAudioAssetHTTPRouter(t, service, trustedAlice)

	response := serveAudioAssetRequest(
		router,
		http.MethodDelete,
		"/v1/audio-assets/"+assetID,
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body)
	}
	if len(service.deleteCalls) != 1 ||
		service.deleteCalls[0].assetID != assetID {
		t.Fatalf("delete calls = %#v", service.deleteCalls)
	}
}

func TestAudioAssetRoutesAuthenticateBeforeValidatingIdentifier(t *testing.T) {
	service := &audioAssetHTTPServiceStub{}
	router := newAudioAssetHTTPRouter(t, service, func(*http.Request) (
		practiceinput.AudioAssetActor,
		bool,
	) {
		return practiceinput.AudioAssetActor{}, false
	})

	response := serveAudioAssetRequest(
		router,
		http.MethodDelete,
		"/v1/audio-assets/"+strings.Repeat("a", 129),
	)

	assertAudioAssetHTTPError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
		"Authentication is required.",
	)
}

func TestAudioAssetRoutesRejectNonCanonicalTrustedActor(t *testing.T) {
	service := &audioAssetHTTPServiceStub{}
	router := newAudioAssetHTTPRouter(t, service, func(*http.Request) (
		practiceinput.AudioAssetActor,
		bool,
	) {
		return practiceinput.AudioAssetActor{UserID: " user-alice "}, true
	})

	response := serveAudioAssetRequest(
		router,
		http.MethodDelete,
		"/v1/audio-assets/asset-1",
	)

	assertAudioAssetHTTPError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
		"Authentication is required.",
	)
	if len(service.deleteCalls) != 0 {
		t.Fatal("non-canonical actor reached application service")
	}
}

func TestAudioAssetPlaybackUsesTrustedActorAndReturnsNoStoreCapability(t *testing.T) {
	expiresAt := time.Date(2026, 7, 25, 12, 2, 0, 0, time.UTC)
	service := &audioAssetHTTPServiceStub{
		playbackResult: objectstore.SignedGetResult{
			URL:       "https://media.example.invalid/signed?token=ephemeral",
			ExpiresAt: expiresAt,
		},
	}
	router := newAudioAssetHTTPRouter(t, service, trustedAlice)

	response := serveAudioAssetRequest(
		router,
		http.MethodGet,
		"/v1/audio-assets/asset-1/playback?owner_id=attacker",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		PlaybackURL string    `json:"playback_url"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.PlaybackURL != service.playbackResult.URL || !body.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("response = %#v", body)
	}
	if len(service.playbackCalls) != 1 {
		t.Fatalf("playback calls = %d, want 1", len(service.playbackCalls))
	}
	call := service.playbackCalls[0]
	if call.actor.UserID != "user-alice" || call.assetID != "asset-1" {
		t.Fatalf("application call = %#v", call)
	}
}

func TestAudioAssetPlaybackHidesAllUnavailableStatesAsNotFound(t *testing.T) {
	for name, serviceErr := range map[string]error{
		"missing":      practiceinput.ErrAudioAssetNotFound,
		"cross user":   practiceinput.ErrAudioAssetForbidden,
		"not readable": practiceinput.ErrAudioAssetInvalidTransition,
		"deleted":      practiceinput.ErrAudioAssetInvalidTransition,
		"malformed id": practiceinput.ErrAudioAssetInvalid,
	} {
		t.Run(name, func(t *testing.T) {
			service := &audioAssetHTTPServiceStub{playbackErr: serviceErr}
			router := newAudioAssetHTTPRouter(t, service, trustedAlice)

			response := serveAudioAssetRequest(
				router,
				http.MethodGet,
				"/v1/audio-assets/asset-hidden/playback",
			)

			assertAudioAssetHTTPError(
				t,
				response,
				http.StatusNotFound,
				"resource_not_found",
				"Resource was not found.",
			)
		})
	}
}

func TestAudioAssetPlaybackHidesInternalProviderDetailsAndIsRetryable(t *testing.T) {
	const sensitive = "https://bucket.internal/path?AccessKeySecret=must-not-leak"
	service := &audioAssetHTTPServiceStub{
		playbackErr: errors.Join(
			errors.New(sensitive),
			objectstore.ErrOperationFailed,
		),
	}
	router := newAudioAssetHTTPRouter(t, service, trustedAlice)

	response := serveAudioAssetRequest(
		router,
		http.MethodGet,
		"/v1/audio-assets/asset-1/playback",
	)

	assertAudioAssetHTTPError(
		t,
		response,
		http.StatusInternalServerError,
		"internal_error",
		"Internal server error.",
		true,
	)
	if strings.Contains(response.Body.String(), sensitive) ||
		strings.Contains(response.Body.String(), "AccessKeySecret") {
		t.Fatalf("sensitive provider detail leaked: %s", response.Body)
	}
}

func TestAudioAssetDeleteReturns204OnlyAfterDeletionCompletes(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		service := &audioAssetHTTPServiceStub{
			deleteResult: practiceinput.AudioAsset{
				ID:     "asset-1",
				Status: practiceinput.AudioAssetDeleted,
			},
		}
		router := newAudioAssetHTTPRouter(t, service, trustedAlice)

		response := serveAudioAssetRequest(
			router,
			http.MethodDelete,
			"/v1/audio-assets/asset-1?owner_id=attacker",
		)

		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body)
		}
		if response.Body.Len() != 0 {
			t.Fatalf("204 response body = %q, want empty", response.Body)
		}
		if len(service.deleteCalls) != 1 {
			t.Fatalf("delete calls = %d, want 1", len(service.deleteCalls))
		}
		call := service.deleteCalls[0]
		if call.actor.UserID != "user-alice" || call.assetID != "asset-1" {
			t.Fatalf("application call = %#v", call)
		}
	})

	t.Run("still deleting", func(t *testing.T) {
		service := &audioAssetHTTPServiceStub{
			deleteResult: practiceinput.AudioAsset{
				ID:     "asset-1",
				Status: practiceinput.AudioAssetDeleting,
			},
		}
		router := newAudioAssetHTTPRouter(t, service, trustedAlice)

		response := serveAudioAssetRequest(
			router,
			http.MethodDelete,
			"/v1/audio-assets/asset-1",
		)

		assertAudioAssetHTTPError(
			t,
			response,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)
	})
}

func TestAudioAssetDeleteHidesLookupAndProviderErrors(t *testing.T) {
	for name, test := range map[string]struct {
		serviceErr error
		status     int
		code       string
		message    string
		retryable  bool
	}{
		"missing": {
			serviceErr: practiceinput.ErrAudioAssetNotFound,
			status:     http.StatusNotFound,
			code:       "resource_not_found",
			message:    "Resource was not found.",
		},
		"cross user": {
			serviceErr: practiceinput.ErrAudioAssetForbidden,
			status:     http.StatusNotFound,
			code:       "resource_not_found",
			message:    "Resource was not found.",
		},
		"provider failure": {
			serviceErr: errors.Join(
				errors.New("oss endpoint and secret must not leak"),
				objectstore.ErrOperationFailed,
			),
			status:    http.StatusInternalServerError,
			code:      "internal_error",
			message:   "Internal server error.",
			retryable: true,
		},
		"concurrent lifecycle transition": {
			serviceErr: practiceinput.ErrAudioAssetConcurrentUpdate,
			status:     http.StatusConflict,
			code:       "resource_conflict",
			message:    "Resource state conflicts with this operation.",
			retryable:  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			service := &audioAssetHTTPServiceStub{deleteErr: test.serviceErr}
			router := newAudioAssetHTTPRouter(t, service, trustedAlice)

			response := serveAudioAssetRequest(
				router,
				http.MethodDelete,
				"/v1/audio-assets/asset-hidden",
			)

			assertAudioAssetHTTPError(
				t,
				response,
				test.status,
				test.code,
				test.message,
				test.retryable,
			)
			if strings.Contains(response.Body.String(), "oss endpoint") {
				t.Fatalf("service error leaked: %s", response.Body)
			}
		})
	}
}

func newAudioAssetHTTPRouter(
	t *testing.T,
	service practiceinput.AudioAssetHTTPService,
	resolve practiceinput.AudioAssetActorResolverFunc,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := practiceinput.RegisterAudioAssetRoutes(router, service, resolve); err != nil {
		t.Fatalf("RegisterAudioAssetRoutes() error = %v", err)
	}
	return router
}

func trustedAlice(*http.Request) (practiceinput.AudioAssetActor, bool) {
	return practiceinput.AudioAssetActor{UserID: "user-alice"}, true
}

func serveAudioAssetRequest(
	router http.Handler,
	method string,
	target string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertAudioAssetHTTPError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
	message string,
	retryable ...bool,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, status, response.Body)
	}
	var body struct {
		Error struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			Retryable     bool   `json:"retryable"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantRetryable := len(retryable) == 1 && retryable[0]
	if body.Error.Code != code ||
		body.Error.Message != message ||
		body.Error.Retryable != wantRetryable ||
		body.Error.CorrelationID == "" {
		t.Fatalf("error response = %#v", body)
	}
}
