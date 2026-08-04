package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestConfirmTurnWithRecordingExcludesCleanupAndReplaysAfterRestart(
	t *testing.T,
) {
	repository, audioRepository, pool :=
		newRecordingConfirmationIntegrationRepositories(t)
	actor, candidate, command, asset := createRecordingConfirmationFixture(
		t,
		repository,
		audioRepository,
		pool,
		"confirmation-wins",
	)

	recordingLocked := make(chan struct{})
	allowConfirmation := make(chan struct{})
	repository.afterRecordingLock = func() {
		close(recordingLocked)
		<-allowConfirmation
	}
	type confirmationOutcome struct {
		result practicevoice.RecordingConfirmation
		err    error
	}
	outcome := make(chan confirmationOutcome, 1)
	go func() {
		result, err :=
			repository.ConfirmTurnWithRecording(
				context.Background(),
				actor,
				command,
				candidate.ReservationID,
			)
		outcome <- confirmationOutcome{
			result: result,
			err:    err,
		}
	}()

	<-recordingLocked
	claims, err := audioRepository.ClaimExpiredUnconfirmed(
		context.Background(),
		time.Minute,
		10,
	)
	if err != nil {
		t.Fatalf("claim cleanup while confirmation holds row: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("cleanup claimed recording during confirmation: %#v", claims)
	}
	close(allowConfirmation)
	confirmed := <-outcome
	repository.afterRecordingLock = nil
	if confirmed.err != nil ||
		confirmed.result.Turn.CandidateID != candidate.ID ||
		confirmed.result.AudioAssetID != asset.ID ||
		confirmed.result.RecordingDeleted {
		t.Fatalf("atomic recording confirmation = %#v", confirmed)
	}
	assertRecordingSessionProgress(
		t,
		pool,
		actor.UserID,
		candidate.SessionID,
		practice.SessionInProgress,
		2,
		1,
		1,
		0,
	)

	restarted, err := New(pool)
	if err != nil {
		t.Fatalf("restart Voice repository: %v", err)
	}
	replayed, err :=
		restarted.ConfirmTurnWithRecording(
			context.Background(),
			actor,
			command,
			candidate.ReservationID,
		)
	if err != nil ||
		replayed.Turn.ID != confirmed.result.Turn.ID ||
		replayed.AudioAssetID != asset.ID ||
		replayed.RecordingDeleted {
		t.Fatalf(
			"confirmation replay after restart = %#v, %v",
			replayed,
			err,
		)
	}
	assertRecordingConfirmationCounts(t, pool, candidate.ID, 1, 1)
	assertRecordingSessionProgress(
		t,
		pool,
		actor.UserID,
		candidate.SessionID,
		practice.SessionInProgress,
		2,
		1,
		1,
		0,
	)
}

func TestConfirmTurnWithRecordingRollsBackWhenCleanupWins(
	t *testing.T,
) {
	repository, audioRepository, pool :=
		newRecordingConfirmationIntegrationRepositories(t)
	actor, candidate, command, _ := createRecordingConfirmationFixture(
		t,
		repository,
		audioRepository,
		pool,
		"cleanup-wins",
	)
	claims, err := audioRepository.ClaimExpiredUnconfirmed(
		context.Background(),
		time.Minute,
		10,
	)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim expired recording = %#v, %v", claims, err)
	}

	for attempt := range 2 {
		candidateRepository := repository
		if attempt == 1 {
			candidateRepository, err = New(pool)
			if err != nil {
				t.Fatalf("restart Voice repository: %v", err)
			}
		}
		_, confirmErr :=
			candidateRepository.ConfirmTurnWithRecording(
				context.Background(),
				actor,
				command,
				candidate.ReservationID,
			)
		if !errors.Is(
			confirmErr,
			practicevoice.ErrAudioAssetInvalidTransition,
		) {
			t.Fatalf(
				"cleanup-won confirmation attempt %d error = %v",
				attempt+1,
				confirmErr,
			)
		}
		assertRecordingConfirmationCounts(t, pool, candidate.ID, 0, 0)
		assertRecordingSessionProgress(
			t,
			pool,
			actor.UserID,
			candidate.SessionID,
			practice.SessionStarting,
			1,
			0,
			0,
			0,
		)
	}

	var candidateStatus string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT status
		 FROM practice_transcript_candidates
		 WHERE owner_user_id = $1 AND candidate_id = $2`,
		actor.UserID,
		candidate.ID,
	).Scan(&candidateStatus); err != nil {
		t.Fatalf("read candidate after rolled-back confirmation: %v", err)
	}
	if candidateStatus != "ready" {
		t.Fatalf("candidate status = %q, want ready", candidateStatus)
	}
}

func TestConfirmTurnWithRecordingReplaysTurnWithoutDeletedRecording(
	t *testing.T,
) {
	repository, audioRepository, pool :=
		newRecordingConfirmationIntegrationRepositories(t)
	actor, candidate, command, asset := createRecordingConfirmationFixture(
		t,
		repository,
		audioRepository,
		pool,
		"deleted-replay",
	)
	confirmed, err :=
		repository.ConfirmTurnWithRecording(
			context.Background(),
			actor,
			command,
			candidate.ReservationID,
		)
	if err != nil ||
		confirmed.AudioAssetID != asset.ID ||
		confirmed.RecordingDeleted {
		t.Fatalf(
			"initial confirmation = %#v, %v",
			confirmed,
			err,
		)
	}

	readable, err := audioRepository.GetOwned(
		context.Background(),
		actor.UserID,
		asset.ID,
	)
	if err != nil {
		t.Fatalf("load readable recording: %v", err)
	}
	deleting := readable
	deleting.Status = practicevoice.AudioAssetDeleting
	deleting.UpdatedAt = audioAssetDatabaseNow(t, pool)
	deleting.Version++
	if err := audioRepository.Save(
		context.Background(),
		deleting,
		readable.Version,
	); err != nil {
		t.Fatalf("persist deleting recording: %v", err)
	}
	deleted := deleting
	deleted.Status = practicevoice.AudioAssetDeleted
	deleted.DeletedAt = audioAssetDatabaseNow(t, pool)
	deleted.UpdatedAt = deleted.DeletedAt
	deleted.Version++
	if err := audioRepository.Save(
		context.Background(),
		deleted,
		deleting.Version,
	); err != nil {
		t.Fatalf("persist deleted recording: %v", err)
	}

	restarted, err := New(pool)
	if err != nil {
		t.Fatalf("restart Voice repository: %v", err)
	}
	replayed, err :=
		restarted.ConfirmTurnWithRecording(
			context.Background(),
			actor,
			command,
			candidate.ReservationID,
		)
	if err != nil ||
		replayed.Turn.ID != confirmed.Turn.ID ||
		replayed.AudioAssetID != "" ||
		!replayed.RecordingDeleted {
		t.Fatalf(
			"deleted recording replay = %#v, %v",
			replayed,
			err,
		)
	}
	assertRecordingConfirmationCounts(t, pool, candidate.ID, 1, 1)
	assertRecordingSessionProgress(
		t,
		pool,
		actor.UserID,
		candidate.SessionID,
		practice.SessionInProgress,
		2,
		1,
		1,
		0,
	)
}

func newRecordingConfirmationIntegrationRepositories(
	t *testing.T,
) (*Repository, *AudioAssetRepository, *pgxpool.Pool) {
	t.Helper()
	repository, pool := newIntegrationRepository(t)
	migrationSQL, err := migrations.Files.ReadFile(
		"000012_conversation_audio_assets.up.sql",
	)
	if err != nil {
		t.Fatalf("read AudioAsset migration: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		string(migrationSQL),
	); err != nil {
		t.Fatalf("apply AudioAsset migration: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		practiceAudioAssetAuthorityFixtureSQL,
	); err != nil {
		t.Fatalf("apply Practice AudioAsset authority fixture: %v", err)
	}
	audioRepository, err := NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("create AudioAsset repository: %v", err)
	}
	return repository, audioRepository, pool
}

func createRecordingConfirmationFixture(
	t *testing.T,
	repository *Repository,
	audioRepository *AudioAssetRepository,
	pool *pgxpool.Pool,
	suffix string,
) (
	practicevoice.Actor,
	practicevoice.StoredTranscriptCandidate,
	practicevoice.ConfirmTurnCommand,
	practicevoice.AudioAsset,
) {
	t.Helper()
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-"+suffix,
		"session-"+suffix,
	)
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-"+suffix,
	)
	candidate := completeTestTranscription(
		t,
		repository,
		reservation,
		"transcript-"+suffix,
	)
	now := recordingDatabaseNow(t, pool)
	asset := newTestAudioAsset(
		"asset-"+suffix,
		actor.UserID,
		reservation.ID,
		now.Add(-2*time.Hour),
		now.Add(-time.Hour),
	)
	if err := audioRepository.Create(
		context.Background(),
		asset,
	); err != nil {
		t.Fatalf("create recording fixture: %v", err)
	}
	claim, err := audioRepository.ClaimUpload(
		context.Background(),
		actor.UserID,
		asset.UploadRequestID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim recording upload: %v", err)
	}
	committed := claim.Asset
	committed.Status = practicevoice.AudioAssetMetadataCommitted
	committed.ETag = "etag-" + suffix
	committed.UpdatedAt = recordingDatabaseNow(t, pool)
	committed.Version++
	if err := audioRepository.CommitUploadClaim(
		context.Background(),
		committed,
		claim.Asset.Version,
		claim.FencingToken,
	); err != nil {
		t.Fatalf("commit recording upload: %v", err)
	}
	setAudioAssetStagedUntil(
		t,
		pool,
		asset.ID,
		recordingDatabaseNow(t, pool).Add(-time.Second),
	)
	return actor, candidate, practicevoice.ConfirmTurnCommand{
		CandidateID:     candidate.ID,
		EvidenceVersion: candidate.EvidenceVersion,
		ConfirmedText:   candidate.Text,
		IdempotencyKey:  "confirm-" + suffix,
	}, committed
}

func recordingDatabaseNow(
	t *testing.T,
	pool *pgxpool.Pool,
) time.Time {
	t.Helper()
	var now time.Time
	if err := pool.QueryRow(
		context.Background(),
		"SELECT transaction_timestamp()",
	).Scan(&now); err != nil {
		t.Fatalf("read recording database time: %v", err)
	}
	return now.UTC().Truncate(time.Microsecond)
}

func assertRecordingConfirmationCounts(
	t *testing.T,
	pool *pgxpool.Pool,
	candidateID string,
	wantTurns int,
	wantConfirmations int,
) {
	t.Helper()
	var turns int
	var confirmations int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
		    (SELECT count(*)::int
		     FROM practice_turns
		     WHERE candidate_id = $1),
		    (SELECT count(*)::int
		     FROM practice_turn_confirmations confirmations
		     JOIN practice_turns turns
		       ON turns.owner_user_id = confirmations.owner_user_id
		      AND turns.turn_id = confirmations.turn_id
		     WHERE turns.candidate_id = $1)`,
		candidateID,
	).Scan(&turns, &confirmations); err != nil {
		t.Fatalf("count recording confirmations: %v", err)
	}
	if turns != wantTurns || confirmations != wantConfirmations {
		t.Fatalf(
			"recording confirmation rows = turns %d, confirmations %d; want %d, %d",
			turns,
			confirmations,
			wantTurns,
			wantConfirmations,
		)
	}
}

