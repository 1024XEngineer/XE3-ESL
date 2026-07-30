BEGIN;

ALTER TABLE conversation_confirmed_turns
    DROP CONSTRAINT
        conversation_confirmed_turns_owner_user_id_practice_session_key;

ALTER TABLE conversation_confirmed_turns
    ADD COLUMN turn_kind text COLLATE "C" NOT NULL DEFAULT 'EFFECTIVE',
    ADD COLUMN retry_request_id uuid,
    ADD COLUMN original_turn_id text COLLATE "C",
    ADD COLUMN counts_toward_effective_turn_limit boolean
        NOT NULL DEFAULT true,
    ADD CONSTRAINT conversation_confirmed_turns_kind_check
        CHECK (turn_kind IN ('EFFECTIVE', 'RETRY')),
    ADD CONSTRAINT conversation_confirmed_turns_retry_shape_check
        CHECK (
            (
                turn_kind = 'EFFECTIVE'
                AND retry_request_id IS NULL
                AND original_turn_id IS NULL
                AND counts_toward_effective_turn_limit
            )
            OR
            (
                turn_kind = 'RETRY'
                AND retry_request_id IS NOT NULL
                AND original_turn_id IS NOT NULL
                AND NOT counts_toward_effective_turn_limit
                AND progress_recorded_at IS NULL
                AND effective_turns = 0
                AND NOT session_completed
                AND review_id IS NULL
                AND review_source_turn_id IS NULL
                AND review_recorded_at IS NULL
            )
        ),
    ADD CONSTRAINT conversation_confirmed_turns_original_turn_fkey
        FOREIGN KEY (owner_user_id, original_turn_id)
        REFERENCES conversation_confirmed_turns (owner_user_id, turn_id)
        ON DELETE CASCADE;

CREATE UNIQUE INDEX conversation_effective_turn_question_unique
    ON conversation_confirmed_turns (
        owner_user_id,
        practice_session_id,
        question_id
    )
    WHERE turn_kind = 'EFFECTIVE';

CREATE UNIQUE INDEX conversation_retry_turn_request_unique
    ON conversation_confirmed_turns (owner_user_id, retry_request_id)
    WHERE turn_kind = 'RETRY';

CREATE TABLE conversation_retry_turn_drafts (
    owner_user_id uuid NOT NULL,
    retry_request_id uuid NOT NULL,
    turn_id text COLLATE "C" NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    original_turn_id text COLLATE "C" NOT NULL,
    question_id text COLLATE "C" NOT NULL,
    status text COLLATE "C" NOT NULL DEFAULT 'ANSWERING',
    candidate_id text COLLATE "C",
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    confirmed_at timestamptz,
    PRIMARY KEY (owner_user_id, turn_id),
    CONSTRAINT conversation_retry_turn_drafts_request_unique
        UNIQUE (owner_user_id, retry_request_id),
    CONSTRAINT conversation_retry_turn_drafts_original_fkey
        FOREIGN KEY (owner_user_id, original_turn_id)
        REFERENCES conversation_confirmed_turns (owner_user_id, turn_id)
        ON DELETE CASCADE,
    CONSTRAINT conversation_retry_turn_drafts_question_fkey
        FOREIGN KEY (owner_user_id, practice_session_id, question_id)
        REFERENCES conversation_questions (
            owner_user_id,
            practice_session_id,
            question_id
        )
        ON DELETE CASCADE,
    CONSTRAINT conversation_retry_turn_drafts_candidate_fkey
        FOREIGN KEY (owner_user_id, candidate_id)
        REFERENCES conversation_transcript_candidates (
            owner_user_id,
            candidate_id
        )
        ON DELETE CASCADE,
    CONSTRAINT conversation_retry_turn_drafts_id_check
        CHECK (
            turn_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND original_turn_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND question_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT conversation_retry_turn_drafts_status_check
        CHECK (status IN ('ANSWERING', 'CONFIRMED')),
    CONSTRAINT conversation_retry_turn_drafts_state_check
        CHECK (
            (
                status = 'ANSWERING'
                AND candidate_id IS NULL
                AND confirmed_at IS NULL
            )
            OR
            (
                status = 'CONFIRMED'
                AND candidate_id IS NOT NULL
                AND confirmed_at IS NOT NULL
            )
        ),
    CONSTRAINT conversation_retry_turn_drafts_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (
                confirmed_at IS NULL
                OR confirmed_at >= created_at
            )
        )
);

