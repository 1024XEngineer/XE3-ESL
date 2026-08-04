package evaluation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type SpeechFeedbackRepository interface {
	EnsureConfirmedConversationTurn(
		context.Context,
		string,
		string,
		string,
	) (SpeechFeedbackReference, error)
	EnsureConfirmedAgentVoiceMessage(
		context.Context,
		string,
		string,
		string,
	) (SpeechFeedbackReference, error)
	GetSpeechFeedback(
		context.Context,
		string,
		string,
	) (SpeechFeedback, error)
	FindSpeechFeedbackByConversationTurn(
		context.Context,
		string,
		string,
	) (SpeechFeedbackReference, bool, error)
	FindSpeechFeedbackByAgentMessage(
		context.Context,
		string,
		string,
	) (SpeechFeedbackReference, bool, error)
	ClaimSpeechFeedback(
		context.Context,
		SpeechFeedbackWorkerConfiguration,
	) (SpeechFeedbackClaim, bool, error)
	CompleteSpeechFeedback(
		context.Context,
		SpeechFeedbackClaim,
		[]SpeechFeedbackDraftItem,
	) (SpeechFeedback, error)
	CompleteSpeechFeedbackInsufficient(
		context.Context,
		SpeechFeedbackClaim,
		[]SpeechFeedbackReasonCode,
	) (SpeechFeedback, error)
	FailSpeechFeedback(
		context.Context,
		SpeechFeedbackClaim,
		SpeechFeedbackStableFailure,
		SpeechFeedbackWorkerConfiguration,
	) (SpeechFeedbackStatus, error)
}

type storedSpeechFeedback struct {
	Feedback           SpeechFeedback
	OwnerUserID        string
	SourceDigest       [sha256.Size]byte
	DeletionGeneration int64
	CanonicalText      string
	PromptText         string
	EvidenceRefID      string
	AudioAssetID       string
	AudioAssetVersion  int64
	AudioChecksum      string
	AudioObjectKey     string
	AttemptCount       int
	FencingToken       int64
	LeaseExpiresAt     *time.Time
}

func (r *PostgresRepository) EnsureConfirmedConversationTurn(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
	turnID string,
) (SpeechFeedbackReference, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validSpeechFeedbackIdentifier(practiceSessionID) ||
		!validSpeechFeedbackIdentifier(turnID) {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"begin Conversation SpeechFeedback ensure: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackReference{}, mapSpeechFeedbackAccountError(err)
	}
	if err := lockSpeechFeedbackSource(
		ctx,
		tx,
		ownerUserID,
		string(SpeechFeedbackSourceConversationTurn),
		turnID,
	); err != nil {
		return SpeechFeedbackReference{}, err
	}

	snapshot, err := ensureSpeechFeedbackTurnSnapshot(
		ctx,
		tx,
		ownerUserID,
		practiceSessionID,
		turnID,
	)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	englishEvidence := classifySpeechFeedbackLanguage(
		snapshot.CanonicalText,
	) == speechFeedbackLanguageEnglish
	if _, err := tx.Exec(ctx, `
		INSERT INTO evaluation_speech_feedbacks (
			owner_user_id,
			source_kind,
			practice_session_id,
			turn_id,
			input_revision,
			evidence_snapshot_id,
			source_digest,
			schema_version,
			strategy_ref,
			pipeline_version,
			feedback_status,
			scoreability_status,
			gate_status,
			reason_codes,
			completed_at
		)
		VALUES (
			$1,
			'CONVERSATION_TURN',
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			CASE WHEN $10::boolean THEN 'QUEUED' ELSE 'READY' END,
			CASE
				WHEN $10::boolean THEN NULL
				ELSE 'INSUFFICIENT'
			END,
			CASE WHEN $10::boolean THEN NULL ELSE 'BLOCKED' END,
			CASE
				WHEN $10::boolean THEN ARRAY[]::text[]
				ELSE ARRAY['TRANSCRIPT_CONFIDENCE_INSUFFICIENT']::text[]
			END,
			CASE
				WHEN $10::boolean THEN NULL
				ELSE transaction_timestamp()
			END
		)
		ON CONFLICT DO NOTHING
	`, ownerUserID, practiceSessionID, turnID,
		snapshot.InputRevision, snapshot.ID, snapshot.SourceDigest[:],
		SpeechFeedbackSchemaVersion, SpeechFeedbackStrategyRef,
		SpeechFeedbackPipelineVersion, englishEvidence); err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"insert Conversation SpeechFeedback: %w",
			err,
		)
	}
	stored, err := selectSpeechFeedbackByConversationTurn(
		ctx,
		tx,
		ownerUserID,
		turnID,
	)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	if stored.Feedback.Source.PracticeSessionID != practiceSessionID ||
		stored.Feedback.Source.InputRevision != snapshot.InputRevision ||
		stored.Feedback.Source.EvidenceSnapshotID != snapshot.ID ||
		stored.SourceDigest != snapshot.SourceDigest {
		return SpeechFeedbackReference{}, ErrSpeechFeedbackConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"commit Conversation SpeechFeedback ensure: %w",
			err,
		)
	}
	return speechFeedbackReference(stored.Feedback), nil
}

