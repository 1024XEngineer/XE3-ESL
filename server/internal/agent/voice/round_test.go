package voice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestVoiceTurnProgressRequiresAuthoritativeLimitCompletionEquivalence(
	t *testing.T,
) {
	valid := VoiceTurnProgress{
		EffectiveTurns:   3,
		SessionVersion:   4,
		TurnLimit:        3,
		SessionCompleted: true,
	}
	if !validVoiceTurnProgress(valid) {
		t.Fatal("authoritative completed Practice progress was rejected")
	}
	for name, progress := range map[string]VoiceTurnProgress{
		"limit reached but not completed": {
			EffectiveTurns: 3,
			SessionVersion: 4,
			TurnLimit:      3,
		},
		"completed before limit": {
			EffectiveTurns:   2,
			SessionVersion:   3,
			TurnLimit:        3,
			SessionCompleted: true,
		},
		"missing limit": {
			EffectiveTurns: 1,
			SessionVersion: 2,
		},
		"missing persisted version": {
			EffectiveTurns: 1,
			TurnLimit:      3,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if validVoiceTurnProgress(progress) {
				t.Fatalf("invalid Practice progress accepted: %#v", progress)
			}
		})
	}
	if !validVoiceTurnProgress(VoiceTurnProgress{
		EffectiveTurns: 1,
		SessionVersion: 2,
		TurnLimit:      6,
	}) {
		t.Fatal("frozen six-turn Practice progress was rejected")
	}
}

func TestVoiceRoundOrchestratorRejectsMismatchedCandidateBeforePractice(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(1)
	mismatched := &mismatchedAgentVoiceConversation{
		agentVoiceConversation: conversations,
	}
	practice := newAgentVoicePractice(0)
	orchestrator := newAgentVoiceOrchestrator(
		t,
		mismatched,
		practice,
		newAgentVoiceReview(),
	)

	_, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    "candidate-1",
			IdempotencyKey: "confirm-candidate-1",
		},
	)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("mismatched candidate error = %v", err)
	}
	if practice.effectiveTurns != 0 {
		t.Fatalf(
			"mismatched candidate advanced Practice to %d",
			practice.effectiveTurns,
		)
	}
}

func TestVoiceRoundOrchestratorOwnsThreeTurnReviewSaga(t *testing.T) {
	conversations := newAgentVoiceConversation(3)
	practice := newAgentVoicePractice(0)
	reviews := newAgentVoiceReview()
	completions := newAgentVoiceCompletionEvaluation()
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		reviews,
		completions,
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

	if !third.SessionCompleted || third.ReviewID != "" {
		t.Fatalf("third round = %#v", third)
	}
	orchestrator.completionTasks.Wait()
	if persisted := conversations.turns[third.ID]; persisted.ReviewID !=
		"review-session-1" {
		t.Fatalf("persisted third round = %#v", persisted)
	}
	replayed, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    "candidate-3",
			IdempotencyKey: "confirm-candidate-3",
		},
	)
	if err != nil || replayed.ReviewID != "review-session-1" {
		t.Fatalf("replay = %#v, %v", replayed, err)
	}
	orchestrator.completionTasks.Wait()
	if practice.effectiveTurns != 3 ||
		reviews.creations != 1 ||
		conversations.reviewSaves != 1 ||
		completions.creations != 1 {
		t.Fatalf(
			"effective=%d reviews=%d saves=%d evaluations=%d",
			practice.effectiveTurns,
			reviews.creations,
			conversations.reviewSaves,
			completions.creations,
		)
	}
}

