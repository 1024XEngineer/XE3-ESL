package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	database Database
}

func New(database Database) (*Repository, error) {
	if database == nil {
		return nil, coachingprofile.ErrRepository
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) Find(
	ctx context.Context,
	userID string,
) (coachingprofile.Profile, error) {
	if repository == nil || repository.database == nil || userID == "" {
		return coachingprofile.Profile{}, coachingprofile.ErrInvalidRequest
	}
	return scanProfile(repository.database.QueryRow(ctx, `
SELECT
    user_id::text,
    memory_enabled,
    profile,
    field_sources,
    version,
    created_at,
    updated_at
FROM coaching_user_profiles
WHERE user_id = $1`, userID))
}

func (repository *Repository) Save(
	ctx context.Context,
	item coachingprofile.Profile,
	expectedVersion int64,
) (coachingprofile.Profile, error) {
	if repository == nil || repository.database == nil ||
		!item.ValidForPersistence() || expectedVersion < 0 ||
		item.Version != expectedVersion+1 {
		return coachingprofile.Profile{}, coachingprofile.ErrInvalidRequest
	}
	data, err := json.Marshal(item.Data)
	if err != nil {
		return coachingprofile.Profile{}, coachingprofile.ErrInvalidRequest
	}
	sources, err := json.Marshal(item.FieldSources)
	if err != nil {
		return coachingprofile.Profile{}, coachingprofile.ErrInvalidRequest
	}
	if expectedVersion == 0 {
		return scanSavedProfile(repository.database.QueryRow(ctx, `
WITH active_user AS (
    SELECT id
    FROM users
    WHERE id = $1 AND status = 'active'
    FOR UPDATE
)
INSERT INTO coaching_user_profiles (
    user_id,
    memory_enabled,
    profile,
    field_sources,
    version,
    created_at,
    updated_at
)
SELECT id, $2, $3::jsonb, $4::jsonb, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM active_user
ON CONFLICT (user_id) DO NOTHING
RETURNING
    user_id::text,
    memory_enabled,
    profile,
    field_sources,
    version,
    created_at,
    updated_at`, item.UserID, item.MemoryEnabled, data, sources))
	}
	return scanSavedProfile(repository.database.QueryRow(ctx, `
WITH active_user AS (
    SELECT id
    FROM users
    WHERE id = $1 AND status = 'active'
    FOR UPDATE
)
UPDATE coaching_user_profiles AS profile
SET
    memory_enabled = $3,
    profile = $4::jsonb,
    field_sources = $5::jsonb,
    version = profile.version + 1,
    updated_at = CURRENT_TIMESTAMP
FROM active_user
WHERE profile.user_id = active_user.id AND profile.version = $2
RETURNING
    profile.user_id::text,
    profile.memory_enabled,
    profile.profile,
    profile.field_sources,
    profile.version,
    profile.created_at,
    profile.updated_at`, item.UserID, expectedVersion, item.MemoryEnabled, data, sources))
}

func scanSavedProfile(row pgx.Row) (coachingprofile.Profile, error) {
	item, err := scanProfile(row)
	if errors.Is(err, coachingprofile.ErrNotFound) {
		return coachingprofile.Profile{}, coachingprofile.ErrVersionConflict
	}
	return item, err
}

func scanProfile(row pgx.Row) (coachingprofile.Profile, error) {
	var item coachingprofile.Profile
	var data []byte
	var sources []byte
	err := row.Scan(
		&item.UserID,
		&item.MemoryEnabled,
		&data,
		&sources,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return coachingprofile.Profile{}, coachingprofile.ErrNotFound
	}
	if err != nil {
		return coachingprofile.Profile{}, mapError(err)
	}
	if strictDecode(data, &item.Data) != nil ||
		strictDecode(sources, &item.FieldSources) != nil {
		return coachingprofile.Profile{}, coachingprofile.ErrRepository
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if !item.ValidStored() {
		return coachingprofile.Profile{}, coachingprofile.ErrRepository
	}
	return item, nil
}

func strictDecode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return coachingprofile.ErrRepository
	}
	return nil
}

func mapError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22P02", "22001", "23502", "23503", "23514":
			return coachingprofile.ErrInvalidRequest
		case "23505", "40001", "40P01":
			return coachingprofile.ErrVersionConflict
		}
	}
	return coachingprofile.ErrRepository
}

var _ coachingprofile.Repository = (*Repository)(nil)
