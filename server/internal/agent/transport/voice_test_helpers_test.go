package transport

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxVoiceReviewMetadataUTF8Bytes = 128
	maxVoiceReviewResultJSONBytes   = 12 * 1024
	maxVoiceReviewSummaryUTF8Bytes  = 2048
	maxVoiceReviewConclusions       = 8
	maxVoiceReviewLabelUTF8Bytes    = 64
	maxVoiceReviewTextUTF8Bytes     = 2048
)

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
		agentVoiceCompletionEvaluation{},
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
	textSubmitCalls      int
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

func (port *agentVoiceConversation) SubmitTextAnswer(
	_ context.Context,
	actor requestcontext.Actor,
	participantID string,
	command conversation.SubmitTextAnswerCommand,
) (conversation.TranscriptionCandidate, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	if actor.UserID != "user-a" ||
		participantID != "participant-a" ||
		command.SessionID != "session-1" ||
		strings.TrimSpace(command.AnswerText) == "" {
		return conversation.TranscriptionCandidate{},
			conversation.ErrVoiceRoundNotFound
	}
	port.textSubmitCalls++
	candidate := conversation.TranscriptionCandidate{
		ID:                      "text-candidate-" + command.QuestionID,
		SessionID:               command.SessionID,
		QuestionID:              command.QuestionID,
		QuestionSpeakerID:       "participant-interviewer",
		AddresseeParticipantIDs: []string{"participant-a"},
		RespondentParticipantID: participantID,
		TranscriptID:            "text-transcript-" + command.QuestionID,
		EvidenceVersion:         1,
		Transcript:              strings.TrimSpace(command.AnswerText),
		Provider:                "speakup",
		Model:                   "direct_text",
		ProviderRequestID:       "text-request-" + command.QuestionID,
		CreatedAt:               time.Unix(1, 0).UTC(),
	}
	port.candidates[candidate.ID] = candidate
	return candidate, nil
}

func (port *agentVoiceConversation) ConfirmText(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	return port.Confirm(ctx, actor, command)
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
		SessionVersion:   practice.effectiveTurns + 1,
		TurnLimit:        3,
		SessionCompleted: practice.effectiveTurns == 3,
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
	return true, nil
}

var errAgentVoiceLostAcknowledgement = errors.New(
	"review acknowledgement lost",
)
var errAgentVoiceCheckpoint = errors.New("conversation checkpoint failed")

type agentVoiceReview struct {
	mu              sync.Mutex
	bySession       map[string]VoiceReviewCheckpoint
	creations       int
	failAfterCreate bool
}

type agentVoiceCompletionEvaluation struct{}

func (agentVoiceCompletionEvaluation) EnsureCompletedSessionEvaluation(
	context.Context,
	requestcontext.Actor,
	VoiceCompletionEvaluationSource,
) error {
	return nil
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

func agentVoiceCandidateID(round int) string {
	return "candidate-" + string(rune('0'+round))
}

func agentVoiceActor(suffix string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "user-" + suffix,
		SessionID: "auth-session-" + suffix,
	}
}

