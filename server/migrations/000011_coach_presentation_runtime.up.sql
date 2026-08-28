BEGIN;

ALTER TABLE coach_avatar_options
ADD COLUMN provider_profile TEXT;

UPDATE coach_avatar_options
SET provider_profile = 'spatialreal_default';

ALTER TABLE coach_avatar_options
ALTER COLUMN provider_profile SET NOT NULL,
DROP CONSTRAINT coach_avatar_options_text_check,
DROP CONSTRAINT coach_avatar_options_provider_binding_unique,
ADD CONSTRAINT coach_avatar_options_text_check CHECK (
    char_length(btrim(display_name)) BETWEEN 1 AND 64
    AND display_name = btrim(display_name)
    AND char_length(btrim(description)) BETWEEN 1 AND 200
    AND description = btrim(description)
    AND char_length(preview_asset_key) BETWEEN 1 AND 128
    AND preview_asset_key = btrim(preview_asset_key)
    AND preview_asset_key !~ '[[:space:]]'
    AND char_length(provider) BETWEEN 1 AND 128
    AND provider = btrim(provider)
    AND provider !~ '[[:space:]]'
    AND char_length(provider_profile) BETWEEN 1 AND 128
    AND provider_profile = btrim(provider_profile)
    AND provider_profile !~ '[[:space:]]'
    AND char_length(provider_avatar_id) BETWEEN 1 AND 128
    AND provider_avatar_id = btrim(provider_avatar_id)
    AND provider_avatar_id !~ '[[:space:]]'
),
ADD CONSTRAINT coach_avatar_options_provider_binding_unique UNIQUE (
    provider,
    provider_profile,
    provider_avatar_id,
    binding_version
);

ALTER TABLE coach_voice_options
ADD COLUMN provider_profile TEXT;

UPDATE coach_voice_options
SET provider_profile = 'qianwen_default';

ALTER TABLE coach_voice_options
ALTER COLUMN provider_profile SET NOT NULL,
DROP CONSTRAINT coach_voice_options_text_check,
DROP CONSTRAINT coach_voice_options_provider_binding_unique,
ADD CONSTRAINT coach_voice_options_text_check CHECK (
    char_length(btrim(display_name)) BETWEEN 1 AND 64
    AND display_name = btrim(display_name)
    AND char_length(btrim(description)) BETWEEN 1 AND 200
    AND description = btrim(description)
    AND char_length(locale) BETWEEN 1 AND 32
    AND locale = btrim(locale)
    AND locale !~ '[[:space:]]'
    AND gender IN ('female', 'male')
    AND char_length(provider) BETWEEN 1 AND 128
    AND provider = btrim(provider)
    AND provider !~ '[[:space:]]'
    AND char_length(provider_profile) BETWEEN 1 AND 128
    AND provider_profile = btrim(provider_profile)
    AND provider_profile !~ '[[:space:]]'
    AND char_length(provider_model) BETWEEN 1 AND 128
    AND provider_model = btrim(provider_model)
    AND provider_model !~ '[[:space:]]'
    AND char_length(provider_voice_id) BETWEEN 1 AND 128
    AND provider_voice_id = btrim(provider_voice_id)
    AND provider_voice_id !~ '[[:space:]]'
),
ADD CONSTRAINT coach_voice_options_provider_binding_unique UNIQUE (
    provider,
    provider_profile,
    provider_model,
    provider_voice_id,
    binding_version
);

ALTER TABLE practice_sessions
ADD COLUMN presentation_snapshot JSONB;

WITH default_avatar AS (
    SELECT * FROM coach_avatar_options
    WHERE enabled = TRUE AND is_default = TRUE
), default_voice AS (
    SELECT * FROM coach_voice_options
    WHERE enabled = TRUE AND is_default = TRUE
)
UPDATE practice_sessions
SET presentation_snapshot = jsonb_build_object(
    'schema_version', 1,
    'avatar', jsonb_build_object(
        'option_id', default_avatar.id,
        'provider', default_avatar.provider,
        'provider_profile', default_avatar.provider_profile,
        'provider_avatar_id', default_avatar.provider_avatar_id,
        'binding_version', default_avatar.binding_version
    ),
    'voice', jsonb_build_object(
        'option_id', default_voice.id,
        'provider', default_voice.provider,
        'provider_profile', default_voice.provider_profile,
        'provider_model', default_voice.provider_model,
        'provider_voice_id', default_voice.provider_voice_id,
        'locale', default_voice.locale,
        'binding_version', default_voice.binding_version
    )
)
FROM default_avatar, default_voice;

ALTER TABLE practice_sessions
ALTER COLUMN presentation_snapshot SET NOT NULL,
ADD CONSTRAINT practice_sessions_presentation_snapshot_check CHECK (
    jsonb_typeof(presentation_snapshot) = 'object'
    AND presentation_snapshot @> '{"schema_version": 1}'::jsonb
    AND jsonb_typeof(presentation_snapshot -> 'avatar') = 'object'
    AND jsonb_typeof(presentation_snapshot -> 'voice') = 'object'
);

COMMIT;
