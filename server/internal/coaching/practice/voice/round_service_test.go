package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestVoiceRoundTranscriptionAndConfirmationAreIdempotent(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	realtimeRecognizer := &streamingVoiceTestRecognizer{}
	recordedRecognizer := &voiceTestRecognizer{}
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
	service, err := NewVoiceRoundService(
		store,
		vault,
		realtimeRecognizer,
		recordedRecognizer,
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	actor := voiceTestActor("a")
	audio := voiceTestWAV()
	candidate, err := service.Transcribe(
		context.Background(),
		actor,
		"participant-a",
		TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "transcribe-question-1",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(audio),
		},
	)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	turn, err := service.Confirm(
		context.Background(),
		actor,
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-question-1",
		},
	)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	replayedCandidate, err := service.Transcribe(
		context.Background(),
		actor,
		"participant-a",
		TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     candidate.QuestionID,
			IdempotencyKey: "transcribe-" + candidate.QuestionID,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(audio),
		},
	)
	if err != nil || replayedCandidate.ID != candidate.ID {
		t.Fatalf("transcription replay = %#v, %v", replayedCandidate, err)
	}
	replayedTurn, err := service.Confirm(
		context.Background(),
		actor,
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-" + candidate.QuestionID,
		},
	)
	if err != nil || !reflect.DeepEqual(replayedTurn, turn) {
		t.Fatalf("confirmation replay = %#v, %v", replayedTurn, err)
	}
	if recordedRecognizer.calls != 1 || realtimeRecognizer.streamCalls != 0 ||
		realtimeRecognizer.transcribeCalls != 0 || store.nextTurn != 1 {
		t.Fatalf(
			"calls: recorded=%d realtime=%#v turns=%d",
			recordedRecognizer.calls,
			realtimeRecognizer,
			store.nextTurn,
		)
	}
}

