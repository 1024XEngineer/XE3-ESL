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
	UpdatedAt    time.Time
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
	UserID              string
	TokenDigest         []byte
	CredentialUpdatedAt time.Time
	Lifetime            time.Duration
	PreviousHash        string
	ReplacementHash     string
}

// Repository is the persistence boundary owned by Identity. Implementations
// must use the reviewed migration history and must not create tables.
type Repository interface {
	CreateUserWithCredential(
		ctx context.Context,
		canonicalEmail string,
		passwordHash string,
	) (User, error)
	FindCredentialByEmail(ctx context.Context, canonicalEmail string) (Credential, error)
	CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)
	FindSessionByTokenDigest(
		ctx context.Context,
		tokenDigest []byte,
	) (SessionIdentity, error)
	FindUserByID(ctx context.Context, userID string) (User, error)
	RevokeSession(
		ctx context.Context,
		userID string,
		sessionID string,
		reason string,
	) error
	RevokeAllSessionsForUser(
		ctx context.Context,
		userID string,
		reason string,
	) error
}

type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(
		ctx context.Context,
		password string,
		encodedHash string,
	) (valid bool, needsRehash bool, err error)
}

type SessionTokens interface {
	Generate() (raw string, digest []byte, err error)
	Digest(raw string) []byte
	ValidWireFormat(raw string) bool
}

type Application interface {
	Register(ctx context.Context, email, password string) (User, error)
	Login(ctx context.Context, email, password string) (LoginResult, error)
	Logout(ctx context.Context, actor requestcontext.Actor) error
	CurrentUser(ctx context.Context, actor requestcontext.Actor) (User, error)
	RevokeAllSessionsForUser(ctx context.Context, userID, reason string) error
}
