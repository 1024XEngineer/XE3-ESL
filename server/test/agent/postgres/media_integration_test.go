package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	voicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/test/support/objectstorefake"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentImageAttachmentOwnershipAndThreadCleanup(t *testing.T) {
	database := newAgentTestDatabase(t)
	conversationService, _, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	imageStore, err := objectfake.New("image/v1", time.Minute)
	if err != nil {
		t.Fatalf("new image store: %v", err)
	}
	mediaService := newTestMediaService(t, database.pool, imageStore, nil)
	ctx := context.Background()
	threadA, err := conversationService.CreateThread(ctx, testActorA())
	if err != nil {
		t.Fatalf("create owner Thread: %v", err)
	}
	threadB, err := conversationService.CreateThread(ctx, testActorB())
	if err != nil {
		t.Fatalf("create other Thread: %v", err)
	}
	payload := []byte("validated-image-fixture")
	checksum := sha256.Sum256(payload)
	asset, err := mediaService.Upload(ctx, sharedmedia.Upload{
		UserID:         testActorA().UserID,
		Kind:           sharedmedia.KindImage,
		IdempotencyKey: "image-owner-guard-1",
		ContentType:    "image/png",
		Body:           bytes.NewReader(payload),
		Size:           int64(len(payload)),
		ChecksumSHA256: hex.EncodeToString(checksum[:]),
		Width:          2,
		Height:         2,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	})
	if err != nil || asset.Status != sharedmedia.StatusReady {
		t.Fatalf("upload Image asset = %#v, %v", asset, err)
	}
	ids := identity.NewUUIDv4Generator(nil)
	submissions, err := runpostgres.NewImageSubmissionRepository(database.pool, ids)
	if err != nil {
		t.Fatalf("new Image submission repository: %v", err)
	}
	if _, err := submissions.CreateInitialWithImages(
		ctx,
		testActorB().UserID,
		threadB.ID,
		"cross-owner-image-1",
		"This image belongs to another account.",
		[]string{asset.ID},
		testRunConfiguration,
	); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("cross-owner attach error = %v, want not found", err)
	}
	submission, err := submissions.CreateInitialWithImages(
		ctx,
		testActorA().UserID,
		threadA.ID,
		"owned-image-1",
		"Use this image as private context.",
		[]string{asset.ID},
		testRunConfiguration,
	)
	if err != nil || !submission.Created {
		t.Fatalf("owned Image submission = %#v, %v", submission, err)
	}
	var expiresAt *time.Time
	if err := database.pool.QueryRow(ctx, `
SELECT expires_at FROM media_assets WHERE id = $1`, asset.ID).Scan(&expiresAt); err != nil {
		t.Fatalf("read retained asset: %v", err)
	}
	if expiresAt != nil {
		t.Fatalf("attached asset expires_at = %v, want NULL", expiresAt)
	}
	if err := conversationService.DeleteThread(ctx, testActorA(), threadA.ID); err != nil {
		t.Fatalf("delete Thread: %v", err)
	}
	var status string
	if err := database.pool.QueryRow(ctx, `
SELECT status FROM media_assets WHERE id = $1`, asset.ID).Scan(&status); err != nil {
		t.Fatalf("read scheduled asset: %v", err)
	}
	if status != string(sharedmedia.StatusDeleting) {
		t.Fatalf("asset status after Thread deletion = %q", status)
	}
	result, err := mediaService.Reclaim(ctx, 10)
	if err != nil || result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("reclaim Thread media = %#v, %v", result, err)
	}
	if imageStore.Has(asset.ObjectKey) {
		t.Fatal("Thread cleanup retained the private Image object")
	}
	for _, table := range []string{"media_assets", "agent_message_attachments"} {
		var count int
		if err := database.pool.QueryRow(
			ctx,
			"SELECT count(*) FROM "+table,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after cleanup = %d", table, count)
		}
	}
}

