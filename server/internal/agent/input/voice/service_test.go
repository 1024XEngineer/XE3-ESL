package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	voiceTestOwner  = "10000000-0000-4000-8000-000000000001"
	voiceTestThread = "20000000-0000-4000-8000-000000000001"
)

func TestVoiceInputUploadUsesFakeObjectStoreAndASRWithoutEarlyMessage(
	t *testing.T,
) {
	fixture := newVoiceInputFixture(
		t,
		&voiceStaticRecognizer{result: successfulVoiceTranscription()},
		&voiceTestRunProcessor{},
	)
	audio := voiceTestWAV(0x10)
	request := UploadRequest{
		ThreadID:       voiceTestThread,
		IdempotencyKey: "voice-upload-0001",
		ContentType:    platformmedia.ContentTypeWAV,
		Audio:          bytes.NewReader(audio),
	}
	candidate, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if candidate.Status != StatusReady ||
		candidate.CandidateVersion != 1 ||
		candidate.ASRCandidateText != "A faithful provider transcript." ||
		candidate.ConfirmedMessageID != "" ||
		candidate.ConfirmedRunID != "" ||
		candidate.MessageAudioID != "" {
		t.Fatalf("Upload() candidate = %#v", candidate)
	}
	if !fixture.store.Has(candidate.ObjectKey) ||
		candidate.ObjectKey != ObjectPrefix+candidate.ID+".wav" {
		t.Fatalf("private object was not retained at Agent prefix: %#v", candidate)
	}
	if fixture.repository.confirmCreates != 0 ||
		len(fixture.repository.messages) != 0 {
		t.Fatal("upload created agentconversation.Message or agentrun.Run before confirmation")
	}

	replayed, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-upload-0001",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(audio),
		},
	)
	if err != nil {
		t.Fatalf("replayed Upload() error = %v", err)
	}
	if replayed.ID != candidate.ID ||
		replayed.CandidateVersion != candidate.CandidateVersion ||
		fixture.repository.claims != 1 {
		t.Fatalf(
			"replayed Upload() = %#v, claims = %d",
			replayed,
			fixture.repository.claims,
		)
	}

	_, err = fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-upload-0001",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x20)),
		},
	)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed upload error = %v, want idempotency conflict", err)
	}
}

func TestVoiceInputUploadStreamUsesStreamingRecognizerWithoutFallback(
	t *testing.T,
) {
	recognizer := &voiceStreamingContractRecognizer{
		result: successfulVoiceTranscription(),
	}
	fixture := newVoiceInputFixture(
		t,
		recognizer,
		&voiceTestRunProcessor{},
	)
	observer := &voiceRecordingTranscriptionObserver{}
	candidate, err := fixture.service.UploadStream(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-upload-stream-0001",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x15)),
		},
		observer,
	)
	if err != nil {
		t.Fatalf("UploadStream() error = %v", err)
	}
	if candidate.Status != StatusReady ||
		candidate.ASRCandidateText != recognizer.result.Transcript {
		t.Fatalf("UploadStream() candidate = %#v", candidate)
	}
	if recognizer.streamCalls != 1 || recognizer.transcribeCalls != 0 {
		t.Fatalf(
			"recognizer calls: stream=%d transcribe=%d",
			recognizer.streamCalls,
			recognizer.transcribeCalls,
		)
	}
	if len(observer.updates) != 2 ||
		observer.updates[0].Final ||
		observer.updates[0].Transcript != "A provider partial transcript." ||
		!observer.updates[1].Final ||
		observer.updates[1].Transcript != recognizer.result.Transcript {
		t.Fatalf("stream updates = %#v", observer.updates)
	}
}

func TestVoiceInputUploadUsesOrdinaryTranscriptionWithoutObserver(
	t *testing.T,
) {
	recognizer := &voiceStreamingContractRecognizer{
		result: successfulVoiceTranscription(),
	}
	fixture := newVoiceInputFixture(
		t,
		recognizer,
		&voiceTestRunProcessor{},
	)
	candidate, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-upload-ordinary-0001",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x16)),
		},
	)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if candidate.Status != StatusReady ||
		candidate.ASRCandidateText != recognizer.result.Transcript {
		t.Fatalf("Upload() candidate = %#v", candidate)
	}
	if recognizer.transcribeCalls != 1 || recognizer.streamCalls != 0 {
		t.Fatalf(
			"recognizer calls: transcribe=%d stream=%d",
			recognizer.transcribeCalls,
			recognizer.streamCalls,
		)
	}
}

func TestVoiceInputRetryAdvancesVersionAndFencesLateASR(t *testing.T) {
	recognizer := &voiceSequenceRecognizer{
		results: []voiceRecognizerStep{
			{err: NewSpeechError(
				SpeechOperationTranscription,
				ErrorProviderUnavailable,
				503,
				"",
				"fake-request-1",
				errors.New("controlled outage"),
			)},
			{result: successfulVoiceTranscription()},
		},
	}
	fixture := newVoiceInputFixture(
		t,
		recognizer,
		&voiceTestRunProcessor{},
	)
	failed, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-upload-retry",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x30)),
		},
	)
	if err != nil {
		t.Fatalf("Upload() failure persistence error = %v", err)
	}
	if failed.Status != StatusFailed ||
		failed.CandidateVersion != 1 ||
		!failed.FailureRetryable {
		t.Fatalf("failed candidate = %#v", failed)
	}
	ready, err := fixture.service.Retry(
		context.Background(),
		voiceTestActor(),
		failed.ID,
	)
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if ready.Status != StatusReady ||
		ready.CandidateVersion != 2 ||
		ready.ASRAttempt != 2 ||
		fixture.sources.calls != 1 {
		t.Fatalf(
			"retried candidate = %#v, loader calls = %d",
			ready,
			fixture.sources.calls,
		)
	}
	_, err = fixture.repository.CompleteTranscription(
		context.Background(),
		voiceTestOwner,
		ready.ID,
		1,
		successfulVoiceTranscription(),
	)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("late ASR completion error = %v, want fenced conflict", err)
	}
	current, _ := fixture.repository.FindCandidate(
		context.Background(),
		voiceTestOwner,
		ready.ID,
	)
	if current.CandidateVersion != 2 ||
		current.ASRCandidateText != ready.ASRCandidateText {
		t.Fatalf("late worker changed current candidate: %#v", current)
	}
}

