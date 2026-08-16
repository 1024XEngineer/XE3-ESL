package avatar

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestIssueSessionTokenUsesOwnedActivePractice(t *testing.T) {
	now := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	provider := avatarProviderStub{token: ProviderSessionToken{
		Value:     "short-lived-token",
		ExpiresAt: now.Add(5 * time.Minute),
	}}
	service, err := NewService(
		ServiceConfiguration{
			Enabled: true, AppID: "app", AvatarID: "avatar",
			Region: "ap-northeast", TokenTTL: 5 * time.Minute,
		},
		avatarSessionReaderStub{session: practice.Session{
			ID: "session-1", Status: practice.SessionInProgress,
		}},
		provider,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.now = func() time.Time { return now }

	grant, err := service.IssueSessionToken(
		context.Background(),
		requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"},
		"session-1",
	)
	if err != nil {
		t.Fatalf("issue session token: %v", err)
	}
	if grant.SessionToken != provider.token.Value || grant.AudioFormat.SampleRateHz != 24000 {
		t.Fatalf("grant = %#v", grant)
	}
}

type avatarSessionReaderStub struct{ session practice.Session }

func (stub avatarSessionReaderStub) GetSession(
	context.Context,
	practice.Actor,
	string,
) (practice.Session, error) {
	return stub.session, nil
}

type avatarProviderStub struct{ token ProviderSessionToken }

func (stub avatarProviderStub) CreateSessionToken(
	context.Context,
	string,
	time.Time,
) (ProviderSessionToken, error) {
	return stub.token, nil
}
