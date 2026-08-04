BEGIN;

ALTER TABLE review_speech_feedback_retry_requests
    DROP CONSTRAINT review_speech_feedback_retry_requests_item_fkey;
ALTER TABLE review_speech_feedback_retry_requests
    RENAME TO review_repractice_requests;
ALTER TABLE review_repractice_requests
    RENAME COLUMN feedback_item_id TO source_feedback_item_id;
ALTER TABLE review_repractice_requests
    RENAME COLUMN speech_feedback_id TO source_feedback_id;
ALTER INDEX review_speech_feedback_retry_requests_item_idx
    RENAME TO review_repractice_requests_source_idx;

DO $$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT constraint_entry.conname
        FROM pg_constraint AS constraint_entry
        JOIN pg_class AS relation
          ON relation.oid = constraint_entry.conrelid
        JOIN pg_namespace AS namespace
          ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = current_schema()
          AND relation.relname = 'review_repractice_requests'
          AND constraint_entry.conname LIKE
              'review_speech_feedback_retry_requests%'
    LOOP
        EXECUTE format(
            'ALTER TABLE review_repractice_requests '
            'RENAME CONSTRAINT %I TO %I',
            item.conname,
            regexp_replace(
                item.conname,
                '^review_speech_feedback_retry_requests',
                'review_repractice_requests'
            )
        );
    END LOOP;
END;
$$;

COMMIT;
