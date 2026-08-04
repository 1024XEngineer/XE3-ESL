package avatar

import (
	"context"
	"errors"
	"strings"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	audioEncoding          = "PCM_S16LE"
	audioSampleRate        = 24000
	audioChannels          = 1
	maximumSessionTokenTTL = 10 * time.Minute
)

type SessionReader interface {
	GetSession(
		context.Context,
		requestcontext.Actor,
		string,
	) (practice.Session, error)
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

func NewService(
	configuration ServiceConfiguration,
	sessions SessionReader,
	provider TokenProvider,
) (*Service, error) {
	if sessions == nil ||
		configuration.TokenTTL <= 0 ||
		configuration.TokenTTL > maximumSessionTokenTTL {
		return nil, errors.New("avatar: service dependency is required")
	}
	if configuration.Enabled &&
		(provider == nil ||
			strings.TrimSpace(configuration.AppID) == "" ||
			strings.TrimSpace(configuration.AvatarID) == "" ||
			strings.TrimSpace(configuration.Region) == "") {
		return nil, errors.New(
			"avatar: enabled provider configuration is required",
		)
	}
	return &Service{
		configuration: configuration,
		sessions:      sessions,
		provider:      provider,
		now:           time.Now,
	}, nil
}

func (service *Service) IssueSessionToken(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (SessionToken, error) {
	if service == nil || service.sessions == nil || ctx == nil ||
		!actor.Valid() || !validResourceID(practiceSessionID) {
		return SessionToken{}, sessionNotFoundError(nil)
	}
	session, err := service.sessions.GetSession(
		ctx,
		actor,
		practiceSessionID,
	)
	if err != nil {
		if errors.Is(err, practice.ErrNotFound) ||
			errors.Is(err, practice.ErrInvalidArgument) {
			return SessionToken{}, sessionNotFoundError(err)
		}
		return SessionToken{}, apperror.New(
			apperror.Internal,
			"internal_error",
			"Internal server error.",
			apperror.WithCause(err),
		)
	}
	if session.ID != practiceSessionID ||
		(session.SceneFamily != scene.SceneFamilyWorkplace &&
			session.SceneFamily != scene.SceneFamilyDaily &&
			session.SceneFamily != scene.SceneFamilyInterview) ||
		(session.Status != practice.SessionStarting &&
			session.Status != practice.SessionInProgress) {
		return SessionToken{}, apperror.New(
			apperror.Conflict,
			"resource_conflict",
			"The practice session cannot start an avatar connection.",
		)
	}
	if !service.configuration.Enabled || service.provider == nil {
		return SessionToken{}, providerUnavailableError(nil)
	}

	now := service.now().UTC()
	requestedExpiry := now.Add(service.configuration.TokenTTL).
		Truncate(time.Second)
	providerToken, err := service.provider.CreateSessionToken(
		ctx,
		service.configuration.AppID,
		requestedExpiry,
	)
	if err != nil {
		if errors.Is(err, ErrProviderQuotaExhausted) {
			return SessionToken{}, apperror.New(
				apperror.Unavailable,
				"quota_exhausted",
				"Avatar capacity is temporarily unavailable.",
				apperror.WithRetryable(true),
				apperror.WithCause(err),
			)
		}
		return SessionToken{}, providerUnavailableError(err)
	}
	if !validSessionToken(providerToken.Value) ||
		providerToken.ExpiresAt.Before(now.Add(15*time.Second)) ||
		providerToken.ExpiresAt.After(requestedExpiry) {
		return SessionToken{}, providerUnavailableError(
			ErrInvalidProviderResponse,
		)
	}
	return SessionToken{
		AppID:        service.configuration.AppID,
		AvatarID:     service.configuration.AvatarID,
		SessionToken: providerToken.Value,
		Region:       service.configuration.Region,
		ExpiresAt: providerToken.ExpiresAt.UTC().
			Truncate(time.Second).
			Format(time.RFC3339),
		AudioFormat: AudioFormat{
			Encoding:     audioEncoding,
			SampleRateHz: audioSampleRate,
			Channels:     audioChannels,
		},
	}, nil
}

func sessionNotFoundError(cause error) error {
	options := make([]apperror.Option, 0, 1)
	if cause != nil {
		options = append(options, apperror.WithCause(cause))
	}
	return apperror.New(
		apperror.NotFound,
		"practice_session_not_found",
		"Practice session was not found.",
		options...,
	)
}

func providerUnavailableError(cause error) error {
	options := []apperror.Option{apperror.WithRetryable(true)}
	if cause != nil {
		options = append(options, apperror.WithCause(cause))
	}
	return apperror.New(
		apperror.Unavailable,
		"provider_unavailable",
		"Avatar service is temporarily unavailable.",
		options...,
	)
}

func validResourceID(value string) bool {
	return value != "" &&
		len(value) <= 128 &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsRune(value, '\x00')
}

func validSessionToken(value string) bool {
	if len(value) < 8 || len(value) > 8192 {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return character <= 0x20 || character > 0x7e
	}) < 0
}
