package speechfeedback

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/jackc/pgx/v5"
)

type ieltsSpeakingAcousticProjection struct {
	FeedbackStatus    SpeechFeedbackStatus
	EvidenceVersion   int64
	AudioAssetID      string
	AudioAssetVersion int64
	AudioChecksum     string
	CompletedAt       time.Time
	Assessment        SpeechFeedbackAcousticAssessment
}

type IELTSSpeakingFeedbackReader interface {
	ReadIELTSSpeakingAcousticProjection(
		context.Context,
		string,
		string,
	) (ieltsSpeakingAcousticProjection, bool, error)
}

type ieltsSpeakingAcousticSource struct {
	feedback IELTSSpeakingFeedbackReader
}

func NewIELTSSpeakingAcousticSource(
	feedback IELTSSpeakingFeedbackReader,
) (scoring.IELTSSpeakingAcousticSource, error) {
	if feedback == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &ieltsSpeakingAcousticSource{feedback: feedback}, nil
}

func (source *ieltsSpeakingAcousticSource) ReadIELTSSpeakingAcoustics(
	ctx context.Context,
	ownerUserID string,
	requests []scoring.IELTSSpeakingAcousticRequest,
	completedBy time.Time,
) (scoring.IELTSSpeakingAcousticRead, error) {
	if source == nil || source.feedback == nil || ctx == nil ||
		completedBy.IsZero() {
		return scoring.IELTSSpeakingAcousticRead{}, evaluation.ErrInvalidRequest
	}
	result := scoring.IELTSSpeakingAcousticRead{
		Values: make([]scoring.IELTSSpeakingTurnAcoustics, 0, len(requests)),
	}
	for _, request := range requests {
		if request.RecordingDurationMS == 0 {
			continue
		}
		projection, found, err := source.feedback.
			ReadIELTSSpeakingAcousticProjection(
				ctx,
				ownerUserID,
				request.TurnID,
			)
		if err != nil {
			return scoring.IELTSSpeakingAcousticRead{}, err
		}
		if !found {
			result.PendingTurnIDs = append(
				result.PendingTurnIDs,
				request.TurnID,
			)
			continue
		}
		if projection.EvidenceVersion != request.EvidenceVersion ||
			projection.AudioAssetID != request.AudioAssetID ||
			projection.AudioAssetVersion != int64(request.AudioAssetVersion) ||
			projection.AudioChecksum != request.AudioChecksumSHA256 {
			return scoring.IELTSSpeakingAcousticRead{},
				scoring.ErrIELTSAcousticEvidenceInvalid
		}
		switch projection.FeedbackStatus {
		case SpeechFeedbackQueued, SpeechFeedbackRunning:
			result.PendingTurnIDs = append(
				result.PendingTurnIDs,
				request.TurnID,
			)
			continue
		case SpeechFeedbackFailed:
			continue
		case SpeechFeedbackReady:
		default:
			return scoring.IELTSSpeakingAcousticRead{}, errors.New(
				"evaluation speech feedback: unsupported feedback status",
			)
		}
		if projection.CompletedAt.IsZero() {
			return scoring.IELTSSpeakingAcousticRead{},
				ErrInvalidSpeechFeedback
		}
		if projection.CompletedAt.After(completedBy) {
			continue
		}
		assessment := projection.Assessment
		if assessment.Pronunciation != SpeechFeedbackAssessed ||
			assessment.AcousticFluency != SpeechFeedbackAssessed {
			continue
		}
		pronunciation := assessment.PronunciationScore
		if pronunciation == nil {
			pronunciation = assessment.AccuracyScore
		}
		if pronunciation == nil ||
			(assessment.FluencyScore == nil &&
				assessment.SpeakingSpeedWPM == nil) {
			continue
		}
		result.Values = append(result.Values, scoring.IELTSSpeakingTurnAcoustics{
			TurnID:               request.TurnID,
			EvidenceRefID:        request.EvidenceRefID,
			PronunciationScore:   *pronunciation,
			AcousticFluencyScore: assessment.FluencyScore,
			SpeakingSpeedWPM:     assessment.SpeakingSpeedWPM,
			Provider:             assessment.Provider,
			ProviderRun: acousticProviderRunHash(
				assessment.ProviderSession,
			),
		})
	}
	return result, nil
}