func TestVoiceRoundStreamsProviderSnapshotsAndPersistsFinalCandidate(
	t *testing.T,
) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	recognizer := &streamingVoiceTestRecognizer{}
	recordedRecognizer := &voiceTestRecognizer{}
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	service, err := NewVoiceRoundService(
		store,
		vault,
		recognizer,
		recordedRecognizer,
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	command := func(pcm io.Reader) TranscribeVoiceStreamCommand {
		return TranscribeVoiceStreamCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "transcribe-stream-question-1",
			PCM:            pcm,
			SampleRate:     16_000,
		}
	}
	observer := &voiceTestTranscriptionObserver{
		observed: make(chan TranscriptionUpdate, 2),
	}
	reader, writer := io.Pipe()
	type streamResult struct {
		candidate TranscriptionCandidate
		err       error
	}
	completed := make(chan streamResult, 1)
	go func() {
		candidate, streamErr := service.TranscribeStream(
			context.Background(),
			voiceTestActor("a"),
			"participant-a",
			command(reader),
			observer,
		)
		completed <- streamResult{candidate: candidate, err: streamErr}
	}()
	pcm := voiceTestPCM()
	if _, err := writer.Write(pcm[:320]); err != nil {
		t.Fatalf("write first live PCM frame: %v", err)
	}
	select {
	case update := <-observer.observed:
		if update.Transcript != "A complete" || update.Final {
			t.Fatalf("live update before stream end = %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("no transcription update before the PCM stream ended")
	}
	if _, err := writer.Write(pcm[320:]); err != nil {
		t.Fatalf("write remaining live PCM: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close live PCM: %v", err)
	}
	result := <-completed
	if result.err != nil {
		t.Fatalf("stream transcription: %v", result.err)
	}
	candidate := result.candidate
	if candidate.Transcript != "A complete streaming answer." ||
		candidate.ProviderRequestID != "asr-stream-request" ||
		recognizer.streamCalls != 1 || recognizer.transcribeCalls != 0 ||
		recordedRecognizer.calls != 0 {
		t.Fatalf("candidate = %#v, recognizer = %#v", candidate, recognizer)
	}
	wantUpdates := []TranscriptionUpdate{
		{Transcript: "A complete", Final: false},
		{Transcript: "A complete streaming answer.", Final: true},
	}
	if !reflect.DeepEqual(observer.updates, wantUpdates) {
		t.Fatalf("updates = %#v, want %#v", observer.updates, wantUpdates)
	}
	replayObserver := &voiceTestTranscriptionObserver{}
	replayed, err := service.TranscribeStream(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command(bytes.NewReader(voiceTestPCM())),
		replayObserver,
	)
	if err != nil || replayed.ID != candidate.ID ||
		recognizer.streamCalls != 1 || len(replayObserver.updates) != 0 {
		t.Fatalf(
			"replay = %#v, err = %v, calls = %d, updates = %#v",
			replayed,
			err,
			recognizer.streamCalls,
			replayObserver.updates,
		)
	}
}

func TestVoiceRoundStreamingRequiresStreamingRecognizer(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	service, err := NewVoiceRoundService(
		store,
		vault,
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = service.TranscribeStream(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		TranscribeVoiceStreamCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "transcribe-stream-question-1",
			PCM:            bytes.NewReader(voiceTestPCM()),
			SampleRate:     16_000,
		},
		&voiceTestTranscriptionObserver{},
	)
	var providerError *ProviderError
	if !errors.As(err, &providerError) ||
		providerError.Kind != ProviderErrorConfiguration ||
		providerError.Retryable() {
		t.Fatalf("streaming error = %#v", err)
	}
}

func TestVoiceRoundTextAnswerUsesDurableCandidateWithoutASR(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	recognizer := &voiceTestRecognizer{}
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	recordings := newVoiceTestRecordings()
	service, err := NewVoiceRoundServiceWithRecordings(
		store,
		vault,
		recognizer,
		recognizer,
		&voiceTestSynthesizer{},
		recordings,
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	command := SubmitTextAnswerCommand{
		SessionID:      "session-1",
		QuestionID:     "question-1",
		IdempotencyKey: "text-question-1",
		AnswerText:     "  I led the rollout and communicated the risk.  ",
	}
	actor := voiceTestActor("a")
	candidate, err := service.SubmitTextAnswer(
		context.Background(),
		actor,
		"participant-a",
		command,
	)
	if err != nil {
		t.Fatalf("submit text: %v", err)
	}
	if candidate.Transcript != "I led the rollout and communicated the risk." ||
		candidate.Provider != "speakup" ||
		candidate.Model != "direct_text" ||
		recognizer.calls != 0 {
		t.Fatalf("text candidate = %#v, ASR calls = %d", candidate, recognizer.calls)
	}
	turn, err := service.ConfirmText(
		context.Background(),
		actor,
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: command.IdempotencyKey,
		},
	)
	if err != nil {
		t.Fatalf("confirm text: %v", err)
	}
	if turn.AnswerText != candidate.Transcript || turn.AudioAssetID != "" {
		t.Fatalf("text turn = %#v", turn)
	}
	replayed, err := service.SubmitTextAnswer(
		context.Background(),
		actor,
		"participant-a",
		command,
	)
	if err != nil || replayed.ID != candidate.ID || store.nextCandidate != 1 {
		t.Fatalf("text replay = %#v, %v", replayed, err)
	}
}

func TestVoiceRoundPersistsRecordingThroughReservationAndTurn(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	recordings := newVoiceTestRecordings()
	store.recordings = recordings
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         2,
			MaxBytes:         platformmedia.MaxAudioBytes * 2,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	newService := func() *VoiceRoundService {
		service, serviceErr := NewVoiceRoundServiceWithRecordings(
			store,
			vault,
			&voiceTestRecognizer{},
			&voiceTestRecognizer{},
			&voiceTestSynthesizer{},
			recordings,
		)
		if serviceErr != nil {
			t.Fatalf("new service: %v", serviceErr)
		}
		return service
	}
	actor := voiceTestActor("a")
	command := func() TranscribeVoiceCommand {
		return TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "transcribe-question-1",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV()),
		}
	}

	candidate, err := newService().Transcribe(
		context.Background(),
		actor,
		"participant-a",
		command(),
	)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if candidate.ReservationID != "reservation-question-1" {
		t.Fatalf("candidate reservation = %#v", candidate)
	}
	turn, err := newService().Confirm(
		context.Background(),
		actor,
		ConfirmVoiceTurnCommand{
			CandidateID:    candidate.ID,
			IdempotencyKey: "confirm-question-1",
		},
	)
	if err != nil {
		t.Fatalf("confirm after restart: %v", err)
	}
	if turn.AudioAssetID != "audio-reservation-question-1" {
		t.Fatalf("confirmed turn recording = %#v", turn)
	}

	replayed, err := newService().Transcribe(
		context.Background(),
		actor,
		"participant-a",
		command(),
	)
	if err != nil || replayed.ID != candidate.ID {
		t.Fatalf("transcribe replay = %#v, %v", replayed, err)
	}
	if recordings.uniqueUploads() != 1 {
		t.Fatalf("unique uploads = %d", recordings.uniqueUploads())
	}
}

