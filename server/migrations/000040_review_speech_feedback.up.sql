BEGIN;

CREATE TABLE review_speech_feedback_turn_snapshots (
    id text COLLATE "C" PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    turn_id text COLLATE "C" NOT NULL,
    input_revision bigint NOT NULL,
    evidence_ref_id text COLLATE "C" NOT NULL,
    transcript_text text NOT NULL,
    source_digest bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT review_speech_feedback_turn_snapshots_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT review_speech_feedback_turn_snapshots_turn_fkey
        FOREIGN KEY (owner_user_id, turn_id)
        REFERENCES conversation_confirmed_turns (owner_user_id, turn_id)
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedback_turn_snapshots_identity_unique
        UNIQUE (
            id,
            owner_user_id,
            practice_session_id,
            turn_id,
            input_revision
        ),
    CONSTRAINT review_speech_feedback_turn_snapshots_turn_unique
        UNIQUE (owner_user_id, turn_id),
    CONSTRAINT review_speech_feedback_turn_snapshots_id_check
        CHECK (
            id ~ '^[A-Za-z][A-Za-z0-9_-]{0,127}$'
            AND evidence_ref_id ~ '^[A-Za-z][A-Za-z0-9_-]{0,127}$'
        ),
    CONSTRAINT review_speech_feedback_turn_snapshots_source_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND turn_id ~ '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
            AND input_revision > 0
            AND octet_length(transcript_text) BETWEEN 1 AND 16384
            AND transcript_text !~ '^[[:space:]]*$'
            AND octet_length(source_digest) = 32
            AND source_digest <> decode(repeat('00', 32), 'hex')
        )
);

CREATE FUNCTION reject_review_speech_feedback_snapshot_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'SpeechFeedback Turn snapshots are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER review_speech_feedback_turn_snapshots_immutable
BEFORE UPDATE ON review_speech_feedback_turn_snapshots
FOR EACH ROW
EXECUTE FUNCTION reject_review_speech_feedback_snapshot_update();

ALTER TABLE agent_voice_transcript_evidence
    ADD CONSTRAINT agent_voice_transcript_evidence_feedback_identity_key
        UNIQUE (
            evidence_id,
            owner_user_id,
            thread_id,
            message_id,
            candidate_version
        );

