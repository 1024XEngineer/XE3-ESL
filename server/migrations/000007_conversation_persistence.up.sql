BEGIN;

CREATE TABLE conversation_deletion_fences (
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    applied_at timestamptz NOT NULL,
    FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE CASCADE
);

CREATE TABLE conversation_questions (
    owner_user_id uuid NOT NULL,
    question_id text NOT NULL,
    practice_session_id text NOT NULL,
    speaker_participant_id text NOT NULL,
    addressee_participant_ids text[] NOT NULL,
    objective_id text NOT NULL,
    question_type text NOT NULL CHECK (question_type IN ('PRIMARY', 'FOLLOW_UP')),
    parent_question_id text,
    content text NOT NULL CHECK (btrim(content) <> ''),
    sequence integer NOT NULL CHECK (sequence > 0),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, question_id),
    UNIQUE (owner_user_id, practice_session_id, question_id),
    UNIQUE (owner_user_id, practice_session_id, sequence),
    FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CHECK (
        (question_type = 'PRIMARY' AND parent_question_id IS NULL)
        OR
        (question_type = 'FOLLOW_UP' AND parent_question_id IS NOT NULL)
    ),
    FOREIGN KEY (owner_user_id, practice_session_id, parent_question_id)
        REFERENCES conversation_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX conversation_questions_session_order_idx
    ON conversation_questions (owner_user_id, practice_session_id, sequence, question_id);

CREATE TABLE conversation_transcription_reservations (
    owner_user_id uuid NOT NULL,
    reservation_id text NOT NULL,
    question_id text NOT NULL,
    practice_session_id text NOT NULL,
    idempotency_key text NOT NULL,
    input_fingerprint text NOT NULL,
    respondent_participant_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('processing', 'completed', 'failed')),
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    deletion_generation bigint NOT NULL CHECK (deletion_generation >= 0),
    lease_expires_at timestamptz NOT NULL,
    candidate_id text,
    current_attempt_id text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, reservation_id),
    UNIQUE (owner_user_id, idempotency_key),
    FOREIGN KEY (owner_user_id, practice_session_id, question_id)
        REFERENCES conversation_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        ON DELETE CASCADE
);

CREATE INDEX conversation_reservations_recovery_idx
    ON conversation_transcription_reservations
        (owner_user_id, status, lease_expires_at, reservation_id);

CREATE TABLE conversation_processing_attempts (
    owner_user_id uuid NOT NULL,
    attempt_id text NOT NULL,
    reservation_id text NOT NULL,
    operation text NOT NULL CHECK (operation = 'transcription'),
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    status text NOT NULL CHECK (status IN ('processing', 'completed', 'failed', 'expired')),
    lease_expires_at timestamptz NOT NULL,
    error_code text NOT NULL DEFAULT '',
    retryable boolean NOT NULL DEFAULT false,
    provider_request_id text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (owner_user_id, attempt_id),
    UNIQUE (owner_user_id, reservation_id, fencing_token),
    FOREIGN KEY (owner_user_id, reservation_id)
        REFERENCES conversation_transcription_reservations (owner_user_id, reservation_id)
        ON DELETE CASCADE
);

CREATE INDEX conversation_attempts_reservation_idx
    ON conversation_processing_attempts
        (owner_user_id, reservation_id, fencing_token);

CREATE TABLE conversation_transcript_candidates (
    owner_user_id uuid NOT NULL,
    candidate_id text NOT NULL,
    reservation_id text NOT NULL,
    question_id text NOT NULL,
    practice_session_id text NOT NULL,
    respondent_participant_id text NOT NULL,
    transcript_id text NOT NULL,
    evidence_version bigint NOT NULL CHECK (evidence_version > 0),
    provider text NOT NULL,
    model text NOT NULL,
    provider_request_id text NOT NULL,
    transcript_text text NOT NULL CHECK (btrim(transcript_text) <> ''),
    status text NOT NULL CHECK (status IN ('ready', 'confirmed')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, candidate_id),
    UNIQUE (owner_user_id, reservation_id),
    UNIQUE (owner_user_id, transcript_id, evidence_version),
    FOREIGN KEY (owner_user_id, reservation_id)
        REFERENCES conversation_transcription_reservations (owner_user_id, reservation_id)
        ON DELETE CASCADE,
    FOREIGN KEY (owner_user_id, practice_session_id, question_id)
        REFERENCES conversation_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        ON DELETE CASCADE
);

ALTER TABLE conversation_transcription_reservations
    ADD CONSTRAINT conversation_reservation_candidate_fk
    FOREIGN KEY (owner_user_id, candidate_id)
    REFERENCES conversation_transcript_candidates (owner_user_id, candidate_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE conversation_confirmed_turns (
    owner_user_id uuid NOT NULL,
    turn_id text NOT NULL,
    candidate_id text NOT NULL,
    question_id text NOT NULL,
    practice_session_id text NOT NULL,
    speaker_participant_id text NOT NULL,
    addressee_participant_ids text[] NOT NULL,
    respondent_participant_id text NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    interaction_mode text NOT NULL DEFAULT 'PUSH_TO_TALK',
    answer_text text NOT NULL CHECK (btrim(answer_text) <> ''),
    evidence_version bigint NOT NULL CHECK (evidence_version > 0),
    effective_turns integer NOT NULL DEFAULT 0 CHECK (effective_turns >= 0),
    session_completed boolean NOT NULL DEFAULT false,
    progress_recorded_at timestamptz,
    review_id text,
    review_source_turn_id text,
    review_recorded_at timestamptz,
    confirmed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, turn_id),
    UNIQUE (owner_user_id, candidate_id),
    UNIQUE (owner_user_id, practice_session_id, question_id),
    FOREIGN KEY (owner_user_id, candidate_id)
        REFERENCES conversation_transcript_candidates (owner_user_id, candidate_id)
        ON DELETE CASCADE,
    FOREIGN KEY (owner_user_id, practice_session_id, question_id)
        REFERENCES conversation_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        ON DELETE CASCADE,
    CHECK (
        (
            progress_recorded_at IS NULL
            AND effective_turns = 0
            AND session_completed = false
        )
        OR
        (
            progress_recorded_at IS NOT NULL
            AND effective_turns > 0
        )
    ),
    CHECK (
        (review_id IS NULL AND review_source_turn_id IS NULL AND review_recorded_at IS NULL)
        OR
        (review_id IS NOT NULL AND review_source_turn_id IS NOT NULL AND review_recorded_at IS NOT NULL)
    ),
    CHECK (review_source_turn_id IS NULL OR review_source_turn_id = turn_id)
);

CREATE INDEX conversation_turns_session_order_idx
    ON conversation_confirmed_turns
        (owner_user_id, practice_session_id, sequence, created_at, turn_id);

CREATE UNIQUE INDEX conversation_turns_review_owner_idx
    ON conversation_confirmed_turns (owner_user_id, review_id)
    WHERE review_id IS NOT NULL;

CREATE TABLE conversation_turn_confirmations (
    owner_user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    payload_hash text NOT NULL,
    turn_id text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (owner_user_id, idempotency_key),
    FOREIGN KEY (owner_user_id, turn_id)
        REFERENCES conversation_confirmed_turns (owner_user_id, turn_id)
        ON DELETE CASCADE
);

COMMIT;
