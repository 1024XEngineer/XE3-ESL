BEGIN;

ALTER TABLE conversation_deletion_fences
    DROP CONSTRAINT conversation_deletion_fences_owner_user_id_fkey;

UPDATE conversation_processing_attempts
SET error_code = 'legacy_provider_failure'
WHERE status = 'failed'
  AND error_code NOT IN (
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
      'legacy_provider_failure'
  );

UPDATE conversation_processing_attempts
SET error_code = ''
WHERE status <> 'failed'
  AND error_code <> '';

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
                'cancelled',
                'legacy_provider_failure'
            )
        )
        OR
        (
            status <> 'failed'
            AND error_code = ''
        )
    );

COMMIT;
