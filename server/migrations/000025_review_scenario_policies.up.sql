BEGIN;

ALTER TABLE reviews
    ADD COLUMN evaluation_context jsonb;

DROP TRIGGER reviews_inserted_evidence_check ON reviews;
DROP TRIGGER reviews_updated_evidence_check ON reviews;
DROP TRIGGER review_evidence_deleted_check ON review_evidence;
DROP TRIGGER review_evidence_updated_check ON review_evidence;
DROP FUNCTION review_check_review_evidence();
DROP FUNCTION review_check_evidence_removal();
DROP FUNCTION review_assert_completed_evidence(uuid);

ALTER TABLE review_evidence
    RENAME COLUMN conclusion_key TO target_key;

ALTER TABLE review_evidence
    ADD COLUMN target_kind text COLLATE "C" NOT NULL DEFAULT 'conclusion',
    ADD COLUMN field text COLLATE "C" NOT NULL DEFAULT 'answer_text',
    ADD COLUMN anchor_kind text COLLATE "C" NOT NULL DEFAULT 'whole_field',
    ADD COLUMN quote text NOT NULL DEFAULT '',
    ADD COLUMN start_utf8_byte integer,
    ADD COLUMN end_utf8_byte integer;

DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname
      INTO constraint_name
      FROM pg_constraint
     WHERE conrelid = 'review_evidence'::regclass
       AND contype = 'u'
       AND conname LIKE
           'review_evidence_review_id_conclusion_key_source_type_source_id%';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format(
            'ALTER TABLE review_evidence DROP CONSTRAINT %I',
            constraint_name
        );
    END IF;
END;
$$;

ALTER TABLE review_evidence
    ADD CONSTRAINT review_evidence_target_kind_check
        CHECK (target_kind IN ('conclusion', 'feedback_item')),
    ADD CONSTRAINT review_evidence_field_check
        CHECK (field = 'answer_text'),
    ADD CONSTRAINT review_evidence_anchor_kind_check
        CHECK (anchor_kind IN ('exact_quote', 'whole_field')),
    ADD CONSTRAINT review_evidence_anchor_shape_check
        CHECK (
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
    ADD CONSTRAINT review_evidence_target_unique
        UNIQUE (
            review_id,
            target_kind,
            target_key,
            source_type,
            source_id,
            source_version,
            field,
            anchor_kind,
            quote
        );

ALTER TABLE reviews
    ADD CONSTRAINT reviews_evaluation_context_check
        CHECK (
            evaluation_context IS NULL
            OR (
                jsonb_typeof(evaluation_context) = 'object'
                AND evaluation_context ?& ARRAY[
                    'schema_version',
                    'context_type',
                    'scene_key',
                    'scenario_definition_id',
                    'scenario_definition_version',
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
    ADD CONSTRAINT reviews_v2_context_required_check
        CHECK (
            implementation_version <> 'qianwen-scenario-review-v2'
            OR evaluation_context IS NOT NULL
        );

CREATE FUNCTION review_assert_completed_evidence(target_review_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    target_result jsonb;
    target_owner uuid;
    eligibility text;
BEGIN
    SELECT result, owner_user_id
      INTO target_result, target_owner
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
       OR eligibility NOT IN ('eligible', 'insufficient_evidence')
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

    IF jsonb_typeof(target_result -> 'overall_score')
            IS DISTINCT FROM 'number'
       OR (target_result ->> 'overall_score')::numeric < 0
       OR (target_result ->> 'overall_score')::numeric > 100
       OR (target_result ->> 'overall_score')::numeric
            <> trunc((target_result ->> 'overall_score')::numeric)
       OR jsonb_array_length(target_result -> 'conclusions') = 0 THEN
        RAISE EXCEPTION 'eligible review score is invalid'
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
