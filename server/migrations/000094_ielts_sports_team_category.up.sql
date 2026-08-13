BEGIN;

ALTER TABLE ielts_part1_topics
    DISABLE TRIGGER ielts_part1_topics_immutable;

UPDATE ielts_part1_topics
SET cue_card_type = 'experience'
WHERE bank_id = 'ielts-speaking-2026-05-08-mainland'
  AND topic_id = 'p1-topic-019'
  AND cue_card_type = 'person';

ALTER TABLE ielts_part1_topics
    ENABLE TRIGGER ielts_part1_topics_immutable;

COMMIT;
