BEGIN;

DROP TRIGGER agent_runs_enqueue_thread_title ON agent_runs;
DROP FUNCTION enqueue_agent_thread_title_job();
DROP TABLE agent_thread_title_jobs;

ALTER TABLE agent_threads
    DROP CONSTRAINT agent_threads_title_check,
    DROP COLUMN title;

COMMIT;
