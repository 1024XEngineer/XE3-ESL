BEGIN;

CREATE TABLE agent_thread_summary_checkpoints (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL,
    thread_id UUID NOT NULL,
    previous_checkpoint_id UUID,
    source_from_sequence BIGINT NOT NULL,
    covered_through_sequence BIGINT NOT NULL,
    summary_content JSONB NOT NULL,
    policy_version TEXT COLLATE "C" NOT NULL,
    prompt_version TEXT COLLATE "C" NOT NULL,
    provider TEXT COLLATE "C" NOT NULL,
    model TEXT COLLATE "C" NOT NULL,
    source_checksum BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_thread_summary_checkpoints_thread_fk
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_summary_checkpoints_previous_fk
        FOREIGN KEY (previous_checkpoint_id, owner_user_id, thread_id)
        REFERENCES agent_thread_summary_checkpoints (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_thread_summary_checkpoints_identity_unique
        UNIQUE (id, owner_user_id, thread_id),
    CONSTRAINT agent_thread_summary_checkpoints_coverage_unique
        UNIQUE (owner_user_id, thread_id, covered_through_sequence),
    CONSTRAINT agent_thread_summary_checkpoints_sequence_check
        CHECK (
            source_from_sequence >= 1
            AND covered_through_sequence >= source_from_sequence
            AND (
                (previous_checkpoint_id IS NULL AND source_from_sequence = 1)
                OR
                (previous_checkpoint_id IS NOT NULL AND source_from_sequence > 1)
            )
        ),
    CONSTRAINT agent_thread_summary_checkpoints_content_check
        CHECK (
            jsonb_typeof(summary_content) = 'object'
            AND summary_content ?& ARRAY[
                'goals',
                'background',
                'progress',
                'decisions',
                'open_questions',
                'next_steps'
            ]
            AND (
                summary_content - ARRAY[
                    'goals',
                    'background',
                    'progress',
                    'decisions',
                    'open_questions',
                    'next_steps'
                ]
            ) = '{}'::JSONB
            AND jsonb_typeof(summary_content->'goals') = 'array'
            AND jsonb_typeof(summary_content->'background') = 'array'
            AND jsonb_typeof(summary_content->'progress') = 'array'
            AND jsonb_typeof(summary_content->'decisions') = 'array'
            AND jsonb_typeof(summary_content->'open_questions') = 'array'
            AND jsonb_typeof(summary_content->'next_steps') = 'array'
            AND octet_length(summary_content::TEXT) <= 131072
        ),
    CONSTRAINT agent_thread_summary_checkpoints_policy_version_check
        CHECK (policy_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    CONSTRAINT agent_thread_summary_checkpoints_prompt_version_check
        CHECK (prompt_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
    CONSTRAINT agent_thread_summary_checkpoints_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9_-]{0,63}$'),
    CONSTRAINT agent_thread_summary_checkpoints_model_check
        CHECK (model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT agent_thread_summary_checkpoints_checksum_check
        CHECK (octet_length(source_checksum) = 32)
);

CREATE UNIQUE INDEX agent_thread_summary_checkpoints_first_unique
    ON agent_thread_summary_checkpoints (owner_user_id, thread_id)
    WHERE previous_checkpoint_id IS NULL;

CREATE UNIQUE INDEX agent_thread_summary_checkpoints_next_unique
    ON agent_thread_summary_checkpoints (previous_checkpoint_id)
    WHERE previous_checkpoint_id IS NOT NULL;

CREATE INDEX agent_thread_summary_checkpoints_latest_idx
    ON agent_thread_summary_checkpoints (
        owner_user_id,
        thread_id,
        covered_through_sequence DESC
    );

COMMIT;
