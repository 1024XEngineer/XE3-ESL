package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
)

func TestProgressTranscriptionAdvancesBeforeASRAndCapsRetries(t *testing.T) {
	pool := questionTipTestDatabase(t)
	ctx := context.Background()
	actor := practiceinteraction.Actor{
		UserID:    "71111111-1111-4111-8111-111111111111",
		SessionID: "auth-session-1",
	}
	const (
		planID        = "7aaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		sessionID     = "7bbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		questionID    = "7ccccccc-cccc-4ccc-8ccc-cccccccccccc"
		turnID        = "7ddddddd-dddd-4ddd-8ddd-dddddddddddd"
		assetID       = "7eeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
		reservationID = "part-2-reservation"
		clientKey     = "part-2-client-key"
		fingerprint   = "part-2-input-fingerprint"
	)

	snapshot, err := json.Marshal(practice.SessionSnapshot{
		SessionID:   sessionID,
		PlanVersion: 1,
		Experience:  practice.PracticeExperienceInterview,
		SessionPolicy: practice.SessionPolicy{
			CompletionMode:    practice.CompletionModeTurnLimited,
			MaxEffectiveTurns: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestFingerprint := sha256.Sum256([]byte("create"))
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, canonical_email) VALUES ($1, 'deferred-progress@example.com')
	`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_plans (
			user_id, plan_id, preparation_snapshot, scene_selection, session_policy,
			practice_objectives, practice_experience, status,
			initial_client_request_id, initial_request_fingerprint
		) VALUES ($1,$2,'{}','{}','{}','[{}]','INTERVIEW','ready',
			'plan-create-deferred',$3)
	`, actor.UserID, planID, requestFingerprint[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_sessions (
			session_id,user_id,plan_id,plan_version,practice_experience,
			scene_category,practice_mode,evaluation_policy_ref,status,version,
			effective_turns,plan_snapshot,participants,initial_client_request_id,
			initial_request_fingerprint,started_at
		) VALUES ($5,$1,$2,1,'INTERVIEW','INTERVIEW_PROFESSIONAL',
			'FULL_SIMULATION','interview.evaluation.v1','in_progress',1,0,
			$4::jsonb,'[{}]','session-create-deferred',$3,transaction_timestamp())
	`, actor.UserID, planID, requestFingerprint[:], snapshot, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_questions (
			question_id,session_id,objective_id,question_type,content,
			speaker_participant_id,addressee_participant_ids,sequence
		) VALUES ($1,$2,'objective-1','PRIMARY','Describe an experience.',
			'assistant',ARRAY['learner'],1)
	`, questionID, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_assets (
			id,user_id,kind,upload_request_id,object_key,content_type,size_bytes,
			checksum_sha256,etag,duration_ns,sample_rate,status
		) VALUES ($1,$2,'audio','deferred-audio-upload',
			'audio/v1/media/deferred-progress.wav','audio/wav',64044,
			repeat('a',64),'etag-1',1000000000,16000,'ready')
	`, assetID, actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_turns (
			turn_id,session_id,question_id,respondent_participant_id,sequence,
			turn_kind,status,counts_toward_turn_limit,transcription_request_id,
			transcription_client_request_id,transcription_input_fingerprint,
			asr_fencing_token,asr_lease_expires_at,asr_attempt_count,audio_asset_id
		) VALUES ($1,$2,$3,'learner',1,'EFFECTIVE','transcribing',true,
			$5,$6,$7,1,transaction_timestamp()+interval '1 minute',1,$4)
	`, turnID, sessionID, questionID, assetID, reservationID, clientKey,
		fingerprint); err != nil {
		t.Fatal(err)
	}

	repository, err := New(
		pool,
		transcriptionCompletionScheduler{},
		transcriptionTurnFeedbackScheduler{},
		transcriptionProfileScheduler{},
		transcriptionIDGenerator{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ProgressTranscription(ctx, actor, reservationID); err != nil {
		t.Fatalf("progress transcription: %v", err)
	}
	if err := repository.ProgressTranscription(ctx, actor, reservationID); err != nil {
		t.Fatalf("replay progress transcription: %v", err)
	}

	var effectiveTurns, version int
	var progressed bool
	if err := pool.QueryRow(ctx, `
		SELECT s.effective_turns,s.version,t.progressed_at IS NOT NULL
		FROM practice_sessions s JOIN practice_turns t ON t.session_id=s.session_id
		WHERE t.turn_id=$1
	`, turnID).Scan(&effectiveTurns, &version, &progressed); err != nil {
		t.Fatal(err)
	}
	if effectiveTurns != 1 || version != 2 || !progressed {
		t.Fatalf("progress = turns %d, version %d, progressed %t", effectiveTurns, version, progressed)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE practice_turns
		SET status='failed',asr_lease_expires_at=NULL,asr_attempt_count=3,
		    failure_code='provider_invalid_request',updated_at=transaction_timestamp()
		WHERE turn_id=$1
	`, turnID); err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.ReserveTranscription(
		ctx,
		actor,
		practiceinteraction.StoreReserveTranscriptionCommand{
			SessionID: sessionID, QuestionID: questionID,
			RespondentParticipantID: "learner",
			IdempotencyKey:          clientKey, InputFingerprint: fingerprint,
			LeaseDuration: time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != practiceinteraction.StoredTranscriptionFailed ||
		reservation.AttemptCount != 3 || reservation.LeaseAcquired {
		t.Fatalf("capped reservation = %#v", reservation)
	}
}

func TestProgressTranscriptionRejectsInvalidContext(t *testing.T) {
	var repository *Repository
	err := repository.ProgressTranscription(
		context.Background(), practiceinteraction.Actor{}, "",
	)
	if !errors.Is(err, practiceinteraction.ErrPersistenceInvalid) {
		t.Fatalf("invalid progress error = %v", err)
	}
}

type transcriptionCompletionScheduler struct{}

func (transcriptionCompletionScheduler) ScheduleCompletedSession(
	context.Context,
	pgx.Tx,
	practice.SessionEvidence,
) error {
	return nil
}

type transcriptionTurnFeedbackScheduler struct{}

func (transcriptionTurnFeedbackScheduler) ScheduleConfirmedTurn(
	context.Context,
	pgx.Tx,
	practice.TurnFeedbackEvidence,
) error {
	return nil
}

type transcriptionProfileScheduler struct{}

func (transcriptionProfileScheduler) ScheduleCompletedPart(
	context.Context,
	pgx.Tx,
	practice.IELTSPartProfileEvidence,
) error {
	return nil
}

type transcriptionIDGenerator struct{}

func (transcriptionIDGenerator) NewID() (string, error) {
	return "7fffffff-ffff-4fff-8fff-ffffffffffff", nil
}
