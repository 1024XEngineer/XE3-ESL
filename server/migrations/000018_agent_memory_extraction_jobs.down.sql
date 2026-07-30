BEGIN;

DROP TRIGGER agent_runs_enqueue_memory_extraction ON agent_runs;
DROP FUNCTION enqueue_agent_memory_extraction_job();
DROP TABLE agent_memory_extraction_jobs;

COMMIT;
