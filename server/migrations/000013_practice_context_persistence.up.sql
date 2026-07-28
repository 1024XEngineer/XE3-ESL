BEGIN;

CREATE TABLE preparation_deletion_fences (
    -- This module tombstone deliberately outlives the Identity row.
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (updated_at >= created_at)
);

CREATE TABLE preparation_profiles (
    owner_user_id uuid NOT NULL,
    profile_id text NOT NULL CHECK (btrim(profile_id) <> ''),
    resume_ref text,
    job_description_ref text,
    background_summary text NOT NULL,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, profile_id),
    CONSTRAINT preparation_profiles_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_profiles_optional_refs_check
        CHECK (
            (resume_ref IS NULL OR btrim(resume_ref) <> '')
            AND
            (job_description_ref IS NULL OR btrim(job_description_ref) <> '')
        ),
    CONSTRAINT preparation_profiles_background_check
        CHECK (btrim(background_summary) <> ''),
    CONSTRAINT preparation_profiles_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX preparation_profiles_owner_updated_idx
    ON preparation_profiles (owner_user_id, updated_at DESC, profile_id);

CREATE TABLE preparation_snapshots (
    owner_user_id uuid NOT NULL,
    snapshot_id text NOT NULL CHECK (btrim(snapshot_id) <> ''),
    source_profile_id text NOT NULL,
    source_version integer NOT NULL CHECK (source_version > 0),
    resume_snapshot text,
    job_description_snapshot text,
    background_snapshot text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, snapshot_id),
    CONSTRAINT preparation_snapshots_source_profile_fkey
        FOREIGN KEY (owner_user_id, source_profile_id)
        REFERENCES preparation_profiles (owner_user_id, profile_id)
        ON DELETE CASCADE,
    CONSTRAINT preparation_snapshots_optional_content_check
        CHECK (
            (resume_snapshot IS NULL OR btrim(resume_snapshot) <> '')
            AND
            (
                job_description_snapshot IS NULL
                OR btrim(job_description_snapshot) <> ''
            )
        ),
    CONSTRAINT preparation_snapshots_background_check
        CHECK (btrim(background_snapshot) <> '')
);

CREATE INDEX preparation_snapshots_owner_profile_idx
    ON preparation_snapshots (
        owner_user_id,
        source_profile_id,
        created_at,
        snapshot_id
    );

CREATE FUNCTION reject_preparation_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'preparation snapshots are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER preparation_snapshots_immutable
BEFORE UPDATE ON preparation_snapshots
FOR EACH ROW
EXECUTE FUNCTION reject_preparation_snapshot_mutation();

CREATE TABLE preparation_idempotency_records (
    owner_user_id uuid NOT NULL,
    method text NOT NULL,
    canonical_path text NOT NULL,
    idempotency_key text COLLATE "C" NOT NULL,
    payload_fingerprint bytea NOT NULL,
    resource_kind text NOT NULL,
    resource_id text NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (
        owner_user_id,
        method,
        canonical_path,
        idempotency_key
    ),
    CONSTRAINT preparation_idempotency_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_idempotency_method_check
        CHECK (method ~ '^[A-Z]+$'),
    CONSTRAINT preparation_idempotency_path_check
        CHECK (
            left(canonical_path, 1) = '/'
            AND btrim(canonical_path) = canonical_path
        ),
    CONSTRAINT preparation_idempotency_key_check
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128),
    CONSTRAINT preparation_idempotency_fingerprint_check
        CHECK (octet_length(payload_fingerprint) = 32),
    CONSTRAINT preparation_idempotency_resource_check
        CHECK (
            resource_kind IN ('profile', 'snapshot')
            AND btrim(resource_id) <> ''
        ),
    CONSTRAINT preparation_idempotency_response_check
        CHECK (
            response_status BETWEEN 200 AND 299
            AND jsonb_typeof(response_body) = 'object'
        )
);

ALTER TABLE agent_thread_matter_links
    ADD CONSTRAINT agent_thread_matter_links_owner_thread_matter_key
    UNIQUE (owner_user_id, thread_id, matter_id);

