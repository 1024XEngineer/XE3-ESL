BEGIN;

ALTER TABLE agent_context_manifests
    DROP CONSTRAINT agent_context_manifests_memory_extraction_barrier_shape_check,
    DROP CONSTRAINT agent_context_manifests_memory_extraction_barrier_policy_check,
    DROP COLUMN memory_extraction_barrier_covered_through,
    DROP COLUMN memory_extraction_barrier_waited_milliseconds,
    DROP COLUMN memory_extraction_barrier_status,
    DROP COLUMN memory_extraction_barrier_cutoff,
    DROP COLUMN memory_extraction_barrier_policy_version;

COMMIT;