func TestVoiceInputUploadReplayRetakesOnlyExpiredTranscriptionLease(
	t *testing.T,
) {
	recognizer := &voiceSequenceRecognizer{
		results: []voiceRecognizerStep{
			{result: successfulVoiceTranscription()},
			{result: successfulVoiceTranscription()},
		},
	}
	fixture := newVoiceInputFixture(
		t,
		recognizer,
		&voiceTestRunProcessor{},
	)
	audio := voiceTestWAV(0x35)
	request := UploadRequest{
		ThreadID:       voiceTestThread,
		IdempotencyKey: "voice-upload-lease-recovery",
		ContentType:    platformmedia.ContentTypeWAV,
		Audio:          bytes.NewReader(audio),
	}
	ready, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil {
		t.Fatalf("initial Upload() error = %v", err)
	}
	fixture.repository.mu.Lock()
	inFlight := fixture.repository.candidates[ready.ID]
	inFlight.Status = StatusTranscribing
	inFlight.ASRLeaseUntil = time.Now().Add(time.Minute)
	fixture.repository.candidates[ready.ID] = inFlight
	fixture.repository.mu.Unlock()

	request.Audio = bytes.NewReader(audio)
	live, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil || live.Status != StatusTranscribing ||
		fixture.repository.claims != 1 {
		t.Fatalf(
			"live-lease replay = %#v, err=%v claims=%d",
			live,
			err,
			fixture.repository.claims,
		)
	}

	fixture.repository.mu.Lock()
	expired := fixture.repository.candidates[ready.ID]
	expired.ASRLeaseUntil = time.Now().Add(-time.Second)
	fixture.repository.candidates[ready.ID] = expired
	fixture.repository.mu.Unlock()
	request.Audio = bytes.NewReader(audio)
	recovered, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil ||
		recovered.Status != StatusReady ||
		recovered.ASRAttempt != 2 ||
		recovered.CandidateVersion != 2 ||
		fixture.repository.claims != 2 {
		t.Fatalf(
			"expired-lease replay = %#v, err=%v claims=%d",
			recovered,
			err,
			fixture.repository.claims,
		)
	}
}

func TestVoiceUploadLeaseBlocksConcurrentPutAndDeletingOwnerCleanup(
	t *testing.T,
) {
	repository := newMemoryVoiceRepository()
	baseStore, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := newBlockingVoiceStore(baseStore)
	service := newVoiceLeaseTestService(t, repository, store, baseStore)
	audio := voiceTestWAV(0x36)
	request := UploadRequest{
		ThreadID:       voiceTestThread,
		IdempotencyKey: "voice-upload-blocked-put",
		ContentType:    platformmedia.ContentTypeWAV,
		Audio:          bytes.NewReader(audio),
	}
	firstResult := make(chan Candidate, 1)
	firstError := make(chan error, 1)
	go func() {
		candidate, uploadErr := service.Upload(
			context.Background(),
			voiceTestActor(),
			request,
		)
		firstResult <- candidate
		firstError <- uploadErr
	}()
	<-store.started

	request.Audio = bytes.NewReader(audio)
	concurrent, err := service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil ||
		concurrent.Status != StatusStaged ||
		store.puts.Load() != 1 ||
		repository.uploadClaims != 1 {
		t.Fatalf(
			"concurrent replay = %#v err=%v puts=%d claims=%d",
			concurrent,
			err,
			store.puts.Load(),
			repository.uploadClaims,
		)
	}
	repository.mu.Lock()
	repository.ownerDeleting[voiceTestOwner] = true
	active := repository.candidates[concurrent.ID]
	repository.mu.Unlock()
	if active.UploadFencingToken != 1 ||
		!active.UploadLeaseUntil.After(time.Now()) {
		t.Fatalf("active upload claim = %#v", active)
	}
	cleanup, err := service.ReclaimObjects(
		context.Background(),
		4,
	)
	if err != nil || cleanup.Deleted != 0 {
		t.Fatalf("active upload cleanup = %#v, %v", cleanup, err)
	}
	if err := service.DeleteCandidate(
		context.Background(),
		voiceTestActor(),
		concurrent.ID,
	); !errors.Is(err, ErrCandidateProcessing) {
		t.Fatalf("active upload DeleteCandidate() error = %v", err)
	}

	close(store.release)
	completed := <-firstResult
	if err := <-firstError; err != nil ||
		completed.Status != StatusReady {
		t.Fatalf("first upload = %#v, %v", completed, err)
	}
	cleanup, err = service.ReclaimObjects(context.Background(), 4)
	if err != nil || cleanup.Deleted != 1 ||
		baseStore.Has(completed.ObjectKey) {
		t.Fatalf("post-upload owner cleanup = %#v, %v", cleanup, err)
	}
}

func TestVoiceUploadPutDeadlineStopsBeforeCleanupCanClaim(t *testing.T) {
	repository := newMemoryVoiceRepository()
	baseStore, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := newBlockingVoiceStore(baseStore)
	service := newVoiceLeaseTestService(
		t,
		repository,
		store,
		baseStore,
		time.Second,
	)
	result := make(chan error, 1)
	go func() {
		_, uploadErr := service.Upload(
			context.Background(),
			voiceTestActor(),
			UploadRequest{
				ThreadID:       voiceTestThread,
				IdempotencyKey: "voice-put-deadline",
				ContentType:    platformmedia.ContentTypeWAV,
				Audio: bytes.NewReader(
					voiceTestWAV(0x37),
				),
			},
		)
		result <- uploadErr
	}()
	<-store.started
	repository.mu.Lock()
	repository.ownerDeleting[voiceTestOwner] = true
	var candidate Candidate
	for _, item := range repository.candidates {
		candidate = item
	}
	repository.mu.Unlock()

	if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline-bounded Upload() error = %v", err)
	}
	if baseStore.Has(candidate.ObjectKey) {
		t.Fatal("deadline-canceled Put materialized an object")
	}
	cleanup, err := service.ReclaimObjects(
		context.Background(),
		4,
	)
	if err != nil || cleanup.Deleted != 0 {
		t.Fatalf("pre-expiry cleanup = %#v, %v", cleanup, err)
	}
	repository.mu.Lock()
	candidate = repository.candidates[candidate.ID]
	wait := time.Until(candidate.UploadLeaseUntil)
	repository.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait + 10*time.Millisecond)
	}
	cleanup, err = service.ReclaimObjects(context.Background(), 4)
	if err != nil || cleanup.Deleted != 1 {
		t.Fatalf("post-expiry cleanup = %#v, %v", cleanup, err)
	}
}

