package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	voicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/jackc/pgx/v5"
)

func TestPostgresAgentVoiceInputConfirmationHistoryAndDeletion(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	goalService, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	repository, err := voicepostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new voice repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new fake ObjectStore: %v", err)
	}
	service, err := agentvoice.NewService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		newFixedSpeechRecognizer(successfulVoiceTranscription()),
		newFixedSpeechSynthesizer(agentvoice.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatalf("new voice input service: %v", err)
	}

	candidate, err := service.Upload(
		context.Background(),
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "postgres-voice-upload-0001",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x51)),
		},
	)
	if err != nil {
		t.Fatalf("upload voice candidate: %v", err)
	}
	if candidate.Status != agentvoice.StatusReady ||
		candidate.CandidateVersion != 1 ||
		!store.Has(candidate.ObjectKey) {
		t.Fatalf("unexpected ready candidate: %#v", candidate)
	}
	if _, err := service.GetCandidate(
		context.Background(),
		testActorB(),
		candidate.ID,
	); !errors.Is(err, agentvoice.ErrNotFound) {
		t.Fatalf("cross-owner candidate read error = %v, want not found", err)
	}

	command := agentvoice.ConfirmCandidateCommand{
		CandidateID:      candidate.ID,
		CandidateVersion: candidate.CandidateVersion,
		ClientMessageID:  "postgres-voice-message-0001",
		ConfirmedText:    "The user-confirmed canonical voice transcript.",
	}
	confirmation, err := service.Confirm(
		context.Background(),
		actor,
		command,
	)
	if err != nil {
		t.Fatalf("confirm voice candidate: %v", err)
	}
	if confirmation.Run.Status != agentrun.StatusCompleted ||
		confirmation.Message.Modality != conversation.MessageModalityVoice ||
		confirmation.Message.Content != command.ConfirmedText ||
		confirmation.Message.Audio == nil ||
		confirmation.Message.Audio.ID != confirmation.Audio.ID {
		t.Fatalf("unexpected confirmation: %#v", confirmation)
	}
	if confirmation.Evidence.ASRCandidateText !=
		successfulVoiceTranscription().Transcript ||
		confirmation.Evidence.ConfirmedText != command.ConfirmedText {
		t.Fatalf("candidate and confirmed evidence were conflated: %#v",
			confirmation.Evidence)
	}
	replayed, err := service.Confirm(
		context.Background(),
		actor,
		command,
	)
	if err != nil || replayed.Created ||
		replayed.Message.ID != confirmation.Message.ID ||
		replayed.Run.ID != confirmation.Run.ID {
		t.Fatalf("confirmation replay = %#v, error = %v", replayed, err)
	}
	if _, err := dataService.AppendUserMessage(
		context.Background(),
		actor,
		thread.ID,
		command.ClientMessageID,
		command.ConfirmedText,
	); !errors.Is(err, conversation.ErrIdempotencyConflict) {
		t.Fatalf(
			"text Message replay of voice client ID error = %v, want idempotency conflict",
			err,
		)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		command.ClientMessageID,
		command.ConfirmedText,
	); !errors.Is(err, agentrun.ErrIdempotencyConflict) {
		t.Fatalf(
			"text Run replay of voice client ID error = %v, want idempotency conflict",
			err,
		)
	}
	otherCandidate, err := service.Upload(
		context.Background(),
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "postgres-voice-upload-0002",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x52)),
		},
	)
	if err != nil {
		t.Fatalf("upload second voice candidate: %v", err)
	}
	assertPostgresConstraint(
		t,
		database.pool,
		`UPDATE agent_voice_candidates
SET status = 'confirmed',
    confirmed_message_id = $2,
    confirmed_run_id = $3,
    message_audio_id = $4,
    confirmed_at = CURRENT_TIMESTAMP
WHERE candidate_id = $1`,
		[]any{
			otherCandidate.ID,
			confirmation.Message.ID,
			confirmation.Run.ID,
			confirmation.Audio.ID,
		},
		"23503",
		"agent_voice_candidates_message_audio_fkey",
	)
	var messageCount, runCount, audioCount, evidenceCount int
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT COUNT(*) FROM agent_messages
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_runs
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_message_audios
     WHERE owner_user_id = $1 AND thread_id = $2),
    (SELECT COUNT(*) FROM agent_voice_transcript_evidence
     WHERE owner_user_id = $1 AND thread_id = $2)`,
		actor.UserID,
		thread.ID,
	).Scan(
		&messageCount,
		&runCount,
		&audioCount,
		&evidenceCount,
	); err != nil {
		t.Fatalf("count durable voice records: %v", err)
	}
	if messageCount != 2 || runCount != 1 ||
		audioCount != 1 || evidenceCount != 1 {
		t.Fatalf(
			"durable counts = messages %d runs %d audios %d evidence %d",
			messageCount,
			runCount,
			audioCount,
			evidenceCount,
		)
	}

	newest, err := dataService.PageMessages(
		context.Background(),
		actor,
		thread.ID,
		1,
		"",
	)
	if err != nil {
		t.Fatalf("page newest Message: %v", err)
	}
	if len(newest.Messages) != 1 ||
		newest.Messages[0].Role != conversation.MessageRoleAssistant ||
		newest.Messages[0].Modality != conversation.MessageModalityText ||
		newest.NextCursor == "" {
		t.Fatalf("unexpected newest page: %#v", newest)
	}
	older, err := dataService.PageMessages(
		context.Background(),
		actor,
		thread.ID,
		1,
		newest.NextCursor,
	)
	if err != nil {
		t.Fatalf("page voice input Message: %v", err)
	}
	assertPersistedVoiceInputMessage(
		t,
		older.Messages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		conversation.MessageAudioReadable,
	)
	if older.NextCursor != "" {
		t.Fatalf("unexpected terminal voice page cursor %q", older.NextCursor)
	}

	restartedRepository, err := conversationpostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new restarted Agent repository: %v", err)
	}
	restartedData, err := conversation.NewService(restartedRepository, goalService)
	if err != nil {
		t.Fatalf("new restarted Agent data service: %v", err)
	}
	restartedMessages, err := restartedData.ListMessages(
		context.Background(),
		actor,
		thread.ID,
	)
	if err != nil {
		t.Fatalf("list Messages after restart: %v", err)
	}
	assertPersistedVoiceInputMessage(
		t,
		restartedMessages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		conversation.MessageAudioReadable,
	)

	if _, err := service.Playback(
		context.Background(),
		testActorB(),
		confirmation.Audio.ID,
	); !errors.Is(err, agentvoice.ErrNotFound) {
		t.Fatalf("cross-owner playback error = %v, want not found", err)
	}
	if err := service.DeleteAudio(
		context.Background(),
		actor,
		confirmation.Audio.ID,
	); err != nil {
		t.Fatalf("delete Message audio: %v", err)
	}
	if store.Has(candidate.ObjectKey) {
		t.Fatal("deleted Message audio remained in private ObjectStore")
	}
	deletedMessages, err := restartedData.ListMessages(
		context.Background(),
		actor,
		thread.ID,
	)
	if err != nil {
		t.Fatalf("list Messages after audio deletion: %v", err)
	}
	deleted := assertPersistedVoiceInputMessage(
		t,
		deletedMessages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		conversation.MessageAudioDeleted,
	)
	if deleted.Content != command.ConfirmedText {
		t.Fatalf("audio deletion changed canonical Message content %q",
			deleted.Content)
	}
	publicJSON, err := json.Marshal(messageResponse(deleted))
	if err != nil {
		t.Fatalf("marshal public Message projection: %v", err)
	}
	for _, secret := range []string{
		"object_key",
		"checksum",
		"playback_path",
		candidate.ObjectKey,
		candidate.ChecksumSHA256,
	} {
		if strings.Contains(string(publicJSON), secret) {
			t.Fatalf("deleted public projection leaked %q: %s",
				secret, publicJSON)
		}
	}
}

func TestPostgresAgentVoiceConfirmationRollsBackWhenRunCreationConflicts(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	voiceRepository, err := voicepostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new voice repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new fake ObjectStore: %v", err)
	}
	service, err := agentvoice.NewService(
		voiceRepository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		newFixedSpeechRecognizer(successfulVoiceTranscription()),
		newFixedSpeechSynthesizer(agentvoice.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatalf("new voice service: %v", err)
	}
	candidate, err := service.Upload(
		context.Background(),
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "postgres-voice-rollback-upload",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x62)),
		},
	)
	if err != nil {
		t.Fatalf("upload voice candidate: %v", err)
	}
	blockingSubmission, err := repositories.run.CreateInitial(
		context.Background(),
		actor.UserID,
		thread.ID,
		"postgres-voice-rollback-blocking-message",
		"Keep one Run pending to force the confirmation transaction to fail.",
		testRunConfiguration,
	)
	if err != nil {
		t.Fatalf("create blocking pending Run: %v", err)
	}
	if blockingSubmission.Run.Status != agentrun.StatusPending {
		t.Fatalf("blocking Run status = %q, want pending",
			blockingSubmission.Run.Status)
	}

	_, err = service.Confirm(
		context.Background(),
		actor,
		agentvoice.ConfirmCandidateCommand{
			CandidateID:      candidate.ID,
			CandidateVersion: candidate.CandidateVersion,
			ClientMessageID:  "postgres-voice-rollback-message",
			ConfirmedText:    "This confirmation must roll back as one transaction.",
		},
	)
	if !errors.Is(err, agentvoice.ErrConflict) {
		t.Fatalf("confirm error = %v, want conflict", err)
	}

	var (
		candidateStatus    string
		confirmedMessageID string
		confirmedRunID     string
		messageAudioID     string
		messageCount       int
		runCount           int
		audioCount         int
		evidenceCount      int
		nextSequence       int64
	)
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    candidate.status,
    COALESCE(candidate.confirmed_message_id::text, ''),
    COALESCE(candidate.confirmed_run_id::text, ''),
    COALESCE(candidate.message_audio_id::text, ''),
    (SELECT COUNT(*) FROM agent_messages AS message
     WHERE message.owner_user_id = candidate.owner_user_id
       AND message.thread_id = candidate.thread_id),
    (SELECT COUNT(*) FROM agent_runs AS run
     WHERE run.owner_user_id = candidate.owner_user_id
       AND run.thread_id = candidate.thread_id),
    (SELECT COUNT(*) FROM agent_message_audios AS audio
     WHERE audio.candidate_id = candidate.candidate_id),
    (SELECT COUNT(*) FROM agent_voice_transcript_evidence AS evidence
     WHERE evidence.candidate_id = candidate.candidate_id),
    thread.next_message_sequence
FROM agent_voice_candidates AS candidate
INNER JOIN agent_threads AS thread
  ON thread.id = candidate.thread_id
 AND thread.owner_user_id = candidate.owner_user_id
WHERE candidate.candidate_id = $1
  AND candidate.owner_user_id = $2`,
		candidate.ID,
		actor.UserID,
	).Scan(
		&candidateStatus,
		&confirmedMessageID,
		&confirmedRunID,
		&messageAudioID,
		&messageCount,
		&runCount,
		&audioCount,
		&evidenceCount,
		&nextSequence,
	); err != nil {
		t.Fatalf("read rolled-back confirmation state: %v", err)
	}
	if candidateStatus != string(agentvoice.StatusReady) ||
		confirmedMessageID != "" || confirmedRunID != "" ||
		messageAudioID != "" || messageCount != 1 || runCount != 1 ||
		audioCount != 0 || evidenceCount != 0 || nextSequence != 2 {
		t.Fatalf(
			"state after rollback = status %q message %q run %q audio %q counts %d/%d/%d/%d next %d",
			candidateStatus,
			confirmedMessageID,
			confirmedRunID,
			messageAudioID,
			messageCount,
			runCount,
			audioCount,
			evidenceCount,
			nextSequence,
		)
	}
}

