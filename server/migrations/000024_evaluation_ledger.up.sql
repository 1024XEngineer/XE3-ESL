BEGIN;

CREATE TABLE evaluation_ledgers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL,
    root_idempotency_key bytea NOT NULL,
    root_request_fingerprint bytea NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    input_snapshot_id text COLLATE "C" NOT NULL,
    input_revision integer NOT NULL,
    scope text COLLATE "C" NOT NULL,
    scene_type text COLLATE "C" NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_ledgers_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT evaluation_ledgers_owner_identity_unique
        UNIQUE (id, owner_user_id),
    CONSTRAINT evaluation_ledgers_root_key_unique
        UNIQUE (owner_user_id, root_idempotency_key),
    CONSTRAINT evaluation_ledgers_root_key_check
        CHECK (octet_length(root_idempotency_key) = 32),
    CONSTRAINT evaluation_ledgers_root_fingerprint_check
        CHECK (octet_length(root_request_fingerprint) = 32),
    CONSTRAINT evaluation_ledgers_practice_session_check
        CHECK (practice_session_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT evaluation_ledgers_input_snapshot_check
        CHECK (input_snapshot_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT evaluation_ledgers_input_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_ledgers_scope_check
        CHECK (scope IN ('TURN', 'SESSION')),
    CONSTRAINT evaluation_ledgers_scene_type_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'INTERVIEW',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
        )
);

CREATE INDEX evaluation_ledgers_owner_created_idx
    ON evaluation_ledgers (owner_user_id, created_at DESC, id DESC);

