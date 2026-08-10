package voice

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

type sessionPortStub struct{ session Session }

func (p sessionPortStub) PrepareStart(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, bool, error) {
	return p.session, false, nil
}

func TestSessionStartPersistsQuestionBeforeActivation(t *testing.T) {
	starting := sessionFixture()
	starting.Status = "starting"
	starting.SessionVersion = 1
	activated := starting
	activated.Status = "in_progress"
	activated.SessionVersion = 2
	port := &activationSessionPortStub{
		prepared:  starting,
		activated: activated,
	}
	questions := &activationQuestionPortStub{sessionPort: port}
	application := sessionApplicationFixture(t, port, questions)

	state, err := application.Start(
		context.Background(), roundActor(), starting.ID, "start-1",
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !questions.called || !port.committed || port.aborted ||
		state.Session.Status != "in_progress" || state.Question == nil {
		t.Fatalf(
			"questions.called=%t committed=%t aborted=%t state=%#v",
			questions.called, port.committed, port.aborted, state,
		)
	}
}

func TestSessionStartAbortsEmptySessionAfterProviderFailure(t *testing.T) {
	starting := sessionFixture()
	starting.Status = "starting"
	port := &activationSessionPortStub{prepared: starting}
	providerFailure := NewProviderError(
		ProviderOperationQuestionGeneration,
		ProviderErrorUnavailable,
		"request-1",
		errors.New("provider unavailable"),
	)
	application := sessionApplicationFixture(
		t,
		port,
		&activationQuestionPortStub{err: providerFailure},
	)

	_, err := application.Start(
		context.Background(), roundActor(), starting.ID, "start-1",
	)
	var activationFailure *ActivationError
	if !errors.As(err, &activationFailure) ||
		!errors.Is(err, providerFailure) || !port.aborted || port.committed {
		t.Fatalf(
			"error=%v aborted=%t committed=%t",
			err, port.aborted, port.committed,
		)
	}
}

func TestSessionResumeDiscardsLegacyEmptyActivationAfterProviderFailure(
	t *testing.T,
) {
	legacy := sessionFixture()
	legacy.Status = "in_progress"
	legacy.EffectiveTurns = 0
	port := &activationSessionPortStub{prepared: legacy}
	providerFailure := NewProviderError(
		ProviderOperationQuestionGeneration,
		ProviderErrorUnavailable,
		"request-legacy",
		errors.New("provider unavailable"),
	)
	application := sessionApplicationFixture(
		t,
		port,
		&activationQuestionPortStub{err: providerFailure},
	)

	_, err := application.Resume(
		context.Background(), roundActor(), legacy.ID,
	)
	var activationFailure *ActivationError
	if !errors.As(err, &activationFailure) || !port.aborted {
		t.Fatalf("Resume error=%v aborted=%t", err, port.aborted)
	}
}

func sessionApplicationFixture(
	t *testing.T,
	sessions SessionPort,
	questions QuestionPort,
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
		questions,
		checkpointPortStub{},
		orchestrator,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}
	return application
}

type activationSessionPortStub struct {
	prepared  Session
	activated Session
	committed bool
	aborted   bool
}

func (stub *activationSessionPortStub) PrepareStart(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (Session, bool, error) {
	return stub.prepared, false, nil
}

func (stub *activationSessionPortStub) CommitStart(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (Session, error) {
	stub.committed = true
	return stub.activated, nil
}

func (stub *activationSessionPortStub) AbortStart(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	_ string,
) error {
	stub.aborted =
		(session.Status == "starting" || session.Status == "in_progress") &&
			session.EffectiveTurns == 0
	return nil
}

func (stub *activationSessionPortStub) GetByID(
	context.Context,
	requestcontext.Actor,
	string,
) (Session, error) {
	return stub.prepared, nil
}

type activationQuestionPortStub struct {
	sessionPort *activationSessionPortStub
	called      bool
	err         error
}

func (stub *activationQuestionPortStub) EnsureQuestion(
	_ context.Context,
	_ requestcontext.Actor,
	session Session,
	sequence int,
) (practice.Question, error) {
	stub.called = true
	if stub.sessionPort != nil && stub.sessionPort.committed {
		return practice.Question{}, errors.New("question generated after activation")
	}
	if stub.err != nil {
		return practice.Question{}, stub.err
	}
	return questionPortStub{}.EnsureQuestion(
		context.Background(), roundActor(), session, sequence,
	)
}

func (*activationQuestionPortStub) GetQuestion(
	context.Context,
	requestcontext.Actor,
	string,
) (practice.Question, error) {
	return practice.Question{}, ErrNotFound
}

func (p sessionPortStub) CommitStart(
	context.Context,
	requestcontext.Actor,
	string,
	string,
	string,
) (Session, error) {
	return p.session, nil
}

func (sessionPortStub) AbortStart(
	context.Context,
	requestcontext.Actor,
	Session,
	string,
) error {
	return nil
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
