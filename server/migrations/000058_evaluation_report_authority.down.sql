BEGIN;

CREATE TABLE reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL
        REFERENCES identity_users (id) ON DELETE RESTRICT,
    practice_session_id text NOT NULL CHECK (practice_session_id <> ''),
    implementation_version text NOT NULL CHECK (implementation_version <> ''),
    source_turn_id text NOT NULL CHECK (source_turn_id <> ''),
    source_turn_version text NOT NULL CHECK (source_turn_version <> ''),
    source_manifest_fingerprint text NOT NULL
        CHECK (source_manifest_fingerprint <> ''),
    deletion_generation bigint NOT NULL CHECK (deletion_generation >= 0),
    status text NOT NULL CHECK (
        status IN ('pending', 'generating', 'completed', 'failed')
    ),
    result jsonb,
    stable_error_category text CHECK (
        stable_error_category IS NULL
        OR stable_error_category IN (
            'invalid_request',
            'configuration',
            'authentication',
            'authorization',
            'quota_exhausted',
            'rate_limited',
            'timeout',
            'provider_timeout',
            'provider_unavailable',
            'invalid_response',
            'cancelled',
            'source_unavailable',
            'invalid_source',
            'generation_failed',
            'invalid_result',
            'lease_expired'
        )
    ),
    evaluation_context jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (owner_user_id, practice_session_id),
    UNIQUE (id, owner_user_id),
    CONSTRAINT reviews_state_shape_check CHECK (
        (
            status IN ('pending', 'generating')
            AND result IS NULL
            AND completed_at IS NULL
            AND stable_error_category IS NULL
        )
        OR
        (
            status = 'completed'
            AND result IS NOT NULL
            AND completed_at IS NOT NULL
            AND stable_error_category IS NULL
        )
        OR
        (
            status = 'failed'
            AND result IS NULL
            AND completed_at IS NULL
            AND stable_error_category IS NOT NULL
            AND stable_error_category <> ''
        )
    ),
    CONSTRAINT reviews_evaluation_context_check CHECK (
        evaluation_context IS NULL
        OR (
            jsonb_typeof(evaluation_context) = 'object'
            AND evaluation_context ?& ARRAY[
                'schema_version',
                'context_type',
                'scene_key',
                'scene_id',
                'scene_version',
                'practice_option_type',
                'difficulty_ref',
                'assistance_ref',
                'turn_policy_ref',
                'session_policy_ref',
                'scene_specific_context'
            ]
            AND evaluation_context->>'schema_version' =
                'evaluation-context.v1'
            AND octet_length(evaluation_context::text) <= 16384
        )
    ),
    CONSTRAINT reviews_v2_context_required_check CHECK (
        implementation_version <> 'qianwen-scenario-review-v2'
        OR evaluation_context IS NOT NULL
    )
);

CREATE INDEX reviews_owner_created_idx
    ON reviews (owner_user_id, created_at DESC, id DESC);

CREATE TABLE review_generation_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    worker_token uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    deletion_generation bigint NOT NULL CHECK (deletion_generation >= 0),
    status text NOT NULL CHECK (
        status IN ('running', 'succeeded', 'failed', 'cancelled')
    ),
    lease_until timestamptz NOT NULL,
    stable_error_category text CHECK (
        stable_error_category IS NULL
        OR stable_error_category IN (
            'invalid_request',
            'configuration',
            'authentication',
            'authorization',
            'quota_exhausted',
            'rate_limited',
            'timeout',
            'provider_timeout',
            'provider_unavailable',
            'invalid_response',
            'cancelled',
            'source_unavailable',
            'invalid_source',
            'generation_failed',
            'invalid_result',
            'lease_expired'
        )
    ),
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (review_id, attempt_number),
    FOREIGN KEY (review_id, owner_user_id)
        REFERENCES reviews (id, owner_user_id)
        ON DELETE CASCADE,
    CHECK (
        (status = 'running'
            AND finished_at IS NULL
            AND stable_error_category IS NULL)
        OR
        (status = 'succeeded'
            AND finished_at IS NOT NULL
            AND stable_error_category IS NULL)
        OR
        (status IN ('failed', 'cancelled')
            AND finished_at IS NOT NULL
            AND stable_error_category IS NOT NULL
            AND stable_error_category <> '')
    )
);

CREATE INDEX review_generation_attempts_recovery_idx
    ON review_generation_attempts (review_id, status, lease_until DESC);