func (r *PostgresRepository) EnsureConfirmedAgentVoiceMessage(
	ctx context.Context,
	ownerUserID string,
	threadID string,
	messageID string,
) (SpeechFeedbackReference, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(threadID) ||
		!validUUID(messageID) {
		return SpeechFeedbackReference{}, ErrInvalidSpeechFeedback
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"begin Agent SpeechFeedback ensure: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		return SpeechFeedbackReference{}, mapSpeechFeedbackAccountError(err)
	}
	if err := lockSpeechFeedbackSource(
		ctx,
		tx,
		ownerUserID,
		string(SpeechFeedbackSourceAgentVoiceMessage),
		messageID,
	); err != nil {
		return SpeechFeedbackReference{}, err
	}

	source, canonicalText, sourceDigest, err :=
		selectConfirmedAgentSpeechFeedbackSource(
			ctx,
			tx,
			ownerUserID,
			threadID,
			messageID,
		)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	englishEvidence := classifySpeechFeedbackLanguage(
		canonicalText,
	) == speechFeedbackLanguageEnglish
	if _, err := tx.Exec(ctx, `
		INSERT INTO evaluation_speech_feedbacks (
			owner_user_id,
			source_kind,
			thread_id,
			message_id,
			transcript_evidence_id,
			candidate_version,
			source_digest,
			schema_version,
			strategy_ref,
			pipeline_version,
			feedback_status,
			scoreability_status,
			gate_status,
			reason_codes,
			completed_at
		)
		VALUES (
			$1,
			'AGENT_VOICE_MESSAGE',
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			CASE WHEN $10::boolean THEN 'QUEUED' ELSE 'READY' END,
			CASE
				WHEN $10::boolean THEN NULL
				ELSE 'INSUFFICIENT'
			END,
			CASE WHEN $10::boolean THEN NULL ELSE 'BLOCKED' END,
			CASE
				WHEN $10::boolean THEN ARRAY[]::text[]
				ELSE ARRAY['TRANSCRIPT_CONFIDENCE_INSUFFICIENT']::text[]
			END,
			CASE
				WHEN $10::boolean THEN NULL
				ELSE transaction_timestamp()
			END
		)
		ON CONFLICT DO NOTHING
	`, ownerUserID, source.ThreadID, source.MessageID,
		source.TranscriptEvidenceID, source.CandidateVersion,
		sourceDigest[:], SpeechFeedbackSchemaVersion,
		SpeechFeedbackStrategyRef,
		SpeechFeedbackPipelineVersion, englishEvidence); err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"insert Agent SpeechFeedback: %w",
			err,
		)
	}
	stored, err := selectSpeechFeedbackByAgentMessage(
		ctx,
		tx,
		ownerUserID,
		messageID,
	)
	if err != nil {
		return SpeechFeedbackReference{}, err
	}
	if stored.Feedback.Source != source ||
		stored.SourceDigest != sourceDigest ||
		stored.CanonicalText != canonicalText {
		return SpeechFeedbackReference{}, ErrSpeechFeedbackConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedbackReference{}, fmt.Errorf(
			"commit Agent SpeechFeedback ensure: %w",
			err,
		)
	}
	return speechFeedbackReference(stored.Feedback), nil
}

