BEGIN;

DROP TRIGGER IF EXISTS review_evidence_updated_check ON review_evidence;
DROP TRIGGER IF EXISTS review_evidence_deleted_check ON review_evidence;
DROP FUNCTION IF EXISTS review_check_evidence_removal();
DROP TRIGGER IF EXISTS reviews_updated_evidence_check ON reviews;
DROP TRIGGER IF EXISTS reviews_inserted_evidence_check ON reviews;
DROP FUNCTION IF EXISTS review_check_review_evidence();
DROP FUNCTION IF EXISTS review_assert_completed_evidence(uuid);
DROP TABLE IF EXISTS review_evidence;
DROP TABLE IF EXISTS review_generation_attempts;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS review_deletion_fences;

COMMIT;
