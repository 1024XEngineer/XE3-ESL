package interaction

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

func TestSessionTranslationUsesFrozenPolicy(t *testing.T) {
	translator := &questionTranslatorStub{content: "接下来发生了什么？"}
	allowed := sessionFixture()
	allowed.PracticeExperience = "LIFE_AND_TRAVEL"
	allowed.SceneCategory = "LIFE_DAILY"
	application := translationApplication(
		t,
		sessionPortStub{session: allowed},
		translator,
	)

	translation, err := application.QuestionTranslation(
		context.Background(), roundActor(), "question-1",
	)
	if err != nil {
		t.Fatalf("QuestionTranslation: %v", err)
	}
	if translation.QuestionID != "question-1" ||
		translation.TargetLanguage != "zh-CN" ||
		translation.Content != translator.content ||
		translator.calls != 1 ||
		translator.question != "What happened next?" {
		t.Fatalf("translation = %#v, translator = %#v", translation, translator)
	}

	daily := sessionFixture()
	daily.PracticeExperience = "INTERVIEW"
	daily.SceneCategory = "INTERVIEW_PROFESSIONAL"
	daily.QuestionTranslationAllowed = false
	dailyTranslator := &questionTranslatorStub{content: "不应调用"}
	dailyApplication := translationApplication(
		t,
		sessionPortStub{session: daily},
		dailyTranslator,
	)
	_, err = dailyApplication.QuestionTranslation(
		context.Background(), roundActor(), "question-1",
	)
	if !errors.Is(err, ErrInvalidContext) || dailyTranslator.calls != 0 {
		t.Fatalf("daily translation error = %v, calls = %d", err, dailyTranslator.calls)
	}
}

