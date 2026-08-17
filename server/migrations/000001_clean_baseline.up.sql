BEGIN;

CREATE TABLE users (
    id uuid PRIMARY KEY,
    canonical_email text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'active',
    display_name text,
    profile_version bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_email_check CHECK (
        octet_length(canonical_email) BETWEEN 3 AND 254
        AND canonical_email !~ '[^\x21-\x7e]'
        AND canonical_email ~ '^[^@]+@[^@]+$'
        AND canonical_email = lower(canonical_email)
    ),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'deleting')),
    CONSTRAINT users_profile_check CHECK (
        (display_name IS NULL AND profile_version = 0)
        OR (
            display_name IS NOT NULL AND profile_version >= 1
            AND char_length(display_name) BETWEEN 1 AND 40
            AND octet_length(display_name) <= 120
            AND display_name !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT users_timestamps_check CHECK (updated_at >= created_at)
);

CREATE TABLE credentials (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT credentials_password_hash_check CHECK (
        octet_length(password_hash) BETWEEN 64 AND 512
        AND password_hash !~ '[^\x21-\x7e]'
        AND password_hash ~ '^\$argon2id\$v=[0-9]+\$[A-Za-z0-9._-]+=[A-Za-z0-9._-]+(,[A-Za-z0-9._-]+=[A-Za-z0-9._-]+)*\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$'
        AND octet_length(split_part(password_hash, '$', 5)) % 4 <> 1
        AND octet_length(split_part(password_hash, '$', 6)) % 4 <> 1
    )
);

CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_digest bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revocation_reason text,
    CONSTRAINT auth_sessions_token_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT auth_sessions_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT auth_sessions_revocation_check CHECK (
        (revoked_at IS NULL AND revocation_reason IS NULL)
        OR (
            revoked_at >= created_at
            AND revocation_reason ~ '^[a-z][a-z0-9_]{0,63}$'
        )
    )
);

CREATE INDEX auth_sessions_user_created_idx
    ON auth_sessions (user_id, created_at DESC);