func TestPostgresAgentVoiceInputDeletionLocksCandidateBeforeAudio(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	repository, err := voicepostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new voice repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new fake ObjectStore: %v", err)
	}
	service, err := agentvoice.NewService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		newFixedSpeechRecognizer(successfulVoiceTranscription()),
		newFixedSpeechSynthesizer(agentvoice.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatalf("new voice input service: %v", err)
	}
	candidate, err := service.Upload(
		context.Background(),
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "postgres-voice-lock-order-upload",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x61)),
		},
	)
	if err != nil {
		t.Fatalf("upload voice candidate: %v", err)
	}
	confirmation, err := service.Confirm(
		context.Background(),
		actor,
		agentvoice.ConfirmCandidateCommand{
			CandidateID:      candidate.ID,
			CandidateVersion: candidate.CandidateVersion,
			ClientMessageID:  "postgres-voice-lock-order-message",
			ConfirmedText:    "Confirm deletion lock ordering.",
		},
	)
	if err != nil {
		t.Fatalf("confirm voice candidate: %v", err)
	}

	const advisoryLockKey int64 = 731136
	blocker, err := database.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire advisory lock connection: %v", err)
	}
	if _, err := blocker.Exec(
		context.Background(),
		"SELECT pg_advisory_lock($1)",
		advisoryLockKey,
	); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}
	advisoryHeld := true
	t.Cleanup(func() {
		if advisoryHeld {
			_, _ = blocker.Exec(
				context.Background(),
				"SELECT pg_advisory_unlock($1)",
				advisoryLockKey,
			)
		}
		blocker.Release()
	})
	if _, err := database.pool.Exec(context.Background(), `
CREATE FUNCTION agent_voice_audio_delete_lock_order()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(731136);
    RETURN NEW;
END
$$;
CREATE TRIGGER agent_voice_audio_delete_lock_order
BEFORE UPDATE ON agent_message_audios
FOR EACH ROW
WHEN (NEW.status = 'deleting')
EXECUTE FUNCTION agent_voice_audio_delete_lock_order()`); err != nil {
		t.Fatalf("install deletion lock-order trigger: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	go func() {
		_, beginErr := repository.BeginMessageAudioDeletion(
			ctx,
			actor.UserID,
			confirmation.Audio.ID,
		)
		errs <- beginErr
	}()
	waitForAdvisoryLockWaiter(
		t,
		ctx,
		database.pool,
		advisoryLockKey,
	)
	go func() {
		_, beginErr := repository.BeginCandidateDeletion(
			ctx,
			actor.UserID,
			candidate.ID,
		)
		errs <- beginErr
	}()
	waitForPostgresLockWaiters(t, ctx, database.pool, 2)

	if _, err := blocker.Exec(
		context.Background(),
		"SELECT pg_advisory_unlock($1)",
		advisoryLockKey,
	); err != nil {
		t.Fatalf("release advisory lock: %v", err)
	}
	advisoryHeld = false
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent deletion begin failed: %v", err)
		}
	}
	if _, err := repository.FinishMessageAudioDeletion(
		ctx,
		actor.UserID,
		confirmation.Audio.ID,
	); err != nil {
		t.Fatalf("finish Message audio deletion: %v", err)
	}
	if _, err := repository.FinishCandidateDeletion(
		ctx,
		actor.UserID,
		candidate.ID,
	); err != nil {
		t.Fatalf("finish voice candidate deletion: %v", err)
	}
}

