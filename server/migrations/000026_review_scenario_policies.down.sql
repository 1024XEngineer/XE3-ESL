BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM reviews
         WHERE implementation_version = 'qianwen-scenario-review-v2'
    ) THEN
        RAISE EXCEPTION
            'cannot remove scenario Review schema while v2 reviews exist';
    END IF;
END;
$$;

DROP TRIGGER reviews_inserted_evidence_check ON reviews;
DROP TRIGGER reviews_updated_evidence_check ON reviews;
DROP TRIGGER review_evidence_deleted_check ON review_evidence;
DROP TRIGGER review_evidence_updated_check ON review_evidence;
DROP FUNCTION review_check_review_evidence();
DROP FUNCTION review_check_evidence_removal();
DROP FUNCTION review_assert_completed_evidence(uuid);

ALTER TABLE reviews
    DROP CONSTRAINT reviews_v2_context_required_check,
    DROP CONSTRAINT reviews_evaluation_context_check,
    DROP COLUMN evaluation_context;

ALTER TABLE review_evidence
    DROP CONSTRAINT review_evidence_target_unique,
    DROP CONSTRAINT review_evidence_anchor_shape_check,
    DROP CONSTRAINT review_evidence_anchor_kind_check,
    DROP CONSTRAINT review_evidence_field_check,
    DROP CONSTRAINT review_evidence_target_kind_check,
    DROP COLUMN end_utf8_byte,
    DROP COLUMN start_utf8_byte,
    DROP COLUMN quote,
    DROP COLUMN anchor_kind,
    DROP COLUMN field,
    DROP COLUMN target_kind;

ALTER TABLE review_evidence
    RENAME COLUMN target_key TO conclusion_key;

ALTER TABLE review_evidence
    ADD CONSTRAINT review_evidence_legacy_unique
        UNIQUE (
            review_id,
            conclusion_key,
            source_type,
            source_id,
            source_version
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
       OR jsonb_typeof(target_result -> 'overall_score')
            IS DISTINCT FROM 'number'
       OR jsonb_typeof(target_result -> 'summary')
            IS DISTINCT FROM 'string'
       OR btrim(target_result ->> 'summary') = ''
       OR (target_result ->> 'overall_score')::numeric < 0
       OR (target_result ->> 'overall_score')::numeric > 100
       OR (target_result ->> 'overall_score')::numeric
            <> trunc((target_result ->> 'overall_score')::numeric)
       OR jsonb_typeof(target_result -> 'conclusions')
            IS DISTINCT FROM 'array'
       OR jsonb_array_length(target_result -> 'conclusions') = 0 THEN
        RAISE EXCEPTION 'completed review result has invalid structure'
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
