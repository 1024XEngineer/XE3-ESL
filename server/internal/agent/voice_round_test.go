package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestVoiceRoundOrchestratorOwnsThreeTurnReviewSaga(t *testing.T) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)

	var third conversation.ConfirmedVoiceTurn
	for round := 1; round <= 3; round++ {
		candidateID := agentVoiceCandidateID(round)
		result, err := orchestrator.Confirm(
			context.Background(),
			agentVoiceActor("a"),
			conversation.ConfirmVoiceTurnCommand{
				CandidateID:    candidateID,
				IdempotencyKey: "confirm-" + candidateID,
			},
		)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if result.EffectiveTurns != round {
			t.Fatalf(
				"round %d effective turns = %d",
				round,
				result.EffectiveTurns,
			)
		}
		if round < 3 && (result.SessionCompleted || result.ReviewID != "") {
			t.Fatalf("round %d completed early: %#v", round, result)
		}
		third = result
	}

	if !third.SessionCompleted || third.ReviewID != "review-session-1" {
		t.Fatalf("third round = %#v", third)
	}
	replayed, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    "candidate-3",
			IdempotencyKey: "confirm-candidate-3",
		},
	)
	if err != nil || replayed.ReviewID != third.ReviewID {
		t.Fatalf("replay = %#v, %v", replayed, err)
	}
	if practice.effectiveTurns != 3 ||
		reviews.creations != 1 ||
		conversations.reviewSaves != 1 {
		t.Fatalf(
			"effective=%d reviews=%d saves=%d",
			practice.effectiveTurns,
			reviews.creations,
			conversations.reviewSaves,
		)
	}
}

func TestVoiceRoundOrchestratorConcurrentThirdTurnCreatesOneReview(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)

	const callers = 16
	results := make(chan conversation.ConfirmedVoiceTurn, callers)
	failures := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := orchestrator.Confirm(
				context.Background(),
				agentVoiceActor("a"),
				conversation.ConfirmVoiceTurnCommand{
					CandidateID:    "candidate-1",
					IdempotencyKey: "confirm-candidate-1",
				},
			)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(failures)

	for err := range failures {
		t.Errorf("concurrent confirm: %v", err)
	}
	for result := range results {
		if result.EffectiveTurns != 3 ||
			!result.SessionCompleted ||
			result.ReviewID != "review-session-1" {
			t.Errorf("concurrent result = %#v", result)
		}
	}
	if practice.effectiveTurns != 3 ||
		reviews.creations != 1 ||
		conversations.turnCreations != 1 {
		t.Fatalf(
			"effective=%d reviews=%d turns=%d",
			practice.effectiveTurns,
			reviews.creations,
			conversations.turnCreations,
		)
	}
}

func TestVoiceRoundOrchestratorRecoversReviewAcknowledgementLoss(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	reviews := newAgentVoiceReview()
	reviews.failAfterCreate = true
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)
	command := conversation.ConfirmVoiceTurnCommand{
		CandidateID:    "candidate-1",
		IdempotencyKey: "confirm-candidate-1",
	}

	if _, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	); !errors.Is(err, errAgentVoiceLostAcknowledgement) {
		t.Fatalf("first confirm error = %v", err)
	}
	recovered, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	)
	if err != nil || recovered.ReviewID != "review-session-1" {
		t.Fatalf("recovered = %#v, %v", recovered, err)
	}
	if reviews.creations != 1 ||
		practice.effectiveTurns != 3 ||
		conversations.turnCreations != 1 {
		t.Fatalf(
			"reviews=%d effective=%d turns=%d",
			reviews.creations,
			practice.effectiveTurns,
			conversations.turnCreations,
		)
	}
}

func TestVoiceRoundOrchestratorRecoversLocalCheckpointFailures(t *testing.T) {
	tests := []struct {
		name              string
		failProgressSaves int
		failReviewSaves   int
	}{
		{name: "practice progress checkpoint", failProgressSaves: 1},
		{name: "review reference checkpoint", failReviewSaves: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversations := newAgentVoiceConversation(1)
			conversations.progressSaveFailures = test.failProgressSaves
			conversations.reviewSaveFailures = test.failReviewSaves
			practice := newAgentVoicePractice(2)
			reviews := newAgentVoiceReview()
			orchestrator := newAgentVoiceOrchestrator(
				t,
				conversations,
				practice,
				reviews,
			)
			command := conversation.ConfirmVoiceTurnCommand{
				CandidateID:    "candidate-1",
				IdempotencyKey: "confirm-candidate-1",
			}

			if _, err := orchestrator.Confirm(
				context.Background(),
				agentVoiceActor("a"),
				command,
			); !errors.Is(err, errAgentVoiceCheckpoint) {
				t.Fatalf("first confirm error = %v", err)
			}
			recovered, err := orchestrator.Confirm(
				context.Background(),
				agentVoiceActor("a"),
				command,
			)
			if err != nil || recovered.ReviewID != "review-session-1" {
				t.Fatalf("recovered = %#v, %v", recovered, err)
			}
			if practice.effectiveTurns != 3 ||
				reviews.creations != 1 ||
				conversations.turnCreations != 1 {
				t.Fatalf(
					"effective=%d reviews=%d turns=%d",
					practice.effectiveTurns,
					reviews.creations,
					conversations.turnCreations,
				)
			}
		})
	}
}

