package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Session is the voice-practice view of a frozen Practice Session. It carries
// only the immutable routing data needed to create Conversation
// Questions and the authoritative progress returned by Practice.
type Session struct {
	ID                       string
	PlanID                   string
	SceneID                  string
	SceneVersion             int
	SceneFamily              string
	SceneModel               string
	Prompt                   scene.ScenePrompt
	PreviousUserResponse     string
	PreviousQuestion         string
	SessionVersion           int
	EffectiveTurns           int
	TurnLimit                int
	MaxFollowUpsPerQuestion  int
	Completed                bool
	Status                   string
	FacilitatorParticipantID string
	LearnerParticipantID     string
}

type SessionPort interface {
	Start(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (Session, error)
	GetByID(
		context.Context,
		requestcontext.Actor,
		string,
	) (Session, error)
}

type QuestionPort interface {
	EnsureQuestion(
		context.Context,
		requestcontext.Actor,
		Session,
		int,
	) (practice.Question, error)
	GetQuestion(
		context.Context,
		requestcontext.Actor,
		string,
	) (practice.Question, error)
}

type CheckpointPort interface {
	LatestTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (practice.Turn, bool, error)
	ListTurnHistory(
		context.Context,
		requestcontext.Actor,
		string,
	) ([]TurnExchange, error)
}

type TurnExchange struct {
	Question practice.Question
	Turn     practice.Turn
}

type SessionState struct {
	Session     Session
	Question    *practice.Question
	Turn        *practice.Turn
	TurnHistory []TurnExchange
}

type SessionApplication struct {
	sessions     SessionPort
	questions    QuestionPort
	checkpoints  CheckpointPort
	orchestrator *RoundOrchestrator
}

func NewSessionApplication(
	sessions SessionPort,
	questions QuestionPort,
	checkpoints CheckpointPort,
	orchestrator *RoundOrchestrator,
) (*SessionApplication, error) {
	if sessions == nil || questions == nil || checkpoints == nil ||
		orchestrator == nil {
		return nil, errors.New("practice voice: session dependency is required")
	}
	return &SessionApplication{
		sessions:     sessions,
		questions:    questions,
		checkpoints:  checkpoints,
		orchestrator: orchestrator,
	}, nil
}

func (application *SessionApplication) Start(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
) (SessionState, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return SessionState{}, ErrInvalidRequest
	}
	session, err := application.sessions.Start(
		ctx,
		actor,
		sessionID,
		idempotencyKey,
	)
	if err != nil {
		return SessionState{}, err
	}
	if session.ID != sessionID {
		return SessionState{}, ErrInvalidContext
	}
	return application.state(ctx, actor, session)
}

func (application *SessionApplication) Resume(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (SessionState, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(sessionID) == "" {
		return SessionState{}, ErrInvalidRequest
	}
	session, err := application.sessions.GetByID(ctx, actor, sessionID)
	if err != nil {
		return SessionState{}, err
	}
	if session.ID != sessionID {
		return SessionState{}, ErrInvalidContext
	}
	return application.state(ctx, actor, session)
}

func (application *SessionApplication) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.TranscribeVoiceCommand,
) (practiceinput.TranscriptionCandidate, error) {
	return application.orchestrator.Transcribe(ctx, actor, command)
}

func (application *SessionApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.ConfirmVoiceTurnCommand,
) (SessionState, error) {
	turn, err := application.orchestrator.Confirm(ctx, actor, command)
	if err != nil {
		return SessionState{}, err
	}
	session, err := application.sessions.GetByID(ctx, actor, turn.SessionID)
	if err != nil {
		return SessionState{}, err
	}
	state, err := application.state(ctx, actor, session)
	if err != nil {
		return SessionState{}, err
	}
	state.Turn = &turn
	return state, nil
}

