BEGIN;

CREATE TABLE practice_question_tips (
    owner_user_id uuid NOT NULL,
    tip_id text NOT NULL,
    practice_session_id text NOT NULL,
    question_id text NOT NULL,
    idempotency_key text NOT NULL,
    status text NOT NULL CHECK (status IN ('processing', 'completed', 'failed')),
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    deletion_generation bigint NOT NULL CHECK (deletion_generation >= 0),
    lease_expires_at timestamptz NOT NULL,
    content text,
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    provider_request_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    completed_at timestamptz,
    PRIMARY KEY (owner_user_id, tip_id),
    UNIQUE (owner_user_id, practice_session_id, question_id),
    UNIQUE (owner_user_id, idempotency_key),
    FOREIGN KEY (owner_user_id, practice_session_id, question_id)
        REFERENCES practice_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        ON DELETE CASCADE,
    CHECK (
        (status = 'completed' AND content IS NOT NULL AND btrim(content) <> ''
            AND completed_at IS NOT NULL)
        OR
        (status <> 'completed' AND content IS NULL AND completed_at IS NULL)
    )
);

CREATE INDEX practice_question_tips_recovery_idx
    ON practice_question_tips
        (owner_user_id, status, lease_expires_at, tip_id);

COMMIT;