func TestPostgresAgentVoiceInputCleanupRecoversExpiredLeaseAndDeletingOwners(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	repository, err := voicepostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new voice repository: %v", err)
	}
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service, err := agentvoice.NewService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		newFixedSpeechRecognizer(successfulVoiceTranscription()),
		newFixedSpeechSynthesizer(agentvoice.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actorA := testActorA()
	actorB := testActorB()
	threadA, err := dataService.CreateThread(
		context.Background(),
		actorA,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := dataService.CreateThread(
		context.Background(),
		actorB,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	expired, err := service.Upload(
		context.Background(),
		actorA,
		agentvoice.UploadRequest{
			ThreadID:       threadA.ID,
			IdempotencyKey: "postgres-expired-asr-lease",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x71)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE agent_voice_candidates
SET
    status = 'transcribing',
    asr_lease_until = transaction_timestamp() - interval '1 second',
    asr_candidate_text = NULL,
    created_at = transaction_timestamp() - interval '2 hours',
    expires_at = transaction_timestamp() - interval '1 hour',
    updated_at = transaction_timestamp()
WHERE candidate_id = $1`,
		expired.ID,
	); err != nil {
		t.Fatalf("expire transcribing candidate: %v", err)
	}
	reclaimed, err := service.ReclaimObjects(
		context.Background(),
		1,
	)
	if err != nil || reclaimed.Deleted != 1 ||
		store.Has(expired.ObjectKey) {
		t.Fatalf(
			"expired ASR cleanup = %#v, err=%v object=%t",
			reclaimed,
			err,
			store.Has(expired.ObjectKey),
		)
	}

	deletingOwner, err := service.Upload(
		context.Background(),
		actorA,
		agentvoice.UploadRequest{
			ThreadID:       threadA.ID,
			IdempotencyKey: "postgres-deleting-owner",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x72)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deletedOwner, err := service.Upload(
		context.Background(),
		actorB,
		agentvoice.UploadRequest{
			ThreadID:       threadB.ID,
			IdempotencyKey: "postgres-deleted-owner",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x73)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := service.Confirm(
		context.Background(),
		actorB,
		agentvoice.ConfirmCandidateCommand{
			CandidateID:      deletedOwner.ID,
			CandidateVersion: deletedOwner.CandidateVersion,
			ClientMessageID:  "postgres-owner-delete-message",
			ConfirmedText:    "Text survives account audio cleanup.",
		},
	)
	if err != nil {
		t.Fatalf("confirm deleted-owner candidate: %v", err)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE identity_users
SET account_status = CASE id
    WHEN $1 THEN 'deleting'
    WHEN $2 THEN 'deleted'
    ELSE account_status
END
WHERE id IN ($1, $2)`,
		actorA.UserID,
		actorB.UserID,
	); err != nil {
		t.Fatalf("mark owners deleting/deleted: %v", err)
	}
	reclaimed, err = service.ReclaimObjects(
		context.Background(),
		4,
	)
	if err != nil || reclaimed.Deleted != 2 ||
		store.Has(deletingOwner.ObjectKey) ||
		store.Has(deletedOwner.ObjectKey) {
		t.Fatalf(
			"owner cleanup = %#v, err=%v objects=(%t,%t)",
			reclaimed,
			err,
			store.Has(deletingOwner.ObjectKey),
			store.Has(deletedOwner.ObjectKey),
		)
	}
	var (
		deletingStatus agentvoice.CandidateStatus
		deletedStatus  agentvoice.CandidateStatus
		audioStatus    conversation.MessageAudioStatus
		messageContent string
	)
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    (SELECT status FROM agent_voice_candidates WHERE candidate_id = $1),
    (SELECT status FROM agent_voice_candidates WHERE candidate_id = $2),
    (SELECT status FROM agent_message_audios WHERE audio_id = $3),
    (SELECT content FROM agent_messages WHERE id = $4)`,
		deletingOwner.ID,
		deletedOwner.ID,
		confirmation.Audio.ID,
		confirmation.Message.ID,
	).Scan(
		&deletingStatus,
		&deletedStatus,
		&audioStatus,
		&messageContent,
	); err != nil {
		t.Fatalf("read durable cleanup projections: %v", err)
	}
	if deletingStatus != agentvoice.StatusDeleted ||
		deletedStatus != agentvoice.StatusDeleted ||
		audioStatus != conversation.MessageAudioDeleted ||
		messageContent != "Text survives account audio cleanup." {
		t.Fatalf(
			"durable cleanup = (%s,%s,%s,%q)",
			deletingStatus,
			deletedStatus,
			audioStatus,
			messageContent,
		)
	}
}

func waitForAdvisoryLockWaiter(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	key int64,
) {
	t.Helper()
	waitForPostgresCondition(t, ctx, func() (bool, error) {
		var waiting bool
		err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_locks
    WHERE locktype = 'advisory'
      AND NOT granted
      AND classid = 0
      AND objid = $1
)`,
			key,
		).Scan(&waiting)
		return waiting, err
	})
}

