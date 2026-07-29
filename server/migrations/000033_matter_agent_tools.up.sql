BEGIN;

CREATE TABLE matter_agent_create_requests (
    owner_user_id uuid NOT NULL,
    request_id text COLLATE "C" NOT NULL,
    payload_fingerprint bytea NOT NULL,
    matter_id uuid NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT matter_agent_create_requests_pkey
        PRIMARY KEY (owner_user_id, request_id),
    CONSTRAINT matter_agent_create_requests_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE,
    CONSTRAINT matter_agent_create_requests_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT matter_agent_create_requests_request_id_check
        CHECK (
            octet_length(request_id) BETWEEN 1 AND 256
            AND request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'
        ),
    CONSTRAINT matter_agent_create_requests_fingerprint_check
        CHECK (octet_length(payload_fingerprint) = 32)
);

CREATE UNIQUE INDEX matter_agent_create_requests_matter_idx
    ON matter_agent_create_requests (owner_user_id, matter_id);

COMMIT;