func TestVoiceUploadExpiredLeaseFencesOldCommitAndAllowsTakeover(
	t *testing.T,
) {
	repository := newMemoryVoiceRepository()
	now := time.Now()
	candidate := Candidate{
		ID:              "30000000-0000-4000-8000-000000000021",
		OwnerID:         voiceTestOwner,
		ThreadID:        voiceTestThread,
		UploadRequestID: "voice-upload-takeover",
		ObjectKey:       "audio/v1/agent/upload-takeover.wav",
		ContentType:     platformmedia.ContentTypeWAV,
		Size:            int64(len(voiceTestWAV(0x38))),
		ChecksumSHA256:  strings.Repeat("b", 64),
		Duration:        100 * time.Millisecond,
		SampleRate:      16_000,
		Status:          StatusStaged,
		ExpiresAt:       now.Add(time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if _, err := repository.StageCandidate(
		context.Background(),
		StageCandidateCommand{Candidate: candidate},
	); err != nil {
		t.Fatal(err)
	}
	first, acquired, err := repository.ClaimUpload(
		context.Background(),
		voiceTestOwner,
		candidate.ID,
		time.Minute,
	)
	if err != nil || !acquired {
		t.Fatalf("first ClaimUpload() = %#v, %t, %v",
			first, acquired, err)
	}
	repository.mu.Lock()
	expired := repository.candidates[candidate.ID]
	expired.UploadLeaseUntil = time.Now().Add(-time.Second)
	repository.candidates[candidate.ID] = expired
	repository.mu.Unlock()
	second, acquired, err := repository.ClaimUpload(
		context.Background(),
		voiceTestOwner,
		candidate.ID,
		time.Minute,
	)
	if err != nil || !acquired ||
		second.FencingToken <= first.FencingToken {
		t.Fatalf("takeover ClaimUpload() = %#v, %t, %v",
			second, acquired, err)
	}
	if _, err := repository.CommitUpload(
		context.Background(),
		voiceTestOwner,
		candidate.ID,
		first.FencingToken,
		"stale-etag",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("old fenced CommitUpload() error = %v", err)
	}
	committed, err := repository.CommitUpload(
		context.Background(),
		voiceTestOwner,
		candidate.ID,
		second.FencingToken,
		"current-etag",
	)
	if err != nil ||
		committed.ETag != "current-etag" ||
		!committed.UploadLeaseUntil.IsZero() {
		t.Fatalf("takeover CommitUpload() = %#v, %v", committed, err)
	}
}

func TestVoiceUploadLeaseRecoversPrePutAndCommitFailureWithoutOrphan(
	t *testing.T,
) {
	t.Run("crash before Put", func(t *testing.T) {
		repository := newMemoryVoiceRepository()
		now := time.Now()
		candidate := Candidate{
			ID:              "30000000-0000-4000-8000-000000000011",
			OwnerID:         voiceTestOwner,
			ThreadID:        voiceTestThread,
			UploadRequestID: "voice-pre-put-crash",
			ObjectKey:       "audio/v1/agent/pre-put-crash.wav",
			ContentType:     platformmedia.ContentTypeWAV,
			Size:            int64(len(voiceTestWAV(0x41))),
			ChecksumSHA256:  strings.Repeat("a", 64),
			Duration:        100 * time.Millisecond,
			SampleRate:      16_000,
			Status:          StatusStaged,
			ExpiresAt:       now.Add(time.Hour),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if _, err := repository.StageCandidate(
			context.Background(),
			StageCandidateCommand{Candidate: candidate},
		); err != nil {
			t.Fatal(err)
		}
		claim, acquired, err := repository.ClaimUpload(
			context.Background(),
			voiceTestOwner,
			candidate.ID,
			time.Minute,
		)
		if err != nil || !acquired {
			t.Fatalf("ClaimUpload() = %#v, %t, %v",
				claim, acquired, err)
		}
		repository.mu.Lock()
		repository.ownerDeleting[voiceTestOwner] = true
		repository.mu.Unlock()
		claims, err := repository.ClaimCleanup(
			context.Background(),
			time.Minute,
			4,
		)
		if err != nil || len(claims) != 0 {
			t.Fatalf("active pre-Put cleanup = %#v, %v", claims, err)
		}
		repository.mu.Lock()
		expired := repository.candidates[candidate.ID]
		expired.UploadLeaseUntil = time.Now().Add(-time.Second)
		repository.candidates[candidate.ID] = expired
		repository.mu.Unlock()
		claims, err = repository.ClaimCleanup(
			context.Background(),
			time.Minute,
			4,
		)
		if err != nil || len(claims) != 1 {
			t.Fatalf("expired pre-Put cleanup = %#v, %v", claims, err)
		}
		store, err := objectfake.New("audio/v1", 2*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(
			context.Background(),
			claims[0].ObjectKey,
		); err != nil {
			t.Fatalf("delete absent pre-Put object: %v", err)
		}
		if err := repository.FinishCleanup(
			context.Background(),
			claims[0],
		); err != nil {
			t.Fatalf("finish pre-Put cleanup: %v", err)
		}
		current, err := repository.FindCandidate(
			context.Background(),
			voiceTestOwner,
			candidate.ID,
		)
		if err != nil || current.Status != StatusDeleted {
			t.Fatalf("pre-Put durable cleanup = %#v, %v", current, err)
		}
	})

	t.Run("failed Commit", func(t *testing.T) {
		repository := newMemoryVoiceRepository()
		repository.failCommitBefore = true
		store, err := objectfake.New("audio/v1", 2*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		counting := &countingVoiceStore{Store: store}
		service := newVoiceLeaseTestService(
			t,
			repository,
			counting,
			store,
		)
		_, err = service.Upload(
			context.Background(),
			voiceTestActor(),
			UploadRequest{
				ThreadID:       voiceTestThread,
				IdempotencyKey: "voice-commit-failure",
				ContentType:    platformmedia.ContentTypeWAV,
				Audio: bytes.NewReader(
					voiceTestWAV(0x42),
				),
			},
		)
		if !errors.Is(err, ErrRepository) {
			t.Fatalf("failed Commit Upload() error = %v", err)
		}
		repository.mu.Lock()
		repository.ownerDeleting[voiceTestOwner] = true
		var candidate Candidate
		for _, item := range repository.candidates {
			candidate = item
		}
		repository.mu.Unlock()
		cleanup, err := service.ReclaimObjects(
			context.Background(),
			4,
		)
		if err != nil || cleanup.Deleted != 0 ||
			!store.Has(candidate.ObjectKey) {
			t.Fatalf("active failed-Commit cleanup = %#v, %v", cleanup, err)
		}
		repository.mu.Lock()
		candidate = repository.candidates[candidate.ID]
		candidate.UploadLeaseUntil = time.Now().Add(-time.Second)
		repository.candidates[candidate.ID] = candidate
		repository.mu.Unlock()
		cleanup, err = service.ReclaimObjects(
			context.Background(),
			4,
		)
		if err != nil || cleanup.Deleted != 1 ||
			store.Has(candidate.ObjectKey) {
			t.Fatalf("expired failed-Commit cleanup = %#v, %v", cleanup, err)
		}
	})
}

func TestVoiceUploadAmbiguousCommitReplaysWithoutSecondPut(t *testing.T) {
	repository := newMemoryVoiceRepository()
	repository.failCommitAfterWrite = true
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingVoiceStore{Store: store}
	service := newVoiceLeaseTestService(t, repository, counting, store)
	audio := voiceTestWAV(0x43)
	request := UploadRequest{
		ThreadID:       voiceTestThread,
		IdempotencyKey: "voice-ambiguous-commit",
		ContentType:    platformmedia.ContentTypeWAV,
		Audio:          bytes.NewReader(audio),
	}
	if _, err := service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	); !errors.Is(err, ErrRepository) {
		t.Fatalf("ambiguous Commit Upload() error = %v", err)
	}
	request.Audio = bytes.NewReader(audio)
	recovered, err := service.Upload(
		context.Background(),
		voiceTestActor(),
		request,
	)
	if err != nil ||
		recovered.Status != StatusReady ||
		counting.puts.Load() != 1 ||
		repository.uploadClaims != 1 {
		t.Fatalf(
			"ambiguous Commit replay = %#v err=%v puts=%d claims=%d",
			recovered,
			err,
			counting.puts.Load(),
			repository.uploadClaims,
		)
	}
}

func TestVoiceConfirmationReplayResumesOriginalPendingRun(t *testing.T) {
	processor := &voiceTestRunProcessor{failFirst: true}
	fixture := newVoiceInputFixture(
		t,
		&voiceStaticRecognizer{result: successfulVoiceTranscription()},
		processor,
	)
	candidate, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-confirm-recovery",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x40)),
		},
	)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	command := ConfirmCandidateCommand{
		CandidateID:      candidate.ID,
		CandidateVersion: candidate.CandidateVersion,
		ClientMessageID:  "voice-message-client-1",
		ConfirmedText:    "User corrected canonical transcript.",
	}
	if _, err := fixture.service.Confirm(
		context.Background(),
		voiceTestActor(),
		command,
	); err == nil {
		t.Fatal("first Confirm() unexpectedly hid processing failure")
	}
	if fixture.repository.confirmCreates != 1 ||
		len(fixture.repository.messages) != 1 {
		t.Fatalf(
			"atomic confirmation counts = creates %d messages %d",
			fixture.repository.confirmCreates,
			len(fixture.repository.messages),
		)
	}
	original := fixture.repository.confirmation
	if original.Run.Status != agentrun.StatusPending {
		t.Fatalf("original agentrun.Run after failed processing = %#v", original.Run)
	}

	recovered, err := fixture.service.Confirm(
		context.Background(),
		voiceTestActor(),
		command,
	)
	if err != nil {
		t.Fatalf("replayed Confirm() error = %v", err)
	}
	if recovered.Run.ID != original.Run.ID ||
		recovered.Message.ID != original.Message.ID ||
		recovered.Run.Status != agentrun.StatusCompleted ||
		fixture.repository.confirmCreates != 1 ||
		len(fixture.repository.messages) != 1 ||
		processor.calls != 2 {
		t.Fatalf(
			"recovered confirmation = %#v, processor calls = %d",
			recovered,
			processor.calls,
		)
	}
	terminal, err := fixture.service.Confirm(
		context.Background(),
		voiceTestActor(),
		command,
	)
	if err != nil || terminal.Run.ID != recovered.Run.ID ||
		processor.calls != 2 {
		t.Fatalf(
			"terminal replay = %#v, err=%v calls=%d",
			terminal,
			err,
			processor.calls,
		)
	}
}

func TestVoiceConfirmationAllowsFeedbackNotApplicable(t *testing.T) {
	fixture := newVoiceInputFixture(
		t,
		&voiceStaticRecognizer{result: successfulVoiceTranscription()},
		&voiceTestRunProcessor{},
	)
	fixture.service.feedback = voiceInputFeedbackStub{}
	candidate, err := fixture.service.Upload(
		context.Background(),
		voiceTestActor(),
		UploadRequest{
			ThreadID:       voiceTestThread,
			IdempotencyKey: "voice-feedback-not-applicable",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV(0x41)),
		},
	)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	confirmation, err := fixture.service.Confirm(
		context.Background(),
		voiceTestActor(),
		ConfirmCandidateCommand{
			CandidateID:      candidate.ID,
			CandidateVersion: candidate.CandidateVersion,
			ClientMessageID:  "voice-message-client-not-applicable",
			ConfirmedText:    "你好，我叫奶龙。",
		},
	)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmation.Run.Status != agentrun.StatusCompleted ||
		confirmation.Message.SpeechFeedbackStatusURL != "" {
		t.Fatalf("confirmation = %#v", confirmation)
	}
}

func TestVoicePlaybackRejectsExpiredOrUntrustedCapabilities(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cases := map[string]objectstore.SignedGetResult{
		"expired": {
			URL:       "https://private.example/audio.wav?signature=fake",
			ExpiresAt: now.Add(-time.Second),
		},
		"insecure": {
			URL:       "http://private.example/audio.wav?signature=fake",
			ExpiresAt: now.Add(time.Minute),
		},
		"missing host": {
			URL:       "https:///audio.wav?signature=fake",
			ExpiresAt: now.Add(time.Minute),
		},
	}
	for name, signed := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newVoiceInputFixture(
				t,
				&voiceStaticRecognizer{result: successfulVoiceTranscription()},
				&voiceTestRunProcessor{},
			)
			fixture.service.clock = func() time.Time { return now }
			fixture.service.store = signedVoiceStore{
				Store:  fixture.store,
				result: signed,
			}
			fixture.repository.audios["50000000-0000-4000-8000-000000000001"] =
				agentconversation.MessageAudio{
					ID:        "50000000-0000-4000-8000-000000000001",
					OwnerID:   voiceTestOwner,
					ObjectKey: "audio/v1/agent/private.wav",
					Status:    agentconversation.MessageAudioReadable,
				}
			_, err := fixture.service.Playback(
				context.Background(),
				voiceTestActor(),
				"50000000-0000-4000-8000-000000000001",
			)
			if !errors.Is(err, ErrRepository) {
				t.Fatalf("Playback() error = %v, want safe rejection", err)
			}
		})
	}
}