func assertRecordingSessionProgress(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
	sessionID string,
	wantStatus practice.SessionStatus,
	wantVersion int,
	wantEffectiveTurns int,
	wantTurnResults int,
	wantCompletions int,
) {
	t.Helper()
	var status practice.SessionStatus
	var version int
	var effectiveTurns int
	var turnResults int
	var completions int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
		    session.status,
		    session.version,
		    session.effective_turns,
		    (SELECT count(*)::int
		     FROM practice_turn_results
		     WHERE owner_user_id = $1 AND session_id = $2),
		    (SELECT count(*)::int
		     FROM practice_completed
		     WHERE owner_user_id = $1 AND session_id = $2)
		 FROM practice_sessions AS session
		 WHERE session.owner_user_id = $1 AND session.session_id = $2`,
		ownerUserID,
		sessionID,
	).Scan(
		&status,
		&version,
		&effectiveTurns,
		&turnResults,
		&completions,
	); err != nil {
		t.Fatalf("read recording Session progress: %v", err)
	}
	if status != wantStatus ||
		version != wantVersion ||
		effectiveTurns != wantEffectiveTurns ||
		turnResults != wantTurnResults ||
		completions != wantCompletions {
		t.Fatalf(
			"recording Session progress = status %q, version %d, effective Turns %d, results %d, completions %d; want %q, %d, %d, %d, %d",
			status,
			version,
			effectiveTurns,
			turnResults,
			completions,
			wantStatus,
			wantVersion,
			wantEffectiveTurns,
			wantTurnResults,
			wantCompletions,
		)
	}
}
