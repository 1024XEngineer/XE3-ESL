BEGIN;

ALTER TABLE ielts_part1_topics
    DROP CONSTRAINT ielts_part1_topics_cue_card_type_check,
    DROP COLUMN cue_card_type;

ALTER TABLE ielts_question_bank_versions
    DROP CONSTRAINT ielts_question_bank_versions_schema_check;

ALTER TABLE ielts_question_bank_versions
    DISABLE TRIGGER ielts_question_bank_versions_immutable;

UPDATE ielts_question_bank_versions
SET schema_version = 3
WHERE schema_version = 4;

ALTER TABLE ielts_question_bank_versions
    ENABLE TRIGGER ielts_question_bank_versions_immutable;

ALTER TABLE ielts_question_bank_versions
    ADD CONSTRAINT ielts_question_bank_versions_schema_check
    CHECK (schema_version = 3);

COMMIT;
