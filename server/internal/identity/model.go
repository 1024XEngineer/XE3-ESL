package identity

import (
	"context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type AccountStatus string

const (
	AccountActive   AccountStatus = "active"
	AccountDisabled AccountStatus = "disabled"
	AccountDeleting AccountStatus = "deleting"
	AccountDeleted  AccountStatus = "deleted"
)

type User struct {
	ID        string
	Email     string
	Status    AccountStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Credential struct {
	User         User
	PasswordHash string
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

type SessionIdentity struct {
	User      User
	SessionID string
	ExpiresAt time.Time
}

type LoginResult struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

type CreateSessionParams struct {
	UserID          string
	TokenDigest     []byte
	CreatedAt       time.Time
	ExpiresAt       time.Time
	PreviousHash    string
	ReplacementHash string
}

// Repository is the persistence boundary owned by Identity. Implementations
// must use the reviewed migration history and must not create tables.
type Repository interface {
	CreateUserWithCredential(
		ctx context.Context,
		canonicalEmail string,
		passwordHash string,
		now time.Time,
	) (User, error)
	FindCredentialByEmail(ctx context.Context, canonicalEmail string) (Credential, error)
	CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)
	FindSessionByTokenDigest(
		ctx context.Context,
		tokenDigest []byte,
		now time.Time,
	) (SessionIdentity, error)
	FindUserByID(ctx context.Context, userID string) (User, error)
	RevokeSession(
		ctx context.Context,
		userID string,
		sessionID string,
		revokedAt time.Time,
		reason string,
	) error
	RevokeAllSessionsForUser(
		ctx context.Context,
		userID string,
		revokedAt time.Time,
		reason string,
	) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (valid bool, needsRehash bool, err error)
}

type SessionTokens interface {
	Generate() (raw string, digest []byte, err error)
	Digest(raw string) []byte
	ValidWireFormat(raw string) bool
}

type Clock interface {
	Now() time.Time
}

type Application interface {
	Register(ctx context.Context, email, password string) (User, error)
	Login(ctx context.Context, email, password string) (LoginResult, error)
	Logout(ctx context.Context, actor requestcontext.Actor) error
	CurrentUser(ctx context.Context, actor requestcontext.Actor) (User, error)
	RevokeAllSessionsForUser(ctx context.Context, userID, reason string) error
}