func TestVoiceRoundOrchestratorCompletesWhenReviewIsDeferred(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	practice.skipReview = true
	reviews := newAgentVoiceReview()
	completions := newAgentVoiceCompletionEvaluation()
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		reviews,
		completions,
	)

	result, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		conversation.ConfirmVoiceTurnCommand{
			CandidateID:    "candidate-1",
			IdempotencyKey: "confirm-candidate-1",
		},
	)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !result.SessionCompleted || result.ReviewID != "" {
		t.Fatalf("completed result = %#v", result)
	}
	orchestrator.completionTasks.Wait()
	if reviews.creations != 0 || conversations.reviewSaves != 0 {
		t.Fatalf(
			"deferred review generated: reviews=%d saves=%d",
			reviews.creations,
			conversations.reviewSaves,
		)
	}
	if completions.creations != 1 {
		t.Fatalf(
			"completion Evaluations = %d, want 1",
			completions.creations,
		)
	}
}

func TestVoiceRoundOrchestratorReturnsCompletedTurnBeforeReview(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	reviews := &blockingAgentVoiceReview{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseReview := func() {
		releaseOnce.Do(func() { close(reviews.release) })
	}
	defer releaseReview()
	feedback := &agentVoiceTurnFeedback{}
	orchestrator, err := NewVoiceRoundOrchestrator(
		conversations,
		practice,
		reviews,
		newAgentVoiceCompletionEvaluation(),
		feedback,
	)
	if err != nil {
		t.Fatalf("NewVoiceRoundOrchestrator: %v", err)
	}

	type confirmation struct {
		turn conversation.ConfirmedVoiceTurn
		err  error
	}
	result := make(chan confirmation, 1)
	go func() {
		turn, confirmErr := orchestrator.Confirm(
			context.Background(),
			agentVoiceActor("a"),
			conversation.ConfirmVoiceTurnCommand{
				CandidateID:    "candidate-1",
				IdempotencyKey: "confirm-candidate-1",
			},
		)
		result <- confirmation{turn: turn, err: confirmErr}
	}()

	select {
	case confirmed := <-result:
		if confirmed.err != nil ||
			!confirmed.turn.SessionCompleted ||
			confirmed.turn.ReviewID != "" ||
			confirmed.turn.SpeechFeedbackStatusURL !=
				"/v1/speech-feedback/feedback-1" {
			t.Fatalf("completed confirmation = %#v, %v", confirmed.turn, confirmed.err)
		}
	case <-time.After(time.Second):
		t.Fatal("completed confirmation waited for formal Review")
	}
	select {
	case <-reviews.started:
	case <-time.After(time.Second):
		t.Fatal("formal Review did not start asynchronously")
	}
	releaseReview()
	orchestrator.completionTasks.Wait()
}

func TestVoiceRoundOrchestratorConcurrentThirdTurnCreatesOneReview(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	reviews := newAgentVoiceReview()
	completions := newAgentVoiceCompletionEvaluation()
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		reviews,
		completions,
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
	orchestrator.completionTasks.Wait()
	close(results)
	close(failures)

	for err := range failures {
		t.Errorf("concurrent confirm: %v", err)
	}
	for result := range results {
		if result.EffectiveTurns != 3 ||
			!result.SessionCompleted ||
			(result.ReviewID != "" &&
				result.ReviewID != "review-session-1") {
			t.Errorf("concurrent result = %#v", result)
		}
	}
	if practice.effectiveTurns != 3 ||
		reviews.creations != 1 ||
		conversations.turnCreations != 1 ||
		completions.creations != 1 {
		t.Fatalf(
			"effective=%d reviews=%d turns=%d evaluations=%d",
			practice.effectiveTurns,
			reviews.creations,
			conversations.turnCreations,
			completions.creations,
		)
	}
}

func TestVoiceRoundOrchestratorRecoversReviewAcknowledgementLoss(t *testing.T) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	reviews := newAgentVoiceReview()
	reviews.failAfterCreate = true
	completions := newAgentVoiceCompletionEvaluation()
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		reviews,
		completions,
	)
	command := conversation.ConfirmVoiceTurnCommand{
		CandidateID:    "candidate-1",
		IdempotencyKey: "confirm-candidate-1",
	}

	if _, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	); err != nil {
		t.Fatalf("first confirm error = %v", err)
	}
	orchestrator.completionTasks.Wait()
	recovered, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	)
	if err != nil || !recovered.SessionCompleted {
		t.Fatalf("recovered = %#v, %v", recovered, err)
	}
	orchestrator.completionTasks.Wait()
	if persisted := conversations.turns[recovered.ID]; persisted.ReviewID !=
		"review-session-1" {
		t.Fatalf("persisted recovered turn = %#v", persisted)
	}
	if reviews.creations != 1 ||
		practice.effectiveTurns != 3 ||
		conversations.turnCreations != 1 ||
		completions.creations != 1 {
		t.Fatalf(
			"reviews=%d effective=%d turns=%d evaluations=%d",
			reviews.creations,
			practice.effectiveTurns,
			conversations.turnCreations,
			completions.creations,
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
			completions := newAgentVoiceCompletionEvaluation()
			orchestrator := newAgentVoiceOrchestratorWithCompletion(
				t,
				conversations,
				practice,
				reviews,
				completions,
			)
			command := conversation.ConfirmVoiceTurnCommand{
				CandidateID:    "candidate-1",
				IdempotencyKey: "confirm-candidate-1",
			}

			_, firstErr := orchestrator.Confirm(
				context.Background(),
				agentVoiceActor("a"),
				command,
			)
			if test.failProgressSaves > 0 {
				if !errors.Is(firstErr, errAgentVoiceCheckpoint) {
					t.Fatalf("first confirm error = %v", firstErr)
				}
			} else if firstErr != nil {
				t.Fatalf("first confirm error = %v", firstErr)
			}
			orchestrator.completionTasks.Wait()
			recovered, err := orchestrator.Confirm(
				context.Background(),
				agentVoiceActor("a"),
				command,
			)
			if err != nil || !recovered.SessionCompleted {
				t.Fatalf("recovered = %#v, %v", recovered, err)
			}
			orchestrator.completionTasks.Wait()
			if persisted := conversations.turns[recovered.ID]; persisted.ReviewID !=
				"review-session-1" {
				t.Fatalf("persisted recovered turn = %#v", persisted)
			}
			if practice.effectiveTurns != 3 ||
				reviews.creations != 1 ||
				conversations.turnCreations != 1 ||
				completions.creations != 1 {
				t.Fatalf(
					"effective=%d reviews=%d turns=%d evaluations=%d",
					practice.effectiveTurns,
					reviews.creations,
					conversations.turnCreations,
					completions.creations,
				)
			}
		})
	}
}