CREATE TABLE review_speech_feedbacks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL,
    source_kind text COLLATE "C" NOT NULL,
    practice_session_id text COLLATE "C",
    turn_id text COLLATE "C",
    input_revision bigint,
    evidence_snapshot_id text COLLATE "C",
    thread_id uuid,
    message_id uuid,
    transcript_evidence_id uuid,
    candidate_version bigint,
    source_digest bytea NOT NULL,
    deletion_generation bigint NOT NULL DEFAULT 0,
    schema_version text COLLATE "C" NOT NULL,
    strategy_ref text COLLATE "C" NOT NULL,
    pipeline_version text COLLATE "C" NOT NULL,
    feedback_status text COLLATE "C" NOT NULL DEFAULT 'QUEUED',
    scoreability_status text COLLATE "C",
    gate_status text COLLATE "C",
    reason_codes text[] NOT NULL DEFAULT ARRAY[]::text[],
    stable_failure_code text COLLATE "C",
    stable_failure_retryable boolean,
    attempt_count integer NOT NULL DEFAULT 0,
    fencing_token bigint NOT NULL DEFAULT 0,
    lease_expires_at timestamptz,
    available_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    CONSTRAINT review_speech_feedbacks_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT review_speech_feedbacks_conversation_source_fkey
        FOREIGN KEY (
            evidence_snapshot_id,
            owner_user_id,
            practice_session_id,
            turn_id,
            input_revision
        )
        REFERENCES review_speech_feedback_turn_snapshots (
            id,
            owner_user_id,
            practice_session_id,
            turn_id,
            input_revision
        )
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedbacks_agent_source_fkey
        FOREIGN KEY (
            transcript_evidence_id,
            owner_user_id,
            thread_id,
            message_id,
            candidate_version
        )
        REFERENCES agent_voice_transcript_evidence (
            evidence_id,
            owner_user_id,
            thread_id,
            message_id,
            candidate_version
        )
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedbacks_identity_unique
        UNIQUE (id, owner_user_id),
    CONSTRAINT review_speech_feedbacks_source_kind_check
        CHECK (
            (
                source_kind = 'CONVERSATION_TURN'
                AND practice_session_id IS NOT NULL
                AND turn_id IS NOT NULL
                AND input_revision IS NOT NULL
                AND evidence_snapshot_id IS NOT NULL
                AND thread_id IS NULL
                AND message_id IS NULL
                AND transcript_evidence_id IS NULL
                AND candidate_version IS NULL
            )
            OR
            (
                source_kind = 'AGENT_VOICE_MESSAGE'
                AND practice_session_id IS NULL
                AND turn_id IS NULL
                AND input_revision IS NULL
                AND evidence_snapshot_id IS NULL
                AND thread_id IS NOT NULL
                AND message_id IS NOT NULL
                AND transcript_evidence_id IS NOT NULL
                AND candidate_version IS NOT NULL
            )
        ),
    CONSTRAINT review_speech_feedbacks_source_revision_check
        CHECK (
            (input_revision IS NULL OR input_revision > 0)
            AND (candidate_version IS NULL OR candidate_version > 0)
            AND octet_length(source_digest) = 32
            AND source_digest <> decode(repeat('00', 32), 'hex')
            AND deletion_generation >= 0
        ),
    CONSTRAINT review_speech_feedbacks_version_check
        CHECK (
            schema_version = 'speech-feedback/v1'
            AND strategy_ref =
                'qianwen-speech-feedback/v1'
            AND pipeline_version =
                'speech-feedback-pipeline/v1'
        ),
    CONSTRAINT review_speech_feedbacks_status_check
        CHECK (feedback_status IN ('QUEUED', 'RUNNING', 'READY', 'FAILED')),
    CONSTRAINT review_speech_feedbacks_scoreability_check
        CHECK (
            scoreability_status IS NULL
            OR scoreability_status IN ('PROVISIONAL', 'INSUFFICIENT')
        ),
    CONSTRAINT review_speech_feedbacks_gate_check
        CHECK (
            gate_status IS NULL
            OR gate_status IN ('FEEDBACK_ONLY', 'BLOCKED')
        ),
    CONSTRAINT review_speech_feedbacks_reason_codes_check
        CHECK (
            cardinality(reason_codes) <= 3
            AND
            reason_codes <@ ARRAY[
                'TEXT_TOO_SHORT',
                'TRANSCRIPT_CONFIDENCE_INSUFFICIENT',
                'EVIDENCE_INCONSISTENT'
            ]::text[]
        ),
    CONSTRAINT review_speech_feedbacks_failure_check
        CHECK (
            (
                stable_failure_code IS NULL
                AND stable_failure_retryable IS NULL
            )
            OR
            (
                stable_failure_code IN (
                    'PROVIDER_UNAVAILABLE',
                    'PROVIDER_RESPONSE_INVALID',
                    'PROCESSING_TIMEOUT',
                    'INTERNAL_PROCESSING_ERROR'
                )
                AND stable_failure_retryable IS NOT NULL
            )
        ),
    CONSTRAINT review_speech_feedbacks_attempt_check
        CHECK (
            attempt_count >= 0
            AND fencing_token >= 0
            AND fencing_token >= attempt_count
        ),
    CONSTRAINT review_speech_feedbacks_state_shape_check
        CHECK (
            (
                feedback_status = 'QUEUED'
                AND scoreability_status IS NULL
                AND gate_status IS NULL
                AND cardinality(reason_codes) = 0
                AND stable_failure_code IS NULL
                AND stable_failure_retryable IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NULL
            )
            OR
            (
                feedback_status = 'RUNNING'
                AND scoreability_status IS NULL
                AND gate_status IS NULL
                AND cardinality(reason_codes) = 0
                AND stable_failure_code IS NULL
                AND stable_failure_retryable IS NULL
                AND attempt_count > 0
                AND fencing_token > 0
                AND lease_expires_at IS NOT NULL
                AND completed_at IS NULL
            )
            OR
            (
                feedback_status = 'READY'
                AND (
                    (
                        scoreability_status = 'PROVISIONAL'
                        AND gate_status = 'FEEDBACK_ONLY'
                        AND cardinality(reason_codes) = 0
                    )
                    OR
                    (
                        scoreability_status = 'INSUFFICIENT'
                        AND gate_status = 'BLOCKED'
                        AND cardinality(reason_codes) > 0
                    )
                )
                AND stable_failure_code IS NULL
                AND stable_failure_retryable IS NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
            )
            OR
            (
                feedback_status = 'FAILED'
                AND scoreability_status IS NULL
                AND gate_status IS NULL
                AND cardinality(reason_codes) = 0
                AND stable_failure_code IS NOT NULL
                AND stable_failure_retryable IS NOT NULL
                AND lease_expires_at IS NULL
                AND completed_at IS NOT NULL
            )
        ),
    CONSTRAINT review_speech_feedbacks_timestamps_check
        CHECK (
            updated_at >= created_at
            AND available_at >= created_at
            AND (completed_at IS NULL OR completed_at >= created_at)
        )
);