CREATE TABLE evaluation_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    revision integer NOT NULL,
    supersedes_revision_id uuid,
    channels text[] COLLATE "C" NOT NULL,
    scene_strategy_ref text COLLATE "C",
    core_4d_strategy_ref text COLLATE "C",
    pipeline_version text COLLATE "C" NOT NULL,
    schema_version text COLLATE "C" NOT NULL,
    request_fingerprint bytea NOT NULL,
    client_request_id text COLLATE "C",
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_revisions_ledger_fkey
        FOREIGN KEY (evaluation_id, owner_user_id)
        REFERENCES evaluation_ledgers (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_revisions_identity_unique
        UNIQUE (evaluation_id, id),
    CONSTRAINT evaluation_revisions_number_unique
        UNIQUE (evaluation_id, revision),
    CONSTRAINT evaluation_revisions_supersedes_fkey
        FOREIGN KEY (evaluation_id, supersedes_revision_id)
        REFERENCES evaluation_revisions (evaluation_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT evaluation_revisions_revision_check
        CHECK (revision > 0),
    CONSTRAINT evaluation_revisions_supersedes_shape_check
        CHECK (
            (revision = 1 AND supersedes_revision_id IS NULL)
            OR (revision > 1 AND supersedes_revision_id IS NOT NULL)
        ),
    CONSTRAINT evaluation_revisions_channels_check
        CHECK (
            cardinality(channels) BETWEEN 1 AND 2
            AND channels <@ ARRAY['SCENE', 'CORE_4D']::text[]
            AND (
                cardinality(channels) = 1
                OR channels[1] <> channels[2]
            )
        ),
    CONSTRAINT evaluation_revisions_scene_strategy_check
        CHECK (
            (
                'SCENE' = ANY(channels)
                AND scene_strategy_ref ~
                    '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
            )
            OR (
                NOT ('SCENE' = ANY(channels))
                AND scene_strategy_ref IS NULL
            )
        ),
    CONSTRAINT evaluation_revisions_core_strategy_check
        CHECK (
            (
                'CORE_4D' = ANY(channels)
                AND core_4d_strategy_ref ~
                    '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
            )
            OR (
                NOT ('CORE_4D' = ANY(channels))
                AND core_4d_strategy_ref IS NULL
            )
        ),
    CONSTRAINT evaluation_revisions_pipeline_version_check
        CHECK (
            pipeline_version ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
        ),
    CONSTRAINT evaluation_revisions_schema_version_check
        CHECK (
            schema_version ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'
        ),
    CONSTRAINT evaluation_revisions_fingerprint_check
        CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT evaluation_revisions_client_request_check
        CHECK (
            client_request_id IS NULL
            OR client_request_id ~ '^[A-Za-z0-9._:-]{1,128}$'
        )
);

CREATE FUNCTION evaluation_assert_revision_chain()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_revision integer;
BEGIN
    IF NEW.revision = 1 THEN
        RETURN NEW;
    END IF;

    SELECT revision
      INTO previous_revision
      FROM evaluation_revisions
     WHERE evaluation_id = NEW.evaluation_id
       AND id = NEW.supersedes_revision_id;

    IF previous_revision IS NULL OR previous_revision <> NEW.revision - 1 THEN
        RAISE EXCEPTION 'evaluation revision must supersede its predecessor'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER evaluation_revision_chain
BEFORE INSERT ON evaluation_revisions
FOR EACH ROW
EXECUTE FUNCTION evaluation_assert_revision_chain();

CREATE FUNCTION reject_evaluation_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'evaluation revisions are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_revisions_immutable
BEFORE UPDATE ON evaluation_revisions
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_revision_mutation();

CREATE TABLE evaluation_revision_states (
    revision_id uuid PRIMARY KEY,
    evaluation_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    evaluation_status text COLLATE "C" NOT NULL DEFAULT 'QUEUED',
    is_final boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT evaluation_revision_states_revision_fkey
        FOREIGN KEY (evaluation_id, revision_id)
        REFERENCES evaluation_revisions (evaluation_id, id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_revision_states_ledger_fkey
        FOREIGN KEY (evaluation_id, owner_user_id)
        REFERENCES evaluation_ledgers (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_revision_states_status_check
        CHECK (
            evaluation_status IN (
                'RECEIVED',
                'VALIDATING',
                'QUEUED',
                'RUNNING',
                'PARTIAL_READY',
                'READY',
                'FAILED',
                'SUPERSEDED'
            )
        ),
    CONSTRAINT evaluation_revision_states_completion_check
        CHECK (
            (
                evaluation_status IN ('READY', 'FAILED', 'SUPERSEDED')
                AND completed_at IS NOT NULL
            )
            OR (
                evaluation_status NOT IN ('READY', 'FAILED', 'SUPERSEDED')
                AND completed_at IS NULL
            )
        ),
    CONSTRAINT evaluation_revision_states_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (completed_at IS NULL OR completed_at >= created_at)
        )
);

CREATE TABLE evaluation_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id uuid NOT NULL,
    evaluation_revision_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    channel text COLLATE "C" NOT NULL,
    channel_key bytea NOT NULL,
    event_type text COLLATE "C" NOT NULL,
    payload jsonb NOT NULL,
    delivery_status text COLLATE "C" NOT NULL DEFAULT 'PENDING',
    attempt_count integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    delivered_at timestamptz,
    CONSTRAINT evaluation_outbox_revision_fkey
        FOREIGN KEY (evaluation_id, evaluation_revision_id)
        REFERENCES evaluation_revisions (evaluation_id, id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_outbox_ledger_fkey
        FOREIGN KEY (evaluation_id, owner_user_id)
        REFERENCES evaluation_ledgers (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_outbox_revision_channel_unique
        UNIQUE (evaluation_revision_id, channel),
    CONSTRAINT evaluation_outbox_channel_key_unique
        UNIQUE (channel_key),
    CONSTRAINT evaluation_outbox_channel_check
        CHECK (channel IN ('SCENE', 'CORE_4D')),
    CONSTRAINT evaluation_outbox_channel_key_check
        CHECK (octet_length(channel_key) = 32),
    CONSTRAINT evaluation_outbox_event_type_check
        CHECK (event_type = 'evaluation.revision.queued'),
    CONSTRAINT evaluation_outbox_payload_check
        CHECK (
            jsonb_typeof(payload) = 'object'
            AND payload ?& ARRAY[
                'evaluation_id',
                'evaluation_revision_id',
                'revision',
                'channel'
            ]
            AND (
                payload - ARRAY[
                    'evaluation_id',
                    'evaluation_revision_id',
                    'revision',
                    'channel'
                ]
            ) = '{}'::jsonb
        ),
    CONSTRAINT evaluation_outbox_delivery_status_check
        CHECK (delivery_status IN ('PENDING', 'DELIVERED')),
    CONSTRAINT evaluation_outbox_attempt_count_check
        CHECK (attempt_count >= 0),
    CONSTRAINT evaluation_outbox_delivery_shape_check
        CHECK (
            (delivery_status = 'PENDING' AND delivered_at IS NULL)
            OR (delivery_status = 'DELIVERED' AND delivered_at IS NOT NULL)
        )
);

CREATE INDEX evaluation_outbox_pending_idx
    ON evaluation_outbox (available_at, created_at, id)
    WHERE delivery_status = 'PENDING';

COMMIT;
