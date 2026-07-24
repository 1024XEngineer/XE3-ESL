package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PostgreSQL interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// PostgresRepository is the sole Identity adapter for the reviewed PostgreSQL
// schema. It never creates or migrates tables.
type PostgresRepository struct {
	database PostgreSQL
	ids      IDGenerator
}

func NewPostgresRepository(
	database PostgreSQL,
	ids IDGenerator,
) (*PostgresRepository, error) {
	if database == nil || ids == nil {
		return nil, ErrRepository
	}
	return &PostgresRepository{database: database, ids: ids}, nil
}

func (r *PostgresRepository) CreateUserWithCredential(
	ctx context.Context,
	canonicalEmail string,
	passwordHash string,
	now time.Time,
) (_ User, resultErr error) {
	userID, err := r.ids.NewID()
	if err != nil {
		return User{}, ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return User{}, ErrRepository
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, `
INSERT INTO identity_users (
    id,
    canonical_email,
    account_status,
    created_at,
    updated_at
) VALUES ($1, $2, 'active', $3, $3)`,
		userID,
		canonicalEmail,
		now,
	); err != nil {
		return User{}, mapPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_credentials (user_id, password_hash, updated_at)
VALUES ($1, $2, $3)`,
		userID,
		passwordHash,
		now,
	); err != nil {
		return User{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, ErrRepository
	}
	return User{
		ID:        userID,
		Email:     canonicalEmail,
		Status:    AccountActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (r *PostgresRepository) FindCredentialByEmail(
	ctx context.Context,
	canonicalEmail string,
) (Credential, error) {
	var credential Credential
	var status string
	err := r.database.QueryRow(ctx, `
SELECT
    users.id::text,
    users.canonical_email,
    users.account_status,
    users.created_at,
    users.updated_at,
    credentials.password_hash
FROM identity_users AS users
JOIN identity_credentials AS credentials ON credentials.user_id = users.id
WHERE users.canonical_email = $1`,
		canonicalEmail,
	).Scan(
		&credential.User.ID,
		&credential.User.Email,
		&status,
		&credential.User.CreatedAt,
		&credential.User.UpdatedAt,
		&credential.PasswordHash,
	)
	if err != nil {
		return Credential{}, mapPostgresError(err)
	}
	credential.User.Status = AccountStatus(status)
	return credential, nil
}

func (r *PostgresRepository) CreateSession(
	ctx context.Context,
	params CreateSessionParams,
) (_ Session, resultErr error) {
	sessionID, err := r.ids.NewID()
	if err != nil {
		return Session{}, ErrRepository
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Session{}, ErrRepository
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if params.ReplacementHash != "" {
		if _, err := tx.Exec(ctx, `
UPDATE identity_credentials
SET password_hash = $1, updated_at = $2
WHERE user_id = $3 AND password_hash = $4`,
			params.ReplacementHash,
			params.CreatedAt,
			params.UserID,
			params.PreviousHash,
		); err != nil {
			return Session{}, mapPostgresError(err)
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_auth_sessions (
    id,
    user_id,
    token_digest,
    created_at,
    expires_at
) VALUES ($1, $2, $3, $4, $5)`,
		sessionID,
		params.UserID,
		params.TokenDigest,
		params.CreatedAt,
		params.ExpiresAt,
	); err != nil {
		return Session{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, ErrRepository
	}
	return Session{
		ID:        sessionID,
		UserID:    params.UserID,
		ExpiresAt: params.ExpiresAt,
	}, nil
}

func (r *PostgresRepository) FindSessionByTokenDigest(
	ctx context.Context,
	tokenDigest []byte,
	now time.Time,
) (SessionIdentity, error) {
	var identity SessionIdentity
	var status string
	err := r.database.QueryRow(ctx, `
SELECT
    sessions.id::text,
    sessions.expires_at,
    users.id::text,
    users.canonical_email,
    users.account_status,
    users.created_at,
    users.updated_at
FROM identity_auth_sessions AS sessions
JOIN identity_users AS users ON users.id = sessions.user_id
WHERE sessions.token_digest = $1
  AND sessions.revoked_at IS NULL
  AND sessions.expires_at > $2`,
		tokenDigest,
		now,
	).Scan(
		&identity.SessionID,
		&identity.ExpiresAt,
		&identity.User.ID,
		&identity.User.Email,
		&status,
		&identity.User.CreatedAt,
		&identity.User.UpdatedAt,
	)
	if err != nil {
		return SessionIdentity{}, mapPostgresError(err)
	}
	identity.User.Status = AccountStatus(status)
	return identity, nil
}

func (r *PostgresRepository) FindUserByID(
	ctx context.Context,
	userID string,
) (User, error) {
	var user User
	var status string
	err := r.database.QueryRow(ctx, `
SELECT id::text, canonical_email, account_status, created_at, updated_at
FROM identity_users
WHERE id = $1`,
		userID,
	).Scan(
		&user.ID,
		&user.Email,
		&status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return User{}, mapPostgresError(err)
	}
	user.Status = AccountStatus(status)
	return user, nil
}

func (r *PostgresRepository) RevokeSession(
	ctx context.Context,
	userID string,
	sessionID string,
	revokedAt time.Time,
	reason string,
) error {
	tag, err := r.database.Exec(ctx, `
UPDATE identity_auth_sessions
SET
    revoked_at = COALESCE(revoked_at, $3),
    revocation_reason = COALESCE(revocation_reason, $4)
WHERE id = $1 AND user_id = $2`,
		sessionID,
		userID,
		revokedAt,
		reason,
	)
	if err != nil {
		return mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	revokedAt time.Time,
	reason string,
) error {
	_, err := r.database.Exec(ctx, `
UPDATE identity_auth_sessions
SET revoked_at = $2, revocation_reason = $3
WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
		revokedAt,
		reason,
	)
	return mapPostgresError(err)
}

func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		postgresError.Code == "23505" {
		return ErrConflict
	}
	return ErrRepository
}