type voiceInputFixture struct {
	service    *Service
	repository *memoryVoiceRepository
	store      *objectfake.Store
	sources    *storedVoiceSourceLoader
}

func newVoiceInputFixture(
	t *testing.T,
	recognizer StreamingSpeechRecognizer,
	processor *voiceTestRunProcessor,
) voiceInputFixture {
	t.Helper()
	store, err := objectfake.New("audio/v1", 2*time.Minute)
	if err != nil {
		t.Fatalf("new fake object store: %v", err)
	}
	repository := newMemoryVoiceRepository()
	processor.repository = repository
	sources := &storedVoiceSourceLoader{
		store:     store,
		directory: t.TempDir(),
	}
	service, err := NewService(
		repository,
		store,
		sources,
		recognizer,
		&voiceStaticSynthesizer{},
		processor,
		&voiceTestIDs{values: []string{
			"30000000-0000-4000-8000-000000000001",
			"30000000-0000-4000-8000-000000000002",
			"30000000-0000-4000-8000-000000000003",
		}},
		Config{
			Configuration: agentrun.Configuration{
				Provider:           "fake",
				Model:              "fake-free-model",
				MaxOutputTokens:    128,
				MaxInputCharacters: 12000,
			},
			ScratchDirectory: t.TempDir(),
		},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return voiceInputFixture{
		service:    service,
		repository: repository,
		store:      store,
		sources:    sources,
	}
}

func successfulVoiceTranscription() TranscriptionResult {
	return TranscriptionResult{
		ID:         "fake-asr-request-1",
		Provider:   "fake",
		Model:      "fake-asr-model",
		Transcript: "A faithful provider transcript.",
		Language:   "en",
	}
}

func voiceTestActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    voiceTestOwner,
		SessionID: "90000000-0000-4000-8000-000000000001",
	}
}

