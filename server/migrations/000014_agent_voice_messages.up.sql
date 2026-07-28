BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

ALTER TABLE agent_messages
    ADD COLUMN modality text NOT NULL DEFAULT 'text',
    ADD CONSTRAINT agent_messages_modality_check
        CHECK (modality IN ('text', 'voice')),
    ADD CONSTRAINT agent_messages_voice_role_check
        CHECK (modality <> 'voice' OR role = 'user');

CREATE TABLE agent_voice_candidates (
    candidate_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    upload_request_id text COLLATE "C" NOT NULL,
    object_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 text NOT NULL,
    duration_ns bigint NOT NULL,
    sample_rate integer NOT NULL,
    etag text NOT NULL DEFAULT '',
    upload_lease_until timestamptz,
    upload_fencing_token bigint NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'staged',
    asr_attempt integer NOT NULL DEFAULT 0,
    candidate_version bigint NOT NULL DEFAULT 0,
    asr_lease_until timestamptz,
    asr_fencing_token bigint NOT NULL DEFAULT 0,
    cleanup_lease_until timestamptz,
    cleanup_fencing_token bigint NOT NULL DEFAULT 0,
    asr_request_id text,
    asr_provider text,
    asr_model text,
    asr_candidate_text text,
    asr_language text,
    asr_emotion text,
    asr_finish_reason text,
    failure_kind text,
    failure_retryable boolean,
    expires_at timestamptz NOT NULL,
    confirmed_message_id uuid,
    confirmed_run_id uuid,
    message_audio_id uuid,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confirmed_at timestamptz,
    deleted_at timestamptz,
    CONSTRAINT agent_voice_candidates_id_owner_thread_key
        UNIQUE (candidate_id, owner_user_id, thread_id),
    CONSTRAINT agent_voice_candidates_owner_upload_key
        UNIQUE (owner_user_id, thread_id, upload_request_id),
    CONSTRAINT agent_voice_candidates_object_key_key UNIQUE (object_key),
    CONSTRAINT agent_voice_candidates_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_voice_candidates_upload_request_check
        CHECK (
            octet_length(upload_request_id) BETWEEN 8 AND 128
            AND upload_request_id !~ '[[:cntrl:]]'
        ),
    CONSTRAINT agent_voice_candidates_object_key_check
        CHECK (
            octet_length(object_key) BETWEEN 22 AND 1024
            AND object_key LIKE 'audio/v1/agent/%.wav'
            AND object_key NOT LIKE '%..%'
            AND object_key !~ '[[:cntrl:]\\]'
        ),
    CONSTRAINT agent_voice_candidates_audio_metadata_check
        CHECK (
            content_type = 'audio/wav'
            AND size_bytes BETWEEN 1 AND 7400000
            AND checksum_sha256 ~ '^[0-9a-f]{64}$'
            AND duration_ns BETWEEN 1 AND 60000000000
            AND sample_rate BETWEEN 8000 AND 48000
            AND octet_length(etag) <= 512
        ),
    CONSTRAINT agent_voice_candidates_status_check
        CHECK (
            status IN (
                'staged',
                'transcribing',
                'candidate_ready',
                'failed',
                'confirming',
                'confirmed',
                'deleting',
                'deleted'
            )
        ),
    CONSTRAINT agent_voice_candidates_attempt_check
        CHECK (
            asr_attempt >= 0
            AND candidate_version >= 0
            AND candidate_version <= asr_attempt
            AND upload_fencing_token >= 0
            AND asr_fencing_token >= 0
            AND cleanup_fencing_token >= 0
        ),
    CONSTRAINT agent_voice_candidates_upload_lease_check
        CHECK (
            upload_lease_until IS NULL
            OR (
                status = 'staged'
                AND etag = ''
            )
        ),
    CONSTRAINT agent_voice_candidates_cleanup_check
        CHECK (
            cleanup_lease_until IS NULL
            OR status = 'deleting'
        ),
    CONSTRAINT agent_voice_candidates_provider_check
        CHECK (
            (asr_request_id IS NULL OR octet_length(asr_request_id) BETWEEN 1 AND 128)
            AND (asr_provider IS NULL OR asr_provider ~ '^[a-z][a-z0-9_-]{0,63}$')
            AND (
                asr_model IS NULL
                OR asr_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            )
            AND (
                asr_candidate_text IS NULL
                OR (
                    char_length(asr_candidate_text) BETWEEN 1 AND 4096
                    AND octet_length(asr_candidate_text) <= 16384
                    AND asr_candidate_text !~ '^[[:space:]]*$'
                )
            )
            AND (asr_language IS NULL OR octet_length(asr_language) <= 64)
            AND (asr_emotion IS NULL OR octet_length(asr_emotion) <= 64)
            AND (asr_finish_reason IS NULL OR octet_length(asr_finish_reason) <= 64)
        ),
    CONSTRAINT agent_voice_candidates_failure_check
        CHECK (
            (
                failure_kind IS NULL
                AND failure_retryable IS NULL
            )
            OR
            (
                failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
                AND failure_retryable IS NOT NULL
            )
        ),
    CONSTRAINT agent_voice_candidates_state_shape_check
        CHECK (
            (
                status = 'staged'
                AND asr_lease_until IS NULL
                AND asr_candidate_text IS NULL
                AND failure_kind IS NULL
            )
            OR
            (
                status = 'transcribing'
                AND etag <> ''
                AND asr_attempt > 0
                AND candidate_version = asr_attempt
                AND asr_lease_until IS NOT NULL
                AND asr_candidate_text IS NULL
                AND failure_kind IS NULL
            )
            OR
            (
                status IN ('candidate_ready', 'confirming', 'confirmed')
                AND etag <> ''
                AND asr_attempt > 0
                AND candidate_version = asr_attempt
                AND asr_lease_until IS NULL
                AND asr_request_id IS NOT NULL
                AND asr_provider IS NOT NULL
                AND asr_model IS NOT NULL
                AND asr_candidate_text IS NOT NULL
                AND failure_kind IS NULL
            )
            OR
            (
                status = 'failed'
                AND etag <> ''
                AND asr_attempt > 0
                AND candidate_version = asr_attempt
                AND asr_lease_until IS NULL
                AND asr_candidate_text IS NULL
                AND failure_kind IS NOT NULL
            )
            OR status IN ('deleting', 'deleted')
        ),
    CONSTRAINT agent_voice_candidates_confirmation_shape_check
        CHECK (
            (
                status = 'confirmed'
                AND confirmed_message_id IS NOT NULL
                AND confirmed_run_id IS NOT NULL
                AND message_audio_id IS NOT NULL
                AND confirmed_at IS NOT NULL
            )
            OR
            (
                status <> 'confirmed'
                AND (
                    status IN ('deleting', 'deleted')
                    OR (
                        confirmed_message_id IS NULL
                        AND confirmed_run_id IS NULL
                        AND message_audio_id IS NULL
                        AND confirmed_at IS NULL
                    )
                )
            )
        ),
    CONSTRAINT agent_voice_candidates_timestamps_check
        CHECK (
            expires_at > created_at
            AND updated_at >= created_at
            AND (confirmed_at IS NULL OR confirmed_at >= created_at)
            AND (
                (status = 'deleted' AND deleted_at IS NOT NULL)
                OR (status <> 'deleted' AND deleted_at IS NULL)
            )
        )
);

