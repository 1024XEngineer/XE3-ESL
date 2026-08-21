BEGIN;

CREATE TABLE pending_practice_actions (
    pending_action_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id uuid NOT NULL,
    source_run_id uuid NOT NULL,
    source_input_message_id uuid NOT NULL,
    source_input_sequence bigint NOT NULL CHECK (source_input_sequence > 0),
    proposal jsonb NOT NULL CHECK (jsonb_typeof(proposal) = 'object'),
    proposal_fingerprint bytea NOT NULL CHECK (octet_length(proposal_fingerprint) = 32),
    state text NOT NULL CHECK (state IN ('OPEN', 'CONFIRMING', 'CONFIRMED', 'REJECTED', 'SUPERSEDED')),
    resolution_input_message_id uuid,
    resolved_plan_id uuid,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at timestamptz,
    CONSTRAINT pending_practice_actions_thread_fk
        FOREIGN KEY (thread_id, owner_id) REFERENCES agent_threads(id, user_id) ON DELETE CASCADE,
    CONSTRAINT pending_practice_actions_source_run_fk
        FOREIGN KEY (source_run_id, thread_id) REFERENCES agent_runs(id, thread_id) ON DELETE CASCADE,
    CONSTRAINT pending_practice_actions_source_message_fk
        FOREIGN KEY (source_input_message_id, thread_id) REFERENCES agent_messages(id, thread_id) ON DELETE CASCADE,
    CONSTRAINT pending_practice_actions_resolution_message_fk
        FOREIGN KEY (resolution_input_message_id, thread_id) REFERENCES agent_messages(id, thread_id) ON DELETE CASCADE,
    CONSTRAINT pending_practice_actions_plan_fk
        FOREIGN KEY (resolved_plan_id, owner_id) REFERENCES practice_plans(plan_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT pending_practice_actions_state_shape CHECK (
        (state = 'OPEN' AND resolution_input_message_id IS NULL AND resolved_plan_id IS NULL AND resolved_at IS NULL) OR
        (state = 'CONFIRMING' AND resolution_input_message_id IS NOT NULL AND resolved_plan_id IS NULL AND resolved_at IS NULL) OR
        (state = 'CONFIRMED' AND resolution_input_message_id IS NOT NULL AND resolved_plan_id IS NOT NULL AND resolved_at IS NOT NULL) OR
        (state = 'REJECTED' AND resolution_input_message_id IS NOT NULL AND resolved_plan_id IS NULL AND resolved_at IS NOT NULL) OR
        (state = 'SUPERSEDED' AND resolution_input_message_id IS NULL AND resolved_plan_id IS NULL AND resolved_at IS NOT NULL)
    ),
    UNIQUE (owner_id, thread_id, source_input_message_id)
);

CREATE UNIQUE INDEX pending_practice_actions_one_open_per_thread
    ON pending_practice_actions(owner_id, thread_id)
    WHERE state = 'OPEN';

CREATE INDEX pending_practice_actions_reply_lookup
    ON pending_practice_actions(owner_id, thread_id, source_input_sequence, state);

COMMIT;
