BEGIN;

DROP TRIGGER agent_memories_enqueue_vector_index ON agent_memories;
DROP FUNCTION enqueue_agent_memory_index_job();
DROP TABLE agent_memory_index_jobs;
DROP TABLE agent_memory_vectors;

COMMIT;