func TestVoiceRoundRejectsInvalidAtomicRecordingProjection(
	t *testing.T,
) {
	base := newVoiceTestStore()
	base.addQuestion("question-1")
	recordings := newVoiceTestRecordings()
	base.recordings = recordings
	store := &voiceProjectionStore{voiceTestStore: base}
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	service, err := NewVoiceRoundServiceWithRecordings(
		store,
		vault,
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{},
		recordings,
	)
	if err != nil {
		t.Fatalf("new recording service: %v", err)
	}
	actor := voiceTestActor("a")
	candidate, err := service.Transcribe(
		context.Background(),
		actor,
		"participant-a",
		TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "transcribe-projection",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV()),
		},
	)
	if err != nil {
		t.Fatalf("transcribe projection fixture: %v", err)
	}
	valid := practice.Turn{
		ID:                      "turn-projection",
		SessionID:               candidate.SessionID,
		QuestionID:              candidate.QuestionID,
		SpeakerParticipantID:    candidate.QuestionSpeakerID,
		AddresseeParticipantIDs: candidate.AddresseeParticipantIDs,
		RespondentParticipantID: candidate.RespondentParticipantID,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
		AudioAssetID:            "audio-projection",
	}
	for name, mutate := range map[string]func(*voiceProjectionStore){
		"candidate projection mismatch": func(store *voiceProjectionStore) {
			store.result = valid
			store.result.SessionID = "other-session"
		},
		"missing live recording": func(store *voiceProjectionStore) {
			store.result = valid
			store.result.AudioAssetID = ""
		},
		"deleted marker with capability": func(store *voiceProjectionStore) {
			store.result = valid
			store.recordingDeleted = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			store.recordingDeleted = false
			mutate(store)
			if _, err := service.Confirm(
				context.Background(),
				actor,
				ConfirmVoiceTurnCommand{
					CandidateID:    candidate.ID,
					IdempotencyKey: "confirm-projection",
				},
			); !errors.Is(err, ErrVoiceRoundConflict) {
				t.Fatalf("invalid atomic projection error = %v", err)
			}
		})
	}
}

func TestVoiceRoundFailureAndForeignActorStayVoiceScoped(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	providerError := NewProviderError(
		ProviderOperationTranscription,
		ProviderErrorTimeout,
		"safe-request",
		context.DeadlineExceeded,
	)
	recognizer := &voiceTestRecognizer{err: providerError}
	service, err := NewVoiceRoundService(
		store,
		vault,
		recognizer,
		recognizer,
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	command := TranscribeVoiceCommand{
		SessionID:      "session-1",
		QuestionID:     "question-1",
		IdempotencyKey: "transcribe-failure",
		ContentType:    platformmedia.ContentTypeWAV,
		Audio:          bytes.NewReader(voiceTestWAV()),
	}
	_, err = service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command,
	)
	if !errors.Is(err, providerError) {
		t.Fatalf("provider failure = %v", err)
	}
	if len(store.attempts) != 1 ||
		store.attempts[0].RequestID != "safe-request" {
		t.Fatalf(
			"failure changed progress or lost safe audit: %#v",
			store.attempts,
		)
	}

	command.Audio = bytes.NewReader(voiceTestWAV())
	_, err = service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command,
	)
	if !errors.Is(err, providerError) {
		t.Fatalf("provider retry after cleanup = %v", err)
	}
	if len(store.attempts) != 2 {
		t.Fatalf("provider retry attempts = %#v", store.attempts)
	}

	recognizer.err = nil
	command.Audio = bytes.NewReader(voiceTestWAV())
	candidate, err := service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command,
	)
	if err != nil || candidate.ID == "" {
		t.Fatalf("provider recovery = %#v, %v", candidate, err)
	}
	command.Audio = bytes.NewReader(voiceTestWAV())
	replayed, err := service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command,
	)
	if err != nil || replayed.ID != candidate.ID || recognizer.calls != 3 ||
		len(store.candidates) != 1 {
		t.Fatalf(
			"provider recovery replay = %#v, %v, calls=%d candidates=%d",
			replayed,
			err,
			recognizer.calls,
			len(store.candidates),
		)
	}

	command.Audio = bytes.NewReader(voiceTestWAV())
	_, err = service.Transcribe(
		context.Background(),
		voiceTestActor("b"),
		"participant-b",
		command,
	)
	if !errors.Is(err, ErrVoiceRoundNotFound) {
		t.Fatalf("foreign actor error = %v", err)
	}
	if _, err := service.Confirm(
		context.Background(),
		voiceTestActor("b"),
		ConfirmVoiceTurnCommand{
			CandidateID:    "candidate-question-1",
			IdempotencyKey: "foreign-confirm",
		},
	); !errors.Is(err, ErrVoiceRoundNotFound) {
		t.Fatalf("foreign confirmation error = %v", err)
	}
}

func TestVoiceRoundCapacityFailsBeforeReservationAndProvider(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	recognizer := &voiceTestRecognizer{}
	service, err := NewVoiceRoundService(
		store,
		voiceCapacityVault{},
		recognizer,
		recognizer,
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "capacity-before-reservation",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV()),
		},
	)
	if !errors.Is(err, ErrVoiceRoundCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	if recognizer.calls != 0 || len(store.reservations) != 0 {
		t.Fatalf(
			"capacity reached persistence/provider: ASR=%d reservations=%d",
			recognizer.calls,
			len(store.reservations),
		)
	}
}

