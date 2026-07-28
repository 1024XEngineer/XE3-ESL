BEGIN;

CREATE TABLE identity_user_profiles (
    user_id uuid PRIMARY KEY,
    display_name text NOT NULL,
    profile_version bigint NOT NULL DEFAULT 1,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT identity_user_profiles_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT identity_user_profiles_display_name_bytes_check
        CHECK (octet_length(display_name) BETWEEN 1 AND 120),
    CONSTRAINT identity_user_profiles_display_name_characters_check
        CHECK (char_length(display_name) BETWEEN 1 AND 40),
    CONSTRAINT identity_user_profiles_display_name_no_controls_check
        CHECK (display_name !~ '[[:cntrl:]]'),
    CONSTRAINT identity_user_profiles_profile_version_check
        CHECK (profile_version >= 1),
    CONSTRAINT identity_user_profiles_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE TABLE identity_profile_idempotency (
    user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_digest bytea NOT NULL,
    response_display_name text NOT NULL,
    response_profile_version bigint NOT NULL,
    response_created_at timestamp with time zone NOT NULL,
    response_updated_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, idempotency_key),
    CONSTRAINT identity_profile_idempotency_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT identity_profile_idempotency_key_length_check
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128),
    CONSTRAINT identity_profile_idempotency_key_ascii_check
        CHECK (idempotency_key ~ '^[A-Za-z0-9._~+/-]+$'),
    CONSTRAINT identity_profile_idempotency_digest_length_check
        CHECK (octet_length(request_digest) = 32),
    CONSTRAINT identity_profile_idempotency_response_name_bytes_check
        CHECK (octet_length(response_display_name) BETWEEN 1 AND 120),
    CONSTRAINT identity_profile_idempotency_response_name_characters_check
        CHECK (char_length(response_display_name) BETWEEN 1 AND 40),
    CONSTRAINT identity_profile_idempotency_response_version_check
        CHECK (response_profile_version >= 1),
    CONSTRAINT identity_profile_idempotency_response_timestamps_check
        CHECK (response_updated_at >= response_created_at)
);

CREATE INDEX identity_profile_idempotency_user_created_idx
    ON identity_profile_idempotency (
        user_id,
        created_at DESC,
        idempotency_key DESC
    );

COMMIT;