CREATE TABLE review_evidence (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    target_key text NOT NULL CHECK (target_key <> ''),
    source_type text NOT NULL CHECK (source_type <> ''),
    source_id text NOT NULL CHECK (source_id <> ''),
    source_version text NOT NULL CHECK (source_version <> ''),
    source_checksum text,
    evidence_snapshot jsonb,
    target_kind text COLLATE "C" NOT NULL DEFAULT 'conclusion',
    field text COLLATE "C" NOT NULL DEFAULT 'answer_text',
    anchor_kind text COLLATE "C" NOT NULL DEFAULT 'whole_field',
    quote text NOT NULL DEFAULT '',
    start_utf8_byte integer,
    end_utf8_byte integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (review_id, owner_user_id)
        REFERENCES reviews (id, owner_user_id)
        ON DELETE CASCADE,
    CHECK (
        source_checksum IS NULL
        OR (
            source_checksum <> ''
            AND octet_length(source_checksum) <= 512
        )
    ),
    CHECK (
        evidence_snapshot IS NULL
        OR octet_length(evidence_snapshot::text) <= 16384
    ),
    CONSTRAINT review_evidence_target_kind_check
        CHECK (target_kind IN ('conclusion', 'feedback_item')),
    CONSTRAINT review_evidence_field_check
        CHECK (field = 'answer_text'),
    CONSTRAINT review_evidence_anchor_kind_check
        CHECK (anchor_kind IN ('exact_quote', 'whole_field')),
    CONSTRAINT review_evidence_anchor_shape_check CHECK (
        (
            anchor_kind = 'exact_quote'
            AND quote <> ''
            AND start_utf8_byte IS NOT NULL
            AND end_utf8_byte IS NOT NULL
            AND start_utf8_byte >= 0
            AND end_utf8_byte > start_utf8_byte
        )
        OR (
            anchor_kind = 'whole_field'
            AND quote = ''
            AND start_utf8_byte IS NULL
            AND end_utf8_byte IS NULL
        )
    ),
    CONSTRAINT review_evidence_target_unique UNIQUE (
        review_id,
        target_kind,
        target_key,
        source_type,
        source_id,
        source_version,
        field,
        anchor_kind,
        quote
    )
);

CREATE INDEX review_evidence_source_idx
    ON review_evidence (
        owner_user_id,
        source_type,
        source_id,
        source_version,
        review_id
    );