func TestVoiceRoundPersistsProviderOutcomeAfterCallerCancellation(t *testing.T) {
	for _, test := range []struct {
		name        string
		providerErr error
		wantFailed  bool
	}{
		{
			name: "failure audit",
			providerErr: NewProviderError(
				ProviderOperationTranscription,
				ProviderErrorCancelled,
				"safe-cancelled-request",
				context.Canceled,
			),
			wantFailed: true,
		},
		{name: "successful candidate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			inner := newVoiceTestStore()
			inner.addQuestion("question-1")
			store := &voiceCancellationAuditStore{
				voiceTestStore: inner,
			}
			vault, err := platformmedia.NewTemporaryAudioVault(
				platformmedia.TemporaryAudioVaultConfig{
					ScratchDirectory: t.TempDir(),
					Lifetime:         time.Minute,
					MaxItems:         1,
					MaxBytes:         platformmedia.MaxAudioBytes,
				},
			)
			if err != nil {
				t.Fatalf("new vault: %v", err)
			}
			t.Cleanup(func() { _ = vault.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			recognizer := &cancelingVoiceRecognizer{
				cancel: cancel,
				err:    test.providerErr,
			}
			service, err := NewVoiceRoundService(
				store,
				vault,
				recognizer,
				recognizer,
				&voiceTestSynthesizer{},
			)
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			candidate, err := service.Transcribe(
				ctx,
				voiceTestActor("a"),
				"participant-a",
				TranscribeVoiceCommand{
					SessionID:      "session-1",
					QuestionID:     "question-1",
					IdempotencyKey: "cancelled-provider",
					ContentType:    platformmedia.ContentTypeWAV,
					Audio:          bytes.NewReader(voiceTestWAV()),
				},
			)
			if test.wantFailed {
				if !errors.Is(err, test.providerErr) ||
					len(inner.attempts) != 1 ||
					!store.failureContextLive {
					t.Fatalf(
						"failure result = %#v, %v, attempts=%#v live=%t",
						candidate,
						err,
						inner.attempts,
						store.failureContextLive,
					)
				}
				return
			}
			if err != nil ||
				candidate.ID == "" ||
				!store.completionContextLive {
				t.Fatalf(
					"completion result = %#v, %v, live=%t",
					candidate,
					err,
					store.completionContextLive,
				)
			}
		})
	}
}

func TestVoiceRoundExpiredASRLeaseFencesLateWorker(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         2,
			MaxBytes:         platformmedia.MaxAudioBytes * 2,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	recognizer := &leaseTestRecognizer{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	service, err := NewVoiceRoundService(
		store,
		vault,
		recognizer,
		recognizer,
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	command := func() TranscribeVoiceCommand {
		return TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-1",
			IdempotencyKey: "crash-recovery",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV()),
		}
	}
	firstResult := make(chan error, 1)
	go func() {
		_, err := service.Transcribe(
			context.Background(),
			voiceTestActor("a"),
			"participant-a",
			command(),
		)
		firstResult <- err
	}()
	<-recognizer.firstStarted
	store.expireReservation("crash-recovery")

	recovered, err := service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		command(),
	)
	if err != nil || recovered.TranscriptID != "asr-2" {
		t.Fatalf("recovered transcription = %#v, %v", recovered, err)
	}
	close(recognizer.releaseFirst)
	if err := <-firstResult; !errors.Is(err, ErrVoiceRoundConflict) {
		t.Fatalf("late worker error = %v", err)
	}
}

func TestVoiceRoundConcurrentConfirmationCreatesOneVoiceTurn(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-3")
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	service, err := NewVoiceRoundService(
		store,
		vault,
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	candidate, err := service.Transcribe(
		context.Background(),
		voiceTestActor("a"),
		"participant-a",
		TranscribeVoiceCommand{
			SessionID:      "session-1",
			QuestionID:     "question-3",
			IdempotencyKey: "transcribe-question-3",
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          bytes.NewReader(voiceTestWAV()),
		},
	)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	const callers = 16
	results := make(chan practice.Turn, callers)
	failures := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			turn, err := service.Confirm(
				context.Background(),
				voiceTestActor("a"),
				ConfirmVoiceTurnCommand{
					CandidateID:    candidate.ID,
					IdempotencyKey: "confirm-question-3",
				},
			)
			if err != nil {
				failures <- err
				return
			}
			results <- turn
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Errorf("concurrent confirm: %v", err)
	}
	for turn := range results {
		if turn.ID != "turn-question-3" ||
			turn.EffectiveTurns != 0 ||
			turn.SessionCompleted {
			t.Errorf("concurrent result = %#v", turn)
		}
	}
	if store.nextTurn != 1 {
		t.Fatalf("voice turns=%d", store.nextTurn)
	}
}

