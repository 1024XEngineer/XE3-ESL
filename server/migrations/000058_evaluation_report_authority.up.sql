BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DROP TRIGGER reviews_inserted_evidence_check ON reviews;
DROP TRIGGER reviews_updated_evidence_check ON reviews;
DROP TRIGGER review_evidence_deleted_check ON review_evidence;
DROP TRIGGER review_evidence_updated_check ON review_evidence;
DROP FUNCTION review_check_review_evidence();
DROP FUNCTION review_check_evidence_removal();
DROP FUNCTION review_assert_completed_evidence(uuid);

DROP TABLE review_evidence;
DROP TABLE review_generation_attempts;
DROP TABLE reviews;

COMMIT;