CREATE FUNCTION review_assert_completed_evidence(target_review_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    target_result jsonb;
    target_owner uuid;
    target_context_type text;
    eligibility text;
BEGIN
    SELECT result, owner_user_id, evaluation_context ->> 'context_type'
      INTO target_result, target_owner, target_context_type
      FROM reviews
     WHERE id = target_review_id
       AND status = 'completed';

    IF NOT FOUND THEN
        RETURN;
    END IF;

    eligibility := coalesce(
        target_result ->> 'summary_eligibility',
        'eligible'
    );

    IF jsonb_typeof(target_result) IS DISTINCT FROM 'object'
       OR jsonb_typeof(target_result -> 'summary') IS DISTINCT FROM 'string'
       OR btrim(target_result ->> 'summary') = ''
       OR eligibility NOT IN (
            'eligible',
            'provisional',
            'insufficient_evidence'
       )
       OR jsonb_typeof(target_result -> 'conclusions')
            IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'completed review result has invalid structure'
            USING ERRCODE = '23514';
    END IF;

    IF eligibility = 'insufficient_evidence' THEN
        IF target_result ? 'overall_score'
           OR jsonb_array_length(target_result -> 'conclusions') <> 0
           OR jsonb_typeof(
                target_result -> 'insufficient_evidence_reasons'
              ) IS DISTINCT FROM 'array'
           OR jsonb_array_length(
                target_result -> 'insufficient_evidence_reasons'
              ) = 0
           OR EXISTS (
                SELECT 1
                  FROM review_evidence evidence
                 WHERE evidence.review_id = target_review_id
           ) THEN
            RAISE EXCEPTION
                'insufficient review must omit score and evidence'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    IF eligibility = 'provisional'
       AND (
            target_context_type IS DISTINCT FROM 'ielts.speaking_part2'
            OR target_result ? 'overall_score'
            OR jsonb_typeof(
                target_result -> 'insufficient_evidence_reasons'
            ) IS DISTINCT FROM 'array'
            OR jsonb_array_length(
                target_result -> 'insufficient_evidence_reasons'
            ) = 0
       ) THEN
        RAISE EXCEPTION
            'only IELTS may be provisional and it must omit overall score'
            USING ERRCODE = '23514';
    END IF;

    IF eligibility = 'eligible'
       AND target_context_type = 'ielts.speaking_part2'
       AND NOT (target_result ? 'overall_score') THEN
        RAISE EXCEPTION 'reliable IELTS review requires overall score'
            USING ERRCODE = '23514';
    END IF;

    IF target_context_type IS NOT NULL
       AND target_context_type <> 'ielts.speaking_part2'
       AND target_result ? 'overall_score' THEN
        RAISE EXCEPTION 'non-IELTS review must omit overall score'
            USING ERRCODE = '23514';
    END IF;

    IF (
        target_result ? 'overall_score'
        AND (
            jsonb_typeof(target_result -> 'overall_score')
                IS DISTINCT FROM 'number'
            OR (target_result ->> 'overall_score')::numeric < 0
            OR (target_result ->> 'overall_score')::numeric > 100
            OR (target_result ->> 'overall_score')::numeric
                <> trunc((target_result ->> 'overall_score')::numeric)
        )
       )
       OR (
            target_context_type IS NULL
            AND eligibility = 'eligible'
            AND NOT (target_result ? 'overall_score')
       )
       OR jsonb_array_length(target_result -> 'conclusions') = 0 THEN
        RAISE EXCEPTION 'review score or conclusions are invalid'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM jsonb_array_elements(
              target_result -> 'conclusions'
          ) conclusion
         WHERE jsonb_typeof(conclusion) IS DISTINCT FROM 'object'
            OR jsonb_typeof(conclusion -> 'key') IS DISTINCT FROM 'string'
            OR btrim(conclusion ->> 'key') = ''
            OR jsonb_typeof(conclusion -> 'category')
                IS DISTINCT FROM 'string'
            OR btrim(conclusion ->> 'category') = ''
            OR jsonb_typeof(conclusion -> 'message')
                IS DISTINCT FROM 'string'
            OR btrim(conclusion ->> 'message') = ''
            OR (
                target_result ? 'summary_eligibility'
                AND (
                    jsonb_typeof(conclusion -> 'score')
                        IS DISTINCT FROM 'number'
                    OR (conclusion ->> 'score')::numeric < 0
                    OR (conclusion ->> 'score')::numeric > 100
                )
            )
            OR NOT EXISTS (
                SELECT 1
                  FROM review_evidence evidence
                 WHERE evidence.review_id = target_review_id
                   AND evidence.owner_user_id = target_owner
                   AND evidence.target_kind = 'conclusion'
                   AND evidence.target_key = conclusion ->> 'key'
            )
    ) THEN
        RAISE EXCEPTION
            'each completed review conclusion requires valid evidence'
            USING ERRCODE = '23514';
    END IF;

    IF (
        SELECT count(*)
          FROM jsonb_array_elements(target_result -> 'conclusions')
    ) <> (
        SELECT count(DISTINCT conclusion ->> 'key')
          FROM jsonb_array_elements(
              target_result -> 'conclusions'
          ) conclusion
    ) THEN
        RAISE EXCEPTION 'completed review conclusion keys must be unique'
            USING ERRCODE = '23514';
    END IF;

    IF target_result ? 'feedback_items'
       AND (
           jsonb_typeof(target_result -> 'feedback_items')
                IS DISTINCT FROM 'array'
           OR EXISTS (
                SELECT 1
                  FROM jsonb_array_elements(
                      target_result -> 'feedback_items'
                  ) feedback
                 WHERE jsonb_typeof(feedback) IS DISTINCT FROM 'object'
                    OR jsonb_typeof(feedback -> 'key')
                        IS DISTINCT FROM 'string'
                    OR btrim(feedback ->> 'key') = ''
                    OR NOT EXISTS (
                        SELECT 1
                          FROM review_evidence evidence
                         WHERE evidence.review_id = target_review_id
                           AND evidence.owner_user_id = target_owner
                           AND evidence.target_kind = 'feedback_item'
                           AND evidence.target_key = feedback ->> 'key'
                    )
           )
       ) THEN
        RAISE EXCEPTION
            'each completed review feedback item requires evidence'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION review_check_review_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM review_assert_completed_evidence(NEW.id);
    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER reviews_inserted_evidence_check
AFTER INSERT ON reviews
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION review_check_review_evidence();

CREATE CONSTRAINT TRIGGER reviews_updated_evidence_check
AFTER UPDATE OF status, result ON reviews
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION review_check_review_evidence();

CREATE FUNCTION review_check_evidence_removal()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM review_assert_completed_evidence(OLD.review_id);
    RETURN OLD;
END;
$$;

CREATE CONSTRAINT TRIGGER review_evidence_deleted_check
AFTER DELETE ON review_evidence
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION review_check_evidence_removal();

CREATE CONSTRAINT TRIGGER review_evidence_updated_check
AFTER UPDATE OF review_id, target_kind, target_key ON review_evidence
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION review_check_evidence_removal();

COMMIT;