func (r *PostgresRepository) GetSpeechFeedback(
	ctx context.Context,
	ownerUserID string,
	speechFeedbackID string,
) (SpeechFeedback, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(speechFeedbackID) {
		return SpeechFeedback{}, ErrSpeechFeedbackNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"begin SpeechFeedback read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveIdentityUser(ctx, tx, ownerUserID, 0); err != nil {
		if errors.Is(err, ErrAccountUnavailable) {
			return SpeechFeedback{}, ErrSpeechFeedbackNotFound
		}
		return SpeechFeedback{}, fmt.Errorf(
			"lock SpeechFeedback owner: %w",
			err,
		)
	}
	stored, err := selectSpeechFeedbackByID(
		ctx,
		tx,
		ownerUserID,
		speechFeedbackID,
	)
	if err != nil {
		return SpeechFeedback{}, err
	}
	items, err := listSpeechFeedbackItems(
		ctx,
		tx,
		ownerUserID,
		speechFeedbackID,
	)
	if err != nil {
		return SpeechFeedback{}, err
	}
	stored.Feedback.Items = items
	acoustics, found, err := getSpeechFeedbackAcousticAssessment(
		ctx,
		tx,
		ownerUserID,
		speechFeedbackID,
	)
	if err != nil {
		return SpeechFeedback{}, err
	}
	if found {
		stored.Feedback.AcousticAssessment = acoustics
	}
	if !stored.Feedback.valid(
		stored.EvidenceRefID,
		stored.CanonicalText,
	) {
		return SpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return SpeechFeedback{}, fmt.Errorf(
			"commit SpeechFeedback read: %w",
			err,
		)
	}
	return stored.Feedback, nil
}

func (r *PostgresRepository) FindSpeechFeedbackByConversationTurn(
	ctx context.Context,
	ownerUserID string,
	turnID string,
) (SpeechFeedbackReference, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validSpeechFeedbackIdentifier(turnID) {
		return SpeechFeedbackReference{}, false,
			ErrInvalidSpeechFeedback
	}
	stored, err := selectSpeechFeedbackByConversationTurn(
		ctx,
		r.pool,
		ownerUserID,
		turnID,
	)
	if errors.Is(err, ErrSpeechFeedbackNotFound) {
		return SpeechFeedbackReference{}, false, nil
	}
	if err != nil {
		return SpeechFeedbackReference{}, false, err
	}
	return speechFeedbackReference(stored.Feedback), true, nil
}

func (r *PostgresRepository) FindSpeechFeedbackByAgentMessage(
	ctx context.Context,
	ownerUserID string,
	messageID string,
) (SpeechFeedbackReference, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validUUID(messageID) {
		return SpeechFeedbackReference{}, false,
			ErrInvalidSpeechFeedback
	}
	stored, err := selectSpeechFeedbackByAgentMessage(
		ctx,
		r.pool,
		ownerUserID,
		messageID,
	)
	if errors.Is(err, ErrSpeechFeedbackNotFound) {
		return SpeechFeedbackReference{}, false, nil
	}
	if err != nil {
		return SpeechFeedbackReference{}, false, err
	}
	return speechFeedbackReference(stored.Feedback), true, nil
}

type speechFeedbackTurnSnapshot struct {
	ID            string
	InputRevision int64
	EvidenceRefID string
	CanonicalText string
	AudioAssetID  string
	AudioVersion  int64
	AudioChecksum string
	SourceDigest  [sha256.Size]byte
}

func ensureSpeechFeedbackTurnSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	practiceSessionID string,
	turnID string,
) (speechFeedbackTurnSnapshot, error) {
	var (
		answerText      string
		evidenceVersion int64
		candidateID     string
		transcriptID    string
		scenarioType    string
		scenarioModel   string
		audioAssetID    string
		audioVersion    int64
		audioChecksum   string
	)
	err := tx.QueryRow(ctx, `
		SELECT
			turn.answer_text,
			turn.evidence_version,
			candidate.candidate_id,
			candidate.transcript_id,
			session.scene_family,
			session.scene_model,
			audio.audio_asset_id,
			audio.version,
			audio.checksum_sha256
		FROM practice_turns AS turn
		JOIN practice_transcript_candidates AS candidate
		  ON candidate.owner_user_id = turn.owner_user_id
		 AND candidate.candidate_id = turn.candidate_id
		 AND candidate.practice_session_id =
		     turn.practice_session_id
		 AND candidate.question_id = turn.question_id
		JOIN practice_sessions AS session
		  ON session.owner_user_id = turn.owner_user_id
		 AND session.session_id = turn.practice_session_id
		JOIN practice_audio_assets AS audio
		  ON audio.owner_user_id = turn.owner_user_id
		 AND audio.turn_id = turn.turn_id
		 AND audio.candidate_id = turn.candidate_id
		 AND audio.status = 'readable'
		WHERE turn.owner_user_id = $1
		  AND turn.practice_session_id = $2
		  AND turn.turn_id = $3
		  AND candidate.status = 'confirmed'
		FOR SHARE OF turn, candidate, session
	`, ownerUserID, practiceSessionID, turnID).Scan(
		&answerText,
		&evidenceVersion,
		&candidateID,
		&transcriptID,
		&scenarioType,
		&scenarioModel,
		&audioAssetID,
		&audioVersion,
		&audioChecksum,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return speechFeedbackTurnSnapshot{},
			ErrSpeechFeedbackNotApplicable
	}
	if err != nil {
		return speechFeedbackTurnSnapshot{}, fmt.Errorf(
			"read confirmed Conversation Turn source: %w",
			err,
		)
	}
	if !eligibleSpeechFeedbackScenario(scenarioType, scenarioModel) ||
		evidenceVersion < 1 ||
		!validSpeechFeedbackText(answerText, 16*1024) {
		return speechFeedbackTurnSnapshot{},
			ErrSpeechFeedbackNotApplicable
	}
	digest := digestSpeechFeedbackParts(
		"conversation-turn/v1",
		ownerUserID,
		practiceSessionID,
		turnID,
		fmt.Sprintf("%d", evidenceVersion),
		candidateID,
		transcriptID,
		audioAssetID,
		fmt.Sprintf("%d", audioVersion),
		audioChecksum,
		answerText,
	)
	snapshotID := "evaluation_snapshot_" +
		hex.EncodeToString(digest[:16])
	evidenceRefID := "evaluation_evidence_" +
		hex.EncodeToString(digest[16:])
	if _, err := tx.Exec(ctx, `
		INSERT INTO evaluation_speech_feedback_turn_snapshots (
			id,
			owner_user_id,
			practice_session_id,
			turn_id,
			input_revision,
			evidence_ref_id,
			transcript_text,
			source_digest,
			audio_asset_id,
			audio_asset_version,
			audio_checksum_sha256
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (owner_user_id, turn_id) DO NOTHING
	`, snapshotID, ownerUserID, practiceSessionID, turnID,
		evidenceVersion, evidenceRefID, answerText, digest[:],
		audioAssetID, audioVersion, audioChecksum); err != nil {
		return speechFeedbackTurnSnapshot{}, fmt.Errorf(
			"insert SpeechFeedback Turn snapshot: %w",
			err,
		)
	}
	var persistedDigest []byte
	var snapshot speechFeedbackTurnSnapshot
	err = tx.QueryRow(ctx, `
		SELECT
			id,
			input_revision,
			evidence_ref_id,
			transcript_text,
			source_digest,
			audio_asset_id,
			audio_asset_version,
			audio_checksum_sha256
		FROM evaluation_speech_feedback_turn_snapshots
		WHERE owner_user_id = $1
		  AND practice_session_id = $2
		  AND turn_id = $3
	`, ownerUserID, practiceSessionID, turnID).Scan(
		&snapshot.ID,
		&snapshot.InputRevision,
		&snapshot.EvidenceRefID,
		&snapshot.CanonicalText,
		&persistedDigest,
		&snapshot.AudioAssetID,
		&snapshot.AudioVersion,
		&snapshot.AudioChecksum,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return speechFeedbackTurnSnapshot{},
			ErrSpeechFeedbackConflict
	}
	if err != nil {
		return speechFeedbackTurnSnapshot{}, fmt.Errorf(
			"read SpeechFeedback Turn snapshot: %w",
			err,
		)
	}
	if len(persistedDigest) != sha256.Size {
		return speechFeedbackTurnSnapshot{},
			ErrInvalidSpeechFeedback
	}
	copy(snapshot.SourceDigest[:], persistedDigest)
	if snapshot.ID != snapshotID ||
		snapshot.InputRevision != evidenceVersion ||
		snapshot.EvidenceRefID != evidenceRefID ||
		snapshot.CanonicalText != answerText ||
		snapshot.AudioAssetID != audioAssetID ||
		snapshot.AudioVersion != audioVersion ||
		snapshot.AudioChecksum != audioChecksum ||
		snapshot.SourceDigest != digest {
		return speechFeedbackTurnSnapshot{},
			ErrSpeechFeedbackConflict
	}
	return snapshot, nil
}

