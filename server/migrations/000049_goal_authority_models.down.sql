BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM coaching_goals)
       OR EXISTS (SELECT 1 FROM agent_thread_goal_links)
       OR EXISTS (SELECT 1 FROM goal_agent_create_requests)
       OR EXISTS (
           SELECT 1
           FROM agent_context_manifests
           WHERE active_goal_id IS NOT NULL
       )
       OR EXISTS (
           SELECT 1
           FROM agent_memories
           WHERE goal_id IS NOT NULL OR scope_type = 'goal'
       )
       OR EXISTS (
           SELECT 1
           FROM practice_plans
           WHERE goal_id IS NOT NULL
       )
       OR EXISTS (
           SELECT 1
           FROM practice_sessions
           WHERE goal_id IS NOT NULL
       ) THEN
        RAISE EXCEPTION
            'Goal authority downgrade requires an empty Goal data slice; recreate the development or test database before reverting migration 000049'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_goal_context_anchor_fkey,
    DROP CONSTRAINT practice_sessions_context_fields_check;

ALTER TABLE practice_plans
    DROP CONSTRAINT practice_plans_thread_goal_link_fkey,
    DROP CONSTRAINT practice_plans_goal_owner_fkey,
    DROP CONSTRAINT practice_plans_goal_context_anchor_key;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_goal_owner_fkey,
    DROP CONSTRAINT agent_context_manifests_goal_shape_check;

ALTER TABLE agent_memories
    DROP CONSTRAINT agent_memories_goal_owner_fkey,
    DROP CONSTRAINT agent_memories_scope_check;

DROP INDEX agent_memories_one_active_goal_key;
DROP INDEX agent_memories_active_goal_updated_idx;

ALTER TABLE agent_context_manifests
    RENAME COLUMN active_goal_id TO active_matter_id;

ALTER TABLE agent_context_manifests
    RENAME COLUMN active_goal_version TO active_matter_version;

ALTER TABLE agent_memories
    RENAME COLUMN goal_id TO matter_id;

ALTER TABLE practice_plans
    RENAME COLUMN goal_id TO matter_id;

ALTER TABLE practice_sessions
    RENAME COLUMN goal_id TO matter_id;

DROP TABLE goal_agent_create_requests;
DROP TABLE agent_thread_goal_links;
DROP TABLE coaching_goals;

CREATE TABLE matters (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
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

CREATE TABLE agent_thread_matter_links (
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    matter_id uuid NOT NULL,
    is_active boolean NOT NULL DEFAULT false,
    linked_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
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
    CONSTRAINT agent_thread_matter_links_owner_thread_matter_key
        UNIQUE (owner_user_id, thread_id, matter_id),
    CONSTRAINT agent_thread_matter_links_timestamps_check
        CHECK (updated_at >= linked_at)
);

CREATE UNIQUE INDEX agent_thread_matter_links_one_active_idx
    ON agent_thread_matter_links (owner_user_id, thread_id)
    WHERE is_active;

CREATE INDEX agent_thread_matter_links_matter_idx
    ON agent_thread_matter_links (owner_user_id, matter_id, thread_id);

CREATE TABLE matter_agent_create_requests (
    owner_user_id uuid NOT NULL,
    request_id text COLLATE "C" NOT NULL,
    payload_fingerprint bytea NOT NULL,
    matter_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
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

ALTER TABLE agent_context_manifests
    ADD CONSTRAINT agent_context_manifests_matter_owner_fkey
        FOREIGN KEY (active_matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT agent_context_manifests_matter_shape_check
        CHECK (
            (
                active_matter_id IS NULL
                AND active_matter_version IS NULL
            )
            OR
            (
                active_matter_id IS NOT NULL
                AND active_matter_version > 0
            )
        );

ALTER TABLE agent_memories
    ADD CONSTRAINT agent_memories_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT agent_memories_scope_check
        CHECK (
            (scope_type = 'user' AND matter_id IS NULL)
            OR (scope_type = 'matter' AND matter_id IS NOT NULL)
        );

CREATE UNIQUE INDEX agent_memories_one_active_matter_key
    ON agent_memories (
        owner_user_id,
        matter_id,
        memory_type,
        canonical_key
    )
    WHERE status = 'active' AND scope_type = 'matter';

CREATE INDEX agent_memories_active_matter_updated_idx
    ON agent_memories (
        owner_user_id,
        matter_id,
        updated_at DESC,
        id DESC
    )
    WHERE status = 'active' AND scope_type = 'matter';

ALTER TABLE practice_plans
    ADD CONSTRAINT practice_plans_context_anchor_key
        UNIQUE (
            owner_user_id,
            plan_id,
            agent_thread_id,
            matter_id
        ),
    ADD CONSTRAINT practice_plans_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_plans_thread_matter_link_fkey
        FOREIGN KEY (owner_user_id, agent_thread_id, matter_id)
        REFERENCES agent_thread_matter_links (
            owner_user_id,
            thread_id,
            matter_id
        )
        ON DELETE RESTRICT;

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_context_anchor_fkey
        FOREIGN KEY (
            owner_user_id,
            context_plan_id,
            agent_thread_id,
            matter_id
        )
        REFERENCES practice_plans (
            owner_user_id,
            plan_id,
            agent_thread_id,
            matter_id
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            (
                context_plan_id IS NULL
                AND agent_thread_id IS NULL
                AND matter_id IS NULL
                AND snapshot_id IS NULL
                AND scenario_type IS NULL
                AND scenario_model IS NULL
            )
            OR
            (
                context_plan_id IS NOT NULL
                AND agent_thread_id IS NOT NULL
                AND snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND scenario_type IS NOT NULL
                AND btrim(scenario_type) <> ''
                AND scenario_model IS NOT NULL
                AND btrim(scenario_model) <> ''
                AND plan_id = context_plan_id
            )
        );

COMMIT;