func maximumVoiceReviewResult(t *testing.T) *VoiceReviewResult {
	t.Helper()
	result := &VoiceReviewResult{
		OverallScore: 100,
		Summary:      strings.Repeat("s", maxVoiceReviewSummaryUTF8Bytes),
		Conclusions: make(
			[]VoiceReviewConclusion,
			maxVoiceReviewConclusions,
		),
	}
	for index := range result.Conclusions {
		result.Conclusions[index] = VoiceReviewConclusion{
			Key: fmt.Sprintf("%02d", index) +
				strings.Repeat("k", maxVoiceReviewLabelUTF8Bytes-2),
			Category: strings.Repeat(
				"c",
				maxVoiceReviewLabelUTF8Bytes,
			),
			Message:    strings.Repeat("m", 700),
			Suggestion: strings.Repeat("s", 300),
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal Voice Review Result fixture: %v", err)
	}
	remaining := maxVoiceReviewResultJSONBytes - len(payload)
	last := &result.Conclusions[len(result.Conclusions)-1]
	if remaining < 0 ||
		len(last.Suggestion)+remaining > maxVoiceReviewTextUTF8Bytes {
		t.Fatalf(
			"Voice Review fixture cannot reach budget: bytes=%d remaining=%d",
			len(payload),
			remaining,
		)
	}
	last.Suggestion += strings.Repeat("x", remaining)
	return result
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

func reviewHistoryKeyBefore(
	createdAt time.Time,
	reviewID string,
	boundaryCreatedAt time.Time,
	boundaryReviewID string,
) bool {
	return createdAt.Before(boundaryCreatedAt) ||
		(createdAt.Equal(boundaryCreatedAt) && reviewID < boundaryReviewID)
}

var _ VoiceConversationPort = (*conversation.VoiceRoundService)(nil)

func newVoiceSessionTestApplication(
	t *testing.T,
	conversations *agentVoiceConversation,
	practice *agentVoicePractice,
	reviews *agentVoiceReview,
	orchestrator *VoiceRoundOrchestrator,
) *VoiceSessionApplication {
	t.Helper()
	return newVoiceSessionTestApplicationWithReader(
		t,
		conversations,
		practice,
		orchestrator,
		voiceSessionTestReviews{reviews: reviews},
	)
}

func newVoiceSessionTestApplicationWithReader(
	t *testing.T,
	conversations *agentVoiceConversation,
	practice *agentVoicePractice,
	orchestrator *VoiceRoundOrchestrator,
	reader VoiceReviewReader,
) *VoiceSessionApplication {
	t.Helper()
	application, err := NewVoiceSessionApplication(
		&voiceSessionTestSessions{practice: practice},
		voiceSessionTestQuestions{},
		voiceSessionTestCheckpoints{conversations: conversations},
		orchestrator,
		reader,
		voiceSessionTestMatters{},
	)
	if err != nil {
		t.Fatalf("new Voice Session application: %v", err)
	}
	return application
}

type voiceSessionTestSessions struct {
	practice *agentVoicePractice
}

type fixedVoiceSessionPort struct {
	session VoicePracticeSession
}

func (port fixedVoiceSessionPort) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

func (port fixedVoiceSessionPort) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (VoicePracticeSession, error) {
	return port.session, nil
}

type fixedVoiceCheckpoint struct {
	turn conversation.ConfirmedVoiceTurn
}

func (checkpoint fixedVoiceCheckpoint) LatestTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	return checkpoint.turn, true, nil
}

func (sessions *voiceSessionTestSessions) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByThread(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (VoicePracticeSession, error) {
	return sessions.current(), nil
}

func (sessions *voiceSessionTestSessions) current() VoicePracticeSession {
	sessions.practice.mu.Lock()
	defer sessions.practice.mu.Unlock()
	effective := sessions.practice.effectiveTurns
	return VoicePracticeSession{
		ID:            "session-1",
		PlanID:        "plan-1",
		ThreadID:      "thread-1",
		MatterID:      "matter-1",
		ScenarioType:  "INTERVIEW",
		ScenarioModel: "PROJECT_EXPERIENCE_DEEP_DIVE",
		PromptModel: VoiceScenarioPrompt{
			PublicSceneBrief: "Discuss one project.",
			PracticeGoal:     "Explain decisions clearly.",
			UserRole:         "Candidate",
			AIRole:           "Technical interviewer",
			PersonaSummary:   "Professional and concise",
			FocusAreas:       []string{"clarity"},
			TurnBlueprints:   []string{"Ask about the project"},
		},
		SessionVersion: effective + 1,
		EffectiveTurns: effective,
		TurnLimit:      3,
		Completed:      effective == 3,
		Status: func() string {
			if effective == 3 {
				return "completed"
			}
			return "in_progress"
		}(),
		InterviewerParticipantID: "participant-interviewer",
		CandidateParticipantID:   "participant-a",
	}
}

type voiceSessionTestQuestions struct{}

func (voiceSessionTestQuestions) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session VoicePracticeSession,
	sequence int,
) (conversation.VoiceQuestion, error) {
	return conversation.VoiceQuestion{
		ID:                      "question-next",
		SessionID:               session.ID,
		Text:                    "What happened next?",
		SpeakerParticipantID:    session.InterviewerParticipantID,
		AddresseeParticipantIDs: []string{session.CandidateParticipantID},
	}, nil
}

func (voiceSessionTestQuestions) GetQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	questionID string,
) (conversation.VoiceQuestion, error) {
	return conversation.VoiceQuestion{
		ID:        questionID,
		SessionID: "session-1",
		Text:      "What happened next?",
	}, nil
}

