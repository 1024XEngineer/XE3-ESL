BEGIN;

CREATE TABLE preparation_job_targets (
    owner_user_id uuid NOT NULL,
    target_id text NOT NULL CHECK (
        btrim(target_id) = target_id
        AND octet_length(target_id) BETWEEN 1 AND 128
    ),
    source_kind text NOT NULL CHECK (
        source_kind IN ('job_description', 'quick_start')
    ),
    job_title text,
    job_description text,
    company text,
    seniority text,
    candidate_background text,
    resume_ref text,
    practice_focus text,
    input_version integer NOT NULL DEFAULT 1 CHECK (input_version > 0),
    stage text NOT NULL DEFAULT 'draft' CHECK (
        stage IN (
            'draft',
            'parsing',
            'analysis_failed',
            'awaiting_confirmation',
            'confirmed',
            'discarded'
        )
    ),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, target_id),
    CONSTRAINT preparation_job_targets_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_job_targets_source_shape_check
        CHECK (
            (
                source_kind = 'job_description'
                AND job_description IS NOT NULL
                AND btrim(job_description) <> ''
                AND btrim(job_description) = job_description
            )
            OR
            (
                source_kind = 'quick_start'
                AND job_title IS NOT NULL
                AND btrim(job_title) <> ''
                AND job_description IS NULL
            )
        ),
    CONSTRAINT preparation_job_targets_text_check
        CHECK (
            (
                job_title IS NULL
                OR (
                    btrim(job_title) = job_title
                    AND octet_length(job_title) BETWEEN 1 AND 512
                )
            )
            AND octet_length(coalesce(job_description, '')) <= 65536
            AND (
                company IS NULL
                OR (
                    btrim(company) = company
                    AND octet_length(company) BETWEEN 1 AND 512
                )
            )
            AND (
                seniority IS NULL
                OR (
                    btrim(seniority) = seniority
                    AND octet_length(seniority) BETWEEN 1 AND 256
                )
            )
            AND (
                candidate_background IS NULL
                OR (
                    btrim(candidate_background) = candidate_background
                    AND btrim(candidate_background) <> ''
                    AND octet_length(candidate_background) <= 16384
                )
            )
            AND (
                resume_ref IS NULL
                OR (
                    btrim(resume_ref) = resume_ref
                    AND btrim(resume_ref) <> ''
                    AND octet_length(resume_ref) <= 16384
                )
            )
            AND (
                practice_focus IS NULL
                OR (
                    btrim(practice_focus) = practice_focus
                    AND btrim(practice_focus) <> ''
                    AND octet_length(practice_focus) <= 8192
                )
            )
        ),
    CONSTRAINT preparation_job_targets_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX preparation_job_targets_owner_updated_idx
    ON preparation_job_targets (
        owner_user_id,
        updated_at DESC,
        target_id
    )
    WHERE stage <> 'discarded';

CREATE TABLE preparation_job_target_analysis_attempts (
    attempt_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL,
    target_id text NOT NULL,
    input_version integer NOT NULL CHECK (input_version > 0),
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    worker_token uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    status text NOT NULL CHECK (
        status IN ('running', 'succeeded', 'failed')
    ),
    lease_until timestamptz NOT NULL,
    candidate jsonb,
    stable_error_category text,
    started_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    finished_at timestamptz,
    CONSTRAINT preparation_job_target_attempt_identity_key
        UNIQUE (
            owner_user_id,
            target_id,
            input_version,
            attempt_number
        ),
    CONSTRAINT preparation_job_target_attempt_target_fkey
        FOREIGN KEY (owner_user_id, target_id)
        REFERENCES preparation_job_targets (owner_user_id, target_id)
        ON DELETE CASCADE,
    CONSTRAINT preparation_job_target_attempt_state_check
        CHECK (
            (
                status = 'running'
                AND candidate IS NULL
                AND stable_error_category IS NULL
                AND finished_at IS NULL
            )
            OR
            (
                status = 'succeeded'
                AND jsonb_typeof(candidate) = 'object'
                AND stable_error_category IS NULL
                AND finished_at IS NOT NULL
            )
            OR
            (
                status = 'failed'
                AND candidate IS NULL
                AND btrim(stable_error_category) <> ''
                AND octet_length(stable_error_category) <= 128
                AND finished_at IS NOT NULL
            )
        ),
    CONSTRAINT preparation_job_target_attempt_candidate_size_check
        CHECK (
            candidate IS NULL
            OR octet_length(candidate::text) <= 65536
        )
);

CREATE UNIQUE INDEX preparation_job_target_one_running_attempt_idx
    ON preparation_job_target_analysis_attempts (
        owner_user_id,
        target_id,
        input_version
    )
    WHERE status = 'running';

CREATE INDEX preparation_job_target_attempt_recovery_idx
    ON preparation_job_target_analysis_attempts (
        owner_user_id,
        target_id,
        input_version,
        attempt_number DESC
    );

CREATE TABLE preparation_job_target_confirmations (
    owner_user_id uuid NOT NULL,
    target_id text NOT NULL,
    input_version integer NOT NULL CHECK (input_version > 0),
    analysis_version integer NOT NULL CHECK (analysis_version > 0),
    confirmation_version integer NOT NULL CHECK (confirmation_version > 0),
    candidate jsonb NOT NULL CHECK (
        jsonb_typeof(candidate) = 'object'
        AND octet_length(candidate::text) <= 65536
    ),
    confirmed_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, target_id, input_version),
    CONSTRAINT preparation_job_target_confirmation_version_key
        UNIQUE (owner_user_id, target_id, confirmation_version),
    CONSTRAINT preparation_job_target_confirmation_target_fkey
        FOREIGN KEY (owner_user_id, target_id)
        REFERENCES preparation_job_targets (owner_user_id, target_id)
        ON DELETE CASCADE,
    CONSTRAINT preparation_job_target_confirmation_attempt_fkey
        FOREIGN KEY (
            owner_user_id,
            target_id,
            input_version,
            analysis_version
        )
        REFERENCES preparation_job_target_analysis_attempts (
            owner_user_id,
            target_id,
            input_version,
            attempt_number
        )
        ON DELETE CASCADE
);

CREATE TABLE preparation_job_target_idempotency_records (
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
    CONSTRAINT preparation_job_target_idempotency_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT preparation_job_target_idempotency_method_check
        CHECK (method IN ('POST', 'PUT')),
    CONSTRAINT preparation_job_target_idempotency_path_check
        CHECK (
            left(canonical_path, 1) = '/'
            AND btrim(canonical_path) = canonical_path
            AND octet_length(canonical_path) <= 1024
        ),
    CONSTRAINT preparation_job_target_idempotency_key_check
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128),
    CONSTRAINT preparation_job_target_idempotency_fingerprint_check
        CHECK (octet_length(payload_fingerprint) = 32),
    CONSTRAINT preparation_job_target_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'job_target',
                'job_target_update',
                'job_target_analysis',
                'job_target_confirmation',
                'job_target_discard'
            )
            AND btrim(resource_id) <> ''
        ),
    CONSTRAINT preparation_job_target_idempotency_response_check
        CHECK (
            response_status BETWEEN 200 AND 299
            AND jsonb_typeof(response_body) = 'object'
        )
);

COMMIT;