func TestAgentVoiceRetryConfirmReplayPlaybackAndDelete(t *testing.T) {
	database := newAgentTestDatabase(t)
	conversationService, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	audioStore, err := objectfake.New("audio/v1", time.Minute)
	if err != nil {
		t.Fatalf("new audio store: %v", err)
	}
	mediaService := newTestMediaService(t, database.pool, nil, audioStore)
	recognizer := newFixedSpeechRecognizer(successfulVoiceTranscription())
	recognizer.err = errors.New("temporary ASR outage")
	feedback := &agentVoiceFeedbackStub{}
	voiceService := newTestAgentVoiceService(
		t,
		database.pool,
		mediaService,
		audioStore,
		runService,
		recognizer,
		feedback,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := conversationService.CreateThread(ctx, actor)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	draft, err := voiceService.Upload(
		ctx,
		actor,
		agentvoice.UploadRequest{
			ThreadID:       thread.ID,
			IdempotencyKey: "voice-draft-retry-1",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(1)),
		},
	)
	if err != nil || draft.Status != agentvoice.StatusFailed ||
		!draft.FailureRetryable || draft.Version != 1 {
		t.Fatalf("failed ASR Draft = %#v, %v", draft, err)
	}
	recognizer.err = nil
	draft, err = voiceService.Retry(ctx, actor, draft.ID)
	if err != nil || draft.Status != agentvoice.StatusReady ||
		draft.Version != 2 || draft.Transcript == "" {
		t.Fatalf("retried ASR Draft = %#v, %v", draft, err)
	}
	command := agentvoice.ConfirmDraftCommand{
		DraftID:         draft.ID,
		Version:         draft.Version,
		ClientMessageID: "voice-confirmation-1",
		ConfirmedText:   "I corrected the provider transcript before sending.",
	}
	first, err := voiceService.Confirm(ctx, actor, command)
	if err != nil || !first.Created ||
		first.Draft.Status != agentvoice.StatusConfirmed ||
		first.Run.Status != agentrun.StatusCompleted ||
		first.Message.Content != command.ConfirmedText ||
		first.Message.SpeechFeedbackStatusURL == "" {
		t.Fatalf("first confirmation = %#v, %v", first, err)
	}
	replayed, err := voiceService.Confirm(ctx, actor, command)
	if err != nil || replayed.Created ||
		replayed.Message.ID != first.Message.ID ||
		replayed.Run.ID != first.Run.ID ||
		replayed.Attachment.ID != first.Attachment.ID {
		t.Fatalf("replayed confirmation = %#v, %v", replayed, err)
	}
	if feedback.calls != 2 {
		t.Fatalf("feedback EnsureMessage calls = %d, want 2", feedback.calls)
	}

	reopened := database.reopen(t)
	resumedMedia := newTestMediaService(t, reopened, nil, audioStore)
	resumedVoice := newTestAgentVoiceService(
		t,
		reopened,
		resumedMedia,
		audioStore,
		runService,
		recognizer,
		feedback,
	)
	playback, err := resumedVoice.Playback(ctx, actor, first.Attachment.ID)
	if err != nil || playback.URL == "" || !playback.ExpiresAt.After(time.Now()) {
		t.Fatalf("resumed confirmed playback = %#v, %v", playback, err)
	}
	if err := resumedVoice.DeleteAudio(ctx, actor, first.Attachment.ID); err != nil {
		t.Fatalf("delete confirmed audio: %v", err)
	}
	if audioStore.Has(first.Draft.ObjectKey) {
		t.Fatal("confirmed audio deletion retained the private object")
	}
	if _, err := resumedVoice.Playback(ctx, actor, first.Attachment.ID); !errors.Is(err, agentvoice.ErrNotFound) {
		t.Fatalf("playback after deletion error = %v, want not found", err)
	}
	for _, table := range []string{
		"media_assets", "agent_message_attachments", "agent_voice_drafts",
	} {
		var count int
		if err := reopened.QueryRow(
			ctx,
			"SELECT count(*) FROM "+table,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after audio deletion = %d", table, count)
		}
	}
	messages, err := conversationService.ListMessages(ctx, actor, thread.ID)
	if err != nil || len(messages) < 2 {
		t.Fatalf("Messages after audio deletion = %#v, %v", messages, err)
	}
	if messages[0].Audio != nil ||
		messages[0].Modality != conversation.MessageModalityVoice {
		t.Fatalf("voice Message projection after audio deletion = %#v", messages[0])
	}
}

