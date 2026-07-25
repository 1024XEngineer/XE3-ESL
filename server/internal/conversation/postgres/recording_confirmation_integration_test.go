package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domainconversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
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
		result conversation.RecordingConfirmation
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

	restarted, err := New(pool)
	if err != nil {
		t.Fatalf("restart Conversation repository: %v", err)
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
				t.Fatalf("restart Conversation repository: %v", err)
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
			domainconversation.ErrAudioAssetInvalidTransition,
		) {
			t.Fatalf(
				"cleanup-won confirmation attempt %d error = %v",
				attempt+1,
				confirmErr,
			)
		}
		assertRecordingConfirmationCounts(t, pool, candidate.ID, 0, 0)
	}

	var candidateStatus string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT status
		 FROM conversation_transcript_candidates
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
	deleting.Status = domainconversation.AudioAssetDeleting
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
	deleted.Status = domainconversation.AudioAssetDeleted
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
		t.Fatalf("restart Conversation repository: %v", err)
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
}

func newRecordingConfirmationIntegrationRepositories(
	t *testing.T,
) (*Repository, *AudioAssetRepository, *pgxpool.Pool) {
	t.Helper()
	repository, pool := newIntegrationRepository(t)
	migrationSQL, err := migrations.Files.ReadFile(
		"000009_conversation_audio_assets.up.sql",
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
	conversation.Actor,
	conversation.TranscriptCandidate,
	conversation.ConfirmTurnCommand,
	domainconversation.AudioAsset,
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
	committed.Status = domainconversation.AudioAssetMetadataCommitted
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
	return actor, candidate, conversation.ConfirmTurnCommand{
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
		     FROM conversation_confirmed_turns
		     WHERE candidate_id = $1),
		    (SELECT count(*)::int
		     FROM conversation_turn_confirmations confirmations
		     JOIN conversation_confirmed_turns turns
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
