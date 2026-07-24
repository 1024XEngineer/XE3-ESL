BEGIN;

CREATE TABLE identity_users (
    id uuid PRIMARY KEY,
    canonical_email text NOT NULL,
    account_status text NOT NULL DEFAULT 'active',
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT identity_users_canonical_email_key UNIQUE (canonical_email),
    CONSTRAINT identity_users_canonical_email_length_check
        CHECK (octet_length(canonical_email) BETWEEN 3 AND 254),
    CONSTRAINT identity_users_canonical_email_ascii_check
        CHECK (canonical_email !~ '[^\x21-\x7e]'),
    CONSTRAINT identity_users_canonical_email_shape_check
        CHECK (canonical_email ~ '^[^@]+@[^@]+$'),
    CONSTRAINT identity_users_canonical_email_lowercase_check
        CHECK (canonical_email = lower(canonical_email)),
    CONSTRAINT identity_users_account_status_check
        CHECK (account_status IN ('active', 'deleting', 'deleted')),
    CONSTRAINT identity_users_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE TABLE identity_credentials (
    user_id uuid PRIMARY KEY,
    password_hash text NOT NULL,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT identity_credentials_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT identity_credentials_password_hash_length_check
        CHECK (octet_length(password_hash) BETWEEN 64 AND 512),
    CONSTRAINT identity_credentials_password_hash_ascii_check
        CHECK (password_hash !~ '[^\x21-\x7e]'),
    CONSTRAINT identity_credentials_password_hash_algorithm_check
        CHECK (password_hash LIKE '$argon2id$%')
);

CREATE TABLE identity_auth_sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    token_digest bytea NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    revocation_reason text,
    CONSTRAINT identity_auth_sessions_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT identity_auth_sessions_token_digest_key UNIQUE (token_digest),
    CONSTRAINT identity_auth_sessions_token_digest_length_check
        CHECK (octet_length(token_digest) = 32),
    CONSTRAINT identity_auth_sessions_expiry_check
        CHECK (expires_at > created_at),
    CONSTRAINT identity_auth_sessions_revocation_pair_check
        CHECK (
            (revoked_at IS NULL AND revocation_reason IS NULL)
            OR
            (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
        ),
    CONSTRAINT identity_auth_sessions_revoked_at_check
        CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT identity_auth_sessions_revocation_reason_check
        CHECK (
            revocation_reason IS NULL
            OR revocation_reason ~ '^[a-z][a-z0-9_]{0,63}$'
        )
);

CREATE INDEX identity_auth_sessions_user_created_idx
    ON identity_auth_sessions (user_id, created_at DESC);

CREATE INDEX identity_auth_sessions_active_user_idx
    ON identity_auth_sessions (user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX identity_auth_sessions_active_expiry_idx
    ON identity_auth_sessions (expires_at)
    WHERE revoked_at IS NULL;

COMMIT;