func selectConfirmedAgentSpeechFeedbackSource(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	threadID string,
	messageID string,
) (SpeechFeedbackSource, string, [sha256.Size]byte, error) {
	var source SpeechFeedbackSource
	var canonicalText string
	var candidateID string
	err := tx.QueryRow(ctx, `
		SELECT
			evidence.thread_id::text,
			evidence.message_id::text,
			evidence.evidence_id::text,
			evidence.candidate_version,
			evidence.confirmed_text,
			evidence.candidate_id::text
		FROM agent_voice_transcript_evidence AS evidence
		JOIN agent_messages AS message
		  ON message.id = evidence.message_id
		 AND message.owner_user_id = evidence.owner_user_id
		 AND message.thread_id = evidence.thread_id
		JOIN agent_voice_candidates AS candidate
		  ON candidate.candidate_id = evidence.candidate_id
		 AND candidate.owner_user_id = evidence.owner_user_id
		 AND candidate.thread_id = evidence.thread_id
		WHERE evidence.owner_user_id = $1
		  AND evidence.thread_id = $2
		  AND evidence.message_id = $3
		  AND message.role = 'user'
		  AND message.modality = 'voice'
		  AND message.content = evidence.confirmed_text
		  AND candidate.status = 'confirmed'
		  AND candidate.confirmed_message_id = evidence.message_id
		  AND candidate.candidate_version =
		      evidence.candidate_version
		FOR SHARE OF evidence, message, candidate
	`, ownerUserID, threadID, messageID).Scan(
		&source.ThreadID,
		&source.MessageID,
		&source.TranscriptEvidenceID,
		&source.CandidateVersion,
		&canonicalText,
		&candidateID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SpeechFeedbackSource{}, "", [sha256.Size]byte{},
			ErrSpeechFeedbackNotApplicable
	}
	if err != nil {
		return SpeechFeedbackSource{}, "", [sha256.Size]byte{},
			fmt.Errorf("read confirmed Agent voice source: %w", err)
	}
	source.SourceKind = SpeechFeedbackSourceAgentVoiceMessage
	if !source.valid() ||
		!validSpeechFeedbackText(canonicalText, 16*1024) {
		return SpeechFeedbackSource{}, "", [sha256.Size]byte{},
			ErrSpeechFeedbackNotApplicable
	}
	digest := digestSpeechFeedbackParts(
		"agent-voice-message/v1",
		ownerUserID,
		source.ThreadID,
		source.MessageID,
		source.TranscriptEvidenceID,
		fmt.Sprintf("%d", source.CandidateVersion),
		candidateID,
		canonicalText,
	)
	return source, canonicalText, digest, nil
}

func eligibleSpeechFeedbackScenario(
	scenarioType string,
	scenarioModel string,
) bool {
	switch scenarioType {
	case "DAILY":
		return scenarioModel == "HOTEL_CHECKIN_AND_ISSUE_HANDLING" ||
			scenarioModel == "DAILY_BASIC_DIALOGUE"
	case "WORKPLACE":
		return scenarioModel == "PROGRESS_AND_RISK_UPDATE" ||
			scenarioModel == "WORKPLACE_BASIC_DIALOGUE"
	case "INTERVIEW":
		return scenarioModel == "PROJECT_EXPERIENCE_DEEP_DIVE" ||
			scenarioModel == "INTERVIEW_BASIC_DIALOGUE"
	case "EXAM":
		return scenarioModel == "IELTS_SPEAKING_PART_1" ||
			scenarioModel == "IELTS_SPEAKING_PART_2" ||
			scenarioModel == "IELTS_SPEAKING_PART_3" ||
			scenarioModel == "IELTS_SPEAKING_FULL_MOCK" ||
			scenarioModel == "EXAM_BASIC_DIALOGUE"
	default:
		return false
	}
}