func waitForPostgresLockWaiters(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	minimum int,
) {
	t.Helper()
	waitForPostgresCondition(t, ctx, func() (bool, error) {
		var count int
		err := pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND wait_event_type = 'Lock'`).Scan(&count)
		return count >= minimum, err
	})
}

func waitForPostgresCondition(
	t *testing.T,
	ctx context.Context,
	check func() (bool, error),
) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil {
			t.Fatalf("query PostgreSQL lock state: %v", err)
		}
		if ok {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for PostgreSQL lock state: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestPostgresAgentVoiceInputUploadLeaseFencesDeletingOwnerCleanup(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	repository, err := voicepostgres.New(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new voice repository: %v", err)
	}
	actor := testActorA()
	thread, err := dataService.CreateThread(
		context.Background(),
		actor,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	baseStore, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := newBlockingVoiceStore(baseStore)
	service, err := agentvoice.NewService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     baseStore,
			directory: t.TempDir(),
		},
		newFixedSpeechRecognizer(successfulVoiceTranscription()),
		newFixedSpeechSynthesizer(agentvoice.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
			UploadLease:      time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	audio := voiceTestWAV(0x74)
	request := agentvoice.UploadRequest{
		ThreadID:       thread.ID,
		IdempotencyKey: "postgres-blocked-upload",
		ContentType:    "audio/wav",
		Audio:          bytes.NewReader(audio),
	}
	result := make(chan agentvoice.Candidate, 1)
	uploadError := make(chan error, 1)
	go func() {
		candidate, uploadErr := service.Upload(
			context.Background(),
			actor,
			request,
		)
		result <- candidate
		uploadError <- uploadErr
	}()
	<-store.started

	var (
		candidateID string
		token       int64
		leaseActive bool
	)
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    candidate_id::text,
    upload_fencing_token,
    upload_lease_until > transaction_timestamp()
FROM agent_voice_candidates
WHERE owner_user_id = $1
  AND thread_id = $2
  AND upload_request_id = $3`,
		actor.UserID,
		thread.ID,
		request.IdempotencyKey,
	).Scan(&candidateID, &token, &leaseActive); err != nil {
		t.Fatalf("read active upload claim: %v", err)
	}
	if token != 1 || !leaseActive {
		t.Fatalf("active upload token=%d lease=%t", token, leaseActive)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE identity_users
SET account_status = 'deleting'
WHERE id = $1`,
		actor.UserID,
	); err != nil {
		t.Fatalf("mark owner deleting: %v", err)
	}
	cleanup, err := service.ReclaimObjects(
		context.Background(),
		4,
	)
	if err != nil || cleanup.Deleted != 0 {
		t.Fatalf("active upload cleanup = %#v, %v", cleanup, err)
	}
	request.Audio = bytes.NewReader(audio)
	replayed, err := service.Upload(
		context.Background(),
		actor,
		request,
	)
	if err != nil ||
		replayed.ID != candidateID ||
		replayed.Status != agentvoice.StatusStaged ||
		store.puts.Load() != 1 {
		t.Fatalf(
			"concurrent replay = %#v err=%v puts=%d",
			replayed,
			err,
			store.puts.Load(),
		)
	}

	close(store.release)
	completed := <-result
	if err := <-uploadError; err != nil ||
		completed.Status != agentvoice.StatusReady {
		t.Fatalf("completed upload = %#v, %v", completed, err)
	}
	if !completed.UploadLeaseUntil.IsZero() ||
		completed.UploadFencingToken != uint64(token) {
		t.Fatalf("committed upload lease was not cleared: %#v", completed)
	}
	cleanup, err = service.ReclaimObjects(context.Background(), 4)
	if err != nil || cleanup.Deleted != 1 ||
		baseStore.Has(completed.ObjectKey) {
		t.Fatalf("deleting-owner cleanup = %#v, %v", cleanup, err)
	}
	if _, err := repository.CommitUpload(
		context.Background(),
		actor.UserID,
		completed.ID,
		uint64(token),
		completed.ETag,
	); !errors.Is(err, agentvoice.ErrConflict) {
		t.Fatalf("late fenced CommitUpload() error = %v", err)
	}
	var (
		status      agentvoice.CandidateStatus
		leaseExists bool
		current     int64
	)
	if err := database.pool.QueryRow(context.Background(), `
SELECT
    status,
    upload_lease_until IS NOT NULL,
    upload_fencing_token
FROM agent_voice_candidates
WHERE candidate_id = $1`,
		completed.ID,
	).Scan(&status, &leaseExists, &current); err != nil {
		t.Fatal(err)
	}
	if status != agentvoice.StatusDeleted ||
		leaseExists ||
		current <= token {
		t.Fatalf(
			"durable fenced cleanup = status=%s lease=%t token=%d",
			status,
			leaseExists,
			current,
		)
	}

	threadB, err := dataService.CreateThread(
		context.Background(),
		testActorB(),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	takeoverCandidate := agentvoice.Candidate{
		ID:              "30000000-0000-4000-8000-000000000031",
		OwnerID:         testActorB().UserID,
		ThreadID:        threadB.ID,
		UploadRequestID: "postgres-upload-takeover",
		ObjectKey:       "audio/v1/agent/postgres-upload-takeover.wav",
		ContentType:     "audio/wav",
		Size:            1024,
		ChecksumSHA256:  strings.Repeat("c", 64),
		Duration:        100 * time.Millisecond,
		SampleRate:      16_000,
		Status:          agentvoice.StatusStaged,
		ExpiresAt:       now.Add(time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if _, err := repository.StageCandidate(
		context.Background(),
		agentvoice.StageCandidateCommand{Candidate: takeoverCandidate},
	); err != nil {
		t.Fatalf("stage takeover candidate: %v", err)
	}
	first, acquired, err := repository.ClaimUpload(
		context.Background(),
		takeoverCandidate.OwnerID,
		takeoverCandidate.ID,
		time.Minute,
	)
	if err != nil || !acquired {
		t.Fatalf("first PostgreSQL upload claim = %#v, %t, %v",
			first, acquired, err)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE agent_voice_candidates
SET upload_lease_until = transaction_timestamp() - interval '1 second'
WHERE candidate_id = $1`,
		takeoverCandidate.ID,
	); err != nil {
		t.Fatalf("expire PostgreSQL upload claim: %v", err)
	}
	second, acquired, err := repository.ClaimUpload(
		context.Background(),
		takeoverCandidate.OwnerID,
		takeoverCandidate.ID,
		time.Minute,
	)
	if err != nil || !acquired ||
		second.FencingToken <= first.FencingToken {
		t.Fatalf("PostgreSQL upload takeover = %#v, %t, %v",
			second, acquired, err)
	}
	if _, err := repository.CommitUpload(
		context.Background(),
		takeoverCandidate.OwnerID,
		takeoverCandidate.ID,
		first.FencingToken,
		"stale-etag",
	); !errors.Is(err, agentvoice.ErrConflict) {
		t.Fatalf("stale PostgreSQL upload Commit error = %v", err)
	}
	committed, err := repository.CommitUpload(
		context.Background(),
		takeoverCandidate.OwnerID,
		takeoverCandidate.ID,
		second.FencingToken,
		"current-etag",
	)
	if err != nil ||
		committed.ETag != "current-etag" ||
		!committed.UploadLeaseUntil.IsZero() {
		t.Fatalf("current PostgreSQL upload Commit = %#v, %v",
			committed, err)
	}
}

func assertPersistedVoiceInputMessage(
	t *testing.T,
	messages []conversation.Message,
	messageID string,
	audioID string,
	status conversation.MessageAudioStatus,
) conversation.Message {
	t.Helper()
	for _, message := range messages {
		if message.ID != messageID {
			continue
		}
		if message.Role != conversation.MessageRoleUser ||
			message.Modality != conversation.MessageModalityVoice ||
			message.Audio == nil ||
			message.Audio.ID != audioID ||
			message.Audio.Status != status {
			t.Fatalf("unexpected persisted voice input Message: %#v", message)
		}
		return message
	}
	t.Fatalf("voice input Message %q not found in %#v", messageID, messages)
	return conversation.Message{}
}