CREATE UNIQUE INDEX review_speech_feedbacks_conversation_source_unique
    ON review_speech_feedbacks (owner_user_id, turn_id)
    WHERE source_kind = 'CONVERSATION_TURN';

CREATE UNIQUE INDEX review_speech_feedbacks_agent_source_unique
    ON review_speech_feedbacks (owner_user_id, message_id)
    WHERE source_kind = 'AGENT_VOICE_MESSAGE';

CREATE INDEX review_speech_feedbacks_pending_idx
    ON review_speech_feedbacks (
        available_at,
        lease_expires_at,
        created_at,
        id
    )
    WHERE feedback_status IN ('QUEUED', 'RUNNING');

CREATE TABLE review_speech_feedback_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    speech_feedback_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    kind text COLLATE "C" NOT NULL,
    anchor_kind text COLLATE "C" NOT NULL,
    evidence_ref_id text COLLATE "C",
    turn_id text COLLATE "C",
    transcript_evidence_id uuid,
    message_id uuid,
    start_utf8_byte integer NOT NULL,
    end_utf8_byte integer NOT NULL,
    original_excerpt text NOT NULL,
    explanation text NOT NULL,
    suggested_text text,
    repractice_mode text COLLATE "C" NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT review_speech_feedback_items_feedback_fkey
        FOREIGN KEY (speech_feedback_id, owner_user_id)
        REFERENCES review_speech_feedbacks (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT review_speech_feedback_items_identity_unique
        UNIQUE (id, owner_user_id, speech_feedback_id),
    CONSTRAINT review_speech_feedback_items_kind_check
        CHECK (
            kind IN (
                'CORRECTION',
                'STRENGTH',
                'IMPROVEMENT',
                'RECOMMENDED_EXPRESSION'
            )
        ),
    CONSTRAINT review_speech_feedback_items_anchor_check
        CHECK (
            (
                anchor_kind = 'CONVERSATION_TRANSCRIPT'
                AND evidence_ref_id IS NOT NULL
                AND turn_id IS NOT NULL
                AND transcript_evidence_id IS NULL
                AND message_id IS NULL
            )
            OR
            (
                anchor_kind = 'AGENT_TRANSCRIPT'
                AND evidence_ref_id IS NULL
                AND turn_id IS NULL
                AND transcript_evidence_id IS NOT NULL
                AND message_id IS NOT NULL
            )
        ),
    CONSTRAINT review_speech_feedback_items_span_check
        CHECK (
            start_utf8_byte >= 0
            AND end_utf8_byte > start_utf8_byte
            AND octet_length(original_excerpt) BETWEEN 1 AND 16384
        ),
    CONSTRAINT review_speech_feedback_items_content_check
        CHECK (
            octet_length(explanation) BETWEEN 1 AND 2048
            AND explanation !~ '^[[:space:]]*$'
            AND (
                suggested_text IS NULL
                OR (
                    octet_length(suggested_text) BETWEEN 1 AND 2048
                    AND suggested_text !~ '^[[:space:]]*$'
                )
            )
        ),
    CONSTRAINT review_speech_feedback_items_suggestion_check
        CHECK (
            (
                kind = 'STRENGTH'
                AND suggested_text IS NULL
                AND repractice_mode = 'NONE'
            )
            OR
            (
                kind <> 'STRENGTH'
                AND suggested_text IS NOT NULL
                AND repractice_mode IN ('SAME_QUESTION', 'SAME_THREAD')
            )
        )
);

CREATE INDEX review_speech_feedback_items_feedback_idx
    ON review_speech_feedback_items (
        owner_user_id,
        speech_feedback_id,
        created_at,
        id
    );

COMMIT;