func TestVoiceRoundTTSFailurePreservesQuestionText(t *testing.T) {
	service, err := NewVoiceRoundService(
		newVoiceTestStore(),
		voiceNoopVault{},
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{err: NewProviderError(
			ProviderOperationSynthesis,
			ProviderErrorUnavailable,
			"",
			errors.New("private provider detail"),
		)},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	result, err := service.SynthesizeQuestion(
		context.Background(),
		"Tell me about a difficult project.",
	)
	if err != nil {
		t.Fatalf("synthesize question: %v", err)
	}
	if result.Text != "Tell me about a difficult project." ||
		result.Audio != nil ||
		result.Failure == nil ||
		result.Failure.Kind != ProviderErrorUnavailable {
		t.Fatalf("unexpected text fallback: %#v", result)
	}
}

func TestVoiceRoundTTSGenericFailureUsesSynthesisAuditOperation(t *testing.T) {
	service, err := NewVoiceRoundService(
		newVoiceTestStore(),
		voiceNoopVault{},
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{err: errors.New("private provider detail")},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	result, err := service.SynthesizeQuestion(
		context.Background(),
		"Tell me about a difficult project.",
	)
	if err != nil {
		t.Fatalf("synthesize question: %v", err)
	}
	if result.Failure == nil ||
		result.Failure.Operation != ProviderOperationSynthesis ||
		result.Failure.Kind != ProviderErrorUnavailable {
		t.Fatalf("unexpected generic TTS audit: %#v", result.Failure)
	}
}

func TestVoiceRoundClosesInvalidTTSProviderAudio(t *testing.T) {
	audio, err := platformmedia.CaptureTemporaryAudio(
		t.TempDir(),
		platformmedia.ContentTypeWAV,
		bytes.NewReader(voiceTestWAV()),
	)
	if err != nil {
		t.Fatalf("capture provider audio: %v", err)
	}
	service, err := NewVoiceRoundService(
		newVoiceTestStore(),
		voiceNoopVault{},
		&voiceTestRecognizer{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{result: SynthesisResult{
			// Missing the required request/provider/model/audio identifiers.
			Audio: audio,
		}},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	result, err := service.SynthesizeQuestion(
		context.Background(),
		"Tell me about a difficult project.",
	)
	if err != nil ||
		result.Audio != nil ||
		result.Failure == nil ||
		result.Failure.Kind != ProviderErrorInvalidResponse {
		t.Fatalf("invalid provider result = %#v, %v", result, err)
	}
	if _, err := audio.Open(); err == nil {
		t.Fatal("invalid provider audio was not closed")
	}
}

type voiceTestStore struct {
	mu            sync.Mutex
	recordings    *voiceTestRecordings
	questions     map[string]practice.Question
	reservations  map[string]voiceTestReservation
	candidates    map[string]TranscriptionCandidate
	confirmations map[string]practice.Turn
	turns         map[string]practice.Turn
	attempts      []SafeProcessingAttempt
	nextCandidate int
	nextTurn      int
}

type voiceProjectionStore struct {
	*voiceTestStore
	result           practice.Turn
	recordingDeleted bool
}

func (store *voiceProjectionStore) ReserveRecordingConfirmation(
	_ context.Context,
	_ requestcontext.Actor,
	_ ConfirmVoiceTurnCommand,
	_ string,
) (VoiceRecordingConfirmation, error) {
	return VoiceRecordingConfirmation{
		Turn:             store.result,
		RecordingDeleted: store.recordingDeleted,
	}, nil
}

type voiceTestRecordings struct {
	mu      sync.Mutex
	uploads map[string]AudioAsset
}

func newVoiceTestRecordings() *voiceTestRecordings {
	return &voiceTestRecordings{uploads: make(map[string]AudioAsset)}
}

func (recordings *voiceTestRecordings) Upload(
	_ context.Context,
	actor AudioAssetActor,
	request UploadRecordingRequest,
) (AudioAsset, error) {
	recordings.mu.Lock()
	defer recordings.mu.Unlock()
	key := actor.UserID + "/" + request.RequestID
	if asset, found := recordings.uploads[key]; found {
		return asset, nil
	}
	audio, err := io.ReadAll(request.Body)
	if err != nil || int64(len(audio)) != request.Size {
		return AudioAsset{}, ErrAudioAssetInvalid
	}
	asset := AudioAsset{
		ID:              "audio-" + request.RequestID,
		OwnerID:         actor.UserID,
		UploadRequestID: request.RequestID,
		ObjectKey:       "audio/v1/assets/audio.wav",
		ContentType:     request.ContentType,
		Size:            request.Size,
		ChecksumSHA256:  request.ChecksumSHA256,
		Duration:        request.Duration,
		ETag:            "etag",
		Status:          AudioAssetMetadataCommitted,
		Version:         2,
	}
	recordings.uploads[key] = asset
	return asset, nil
}

func (recordings *voiceTestRecordings) ConfirmUploadRequest(
	_ context.Context,
	actor AudioAssetActor,
	requestID string,
	candidateID string,
	turnID string,
) (AudioAsset, error) {
	recordings.mu.Lock()
	defer recordings.mu.Unlock()
	key := actor.UserID + "/" + requestID
	asset, found := recordings.uploads[key]
	if !found {
		return AudioAsset{}, ErrAudioAssetNotFound
	}
	if asset.Status == AudioAssetReadable {
		if asset.CandidateID != candidateID || asset.TurnID != turnID {
			return AudioAsset{}, ErrAudioAssetAlreadyBound
		}
		return asset, nil
	}
	asset.CandidateID = candidateID
	asset.TurnID = turnID
	asset.Status = AudioAssetReadable
	asset.Version++
	recordings.uploads[key] = asset
	return asset, nil
}

func (recordings *voiceTestRecordings) GetReadableByTurn(
	_ context.Context,
	actor AudioAssetActor,
	turnID string,
) (AudioAsset, error) {
	recordings.mu.Lock()
	defer recordings.mu.Unlock()
	for _, asset := range recordings.uploads {
		if asset.OwnerID != actor.UserID || asset.TurnID != turnID {
			continue
		}
		if asset.Status != AudioAssetReadable {
			return AudioAsset{}, ErrAudioAssetInvalidTransition
		}
		return asset, nil
	}
	return AudioAsset{}, ErrAudioAssetNotFound
}

func (recordings *voiceTestRecordings) uniqueUploads() int {
	recordings.mu.Lock()
	defer recordings.mu.Unlock()
	return len(recordings.uploads)
}

type voiceTestReservation struct {
	ID          string
	Fingerprint string
	CandidateID string
	Failed      bool
	LeaseToken  string
	LeaseNumber int
}

func newVoiceTestStore() *voiceTestStore {
	return &voiceTestStore{
		questions:     make(map[string]practice.Question),
		reservations:  make(map[string]voiceTestReservation),
		candidates:    make(map[string]TranscriptionCandidate),
		confirmations: make(map[string]practice.Turn),
		turns:         make(map[string]practice.Turn),
	}
}

func (store *voiceTestStore) addQuestion(id string) {
	store.questions[id] = practice.Question{
		ID:                      id,
		SessionID:               "session-1",
		Content:                 "Question",
		SpeakerParticipantID:    "participant-interviewer",
		AddresseeParticipantIDs: []string{"participant-a"},
	}
}

func (store *voiceTestStore) expireReservation(key string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	reservation := store.reservations[key]
	reservation.Failed = true
	store.reservations[key] = reservation
}

func (store *voiceTestStore) GetVoiceQuestion(
	_ context.Context,
	actor requestcontext.Actor,
	sessionID string,
	questionID string,
) (practice.Question, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	question, found := store.questions[questionID]
	if actor.UserID != "user-a" || !found || question.SessionID != sessionID {
		return practice.Question{}, ErrVoiceRoundNotFound
	}
	return question, nil
}

func (store *voiceTestStore) ReserveTranscription(
	_ context.Context,
	actor requestcontext.Actor,
	command ReserveTranscriptionCommand,
) (TranscriptionReservation, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actor.UserID != "user-a" {
		return TranscriptionReservation{}, ErrVoiceRoundNotFound
	}
	if existing, found := store.reservations[command.IdempotencyKey]; found {
		if existing.Fingerprint != command.InputFingerprint {
			return TranscriptionReservation{}, ErrVoiceRoundConflict
		}
		if candidate, completed := store.candidates[existing.CandidateID]; completed {
			return TranscriptionReservation{
				ID:        existing.ID,
				Status:    TranscriptionCompleted,
				Candidate: candidate,
			}, nil
		}
		if !existing.Failed {
			return TranscriptionReservation{
				ID:     existing.ID,
				Status: TranscriptionProcessing,
			}, nil
		}
		existing.Failed = false
		existing.LeaseNumber++
		existing.LeaseToken = voiceLeaseToken(existing.LeaseNumber)
		store.reservations[command.IdempotencyKey] = existing
		return TranscriptionReservation{
			ID:         existing.ID,
			LeaseToken: existing.LeaseToken,
			Status:     TranscriptionReserved,
		}, nil
	}
	id := "reservation-" + command.QuestionID
	store.reservations[command.IdempotencyKey] = voiceTestReservation{
		ID:          id,
		Fingerprint: command.InputFingerprint,
		LeaseToken:  voiceLeaseToken(1),
		LeaseNumber: 1,
	}
	return TranscriptionReservation{
		ID:         id,
		LeaseToken: voiceLeaseToken(1),
		Status:     TranscriptionReserved,
	}, nil
}

func (store *voiceTestStore) CompleteTranscription(
	_ context.Context,
	actor requestcontext.Actor,
	command CompleteTranscriptionCommand,
) (TranscriptionCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actor.UserID != "user-a" {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}
	var key string
	var reservation voiceTestReservation
	for candidateKey, value := range store.reservations {
		if value.ID == command.ReservationID {
			key, reservation = candidateKey, value
			break
		}
	}
	if key == "" {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}
	if reservation.LeaseToken != command.LeaseToken {
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}
	store.nextCandidate++
	questionID := string([]byte(command.ReservationID)[len("reservation-"):])
	candidate := TranscriptionCandidate{
		ID:                      "candidate-" + questionID,
		ReservationID:           command.ReservationID,
		SessionID:               "session-1",
		QuestionID:              questionID,
		QuestionSpeakerID:       "participant-interviewer",
		AddresseeParticipantIDs: []string{"participant-a"},
		RespondentParticipantID: "participant-a",
		TranscriptID:            command.TranscriptID,
		EvidenceVersion:         command.EvidenceVersion,
		Transcript:              command.Transcript,
		Provider:                command.Provider,
		Model:                   command.Model,
		ProviderRequestID:       command.ProviderRequestID,
		CreatedAt:               command.CompletedAt,
	}
	store.candidates[candidate.ID] = candidate
	reservation.CandidateID = candidate.ID
	store.reservations[key] = reservation
	return candidate, nil
}

func (store *voiceTestStore) FailTranscription(
	_ context.Context,
	_ requestcontext.Actor,
	command FailTranscriptionCommand,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, reservation := range store.reservations {
		if reservation.ID == command.ReservationID {
			if reservation.LeaseToken != command.LeaseToken {
				return ErrVoiceRoundConflict
			}
			reservation.Failed = true
			store.reservations[key] = reservation
			store.attempts = append(store.attempts, command.Attempt)
			return nil
		}
	}
	return ErrVoiceRoundNotFound
}

func voiceLeaseToken(number int) string {
	return "lease-" + string(rune('0'+number))
}

func (store *voiceTestStore) GetTranscriptionCandidate(
	_ context.Context,
	actor requestcontext.Actor,
	id string,
) (TranscriptionCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	candidate, found := store.candidates[id]
	if actor.UserID != "user-a" || !found {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}
	return candidate, nil
}

func (store *voiceTestStore) ReserveConfirmation(
	_ context.Context,
	actor requestcontext.Actor,
	command ReserveConfirmationCommand,
) (practice.Turn, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actor.UserID != "user-a" {
		return practice.Turn{}, ErrVoiceRoundNotFound
	}
	if turn, found := store.confirmations[command.IdempotencyKey]; found {
		if turn.TranscriptID != store.candidates[command.CandidateID].TranscriptID {
			return practice.Turn{}, ErrVoiceRoundConflict
		}
		return turn, nil
	}
	candidate, found := store.candidates[command.CandidateID]
	if !found {
		return practice.Turn{}, ErrVoiceRoundNotFound
	}
	store.nextTurn++
	turn := practice.Turn{
		ID:                   "turn-" + candidate.QuestionID,
		SessionID:            candidate.SessionID,
		QuestionID:           candidate.QuestionID,
		SpeakerParticipantID: candidate.QuestionSpeakerID,
		AddresseeParticipantIDs: append(
			[]string(nil),
			candidate.AddresseeParticipantIDs...,
		),
		RespondentParticipantID: candidate.RespondentParticipantID,
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
	}
	store.confirmations[command.IdempotencyKey] = turn
	store.turns[turn.ID] = turn
	return turn, nil
}

func (store *voiceTestStore) ReserveRecordingConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
	uploadRequestID string,
) (VoiceRecordingConfirmation, error) {
	turn, err := store.ReserveConfirmation(
		ctx,
		actor,
		ReserveConfirmationCommand(command),
	)
	if err != nil {
		return VoiceRecordingConfirmation{}, err
	}
	if store.recordings == nil {
		return VoiceRecordingConfirmation{}, ErrVoiceRoundInvalid
	}
	asset, err := store.recordings.ConfirmUploadRequest(
		ctx,
		AudioAssetActor{UserID: actor.UserID},
		uploadRequestID,
		command.CandidateID,
		turn.ID,
	)
	if err != nil {
		return VoiceRecordingConfirmation{}, err
	}
	turn.AudioAssetID = asset.ID
	return VoiceRecordingConfirmation{Turn: turn}, nil
}

func (store *voiceTestStore) replaceTurn(turn practice.Turn) {
	store.turns[turn.ID] = turn
	for key, candidate := range store.confirmations {
		if candidate.ID == turn.ID {
			store.confirmations[key] = turn
		}
	}
}

type voiceTestRecognizer struct {
	calls int
	err   error
}

type streamingVoiceTestRecognizer struct {
	streamCalls     int
	transcribeCalls int
}

func (recognizer *streamingVoiceTestRecognizer) Transcribe(
	_ context.Context,
	request TranscriptionRequest,
) (TranscriptionResult, error) {
	recognizer.transcribeCalls++
	return TranscriptionResult{}, platformmedia.ValidateAudioSource(request.Audio)
}

func (recognizer *streamingVoiceTestRecognizer) TranscribeStream(
	ctx context.Context,
	request StreamingTranscriptionRequest,
	observer TranscriptionObserver,
) (TranscriptionResult, error) {
	recognizer.streamCalls++
	if request.PCM == nil || request.SampleRate != 16_000 {
		return TranscriptionResult{}, ErrVoiceRoundInvalid
	}
	first := make([]byte, 320)
	if _, err := io.ReadFull(request.PCM, first); err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{Transcript: "A complete"},
	); err != nil {
		return TranscriptionResult{}, err
	}
	if _, err := io.Copy(io.Discard, request.PCM); err != nil {
		return TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		TranscriptionUpdate{
			Transcript: "A complete streaming answer.",
			Final:      true,
		},
	); err != nil {
		return TranscriptionResult{}, err
	}
	return TranscriptionResult{
		ID:         "asr-stream-request",
		Provider:   "fake",
		Model:      "fake-streaming-asr-v1",
		Transcript: "A complete streaming answer.",
	}, nil
}

type voiceTestTranscriptionObserver struct {
	updates  []TranscriptionUpdate
	observed chan TranscriptionUpdate
}

func (observer *voiceTestTranscriptionObserver) OnTranscriptionUpdate(
	_ context.Context,
	update TranscriptionUpdate,
) error {
	observer.updates = append(observer.updates, update)
	if observer.observed != nil {
		observer.observed <- update
	}
	return nil
}

type cancelingVoiceRecognizer struct {
	cancel context.CancelFunc
	err    error
}

func (recognizer *cancelingVoiceRecognizer) Transcribe(
	_ context.Context,
	request TranscriptionRequest,
) (TranscriptionResult, error) {
	if err := platformmedia.ValidateAudioSource(request.Audio); err != nil {
		return TranscriptionResult{}, err
	}
	recognizer.cancel()
	if recognizer.err != nil {
		return TranscriptionResult{}, recognizer.err
	}
	return TranscriptionResult{
		ID:         "asr-cancelled-request",
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: "Persist this answer.",
	}, nil
}

type voiceCancellationAuditStore struct {
	*voiceTestStore
	failureContextLive    bool
	completionContextLive bool
}

func (store *voiceCancellationAuditStore) FailTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command FailTranscriptionCommand,
) error {
	store.failureContextLive = ctx.Err() == nil
	if !store.failureContextLive {
		return ctx.Err()
	}
	return store.voiceTestStore.FailTranscription(ctx, actor, command)
}

func (store *voiceCancellationAuditStore) CompleteTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command CompleteTranscriptionCommand,
) (TranscriptionCandidate, error) {
	store.completionContextLive = ctx.Err() == nil
	if !store.completionContextLive {
		return TranscriptionCandidate{}, ctx.Err()
	}
	return store.voiceTestStore.CompleteTranscription(ctx, actor, command)
}

