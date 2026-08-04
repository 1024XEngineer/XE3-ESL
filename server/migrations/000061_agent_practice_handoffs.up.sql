BEGIN;

ALTER TABLE agent_tool_calls
    ADD COLUMN handoffs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT agent_tool_calls_handoffs_check
        CHECK (jsonb_typeof(handoffs) = 'array');

COMMIT;
