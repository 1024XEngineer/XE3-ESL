BEGIN;

DROP TRIGGER IF EXISTS ielts_question_bank_versions_immutable
    ON ielts_question_bank_versions;
DROP FUNCTION IF EXISTS ielts_guard_question_bank_version_mutation();

DROP TABLE IF EXISTS ielts_part23_group_tags;
DROP TABLE IF EXISTS ielts_part3_questions;
DROP TABLE IF EXISTS ielts_part23_groups;
DROP TABLE IF EXISTS ielts_part1_set_questions;
DROP TABLE IF EXISTS ielts_part1_sets;
DROP TABLE IF EXISTS ielts_part1_topic_tags;
DROP TABLE IF EXISTS ielts_part1_questions;
DROP TABLE IF EXISTS ielts_part1_topics;
DROP TABLE IF EXISTS ielts_question_bank_tags;
DROP TABLE IF EXISTS ielts_question_bank_sources;

DROP FUNCTION IF EXISTS ielts_reject_published_bank_content_mutation();
DROP TABLE IF EXISTS ielts_question_bank_versions;

COMMIT;