CREATE TABLE agent_message_audios (
    audio_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    message_id uuid NOT NULL,
    candidate_id uuid NOT NULL,
    object_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 text NOT NULL,
    duration_ns bigint NOT NULL,
    sample_rate integer NOT NULL,
    etag text NOT NULL,
    status text NOT NULL DEFAULT 'readable',
    cleanup_lease_until timestamptz,
    cleanup_fencing_token bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamptz,
    CONSTRAINT agent_message_audios_id_owner_thread_message_key
        UNIQUE (audio_id, owner_user_id, thread_id, message_id),
    CONSTRAINT agent_message_audios_message_key UNIQUE (message_id),
    CONSTRAINT agent_message_audios_candidate_key UNIQUE (candidate_id),
    CONSTRAINT agent_message_audios_object_key_key UNIQUE (object_key),
    CONSTRAINT agent_message_audios_message_owner_fkey
        FOREIGN KEY (message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_message_audios_candidate_owner_fkey
        FOREIGN KEY (candidate_id, owner_user_id, thread_id)
        REFERENCES agent_voice_candidates (
            candidate_id,
            owner_user_id,
            thread_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT agent_message_audios_object_key_check
        CHECK (
            object_key LIKE 'audio/v1/agent/%.wav'
            AND object_key NOT LIKE '%..%'
            AND object_key !~ '[[:cntrl:]\\]'
        ),
    CONSTRAINT agent_message_audios_metadata_check
        CHECK (
            content_type = 'audio/wav'
            AND size_bytes BETWEEN 1 AND 7400000
            AND checksum_sha256 ~ '^[0-9a-f]{64}$'
            AND duration_ns BETWEEN 1 AND 60000000000
            AND sample_rate BETWEEN 8000 AND 48000
            AND octet_length(etag) BETWEEN 1 AND 512
        ),
    CONSTRAINT agent_message_audios_status_check
        CHECK (status IN ('readable', 'deleting', 'deleted')),
    CONSTRAINT agent_message_audios_cleanup_check
        CHECK (
            cleanup_fencing_token >= 0
            AND (
                cleanup_lease_until IS NULL
                OR status = 'deleting'
            )
        ),
    CONSTRAINT agent_message_audios_timestamps_check
        CHECK (
            updated_at >= created_at
            AND (
                (status = 'deleted' AND deleted_at IS NOT NULL)
                OR (status <> 'deleted' AND deleted_at IS NULL)
            )
        )
);

CREATE TABLE agent_voice_transcript_evidence (
    evidence_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    candidate_id uuid NOT NULL,
    candidate_version bigint NOT NULL,
    message_id uuid NOT NULL,
    asr_request_id text NOT NULL,
    asr_provider text NOT NULL,
    asr_model text NOT NULL,
    asr_candidate_text text NOT NULL,
    confirmed_text text NOT NULL,
    language text,
    emotion text,
    finish_reason text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_voice_transcript_evidence_candidate_version_key
        UNIQUE (candidate_id, candidate_version),
    CONSTRAINT agent_voice_transcript_evidence_message_key UNIQUE (message_id),
    CONSTRAINT agent_voice_transcript_evidence_candidate_owner_fkey
        FOREIGN KEY (candidate_id, owner_user_id, thread_id)
        REFERENCES agent_voice_candidates (
            candidate_id,
            owner_user_id,
            thread_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT agent_voice_transcript_evidence_message_owner_fkey
        FOREIGN KEY (message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_voice_transcript_evidence_version_check
        CHECK (candidate_version > 0),
    CONSTRAINT agent_voice_transcript_evidence_provider_check
        CHECK (
            octet_length(asr_request_id) BETWEEN 1 AND 128
            AND asr_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND asr_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_voice_transcript_evidence_text_check
        CHECK (
            char_length(asr_candidate_text) BETWEEN 1 AND 4096
            AND octet_length(asr_candidate_text) <= 16384
            AND asr_candidate_text !~ '^[[:space:]]*$'
            AND char_length(confirmed_text) BETWEEN 1 AND 4096
            AND octet_length(confirmed_text) <= 16384
            AND confirmed_text !~ '^[[:space:]]*$'
        )
);

ALTER TABLE agent_voice_candidates
    ADD CONSTRAINT agent_voice_candidates_confirmed_message_fkey
        FOREIGN KEY (
            confirmed_message_id,
            owner_user_id,
            thread_id
        )
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT agent_voice_candidates_confirmed_run_fkey
        FOREIGN KEY (
            confirmed_run_id,
            owner_user_id,
            thread_id,
            confirmed_message_id
        )
        REFERENCES agent_runs (
            id,
            owner_user_id,
            thread_id,
            input_message_id
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT agent_voice_candidates_message_audio_fkey
        FOREIGN KEY (
            message_audio_id,
            owner_user_id,
            thread_id,
            confirmed_message_id
        )
        REFERENCES agent_message_audios (
            audio_id,
            owner_user_id,
            thread_id,
            message_id
        )
        ON DELETE RESTRICT;

CREATE INDEX agent_voice_candidates_asr_recovery_idx
    ON agent_voice_candidates (asr_lease_until, updated_at, candidate_id)
    WHERE status = 'transcribing';

CREATE INDEX agent_voice_candidates_upload_recovery_idx
    ON agent_voice_candidates (
        upload_lease_until,
        updated_at,
        candidate_id
    )
    WHERE status = 'staged' AND etag = '';

CREATE INDEX agent_voice_candidates_expired_idx
    ON agent_voice_candidates (expires_at, updated_at, candidate_id)
    WHERE status IN (
        'staged',
        'transcribing',
        'candidate_ready',
        'failed'
    );

CREATE INDEX agent_voice_candidates_deleting_idx
    ON agent_voice_candidates (
        cleanup_lease_until,
        updated_at,
        candidate_id
    )
    WHERE status = 'deleting';

CREATE INDEX agent_voice_candidates_owner_cleanup_idx
    ON agent_voice_candidates (
        owner_user_id,
        updated_at,
        candidate_id
    )
    WHERE status <> 'deleted';

CREATE INDEX agent_message_audios_cleanup_idx
    ON agent_message_audios (
        cleanup_lease_until,
        updated_at,
        audio_id
    )
    WHERE status = 'deleting';

COMMIT;