CREATE UNIQUE INDEX conversation_retry_turn_drafts_candidate_unique
    ON conversation_retry_turn_drafts (owner_user_id, candidate_id)
    WHERE candidate_id IS NOT NULL;

CREATE TABLE practice_retry_turn_authorizations (
    owner_user_id uuid NOT NULL,
    retry_request_id uuid NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    original_turn_id text COLLATE "C" NOT NULL,
    question_id text COLLATE "C" NOT NULL,
    scenario_type text COLLATE "C" NOT NULL,
    scenario_model text COLLATE "C" NOT NULL,
    session_status_at_authorization text COLLATE "C" NOT NULL,
    counts_toward_effective_turn_limit boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, retry_request_id),
    CONSTRAINT practice_retry_turn_authorizations_session_fkey
        FOREIGN KEY (owner_user_id, practice_session_id)
        REFERENCES practice_sessions (owner_user_id, session_id)
        ON DELETE CASCADE,
    CONSTRAINT practice_retry_turn_authorizations_source_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND original_turn_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND question_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND NOT counts_toward_effective_turn_limit
        ),
    CONSTRAINT practice_retry_turn_authorizations_scenario_check
        CHECK (
            (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR
            (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        ),
    CONSTRAINT practice_retry_turn_authorizations_status_check
        CHECK (
            session_status_at_authorization IN (
                'in_progress',
                'completed'
            )
        )
);

CREATE TABLE review_speech_feedback_retry_requests (
    retry_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL,
    feedback_item_id uuid NOT NULL,
    speech_feedback_id uuid NOT NULL,
    idempotency_key text COLLATE "C" NOT NULL,
    request_fingerprint bytea NOT NULL,
    deletion_generation bigint NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    original_turn_id text COLLATE "C" NOT NULL,
    question_id text COLLATE "C" NOT NULL,
    retry_status text COLLATE "C" NOT NULL DEFAULT 'PENDING',
    new_turn_id text COLLATE "C",
    stable_failure_reason text COLLATE "C",
    stable_failure_retryable boolean,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT review_speech_feedback_retry_requests_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT review_speech_feedback_retry_requests_item_fkey
        FOREIGN KEY (
            feedback_item_id,
            owner_user_id,
            speech_feedback_id
        )
        REFERENCES review_speech_feedback_items (
            id,
            owner_user_id,
            speech_feedback_id
        )
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedback_retry_requests_idempotency_unique
        UNIQUE (owner_user_id, idempotency_key),
    CONSTRAINT review_speech_feedback_retry_requests_identity_unique
        UNIQUE (retry_request_id, owner_user_id),
    CONSTRAINT review_speech_feedback_retry_requests_key_check
        CHECK (
            idempotency_key ~ '^[\x21-\x7E]{1,128}$'
            AND octet_length(request_fingerprint) = 32
            AND deletion_generation >= 0
        ),
    CONSTRAINT review_speech_feedback_retry_requests_source_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND original_turn_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND question_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT review_speech_feedback_retry_requests_status_check
        CHECK (
            retry_status IN ('PENDING', 'TURN_CREATED', 'FAILED')
        ),
    CONSTRAINT review_speech_feedback_retry_requests_failure_check
        CHECK (
            stable_failure_reason IS NULL
            OR stable_failure_reason IN (
                'SOURCE_NO_LONGER_AVAILABLE',
                'RETRY_TURN_CREATION_FAILED'
            )
        ),
    CONSTRAINT review_speech_feedback_retry_requests_state_check
        CHECK (
            (
                retry_status = 'PENDING'
                AND new_turn_id IS NULL
                AND stable_failure_reason IS NULL
                AND stable_failure_retryable IS NULL
                AND completed_at IS NULL
            )
            OR
            (
                retry_status = 'TURN_CREATED'
                AND new_turn_id IS NOT NULL
                AND stable_failure_reason IS NULL
                AND stable_failure_retryable IS NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                retry_status = 'FAILED'
                AND new_turn_id IS NULL
                AND stable_failure_reason IS NOT NULL
                AND stable_failure_retryable IS NOT NULL
                AND completed_at IS NOT NULL
            )
        ),
    CONSTRAINT review_speech_feedback_retry_requests_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (
                completed_at IS NULL
                OR completed_at >= created_at
            )
        )
);

CREATE INDEX review_speech_feedback_retry_requests_item_idx
    ON review_speech_feedback_retry_requests (
        owner_user_id,
        feedback_item_id,
        created_at,
        retry_request_id
    );

COMMIT;
