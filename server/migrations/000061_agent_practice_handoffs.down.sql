BEGIN;

ALTER TABLE agent_tool_calls
    DROP CONSTRAINT agent_tool_calls_handoffs_check,
    DROP COLUMN handoffs;

COMMIT;