func lockSpeechFeedbackSource(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	sourceKind string,
	sourceID string,
) error {
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended(
				jsonb_build_array(
					$1::text,
					$2::text,
					$3::text
				)::text,
				0
			)
		)
	`, ownerUserID, sourceKind, sourceID); err != nil {
		return fmt.Errorf("lock SpeechFeedback source: %w", err)
	}
	return nil
}

func digestSpeechFeedbackParts(parts ...string) [sha256.Size]byte {
	hasher := sha256.New()
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(part))
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func speechFeedbackReference(
	feedback SpeechFeedback,
) SpeechFeedbackReference {
	return SpeechFeedbackReference{
		SpeechFeedbackID: feedback.SpeechFeedbackID,
		StatusURL:        feedback.StatusURL,
	}
}

func selectSpeechFeedbackByID(
	ctx context.Context,
	database queryable,
	ownerUserID string,
	speechFeedbackID string,
) (storedSpeechFeedback, error) {
	stored, err := scanStoredSpeechFeedback(database.QueryRow(
		ctx,
		speechFeedbackSelect+`
		WHERE feedback.id = $1
		  AND feedback.owner_user_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
		FOR SHARE OF feedback
	`,
		speechFeedbackID,
		ownerUserID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSpeechFeedback{}, ErrSpeechFeedbackNotFound
	}
	if err != nil {
		return storedSpeechFeedback{}, fmt.Errorf(
			"read SpeechFeedback: %w",
			err,
		)
	}
	return stored, nil
}

func selectSpeechFeedbackByConversationTurn(
	ctx context.Context,
	database queryable,
	ownerUserID string,
	turnID string,
) (storedSpeechFeedback, error) {
	stored, err := scanStoredSpeechFeedback(database.QueryRow(
		ctx,
		speechFeedbackSelect+`
		WHERE feedback.owner_user_id = $1
		  AND feedback.source_kind = 'CONVERSATION_TURN'
		  AND feedback.turn_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`,
		ownerUserID,
		turnID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSpeechFeedback{}, ErrSpeechFeedbackNotFound
	}
	if err != nil {
		return storedSpeechFeedback{}, fmt.Errorf(
			"read Conversation SpeechFeedback: %w",
			err,
		)
	}
	return stored, nil
}

func selectSpeechFeedbackByAgentMessage(
	ctx context.Context,
	database queryable,
	ownerUserID string,
	messageID string,
) (storedSpeechFeedback, error) {
	stored, err := scanStoredSpeechFeedback(database.QueryRow(
		ctx,
		speechFeedbackSelect+`
		WHERE feedback.owner_user_id = $1
		  AND feedback.source_kind = 'AGENT_VOICE_MESSAGE'
		  AND feedback.message_id = $2
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`,
		ownerUserID,
		messageID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSpeechFeedback{}, ErrSpeechFeedbackNotFound
	}
	if err != nil {
		return storedSpeechFeedback{}, fmt.Errorf(
			"read Agent SpeechFeedback: %w",
			err,
		)
	}
	return stored, nil
}

const speechFeedbackSelect = `
	SELECT
		feedback.id::text,
		feedback.owner_user_id::text,
		feedback.source_kind,
		feedback.practice_session_id,
		feedback.turn_id,
		feedback.input_revision,
		feedback.evidence_snapshot_id,
		feedback.thread_id::text,
		feedback.message_id::text,
		feedback.transcript_evidence_id::text,
		feedback.candidate_version,
		feedback.source_digest,
		feedback.deletion_generation,
		feedback.schema_version,
		feedback.strategy_ref,
		feedback.pipeline_version,
		feedback.feedback_status,
		feedback.scoreability_status,
		feedback.gate_status,
		feedback.reason_codes,
		feedback.stable_failure_code,
		feedback.stable_failure_retryable,
		feedback.attempt_count,
		feedback.fencing_token,
		feedback.lease_expires_at,
		feedback.created_at,
		feedback.updated_at,
		feedback.completed_at,
		coalesce(snapshot.transcript_text, evidence.confirmed_text, ''),
		coalesce(snapshot.evidence_ref_id, ''),
		coalesce(snapshot.audio_asset_id, agent_audio.audio_id::text, ''),
		coalesce(snapshot.audio_asset_version, evidence.candidate_version, 0),
		coalesce(
			snapshot.audio_checksum_sha256,
			agent_audio.checksum_sha256,
			''
		),
		coalesce(agent_audio.object_key, '')
	FROM evaluation_speech_feedbacks AS feedback
	JOIN identity_users AS owner
	  ON owner.id = feedback.owner_user_id
	LEFT JOIN evaluation_deletion_fences AS fence
	  ON fence.owner_user_id = feedback.owner_user_id
	LEFT JOIN evaluation_speech_feedback_turn_snapshots AS snapshot
	  ON snapshot.id = feedback.evidence_snapshot_id
	 AND snapshot.owner_user_id = feedback.owner_user_id
	LEFT JOIN agent_voice_transcript_evidence AS evidence
	  ON evidence.evidence_id = feedback.transcript_evidence_id
	 AND evidence.owner_user_id = feedback.owner_user_id
	LEFT JOIN agent_message_audios AS agent_audio
	  ON agent_audio.owner_user_id = feedback.owner_user_id
	 AND agent_audio.thread_id = feedback.thread_id
	 AND agent_audio.message_id = feedback.message_id
	 AND agent_audio.candidate_id = evidence.candidate_id
	 AND agent_audio.status = 'readable'
