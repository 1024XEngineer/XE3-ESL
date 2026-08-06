BEGIN;

ALTER TABLE agent_context_manifests
    ADD COLUMN memory_extraction_barrier_policy_version text,
    ADD COLUMN memory_extraction_barrier_cutoff timestamptz,
    ADD COLUMN memory_extraction_barrier_status text,
    ADD COLUMN memory_extraction_barrier_waited_milliseconds bigint,
    ADD COLUMN memory_extraction_barrier_covered_through timestamptz;

UPDATE agent_context_manifests AS manifests
SET
    memory_extraction_barrier_policy_version = 'memory-extraction-barrier-v1',
    memory_extraction_barrier_cutoff = runs.created_at,
    memory_extraction_barrier_status = 'not_required',
    memory_extraction_barrier_waited_milliseconds = 0
FROM agent_runs AS runs
WHERE runs.id = manifests.run_id;

ALTER TABLE agent_context_manifests
    ALTER COLUMN memory_extraction_barrier_policy_version SET NOT NULL,
    ALTER COLUMN memory_extraction_barrier_cutoff SET NOT NULL,
    ALTER COLUMN memory_extraction_barrier_status SET NOT NULL,
    ALTER COLUMN memory_extraction_barrier_waited_milliseconds SET NOT NULL,
    ADD CONSTRAINT agent_context_manifests_memory_extraction_barrier_policy_check
        CHECK (
            memory_extraction_barrier_policy_version =
                'memory-extraction-barrier-v1'
        ),
    ADD CONSTRAINT agent_context_manifests_memory_extraction_barrier_shape_check
        CHECK (
            memory_extraction_barrier_cutoff <= created_at
            AND memory_extraction_barrier_waited_milliseconds BETWEEN 0 AND 5000
            AND (
                (
                    memory_extraction_barrier_status = 'not_required'
                    AND memory_extraction_barrier_waited_milliseconds = 0
                    AND memory_extraction_barrier_covered_through IS NULL
                )
                OR (
                    memory_extraction_barrier_status = 'ready'
                    AND memory_extraction_barrier_waited_milliseconds = 0
                    AND (
                        memory_extraction_barrier_covered_through IS NULL
                        OR memory_extraction_barrier_covered_through <=
                            memory_extraction_barrier_cutoff
                    )
                )
                OR (
                    memory_extraction_barrier_status = 'waited'
                    AND memory_extraction_barrier_waited_milliseconds > 0
                    AND memory_extraction_barrier_covered_through IS NOT NULL
                    AND memory_extraction_barrier_covered_through <=
                        memory_extraction_barrier_cutoff
                )
            )
        );

COMMIT;