func voiceTestWAV(sample byte) []byte {
	const (
		sampleRate = 16_000
		samples    = 1_600
		dataSize   = samples * 2
	)
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(result[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], dataSize)
	for index := 44; index < len(result); index++ {
		result[index] = sample
	}
	return result
}

type voiceTestIDs struct {
	mu     sync.Mutex
	values []string
}

func (ids *voiceTestIDs) NewID() (string, error) {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	if len(ids.values) == 0 {
		return "", errors.New("voice test IDs exhausted")
	}
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}

type voiceRecognizerStep struct {
	result TranscriptionResult
	err    error
}

type voiceStaticRecognizer struct {
	result TranscriptionResult
}

func (recognizer *voiceStaticRecognizer) Transcribe(
	ctx context.Context,
	request TranscriptionRequest,
) (TranscriptionResult, error) {
	if err := ctx.Err(); err != nil {
		return TranscriptionResult{}, err
	}
	if err := ValidateTranscriptionRequest(request); err != nil {
		return TranscriptionResult{}, err
	}
	return recognizer.result, nil
}

func (recognizer *voiceStaticRecognizer) TranscribeStream(
	ctx context.Context,
	request TranscriptionRequest,
	observer TranscriptionObserver,
) (TranscriptionResult, error) {
	result, err := recognizer.Transcribe(ctx, request)
	if err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: result.Transcript},
	); err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: result.Transcript, Final: true},
	); err != nil {
		return TranscriptionResult{}, err
	}
	return result, nil
}

type voiceStaticSynthesizer struct{}

func (*voiceStaticSynthesizer) Synthesize(
	ctx context.Context,
	request SynthesisRequest,
) (SynthesisResult, error) {
	if err := ctx.Err(); err != nil {
		return SynthesisResult{}, err
	}
	if err := ValidateSynthesisRequest(request); err != nil {
		return SynthesisResult{}, err
	}
	return SynthesisResult{}, nil
}

type voiceSequenceRecognizer struct {
	mu      sync.Mutex
	results []voiceRecognizerStep
}

func (recognizer *voiceSequenceRecognizer) Transcribe(
	_ context.Context,
	_ TranscriptionRequest,
) (TranscriptionResult, error) {
	return recognizer.next()
}

func (recognizer *voiceSequenceRecognizer) TranscribeStream(
	ctx context.Context,
	_ TranscriptionRequest,
	observer TranscriptionObserver,
) (TranscriptionResult, error) {
	result, err := recognizer.next()
	if err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: result.Transcript},
	); err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: result.Transcript, Final: true},
	); err != nil {
		return TranscriptionResult{}, err
	}
	return result, nil
}

func (recognizer *voiceSequenceRecognizer) next() (
	TranscriptionResult,
	error,
) {
	recognizer.mu.Lock()
	defer recognizer.mu.Unlock()
	if len(recognizer.results) == 0 {
		return TranscriptionResult{}, errors.New("no fake ASR result")
	}
	result := recognizer.results[0]
	recognizer.results = recognizer.results[1:]
	return result.result, result.err
}

type voiceStreamingContractRecognizer struct {
	result          TranscriptionResult
	transcribeCalls int
	streamCalls     int
}

func (recognizer *voiceStreamingContractRecognizer) Transcribe(
	context.Context,
	TranscriptionRequest,
) (TranscriptionResult, error) {
	recognizer.transcribeCalls++
	return recognizer.result, nil
}

func (recognizer *voiceStreamingContractRecognizer) TranscribeStream(
	ctx context.Context,
	_ TranscriptionRequest,
	observer TranscriptionObserver,
) (TranscriptionResult, error) {
	recognizer.streamCalls++
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: "A provider partial transcript."},
	); err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{
			Transcript: recognizer.result.Transcript,
			Final:      true,
		},
	); err != nil {
		return TranscriptionResult{}, err
	}
	return recognizer.result, nil
}

type voiceRecordingTranscriptionObserver struct {
	updates []TranscriptionUpdate
}

func (observer *voiceRecordingTranscriptionObserver) OnTranscriptionUpdate(
	_ context.Context,
	update TranscriptionUpdate,
) error {
	observer.updates = append(observer.updates, update)
	return nil
}

type storedVoiceSourceLoader struct {
	store     *objectfake.Store
	directory string
	calls     int
}