func (application *SessionApplication) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.SubmitTextAnswerCommand,
) (SessionState, error) {
	turn, err := application.orchestrator.SubmitText(ctx, actor, command)
	if err != nil {
		return SessionState{}, err
	}
	session, err := application.sessions.GetByID(ctx, actor, turn.SessionID)
	if err != nil {
		return SessionState{}, err
	}
	state, err := application.state(ctx, actor, session)
	if err != nil {
		return SessionState{}, err
	}
	state.Turn = &turn
	return state, nil
}

func (application *SessionApplication) QuestionSpeech(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (practiceinput.QuestionSpeech, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(questionID) == "" {
		return practiceinput.QuestionSpeech{}, ErrInvalidRequest
	}
	question, err := application.questions.GetQuestion(
		ctx,
		actor,
		questionID,
	)
	if err != nil {
		return practiceinput.QuestionSpeech{}, err
	}
	if question.ID != questionID || strings.TrimSpace(question.Content) == "" {
		return practiceinput.QuestionSpeech{}, ErrInvalidContext
	}
	return application.orchestrator.SynthesizeQuestion(ctx, question.Content)
}

func (application *SessionApplication) state(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
) (SessionState, error) {
	if session.ID == "" ||
		session.PlanID == "" ||
		session.SceneID == "" ||
		session.SceneVersion < 1 ||
		!validVoiceScenePrompt(session) ||
		session.SessionVersion < 1 ||
		session.TurnLimit < 1 ||
		session.TurnLimit > 14 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > session.TurnLimit ||
		!validVoiceSessionLifecycle(session) ||
		session.FacilitatorParticipantID == "" ||
		session.LearnerParticipantID == "" ||
		session.FacilitatorParticipantID == session.LearnerParticipantID {
		return SessionState{}, ErrInvalidContext
	}
	state := SessionState{Session: session}
	if session.Status == "paused" || session.Status == "ended_early" {
		history, err := application.restoreTurnHistory(
			ctx,
			actor,
			session,
		)
		if err != nil {
			return SessionState{}, err
		}
		state.TurnHistory = history
		return state, nil
	}
	latest, found, err := application.checkpoints.LatestTurn(
		ctx,
		actor,
		session.ID,
	)
	if err != nil {
		return SessionState{}, err
	}
	if found {
		if latest.SessionID != session.ID {
			return SessionState{}, ErrInvalidContext
		}
		state.Turn = &latest
	}
	history, err := application.restoreTurnHistory(ctx, actor, state.Session)
	if err != nil {
		return SessionState{}, err
	}
	for index := range history {
		history[index].Turn, err =
			application.orchestrator.attachTurnFeedback(
				ctx,
				actor,
				history[index].Turn,
			)
		if err != nil {
			return SessionState{}, err
		}
	}
	state.TurnHistory = history
	if len(history) > 0 {
		historyLatest := history[len(history)-1].Turn
		if state.Turn == nil || historyLatest.ID != state.Turn.ID {
			return SessionState{}, ErrInvalidContext
		}
		state.Turn = &historyLatest
	} else if state.Turn != nil {
		enriched, feedbackErr := application.orchestrator.attachTurnFeedback(
			ctx,
			actor,
			*state.Turn,
		)
		if feedbackErr != nil {
			return SessionState{}, feedbackErr
		}
		state.Turn = &enriched
	}
	if state.Turn != nil &&
		(state.Turn.EffectiveTurns != state.Session.EffectiveTurns ||
			state.Turn.SessionCompleted != state.Session.Completed) {
		return SessionState{}, ErrInvalidContext
	}
	if state.Session.Completed {
		if state.Turn == nil {
			return SessionState{}, ErrInvalidContext
		}
		return state, nil
	}
	if state.Turn != nil {
		state.Session.PreviousUserResponse = state.Turn.AnswerText
		if len(history) > 0 {
			state.Session.PreviousQuestion = history[len(history)-1].Question.Content
		}
	}
	nextQuestionSequence := len(history) + 1
	if len(history) == 0 && state.Session.EffectiveTurns > 0 {
		nextQuestionSequence = state.Session.EffectiveTurns + 1
	}
	question, err := application.questions.EnsureQuestion(
		ctx,
		actor,
		state.Session,
		nextQuestionSequence,
	)
	if err != nil {
		return SessionState{}, err
	}
	state.Question = &question
	if state.Question == nil ||
		question.SessionID != session.ID ||
		strings.TrimSpace(question.Content) == "" ||
		question.SpeakerParticipantID !=
			session.FacilitatorParticipantID ||
		len(question.AddresseeParticipantIDs) != 1 ||
		question.AddresseeParticipantIDs[0] !=
			session.LearnerParticipantID {
		return SessionState{}, ErrInvalidContext
	}
	return state, nil
}

func (application *SessionApplication) restoreTurnHistory(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
) ([]TurnExchange, error) {
	history, err := application.checkpoints.ListTurnHistory(
		ctx,
		actor,
		session.ID,
	)
	if err != nil {
		return nil, err
	}
	effectiveTurns := 0
	primaryQuestionIDs := make(map[string]struct{})
	for index, exchange := range history {
		if exchange.Turn.CountsTowardTurnLimit {
			effectiveTurns++
		}
		if exchange.Question.SessionID != session.ID ||
			exchange.Turn.SessionID != session.ID ||
			exchange.Question.ID != exchange.Turn.QuestionID ||
			exchange.Turn.EffectiveTurns != effectiveTurns ||
			(exchange.Question.Type == "PRIMARY") !=
				exchange.Turn.CountsTowardTurnLimit ||
			(exchange.Question.Type == "PRIMARY" &&
				exchange.Question.ParentQuestionID != "") ||
			(exchange.Question.Type == "FOLLOW_UP" &&
				exchange.Question.ParentQuestionID == "") ||
			(exchange.Question.Type != "PRIMARY" &&
				exchange.Question.Type != "FOLLOW_UP") ||
			(exchange.Turn.SessionCompleted !=
				(effectiveTurns == session.EffectiveTurns &&
					index == len(history)-1 &&
					session.Completed)) {
			return nil, ErrInvalidContext
		}
		if exchange.Question.Type == "FOLLOW_UP" {
			if _, found := primaryQuestionIDs[exchange.Question.ParentQuestionID]; !found {
				return nil, ErrInvalidContext
			}
		} else {
			primaryQuestionIDs[exchange.Question.ID] = struct{}{}
		}
	}
	if effectiveTurns != session.EffectiveTurns {
		return nil, ErrInvalidContext
	}
	return history, nil
}

func validVoiceScenePrompt(session Session) bool {
	prompt := session.Prompt
	return strings.TrimSpace(session.SceneFamily) != "" &&
		strings.TrimSpace(session.SceneModel) != "" &&
		strings.TrimSpace(prompt.PublicSceneBrief) != "" &&
		strings.TrimSpace(prompt.PracticeGoal) != "" &&
		strings.TrimSpace(prompt.UserRole) != "" &&
		strings.TrimSpace(prompt.AIRole) != "" &&
		strings.TrimSpace(prompt.PersonaSummary) != "" &&
		len(prompt.FocusAreas) > 0 &&
		len(prompt.TurnBlueprints) > 0
}

func validVoiceSessionLifecycle(session Session) bool {
	switch session.Status {
	case "in_progress":
		return !session.Completed &&
			session.EffectiveTurns < session.TurnLimit
	case "paused":
		return !session.Completed &&
			session.EffectiveTurns < session.TurnLimit
	case "completed":
		return session.Completed &&
			session.EffectiveTurns > 0 &&
			session.EffectiveTurns <= session.TurnLimit
	case "ended_early":
		return !session.Completed &&
			session.EffectiveTurns < session.TurnLimit
	default:
		return false
	}
}
