package avatar

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maximumSessionTokenTTL = 10 * time.Minute

type SessionReader interface {
	GetSession(context.Context, practice.Actor, string) (practice.Session, error)
}

type ServiceConfiguration struct {
	Enabled  bool
	AppID    string
	AvatarID string
	Region   string
	TokenTTL time.Duration
}

type Service struct {
	configuration ServiceConfiguration
	sessions      SessionReader
	provider      TokenProvider
	now           func() time.Time
}

type AudioFormat struct {
	Encoding     string `json:"encoding"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
}

type SessionToken struct {
	AppID        string      `json:"app_id"`
	AvatarID     string      `json:"avatar_id"`
	SessionToken string      `json:"session_token"`
	Region       string      `json:"region"`
	ExpiresAt    string      `json:"expires_at"`
	AudioFormat  AudioFormat `json:"audio_format"`
}

func NewService(configuration ServiceConfiguration, sessions SessionReader, provider TokenProvider) (*Service, error) {
	if sessions == nil || configuration.TokenTTL <= 0 || configuration.TokenTTL > maximumSessionTokenTTL {
		return nil, errors.New("practice avatar: service dependency is required")
	}
	if configuration.Enabled && (provider == nil || strings.TrimSpace(configuration.AppID) == "" || strings.TrimSpace(configuration.AvatarID) == "" || strings.TrimSpace(configuration.Region) == "") {
		return nil, errors.New("practice avatar: enabled provider configuration is required")
	}
	return &Service{configuration: configuration, sessions: sessions, provider: provider, now: time.Now}, nil
}

func (service *Service) IssueSessionToken(ctx context.Context, actor requestcontext.Actor, sessionID string) (SessionToken, error) {
	if service == nil || service.sessions == nil || ctx == nil || !actor.Valid() || strings.TrimSpace(sessionID) == "" {
		return SessionToken{}, apperror.New(apperror.NotFound, "practice_session_not_found", "Practice session was not found.")
	}
	session, err := service.sessions.GetSession(ctx, practice.Actor{UserID: actor.UserID, SessionID: actor.SessionID}, sessionID)
	if err != nil || session.ID != sessionID {
		return SessionToken{}, apperror.New(apperror.NotFound, "practice_session_not_found", "Practice session was not found.", apperror.WithCause(err))
	}
	if session.Status != practice.SessionStarting && session.Status != practice.SessionInProgress {
		return SessionToken{}, apperror.New(apperror.Conflict, "resource_conflict", "The practice session cannot start an avatar connection.")
	}
	if !service.configuration.Enabled || service.provider == nil {
		return SessionToken{}, providerUnavailableError(nil)
	}
	now := service.now().UTC()
	expiry := now.Add(service.configuration.TokenTTL).Truncate(time.Second)
	providerToken, err := service.provider.CreateSessionToken(ctx, service.configuration.AppID, expiry)
	if err != nil {
		if errors.Is(err, ErrProviderQuotaExhausted) {
			return SessionToken{}, apperror.New(apperror.Unavailable, "quota_exhausted", "Avatar capacity is temporarily unavailable.", apperror.WithRetryable(true), apperror.WithCause(err))
		}
		return SessionToken{}, providerUnavailableError(err)
	}
	if len(providerToken.Value) < 8 || providerToken.ExpiresAt.Before(now.Add(15*time.Second)) || providerToken.ExpiresAt.After(expiry) {
		return SessionToken{}, providerUnavailableError(ErrInvalidProviderResponse)
	}
	return SessionToken{
		AppID: service.configuration.AppID, AvatarID: service.configuration.AvatarID,
		SessionToken: providerToken.Value, Region: service.configuration.Region,
		ExpiresAt:   providerToken.ExpiresAt.UTC().Truncate(time.Second).Format(time.RFC3339),
		AudioFormat: AudioFormat{Encoding: "PCM_S16LE", SampleRateHz: 24000, Channels: 1},
	}, nil
}

func providerUnavailableError(cause error) error {
	options := []apperror.Option{apperror.WithRetryable(true)}
	if cause != nil {
		options = append(options, apperror.WithCause(cause))
	}
	return apperror.New(apperror.Unavailable, "provider_unavailable", "Avatar service is temporarily unavailable.", options...)
}