func (loader *storedVoiceSourceLoader) LoadVoiceAudio(
	_ context.Context,
	candidate Candidate,
) (platformmedia.ManagedAudioSource, error) {
	loader.calls++
	body, found := loader.store.Bytes(candidate.ObjectKey)
	if !found {
		return nil, objectstore.ErrOperationFailed
	}
	return platformmedia.CaptureTemporaryAudio(
		loader.directory,
		candidate.ContentType,
		bytes.NewReader(body),
	)
}

type voiceTestRunProcessor struct {
	calls      int
	failFirst  bool
	repository *memoryVoiceRepository
}

func (processor *voiceTestRunProcessor) ProcessPending(
	_ context.Context,
	_ requestcontext.Actor,
	run agentrun.Run,
) (agentrun.Run, error) {
	processor.calls++
	if processor.failFirst {
		processor.failFirst = false
		return agentrun.Run{}, errors.New("controlled post-commit processing failure")
	}
	run.Status = agentrun.StatusCompleted
	run.AssistantMessageID = "70000000-0000-4000-8000-000000000001"
	if processor.repository != nil {
		processor.repository.mu.Lock()
		processor.repository.confirmation.Run = run
		processor.repository.mu.Unlock()
	}
	return run, nil
}

type voiceInputFeedbackStub struct{}

func (voiceInputFeedbackStub) EnsureMessage(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (FeedbackReference, error) {
	return FeedbackReference{}, nil
}

type signedVoiceStore struct {
	*objectfake.Store
	result objectstore.SignedGetResult
}

type countingVoiceStore struct {
	*objectfake.Store
	puts atomic.Int32
}

func (store *countingVoiceStore) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	store.puts.Add(1)
	return store.Store.Put(ctx, request)
}

type blockingVoiceStore struct {
	*objectfake.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
	puts    atomic.Int32
}

func newBlockingVoiceStore(store *objectfake.Store) *blockingVoiceStore {
	return &blockingVoiceStore{
		Store:   store,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (store *blockingVoiceStore) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	store.puts.Add(1)
	store.once.Do(func() { close(store.started) })
	select {
	case <-ctx.Done():
		return objectstore.PutResult{}, ctx.Err()
	case <-store.release:
		return store.Store.Put(ctx, request)
	}
}

func newVoiceLeaseTestService(
	t *testing.T,
	repository *memoryVoiceRepository,
	store objectstore.Store,
	sourceStore *objectfake.Store,
	uploadLeases ...time.Duration,
) *Service {
	t.Helper()
	uploadLease := time.Minute
	if len(uploadLeases) == 1 {
		uploadLease = uploadLeases[0]
	}
	processor := &voiceTestRunProcessor{repository: repository}
	service, err := NewService(
		repository,
		store,
		&storedVoiceSourceLoader{
			store:     sourceStore,
			directory: t.TempDir(),
		},
		&voiceStaticRecognizer{result: successfulVoiceTranscription()},
		&voiceStaticSynthesizer{},
		processor,
		&voiceTestIDs{values: []string{
			"30000000-0000-4000-8000-000000000011",
			"30000000-0000-4000-8000-000000000012",
			"30000000-0000-4000-8000-000000000013",
		}},
		Config{
			Configuration: agentrun.Configuration{
				Provider:           "fake",
				Model:              "fake-free-model",
				MaxOutputTokens:    128,
				MaxInputCharacters: 12000,
			},
			ScratchDirectory: t.TempDir(),
			UploadLease:      uploadLease,
		},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func (store signedVoiceStore) SignedGet(
	context.Context,
	string,
) (objectstore.SignedGetResult, error) {
	return store.result, nil
}

type memoryVoiceRepository struct {
	mu                   sync.Mutex
	candidates           map[string]Candidate
	uploadKeys           map[string]string
	audios               map[string]agentconversation.MessageAudio
	messages             map[string]agentconversation.Message
	ownerDeleting        map[string]bool
	cleanupTokens        map[string]uint64
	confirmation         Confirmation
	confirmCreates       int
	claims               int
	uploadClaims         int
	failCommitBefore     bool
	failCommitAfterWrite bool
}

func newMemoryVoiceRepository() *memoryVoiceRepository {
	return &memoryVoiceRepository{
		candidates:    make(map[string]Candidate),
		uploadKeys:    make(map[string]string),
		audios:        make(map[string]agentconversation.MessageAudio),
		messages:      make(map[string]agentconversation.Message),
		ownerDeleting: make(map[string]bool),
		cleanupTokens: make(map[string]uint64),
	}
}

func (repository *memoryVoiceRepository) StageCandidate(
	_ context.Context,
	command StageCandidateCommand,
) (CandidateStage, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate := command.Candidate
	key := candidate.OwnerID + "/" + candidate.ThreadID + "/" +
		candidate.UploadRequestID
	if id, found := repository.uploadKeys[key]; found {
		return CandidateStage{
			Candidate: repository.candidates[id],
			Created:   false,
		}, nil
	}
	repository.candidates[candidate.ID] = candidate
	repository.uploadKeys[key] = candidate.ID
	return CandidateStage{Candidate: candidate, Created: true}, nil
}

func (repository *memoryVoiceRepository) CommitUpload(
	_ context.Context,
	ownerID string,
	candidateID string,
	fencingToken uint64,
	etag string,
) (Candidate, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return Candidate{}, ErrNotFound
	}
	if repository.failCommitBefore {
		repository.failCommitBefore = false
		return Candidate{}, ErrRepository
	}
	if candidate.Status != StatusStaged ||
		candidate.ETag != "" ||
		candidate.UploadFencingToken != fencingToken ||
		!candidate.UploadLeaseUntil.After(time.Now()) {
		return Candidate{}, ErrConflict
	}
	candidate.ETag = etag
	candidate.UploadLeaseUntil = time.Time{}
	repository.candidates[candidateID] = candidate
	if repository.failCommitAfterWrite {
		repository.failCommitAfterWrite = false
		return Candidate{}, ErrRepository
	}
	return candidate, nil
}

func (repository *memoryVoiceRepository) ClaimUpload(
	_ context.Context,
	ownerID string,
	candidateID string,
	lease time.Duration,
) (UploadClaim, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return UploadClaim{}, false, ErrNotFound
	}
	if repository.ownerDeleting[ownerID] ||
		candidate.Status != StatusStaged ||
		candidate.ETag != "" ||
		candidate.UploadLeaseUntil.After(time.Now()) {
		return UploadClaim{
			Candidate:      candidate,
			FencingToken:   candidate.UploadFencingToken,
			LeaseExpiresAt: candidate.UploadLeaseUntil,
		}, false, nil
	}
	repository.uploadClaims++
	candidate.UploadFencingToken++
	candidate.UploadLeaseUntil = time.Now().Add(lease)
	repository.candidates[candidateID] = candidate
	return UploadClaim{
		Candidate:      candidate,
		FencingToken:   candidate.UploadFencingToken,
		LeaseExpiresAt: candidate.UploadLeaseUntil,
	}, true, nil
}

func (repository *memoryVoiceRepository) FindCandidate(
	_ context.Context,
	ownerID string,
	candidateID string,
) (Candidate, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return Candidate{}, ErrNotFound
	}
	return candidate, nil
}