func (r *PostgresRepository) ReadIELTSSpeakingAcousticProjection(
	ctx context.Context,
	ownerUserID string,
	turnID string,
) (ieltsSpeakingAcousticProjection, bool, error) {
	if r == nil || r.pool == nil || ctx == nil || !validUUID(ownerUserID) ||
		!validSpeechFeedbackIdentifier(turnID) {
		return ieltsSpeakingAcousticProjection{}, false,
			ErrInvalidSpeechFeedback
	}
	var projection ieltsSpeakingAcousticProjection
	var (
		completedAt   sql.NullTime
		accuracy      sql.NullFloat64
		fluency       sql.NullFloat64
		integrity     sql.NullFloat64
		pronunciation sql.NullFloat64
		speed         sql.NullFloat64
		provider      sql.NullString
		providerRun   sql.NullString
		category      sql.NullString
	)
	err := r.pool.QueryRow(ctx, `
		SELECT
			feedback.feedback_status,
			feedback.completed_at,
			snapshot.input_revision,
			snapshot.audio_asset_id,
			snapshot.audio_asset_version,
			snapshot.audio_checksum_sha256,
			acoustic.accuracy_score,
			acoustic.fluency_score,
			acoustic.integrity_score,
			acoustic.phone_score,
			acoustic.speaking_speed_wpm,
			acoustic.provider,
			acoustic.provider_session_id,
			acoustic.category
		FROM evaluation_speech_feedback_turn_snapshots AS snapshot
		JOIN evaluation_speech_feedbacks AS feedback
		  ON feedback.owner_user_id = snapshot.owner_user_id
		 AND feedback.evidence_snapshot_id = snapshot.id
		 AND feedback.turn_id = snapshot.turn_id
		LEFT JOIN evaluation_speech_feedback_acoustic_evidence AS acoustic
		  ON acoustic.owner_user_id = feedback.owner_user_id
		 AND acoustic.speech_feedback_id = feedback.id
		WHERE snapshot.owner_user_id = $1
		  AND snapshot.turn_id = $2
	`, ownerUserID, turnID).Scan(
		&projection.FeedbackStatus,
		&completedAt,
		&projection.EvidenceVersion,
		&projection.AudioAssetID,
		&projection.AudioAssetVersion,
		&projection.AudioChecksum,
		&accuracy,
		&fluency,
		&integrity,
		&pronunciation,
		&speed,
		&provider,
		&providerRun,
		&category,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ieltsSpeakingAcousticProjection{}, false, nil
	}
	if err != nil {
		return ieltsSpeakingAcousticProjection{}, false, fmt.Errorf(
			"read IELTS Speaking acoustic projection: %w",
			err,
		)
	}
	if completedAt.Valid {
		projection.CompletedAt = completedAt.Time
	}
	if provider.Valid {
		projection.Assessment = SpeechFeedbackAcousticAssessment{
			Pronunciation:      SpeechFeedbackAssessed,
			AcousticFluency:    SpeechFeedbackAssessed,
			FluencyScore:       nullableFloat64(fluency),
			IntegrityScore:     nullableFloat64(integrity),
			PronunciationScore: nullableFloat64(pronunciation),
			SpeakingSpeedWPM:   nullableFloat64(speed),
			Provider:           provider.String,
			ProviderSession:    providerRun.String,
			Category:           category.String,
			Notice:             SpeechFeedbackAcousticNotice,
		}
		if category.String == "topic" {
			projection.Assessment.SemanticScore = nullableFloat64(accuracy)
		} else {
			projection.Assessment.AccuracyScore = nullableFloat64(accuracy)
			projection.Assessment.Integrity = SpeechFeedbackAssessed
		}
		if !projection.Assessment.valid() {
			return ieltsSpeakingAcousticProjection{}, false,
				ErrInvalidSpeechFeedback
		}
	} else {
		projection.Assessment = unavailableSpeechFeedbackAcoustics()
	}
	return projection, true, nil
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func acousticProviderRunHash(providerSession string) string {
	digest := sha256.Sum256([]byte(providerSession))
	return "run_" + hex.EncodeToString(digest[:12])
}

var _ scoring.IELTSSpeakingAcousticSource = (*ieltsSpeakingAcousticSource)(nil)
var _ IELTSSpeakingFeedbackReader = (*PostgresRepository)(nil)