func TestMediaCleanupFailureReleasesLeaseAndRetries(t *testing.T) {
	database := newAgentTestDatabase(t)
	baseStore, err := objectfake.New("image/v1", time.Minute)
	if err != nil {
		t.Fatalf("new image store: %v", err)
	}
	store := &failFirstDeleteStore{Store: baseStore}
	mediaService := newTestMediaService(t, database.pool, store, nil)
	payload := []byte("cleanup-retry-image")
	checksum := sha256.Sum256(payload)
	asset, err := mediaService.Upload(context.Background(), sharedmedia.Upload{
		UserID:         testActorA().UserID,
		Kind:           sharedmedia.KindImage,
		IdempotencyKey: "cleanup-retry-image-1",
		ContentType:    "image/png",
		Body:           bytes.NewReader(payload),
		Size:           int64(len(payload)),
		ChecksumSHA256: hex.EncodeToString(checksum[:]),
		Width:          2,
		Height:         2,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("upload cleanup asset: %v", err)
	}
	if err := mediaService.Delete(
		context.Background(), testActorA().UserID, asset.ID,
	); !errors.Is(err, objectstore.ErrOperationFailed) {
		t.Fatalf("first cleanup error = %v", err)
	}
	var status string
	var cleanupError *string
	var cleanupLease *time.Time
	var attempts int
	if err := database.pool.QueryRow(context.Background(), `
SELECT status, cleanup_error, cleanup_lease_until, cleanup_attempt_count
FROM media_assets
WHERE id = $1`, asset.ID).Scan(
		&status, &cleanupError, &cleanupLease, &attempts,
	); err != nil {
		t.Fatalf("read failed cleanup state: %v", err)
	}
	if status != string(sharedmedia.StatusDeleting) || cleanupError == nil ||
		*cleanupError != "object_delete_failed" || cleanupLease != nil || attempts != 1 {
		t.Fatalf(
			"failed cleanup state = status:%q error:%v lease:%v attempts:%d",
			status, cleanupError, cleanupLease, attempts,
		)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE media_assets
SET cleanup_available_at = CURRENT_TIMESTAMP - interval '1 second'
WHERE id = $1`, asset.ID); err != nil {
		t.Fatalf("make cleanup retry eligible: %v", err)
	}
	result, err := mediaService.Reclaim(context.Background(), 10)
	if err != nil || result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("cleanup retry = %#v, %v", result, err)
	}
	if baseStore.Has(asset.ObjectKey) {
		t.Fatal("cleanup retry retained the private object")
	}
}

func newTestMediaService(
	t *testing.T,
	pool *pgxpool.Pool,
	imageStore objectstore.Store,
	audioStore objectstore.Store,
) *sharedmedia.Service {
	t.Helper()
	repository, err := mediapostgres.New(pool)
	if err != nil {
		t.Fatalf("new Media repository: %v", err)
	}
	service, err := sharedmedia.NewService(
		repository,
		sharedmedia.Stores{Images: imageStore, Audio: audioStore},
		identity.NewUUIDv4Generator(nil),
		sharedmedia.Config{
			UploadLease:  5 * time.Second,
			CleanupLease: 5 * time.Second,
			PlaybackTTL:  2 * time.Minute,
			CleanupBatch: 10,
		},
	)
	if err != nil {
		t.Fatalf("new Media service: %v", err)
	}
	return service
}

func newTestAgentVoiceService(
	t *testing.T,
	pool *pgxpool.Pool,
	mediaService *sharedmedia.Service,
	audioStore *objectfake.Store,
	runs agentvoice.PendingRunProcessor,
	recognizer agentvoice.StreamingSpeechRecognizer,
	feedback agentvoice.FeedbackPort,
) *agentvoice.Service {
	t.Helper()
	repository, err := voicepostgres.New(pool, identity.NewUUIDv4Generator(nil))
	if err != nil {
		t.Fatalf("new Agent Voice repository: %v", err)
	}
	service, err := agentvoice.NewService(
		repository,
		mediaService,
		&storedVoiceSourceLoader{store: audioStore, directory: t.TempDir()},
		recognizer,
		&fixedSpeechSynthesizer{err: errors.New("synthesis is not used")},
		runs,
		agentvoice.Config{
			Configuration:    testRunConfiguration,
			ScratchDirectory: t.TempDir(),
			DraftTTL:         time.Hour,
			ASRLease:         5 * time.Second,
		},
		feedback,
	)
	if err != nil {
		t.Fatalf("new Agent Voice service: %v", err)
	}
	return service
}

type agentVoiceFeedbackStub struct {
	calls int
}

func (feedback *agentVoiceFeedbackStub) EnsureMessage(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	messageID string,
) (agentvoice.FeedbackReference, error) {
	feedback.calls++
	return agentvoice.FeedbackReference{
		StatusURL: "/v1/agent-messages/" + messageID + "/evaluation",
	}, nil
}

type failFirstDeleteStore struct {
	*objectfake.Store
	failed bool
}

func (store *failFirstDeleteStore) Delete(ctx context.Context, key string) error {
	if !store.failed {
		store.failed = true
		return objectstore.ErrOperationFailed
	}
	return store.Store.Delete(ctx, key)
}

var _ objectstore.Store = (*failFirstDeleteStore)(nil)
