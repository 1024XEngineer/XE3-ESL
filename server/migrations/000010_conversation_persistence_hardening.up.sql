BEGIN;

ALTER TABLE conversation_deletion_fences
    DROP CONSTRAINT conversation_deletion_fences_owner_user_id_fkey;

ALTER TABLE conversation_processing_attempts
    ADD CONSTRAINT conversation_processing_attempts_error_code_normalized
    CHECK (
        (
            status = 'failed'
            AND error_code IN (
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
                'cancelled'
            )
        )
        OR
        (
            status <> 'failed'
            AND error_code = ''
        )
    );

COMMIT;