func TestVoiceRoundOrchestratorUsesTrustedActorForParticipantResolution(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	orchestrator := newAgentVoiceOrchestrator(
		t,
		conversations,
		practice,
		reviews,
	)

	_, err := orchestrator.Transcribe(
		context.Background(),
		agentVoiceActor("b"),
		conversation.TranscribeVoiceCommand{SessionID: "session-1"},
	)
	if !errors.Is(err, conversation.ErrVoiceRoundNotFound) {
		t.Fatalf("foreign Actor error = %v", err)
	}
	if conversations.transcribeCalls != 0 {
		t.Fatal("Conversation was called after Practice rejected the Actor")
	}
}

func newAgentVoiceOrchestrator(
	t *testing.T,
	conversations VoiceConversationPort,
	practice VoicePracticePort,
	reviews VoiceReviewPort,
) *VoiceRoundOrchestrator {
	t.Helper()
	orchestrator, err := NewVoiceRoundOrchestrator(
		conversations,
		practice,
		reviews,
	)
	if err != nil {
		t.Fatalf("new orchestrator: %v", err)
	}
	return orchestrator
}

type agentVoiceConversation struct {
	mu                   sync.Mutex
	candidates           map[string]conversation.TranscriptionCandidate
	confirmations        map[string]string
	turns                map[string]conversation.ConfirmedVoiceTurn
	turnCreations        int
	reviewSaves          int
	transcribeCalls      int
	progressSaveFailures int
	reviewSaveFailures   int
}

func newAgentVoiceConversation(count int) *agentVoiceConversation {
	result := &agentVoiceConversation{
		candidates:    make(map[string]conversation.TranscriptionCandidate),
		confirmations: make(map[string]string),
		turns:         make(map[string]conversation.ConfirmedVoiceTurn),
	}
	for round := 1; round <= count; round++ {
		id := agentVoiceCandidateID(round)
		result.candidates[id] = conversation.TranscriptionCandidate{
			ID:                      id,
			SessionID:               "session-1",
			QuestionID:              "question-" + string(rune('0'+round)),
			QuestionSpeakerID:       "participant-interviewer",
			AddresseeParticipantIDs: []string{"participant-a"},
			RespondentParticipantID: "participant-a",
			TranscriptID:            "transcript-" + string(rune('0'+round)),
			TranscriptVersion:       "fake-asr-v1",
			Transcript:              "Confirmed answer.",
			Provider:                "fake",
			Model:                   "fake-asr-v1",
			ProviderRequestID:       "request-" + string(rune('0'+round)),
		}
	}
	return result
}

func (port *agentVoiceConversation) Transcribe(
	_ context.Context,
	actor requestcontext.Actor,
	participantID string,
	_ conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	port.transcribeCalls++
	if actor.UserID != "user-a" || participantID != "participant-a" {
		return conversation.TranscriptionCandidate{},
			conversation.ErrVoiceRoundNotFound
	}
	return port.candidates["candidate-1"], nil
}

func (port *agentVoiceConversation) GetTranscriptionCandidate(
	_ context.Context,
	actor requestcontext.Actor,
	id string,
) (conversation.TranscriptionCandidate, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	candidate, found := port.candidates[id]
	if actor.UserID != "user-a" || !found {
		return conversation.TranscriptionCandidate{},
			conversation.ErrVoiceRoundNotFound
	}
	return candidate, nil
}

func (port *agentVoiceConversation) Confirm(
	_ context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	candidate, found := port.candidates[command.CandidateID]
	if actor.UserID != "user-a" || !found {
		return conversation.ConfirmedVoiceTurn{},
			conversation.ErrVoiceRoundNotFound
	}
	if turnID, ok := port.confirmations[command.IdempotencyKey]; ok {
		return port.turns[turnID], nil
	}
	turn := conversation.ConfirmedVoiceTurn{
		ID:                "turn-" + candidate.QuestionID,
		SessionID:         candidate.SessionID,
		QuestionID:        candidate.QuestionID,
		QuestionSpeakerID: candidate.QuestionSpeakerID,
		AddresseeParticipantIDs: append(
			[]string(nil),
			candidate.AddresseeParticipantIDs...,
		),
		RespondentParticipantID: candidate.RespondentParticipantID,
		TranscriptID:            candidate.TranscriptID,
		TranscriptVersion:       candidate.TranscriptVersion,
		AnswerText:              candidate.Transcript,
	}
	port.confirmations[command.IdempotencyKey] = turn.ID
	port.turns[turn.ID] = turn
	port.turnCreations++
	return turn, nil
}

