package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	aifake "github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
)

func TestPostgresAgentVoiceMessageConfirmationHistoryAndDeletion(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	matterService, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		aifake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new fake ObjectStore: %v", err)
	}
	service, err := NewVoiceMessageService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		aifake.NewSpeechRecognizer(successfulVoiceTranscription()),
		aifake.NewSpeechSynthesizer(ai.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		VoiceMessageConfig{
			RunConfiguration: testRunConfiguration,
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatalf("new voice Message service: %v", err)
	}

	candidate, err := service.Upload(
		context.Background(),
		actor,
		UploadVoiceCandidateRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "postgres-voice-upload-0001",
			ContentType:    "audio/wav",
			Audio:          bytes.NewReader(voiceTestWAV(0x51)),
		},
	)
	if err != nil {
		t.Fatalf("upload voice candidate: %v", err)
	}
	if candidate.Status != VoiceCandidateReady ||
		candidate.CandidateVersion != 1 ||
		!store.Has(candidate.ObjectKey) {
		t.Fatalf("unexpected ready candidate: %#v", candidate)
	}
	if _, err := service.GetCandidate(
		context.Background(),
		testActorB(),
		candidate.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner candidate read error = %v, want not found", err)
	}

	command := ConfirmVoiceCandidateCommand{
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
	if confirmation.Run.Status != RunStatusCompleted ||
		confirmation.Message.Modality != MessageModalityVoice ||
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
	); !errors.Is(err, ErrIdempotencyConflict) {
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
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf(
			"text Run replay of voice client ID error = %v, want idempotency conflict",
			err,
		)
	}
	otherCandidate, err := service.Upload(
		context.Background(),
		actor,
		UploadVoiceCandidateRequest{
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
		newest.Messages[0].Role != MessageRoleAssistant ||
		newest.Messages[0].Modality != MessageModalityText ||
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
		t.Fatalf("page voice Message: %v", err)
	}
	assertPersistedVoiceMessage(
		t,
		older.Messages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		MessageAudioReadable,
	)
	if older.NextCursor != "" {
		t.Fatalf("unexpected terminal voice page cursor %q", older.NextCursor)
	}

	restartedRepository, err := NewPostgresRepository(
		database.pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new restarted Agent repository: %v", err)
	}
	restartedData, err := NewService(restartedRepository, matterService)
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
	assertPersistedVoiceMessage(
		t,
		restartedMessages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		MessageAudioReadable,
	)

	if _, err := service.Playback(
		context.Background(),
		testActorB(),
		confirmation.Audio.ID,
	); !errors.Is(err, ErrNotFound) {
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
	deleted := assertPersistedVoiceMessage(
		t,
		deletedMessages,
		confirmation.Message.ID,
		confirmation.Audio.ID,
		MessageAudioDeleted,
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

func TestPostgresAgentVoiceCleanupRecoversExpiredLeaseAndDeletingOwners(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		aifake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewVoiceMessageService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     store,
			directory: t.TempDir(),
		},
		aifake.NewSpeechRecognizer(successfulVoiceTranscription()),
		aifake.NewSpeechSynthesizer(ai.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		VoiceMessageConfig{
			RunConfiguration: testRunConfiguration,
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
		UploadVoiceCandidateRequest{
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
	reclaimed, err := service.ReclaimVoiceObjects(
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
		UploadVoiceCandidateRequest{
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
		UploadVoiceCandidateRequest{
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
		ConfirmVoiceCandidateCommand{
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
	reclaimed, err = service.ReclaimVoiceObjects(
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
		deletingStatus VoiceCandidateStatus
		deletedStatus  VoiceCandidateStatus
		audioStatus    MessageAudioStatus
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
	if deletingStatus != VoiceCandidateDeleted ||
		deletedStatus != VoiceCandidateDeleted ||
		audioStatus != MessageAudioDeleted ||
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

func TestPostgresAgentVoiceUploadLeaseFencesDeletingOwnerCleanup(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		aifake.NewTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
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
	service, err := NewVoiceMessageService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     baseStore,
			directory: t.TempDir(),
		},
		aifake.NewSpeechRecognizer(successfulVoiceTranscription()),
		aifake.NewSpeechSynthesizer(ai.SynthesisResult{}, nil),
		runService,
		identity.NewUUIDv4Generator(nil),
		VoiceMessageConfig{
			RunConfiguration: testRunConfiguration,
			ScratchDirectory: t.TempDir(),
			UploadLease:      time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	audio := voiceTestWAV(0x74)
	request := UploadVoiceCandidateRequest{
		ThreadID:       thread.ID,
		IdempotencyKey: "postgres-blocked-upload",
		ContentType:    "audio/wav",
		Audio:          bytes.NewReader(audio),
	}
	result := make(chan VoiceCandidate, 1)
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
	cleanup, err := service.ReclaimVoiceObjects(
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
		replayed.Status != VoiceCandidateStaged ||
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
		completed.Status != VoiceCandidateReady {
		t.Fatalf("completed upload = %#v, %v", completed, err)
	}
	if !completed.UploadLeaseUntil.IsZero() ||
		completed.UploadFencingToken != uint64(token) {
		t.Fatalf("committed upload lease was not cleared: %#v", completed)
	}
	cleanup, err = service.ReclaimVoiceObjects(context.Background(), 4)
	if err != nil || cleanup.Deleted != 1 ||
		baseStore.Has(completed.ObjectKey) {
		t.Fatalf("deleting-owner cleanup = %#v, %v", cleanup, err)
	}
	if _, err := repository.CommitVoiceUpload(
		context.Background(),
		actor.UserID,
		completed.ID,
		uint64(token),
		completed.ETag,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("late fenced CommitVoiceUpload() error = %v", err)
	}
	var (
		status      VoiceCandidateStatus
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
	if status != VoiceCandidateDeleted ||
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
	takeoverCandidate := VoiceCandidate{
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
		Status:          VoiceCandidateStaged,
		ExpiresAt:       now.Add(time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if _, err := repository.StageVoiceCandidate(
		context.Background(),
		StageVoiceCandidateCommand{Candidate: takeoverCandidate},
	); err != nil {
		t.Fatalf("stage takeover candidate: %v", err)
	}
	first, acquired, err := repository.ClaimVoiceUpload(
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
	second, acquired, err := repository.ClaimVoiceUpload(
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
	if _, err := repository.CommitVoiceUpload(
		context.Background(),
		takeoverCandidate.OwnerID,
		takeoverCandidate.ID,
		first.FencingToken,
		"stale-etag",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale PostgreSQL upload Commit error = %v", err)
	}
	committed, err := repository.CommitVoiceUpload(
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

func assertPersistedVoiceMessage(
	t *testing.T,
	messages []Message,
	messageID string,
	audioID string,
	status MessageAudioStatus,
) Message {
	t.Helper()
	for _, message := range messages {
		if message.ID != messageID {
			continue
		}
		if message.Role != MessageRoleUser ||
			message.Modality != MessageModalityVoice ||
			message.Audio == nil ||
			message.Audio.ID != audioID ||
			message.Audio.Status != status {
			t.Fatalf("unexpected persisted voice Message: %#v", message)
		}
		return message
	}
	t.Fatalf("voice Message %q not found in %#v", messageID, messages)
	return Message{}
}
