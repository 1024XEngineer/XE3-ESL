BEGIN;

CREATE TABLE practice_sessions (
    owner_user_id uuid NOT NULL,
    session_id text NOT NULL CHECK (btrim(session_id) <> ''),
    plan_id text NOT NULL CHECK (btrim(plan_id) <> ''),
    status text NOT NULL CHECK (status IN ('active', 'completed')),
    version integer NOT NULL CHECK (version > 0),
    effective_turns integer NOT NULL DEFAULT 0 CHECK (effective_turns >= 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    PRIMARY KEY (owner_user_id, session_id),
    CONSTRAINT practice_sessions_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CHECK (
        (status = 'active' AND completed_at IS NULL)
        OR (status = 'completed' AND completed_at IS NOT NULL)
    )
);

CREATE TABLE practice_deletion_fences (
    -- Deliberately has no Identity FK: the module tombstone must survive
    -- physical removal of the authoritative Identity row.
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX practice_one_active_session_per_plan
    ON practice_sessions (owner_user_id, plan_id)
    WHERE status = 'active';

CREATE INDEX practice_sessions_owner_updated
    ON practice_sessions (owner_user_id, updated_at DESC, session_id);

CREATE TABLE practice_session_snapshots (
    owner_user_id uuid NOT NULL,
    session_id text NOT NULL,
    mode text NOT NULL CHECK (btrim(mode) <> ''),
    target_ids jsonb NOT NULL CHECK (jsonb_typeof(target_ids) = 'array'),
    participants jsonb NOT NULL CHECK (jsonb_typeof(participants) = 'array'),
    turn_limit integer NOT NULL CHECK (turn_limit > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, session_id),
    FOREIGN KEY (owner_user_id, session_id)
        REFERENCES practice_sessions (owner_user_id, session_id)
        ON DELETE CASCADE
);

CREATE FUNCTION reject_practice_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'practice session snapshots are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER practice_session_snapshots_immutable
BEFORE UPDATE ON practice_session_snapshots
FOR EACH ROW
EXECUTE FUNCTION reject_practice_snapshot_mutation();

CREATE TABLE practice_turn_results (
    owner_user_id uuid NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL CHECK (btrim(turn_id) <> ''),
    payload_fingerprint bytea NOT NULL CHECK (octet_length(payload_fingerprint) = 32),
    round_number integer NOT NULL CHECK (round_number > 0),
    effective_turns integer NOT NULL CHECK (effective_turns = round_number),
    session_version integer NOT NULL CHECK (session_version > 1),
    completed boolean NOT NULL,
    completion_token text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, session_id, turn_id),
    CONSTRAINT practice_turn_results_owner_turn_key
        UNIQUE (owner_user_id, turn_id),
    UNIQUE (owner_user_id, session_id, round_number),
    FOREIGN KEY (owner_user_id, session_id)
        REFERENCES practice_sessions (owner_user_id, session_id)
        ON DELETE CASCADE,
    CHECK (
        (completed AND completion_token <> '')
        OR (NOT completed AND completion_token = '')
    )
);

COMMIT;
