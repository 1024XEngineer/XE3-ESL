BEGIN;

ALTER TABLE practice_sessions
DROP COLUMN presentation_snapshot;

ALTER TABLE coach_voice_options
DROP CONSTRAINT coach_voice_options_text_check,
DROP CONSTRAINT coach_voice_options_provider_binding_unique,
DROP COLUMN provider_profile,
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
    AND char_length(provider_model) BETWEEN 1 AND 128
    AND provider_model = btrim(provider_model)
    AND provider_model !~ '[[:space:]]'
    AND char_length(provider_voice_id) BETWEEN 1 AND 128
    AND provider_voice_id = btrim(provider_voice_id)
    AND provider_voice_id !~ '[[:space:]]'
),
ADD CONSTRAINT coach_voice_options_provider_binding_unique UNIQUE (
    provider,
    provider_model,
    provider_voice_id,
    binding_version
);

ALTER TABLE coach_avatar_options
DROP CONSTRAINT coach_avatar_options_text_check,
DROP CONSTRAINT coach_avatar_options_provider_binding_unique,
DROP COLUMN provider_profile,
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
    AND char_length(provider_avatar_id) BETWEEN 1 AND 128
    AND provider_avatar_id = btrim(provider_avatar_id)
    AND provider_avatar_id !~ '[[:space:]]'
),
ADD CONSTRAINT coach_avatar_options_provider_binding_unique UNIQUE (
    provider,
    provider_avatar_id,
    binding_version
);

COMMIT;