type leaseTestRecognizer struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

func (recognizer *leaseTestRecognizer) Transcribe(
	_ context.Context,
	request TranscriptionRequest,
) (TranscriptionResult, error) {
	if err := platformmedia.ValidateAudioSource(request.Audio); err != nil {
		return TranscriptionResult{}, err
	}
	recognizer.mu.Lock()
	recognizer.calls++
	call := recognizer.calls
	recognizer.mu.Unlock()
	if call == 1 {
		close(recognizer.firstStarted)
		<-recognizer.releaseFirst
	}
	return TranscriptionResult{
		ID:         "asr-" + string(rune('0'+call)),
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: "Recovered answer.",
	}, nil
}

func (recognizer *voiceTestRecognizer) Transcribe(
	_ context.Context,
	request TranscriptionRequest,
) (TranscriptionResult, error) {
	recognizer.calls++
	if recognizer.err != nil {
		return TranscriptionResult{}, recognizer.err
	}
	return TranscriptionResult{
		ID:         "asr-request",
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: "A confirmed English answer.",
	}, platformmedia.ValidateAudioSource(request.Audio)
}

type voiceTestSynthesizer struct {
	result SynthesisResult
	err    error
}

func (synthesizer *voiceTestSynthesizer) Synthesize(
	_ context.Context,
	_ SynthesisRequest,
) (SynthesisResult, error) {
	return synthesizer.result, synthesizer.err
}

