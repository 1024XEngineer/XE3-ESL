package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const transactionRollbackTimeout = 5 * time.Second

type PostgreSQL interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
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
			rollback(tx)
		}
	}()

	var createdAt time.Time
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
INSERT INTO identity_users (
    id,
    canonical_email,
    account_status,
    created_at,
    updated_at
) VALUES ($1, $2, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING created_at, updated_at`,
		userID,
		canonicalEmail,
	).Scan(&createdAt, &updatedAt); err != nil {
		return User{}, mapPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_credentials (user_id, password_hash, updated_at)
VALUES ($1, $2, CURRENT_TIMESTAMP)`,
		userID,
		passwordHash,
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
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
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
    credentials.password_hash,
    credentials.updated_at
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
		&credential.UpdatedAt,
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
	if params.Lifetime <= 0 ||
		params.Lifetime%time.Second != 0 ||
		len(params.TokenDigest) != 32 {
		return Session{}, ErrRepository
	}
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
			rollback(tx)
		}
	}()

	var status string
	if err := tx.QueryRow(ctx, `
SELECT account_status
FROM identity_users
WHERE id = $1
FOR UPDATE`,
		params.UserID,
	).Scan(&status); err != nil {
		return Session{}, mapPostgresError(err)
	}
	var currentHash string
	var credentialUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT password_hash, updated_at
FROM identity_credentials
WHERE user_id = $1
FOR UPDATE`,
		params.UserID,
	).Scan(&currentHash, &credentialUpdatedAt); err != nil {
		return Session{}, mapPostgresError(err)
	}
	if AccountStatus(status) != AccountActive ||
		currentHash != params.PreviousHash ||
		!credentialUpdatedAt.Equal(params.CredentialUpdatedAt) {
		return Session{}, ErrAuthenticationChanged
	}

	if params.ReplacementHash != "" {
		tag, err := tx.Exec(ctx, `
UPDATE identity_credentials
SET
    password_hash = $1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE user_id = $2
  AND password_hash = $3
  AND updated_at = $4`,
			params.ReplacementHash,
			params.UserID,
			params.PreviousHash,
			params.CredentialUpdatedAt,
		)
		if err != nil {
			return Session{}, mapPostgresError(err)
		}
		if tag.RowsAffected() != 1 {
			return Session{}, ErrAuthenticationChanged
		}
	}
	var createdAt time.Time
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `
INSERT INTO identity_auth_sessions (
    id,
    user_id,
    token_digest,
    created_at,
    expires_at
) VALUES (
    $1,
    $2,
    $3,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP + ($4::bigint * INTERVAL '1 second')
)
RETURNING created_at, expires_at`,
		sessionID,
		params.UserID,
		params.TokenDigest,
		int64(params.Lifetime/time.Second),
	).Scan(&createdAt, &expiresAt); err != nil {
		return Session{}, mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, ErrRepository
	}
	return Session{
		ID:        sessionID,
		UserID:    params.UserID,
		ExpiresAt: expiresAt,
	}, nil
}

func (r *PostgresRepository) FindSessionByTokenDigest(
	ctx context.Context,
	tokenDigest []byte,
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
  AND sessions.created_at <= CURRENT_TIMESTAMP
  AND sessions.expires_at > CURRENT_TIMESTAMP`,
		tokenDigest,
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
	reason string,
) (resultErr error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return ErrRepository
	}
	defer func() {
		if resultErr != nil {
			rollback(tx)
		}
	}()
	var lockedUserID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM identity_users
WHERE id = $1
FOR UPDATE`,
		userID,
	).Scan(&lockedUserID); err != nil {
		return mapPostgresError(err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE identity_auth_sessions
SET
    revoked_at = COALESCE(
        revoked_at,
        GREATEST(CURRENT_TIMESTAMP, created_at)
    ),
    revocation_reason = COALESCE(revocation_reason, $3)
WHERE id = $1 AND user_id = $2`,
		sessionID,
		userID,
		reason,
	)
	if err != nil {
		return mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrRepository
	}
	return nil
}

func (r *PostgresRepository) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	reason string,
) (resultErr error) {
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return ErrRepository
	}
	defer func() {
		if resultErr != nil {
			rollback(tx)
		}
	}()
	var lockedUserID string
	if err := tx.QueryRow(ctx, `
SELECT id::text
FROM identity_users
WHERE id = $1
FOR UPDATE`,
		userID,
	).Scan(&lockedUserID); err != nil {
		return mapPostgresError(err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE identity_credentials
SET updated_at = GREATEST(
    CURRENT_TIMESTAMP,
    updated_at + INTERVAL '1 microsecond'
)
WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return mapPostgresError(err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
UPDATE identity_auth_sessions
SET
    revoked_at = GREATEST(CURRENT_TIMESTAMP, created_at),
    revocation_reason = $2
WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
		reason,
	); err != nil {
		return mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrRepository
	}
	return nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), transactionRollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
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