CREATE TABLE practice_plans (
    owner_user_id uuid NOT NULL,
    plan_id text NOT NULL CHECK (btrim(plan_id) <> ''),
    agent_thread_id uuid NOT NULL,
    matter_id uuid NOT NULL,
    scenario_definition_id text NOT NULL,
    scenario_definition_version integer NOT NULL
        CHECK (scenario_definition_version > 0),
    scenario_type text NOT NULL,
    scenario_config_id text NOT NULL,
    scenario_config_version integer NOT NULL
        CHECK (scenario_config_version > 0),
    preparation_profile_id text NOT NULL,
    selected_role_ids jsonb NOT NULL,
    plan_revision integer NOT NULL DEFAULT 1 CHECK (plan_revision > 0),
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, plan_id),
    CONSTRAINT practice_plans_context_anchor_key
        UNIQUE (
            owner_user_id,
            plan_id,
            agent_thread_id,
            matter_id
        ),
    CONSTRAINT practice_plans_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_thread_owner_fkey
        FOREIGN KEY (agent_thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_matter_owner_fkey
        FOREIGN KEY (matter_id, owner_user_id)
        REFERENCES matters (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_thread_matter_link_fkey
        FOREIGN KEY (owner_user_id, agent_thread_id, matter_id)
        REFERENCES agent_thread_matter_links (
            owner_user_id,
            thread_id,
            matter_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_profile_owner_fkey
        FOREIGN KEY (owner_user_id, preparation_profile_id)
        REFERENCES preparation_profiles (owner_user_id, profile_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_catalog_fields_check
        CHECK (
            btrim(scenario_definition_id) <> ''
            AND btrim(scenario_type) <> ''
            AND btrim(scenario_config_id) <> ''
            AND jsonb_typeof(selected_role_ids) = 'array'
            AND jsonb_array_length(selected_role_ids) > 0
        ),
    CONSTRAINT practice_plans_status_check
        CHECK (
            status IN (
                'configuring',
                'configuration_failed',
                'ready',
                'archived'
            )
        ),
    CONSTRAINT practice_plans_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX practice_plans_owner_updated_idx
    ON practice_plans (owner_user_id, updated_at DESC, plan_id);

CREATE INDEX practice_plans_owner_thread_idx
    ON practice_plans (
        owner_user_id,
        agent_thread_id,
        matter_id,
        updated_at DESC,
        plan_id
    );

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_status_check,
    DROP CONSTRAINT practice_sessions_check,
    ALTER COLUMN started_at DROP NOT NULL,
    ADD COLUMN context_plan_id text,
    ADD COLUMN agent_thread_id uuid,
    ADD COLUMN matter_id uuid,
    ADD COLUMN snapshot_id text,
    ADD COLUMN scenario_type text,
    ADD COLUMN end_reason text,
    ADD CONSTRAINT practice_sessions_context_plan_fkey
        FOREIGN KEY (owner_user_id, context_plan_id)
        REFERENCES practice_plans (owner_user_id, plan_id)
        ON DELETE RESTRICT,
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
    ADD CONSTRAINT practice_sessions_context_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_sessions_context_binding_key
        UNIQUE (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        ),
    ADD CONSTRAINT practice_sessions_status_check
        CHECK (
            status IN (
                'active',
                'starting',
                'in_progress',
                'paused',
                'completed',
                'ended_early'
            )
        ),
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            (
                context_plan_id IS NULL
                AND agent_thread_id IS NULL
                AND matter_id IS NULL
                AND snapshot_id IS NULL
                AND scenario_type IS NULL
            )
            OR
            (
                context_plan_id IS NOT NULL
                AND agent_thread_id IS NOT NULL
                AND matter_id IS NOT NULL
                AND snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND scenario_type IS NOT NULL
                AND btrim(scenario_type) <> ''
                AND plan_id = context_plan_id
            )
        ),
    ADD CONSTRAINT practice_sessions_lifecycle_check
        CHECK (
            (
                context_plan_id IS NULL
                AND (
                    (status = 'active' AND completed_at IS NULL)
                    OR
                    (status = 'completed' AND completed_at IS NOT NULL)
                )
            )
            OR
            (
                context_plan_id IS NOT NULL
                AND (
                    (
                        status = 'starting'
                        AND started_at IS NULL
                        AND completed_at IS NULL
                        AND end_reason IS NULL
                    )
                    OR
                    (
                        status IN ('in_progress', 'paused')
                        AND started_at IS NOT NULL
                        AND completed_at IS NULL
                        AND end_reason IS NULL
                    )
                    OR
                    (
                        status IN ('completed', 'ended_early')
                        AND started_at IS NOT NULL
                        AND completed_at IS NOT NULL
                        AND end_reason IS NOT NULL
                        AND btrim(end_reason) <> ''
                    )
                )
            )
        );

CREATE UNIQUE INDEX practice_one_effective_session_per_context_plan
    ON practice_sessions (owner_user_id, context_plan_id)
    WHERE
        context_plan_id IS NOT NULL
        AND status IN ('starting', 'in_progress', 'paused');

CREATE UNIQUE INDEX practice_one_effective_session_per_agent_thread
    ON practice_sessions (owner_user_id, agent_thread_id)
    WHERE
        context_plan_id IS NOT NULL
        AND status IN ('starting', 'in_progress', 'paused');

ALTER TABLE practice_session_snapshots
    ADD COLUMN snapshot_id text,
    ADD COLUMN context_plan_id text,
    ADD COLUMN preparation_snapshot_id text,
    ADD COLUMN snapshot_document jsonb,
    ADD CONSTRAINT practice_session_snapshots_context_plan_fkey
        FOREIGN KEY (owner_user_id, context_plan_id)
        REFERENCES practice_plans (owner_user_id, plan_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_session_snapshots_preparation_fkey
        FOREIGN KEY (owner_user_id, preparation_snapshot_id)
        REFERENCES preparation_snapshots (owner_user_id, snapshot_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_session_snapshots_context_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_session_snapshots_context_binding_key
        UNIQUE (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        ),
    ADD CONSTRAINT practice_session_snapshots_context_fields_check
        CHECK (
            (
                snapshot_id IS NULL
                AND context_plan_id IS NULL
                AND preparation_snapshot_id IS NULL
                AND snapshot_document IS NULL
            )
            OR
            (
                snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND context_plan_id IS NOT NULL
                AND preparation_snapshot_id IS NOT NULL
                AND snapshot_document IS NOT NULL
                AND jsonb_typeof(snapshot_document) = 'object'
            )
        ),
    ADD CONSTRAINT practice_session_snapshots_context_session_fkey
        FOREIGN KEY (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        REFERENCES practice_sessions (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_context_snapshot_fkey
        FOREIGN KEY (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        REFERENCES practice_session_snapshots (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE practice_idempotency_records (
    owner_user_id uuid NOT NULL,
    method text NOT NULL,
    canonical_path text NOT NULL,
    idempotency_key text COLLATE "C" NOT NULL,
    payload_fingerprint bytea NOT NULL,
    resource_kind text NOT NULL,
    resource_id text NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (
        owner_user_id,
        method,
        canonical_path,
        idempotency_key
    ),
    CONSTRAINT practice_idempotency_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_idempotency_method_check
        CHECK (method ~ '^[A-Z]+$'),
    CONSTRAINT practice_idempotency_path_check
        CHECK (
            left(canonical_path, 1) = '/'
            AND btrim(canonical_path) = canonical_path
        ),
    CONSTRAINT practice_idempotency_key_check
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128),
    CONSTRAINT practice_idempotency_fingerprint_check
        CHECK (octet_length(payload_fingerprint) = 32),
    CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN ('plan', 'session', 'pause', 'resume', 'end_early')
            AND btrim(resource_id) <> ''
        ),
    CONSTRAINT practice_idempotency_response_check
        CHECK (
            response_status BETWEEN 200 AND 299
            AND jsonb_typeof(response_body) = 'object'
        )
);

COMMIT;