func (repository *memoryVoiceRepository) ClaimTranscription(
	_ context.Context,
	ownerID string,
	candidateID string,
	lease time.Duration,
) (TranscriptionClaim, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return TranscriptionClaim{}, false, ErrNotFound
	}
	eligible := candidate.Status == StatusStaged ||
		(candidate.Status == StatusFailed &&
			candidate.FailureRetryable) ||
		(candidate.Status == StatusTranscribing &&
			!candidate.ASRLeaseUntil.After(time.Now()))
	if !eligible || candidate.ETag == "" {
		return TranscriptionClaim{Candidate: candidate}, false, nil
	}
	repository.claims++
	candidate.Status = StatusTranscribing
	candidate.ASRAttempt++
	candidate.CandidateVersion++
	candidate.ASRFencingToken++
	candidate.ASRLeaseUntil = time.Now().Add(lease)
	candidate.ASRCandidateText = ""
	candidate.FailureKind = ""
	candidate.FailureRetryable = false
	repository.candidates[candidateID] = candidate
	return TranscriptionClaim{
		Candidate:      candidate,
		FencingToken:   candidate.ASRFencingToken,
		LeaseExpiresAt: candidate.ASRLeaseUntil,
	}, true, nil
}

func (repository *memoryVoiceRepository) CompleteTranscription(
	_ context.Context,
	ownerID string,
	candidateID string,
	token uint64,
	result TranscriptionResult,
) (Candidate, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return Candidate{}, ErrNotFound
	}
	if candidate.Status != StatusTranscribing ||
		candidate.ASRFencingToken != token {
		return Candidate{}, ErrConflict
	}
	candidate.Status = StatusReady
	candidate.ASRLeaseUntil = time.Time{}
	candidate.ASRRequestID = result.ID
	candidate.ASRProvider = result.Provider
	candidate.ASRModel = result.Model
	candidate.ASRCandidateText = result.Transcript
	candidate.ASRLanguage = result.Language
	candidate.ASREmotion = result.Emotion
	candidate.ASRFinishReason = result.FinishReason
	repository.candidates[candidateID] = candidate
	return candidate, nil
}

func (repository *memoryVoiceRepository) FailTranscription(
	_ context.Context,
	ownerID string,
	candidateID string,
	token uint64,
	kind string,
	retryable bool,
) (Candidate, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[candidateID]
	if !found || candidate.OwnerID != ownerID {
		return Candidate{}, ErrNotFound
	}
	if candidate.Status != StatusTranscribing ||
		candidate.ASRFencingToken != token {
		return Candidate{}, ErrConflict
	}
	candidate.Status = StatusFailed
	candidate.ASRLeaseUntil = time.Time{}
	candidate.FailureKind = kind
	candidate.FailureRetryable = retryable
	repository.candidates[candidateID] = candidate
	return candidate, nil
}

func (repository *memoryVoiceRepository) ConfirmCandidate(
	_ context.Context,
	ownerID string,
	command ConfirmCandidateCommand,
) (Confirmation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[command.CandidateID]
	if !found || candidate.OwnerID != ownerID {
		return Confirmation{}, ErrNotFound
	}
	if candidate.Status == StatusConfirmed {
		result := repository.confirmation
		if candidate.CandidateVersion != command.CandidateVersion {
			return Confirmation{}, ErrCandidateStale
		}
		if result.Message.ClientMessageID != command.ClientMessageID ||
			result.Message.Content != command.ConfirmedText {
			return Confirmation{}, ErrIdempotencyConflict
		}
		result.Created = false
		result.Run = repository.confirmation.Run
		return result, nil
	}
	if candidate.Status != StatusReady {
		return Confirmation{}, ErrConflict
	}
	if candidate.CandidateVersion != command.CandidateVersion {
		return Confirmation{}, ErrCandidateStale
	}
	message := agentconversation.Message{
		ID:              "40000000-0000-4000-8000-000000000001",
		OwnerID:         ownerID,
		ThreadID:        candidate.ThreadID,
		Sequence:        1,
		Role:            agentconversation.MessageRoleUser,
		ClientMessageID: command.ClientMessageID,
		Modality:        agentconversation.MessageModalityVoice,
		Content:         command.ConfirmedText,
	}
	audio := agentconversation.MessageAudio{
		ID:             "50000000-0000-4000-8000-000000000001",
		OwnerID:        ownerID,
		ThreadID:       candidate.ThreadID,
		MessageID:      message.ID,
		CandidateID:    candidate.ID,
		ObjectKey:      candidate.ObjectKey,
		ContentType:    candidate.ContentType,
		Size:           candidate.Size,
		ChecksumSHA256: candidate.ChecksumSHA256,
		Duration:       candidate.Duration,
		SampleRate:     candidate.SampleRate,
		ETag:           candidate.ETag,
		Status:         agentconversation.MessageAudioReadable,
	}
	run := agentrun.Run{
		ID:                "60000000-0000-4000-8000-000000000001",
		OwnerID:           ownerID,
		ThreadID:          candidate.ThreadID,
		InputMessageID:    message.ID,
		Attempt:           1,
		Status:            agentrun.StatusPending,
		RequestedProvider: command.Configuration.Provider,
		RequestedModel:    command.Configuration.Model,
		MaxOutputTokens:   command.Configuration.MaxOutputTokens,
	}
	evidence := TranscriptEvidence{
		ID:               "80000000-0000-4000-8000-000000000001",
		OwnerID:          ownerID,
		ThreadID:         candidate.ThreadID,
		CandidateID:      candidate.ID,
		CandidateVersion: candidate.CandidateVersion,
		MessageID:        message.ID,
		ASRRequestID:     candidate.ASRRequestID,
		ASRProvider:      candidate.ASRProvider,
		ASRModel:         candidate.ASRModel,
		ASRCandidateText: candidate.ASRCandidateText,
		ConfirmedText:    command.ConfirmedText,
	}
	candidate.Status = StatusConfirmed
	candidate.ConfirmedMessageID = message.ID
	candidate.ConfirmedRunID = run.ID
	candidate.MessageAudioID = audio.ID
	result := Confirmation{
		Candidate: candidate,
		Evidence:  evidence,
		Message:   message,
		Audio:     audio,
		Run:       run,
		Created:   true,
	}
	repository.candidates[candidate.ID] = candidate
	repository.messages[message.ID] = message
	repository.audios[audio.ID] = audio
	repository.confirmation = result
	repository.confirmCreates++
	return result, nil
}