func TestVoiceRoundOrchestratorRetriesFailedCompletionEvaluation(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(1)
	practice := newAgentVoicePractice(2)
	practice.skipReview = true
	completions := newAgentVoiceCompletionEvaluation()
	completions.failAfterCreate = true
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		newAgentVoiceReview(),
		completions,
	)
	command := conversation.ConfirmVoiceTurnCommand{
		CandidateID:    "candidate-1",
		IdempotencyKey: "confirm-candidate-1",
	}

	if _, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	); err != nil {
		t.Fatalf("first completion error = %v", err)
	}
	orchestrator.completionTasks.Wait()
	recovered, err := orchestrator.Confirm(
		context.Background(),
		agentVoiceActor("a"),
		command,
	)
	if err != nil || !recovered.SessionCompleted ||
		recovered.ReviewID != "" {
		t.Fatalf("recovered = %#v, %v", recovered, err)
	}
	orchestrator.completionTasks.Wait()
	if completions.creations != 1 || completions.calls != 2 ||
		practice.effectiveTurns != 3 ||
		conversations.turnCreations != 1 {
		t.Fatalf(
			"evaluations=%d calls=%d effective=%d turns=%d",
			completions.creations,
			completions.calls,
			practice.effectiveTurns,
			conversations.turnCreations,
		)
	}
}

