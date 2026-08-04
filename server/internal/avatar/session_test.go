package avatar

import (
	"context"
	"errors"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"testing"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestServiceIssuesFrozenClientContractForOwnedInteractiveSession(
	t *testing.T,
) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	provider := &tokenProviderStub{
		token: ProviderSessionToken{
			Value:     "provider-session-token",
			ExpiresAt: now.Add(10 * time.Minute),
		},
	}
	service := newTestService(
		t,
		contextSessionReaderStub{session: interactiveSession()},
		provider,
	)
	service.now = func() time.Time { return now }

	result, err := service.IssueSessionToken(
		context.Background(),
		testActor(),
		"practice-session-1",
	)
	if err != nil {
		t.Fatalf("IssueSessionToken() error = %v", err)
	}
	if result.AppID != "app-1" ||
		result.AvatarID != "avatar-1" ||
		result.SessionToken != "provider-session-token" ||
		result.Region != "ap-northeast" ||
		result.ExpiresAt != "2026-07-29T10:10:00Z" ||
		result.AudioFormat != (AudioFormat{
			Encoding:     "PCM_S16LE",
			SampleRateHz: 24000,
			Channels:     1,
		}) {
		t.Fatalf("result = %#v", result)
	}
	if provider.appID != "app-1" ||
		!provider.expiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf(
			"provider request = app %q, expiry %s",
			provider.appID,
			provider.expiresAt,
		)
	}
}

func TestServiceIssuesSessionTokenForInterview(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	session := interactiveSession()
	session.SceneFamily = scene.SceneFamilyInterview
	provider := &tokenProviderStub{
		token: ProviderSessionToken{
			Value:     "provider-session-token",
			ExpiresAt: now.Add(10 * time.Minute),
		},
	}
	service := newTestService(
		t,
		contextSessionReaderStub{session: session},
		provider,
	)
	service.now = func() time.Time { return now }

	if _, err := service.IssueSessionToken(
		context.Background(),
		testActor(),
		"practice-session-1",
	); err != nil {
		t.Fatalf("IssueSessionToken() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

func TestServiceRejectsUnownedAndNonInteractiveSessionsBeforeProvider(
	t *testing.T,
) {
	tests := []struct {
		name         string
		reader       contextSessionReaderStub
		expected     apperror.Category
		expectedCode string
	}{
		{
			name: "unowned",
			reader: contextSessionReaderStub{
				err: practice.ErrNotFound,
			},
			expected:     apperror.NotFound,
			expectedCode: "practice_session_not_found",
		},
		{
			name: "paused",
			reader: contextSessionReaderStub{
				session: func() practice.Session {
					session := interactiveSession()
					session.Status = practice.SessionPaused
					return session
				}(),
			},
			expected:     apperror.Conflict,
			expectedCode: "resource_conflict",
		},
		{
			name: "terminal",
			reader: contextSessionReaderStub{
				session: func() practice.Session {
					session := interactiveSession()
					session.Status = practice.SessionCompleted
					return session
				}(),
			},
			expected:     apperror.Conflict,
			expectedCode: "resource_conflict",
		},
		{
			name: "unsupported scenario",
			reader: contextSessionReaderStub{
				session: func() practice.Session {
					session := interactiveSession()
					session.SceneFamily =
						scene.SceneFamilyExam
					return session
				}(),
			},
			expected:     apperror.Conflict,
			expectedCode: "resource_conflict",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &tokenProviderStub{
				token: ProviderSessionToken{
					Value:     "provider-session-token",
					ExpiresAt: time.Now().Add(5 * time.Minute),
				},
			}
			service := newTestService(t, test.reader, provider)
			_, err := service.IssueSessionToken(
				context.Background(),
				testActor(),
				"practice-session-1",
			)
			appError, ok := apperror.From(err)
			if !ok ||
				appError.Category() != test.expected ||
				appError.Code() != test.expectedCode {
				t.Fatalf("error = %#v", err)
			}
			if provider.calls != 0 {
				t.Fatalf("provider calls = %d, want 0", provider.calls)
			}
		})
	}
}

func TestServiceMapsProviderFailuresWithoutExposingProviderDetails(
	t *testing.T,
) {
	tests := []struct {
		name          string
		providerError error
		expectedCode  string
	}{
		{
			name:          "unavailable",
			providerError: ErrProviderUnavailable,
			expectedCode:  "provider_unavailable",
		},
		{
			name:          "quota",
			providerError: ErrProviderQuotaExhausted,
			expectedCode:  "quota_exhausted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(
				t,
				contextSessionReaderStub{session: interactiveSession()},
				&tokenProviderStub{err: test.providerError},
			)
			_, err := service.IssueSessionToken(
				context.Background(),
				testActor(),
				"practice-session-1",
			)
			appError, ok := apperror.From(err)
			if !ok ||
				appError.Category() != apperror.Unavailable ||
				appError.Code() != test.expectedCode ||
				!appError.Retryable() {
				t.Fatalf("error = %#v", err)
			}
			if errors.Is(err, test.providerError) == false {
				t.Fatal("provider cause was not preserved")
			}
		})
	}
}