func translationApplication(
	t *testing.T,
	sessions SessionPort,
	translator sharedtranslation.Translator,
) *SessionApplication {
	t.Helper()
	candidate := roundCandidate()
	orchestrator, err := NewRoundOrchestrator(
		&roundVoice{candidate: candidate, turn: roundTurn(candidate)},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	application, err := NewSessionApplication(
		sessions,
		translationQuestionPortStub{},
		checkpointPortStub{},
		orchestrator,
		translator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}
	return application
}

type translationQuestionPortStub struct{ questionPortStub }

func (translationQuestionPortStub) GetQuestion(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Question, error) {
	return practice.Question{
		ID:        "question-1",
		SessionID: "session-1",
		Content:   "What happened next?",
	}, nil
}

type questionTranslatorStub struct {
	content  string
	question string
	calls    int
}

func (stub *questionTranslatorStub) Translate(
	_ context.Context,
	request sharedtranslation.Request,
) (string, error) {
	stub.calls++
	stub.question = request.Text
	return stub.content, nil
}

func TestSessionStartReturnsQuestionFromFrozenSession(t *testing.T) {
	session := sessionFixture()
	candidate := roundCandidate()
	orchestrator, err := NewRoundOrchestrator(
		&roundVoice{candidate: candidate, turn: roundTurn(candidate)},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	application, err := NewSessionApplication(
		sessionPortStub{session: session},
		questionPortStub{},
		checkpointPortStub{},
		orchestrator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Start(
		context.Background(),
		roundActor(),
		session.ID,
		"start-1",
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Session.ID != session.ID || state.Question == nil ||
		state.Question.SessionID != session.ID {
		t.Fatalf("Session state = %#v", state)
	}
}

func TestSessionApplicationStagesAndRestoresDeferredTranscription(t *testing.T) {
	candidate := roundCandidate()
	rounds := &roundVoice{
		candidate: candidate,
		turn:      roundTurn(candidate),
		deferred: TranscriptionReservation{
			ID: "reservation-1", SessionID: candidate.SessionID,
			QuestionID: candidate.QuestionID, Status: TranscriptionReserved,
		},
	}
	orchestrator, err := NewRoundOrchestrator(rounds, roundPractice{})
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	application, err := NewSessionApplication(
		sessionPortStub{session: sessionFixture()},
		questionPortStub{},
		checkpointPortStub{},
		orchestrator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}
	processor := &DeferredTranscriptionProcessor{
		ctx: context.Background(), orchestrator: orchestrator,
		queue:   make(chan deferredTranscriptionRequest, 4),
		pending: make(map[string]struct{}),
	}
	if err := application.EnableDeferredTranscription(processor); err != nil {
		t.Fatalf("EnableDeferredTranscription: %v", err)
	}
	if err := application.EnableDeferredTranscription(processor); err == nil {
		t.Fatal("duplicate deferred processor was accepted")
	}

	submission, err := application.StageDeferredTranscription(
		context.Background(),
		roundActor(),
		TranscribeVoiceCommand{
			SessionID: candidate.SessionID, QuestionID: candidate.QuestionID,
			IdempotencyKey: "part-2-recording",
		},
	)
	if err != nil || submission.ID != rounds.deferred.ID ||
		submission.Status != TranscriptionProcessing || len(processor.queue) != 1 {
		t.Fatalf("submission = %#v, queue = %d, error = %v", submission, len(processor.queue), err)
	}

	rounds.deferred.Status = TranscriptionConfirmed
	completed, err := application.DeferredTranscriptionStatus(
		context.Background(), roundActor(), rounds.deferred.ID,
	)
	if err != nil || completed.Status != TranscriptionCompleted ||
		len(processor.queue) != 1 {
		t.Fatalf("completed = %#v, queue = %d, error = %v", completed, len(processor.queue), err)
	}

	processor.removePending(rounds.deferred.ID)
	rounds.deferred.Status = TranscriptionFailed
	rounds.deferred.AttemptCount = 2
	retrying, err := application.DeferredTranscriptionStatus(
		context.Background(), roundActor(), rounds.deferred.ID,
	)
	if err != nil || retrying.Status != TranscriptionProcessing ||
		len(processor.queue) != 2 {
		t.Fatalf("retrying = %#v, queue = %d, error = %v", retrying, len(processor.queue), err)
	}

	processor.removePending(rounds.deferred.ID)
	rounds.deferred.AttemptCount = MaxDeferredTranscriptionAttempts
	failed, err := application.DeferredTranscriptionStatus(
		context.Background(), roundActor(), rounds.deferred.ID,
	)
	if err != nil || failed.Status != TranscriptionFailed ||
		len(processor.queue) != 2 {
		t.Fatalf("failed = %#v, queue = %d, error = %v", failed, len(processor.queue), err)
	}
}

func TestSessionResumeUsesProgressedUnconfirmedTurnForNextQuestion(t *testing.T) {
	session := sessionFixture()
	session.SessionVersion = 2
	session.EffectiveTurns = 1
	questions := &progressQuestionPortStub{}
	checkpoints := progressedCheckpointPortStub{
		progress: TurnProgress{
			TurnID: "turn-1", SessionID: session.ID, QuestionID: "question-1",
			Sequence: 1, EffectiveTurns: 1, CountsTowardTurnLimit: true,
		},
	}
	candidate := roundCandidate()
	orchestrator, err := NewRoundOrchestrator(
		&roundVoice{candidate: candidate, turn: roundTurn(candidate)},
		roundPractice{},
	)
	if err != nil {
		t.Fatalf("NewRoundOrchestrator: %v", err)
	}
	application, err := NewSessionApplication(
		sessionPortStub{session: session},
		questions,
		checkpoints,
		orchestrator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}

	state, err := application.Resume(context.Background(), roundActor(), session.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if state.Question == nil || state.Question.Sequence != 2 ||
		questions.ensuredSequence != 2 ||
		state.Session.PreviousQuestion != "Part 2 question" ||
		len(state.TurnHistory) != 0 {
		t.Fatalf("restored state = %#v, questions = %#v", state, questions)
	}
}

type sessionPortStub struct{ session Session }

func (p sessionPortStub) Start(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, error) {
	return p.session, nil
}

func (p sessionPortStub) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return p.session, nil
}

type questionPortStub struct{}

func (questionPortStub) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	sequence int,
) (practice.Question, error) {
	return practice.Question{
		ID:                      "question-1",
		SessionID:               session.ID,
		SpeakerParticipantID:    session.FacilitatorParticipantID,
		AddresseeParticipantIDs: []string{session.LearnerParticipantID},
		ObjectiveID:             "objective-1",
		Type:                    "PRIMARY",
		Content:                 "Tell me about yourself.",
		Sequence:                sequence,
	}, nil
}

func (questionPortStub) GetQuestion(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Question, error) {
	return practice.Question{ID: "question-1", Content: "Question"}, nil
}

type checkpointPortStub struct{}

func (checkpointPortStub) LatestTurn(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Turn, bool, error) {
	return practice.Turn{}, false, nil
}

func (checkpointPortStub) ListTurnHistory(
	context.Context,
	requestcontext.Actor,
	string,
) ([]TurnExchange, error) {
	return nil, nil
}

func (checkpointPortStub) LatestProgress(
	context.Context,
	requestcontext.Actor,
	string,
) (TurnProgress, bool, error) {
	return TurnProgress{}, false, nil
}

type progressedCheckpointPortStub struct {
	checkpointPortStub
	progress TurnProgress
}

func (stub progressedCheckpointPortStub) LatestProgress(
	context.Context,
	requestcontext.Actor,
	string,
) (TurnProgress, bool, error) {
	return stub.progress, true, nil
}

type progressQuestionPortStub struct {
	ensuredSequence int
}

func (stub *progressQuestionPortStub) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	sequence int,
) (practice.Question, error) {
	stub.ensuredSequence = sequence
	return practice.Question{
		ID: "question-2", SessionID: session.ID, Sequence: sequence,
		SpeakerParticipantID:    session.FacilitatorParticipantID,
		AddresseeParticipantIDs: []string{session.LearnerParticipantID},
		ObjectiveID:             "objective-1", Type: "PRIMARY", Content: "Part 3 question",
	}, nil
}

func (*progressQuestionPortStub) GetQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	questionID string,
) (practice.Question, error) {
	return practice.Question{
		ID: questionID, SessionID: "session-1", Sequence: 1,
		Content: "Part 2 question",
	}, nil
}

func sessionFixture() Session {
	return Session{
		ID:                 "session-1",
		PlanID:             "plan-1",
		SceneID:            "scene-1",
		SceneVersion:       1,
		PracticeExperience: "INTERVIEW",
		SceneCategory:      "INTERVIEW_PROFESSIONAL",
		PracticeMode:       "FULL_SIMULATION",
		TurnPolicyRef:      practice.InterviewProjectDeepDiveTurnPolicy,
		Prompt: practice.ScenePrompt{
			PublicSceneBrief: "Interview practice",
			PracticeGoal:     "Answer clearly",
			UserRole:         "Candidate",
			AIRole:           "Interviewer",
			PersonaSummary:   "Professional interviewer",
			FocusAreas:       []string{"clarity"},
			TurnBlueprints:   []string{"Ask for an introduction"},
		},
		SessionVersion:             1,
		TurnLimit:                  3,
		CompletionMode:             practice.CompletionModeTurnLimited,
		MaxFollowUpsPerQuestion:    1,
		QuestionTranslationAllowed: true,
		Status:                     "in_progress",
		FacilitatorParticipantID:   "facilitator-1",
		LearnerParticipantID:       "learner-1",
	}
}