type voiceNoopVault struct{}

func (voiceNoopVault) Capture(
	context.Context,
	requestcontext.Actor,
	string,
	io.Reader,
) (platformmedia.TemporaryAudioMetadata, error) {
	return platformmedia.TemporaryAudioMetadata{}, errors.New("unused")
}

func (voiceNoopVault) Source(
	requestcontext.Actor,
	string,
) (platformmedia.AudioSource, error) {
	return nil, errors.New("unused")
}

func (voiceNoopVault) Delete(requestcontext.Actor, string) error {
	return nil
}

type voiceCapacityVault struct {
	voiceNoopVault
}

func (voiceCapacityVault) Capture(
	context.Context,
	requestcontext.Actor,
	string,
	io.Reader,
) (platformmedia.TemporaryAudioMetadata, error) {
	return platformmedia.TemporaryAudioMetadata{},
		platformmedia.ErrTemporaryAudioCapacity
}

func voiceTestActor(seed string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "user-" + seed,
		SessionID: "auth-" + seed,
	}
}

func voiceTestWAV() []byte {
	const (
		sampleRate    = 8_000
		bitsPerSample = 16
		channels      = 1
		samples       = 80
	)
	dataSize := samples * channels * bitsPerSample / 8
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVEfmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(
		buffer,
		binary.LittleEndian,
		uint32(sampleRate*channels*bitsPerSample/8),
	)
	_ = binary.Write(
		buffer,
		binary.LittleEndian,
		uint16(channels*bitsPerSample/8),
	)
	_ = binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSample))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	return buffer.Bytes()
}

func voiceTestPCM() []byte {
	return make([]byte, 3_200)
}
