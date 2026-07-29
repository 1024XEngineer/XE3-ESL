package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	"github.com/jackc/pgx/v5"
)

// lockCurrentEvidenceSources closes the gap between composing a snapshot and
// inserting it. Every mutable or deletable source row referenced by the
// canonical payload is revalidated and held with FOR SHARE until the
// EvidenceSnapshot transaction commits.
func lockCurrentEvidenceSources(
	ctx context.Context,
	tx pgx.Tx,
	command EnsureEvidenceSnapshotCommand,
) error {
	var payload evidencePayload
	if json.Unmarshal(command.CanonicalPayload, &payload) != nil {
		return ErrInvalidRequest
	}
	var sessionVersion int
	var effectiveTurns int
	var snapshotID string
	var scenarioType string
	var scenarioModel string
	var status string
	var startedAt *time.Time
	var completedAt *time.Time
	var endReason string
	err := tx.QueryRow(ctx, `
		SELECT
			version,
			effective_turns,
			coalesce(snapshot_id, ''),
			coalesce(scenario_type, ''),
			coalesce(scenario_model, ''),
			status,
			started_at,
			completed_at,
			coalesce(end_reason, '')
		FROM practice_sessions
		WHERE owner_user_id = $1 AND session_id = $2
		FOR SHARE
	`, command.OwnerUserID, command.PracticeSessionID).Scan(
		&sessionVersion,
		&effectiveTurns,
		&snapshotID,
		&scenarioType,
		&scenarioModel,
		&status,
		&startedAt,
		&completedAt,
		&endReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock EvidenceSnapshot Practice Session: %w", err)
	}
	context := payload.PracticeContext
	if status != "completed" ||
		sessionVersion != context.SessionVersion ||
		effectiveTurns != len(payload.ConfirmedTurns) ||
		snapshotID != context.SessionSnapshotID ||
		scenarioType != context.SceneFamily ||
		scenarioModel != context.ScenarioModel {
		return ErrInvalidRequest
	}
	var persistedSnapshotID string
	var snapshotDocument []byte
	err = tx.QueryRow(ctx, `
		SELECT snapshot_id, snapshot_document
		FROM practice_session_snapshots
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND snapshot_id = $3
		FOR SHARE
	`, command.OwnerUserID, command.PracticeSessionID,
		context.SessionSnapshotID).Scan(
		&persistedSnapshotID,
		&snapshotDocument,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf(
			"lock EvidenceSnapshot Practice source snapshot: %w",
			err,
		)
	}
	var sourceSnapshot practice.ContextSessionSnapshot
	if json.Unmarshal(snapshotDocument, &sourceSnapshot) != nil {
		return ErrInvalidRequest
	}
	sourceSession := practice.ContextSession{
		ID:             command.PracticeSessionID,
		ScenarioType:   practice.ScenarioFamily(scenarioType),
		ScenarioModel:  practice.ScenarioModel(scenarioModel),
		SnapshotID:     snapshotID,
		Status:         practice.ContextSessionStatus(status),
		Version:        sessionVersion,
		EffectiveTurns: effectiveTurns,
		StartedAt:      startedAt,
		EndedAt:        completedAt,
		EndReason:      endReason,
	}
	if !validCompletedEvidenceSession(
		command.OwnerUserID,
		command.PracticeSessionID,
		command.SceneType,
		sourceSession,
		sourceSnapshot,
	) {
		return ErrInvalidRequest
	}
	expectedContext, _, _, ok := evidencePracticeContextFromSnapshot(
		command.OwnerUserID,
		sourceSession,
		sourceSnapshot,
	)
	if !ok || !reflect.DeepEqual(expectedContext, context) {
		return ErrInvalidRequest
	}
	var questionCount int
	var turnCount int
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*)
			 FROM conversation_questions
			 WHERE owner_user_id = $1
			   AND practice_session_id = $2),
			(SELECT count(*)
			 FROM conversation_confirmed_turns
			 WHERE owner_user_id = $1
			   AND practice_session_id = $2)
	`, command.OwnerUserID, command.PracticeSessionID).Scan(
		&questionCount,
		&turnCount,
	); err != nil {
		return fmt.Errorf("count EvidenceSnapshot Conversation source: %w", err)
	}
	if questionCount != len(payload.OpportunityManifest) ||
		turnCount != len(payload.ConfirmedTurns) {
		return ErrInvalidRequest
	}

	opportunitiesByQuestion := make(
		map[string]evidenceOpportunity,
		len(payload.OpportunityManifest),
	)
	for _, opportunity := range payload.OpportunityManifest {
		if err := lockEvidenceQuestion(
			ctx,
			tx,
			command,
			opportunity,
		); err != nil {
			return err
		}
		opportunitiesByQuestion[opportunity.QuestionID] = opportunity
	}
	refsByTurn := make(map[string]evidenceRef, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		refsByTurn[ref.TurnID] = ref
	}
	asrByTurn := make(
		map[string]evidenceASRLineage,
		len(payload.ProviderLineage.ASR),
	)
	for _, lineage := range payload.ProviderLineage.ASR {
		asrByTurn[lineage.TurnID] = lineage
	}
	for _, turn := range payload.ConfirmedTurns {
		ref, refOK := refsByTurn[turn.TurnID]
		lineage, lineageOK := asrByTurn[turn.TurnID]
		opportunity, opportunityOK := opportunitiesByQuestion[turn.QuestionID]
		if !refOK || !lineageOK || !opportunityOK {
			return ErrInvalidRequest
		}
		if err := lockEvidenceTurn(
			ctx,
			tx,
			command,
			turn,
			ref,
			lineage,
			opportunity,
		); err != nil {
			return err
		}
	}
	return nil
}

func lockEvidenceQuestion(
	ctx context.Context,
	tx pgx.Tx,
	command EnsureEvidenceSnapshotCommand,
	expected evidenceOpportunity,
) error {
	var sessionID string
	var speakerID string
	var addressees []string
	var objectiveID string
	var questionType string
	var parentQuestionID string
	var content string
	var sequence int
	err := tx.QueryRow(ctx, `
		SELECT
			practice_session_id,
			speaker_participant_id,
			addressee_participant_ids,
			objective_id,
			question_type,
			coalesce(parent_question_id, ''),
			content,
			sequence
		FROM conversation_questions
		WHERE owner_user_id = $1 AND question_id = $2
		FOR SHARE
	`, command.OwnerUserID, expected.QuestionID).Scan(
		&sessionID,
		&speakerID,
		&addressees,
		&objectiveID,
		&questionType,
		&parentQuestionID,
		&content,
		&sequence,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock EvidenceSnapshot question: %w", err)
	}
	slices.Sort(addressees)
	if sessionID != command.PracticeSessionID ||
		speakerID != expected.SpeakerParticipantID ||
		!slices.Equal(addressees, expected.AddresseeParticipantIDs) ||
		objectiveID != expected.ObjectiveID ||
		questionType != expected.QuestionType ||
		parentQuestionID != expected.ParentQuestionID ||
		content != expected.QuestionText ||
		sequence != expected.Sequence {
		return ErrInvalidRequest
	}
	return nil
}

func lockEvidenceTurn(
	ctx context.Context,
	tx pgx.Tx,
	command EnsureEvidenceSnapshotCommand,
	expected evidenceConfirmedTurn,
	ref evidenceRef,
	lineage evidenceASRLineage,
	opportunity evidenceOpportunity,
) error {
	var candidateID string
	var questionID string
	var sessionID string
	var speakerID string
	var addressees []string
	var respondentID string
	var sequence int
	var interactionMode string
	var answerText string
	var evidenceVersion int64
	err := tx.QueryRow(ctx, `
		SELECT
			candidate_id,
			question_id,
			practice_session_id,
			speaker_participant_id,
			addressee_participant_ids,
			respondent_participant_id,
			sequence,
			interaction_mode,
			answer_text,
			evidence_version
		FROM conversation_confirmed_turns
		WHERE owner_user_id = $1 AND turn_id = $2
		FOR SHARE
	`, command.OwnerUserID, expected.TurnID).Scan(
		&candidateID,
		&questionID,
		&sessionID,
		&speakerID,
		&addressees,
		&respondentID,
		&sequence,
		&interactionMode,
		&answerText,
		&evidenceVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock EvidenceSnapshot confirmed Turn: %w", err)
	}
	slices.Sort(addressees)
	if candidateID != ref.Lineage.CandidateID ||
		questionID != expected.QuestionID ||
		sessionID != command.PracticeSessionID ||
		speakerID != opportunity.SpeakerParticipantID ||
		!slices.Equal(addressees, opportunity.AddresseeParticipantIDs) ||
		respondentID != expected.RespondentParticipantID ||
		sequence != expected.Sequence ||
		interactionMode != expected.InteractionMode ||
		answerText != expected.Transcript.Text ||
		evidenceVersion != expected.Transcript.EvidenceVersion {
		return ErrInvalidRequest
	}
	if err := lockEvidenceCandidate(
		ctx,
		tx,
		command,
		expected,
		ref,
		lineage,
	); err != nil {
		return err
	}
	return lockEvidenceAudio(ctx, tx, command, expected, candidateID)
}

func lockEvidenceCandidate(
	ctx context.Context,
	tx pgx.Tx,
	command EnsureEvidenceSnapshotCommand,
	turn evidenceConfirmedTurn,
	ref evidenceRef,
	lineage evidenceASRLineage,
) error {
	var sessionID string
	var questionID string
	var respondentID string
	var transcriptID string
	var evidenceVersion int64
	var provider string
	var model string
	var providerRequestID string
	var transcriptText string
	var status string
	err := tx.QueryRow(ctx, `
		SELECT
			practice_session_id,
			question_id,
			respondent_participant_id,
			transcript_id,
			evidence_version,
			provider,
			model,
			provider_request_id,
			transcript_text,
			status
		FROM conversation_transcript_candidates
		WHERE owner_user_id = $1 AND candidate_id = $2
		FOR SHARE
	`, command.OwnerUserID, ref.Lineage.CandidateID).Scan(
		&sessionID,
		&questionID,
		&respondentID,
		&transcriptID,
		&evidenceVersion,
		&provider,
		&model,
		&providerRequestID,
		&transcriptText,
		&status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf(
			"lock EvidenceSnapshot transcript candidate: %w",
			err,
		)
	}
	if sessionID != command.PracticeSessionID ||
		questionID != turn.QuestionID ||
		respondentID != turn.RespondentParticipantID ||
		transcriptID != turn.Transcript.ID ||
		evidenceVersion != turn.Transcript.EvidenceVersion ||
		provider != ref.Lineage.ASRProvider ||
		provider != lineage.Provider ||
		model != ref.Lineage.ASRModel ||
		model != lineage.Model ||
		providerRequestID != lineage.ProviderRequestID ||
		transcriptText != turn.Transcript.Text ||
		status != "confirmed" {
		return ErrInvalidRequest
	}
	return nil
}

func lockEvidenceAudio(
	ctx context.Context,
	tx pgx.Tx,
	command EnsureEvidenceSnapshotCommand,
	turn evidenceConfirmedTurn,
	candidateID string,
) error {
	if turn.Audio.AudioAssetID == "" {
		var unexpectedID string
		err := tx.QueryRow(ctx, `
			SELECT audio_asset_id
			FROM conversation_audio_assets
			WHERE owner_user_id = $1 AND turn_id = $2
			FOR SHARE
		`, command.OwnerUserID, turn.TurnID).Scan(&unexpectedID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf(
				"verify absent EvidenceSnapshot audio: %w",
				err,
			)
		}
		return ErrInvalidRequest
	}
	var persistedCandidateID string
	var persistedTurnID string
	var contentType string
	var sizeBytes int64
	var checksum string
	var durationNanoseconds int64
	var status string
	var version int64
	err := tx.QueryRow(ctx, `
		SELECT
			coalesce(candidate_id, ''),
			coalesce(turn_id, ''),
			content_type,
			size_bytes,
			checksum_sha256,
			duration_ns,
			status,
			version
		FROM conversation_audio_assets
		WHERE owner_user_id = $1 AND audio_asset_id = $2
		FOR SHARE
	`, command.OwnerUserID, turn.Audio.AudioAssetID).Scan(
		&persistedCandidateID,
		&persistedTurnID,
		&contentType,
		&sizeBytes,
		&checksum,
		&durationNanoseconds,
		&status,
		&version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock EvidenceSnapshot audio: %w", err)
	}
	duration := time.Duration(durationNanoseconds)
	durationMS := int64((duration-1)/time.Millisecond) + 1
	if persistedCandidateID != candidateID ||
		persistedTurnID != turn.TurnID ||
		contentType != turn.Audio.ContentType ||
		sizeBytes != turn.Audio.SizeBytes ||
		checksum != turn.Audio.ChecksumSHA256 ||
		durationMS != turn.Audio.DurationMS ||
		status != turn.Audio.Status ||
		version != int64(turn.Audio.Version) {
		return ErrInvalidRequest
	}
	return nil
}
