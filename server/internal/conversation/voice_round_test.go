package conversation

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

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestVoiceRoundTranscriptionAndConfirmationAreIdempotent(t *testing.T) {
	store := newVoiceTestStore()
	store.addQuestion("question-1")
	recognizer := &voiceTestRecognizer{}
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
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
	service, err := NewVoiceRoundService(
		store,
		vault,
		recognizer,
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
	if recognizer.calls != 1 || store.nextTurn != 1 {
		t.Fatalf("calls: ASR=%d turns=%d", recognizer.calls, store.nextTurn)
	}
}

func TestVoiceRoundFailureAndForeignActorStayConversationScoped(t *testing.T) {
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
	providerError := ai.NewSpeechError(
		ai.SpeechOperationTranscription,
		ai.ErrorTimeout,
		0,
		"",
		"safe-request",
		context.DeadlineExceeded,
	)
	service, err := NewVoiceRoundService(
		store,
		vault,
		&voiceTestRecognizer{err: providerError},
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

func TestVoiceRoundConcurrentConfirmationCreatesOneConversationTurn(t *testing.T) {
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
	results := make(chan ConfirmedVoiceTurn, callers)
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
			turn.SessionCompleted ||
			turn.ReviewID != "" {
			t.Errorf("concurrent result = %#v", turn)
		}
	}
	if store.nextTurn != 1 {
		t.Fatalf("conversation turns=%d", store.nextTurn)
	}
}

func TestVoiceRoundTTSFailurePreservesQuestionText(t *testing.T) {
	service, err := NewVoiceRoundService(
		newVoiceTestStore(),
		voiceNoopVault{},
		&voiceTestRecognizer{},
		&voiceTestSynthesizer{err: ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorProviderUnavailable,
			0,
			"",
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
		result.Failure.Kind != ai.ErrorProviderUnavailable {
		t.Fatalf("unexpected text fallback: %#v", result)
	}
}

func TestVoiceRoundTTSGenericFailureUsesSynthesisAuditOperation(t *testing.T) {
	service, err := NewVoiceRoundService(
		newVoiceTestStore(),
		voiceNoopVault{},
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
		result.Failure.Operation != ai.SpeechOperationSynthesis ||
		result.Failure.Kind != ai.ErrorProviderUnavailable {
		t.Fatalf("unexpected generic TTS audit: %#v", result.Failure)
	}
}

type voiceTestStore struct {
	mu            sync.Mutex
	questions     map[string]VoiceQuestion
	reservations  map[string]voiceTestReservation
	candidates    map[string]TranscriptionCandidate
	confirmations map[string]ConfirmedVoiceTurn
	turns         map[string]ConfirmedVoiceTurn
	attempts      []SafeProcessingAttempt
	nextCandidate int
	nextTurn      int
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
		questions:     make(map[string]VoiceQuestion),
		reservations:  make(map[string]voiceTestReservation),
		candidates:    make(map[string]TranscriptionCandidate),
		confirmations: make(map[string]ConfirmedVoiceTurn),
		turns:         make(map[string]ConfirmedVoiceTurn),
	}
}

func (store *voiceTestStore) addQuestion(id string) {
	store.questions[id] = VoiceQuestion{
		ID:                      id,
		SessionID:               "session-1",
		Text:                    "Question",
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
) (VoiceQuestion, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	question, found := store.questions[questionID]
	if actor.UserID != "user-a" || !found || question.SessionID != sessionID {
		return VoiceQuestion{}, ErrVoiceRoundNotFound
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
) (ConfirmedVoiceTurn, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if actor.UserID != "user-a" {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundNotFound
	}
	if turn, found := store.confirmations[command.IdempotencyKey]; found {
		if turn.TranscriptID != store.candidates[command.CandidateID].TranscriptID {
			return ConfirmedVoiceTurn{}, ErrVoiceRoundConflict
		}
		return turn, nil
	}
	candidate, found := store.candidates[command.CandidateID]
	if !found {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundNotFound
	}
	store.nextTurn++
	turn := ConfirmedVoiceTurn{
		ID:                "turn-" + candidate.QuestionID,
		SessionID:         candidate.SessionID,
		QuestionID:        candidate.QuestionID,
		QuestionSpeakerID: candidate.QuestionSpeakerID,
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

func (store *voiceTestStore) SaveTurnProgress(
	_ context.Context,
	_ requestcontext.Actor,
	turnID string,
	progress VoiceTurnProgress,
) (ConfirmedVoiceTurn, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	turn, found := store.turns[turnID]
	if !found {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundNotFound
	}
	turn.EffectiveTurns = progress.EffectiveTurns
	turn.SessionCompleted = progress.SessionCompleted
	store.replaceTurn(turn)
	return turn, nil
}

func (store *voiceTestStore) SaveTurnReview(
	_ context.Context,
	_ requestcontext.Actor,
	turnID string,
	reviewID string,
) (ConfirmedVoiceTurn, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	turn, found := store.turns[turnID]
	if !found {
		return ConfirmedVoiceTurn{}, ErrVoiceRoundNotFound
	}
	turn.ReviewID = reviewID
	store.replaceTurn(turn)
	return turn, nil
}

func (store *voiceTestStore) replaceTurn(turn ConfirmedVoiceTurn) {
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

type leaseTestRecognizer struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

func (recognizer *leaseTestRecognizer) Transcribe(
	_ context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	if err := ai.ValidateTranscriptionRequest(request); err != nil {
		return ai.TranscriptionResult{}, err
	}
	recognizer.mu.Lock()
	recognizer.calls++
	call := recognizer.calls
	recognizer.mu.Unlock()
	if call == 1 {
		close(recognizer.firstStarted)
		<-recognizer.releaseFirst
	}
	return ai.TranscriptionResult{
		ID:         "asr-" + string(rune('0'+call)),
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: "Recovered answer.",
	}, nil
}

func (recognizer *voiceTestRecognizer) Transcribe(
	_ context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	recognizer.calls++
	if recognizer.err != nil {
		return ai.TranscriptionResult{}, recognizer.err
	}
	return ai.TranscriptionResult{
		ID:         "asr-request",
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: "A confirmed English answer.",
	}, ai.ValidateTranscriptionRequest(request)
}

type voiceTestSynthesizer struct {
	err error
}

func (synthesizer *voiceTestSynthesizer) Synthesize(
	_ context.Context,
	_ ai.SynthesisRequest,
) (ai.SynthesisResult, error) {
	return ai.SynthesisResult{}, synthesizer.err
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
