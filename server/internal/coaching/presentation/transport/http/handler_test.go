package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	basehttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const httpUserID = "10000000-0000-4000-8000-000000000001"

var httpActor = requestcontext.Actor{UserID: httpUserID, SessionID: "session-1"}

func TestRoutesRequireAuthentication(t *testing.T) {
	router := presentationRouter(t, &applicationStub{}, false)
	for _, path := range []string{
		"/v1/coach-presentation-catalog",
		"/v1/me/coach-presentation",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(basehttp.MethodGet, path, nil))
		if response.Code != basehttp.StatusUnauthorized {
			t.Fatalf("GET %s status=%d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(
			basehttp.MethodPost,
			"/v1/coach-presentation/voices/voice_ava/previews",
			nil,
		),
	)
	if response.Code != basehttp.StatusUnauthorized {
		t.Fatalf("POST voice preview status=%d", response.Code)
	}
}

func TestCatalogDoesNotExposeProviderBindings(t *testing.T) {
	application := &applicationStub{catalog: httpCatalog()}
	response := performRequest(
		t, presentationRouter(t, application, true),
		basehttp.MethodGet, "/v1/coach-presentation-catalog", nil,
	)
	if response.Code != basehttp.StatusOK ||
		bytes.Contains(response.Body.Bytes(), []byte("provider")) ||
		bytes.Contains(response.Body.Bytes(), []byte("native-avatar-id")) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Avatars []presentation.AvatarOption `json:"avatars"`
		Voices  []presentation.VoiceOption  `json:"voices"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil ||
		len(body.Avatars) != 1 || len(body.Voices) != 1 {
		t.Fatalf("catalog body=%s", response.Body.String())
	}
}

func TestPatchRejectsUnknownNullAndProviderFields(t *testing.T) {
	tests := []string{
		`{"avatar_option_id":"avatar_lisa","voice_option_id":"voice_ava","expected_version":0,"provider_avatar_id":"forbidden"}`,
		`{"avatar_option_id":null,"voice_option_id":"voice_ava","expected_version":0}`,
		`{"avatar_option_id":"avatar_lisa","voice_option_id":"voice_ava","expected_version":null}`,
	}
	for _, body := range tests {
		application := &applicationStub{}
		response := performRequest(
			t, presentationRouter(t, application, true), basehttp.MethodPatch,
			"/v1/me/coach-presentation", []byte(body),
		)
		if response.Code != basehttp.StatusBadRequest || application.updateCalls != 0 {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestPatchMapsCommandAndVersionConflict(t *testing.T) {
	application := &applicationStub{updateErr: presentation.ErrVersionConflict}
	response := performRequest(
		t, presentationRouter(t, application, true), basehttp.MethodPatch,
		"/v1/me/coach-presentation",
		[]byte(`{"avatar_option_id":"avatar_nathan","voice_option_id":"voice_john","expected_version":4}`),
	)
	if response.Code != basehttp.StatusConflict || application.updateCalls != 1 ||
		application.command.AvatarOptionID != "avatar_nathan" ||
		application.command.VoiceOptionID != "voice_john" ||
		application.command.ExpectedVersion != 4 ||
		!bytes.Contains(response.Body.Bytes(), []byte("coach_presentation_version_conflict")) {
		t.Fatalf("status=%d command=%#v body=%s", response.Code, application.command, response.Body.String())
	}
}

func TestVoicePreviewStreamsPrivateWAVAndClosesAudio(t *testing.T) {
	audio := &managedAudioStub{bytes: []byte("RIFF-preview-wav")}
	application := &applicationStub{previewAudio: audio}
	response := performRequest(
		t,
		presentationRouter(t, application, true),
		basehttp.MethodPost,
		"/v1/coach-presentation/voices/voice_ava/previews",
		nil,
	)
	if response.Code != basehttp.StatusOK ||
		response.Header().Get("Content-Type") != platformmedia.ContentTypeWAV ||
		response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Body.String() != "RIFF-preview-wav" ||
		application.previewVoiceID != "voice_ava" || !audio.closed {
		t.Fatalf(
			"status=%d headers=%v body=%q voice=%q closed=%t",
			response.Code,
			response.Header(),
			response.Body.String(),
			application.previewVoiceID,
			audio.closed,
		)
	}
}

func TestVoicePreviewMapsMissingAndUnavailable(t *testing.T) {
	for _, test := range []struct {
		err        error
		statusCode int
		code       string
	}{
		{presentation.ErrNotFound, basehttp.StatusNotFound, "resource_not_found"},
		{presentation.ErrVoicePreviewUnavailable, basehttp.StatusServiceUnavailable, "provider_unavailable"},
	} {
		response := performRequest(
			t,
			presentationRouter(t, &applicationStub{previewErr: test.err}, true),
			basehttp.MethodPost,
			"/v1/coach-presentation/voices/voice_ava/previews",
			nil,
		)
		if response.Code != test.statusCode ||
			!bytes.Contains(response.Body.Bytes(), []byte(test.code)) {
			t.Fatalf("error=%v status=%d body=%s", test.err, response.Code, response.Body.String())
		}
	}
}

func presentationRouter(
	t *testing.T,
	application Application,
	authenticated bool,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := New(application, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), httpActor),
			)
			c.Next()
		})
	}
	handler.RegisterRoutes(router)
	return router
}

func performRequest(
	t *testing.T,
	router basehttp.Handler,
	method string,
	path string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func httpCatalog() presentation.Catalog {
	return presentation.Catalog{
		Avatars: []presentation.AvatarOption{{
			ID: "avatar_lisa", DisplayName: "莉萨", Description: "亲切、开朗",
			PreviewAssetKey: "coach-avatar-lisa", Provider: "spatialreal",
			ProviderProfile:  "spatialreal_default",
			ProviderAvatarID: "native-avatar-id", BindingVersion: 1,
			SortOrder: 10, Default: true,
		}},
		Voices: []presentation.VoiceOption{{
			ID: "voice_ava", DisplayName: "艾娃",
			Description: "清晰自然 · 美式英语 · 女声", Locale: "en-US",
			Gender: "female", Provider: "qianwen",
			ProviderProfile: "qianwen_default",
			ProviderModel:   "native-model", ProviderVoiceID: "native-voice-id",
			BindingVersion: 1, SortOrder: 10, Default: true,
		}},
		DefaultAvatarOptionID: "avatar_lisa",
		DefaultVoiceOptionID:  "voice_ava",
	}
}

type applicationStub struct {
	catalog        presentation.Catalog
	preference     presentation.Preference
	updateErr      error
	command        presentation.UpdateCommand
	updateCalls    int
	previewAudio   platformmedia.ManagedAudioSource
	previewErr     error
	previewVoiceID string
}

func (application *applicationStub) CreateVoicePreview(
	_ context.Context,
	_ requestcontext.Actor,
	voiceOptionID string,
) (platformmedia.ManagedAudioSource, error) {
	application.previewVoiceID = voiceOptionID
	return application.previewAudio, application.previewErr
}

func (application *applicationStub) GetCatalog(
	context.Context,
	requestcontext.Actor,
) (presentation.Catalog, error) {
	return application.catalog, nil
}

type managedAudioStub struct {
	bytes  []byte
	closed bool
}

func (audio *managedAudioStub) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(audio.bytes)), nil
}

func (audio *managedAudioStub) MediaType() string       { return platformmedia.ContentTypeWAV }
func (audio *managedAudioStub) Size() int64             { return int64(len(audio.bytes)) }
func (audio *managedAudioStub) Duration() time.Duration { return time.Second }
func (audio *managedAudioStub) SampleRate() int         { return 24000 }
func (audio *managedAudioStub) Close() error {
	audio.closed = true
	return nil
}

func (application *applicationStub) GetPreference(
	context.Context,
	requestcontext.Actor,
) (presentation.Preference, error) {
	return application.preference, nil
}

func (application *applicationStub) UpdatePreference(
	_ context.Context,
	_ requestcontext.Actor,
	command presentation.UpdateCommand,
) (presentation.Preference, error) {
	application.updateCalls++
	application.command = command
	return application.preference, application.updateErr
}
