package bootstrap

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewIdentityModule builds the production Identity composition. It has no
// fixed-Actor fallback; startup fails if any real dependency cannot be built.
func NewIdentityModule(
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
) (*identity.Module, error) {
	if database == nil {
		return nil, errors.New("bootstrap: identity database is required")
	}
	clock := identity.SystemClock{}
	passwords, err := identity.NewDefaultArgon2idHasher()
	if err != nil {
		return nil, err
	}
	tokens := identity.NewOpaqueSessionTokens(nil)
	dummyMaterial, _, err := tokens.Generate()
	if err != nil {
		return nil, errors.New("bootstrap: identity random source unavailable")
	}
	dummyHash, err := passwords.Hash(context.Background(), dummyMaterial)
	if err != nil {
		return nil, errors.New("bootstrap: identity password hashing unavailable")
	}
	repository, err := identity.NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		return nil, err
	}
	service, err := identity.NewService(
		repository,
		passwords,
		tokens,
		dummyHash,
	)
	if err != nil {
		return nil, err
	}
	rateLimits, err := identity.NewDefaultRateLimiters(clock)
	if err != nil {
		return nil, err
	}
	sourceIPs, err := identity.NewTrustedProxyResolver(trustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	handler, err := identity.NewHTTPHandler(
		service,
		service,
		rateLimits,
		nil,
		identity.WithSourceIPResolver(sourceIPs),
	)
	if err != nil {
		return nil, err
	}
	return identity.NewModule(handler)
}
