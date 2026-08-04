BEGIN;

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
          AND constraint_entry.conname LIKE 'review_repractice_requests%'
    LOOP
        EXECUTE format(
            'ALTER TABLE review_repractice_requests '
            'RENAME CONSTRAINT %I TO %I',
            item.conname,
            regexp_replace(
                item.conname,
                '^review_repractice_requests',
                'review_speech_feedback_retry_requests'
            )
        );
    END LOOP;
END;
$$;

ALTER INDEX review_repractice_requests_source_idx
    RENAME TO review_speech_feedback_retry_requests_item_idx;
ALTER TABLE review_repractice_requests
    RENAME COLUMN source_feedback_id TO speech_feedback_id;
ALTER TABLE review_repractice_requests
    RENAME COLUMN source_feedback_item_id TO feedback_item_id;
ALTER TABLE review_repractice_requests
    RENAME TO review_speech_feedback_retry_requests;
ALTER TABLE review_speech_feedback_retry_requests
    ADD CONSTRAINT review_speech_feedback_retry_requests_item_fkey
        FOREIGN KEY (
            feedback_item_id,
            owner_user_id,
            speech_feedback_id
        )
        REFERENCES evaluation_speech_feedback_items (
            id,
            owner_user_id,
            speech_feedback_id
        )
        ON DELETE CASCADE;

COMMIT;