func TestVoiceRoundOrchestratorTriggersCompletionOnlyAtFourteenthTurn(
	t *testing.T,
) {
	conversations := newAgentVoiceConversation(2)
	practice := newAgentVoicePracticeWithLimit(12, 14)
	practice.skipReview = true
	completions := newAgentVoiceCompletionEvaluation()
	orchestrator := newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		newAgentVoiceReview(),
		completions,
	)

	for round := 1; round <= 2; round++ {
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
			t.Fatalf("round %d: %v", round+12, err)
		}
		if result.SessionCompleted != (round == 2) {
			t.Fatalf("round %d result = %#v", round+12, result)
		}
		orchestrator.completionTasks.Wait()
		if got := completions.creations; got != max(0, round-1) {
			t.Fatalf(
				"round %d completion creations = %d",
				round+12,
				got,
			)
		}
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
	return newAgentVoiceOrchestratorWithCompletion(
		t,
		conversations,
		practice,
		reviews,
		newAgentVoiceCompletionEvaluation(),
	)
}

func newAgentVoiceOrchestratorWithCompletion(
	t *testing.T,
	conversations VoiceConversationPort,
	practice VoicePracticePort,
	reviews VoiceReviewPort,
	completions VoiceCompletionEvaluationPort,
) *VoiceRoundOrchestrator {
	t.Helper()
	orchestrator, err := NewVoiceRoundOrchestrator(
		conversations,
		practice,
		reviews,
		completions,
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
	speech               conversation.QuestionSpeech
}

type mismatchedAgentVoiceConversation struct {
	*agentVoiceConversation
}

func (port *mismatchedAgentVoiceConversation) GetTranscriptionCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	id string,
) (conversation.TranscriptionCandidate, error) {
	candidate, err := port.agentVoiceConversation.GetTranscriptionCandidate(
		ctx,
		actor,
		id,
	)
	candidate.TranscriptID = "different-transcript"
	return candidate, err
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
			EvidenceVersion:         1,
			Transcript:              "Confirmed answer.",
			Provider:                "fake",
			Model:                   "fake-asr-v1",
			ProviderRequestID:       "request-" + string(rune('0'+round)),
			CreatedAt:               time.Unix(int64(round), 0).UTC(),
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
	for turnID, existing := range port.turns {
		if existing.CandidateID == candidate.ID {
			port.confirmations[command.IdempotencyKey] = turnID
			return existing, nil
		}
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
		CandidateID:             candidate.ID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		AnswerText:              candidate.Transcript,
		CountsTowardTurnLimit:   true,
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

func (port *agentVoiceConversation) SynthesizeQuestion(
	context.Context,
	string,
) (conversation.QuestionSpeech, error) {
	return port.speech, nil
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
	turnLimit      int
	skipReview     bool
}

func newAgentVoicePractice(effectiveTurns int) *agentVoicePractice {
	return newAgentVoicePracticeWithLimit(effectiveTurns, 3)
}

func newAgentVoicePracticeWithLimit(
	effectiveTurns int,
	turnLimit int,
) *agentVoicePractice {
	return &agentVoicePractice{
		turns:          make(map[string]VoiceTurnProgress),
		effectiveTurns: effectiveTurns,
		turnLimit:      turnLimit,
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
	countsTowardTurnLimit bool,
) (VoiceTurnProgress, error) {
	practice.mu.Lock()
	defer practice.mu.Unlock()
	if actor.UserID != "user-a" || sessionID != "session-1" {
		return VoiceTurnProgress{}, conversation.ErrVoiceRoundNotFound
	}
	if existing, found := practice.turns[turnID]; found {
		return existing, nil
	}
	if countsTowardTurnLimit {
		practice.effectiveTurns++
	}
	result := VoiceTurnProgress{
		EffectiveTurns:   practice.effectiveTurns,
		SessionVersion:   practice.effectiveTurns + 1,
		TurnLimit:        practice.turnLimit,
		SessionCompleted: practice.effectiveTurns == practice.turnLimit,
	}
	practice.turns[turnID] = result
	return result, nil
}

func (practice *agentVoicePractice) RequiresSessionReview(
	_ context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (bool, error) {
	if actor.UserID != "user-a" || sessionID != "session-1" {
		return false, conversation.ErrVoiceRoundNotFound
	}
	return !practice.skipReview, nil
}

var errAgentVoiceLostAcknowledgement = errors.New(
	"review acknowledgement lost",
)
var errAgentVoiceCheckpoint = errors.New("conversation checkpoint failed")
var errAgentVoiceCompletionAcknowledgement = errors.New(
	"completion evaluation acknowledgement lost",
)

type agentVoiceReview struct {
	mu              sync.Mutex
	bySession       map[string]VoiceReviewCheckpoint
	creations       int
	failAfterCreate bool
}

type blockingAgentVoiceReview struct {
	started chan struct{}
	release chan struct{}
}

func (reviews *blockingAgentVoiceReview) EnsureSessionReview(
	_ context.Context,
	_ requestcontext.Actor,
	source VoiceReviewSource,
) (VoiceReviewCheckpoint, error) {
	close(reviews.started)
	<-reviews.release
	return VoiceReviewCheckpoint{
		ID:           "review-" + source.SessionID,
		SessionID:    source.SessionID,
		SourceTurnID: source.TurnID,
	}, nil
}

type agentVoiceTurnFeedback struct{}

func (*agentVoiceTurnFeedback) EnsureConversationTurn(
	_ context.Context,
	_ requestcontext.Actor,
	_ string,
	_ string,
) (VoiceTurnFeedbackReference, error) {
	return VoiceTurnFeedbackReference{
		StatusURL:  "/v1/speech-feedback/feedback-1",
		Applicable: true,
	}, nil
}

func newAgentVoiceReview() *agentVoiceReview {
	return &agentVoiceReview{
		bySession: make(map[string]VoiceReviewCheckpoint),
	}
}

func (reviews *agentVoiceReview) EnsureSessionReview(
	_ context.Context,
	actor requestcontext.Actor,
	source VoiceReviewSource,
) (VoiceReviewCheckpoint, error) {
	reviews.mu.Lock()
	defer reviews.mu.Unlock()
	if actor.UserID != "user-a" ||
		source.TurnID == "" ||
		source.SessionID == "" {
		return VoiceReviewCheckpoint{}, conversation.ErrVoiceRoundNotFound
	}
	if existing, found := reviews.bySession[source.SessionID]; found {
		return existing, nil
	}
	result := VoiceReviewCheckpoint{
		ID:           "review-" + source.SessionID,
		SessionID:    source.SessionID,
		SourceTurnID: source.TurnID,
	}
	reviews.bySession[source.SessionID] = result
	reviews.creations++
	if reviews.failAfterCreate {
		reviews.failAfterCreate = false
		return VoiceReviewCheckpoint{}, errAgentVoiceLostAcknowledgement
	}
	return result, nil
}

type agentVoiceCompletionEvaluation struct {
	mu              sync.Mutex
	bySession       map[string]VoiceCompletionEvaluationSource
	calls           int
	creations       int
	failAfterCreate bool
}

func newAgentVoiceCompletionEvaluation() *agentVoiceCompletionEvaluation {
	return &agentVoiceCompletionEvaluation{
		bySession: make(map[string]VoiceCompletionEvaluationSource),
	}
}

func (evaluations *agentVoiceCompletionEvaluation) EnsureCompletedSessionEvaluation(
	_ context.Context,
	actor requestcontext.Actor,
	source VoiceCompletionEvaluationSource,
) error {
	evaluations.mu.Lock()
	defer evaluations.mu.Unlock()
	evaluations.calls++
	if actor.UserID != "user-a" ||
		source.TurnID == "" ||
		source.SessionID == "" {
		return conversation.ErrVoiceRoundNotFound
	}
	if _, found := evaluations.bySession[source.SessionID]; found {
		return nil
	}
	evaluations.bySession[source.SessionID] = source
	evaluations.creations++
	if evaluations.failAfterCreate {
		evaluations.failAfterCreate = false
		return errAgentVoiceCompletionAcknowledgement
	}
	return nil
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
