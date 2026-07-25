BEGIN;

CREATE TABLE review_deletion_fences (
    owner_user_id uuid PRIMARY KEY
        REFERENCES identity_users (id) ON DELETE CASCADE,
    deletion_generation bigint NOT NULL CHECK (deletion_generation >= 0),
    deleted_at timestamptz NOT NULL DEFAULT now()
);

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
        OR octet_length(stable_error_category) BETWEEN 1 AND 128
    ),
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
        OR octet_length(stable_error_category) BETWEEN 1 AND 128
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
    conclusion_key text NOT NULL CHECK (conclusion_key <> ''),
    source_type text NOT NULL CHECK (source_type <> ''),
    source_id text NOT NULL CHECK (source_id <> ''),
    source_version text NOT NULL CHECK (source_version <> ''),
    source_checksum text,
    evidence_snapshot jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (
        review_id,
        conclusion_key,
        source_type,
        source_id,
        source_version
    ),
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
BEGIN
    SELECT result
    INTO target_result
    FROM reviews
    WHERE id = target_review_id
      AND status = 'completed';

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF jsonb_typeof(target_result) IS DISTINCT FROM 'object'
       OR jsonb_typeof(target_result -> 'overall_score') IS DISTINCT FROM 'number'
       OR jsonb_typeof(target_result -> 'summary') IS DISTINCT FROM 'string'
       OR btrim(target_result ->> 'summary') = '' THEN
        RAISE EXCEPTION 'completed review result has invalid structure'
            USING ERRCODE = '23514';
    END IF;

    IF (target_result ->> 'overall_score')::numeric < 0
       OR (target_result ->> 'overall_score')::numeric > 100
       OR (target_result ->> 'overall_score')::numeric
            <> trunc((target_result ->> 'overall_score')::numeric) THEN
        RAISE EXCEPTION 'completed review score must be an integer from 0 to 100'
            USING ERRCODE = '23514';
    END IF;

    IF jsonb_typeof(target_result -> 'conclusions') IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'completed review must contain conclusions'
            USING ERRCODE = '23514';
    END IF;

    IF jsonb_array_length(target_result -> 'conclusions') = 0 THEN
        RAISE EXCEPTION 'completed review must contain conclusions'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(target_result -> 'conclusions') conclusion
        WHERE jsonb_typeof(conclusion) IS DISTINCT FROM 'object'
           OR jsonb_typeof(conclusion -> 'key') IS DISTINCT FROM 'string'
           OR btrim(conclusion ->> 'key') = ''
           OR jsonb_typeof(conclusion -> 'category') IS DISTINCT FROM 'string'
           OR btrim(conclusion ->> 'category') = ''
           OR jsonb_typeof(conclusion -> 'message') IS DISTINCT FROM 'string'
           OR btrim(conclusion ->> 'message') = ''
           OR NOT EXISTS (
                SELECT 1
                FROM review_evidence evidence
                WHERE evidence.review_id = target_review_id
                  AND evidence.owner_user_id = (
                      SELECT owner_user_id
                      FROM reviews
                      WHERE id = target_review_id
                  )
                  AND evidence.conclusion_key = conclusion ->> 'key'
           )
    ) THEN
        RAISE EXCEPTION 'each completed review conclusion requires evidence'
            USING ERRCODE = '23514';
    END IF;

    IF (
        SELECT count(*)
        FROM jsonb_array_elements(target_result -> 'conclusions')
    ) <> (
        SELECT count(DISTINCT conclusion ->> 'key')
        FROM jsonb_array_elements(target_result -> 'conclusions') conclusion
    ) THEN
        RAISE EXCEPTION 'completed review conclusion keys must be unique'
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
AFTER UPDATE OF review_id, conclusion_key ON review_evidence
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION review_check_evidence_removal();

COMMIT;
