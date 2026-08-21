package avatar

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProviderUnavailable     = errors.New("avatar provider unavailable")
	ErrProviderQuotaExhausted  = errors.New("avatar provider quota exhausted")
	ErrInvalidProviderResponse = errors.New("invalid avatar provider response")
)

type TokenProvider interface {
	CreateSessionToken(
		context.Context,
		string,
		time.Time,
	) (ProviderSessionToken, error)
}

type ProviderSessionToken struct {
	Value     string
	ExpiresAt time.Time
}
