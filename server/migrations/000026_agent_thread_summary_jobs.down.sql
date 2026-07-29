BEGIN;

DROP TRIGGER agent_runs_enqueue_thread_summary ON agent_runs;
DROP FUNCTION enqueue_agent_thread_summary_job();
DROP TABLE agent_thread_summary_jobs;

COMMIT;
