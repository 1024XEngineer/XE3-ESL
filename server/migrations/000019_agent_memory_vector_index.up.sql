BEGIN;

-- Production must provide pgvector. A missing extension or insufficient
-- privileges intentionally fails this migration instead of silently changing
-- retrieval semantics.
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;

CREATE TABLE agent_memory_vectors (
    memory_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    memory_version bigint NOT NULL,
    provider text COLLATE "C" NOT NULL,
    model text COLLATE "C" NOT NULL,
    dimension integer NOT NULL,
    embedding_policy_version text COLLATE "C" NOT NULL,
    embedding public.vector(1024) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT agent_memory_vectors_memory_owner_fkey
        FOREIGN KEY (memory_id, owner_user_id)
        REFERENCES agent_memories (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_memory_vectors_memory_version_check
        CHECK (memory_version > 0),
    CONSTRAINT agent_memory_vectors_provider_check
        CHECK (
            octet_length(provider) BETWEEN 1 AND 64
            AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
        ),
    CONSTRAINT agent_memory_vectors_model_check
        CHECK (
            octet_length(model) BETWEEN 1 AND 128
            AND model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_memory_vectors_dimension_check
        CHECK (
            dimension = 1024
            AND public.vector_dims(embedding) = dimension
        ),
    CONSTRAINT agent_memory_vectors_policy_version_check
        CHECK (
            octet_length(embedding_policy_version) BETWEEN 1 AND 64
            AND embedding_policy_version
                ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
        ),
    CONSTRAINT agent_memory_vectors_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX agent_memory_vectors_owner_version_idx
    ON agent_memory_vectors (
        owner_user_id,
        provider,
        model,
        dimension,
        embedding_policy_version,
        memory_id
    );

CREATE INDEX agent_memory_vectors_embedding_hnsw_idx
    ON agent_memory_vectors
    USING hnsw (embedding public.vector_cosine_ops);

CREATE TABLE agent_memory_index_jobs (
    memory_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    memory_version bigint NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    embedding_policy_version text COLLATE "C",
    provider text COLLATE "C",
    model text COLLATE "C",
    dimension integer,
    failure_kind text COLLATE "C",
    input_tokens integer,
    total_tokens integer,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    PRIMARY KEY (memory_id, memory_version),
    CONSTRAINT agent_memory_index_jobs_memory_owner_fkey
        FOREIGN KEY (memory_id, owner_user_id)
        REFERENCES agent_memories (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_memory_index_jobs_memory_version_check
        CHECK (memory_version > 0),
    CONSTRAINT agent_memory_index_jobs_status_check
        CHECK (status IN (
            'pending',
            'running',
            'completed',
            'failed',
            'discarded'
        )),
    CONSTRAINT agent_memory_index_jobs_attempt_check
        CHECK (attempt_count BETWEEN 0 AND 10),
    CONSTRAINT agent_memory_index_jobs_policy_check
        CHECK (
            embedding_policy_version IS NULL
            OR (
                octet_length(embedding_policy_version) BETWEEN 1 AND 64
                AND embedding_policy_version
                    ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'
            )
        ),
    CONSTRAINT agent_memory_index_jobs_provider_check
        CHECK (
            provider IS NULL
            OR (
                octet_length(provider) BETWEEN 1 AND 64
                AND provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            )
        ),
    CONSTRAINT agent_memory_index_jobs_model_check
        CHECK (
            model IS NULL
            OR (
                octet_length(model) BETWEEN 1 AND 128
                AND model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
        ),
    CONSTRAINT agent_memory_index_jobs_dimension_check
        CHECK (dimension IS NULL OR dimension = 1024),
    CONSTRAINT agent_memory_index_jobs_failure_check
        CHECK (
            failure_kind IS NULL
            OR (
                octet_length(failure_kind) BETWEEN 1 AND 64
                AND failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
            )
        ),
    CONSTRAINT agent_memory_index_jobs_usage_check
        CHECK (
            (input_tokens IS NULL AND total_tokens IS NULL)
            OR (
                input_tokens >= 0
                AND total_tokens >= input_tokens
            )
        ),
    CONSTRAINT agent_memory_index_jobs_state_check
        CHECK (
            (
                status = 'pending'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NULL
            )
            OR (
                status = 'running'
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND completed_at IS NULL
                AND embedding_policy_version IS NOT NULL
                AND provider IS NOT NULL
                AND model IS NOT NULL
                AND dimension IS NOT NULL
            )
            OR (
                status = 'completed'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
                AND failure_kind IS NULL
                AND input_tokens IS NOT NULL
                AND total_tokens IS NOT NULL
            )
            OR (
                status IN ('failed', 'discarded')
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
                AND failure_kind IS NOT NULL
            )
        ),
    CONSTRAINT agent_memory_index_jobs_timestamps_check
        CHECK (
            updated_at >= created_at
            AND next_attempt_at >= created_at
            AND (
                lease_expires_at IS NULL
                OR lease_expires_at > updated_at
            )
            AND (
                completed_at IS NULL
                OR completed_at >= created_at
            )
        )
);

CREATE INDEX agent_memory_index_jobs_claim_idx
    ON agent_memory_index_jobs (
        next_attempt_at,
        created_at,
        memory_id,
        memory_version
    )
    WHERE status = 'pending';

CREATE UNIQUE INDEX agent_memory_index_jobs_running_lease_idx
    ON agent_memory_index_jobs (lease_token)
    WHERE status = 'running';

CREATE INDEX agent_memory_index_jobs_recovery_idx
    ON agent_memory_index_jobs (
        lease_expires_at,
        memory_id,
        memory_version
    )
    WHERE status = 'running';

CREATE INDEX agent_memory_index_jobs_owner_idx
    ON agent_memory_index_jobs (
        owner_user_id,
        created_at DESC,
        memory_id,
        memory_version DESC
    );

CREATE FUNCTION enqueue_agent_memory_index_job()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'active'
       AND (
           TG_OP = 'INSERT'
           OR OLD.version IS DISTINCT FROM NEW.version
       )
    THEN
        INSERT INTO agent_memory_index_jobs (
            memory_id,
            owner_user_id,
            memory_version,
            status,
            attempt_count,
            next_attempt_at,
            created_at,
            updated_at
        ) VALUES (
            NEW.id,
            NEW.owner_user_id,
            NEW.version,
            'pending',
            0,
            transaction_timestamp(),
            transaction_timestamp(),
            transaction_timestamp()
        )
        ON CONFLICT (memory_id, memory_version) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER agent_memories_enqueue_vector_index
AFTER INSERT OR UPDATE OF version, status ON agent_memories
FOR EACH ROW
EXECUTE FUNCTION enqueue_agent_memory_index_job();

COMMIT;
