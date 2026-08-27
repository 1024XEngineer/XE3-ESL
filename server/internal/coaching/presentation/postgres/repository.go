package postgres

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct {
	database Database
}

func New(database Database) (*Repository, error) {
	if database == nil {
		return nil, presentation.ErrRepository
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) Catalog(
	ctx context.Context,
) (presentation.Catalog, error) {
	if repository == nil || repository.database == nil {
		return presentation.Catalog{}, presentation.ErrInvalidRequest
	}
	avatars, err := repository.loadAvatars(ctx)
	if err != nil {
		return presentation.Catalog{}, err
	}
	voices, err := repository.loadVoices(ctx)
	if err != nil {
		return presentation.Catalog{}, err
	}
	catalog := presentation.Catalog{Avatars: avatars, Voices: voices}
	for _, option := range avatars {
		if option.Default {
			catalog.DefaultAvatarOptionID = option.ID
		}
	}
	for _, option := range voices {
		if option.Default {
			catalog.DefaultVoiceOptionID = option.ID
		}
	}
	if !catalog.Valid() {
		return presentation.Catalog{}, presentation.ErrRepository
	}
	return catalog, nil
}

func (repository *Repository) loadAvatars(
	ctx context.Context,
) ([]presentation.AvatarOption, error) {
	rows, err := repository.database.Query(ctx, `
SELECT
    id,
    display_name,
    description,
    preview_asset_key,
    provider,
    provider_profile,
    provider_avatar_id,
    binding_version,
    sort_order,
    is_default
FROM coach_avatar_options
WHERE enabled = TRUE
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	options := make([]presentation.AvatarOption, 0)
	for rows.Next() {
		var option presentation.AvatarOption
		if err := rows.Scan(
			&option.ID,
			&option.DisplayName,
			&option.Description,
			&option.PreviewAssetKey,
			&option.Provider,
			&option.ProviderProfile,
			&option.ProviderAvatarID,
			&option.BindingVersion,
			&option.SortOrder,
			&option.Default,
		); err != nil {
			return nil, mapError(err)
		}
		if !option.Valid() {
			return nil, presentation.ErrRepository
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return options, nil
}

func (repository *Repository) loadVoices(
	ctx context.Context,
) ([]presentation.VoiceOption, error) {
	rows, err := repository.database.Query(ctx, `
SELECT
    id,
    display_name,
    description,
    locale,
    gender,
    provider,
    provider_profile,
    provider_model,
    provider_voice_id,
    binding_version,
    sort_order,
    is_default
FROM coach_voice_options
WHERE enabled = TRUE
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	options := make([]presentation.VoiceOption, 0)
	for rows.Next() {
		var option presentation.VoiceOption
		if err := rows.Scan(
			&option.ID,
			&option.DisplayName,
			&option.Description,
			&option.Locale,
			&option.Gender,
			&option.Provider,
			&option.ProviderProfile,
			&option.ProviderModel,
			&option.ProviderVoiceID,
			&option.BindingVersion,
			&option.SortOrder,
			&option.Default,
		); err != nil {
			return nil, mapError(err)
		}
		if !option.Valid() {
			return nil, presentation.ErrRepository
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return options, nil
}

// ResolveSelection returns the current enabled provider binding for a user.
// When Repository is backed by pgx.Tx, callers can freeze the result in the
// same transaction that creates the owning Practice Session.
func (repository *Repository) ResolveSelection(
	ctx context.Context,
	userID string,
) (presentation.ResolvedSelection, error) {
	if repository == nil || repository.database == nil || userID == "" {
		return presentation.ResolvedSelection{}, presentation.ErrInvalidRequest
	}
	catalog, err := repository.Catalog(ctx)
	if err != nil {
		return presentation.ResolvedSelection{}, err
	}
	preference, err := repository.FindPreference(ctx, userID)
	if errors.Is(err, presentation.ErrNotFound) {
		preference = presentation.DefaultPreference(userID, catalog)
	} else if err != nil {
		return presentation.ResolvedSelection{}, err
	}
	var resolved presentation.ResolvedSelection
	for _, option := range catalog.Avatars {
		if option.ID == preference.AvatarOptionID {
			resolved.Avatar = option
			break
		}
	}
	for _, option := range catalog.Voices {
		if option.ID == preference.VoiceOptionID {
			resolved.Voice = option
			break
		}
	}
	if !resolved.Valid() {
		return presentation.ResolvedSelection{}, presentation.ErrInvalidRequest
	}
	return resolved, nil
}

func (repository *Repository) FindPreference(
	ctx context.Context,
	userID string,
) (presentation.Preference, error) {
	if repository == nil || repository.database == nil || userID == "" {
		return presentation.Preference{}, presentation.ErrInvalidRequest
	}
	return scanPreference(repository.database.QueryRow(ctx, `
SELECT
    user_id::text,
    avatar_option_id,
    voice_option_id,
    version,
    created_at,
    updated_at
FROM user_coach_presentation_preferences
WHERE user_id = $1`, userID))
}

func (repository *Repository) SavePreference(
	ctx context.Context,
	preference presentation.Preference,
	expectedVersion int64,
) (presentation.Preference, error) {
	if repository == nil || repository.database == nil ||
		expectedVersion < 0 || preference.Version != expectedVersion+1 ||
		preference.UserID == "" || preference.AvatarOptionID == "" ||
		preference.VoiceOptionID == "" {
		return presentation.Preference{}, presentation.ErrInvalidRequest
	}
	var row pgx.Row
	if expectedVersion == 0 {
		row = repository.database.QueryRow(ctx, `
WITH active_user AS (
    SELECT id
    FROM users
    WHERE id = $1 AND status = 'active'
    FOR UPDATE
), valid_avatar AS (
    SELECT id FROM coach_avatar_options WHERE id = $2 AND enabled = TRUE
), valid_voice AS (
    SELECT id FROM coach_voice_options WHERE id = $3 AND enabled = TRUE
)
INSERT INTO user_coach_presentation_preferences (
    user_id,
    avatar_option_id,
    voice_option_id,
    version,
    created_at,
    updated_at
)
SELECT active_user.id, valid_avatar.id, valid_voice.id, 1,
       CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM active_user
CROSS JOIN valid_avatar
CROSS JOIN valid_voice
ON CONFLICT (user_id) DO NOTHING
RETURNING
    user_id::text,
    avatar_option_id,
    voice_option_id,
    version,
    created_at,
    updated_at`, preference.UserID, preference.AvatarOptionID, preference.VoiceOptionID)
	} else {
		row = repository.database.QueryRow(ctx, `
WITH active_user AS (
    SELECT id
    FROM users
    WHERE id = $1 AND status = 'active'
    FOR UPDATE
), valid_avatar AS (
    SELECT id FROM coach_avatar_options WHERE id = $3 AND enabled = TRUE
), valid_voice AS (
    SELECT id FROM coach_voice_options WHERE id = $4 AND enabled = TRUE
)
UPDATE user_coach_presentation_preferences AS preference
SET
    avatar_option_id = valid_avatar.id,
    voice_option_id = valid_voice.id,
    version = preference.version + 1,
    updated_at = CURRENT_TIMESTAMP
FROM active_user, valid_avatar, valid_voice
WHERE preference.user_id = active_user.id AND preference.version = $2
RETURNING
    preference.user_id::text,
    preference.avatar_option_id,
    preference.voice_option_id,
    preference.version,
    preference.created_at,
    preference.updated_at`, preference.UserID, expectedVersion,
			preference.AvatarOptionID, preference.VoiceOptionID)
	}
	saved, err := scanPreference(row)
	if errors.Is(err, presentation.ErrNotFound) {
		return presentation.Preference{}, presentation.ErrVersionConflict
	}
	return saved, err
}

func scanPreference(row pgx.Row) (presentation.Preference, error) {
	var preference presentation.Preference
	err := row.Scan(
		&preference.UserID,
		&preference.AvatarOptionID,
		&preference.VoiceOptionID,
		&preference.Version,
		&preference.CreatedAt,
		&preference.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return presentation.Preference{}, presentation.ErrNotFound
	}
	if err != nil {
		return presentation.Preference{}, mapError(err)
	}
	preference.CreatedAt = preference.CreatedAt.UTC()
	preference.UpdatedAt = preference.UpdatedAt.UTC()
	if !preference.ValidForPersistence() {
		return presentation.Preference{}, presentation.ErrRepository
	}
	return preference, nil
}

func mapError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22P02", "22001", "23502", "23503", "23514":
			return presentation.ErrInvalidRequest
		case "23505", "40001", "40P01":
			return presentation.ErrVersionConflict
		}
	}
	return presentation.ErrRepository
}

var _ presentation.Repository = (*Repository)(nil)