type voiceSessionTestCheckpoints struct {
	conversations *agentVoiceConversation
}

func (checkpoints voiceSessionTestCheckpoints) LatestTurn(
	_ context.Context,
	_ requestcontext.Actor,
	sessionID string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	checkpoints.conversations.mu.Lock()
	defer checkpoints.conversations.mu.Unlock()
	var latest conversation.ConfirmedVoiceTurn
	for _, turn := range checkpoints.conversations.turns {
		if turn.SessionID == sessionID &&
			(latest.ID == "" ||
				turn.EffectiveTurns > latest.EffectiveTurns) {
			latest = turn
		}
	}
	return latest, latest.ID != "", nil
}

type voiceSessionTestReviews struct {
	reviews *agentVoiceReview
	history []VoiceSessionReview
}

func (reader voiceSessionTestReviews) GetReview(
	_ context.Context,
	_ requestcontext.Actor,
	reviewID string,
) (VoiceSessionReview, error) {
	reader.reviews.mu.Lock()
	defer reader.reviews.mu.Unlock()
	for _, item := range reader.reviews.bySession {
		if item.ID == reviewID {
			now := time.Unix(2, 0).UTC()
			return VoiceSessionReview{
				ID:                    item.ID,
				SessionID:             item.SessionID,
				SourceTurnID:          item.SourceTurnID,
				Status:                "completed",
				ImplementationVersion: "review-v1",
				SourceTurnVersion:     "conversation-turn:evidence-v1",
				Result: &VoiceReviewResult{
					OverallScore: 80,
					Summary:      "Clear answer.",
					Conclusions: []VoiceReviewConclusion{{
						Key:      "overall",
						Category: "fluency",
						Message:  "Clear.",
					}},
				},
				CreatedAt:   now,
				UpdatedAt:   now,
				CompletedAt: &now,
			}, nil
		}
	}
	return VoiceSessionReview{}, ErrNotFound
}

func (reader voiceSessionTestReviews) ListReviews(
	_ context.Context,
	_ requestcontext.Actor,
	query VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
	items := make([]VoiceSessionReview, 0, query.Limit)
	for _, item := range reader.history {
		if query.Before != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			continue
		}
		items = append(items, item)
		if len(items) == query.Limit {
			break
		}
	}
	page := VoiceReviewHistoryPage{Items: items}
	consumed := 0
	for _, item := range reader.history {
		if query.Before == nil ||
			reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			consumed++
		}
	}
	if consumed > len(items) && len(items) > 0 {
		last := items[len(items)-1]
		page.Next = &VoiceReviewHistoryCursor{
			CreatedAt: last.CreatedAt,
			ReviewID:  last.ID,
		}
	}
	return page, nil
}

type fixedVoiceReviewPageReader struct {
	page VoiceReviewHistoryPage
}

func (reader fixedVoiceReviewPageReader) GetReview(
	context.Context,
	requestcontext.Actor,
	string,
) (VoiceSessionReview, error) {
	return VoiceSessionReview{}, ErrNotFound
}

func (reader fixedVoiceReviewPageReader) ListReviews(
	context.Context,
	requestcontext.Actor,
	VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
	return reader.page, nil
}

type voiceSessionTestMatters struct{}

func (voiceSessionTestMatters) ReadOwned(
	_ context.Context,
	actor requestcontext.Actor,
	matterID string,
) (matter.Matter, error) {
	if actor.UserID != "user-a" || matterID != "matter-1" {
		return matter.Matter{}, matter.ErrNotFound
	}
	now := time.Unix(1, 0).UTC()
	return matter.Matter{
		ID:        matterID,
		OwnerID:   actor.UserID,
		Title:     "Customer renewal",
		Status:    matter.StatusActive,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
