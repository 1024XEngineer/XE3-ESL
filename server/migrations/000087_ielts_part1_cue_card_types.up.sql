BEGIN;

ALTER TABLE ielts_question_bank_versions
    DROP CONSTRAINT ielts_question_bank_versions_schema_check;

ALTER TABLE ielts_question_bank_versions
    DISABLE TRIGGER ielts_question_bank_versions_immutable;

UPDATE ielts_question_bank_versions
SET schema_version = 4
WHERE schema_version = 3;

ALTER TABLE ielts_question_bank_versions
    ENABLE TRIGGER ielts_question_bank_versions_immutable;

ALTER TABLE ielts_question_bank_versions
    ADD CONSTRAINT ielts_question_bank_versions_schema_check
    CHECK (schema_version = 4);

ALTER TABLE ielts_part1_topics
    ADD COLUMN cue_card_type text;

ALTER TABLE ielts_part1_topics
    DISABLE TRIGGER ielts_part1_topics_immutable;

UPDATE ielts_part1_topics
SET cue_card_type = CASE
    WHEN topic_id IN ('p1-topic-002', 'p1-topic-019') THEN 'person'
    WHEN topic_id IN (
        'p1-topic-009', 'p1-topic-012', 'p1-topic-026', 'p1-topic-027',
        'p1-topic-029', 'p1-topic-033', 'p1-topic-035', 'p1-topic-036',
        'p1-topic-037', 'p1-topic-038'
    ) THEN 'place'
    WHEN topic_id IN (
        'p1-topic-001', 'p1-topic-003', 'p1-topic-005', 'p1-topic-006',
        'p1-topic-008', 'p1-topic-010', 'p1-topic-011', 'p1-topic-014',
        'p1-topic-015', 'p1-topic-016', 'p1-topic-017', 'p1-topic-018',
        'p1-topic-022'
    ) THEN 'thing'
    WHEN topic_id IN (
        'p1-topic-004', 'p1-topic-007', 'p1-topic-013', 'p1-topic-020',
        'p1-topic-021', 'p1-topic-023', 'p1-topic-024', 'p1-topic-025',
        'p1-topic-028', 'p1-topic-030', 'p1-topic-031', 'p1-topic-032',
        'p1-topic-034'
    ) THEN 'experience'
END;

ALTER TABLE ielts_part1_topics
    ENABLE TRIGGER ielts_part1_topics_immutable;

ALTER TABLE ielts_part1_topics
    ALTER COLUMN cue_card_type SET NOT NULL,
    ADD CONSTRAINT ielts_part1_topics_cue_card_type_check
        CHECK (cue_card_type IN ('person', 'place', 'thing', 'experience'));

COMMIT;
