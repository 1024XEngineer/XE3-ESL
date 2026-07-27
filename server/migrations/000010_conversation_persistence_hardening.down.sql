BEGIN;

ALTER TABLE conversation_processing_attempts
    DROP CONSTRAINT conversation_processing_attempts_error_code_normalized;

DELETE FROM conversation_deletion_fences AS fence
WHERE NOT EXISTS (
    SELECT 1
    FROM identity_users AS users
    WHERE users.id = fence.owner_user_id
);

ALTER TABLE conversation_deletion_fences
    ADD CONSTRAINT conversation_deletion_fences_owner_user_id_fkey
    FOREIGN KEY (owner_user_id)
    REFERENCES identity_users (id)
    ON DELETE CASCADE;

COMMIT;