`

func scanStoredSpeechFeedback(
	row rowScanner,
) (storedSpeechFeedback, error) {
	var (
		stored               storedSpeechFeedback
		sourceKind           string
		practiceSessionID    sql.NullString
		turnID               sql.NullString
		inputRevision        sql.NullInt64
		evidenceSnapshotID   sql.NullString
		threadID             sql.NullString
		messageID            sql.NullString
		transcriptEvidenceID sql.NullString
		candidateVersion     sql.NullInt64
		digest               []byte
		status               string
		scoreability         sql.NullString
		gate                 sql.NullString
		reasonCodes          []string
		failureCode          sql.NullString
		failureRetryable     sql.NullBool
		leaseExpiresAt       sql.NullTime
		completedAt          sql.NullTime
	)
	err := row.Scan(
		&stored.Feedback.SpeechFeedbackID,
		&stored.OwnerUserID,
		&sourceKind,
		&practiceSessionID,
		&turnID,
		&inputRevision,
		&evidenceSnapshotID,
		&threadID,
		&messageID,
		&transcriptEvidenceID,
		&candidateVersion,
		&digest,
		&stored.DeletionGeneration,
		&stored.Feedback.SchemaVersion,
		&stored.Feedback.StrategyRef,
		&stored.Feedback.PipelineVersion,
		&status,
		&scoreability,
		&gate,
		&reasonCodes,
		&failureCode,
		&failureRetryable,
		&stored.AttemptCount,
		&stored.FencingToken,
		&leaseExpiresAt,
		&stored.Feedback.CreatedAt,
		&stored.Feedback.UpdatedAt,
		&completedAt,
		&stored.CanonicalText,
		&stored.EvidenceRefID,
		&stored.AudioAssetID,
		&stored.AudioAssetVersion,
		&stored.AudioChecksum,
		&stored.AudioObjectKey,
	)
	if err != nil {
		return storedSpeechFeedback{}, err
	}
	if len(digest) != sha256.Size {
		return storedSpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	copy(stored.SourceDigest[:], digest)
	stored.Feedback.Source = SpeechFeedbackSource{
		SourceKind:           SpeechFeedbackSourceKind(sourceKind),
		PracticeSessionID:    practiceSessionID.String,
		TurnID:               turnID.String,
		InputRevision:        inputRevision.Int64,
		EvidenceSnapshotID:   evidenceSnapshotID.String,
		ThreadID:             threadID.String,
		MessageID:            messageID.String,
		TranscriptEvidenceID: transcriptEvidenceID.String,
		CandidateVersion:     candidateVersion.Int64,
	}
	stored.Feedback.FeedbackStatus = SpeechFeedbackStatus(status)
	if scoreability.Valid {
		value := SpeechFeedbackScoreabilityStatus(scoreability.String)
		stored.Feedback.ScoreabilityStatus = &value
	}
	if gate.Valid {
		value := SpeechFeedbackGateStatus(gate.String)
		stored.Feedback.GateStatus = &value
	}
	stored.Feedback.ReasonCodes = make(
		[]SpeechFeedbackReasonCode,
		len(reasonCodes),
	)
	for index, code := range reasonCodes {
		stored.Feedback.ReasonCodes[index] =
			SpeechFeedbackReasonCode(code)
	}
	if failureCode.Valid && failureRetryable.Valid {
		stored.Feedback.StableFailure = &SpeechFeedbackStableFailure{
			ReasonCode: SpeechFeedbackFailureCode(failureCode.String),
			Retryable:  failureRetryable.Bool,
		}
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time.UTC()
		stored.LeaseExpiresAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		stored.Feedback.CompletedAt = &value
	}
	stored.Feedback.Items = []SpeechFeedbackItem{}
	stored.Feedback.AcousticAssessment =
		unavailableSpeechFeedbackAcoustics()
	stored.Feedback.StatusURL = SpeechFeedbackStatusURL(
		stored.Feedback.SpeechFeedbackID,
	)
	stored.Feedback.CreatedAt =
		stored.Feedback.CreatedAt.UTC()
	stored.Feedback.UpdatedAt =
		stored.Feedback.UpdatedAt.UTC()
	if !stored.Feedback.Source.valid() ||
		stored.DeletionGeneration < 0 ||
		stored.CanonicalText == "" ||
		(stored.Feedback.Source.SourceKind ==
			SpeechFeedbackSourceConversationTurn &&
			stored.EvidenceRefID == "") {
		return storedSpeechFeedback{}, ErrInvalidSpeechFeedback
	}
	return stored, nil
}

func listSpeechFeedbackItems(
	ctx context.Context,
	database queryer,
	ownerUserID string,
	speechFeedbackID string,
) ([]SpeechFeedbackItem, error) {
	rows, err := database.Query(ctx, `
		SELECT
			id::text,
			speech_feedback_id::text,
			kind,
			anchor_kind,
			coalesce(evidence_ref_id, ''),
			coalesce(turn_id, ''),
			coalesce(transcript_evidence_id::text, ''),
			coalesce(message_id::text, ''),
			start_utf8_byte,
			end_utf8_byte,
			original_excerpt,
			explanation,
			suggested_text,
			repractice_mode,
			created_at
		FROM evaluation_speech_feedback_items
		WHERE owner_user_id = $1
		  AND speech_feedback_id = $2
		ORDER BY created_at, id
	`, ownerUserID, speechFeedbackID)
	if err != nil {
		return nil, fmt.Errorf("list SpeechFeedback items: %w", err)
	}
	defer rows.Close()
	items := make([]SpeechFeedbackItem, 0)
	for rows.Next() {
		var (
			item          SpeechFeedbackItem
			anchorKind    string
			suggestedText sql.NullString
		)
		if err := rows.Scan(
			&item.FeedbackItemID,
			&item.SpeechFeedbackID,
			&item.Kind,
			&anchorKind,
			&item.Anchor.EvidenceRefID,
			&item.Anchor.TurnID,
			&item.Anchor.TranscriptEvidenceID,
			&item.Anchor.MessageID,
			&item.Anchor.StartUTF8Byte,
			&item.Anchor.EndUTF8Byte,
			&item.Anchor.OriginalExcerpt,
			&item.Explanation,
			&suggestedText,
			&item.RepracticeMode,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan SpeechFeedback item: %w", err)
		}
		item.Anchor.AnchorKind = SpeechFeedbackAnchorKind(anchorKind)
		if suggestedText.Valid {
			value := suggestedText.String
			item.SuggestedText = &value
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SpeechFeedback items: %w", err)
	}
	return items, nil
}

func mapSpeechFeedbackAccountError(err error) error {
	if errors.Is(err, ErrAccountUnavailable) {
		return ErrSpeechFeedbackNotApplicable
	}
	return err
}

var _ SpeechFeedbackRepository = (*PostgresRepository)(nil)