func TestNewServiceRejectsTokenTTLAboveHardLimit(t *testing.T) {
	_, err := NewService(
		ServiceConfiguration{
			Enabled:  true,
			AppID:    "app-1",
			AvatarID: "avatar-1",
			Region:   "ap-northeast",
			TokenTTL: maximumSessionTokenTTL + time.Nanosecond,
		},
		contextSessionReaderStub{},
		&tokenProviderStub{},
	)
	if err == nil {
		t.Fatal("NewService() error = nil")
	}
}

func TestServiceRejectsProviderExpiryBeyondRequestedHardLimit(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	service := newTestService(
		t,
		contextSessionReaderStub{session: interactiveSession()},
		&tokenProviderStub{
			token: ProviderSessionToken{
				Value:     "provider-session-token",
				ExpiresAt: now.Add(maximumSessionTokenTTL + time.Second),
			},
		},
	)
	service.now = func() time.Time { return now }

	_, err := service.IssueSessionToken(
		context.Background(),
		testActor(),
		"practice-session-1",
	)
	appError, ok := apperror.From(err)
	if !ok ||
		appError.Category() != apperror.Unavailable ||
		appError.Code() != "provider_unavailable" ||
		!errors.Is(err, ErrInvalidProviderResponse) {
		t.Fatalf("error = %#v", err)
	}
}

func newTestService(
	t *testing.T,
	sessions SessionReader,
	provider TokenProvider,
) *Service {
	t.Helper()
	service, err := NewService(
		ServiceConfiguration{
			Enabled:  true,
			AppID:    "app-1",
			AvatarID: "avatar-1",
			Region:   "ap-northeast",
			TokenTTL: 10 * time.Minute,
		},
		sessions,
		provider,
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func interactiveSession() practice.Session {
	return practice.Session{
		ID:          "practice-session-1",
		SceneFamily: scene.SceneFamilyWorkplace,
		Status:      practice.SessionInProgress,
	}
}

func testActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "user-1",
		SessionID: "identity-session-1",
	}
}

type contextSessionReaderStub struct {
	session practice.Session
	err     error
}

func (stub contextSessionReaderStub) GetSession(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Session, error) {
	return stub.session, stub.err
}

type tokenProviderStub struct {
	token     ProviderSessionToken
	err       error
	appID     string
	expiresAt time.Time
	calls     int
}

func (stub *tokenProviderStub) CreateSessionToken(
	_ context.Context,
	appID string,
	expiresAt time.Time,
) (ProviderSessionToken, error) {
	stub.calls++
	stub.appID = appID
	stub.expiresAt = expiresAt
	return stub.token, stub.err
}
