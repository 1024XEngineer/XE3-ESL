BEGIN;

CREATE TABLE matters (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT matters_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT matters_id_owner_key UNIQUE (id, owner_user_id),
    CONSTRAINT matters_title_length_check
        CHECK (
            char_length(title) BETWEEN 1 AND 200
            AND octet_length(title) <= 512
        ),
    CONSTRAINT matters_title_trimmed_check
        CHECK (title = btrim(title) AND title !~ '^[[:space:]]*$'),
    CONSTRAINT matters_status_check
        CHECK (status IN ('active', 'completed', 'archived')),
    CONSTRAINT matters_version_check CHECK (version > 0),
    CONSTRAINT matters_timestamps_check CHECK (updated_at >= created_at)
);

CREATE INDEX matters_owner_updated_idx
    ON matters (owner_user_id, updated_at DESC, id DESC);

CREATE TABLE agent_threads (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    next_message_sequence bigint NOT NULL DEFAULT 1,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_threads_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_threads_id_owner_key UNIQUE (id, owner_user_id),
    CONSTRAINT agent_threads_next_sequence_check
        CHECK (next_message_sequence > 0),
    CONSTRAINT agent_threads_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX agent_threads_owner_updated_idx
    ON agent_threads (owner_user_id, updated_at DESC, id DESC);

CREATE TABLE agent_thread_matter_links (
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    matter_id uuid NOT NULL,
    is_active boolean NOT NULL DEFAULT false,
    linked_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_thread_matter_links_pkey
        PRIMARY KEY (thread_id, matter_id),
    CONSTRAINT agent_thread_matter_links_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_matter_links_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_thread_matter_links_timestamps_check
        CHECK (updated_at >= linked_at)
);

CREATE UNIQUE INDEX agent_thread_matter_links_one_active_idx
    ON agent_thread_matter_links (owner_user_id, thread_id)
    WHERE is_active;

CREATE INDEX agent_thread_matter_links_matter_idx
    ON agent_thread_matter_links (owner_user_id, matter_id, thread_id);

CREATE TABLE agent_messages (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    sequence_no bigint NOT NULL,
    role text NOT NULL,
    client_message_id text COLLATE "C" NOT NULL,
    content text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_messages_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_messages_thread_sequence_key
        UNIQUE (owner_user_id, thread_id, sequence_no),
    CONSTRAINT agent_messages_client_idempotency_key
        UNIQUE (owner_user_id, thread_id, client_message_id),
    CONSTRAINT agent_messages_sequence_check CHECK (sequence_no > 0),
    CONSTRAINT agent_messages_role_check
        CHECK (role = 'user'),
    CONSTRAINT agent_messages_client_id_check
        CHECK (
            octet_length(client_message_id) BETWEEN 1 AND 128
            AND client_message_id ~
                '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_messages_content_length_check
        CHECK (
            char_length(content) BETWEEN 1 AND 4096
            AND octet_length(content) <= 16384
            AND content !~ '^[[:space:]]*$'
        )
);

COMMIT;
