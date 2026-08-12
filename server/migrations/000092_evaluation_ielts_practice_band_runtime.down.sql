BEGIN;

CREATE OR REPLACE FUNCTION evaluation_assert_ielts_part_result_binding_v1()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    assignment jsonb;
    task_blueprints jsonb;
    opportunity_manifest jsonb;
    confirmed_turns jsonb;
    evidence_refs jsonb;
    expected_part_sequence jsonb;
    expected_question_texts jsonb;
BEGIN
    SELECT
        snapshot.canonical_payload #>
            '{practice_context,ielts_assignment}',
        snapshot.canonical_payload #>
            '{practice_context,task_blueprints}',
        snapshot.canonical_payload -> 'opportunity_manifest',
        snapshot.canonical_payload -> 'confirmed_turns',
        snapshot.canonical_payload -> 'evidence_refs'
    INTO
        assignment,
        task_blueprints,
        opportunity_manifest,
        confirmed_turns,
        evidence_refs
    FROM evaluation_evidence_snapshots AS snapshot
    WHERE snapshot.id = NEW.input_snapshot_id
      AND snapshot.owner_user_id = NEW.owner_user_id
      AND snapshot.practice_session_id = NEW.practice_session_id
      AND snapshot.scene_type = 'IELTS_SPEAKING'
      AND snapshot.scope = 'SESSION'
    FOR SHARE;

    IF NOT FOUND
       OR ielts_assignment_is_valid_v1(
            'FULL_MOCK',
            task_blueprints,
            assignment
          ) IS DISTINCT FROM true
       OR jsonb_typeof(opportunity_manifest) IS DISTINCT FROM 'array'
       OR jsonb_typeof(confirmed_turns) IS DISTINCT FROM 'array'
       OR jsonb_typeof(evidence_refs) IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'invalid IELTS Part result binding'
            USING ERRCODE = '23514';
    END IF;

    SELECT
        jsonb_agg(
            to_jsonb(part.value ->> 'part')
            ORDER BY part.ordinality, blueprint.ordinality
        ),
        jsonb_agg(
            to_jsonb(
                btrim(
                    regexp_replace(
                        blueprint.value #>> '{}',
                        '^[^:]+:[[:space:]]*',
                        ''
                    )
                )
            )
            ORDER BY part.ordinality, blueprint.ordinality
        )
    INTO
        expected_part_sequence,
        expected_question_texts
    FROM jsonb_array_elements(assignment -> 'parts')
        WITH ORDINALITY AS part(value, ordinality)
    CROSS JOIN LATERAL jsonb_array_elements(
        part.value -> 'turn_blueprints'
    ) WITH ORDINALITY AS blueprint(value, ordinality);

    IF jsonb_array_length(opportunity_manifest) <>
           jsonb_array_length(expected_part_sequence)
       OR jsonb_array_length(NEW.result_payload -> 'question_results') <>
           jsonb_array_length(expected_part_sequence)
       OR (
            SELECT count(DISTINCT opportunity.value ->> 'question_id')
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
          ) <> jsonb_array_length(opportunity_manifest)
       OR (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> jsonb_array_length(confirmed_turns)
       OR (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> jsonb_array_length(evidence_refs)
       OR (
            SELECT count(DISTINCT opportunity.value ->> 'response_turn_id')
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          ) <> (
            SELECT count(*)
            FROM jsonb_array_elements(opportunity_manifest)
                AS opportunity(value)
            WHERE opportunity.value ? 'response_turn_id'
          )
       OR EXISTS (
            SELECT 1
            FROM jsonb_array_elements(
                NEW.result_payload -> 'question_results'
            ) WITH ORDINALITY AS question(value, ordinality)
            CROSS JOIN LATERAL (
                SELECT opportunity_manifest ->
                    (question.ordinality::integer - 1) AS opportunity
            ) AS expected
            CROSS JOIN LATERAL (
                SELECT coalesce(
                    jsonb_agg(
                        to_jsonb(
                            evidence_ref.value ->> 'evidence_ref_id'
                        )
                        ORDER BY evidence_ref.ordinality
                    ),
                    '[]'::jsonb
                ) AS evidence_ref_ids
                FROM jsonb_array_elements(evidence_refs)
                    WITH ORDINALITY AS evidence_ref(value, ordinality)
                WHERE evidence_ref.value ->> 'turn_id' =
                    expected.opportunity ->> 'response_turn_id'
            ) AS expected_refs
            WHERE expected.opportunity ->> 'sequence' IS DISTINCT FROM
                    question.ordinality::text
               OR question.value ->> 'index' IS DISTINCT FROM
                    question.ordinality::text
               OR question.value ->> 'part_id' IS DISTINCT FROM
                expected_part_sequence ->> (question.ordinality::integer - 1)
               OR question.value ->> 'question_id' IS DISTINCT FROM
                    expected.opportunity ->> 'question_id'
               OR expected.opportunity ->> 'question_text' IS DISTINCT FROM
                    expected_question_texts ->>
                        (question.ordinality::integer - 1)
               OR (
                    NOT (expected.opportunity ? 'response_turn_id')
                    AND (
                        question.value ->> 'opportunity_status'
                            IS DISTINCT FROM 'NOT_PROVIDED'
                        OR question.value ->> 'assessment_status'
                            IS DISTINCT FROM 'NOT_ASSESSED'
                        OR question.value ? 'response_turn_id'
                        OR question.value -> 'evidence_ref_ids'
                            IS DISTINCT FROM '[]'::jsonb
                    )
               )
               OR (
                    expected.opportunity ? 'response_turn_id'
                    AND (
                        question.value ->> 'opportunity_status'
                            IS DISTINCT FROM 'PROVIDED'
                        OR question.value ->> 'assessment_status'
                            IS DISTINCT FROM 'ASSESSED'
                        OR question.value ->> 'response_turn_id'
                            IS DISTINCT FROM
                                expected.opportunity ->> 'response_turn_id'
                        OR expected_refs.evidence_ref_ids = '[]'::jsonb
                        OR question.value -> 'evidence_ref_ids'
                            IS DISTINCT FROM
                                expected_refs.evidence_ref_ids
                        OR NOT EXISTS (
                            SELECT 1
                            FROM jsonb_array_elements(confirmed_turns)
                                AS confirmed_turn(value)
                            WHERE confirmed_turn.value ->> 'turn_id' =
                                expected.opportunity ->> 'response_turn_id'
                              AND confirmed_turn.value ->> 'question_id' =
                                expected.opportunity ->> 'question_id'
                              AND confirmed_turn.value ->> 'sequence' =
                                expected.opportunity ->> 'sequence'
                        )
                    )
               )
       ) THEN
        RAISE EXCEPTION 'invalid IELTS Part result binding'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Keep the v8 prompt and lineage constraints on rollback. Persisted reports are
-- immutable history, and removing v8 support would either reject those rows or
-- require destructive data cleanup. The older runtime continues to write v7.

COMMIT;