func (repository *memoryVoiceRepository) FindMessageAudio(
	_ context.Context,
	ownerID string,
	audioID string,
) (agentconversation.MessageAudio, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	audio, found := repository.audios[audioID]
	if !found || audio.OwnerID != ownerID {
		return agentconversation.MessageAudio{}, ErrNotFound
	}
	return audio, nil
}

func (repository *memoryVoiceRepository) FindMessageByID(
	_ context.Context,
	ownerID string,
	messageID string,
) (agentconversation.Message, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	message, found := repository.messages[messageID]
	if !found || message.OwnerID != ownerID {
		return agentconversation.Message{}, ErrNotFound
	}
	return message, nil
}

func (repository *memoryVoiceRepository) BeginCandidateDeletion(
	_ context.Context,
	ownerID string,
	candidateID string,
) (Candidate, error) {
	candidate, err := repository.FindCandidate(
		context.Background(),
		ownerID,
		candidateID,
	)
	if err != nil {
		return Candidate{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if candidate.UploadLeaseUntil.After(time.Now()) {
		return Candidate{}, ErrCandidateProcessing
	}
	candidate.Status = StatusDeleting
	candidate.UploadLeaseUntil = time.Time{}
	candidate.UploadFencingToken++
	repository.candidates[candidateID] = candidate
	return candidate, nil
}

func (repository *memoryVoiceRepository) FinishCandidateDeletion(
	_ context.Context,
	ownerID string,
	candidateID string,
) (Candidate, error) {
	candidate, err := repository.FindCandidate(
		context.Background(),
		ownerID,
		candidateID,
	)
	if err != nil {
		return Candidate{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate.Status = StatusDeleted
	repository.candidates[candidateID] = candidate
	return candidate, nil
}

func (repository *memoryVoiceRepository) BeginMessageAudioDeletion(
	_ context.Context,
	ownerID string,
	audioID string,
) (agentconversation.MessageAudio, error) {
	audio, err := repository.FindMessageAudio(
		context.Background(),
		ownerID,
		audioID,
	)
	if err != nil {
		return agentconversation.MessageAudio{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	audio.Status = agentconversation.MessageAudioDeleting
	repository.audios[audioID] = audio
	return audio, nil
}

func (repository *memoryVoiceRepository) FinishMessageAudioDeletion(
	_ context.Context,
	ownerID string,
	audioID string,
) (agentconversation.MessageAudio, error) {
	audio, err := repository.FindMessageAudio(
		context.Background(),
		ownerID,
		audioID,
	)
	if err != nil {
		return agentconversation.MessageAudio{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	audio.Status = agentconversation.MessageAudioDeleted
	repository.audios[audioID] = audio
	return audio, nil
}

func (repository *memoryVoiceRepository) ClaimCleanup(
	_ context.Context,
	lease time.Duration,
	limit int,
) ([]CleanupClaim, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	now := time.Now()
	result := make([]CleanupClaim, 0, limit)
	for id, candidate := range repository.candidates {
		if len(result) == limit {
			break
		}
		if candidate.UploadLeaseUntil.After(now) {
			continue
		}
		expired := candidate.ExpiresAt.Before(now) ||
			candidate.ExpiresAt.Equal(now)
		eligible := repository.ownerDeleting[candidate.OwnerID] &&
			candidate.Status != StatusDeleted
		eligible = eligible ||
			candidate.Status == StatusDeleting ||
			(expired &&
				(candidate.Status == StatusStaged ||
					candidate.Status == StatusReady ||
					candidate.Status == StatusFailed)) ||
			(expired &&
				candidate.Status == StatusTranscribing &&
				!candidate.ASRLeaseUntil.After(now))
		if !eligible {
			continue
		}
		candidate.Status = StatusDeleting
		candidate.UploadLeaseUntil = time.Time{}
		candidate.UploadFencingToken++
		candidate.ASRLeaseUntil = time.Time{}
		candidate.ASRFencingToken++
		repository.cleanupTokens[id]++
		repository.candidates[id] = candidate
		result = append(result, CleanupClaim{
			Kind:           CleanupCandidate,
			OwnerID:        candidate.OwnerID,
			CandidateID:    candidate.ID,
			AudioID:        candidate.MessageAudioID,
			ObjectKey:      candidate.ObjectKey,
			FencingToken:   repository.cleanupTokens[id],
			LeaseExpiresAt: now.Add(lease),
		})
	}
	return result, nil
}

func (repository *memoryVoiceRepository) FinishCleanup(
	_ context.Context,
	claim CleanupClaim,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	candidate, found := repository.candidates[claim.CandidateID]
	if !found || candidate.OwnerID != claim.OwnerID {
		return ErrNotFound
	}
	if candidate.Status != StatusDeleting ||
		repository.cleanupTokens[claim.CandidateID] !=
			claim.FencingToken {
		return ErrConflict
	}
	candidate.Status = StatusDeleted
	candidate.DeletedAt = time.Now()
	repository.candidates[claim.CandidateID] = candidate
	if candidate.MessageAudioID != "" {
		audio := repository.audios[candidate.MessageAudioID]
		audio.Status = agentconversation.MessageAudioDeleted
		audio.DeletedAt = time.Now()
		repository.audios[candidate.MessageAudioID] = audio
	}
	return nil
}

func (repository *memoryVoiceRepository) ReleaseCleanup(
	_ context.Context,
	claim CleanupClaim,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.cleanupTokens[claim.CandidateID] !=
		claim.FencingToken {
		return ErrConflict
	}
	return nil
}

var _ Repository = (*memoryVoiceRepository)(nil)
var _ AudioSourceLoader = (*storedVoiceSourceLoader)(nil)
var _ PendingRunProcessor = (*voiceTestRunProcessor)(nil)
