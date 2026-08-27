BEGIN;

CREATE TABLE coach_avatar_options (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL,
    preview_asset_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_avatar_id TEXT NOT NULL,
    binding_version BIGINT NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT coach_avatar_options_id_check CHECK (
        id ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    CONSTRAINT coach_avatar_options_text_check CHECK (
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
    CONSTRAINT coach_avatar_options_binding_version_check CHECK (
        binding_version > 0
    ),
    CONSTRAINT coach_avatar_options_sort_order_check CHECK (sort_order >= 0),
    CONSTRAINT coach_avatar_options_provider_binding_unique UNIQUE (
        provider,
        provider_avatar_id,
        binding_version
    )
);

CREATE UNIQUE INDEX coach_avatar_options_one_enabled_default_idx
ON coach_avatar_options (is_default)
WHERE enabled = TRUE AND is_default = TRUE;

CREATE INDEX coach_avatar_options_enabled_order_idx
ON coach_avatar_options (sort_order, id)
WHERE enabled = TRUE;

CREATE TABLE coach_voice_options (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL,
    locale TEXT NOT NULL,
    gender TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_model TEXT NOT NULL,
    provider_voice_id TEXT NOT NULL,
    binding_version BIGINT NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT coach_voice_options_id_check CHECK (
        id ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    CONSTRAINT coach_voice_options_text_check CHECK (
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
    CONSTRAINT coach_voice_options_binding_version_check CHECK (
        binding_version > 0
    ),
    CONSTRAINT coach_voice_options_sort_order_check CHECK (sort_order >= 0),
    CONSTRAINT coach_voice_options_provider_binding_unique UNIQUE (
        provider,
        provider_model,
        provider_voice_id,
        binding_version
    )
);

CREATE UNIQUE INDEX coach_voice_options_one_enabled_default_idx
ON coach_voice_options (is_default)
WHERE enabled = TRUE AND is_default = TRUE;

CREATE INDEX coach_voice_options_enabled_order_idx
ON coach_voice_options (sort_order, id)
WHERE enabled = TRUE;

CREATE TABLE user_coach_presentation_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    avatar_option_id TEXT NOT NULL REFERENCES coach_avatar_options(id),
    voice_option_id TEXT NOT NULL REFERENCES coach_voice_options(id),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT user_coach_presentation_preferences_version_check CHECK (
        version > 0
    ),
    CONSTRAINT user_coach_presentation_preferences_time_check CHECK (
        updated_at >= created_at
    )
);

CREATE INDEX user_coach_presentation_preferences_avatar_idx
ON user_coach_presentation_preferences (avatar_option_id);

CREATE INDEX user_coach_presentation_preferences_voice_idx
ON user_coach_presentation_preferences (voice_option_id);

INSERT INTO coach_avatar_options (
    id,
    display_name,
    description,
    preview_asset_key,
    provider,
    provider_avatar_id,
    binding_version,
    sort_order,
    enabled,
    is_default
) VALUES
    (
        'avatar_lisa',
        '莉萨',
        '亲切、开朗',
        'coach-avatar-lisa',
        'spatialreal',
        '94a60c13-e835-4bde-aa93-00a1cf178dcd',
        1,
        10,
        TRUE,
        TRUE
    ),
    (
        'avatar_nathan',
        '内森',
        '温暖、沉稳',
        'coach-avatar-nathan',
        'spatialreal',
        '1843ff9f-db3a-45de-be28-9c2b9d6412a3',
        1,
        20,
        TRUE,
        FALSE
    );

INSERT INTO coach_voice_options (
    id,
    display_name,
    description,
    locale,
    gender,
    provider,
    provider_model,
    provider_voice_id,
    binding_version,
    sort_order,
    enabled,
    is_default
) VALUES
    (
        'voice_ava',
        '艾娃',
        '清晰自然 · 美式英语 · 女声',
        'en-US',
        'female',
        'qianwen',
        'qwen-audio-3.0-tts-flash',
        'loongeva_v3.6',
        1,
        10,
        TRUE,
        TRUE
    ),
    (
        'voice_john',
        '约翰',
        '温暖沉稳 · 美式英语 · 男声',
        'en-US',
        'male',
        'qianwen',
        'qwen-audio-3.0-tts-flash',
        'loongjohn',
        1,
        20,
        TRUE,
        FALSE
    );

COMMIT;
