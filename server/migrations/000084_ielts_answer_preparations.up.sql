BEGIN;

CREATE TABLE ielts_answer_preparations (
    owner_user_id uuid NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
    answer_preparation_id text NOT NULL,
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    part text NOT NULL,
    source_id text NOT NULL,
    question_position integer NOT NULL,
    question_prompt text NOT NULL,
    personal_points jsonb NOT NULL DEFAULT '[]'::jsonb,
    target_band numeric(2,1) NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    answer text,
    outline jsonb,
    useful_expressions jsonb,
    speech_text text,
    failure_code text,
    provider text,
    model text,
    provider_request_id text,
    version integer NOT NULL DEFAULT 1,
    generation_revision integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, answer_preparation_id),
    UNIQUE (owner_user_id, bank_id, part, source_id, question_position),
    CONSTRAINT ielts_answer_preparations_id_check CHECK (
        answer_preparation_id ~ '^ielts_answer_[0-9a-f]{32}$'
    ),
    CONSTRAINT ielts_answer_preparations_locator_check CHECK (
        part IN ('PART_1', 'PART_2', 'PART_3')
        AND question_position > 0
        AND (part <> 'PART_2' OR question_position = 1)
        AND length(bank_id) BETWEEN 1 AND 128
        AND length(source_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT ielts_answer_preparations_prompt_check CHECK (
        question_prompt = btrim(question_prompt)
        AND length(question_prompt) BETWEEN 1 AND 2048
    ),
    CONSTRAINT ielts_answer_preparations_input_check CHECK (
        jsonb_typeof(personal_points) = 'array'
        AND jsonb_array_length(personal_points) <= 12
        AND target_band BETWEEN 4.0 AND 9.0
        AND mod(target_band * 2, 1) = 0
    ),
    CONSTRAINT ielts_answer_preparations_status_check CHECK (
        status IN ('draft', 'generating', 'ready', 'failed')
        AND (
            (status IN ('draft', 'generating') AND failure_code IS NULL)
            OR (status = 'ready' AND answer IS NOT NULL AND outline IS NOT NULL
                AND useful_expressions IS NOT NULL AND speech_text IS NOT NULL
                AND failure_code IS NULL)
            OR (status = 'failed' AND failure_code IS NOT NULL)
        )
    ),
    CONSTRAINT ielts_answer_preparations_generated_shape_check CHECK (
        (outline IS NULL OR jsonb_typeof(outline) = 'array')
        AND (useful_expressions IS NULL OR jsonb_typeof(useful_expressions) = 'array')
    ),
    CONSTRAINT ielts_answer_preparations_version_check CHECK (
        version > 0 AND generation_revision >= 0 AND updated_at >= created_at
    )
);

CREATE INDEX ielts_answer_preparations_owner_updated_idx
    ON ielts_answer_preparations(owner_user_id, updated_at DESC);

CREATE TABLE ielts_answer_preparation_idempotency (
    owner_user_id uuid NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
    method text NOT NULL,
    canonical_path text NOT NULL,
    idempotency_key text NOT NULL,
    payload_fingerprint bytea NOT NULL,
    resource_id text,
    pending boolean NOT NULL DEFAULT false,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, method, canonical_path, idempotency_key),
    FOREIGN KEY (owner_user_id, resource_id)
        REFERENCES ielts_answer_preparations(owner_user_id, answer_preparation_id)
        ON DELETE CASCADE,
    CONSTRAINT ielts_answer_preparation_idempotency_method_check
        CHECK (method IN ('POST', 'PATCH', 'DELETE')),
    CONSTRAINT ielts_answer_preparation_idempotency_key_check
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128
               AND idempotency_key !~ '[[:space:]]'),
    CONSTRAINT ielts_answer_preparation_idempotency_fingerprint_check
        CHECK (octet_length(payload_fingerprint) = 32),
    CONSTRAINT ielts_answer_preparation_idempotency_response_check
        CHECK ((pending AND resource_id IS NOT NULL AND response IS NOT NULL)
               OR (NOT pending AND (response IS NOT NULL OR resource_id IS NULL)))
);

COMMIT;
