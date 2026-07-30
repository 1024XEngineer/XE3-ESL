BEGIN;

DROP TABLE IF EXISTS agent_message_images;
DROP TABLE IF EXISTS agent_image_assets;

ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_modality_check,
    ADD CONSTRAINT agent_messages_modality_check
        CHECK (modality IN ('text', 'voice'));

COMMIT;