func (port *agentVoiceConversation) SaveTurnProgress(
	_ context.Context,
	actor requestcontext.Actor,
	turnID string,
	progress conversation.VoiceTurnProgress,
) (conversation.ConfirmedVoiceTurn, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	turn, found := port.turns[turnID]
	if actor.UserID != "user-a" || !found {
		return conversation.ConfirmedVoiceTurn{},
			conversation.ErrVoiceRoundNotFound
	}
	if port.progressSaveFailures > 0 {
		port.progressSaveFailures--
		return conversation.ConfirmedVoiceTurn{}, errAgentVoiceCheckpoint
	}
	if turn.EffectiveTurns == 0 {
		turn.EffectiveTurns = progress.EffectiveTurns
		turn.SessionCompleted = progress.SessionCompleted
		port.replaceTurn(turn)
	}
	return turn, nil
}

func (port *agentVoiceConversation) SaveTurnReview(
	_ context.Context,
	actor requestcontext.Actor,
	turnID string,
	reviewID string,
) (conversation.ConfirmedVoiceTurn, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	turn, found := port.turns[turnID]
	if actor.UserID != "user-a" || !found {
		return conversation.ConfirmedVoiceTurn{},
			conversation.ErrVoiceRoundNotFound
	}
	if port.reviewSaveFailures > 0 {
		port.reviewSaveFailures--
		return conversation.ConfirmedVoiceTurn{}, errAgentVoiceCheckpoint
	}
	if turn.ReviewID == "" {
		turn.ReviewID = reviewID
		port.reviewSaves++
		port.replaceTurn(turn)
	}
	return turn, nil
}

func (*agentVoiceConversation) SynthesizeQuestion(
	context.Context,
	string,
) (conversation.QuestionSpeech, error) {
	return conversation.QuestionSpeech{}, nil
}

func (port *agentVoiceConversation) replaceTurn(
	turn conversation.ConfirmedVoiceTurn,
) {
	port.turns[turn.ID] = turn
}

type agentVoicePractice struct {
	mu             sync.Mutex
	turns          map[string]VoiceTurnProgress
	effectiveTurns int
}

func newAgentVoicePractice(effectiveTurns int) *agentVoicePractice {
	return &agentVoicePractice{
		turns:          make(map[string]VoiceTurnProgress),
		effectiveTurns: effectiveTurns,
	}
}

func (*agentVoicePractice) ResolveActorParticipant(
	_ context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (string, error) {
	if actor.UserID != "user-a" || sessionID != "session-1" {
		return "", conversation.ErrVoiceRoundNotFound
	}
	return "participant-a", nil
}

func (practice *agentVoicePractice) ApplyEffectiveTurn(
	_ context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
) (VoiceTurnProgress, error) {
	practice.mu.Lock()
	defer practice.mu.Unlock()
	if actor.UserID != "user-a" || sessionID != "session-1" {
		return VoiceTurnProgress{}, conversation.ErrVoiceRoundNotFound
	}
	if existing, found := practice.turns[turnID]; found {
		return existing, nil
	}
	practice.effectiveTurns++
	result := VoiceTurnProgress{
		EffectiveTurns:   practice.effectiveTurns,
		SessionCompleted: practice.effectiveTurns == 3,
	}
	practice.turns[turnID] = result
	return result, nil
}

var errAgentVoiceLostAcknowledgement = errors.New(
	"review acknowledgement lost",
)
var errAgentVoiceCheckpoint = errors.New("conversation checkpoint failed")

type agentVoiceReview struct {
	mu              sync.Mutex
	bySession       map[string]VoiceSessionReview
	creations       int
	failAfterCreate bool
}

func newAgentVoiceReview() *agentVoiceReview {
	return &agentVoiceReview{
		bySession: make(map[string]VoiceSessionReview),
	}
}

func (reviews *agentVoiceReview) EnsureSessionReview(
	_ context.Context,
	actor requestcontext.Actor,
	source VoiceReviewSource,
) (VoiceSessionReview, error) {
	reviews.mu.Lock()
	defer reviews.mu.Unlock()
	if actor.UserID != "user-a" ||
		source.TranscriptID == "" ||
		source.TranscriptVersion == "" {
		return VoiceSessionReview{}, conversation.ErrVoiceRoundNotFound
	}
	if existing, found := reviews.bySession[source.SessionID]; found {
		return existing, nil
	}
	result := VoiceSessionReview{
		ID:        "review-" + source.SessionID,
		SessionID: source.SessionID,
		TurnID:    source.TurnID,
	}
	reviews.bySession[source.SessionID] = result
	reviews.creations++
	if reviews.failAfterCreate {
		reviews.failAfterCreate = false
		return VoiceSessionReview{}, errAgentVoiceLostAcknowledgement
	}
	return result, nil
}

func agentVoiceCandidateID(round int) string {
	return "candidate-" + string(rune('0'+round))
}

func agentVoiceActor(suffix string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "user-" + suffix,
		SessionID: "auth-session-" + suffix,
	}
}

var _ VoiceConversationPort = (*conversation.VoiceRoundService)(nil)
