BEGIN;

DO $$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT relation.oid::regclass AS relation_name,
               constraint_entry.conname
        FROM pg_constraint AS constraint_entry
        JOIN pg_class AS relation
          ON relation.oid = constraint_entry.conrelid
        JOIN pg_namespace AS namespace
          ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = current_schema()
          AND relation.relname IN (
              'evaluation_speech_feedback_turn_snapshots',
              'evaluation_speech_feedbacks',
              'evaluation_speech_feedback_items',
              'evaluation_speech_feedback_acoustic_evidence'
          )
          AND constraint_entry.conname LIKE 'evaluation_speech_feedback%'
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RENAME CONSTRAINT %I TO %I',
            item.relation_name,
            item.conname,
            regexp_replace(
                item.conname,
                '^evaluation_speech_feedback',
                'review_speech_feedback'
            )
        );
    END LOOP;
END;
$$;

ALTER TRIGGER evaluation_speech_feedback_acoustic_evidence_immutable
    ON evaluation_speech_feedback_acoustic_evidence
    RENAME TO review_speech_feedback_acoustic_evidence_immutable;
ALTER TRIGGER evaluation_speech_feedback_turn_snapshots_immutable
    ON evaluation_speech_feedback_turn_snapshots
    RENAME TO review_speech_feedback_turn_snapshots_immutable;
ALTER FUNCTION reject_evaluation_speech_feedback_snapshot_update()
    RENAME TO reject_review_speech_feedback_snapshot_update;

ALTER INDEX evaluation_speech_feedback_items_feedback_idx
    RENAME TO review_speech_feedback_items_feedback_idx;
ALTER INDEX evaluation_speech_feedbacks_pending_idx
    RENAME TO review_speech_feedbacks_pending_idx;

ALTER TABLE evaluation_speech_feedback_acoustic_evidence
    RENAME TO review_speech_feedback_acoustic_evidence;
ALTER TABLE evaluation_speech_feedback_items
    RENAME TO review_speech_feedback_items;
ALTER TABLE evaluation_speech_feedbacks
    RENAME TO review_speech_feedbacks;
ALTER TABLE evaluation_speech_feedback_turn_snapshots
    RENAME TO review_speech_feedback_turn_snapshots;

COMMIT;