CREATE INDEX auth_sessions_active_user_idx
    ON auth_sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX auth_sessions_active_expiry_idx
    ON auth_sessions (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE coaching_user_profiles (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    memory_enabled boolean NOT NULL DEFAULT true,
    profile jsonb NOT NULL DEFAULT '{}'::jsonb,
    field_sources jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT coaching_user_profiles_profile_check CHECK (
        jsonb_typeof(profile) = 'object'
        AND octet_length(profile::text) <= 8192
        AND profile - ARRAY[
            'form_of_address', 'occupation', 'professional_context',
            'native_language', 'explanation_language', 'response_detail',
            'interests'
        ] = '{}'::jsonb
    ),
    CONSTRAINT coaching_user_profiles_sources_check CHECK (
        jsonb_typeof(field_sources) = 'object'
        AND octet_length(field_sources::text) <= 16384
        AND field_sources - ARRAY[
            'form_of_address', 'occupation', 'professional_context',
            'native_language', 'explanation_language', 'response_detail',
            'interests'
        ] = '{}'::jsonb
    ),
    CONSTRAINT coaching_user_profiles_version_check CHECK (version > 0),
    CONSTRAINT coaching_user_profiles_timestamps_check CHECK (updated_at >= created_at)
);

CREATE TABLE agent_threads (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title text,
    next_message_sequence bigint NOT NULL DEFAULT 1,
    summary_content jsonb,
    summary_through_sequence bigint,
    summary_target_sequence bigint,
    summary_attempt_count integer NOT NULL DEFAULT 0,
    summary_lease_token uuid,
    summary_lease_expires_at timestamptz,
    summary_available_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    summary_error text,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_threads_id_user_key UNIQUE (id, user_id),
    CONSTRAINT agent_threads_title_check CHECK (
        title IS NULL OR (
            char_length(title) BETWEEN 1 AND 32
            AND octet_length(title) <= 128
            AND title = btrim(title)
            AND title !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT agent_threads_sequence_check CHECK (next_message_sequence > 0),
    CONSTRAINT agent_threads_summary_attempt_check CHECK (summary_attempt_count >= 0),
    CONSTRAINT agent_threads_summary_content_check CHECK (
        (summary_content IS NULL AND summary_through_sequence IS NULL)
        OR (
            summary_through_sequence >= 1
            AND jsonb_typeof(summary_content) = 'object'
        )
    ),
    CONSTRAINT agent_threads_summary_target_check CHECK (
        summary_target_sequence IS NULL OR (
            summary_target_sequence >= 1
            AND summary_target_sequence > COALESCE(summary_through_sequence, 0)
        )
    ),
    CONSTRAINT agent_threads_summary_state_check CHECK (
        (
            summary_target_sequence IS NULL
            AND summary_lease_token IS NULL
            AND summary_lease_expires_at IS NULL
            AND summary_error IS NULL
            AND summary_attempt_count = 0
        ) OR (
            summary_target_sequence IS NOT NULL
            AND summary_lease_token IS NULL
            AND summary_lease_expires_at IS NULL
            AND summary_error IS NULL
        ) OR (
            summary_target_sequence IS NOT NULL
            AND summary_attempt_count > 0
            AND summary_lease_token IS NOT NULL
            AND summary_lease_expires_at IS NOT NULL
            AND summary_error IS NULL
        ) OR (
            summary_target_sequence IS NOT NULL
            AND summary_attempt_count > 0
            AND summary_lease_token IS NULL
            AND summary_lease_expires_at IS NULL
            AND summary_error ~ '^[a-z][a-z0-9_]{0,63}$'
        )
    ),
    CONSTRAINT agent_threads_timestamps_check CHECK (
        updated_at >= created_at
        AND (summary_lease_expires_at IS NULL OR summary_lease_expires_at > updated_at)
    )
);

CREATE INDEX agent_threads_user_updated_idx
    ON agent_threads (user_id, updated_at DESC, id DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX agent_threads_summary_claim_idx
    ON agent_threads (summary_available_at, updated_at, id)
    WHERE summary_target_sequence IS NOT NULL
      AND summary_lease_token IS NULL
      AND summary_error IS NULL;
CREATE INDEX agent_threads_summary_reclaim_idx
    ON agent_threads (summary_lease_expires_at, id)
    WHERE summary_lease_token IS NOT NULL;

CREATE TABLE agent_messages (
    id uuid PRIMARY KEY,
    thread_id uuid NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    sequence_no bigint NOT NULL,
    role text NOT NULL,
    client_message_id text COLLATE "C",
    produced_by_run_id uuid,
    modality text NOT NULL DEFAULT 'text',
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_messages_id_thread_key UNIQUE (id, thread_id),
    CONSTRAINT agent_messages_thread_sequence_key UNIQUE (thread_id, sequence_no),
    CONSTRAINT agent_messages_sequence_check CHECK (sequence_no > 0),
    CONSTRAINT agent_messages_role_check CHECK (role IN ('user', 'assistant')),
    CONSTRAINT agent_messages_origin_check CHECK (
        (role = 'user' AND client_message_id IS NOT NULL AND produced_by_run_id IS NULL)
        OR
        (role = 'assistant' AND client_message_id IS NULL AND produced_by_run_id IS NOT NULL)
    ),
    CONSTRAINT agent_messages_client_id_check CHECK (
        client_message_id IS NULL OR (
            octet_length(client_message_id) BETWEEN 1 AND 128
            AND client_message_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        )
    ),
    CONSTRAINT agent_messages_modality_check CHECK (
        modality IN ('text', 'voice', 'multimodal')
        AND (modality = 'text' OR role = 'user')
    ),
    CONSTRAINT agent_messages_content_check CHECK (
        char_length(content) BETWEEN 1 AND 4096
        AND octet_length(content) <= 16384
        AND content !~ '^[[:space:]]*$'
    )
);

CREATE UNIQUE INDEX agent_messages_client_idempotency_key
    ON agent_messages (thread_id, client_message_id)
    WHERE client_message_id IS NOT NULL;
CREATE UNIQUE INDEX agent_messages_one_assistant_per_run_idx
    ON agent_messages (produced_by_run_id)
    WHERE produced_by_run_id IS NOT NULL;

CREATE TABLE agent_runs (
    id uuid PRIMARY KEY,
    thread_id uuid NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    input_message_id uuid NOT NULL,
    attempt_no integer NOT NULL,
    retry_of_run_id uuid,
    retry_client_id text COLLATE "C",
    status text NOT NULL DEFAULT 'pending',
    phase text NOT NULL DEFAULT 'queued',
    lease_token uuid,
    lease_expires_at timestamptz,
    model_configuration jsonb NOT NULL,
    context_snapshot jsonb,
    tool_trace jsonb NOT NULL DEFAULT '[]'::jsonb,
    model_result jsonb,
    usage jsonb,
    error jsonb,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_runs_id_thread_key UNIQUE (id, thread_id),
    CONSTRAINT agent_runs_id_thread_input_key UNIQUE (id, thread_id, input_message_id),
    CONSTRAINT agent_runs_input_message_fkey FOREIGN KEY (input_message_id, thread_id)
        REFERENCES agent_messages (id, thread_id) ON DELETE CASCADE,
    CONSTRAINT agent_runs_retry_of_fkey FOREIGN KEY (retry_of_run_id, thread_id)
        REFERENCES agent_runs (id, thread_id) ON DELETE CASCADE,
    CONSTRAINT agent_runs_input_attempt_key UNIQUE (thread_id, input_message_id, attempt_no),
    CONSTRAINT agent_runs_attempt_check CHECK (attempt_no > 0),
    CONSTRAINT agent_runs_retry_shape_check CHECK (
        (attempt_no = 1 AND retry_of_run_id IS NULL AND retry_client_id IS NULL)
        OR
        (attempt_no > 1 AND retry_of_run_id IS NOT NULL AND retry_client_id IS NOT NULL)
    ),
    CONSTRAINT agent_runs_retry_client_check CHECK (
        retry_client_id IS NULL OR (
            octet_length(retry_client_id) BETWEEN 1 AND 128
            AND retry_client_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        )
    ),
    CONSTRAINT agent_runs_status_check CHECK (
        status IN ('pending', 'running', 'completed', 'failed')
    ),
    CONSTRAINT agent_runs_phase_check CHECK (
        phase IN ('queued', 'context', 'model', 'tool', 'completed', 'failed')
    ),
    CONSTRAINT agent_runs_model_configuration_check CHECK (
        jsonb_typeof(model_configuration) = 'object'
        AND model_configuration ?& ARRAY[
            'provider', 'model', 'max_output_tokens', 'max_input_characters'
        ]
        AND model_configuration - ARRAY[
            'provider', 'model', 'max_output_tokens', 'max_input_characters'
        ] = '{}'::jsonb
        AND model_configuration->>'provider' ~ '^[a-z][a-z0-9_-]{0,63}$'
        AND model_configuration->>'model' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        AND (model_configuration->>'max_output_tokens')::integer BETWEEN 1 AND 1000000
        AND (model_configuration->>'max_input_characters')::integer BETWEEN 5000 AND 1000000
    ),
    CONSTRAINT agent_runs_json_shape_check CHECK (
        (context_snapshot IS NULL OR jsonb_typeof(context_snapshot) = 'object')
        AND jsonb_typeof(tool_trace) = 'array'
        AND jsonb_array_length(tool_trace) <= 4
        AND octet_length(tool_trace::text) <= 524288
        AND (
            model_result IS NULL OR (
                jsonb_typeof(model_result) = 'object'
                AND model_result ?& ARRAY['completion_id', 'provider', 'model', 'finish_reason']
                AND model_result - ARRAY[
                    'completion_id', 'provider', 'model', 'finish_reason'
                ] = '{}'::jsonb
                AND model_result->>'completion_id' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
                AND model_result->>'provider' ~ '^[a-z][a-z0-9_-]{0,63}$'
                AND model_result->>'model' ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
                AND model_result->>'finish_reason' IN ('stop', 'length')
            )
        )
        AND (
            usage IS NULL OR (
                jsonb_typeof(usage) = 'object'
                AND usage ?& ARRAY['input_tokens', 'output_tokens', 'total_tokens']
                AND usage - ARRAY['input_tokens', 'output_tokens', 'total_tokens'] = '{}'::jsonb
                AND (usage->>'input_tokens')::integer >= 0
                AND (usage->>'output_tokens')::integer >= 0
                AND (usage->>'total_tokens')::integer >= 0
            )
        )
        AND (
            error IS NULL OR (
                jsonb_typeof(error) = 'object'
                AND error ?& ARRAY['kind', 'retryable']
                AND error - ARRAY['kind', 'retryable'] = '{}'::jsonb
                AND error->>'kind' ~ '^[a-z][a-z0-9_]{0,63}$'
                AND jsonb_typeof(error->'retryable') = 'boolean'
            )
        )
    ),
    CONSTRAINT agent_runs_state_check CHECK (
        (
            status = 'pending' AND phase = 'queued'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NULL
        ) OR (
            status = 'running' AND phase IN ('context', 'model', 'tool')
            AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL
            AND started_at IS NOT NULL AND completed_at IS NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NULL
        ) OR (
            status = 'completed' AND phase = 'completed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND model_result IS NOT NULL AND usage IS NOT NULL AND error IS NULL
        ) OR (
            status = 'failed' AND phase = 'failed'
            AND lease_token IS NULL AND lease_expires_at IS NULL
            AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND model_result IS NULL AND usage IS NULL AND error IS NOT NULL
        )
    ),
    CONSTRAINT agent_runs_timestamps_check CHECK (
        updated_at >= created_at
        AND (started_at IS NULL OR started_at >= created_at)
        AND (completed_at IS NULL OR completed_at >= started_at)
        AND (lease_expires_at IS NULL OR lease_expires_at > updated_at)
    )
);

CREATE UNIQUE INDEX agent_runs_one_nonterminal_per_thread_idx
    ON agent_runs (thread_id) WHERE status IN ('pending', 'running');
CREATE UNIQUE INDEX agent_runs_retry_client_key
    ON agent_runs (thread_id, retry_client_id) WHERE retry_client_id IS NOT NULL;
CREATE INDEX agent_runs_thread_created_idx
    ON agent_runs (thread_id, created_at DESC, id DESC);
CREATE INDEX agent_runs_recoverable_idx
    ON agent_runs (lease_expires_at, id) WHERE status = 'running';

ALTER TABLE agent_messages
    ADD CONSTRAINT agent_messages_produced_by_run_fkey
        FOREIGN KEY (produced_by_run_id, thread_id)
        REFERENCES agent_runs (id, thread_id) ON DELETE CASCADE;

CREATE TABLE media_assets (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind text NOT NULL,
    upload_request_id text COLLATE "C" NOT NULL,
    object_key text NOT NULL UNIQUE,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 text NOT NULL,
    etag text NOT NULL DEFAULT '',
    width integer,
    height integer,
    duration_ns bigint,
    sample_rate integer,
    status text NOT NULL DEFAULT 'staged',
    upload_fencing_token bigint NOT NULL DEFAULT 0,
    upload_lease_until timestamptz,
    expires_at timestamptz,
    cleanup_attempt_count integer NOT NULL DEFAULT 0,
    cleanup_fencing_token bigint NOT NULL DEFAULT 0,
    cleanup_lease_until timestamptz,
    cleanup_available_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleanup_error text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT media_assets_user_upload_key
        UNIQUE (user_id, kind, upload_request_id),
    CONSTRAINT media_assets_kind_check CHECK (
        kind IN ('image', 'audio', 'document')
    ),
    CONSTRAINT media_assets_upload_request_check CHECK (
        octet_length(upload_request_id) BETWEEN 8 AND 128
        AND upload_request_id = btrim(upload_request_id)
        AND upload_request_id !~ '[[:cntrl:]]'
    ),
    CONSTRAINT media_assets_object_key_check CHECK (
        octet_length(object_key) BETWEEN 22 AND 1024
        AND object_key NOT LIKE '%..%'
        AND object_key !~ '[[:cntrl:]\\]'
        AND (
            (kind = 'image' AND object_key LIKE 'image/v1/media/%')
            OR (kind = 'audio' AND object_key LIKE 'audio/v1/media/%.wav')
            OR (
                kind = 'document'
                AND object_key LIKE 'resume/v1/media/%.pdf'
            )
        )
    ),
    CONSTRAINT media_assets_common_metadata_check CHECK (
        size_bytes BETWEEN 1 AND 10485760
        AND checksum_sha256 ~ '^[0-9a-f]{64}$'
        AND octet_length(etag) <= 512
    ),
    CONSTRAINT media_assets_kind_metadata_check CHECK (
        (
            kind = 'image'
            AND content_type IN ('image/jpeg', 'image/png', 'image/webp')
            AND width BETWEEN 1 AND 16384
            AND height BETWEEN 1 AND 16384
            AND width::bigint * height::bigint <= 16000000
            AND duration_ns IS NULL
            AND sample_rate IS NULL
        )
        OR (
            kind = 'audio'
            AND content_type = 'audio/wav'
            AND size_bytes <= 7400000
            AND width IS NULL
            AND height IS NULL
            AND duration_ns BETWEEN 1 AND 122000000000
            AND sample_rate BETWEEN 8000 AND 48000
        )
        OR (
            kind = 'document'
            AND content_type = 'application/pdf'
            AND width IS NULL
            AND height IS NULL
            AND duration_ns IS NULL
            AND sample_rate IS NULL
        )
    ),
    CONSTRAINT media_assets_status_check CHECK (
        status IN ('staged', 'ready', 'deleting')
    ),
    CONSTRAINT media_assets_attempt_check CHECK (
        upload_fencing_token >= 0
        AND cleanup_attempt_count >= 0
        AND cleanup_fencing_token >= 0
    ),
    CONSTRAINT media_assets_error_check CHECK (
        cleanup_error IS NULL
        OR cleanup_error ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    CONSTRAINT media_assets_state_check CHECK (
        (
            status = 'staged'
            AND etag = ''
            AND expires_at IS NOT NULL
            AND cleanup_lease_until IS NULL
            AND cleanup_error IS NULL
        ) OR (
            status = 'ready'
            AND etag <> ''
            AND upload_lease_until IS NULL
            AND cleanup_lease_until IS NULL
            AND cleanup_error IS NULL
        ) OR (
            status = 'deleting'
            AND upload_lease_until IS NULL
        )
    ),
    CONSTRAINT media_assets_timestamps_check CHECK (
        updated_at >= created_at
        AND (expires_at IS NULL OR expires_at > created_at)
        AND (upload_lease_until IS NULL OR upload_lease_until > updated_at)
        AND (cleanup_lease_until IS NULL OR cleanup_lease_until > updated_at)
    )
);

CREATE INDEX media_assets_cleanup_deleting_idx
    ON media_assets (cleanup_available_at, cleanup_lease_until, id)
    WHERE status = 'deleting';
CREATE INDEX media_assets_cleanup_expiry_idx
    ON media_assets (expires_at, cleanup_available_at, id)
    WHERE status IN ('staged', 'ready') AND expires_at IS NOT NULL;
CREATE INDEX media_assets_owner_cleanup_idx
    ON media_assets (user_id, cleanup_available_at, id);

CREATE TABLE agent_message_attachments (
    message_id uuid NOT NULL REFERENCES agent_messages (id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES media_assets (id) ON DELETE CASCADE,
    position smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, position),
    CONSTRAINT agent_message_attachments_asset_key UNIQUE (asset_id),
    CONSTRAINT agent_message_attachments_position_check CHECK (
        position BETWEEN 0 AND 3
    )
);

CREATE TABLE agent_voice_drafts (
    id uuid PRIMARY KEY REFERENCES media_assets (id) ON DELETE CASCADE,
    thread_id uuid NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'transcribing',
    asr_attempt integer NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 0,
    asr_fencing_token bigint NOT NULL DEFAULT 0,
    asr_lease_until timestamptz,
    asr_request_id text,
    asr_provider text,
    asr_model text,
    transcript text,
    language text,
    emotion text,
    finish_reason text,
    failure_kind text,
    failure_retryable boolean,
    confirmed_message_id uuid,
    confirmed_run_id uuid,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confirmed_at timestamptz,
    CONSTRAINT agent_voice_drafts_status_check CHECK (
        status IN ('transcribing', 'ready', 'failed', 'confirmed')
    ),
    CONSTRAINT agent_voice_drafts_attempt_check CHECK (
        asr_attempt >= 0
        AND version >= 0
        AND version <= asr_attempt
        AND asr_fencing_token >= 0
    ),
    CONSTRAINT agent_voice_drafts_provider_check CHECK (
        (asr_request_id IS NULL OR octet_length(asr_request_id) BETWEEN 1 AND 128)
        AND (asr_provider IS NULL OR asr_provider ~ '^[a-z][a-z0-9_-]{0,63}$')
        AND (
            asr_model IS NULL
            OR asr_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        )
        AND (
            transcript IS NULL OR (
                char_length(transcript) BETWEEN 1 AND 4096
                AND octet_length(transcript) <= 16384
                AND transcript !~ '^[[:space:]]*$'
            )
        )
        AND (language IS NULL OR octet_length(language) <= 64)
        AND (emotion IS NULL OR octet_length(emotion) <= 64)
        AND (finish_reason IS NULL OR octet_length(finish_reason) <= 64)
    ),
    CONSTRAINT agent_voice_drafts_failure_check CHECK (
        (failure_kind IS NULL AND failure_retryable IS NULL)
        OR (
            failure_kind ~ '^[a-z][a-z0-9_]{0,63}$'
            AND failure_retryable IS NOT NULL
        )
    ),
    CONSTRAINT agent_voice_drafts_state_check CHECK (
        (
            status = 'transcribing'
            AND asr_attempt > 0 AND version = asr_attempt
            AND asr_lease_until IS NOT NULL
            AND transcript IS NULL AND failure_kind IS NULL
        ) OR (
            status IN ('ready', 'confirmed')
            AND asr_attempt > 0 AND version = asr_attempt
            AND asr_lease_until IS NULL
            AND asr_request_id IS NOT NULL
            AND asr_provider IS NOT NULL
            AND asr_model IS NOT NULL
            AND transcript IS NOT NULL
            AND failure_kind IS NULL
        ) OR (
            status = 'failed'
            AND asr_attempt > 0 AND version = asr_attempt
            AND asr_lease_until IS NULL
            AND transcript IS NULL AND failure_kind IS NOT NULL
        )
    ),
    CONSTRAINT agent_voice_drafts_confirmation_check CHECK (
        (
            status = 'confirmed'
            AND confirmed_message_id IS NOT NULL
            AND confirmed_run_id IS NOT NULL
            AND confirmed_at IS NOT NULL
        ) OR (
            status <> 'confirmed'
            AND confirmed_message_id IS NULL
            AND confirmed_run_id IS NULL
            AND confirmed_at IS NULL
        )
    ),
    CONSTRAINT agent_voice_drafts_timestamps_check CHECK (
        updated_at >= created_at
        AND (confirmed_at IS NULL OR confirmed_at >= created_at)
        AND (asr_lease_until IS NULL OR asr_lease_until > updated_at)
    ),
    CONSTRAINT agent_voice_drafts_confirmed_message_fkey
        FOREIGN KEY (confirmed_message_id, thread_id)
        REFERENCES agent_messages (id, thread_id) ON DELETE CASCADE,
    CONSTRAINT agent_voice_drafts_confirmed_run_fkey
        FOREIGN KEY (confirmed_run_id, thread_id, confirmed_message_id)
        REFERENCES agent_runs (id, thread_id, input_message_id) ON DELETE CASCADE
);

CREATE INDEX agent_voice_drafts_asr_claim_idx
    ON agent_voice_drafts (asr_lease_until, updated_at, id)
    WHERE status = 'transcribing';
CREATE INDEX agent_voice_drafts_thread_idx
    ON agent_voice_drafts (thread_id, id);

CREATE TABLE practice_plans (
    plan_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_thread_id uuid,
    preparation_snapshot jsonb NOT NULL,
    scene_selection jsonb NOT NULL,
    session_policy jsonb NOT NULL,
    practice_objectives jsonb NOT NULL,
    ielts_assignment jsonb,
    practice_experience text NOT NULL,
    status text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    initial_client_request_id text NOT NULL,
    initial_request_fingerprint bytea NOT NULL,
    last_client_request_id text,
    last_request_fingerprint bytea,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT practice_plans_id_user_key UNIQUE (plan_id, user_id),
    CONSTRAINT practice_plans_initial_request_key
        UNIQUE (user_id, initial_client_request_id),
    CONSTRAINT practice_plans_source_thread_fkey
        FOREIGN KEY (source_thread_id, user_id)
        REFERENCES agent_threads (id, user_id)
        ON DELETE SET NULL (source_thread_id),
    CONSTRAINT practice_plans_status_check CHECK (status IN ('draft', 'ready')),
    CONSTRAINT practice_plans_version_check CHECK (version > 0),
    CONSTRAINT practice_plans_json_check CHECK (
        jsonb_typeof(preparation_snapshot) = 'object'
        AND jsonb_typeof(scene_selection) = 'object'
        AND jsonb_typeof(session_policy) = 'object'
        AND jsonb_typeof(practice_objectives) = 'array'
        AND jsonb_array_length(practice_objectives) > 0
        AND (ielts_assignment IS NULL OR jsonb_typeof(ielts_assignment) = 'object')
    ),
    CONSTRAINT practice_plans_initial_request_check CHECK (
        octet_length(initial_client_request_id) BETWEEN 8 AND 128
        AND octet_length(initial_request_fingerprint) = 32
    ),
    CONSTRAINT practice_plans_last_request_check CHECK (
        (last_client_request_id IS NULL AND last_request_fingerprint IS NULL)
        OR (
            octet_length(last_client_request_id) BETWEEN 8 AND 128
            AND octet_length(last_request_fingerprint) = 32
        )
    ),
    CONSTRAINT practice_plans_timestamps_check CHECK (updated_at >= created_at)
);

CREATE INDEX practice_plans_owner_experience_idx
    ON practice_plans (
        user_id, practice_experience, created_at DESC, plan_id DESC
    );

CREATE TABLE interview_preparations (
    interview_preparation_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    input jsonb NOT NULL,
    candidate jsonb NOT NULL,
    resume_content jsonb,
    status text NOT NULL DEFAULT 'draft',
    version integer NOT NULL DEFAULT 1,
    initial_client_request_id text NOT NULL,
    initial_request_fingerprint bytea NOT NULL,
    last_client_request_id text,
    last_request_fingerprint bytea,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT interview_preparations_initial_request_key
        UNIQUE (user_id, initial_client_request_id),
    CONSTRAINT interview_preparations_status_check
        CHECK (status IN ('draft', 'confirmed', 'discarded')),
    CONSTRAINT interview_preparations_version_check CHECK (version > 0),
    CONSTRAINT interview_preparations_json_check CHECK (
        jsonb_typeof(input) = 'object'
        AND jsonb_typeof(candidate) = 'object'
        AND (resume_content IS NULL OR jsonb_typeof(resume_content) = 'object')
    ),
    CONSTRAINT interview_preparations_initial_request_check CHECK (
        octet_length(initial_client_request_id) BETWEEN 8 AND 128
        AND octet_length(initial_request_fingerprint) = 32
    ),
    CONSTRAINT interview_preparations_last_request_check CHECK (
        (last_client_request_id IS NULL AND last_request_fingerprint IS NULL)
        OR (
            octet_length(last_client_request_id) BETWEEN 8 AND 128
            AND octet_length(last_request_fingerprint) = 32
        )
    ),
    CONSTRAINT interview_preparations_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE TABLE practice_sessions (
    session_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plan_id uuid NOT NULL,
    plan_version integer NOT NULL,
    practice_experience text NOT NULL,
    scene_category text NOT NULL,
    practice_mode text NOT NULL,
    evaluation_policy_ref text NOT NULL,
    status text NOT NULL DEFAULT 'starting',
    version integer NOT NULL DEFAULT 1,
    effective_turns integer NOT NULL DEFAULT 0,
    plan_snapshot jsonb NOT NULL,
    participants jsonb NOT NULL,
    initial_client_request_id text NOT NULL,
    initial_request_fingerprint bytea NOT NULL,
    last_client_request_id text,
    last_request_fingerprint bytea,
    started_at timestamptz,
    ended_at timestamptz,
    end_reason text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT practice_sessions_initial_request_key
        UNIQUE (user_id, initial_client_request_id),
    CONSTRAINT practice_sessions_plan_fkey
        FOREIGN KEY (plan_id, user_id)
        REFERENCES practice_plans (plan_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT practice_sessions_status_check CHECK (
        status IN ('starting', 'in_progress', 'paused', 'completed', 'ended_early')
    ),
    CONSTRAINT practice_sessions_shape_check CHECK (
        plan_version > 0 AND version > 0 AND effective_turns >= 0
        AND jsonb_typeof(plan_snapshot) = 'object'
        AND jsonb_typeof(participants) = 'array'
        AND jsonb_array_length(participants) > 0
        AND octet_length(initial_client_request_id) BETWEEN 8 AND 128
        AND octet_length(initial_request_fingerprint) = 32
        AND (
            (last_client_request_id IS NULL AND last_request_fingerprint IS NULL)
            OR (
                octet_length(last_client_request_id) BETWEEN 8 AND 128
                AND octet_length(last_request_fingerprint) = 32
            )
        )
        AND updated_at >= created_at
    ),
    CONSTRAINT practice_sessions_state_check CHECK (
        (
            status = 'starting'
            AND started_at IS NULL
            AND ended_at IS NULL
            AND end_reason IS NULL
            AND effective_turns = 0
        ) OR (
            status IN ('in_progress', 'paused')
            AND started_at IS NOT NULL
            AND ended_at IS NULL
            AND end_reason IS NULL
        ) OR (
            status IN ('completed', 'ended_early')
            AND started_at IS NOT NULL
            AND ended_at IS NOT NULL
            AND end_reason IS NOT NULL
            AND end_reason <> ''
        )
    ),
    CONSTRAINT practice_sessions_time_order_check CHECK (
        (started_at IS NULL OR started_at >= created_at)
        AND (ended_at IS NULL OR ended_at >= started_at)
    )
);

CREATE TABLE practice_questions (
    question_id uuid PRIMARY KEY,
    session_id uuid NOT NULL,
    objective_id text NOT NULL,
    question_type text NOT NULL,
    parent_question_id uuid,
    content text NOT NULL,
    speaker_participant_id text NOT NULL,
    addressee_participant_ids text[] NOT NULL,
    sequence integer NOT NULL,
    tip_id uuid,
    tip_client_request_id text,
    tip_status text,
    tip_fencing_token bigint NOT NULL DEFAULT 0,
    tip_lease_expires_at timestamptz,
    tip_content text,
    tip_provider text,
    tip_model text,
    tip_provider_request_id text,
    tip_created_at timestamptz,
    tip_completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT practice_questions_session_id_key UNIQUE (session_id, question_id),
    CONSTRAINT practice_questions_session_sequence_key UNIQUE (session_id, sequence),
    CONSTRAINT practice_questions_tip_client_key
        UNIQUE (session_id, tip_client_request_id),
    CONSTRAINT practice_questions_tip_id_key UNIQUE (tip_id),
    CONSTRAINT practice_questions_session_fkey FOREIGN KEY (session_id)
        REFERENCES practice_sessions (session_id) ON DELETE CASCADE,
    CONSTRAINT practice_questions_parent_fkey
        FOREIGN KEY (session_id, parent_question_id)
        REFERENCES practice_questions (session_id, question_id) ON DELETE RESTRICT,
    CONSTRAINT practice_questions_shape_check CHECK (
        objective_id <> ''
        AND question_type <> ''
        AND content <> ''
        AND content = btrim(content)
        AND sequence > 0
        AND cardinality(addressee_participant_ids) > 0
        AND updated_at >= created_at
    ),
    CONSTRAINT practice_questions_tip_check CHECK (
        (
            tip_status IS NULL
            AND tip_id IS NULL
            AND tip_client_request_id IS NULL
            AND tip_fencing_token = 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NULL
            AND tip_completed_at IS NULL
        ) OR (
            tip_status = 'processing'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at > updated_at
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NULL
        ) OR (
            tip_status = 'completed'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NOT NULL
            AND tip_provider IS NOT NULL
            AND tip_model IS NOT NULL
            AND tip_provider_request_id IS NOT NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NOT NULL
        ) OR (
            tip_status = 'failed'
            AND tip_id IS NOT NULL
            AND tip_client_request_id IS NOT NULL
            AND tip_fencing_token > 0
            AND tip_lease_expires_at IS NULL
            AND tip_content IS NULL
            AND tip_provider IS NULL
            AND tip_model IS NULL
            AND tip_provider_request_id IS NULL
            AND tip_created_at IS NOT NULL
            AND tip_completed_at IS NULL
        )
    )
);

CREATE TABLE practice_turns (
    turn_id uuid PRIMARY KEY,
    session_id uuid NOT NULL,
    question_id uuid NOT NULL,
    respondent_participant_id text NOT NULL,
    sequence integer NOT NULL,
    turn_kind text NOT NULL DEFAULT 'EFFECTIVE',
    status text NOT NULL DEFAULT 'answering',
    original_turn_id uuid,
    client_request_id text,
    counts_toward_turn_limit boolean NOT NULL,
    transcription_request_id text,
    transcription_client_request_id text,
    transcription_input_fingerprint text,
    asr_fencing_token bigint NOT NULL DEFAULT 0,
    asr_lease_expires_at timestamptz,
    asr_attempt_count integer NOT NULL DEFAULT 0,
    candidate_id uuid,
    transcript_id text,
    evidence_version bigint,
    transcript text,
    provider text,
    model text,
    provider_request_id text,
    failure_code text,
    confirmation_client_request_id text,
    confirmation_fingerprint bytea,
    interaction_mode text,
    audio_asset_id uuid,
    progress_fingerprint bytea,
    effective_turns_after integer,
    session_version_after integer,
    progressed_at timestamptz,
    submitted_at timestamptz,
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT practice_turns_session_id_key UNIQUE (session_id, turn_id),
    CONSTRAINT practice_turns_session_sequence_key UNIQUE (session_id, sequence),
    CONSTRAINT practice_turns_session_client_key UNIQUE (session_id, client_request_id),
    CONSTRAINT practice_turns_transcription_request_key UNIQUE (transcription_request_id),
    CONSTRAINT practice_turns_transcription_client_key
        UNIQUE (session_id, transcription_client_request_id),
    CONSTRAINT practice_turns_candidate_key UNIQUE (candidate_id),
    CONSTRAINT practice_turns_confirmation_client_key
        UNIQUE (session_id, confirmation_client_request_id),
    CONSTRAINT practice_turns_audio_asset_key UNIQUE (audio_asset_id),
    CONSTRAINT practice_turns_question_fkey
        FOREIGN KEY (session_id, question_id)
        REFERENCES practice_questions (session_id, question_id) ON DELETE CASCADE,
    CONSTRAINT practice_turns_original_fkey
        FOREIGN KEY (session_id, original_turn_id)
        REFERENCES practice_turns (session_id, turn_id) ON DELETE RESTRICT,
    CONSTRAINT practice_turns_audio_asset_fkey FOREIGN KEY (audio_asset_id)
        REFERENCES media_assets (id) ON DELETE SET NULL,
    CONSTRAINT practice_turns_kind_check CHECK (turn_kind IN ('EFFECTIVE', 'RETRY')),
    CONSTRAINT practice_turns_status_check CHECK (
        status IN ('answering', 'transcribing', 'transcribed', 'confirmed', 'failed')
    ),
    CONSTRAINT practice_turns_shape_check CHECK (
        sequence > 0
        AND asr_fencing_token >= 0
        AND asr_attempt_count >= 0
        AND updated_at >= created_at
        AND (
            (
                turn_kind = 'EFFECTIVE'
                AND original_turn_id IS NULL
                AND client_request_id IS NULL
            ) OR (
                turn_kind = 'RETRY'
                AND original_turn_id IS NOT NULL
                AND client_request_id IS NOT NULL
                AND NOT counts_toward_turn_limit
            )
        )
        AND (
            (
                status = 'confirmed'
                AND candidate_id IS NOT NULL
                AND transcript_id IS NOT NULL
                AND evidence_version > 0
                AND transcript IS NOT NULL
                AND confirmed_at IS NOT NULL
            )
            OR status <> 'confirmed'
        )
        AND (
            confirmation_fingerprint IS NULL
            OR octet_length(confirmation_fingerprint) = 32
        )
        AND (
            progress_fingerprint IS NULL
            OR octet_length(progress_fingerprint) = 32
        )
    )
);

CREATE INDEX practice_turns_session_confirmed_idx
    ON practice_turns (session_id, sequence)
    WHERE status = 'confirmed';
CREATE UNIQUE INDEX practice_turns_one_confirmed_effective_question_idx
    ON practice_turns (session_id, question_id)
    WHERE turn_kind = 'EFFECTIVE' AND status = 'confirmed';

CREATE TABLE evaluations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind text NOT NULL,
    source_id uuid NOT NULL,
    context_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'QUEUED',
    input_snapshot json NOT NULL,
    input_hash bytea NOT NULL,
    config_lineage json NOT NULL,
    config_hash bytea NOT NULL,
    result jsonb,
    attempt_count integer NOT NULL DEFAULT 0,
    lease_token uuid,
    lease_expires_at timestamptz,
    available_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    error jsonb,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at timestamptz,
    finished_at timestamptz,
    CONSTRAINT evaluations_source_key UNIQUE (user_id, kind, source_id),
    CONSTRAINT evaluations_kind_check CHECK (
        kind IN (
            'SESSION_REPORT',
            'PRACTICE_TURN_FEEDBACK',
            'AGENT_MESSAGE_FEEDBACK'
        )
    ),
    CONSTRAINT evaluations_payload_check CHECK (
        json_typeof(input_snapshot) = 'object'
        AND octet_length(input_hash) = 32
        AND json_typeof(config_lineage) = 'object'
        AND octet_length(config_hash) = 32
        AND (result IS NULL OR jsonb_typeof(result) = 'object')
        AND (error IS NULL OR jsonb_typeof(error) = 'object')
    ),
    CONSTRAINT evaluations_state_check CHECK (
        attempt_count >= 0
        AND (
            (
                status = 'QUEUED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND result IS NULL
                AND error IS NULL
                AND finished_at IS NULL
            ) OR (
                status = 'RUNNING'
                AND attempt_count > 0
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND result IS NULL
                AND error IS NULL
                AND started_at IS NOT NULL
                AND finished_at IS NULL
            ) OR (
                status = 'READY'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND result IS NOT NULL
                AND error IS NULL
                AND started_at IS NOT NULL
                AND finished_at IS NOT NULL
            ) OR (
                status = 'FAILED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND result IS NULL
                AND error IS NOT NULL
                AND started_at IS NOT NULL
                AND finished_at IS NOT NULL
            )
        )
    ),
    CONSTRAINT evaluations_timestamps_check CHECK (
        updated_at >= created_at
        AND (started_at IS NULL OR started_at >= created_at)
        AND (finished_at IS NULL OR finished_at >= started_at)
        AND (lease_expires_at IS NULL OR lease_expires_at > updated_at)
    )
);

CREATE INDEX evaluations_claim_idx
    ON evaluations (kind, available_at, created_at, id)
    WHERE status = 'QUEUED';
CREATE INDEX evaluations_reclaim_idx
    ON evaluations (kind, lease_expires_at, id)
    WHERE status = 'RUNNING';
CREATE INDEX evaluations_user_history_idx
    ON evaluations (user_id, finished_at DESC, id DESC)
    WHERE kind = 'SESSION_REPORT' AND status = 'READY';

CREATE TABLE evaluation_feedback_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id uuid NOT NULL
        REFERENCES evaluations (id) ON DELETE CASCADE,
    position integer NOT NULL,
    category text NOT NULL,
    severity text,
    evidence jsonb NOT NULL,
    recommendation text NOT NULL,
    correction text,
    repractice_mode text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT evaluation_feedback_items_position_key
        UNIQUE (evaluation_id, position),
    CONSTRAINT evaluation_feedback_items_shape_check CHECK (
        position > 0
        AND category <> ''
        AND category = btrim(category)
        AND octet_length(category) <= 128
        AND (
            severity IS NULL OR (
                severity <> ''
                AND severity = btrim(severity)
                AND octet_length(severity) <= 128
            )
        )
        AND jsonb_typeof(evidence) = 'object'
        AND recommendation <> ''
        AND recommendation = btrim(recommendation)
        AND octet_length(recommendation) <= 4096
        AND (correction IS NULL OR octet_length(correction) <= 4096)
        AND repractice_mode IN ('NONE', 'SAME_QUESTION')
    )
);

COMMIT;
