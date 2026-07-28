BEGIN;

CREATE TABLE agent_memory_deletion_fences (
    -- The tombstone deliberately survives physical Identity deletion.
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT agent_memory_deletion_fences_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE TABLE agent_memories (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    memory_type text NOT NULL,
    canonical_key text COLLATE "C" NOT NULL,
    content text NOT NULL,
    scope_type text NOT NULL,
    matter_id uuid,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    policy_version text COLLATE "C" NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    inactivated_at timestamptz,
    CONSTRAINT agent_memories_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_memories_id_owner_key
        UNIQUE (id, owner_user_id),
    CONSTRAINT agent_memories_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_memories_type_check
        CHECK (
            octet_length(memory_type) BETWEEN 1 AND 32
            AND memory_type ~ '^[a-z][a-z0-9_]{0,31}$'
        ),
    CONSTRAINT agent_memories_key_check
        CHECK (
            octet_length(canonical_key) BETWEEN 1 AND 128
            AND canonical_key ~ '^[a-z][a-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_memories_content_check
        CHECK (
            char_length(content) BETWEEN 1 AND 4096
            AND octet_length(content) <= 16384
            AND content = btrim(content)
            AND content !~ '^[[:space:]]*$'
        ),
    CONSTRAINT agent_memories_scope_check
        CHECK (
            (scope_type = 'user' AND matter_id IS NULL)
            OR (scope_type = 'matter' AND matter_id IS NOT NULL)
        ),
    CONSTRAINT agent_memories_status_check
        CHECK (status IN ('active', 'inactive')),
    CONSTRAINT agent_memories_version_check CHECK (version > 0),
    CONSTRAINT agent_memories_policy_version_check
        CHECK (
            octet_length(policy_version) BETWEEN 1 AND 64
            AND policy_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    CONSTRAINT agent_memories_lifecycle_check
        CHECK (
            updated_at >= created_at
            AND (expires_at IS NULL OR expires_at > created_at)
            AND (
                (status = 'active' AND inactivated_at IS NULL)
                OR (
                    status = 'inactive'
                    AND inactivated_at IS NOT NULL
                    AND inactivated_at >= created_at
                    AND updated_at >= inactivated_at
                )
            )
        )
);

CREATE UNIQUE INDEX agent_memories_one_active_user_key
    ON agent_memories (
        owner_user_id,
        memory_type,
        canonical_key
    )
    WHERE status = 'active' AND scope_type = 'user';

CREATE UNIQUE INDEX agent_memories_one_active_matter_key
    ON agent_memories (
        owner_user_id,
        matter_id,
        memory_type,
        canonical_key
    )
    WHERE status = 'active' AND scope_type = 'matter';

CREATE INDEX agent_memories_active_user_updated_idx
    ON agent_memories (owner_user_id, updated_at DESC, id DESC)
    WHERE status = 'active' AND scope_type = 'user';

CREATE INDEX agent_memories_active_matter_updated_idx
    ON agent_memories (
        owner_user_id,
        matter_id,
        updated_at DESC,
        id DESC
    )
    WHERE status = 'active' AND scope_type = 'matter';

CREATE TABLE agent_memory_sources (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    memory_id uuid NOT NULL,
    source_type text NOT NULL,
    source_id text COLLATE "C" NOT NULL,
    source_version bigint NOT NULL,
    source_checksum bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT agent_memory_sources_memory_owner_fkey
        FOREIGN KEY (memory_id, owner_user_id)
        REFERENCES agent_memories (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_memory_sources_type_check
        CHECK (
            octet_length(source_type) BETWEEN 1 AND 32
            AND source_type ~ '^[a-z][a-z0-9_]{0,31}$'
        ),
    CONSTRAINT agent_memory_sources_id_check
        CHECK (
            octet_length(source_id) BETWEEN 1 AND 128
            AND source_id = btrim(source_id)
            AND source_id !~ '[[:space:]]'
        ),
    CONSTRAINT agent_memory_sources_version_check
        CHECK (source_version > 0),
    CONSTRAINT agent_memory_sources_checksum_check
        CHECK (octet_length(source_checksum) = 32),
    CONSTRAINT agent_memory_sources_evidence_key
        UNIQUE (
            owner_user_id,
            memory_id,
            source_type,
            source_id,
            source_version,
            source_checksum
        )
);

CREATE INDEX agent_memory_sources_memory_created_idx
    ON agent_memory_sources (
        owner_user_id,
        memory_id,
        created_at,
        id
    );

COMMIT;
