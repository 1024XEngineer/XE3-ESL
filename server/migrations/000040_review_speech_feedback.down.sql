BEGIN;

DROP TABLE IF EXISTS review_speech_feedback_items;
DROP TABLE IF EXISTS review_speech_feedbacks;

ALTER TABLE agent_voice_transcript_evidence
    DROP CONSTRAINT IF EXISTS
        agent_voice_transcript_evidence_feedback_identity_key;

DROP TRIGGER IF EXISTS
    review_speech_feedback_turn_snapshots_immutable
    ON review_speech_feedback_turn_snapshots;
DROP FUNCTION IF EXISTS reject_review_speech_feedback_snapshot_update();
DROP TABLE IF EXISTS review_speech_feedback_turn_snapshots;

COMMIT;
