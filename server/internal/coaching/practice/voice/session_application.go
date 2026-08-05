package voice

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// Session is the voice-practice view of a frozen Practice Session. It carries
// only the immutable routing data needed to create voice-practice
// Questions and the authoritative progress returned by Practice.
type Session struct {
	ID                         string
	PlanID                     string
	SceneID                    string
	SceneVersion               int
	SceneFamily                string
	SceneModel                 string
	TurnPolicyRef              string
	Prompt                     practice.ScenePrompt
	PreviousUserResponse       string
	PreviousQuestion           string
	SessionVersion             int
	EffectiveTurns             int
	TurnLimit                  int
	MaxFollowUpsPerQuestion    int
	QuestionTranslationAllowed bool
	Completed                  bool
	Status                     string
	FacilitatorParticipantID   string
	LearnerParticipantID       string
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

type QuestionTranslation struct {
	QuestionID     string
	TargetLanguage string
	Content        string
}

type SessionApplication struct {
	sessions     SessionPort
	questions    QuestionPort
	checkpoints  CheckpointPort
	orchestrator *RoundOrchestrator
	translator   QuestionTranslator
	tips         QuestionTipPort
}

func NewSessionApplication(
	sessions SessionPort,
	questions QuestionPort,
	checkpoints CheckpointPort,
	orchestrator *RoundOrchestrator,
	translators ...QuestionTranslator,
) (*SessionApplication, error) {
	if sessions == nil || questions == nil || checkpoints == nil ||
		orchestrator == nil || len(translators) > 1 ||
		(len(translators) == 1 && translators[0] == nil) {
		return nil, errors.New("practice voice: session dependency is required")
	}
	var translator QuestionTranslator
	if len(translators) == 1 {
		translator = translators[0]
	}
	return &SessionApplication{
		sessions:     sessions,
		questions:    questions,
		checkpoints:  checkpoints,
		orchestrator: orchestrator,
		translator:   translator,
	}, nil
}

func (application *SessionApplication) QuestionTranslation(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (QuestionTranslation, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(questionID) == "" {
		return QuestionTranslation{}, ErrInvalidRequest
	}
	if application.translator == nil {
		return QuestionTranslation{}, ErrInvalidContext
	}
	question, err := application.questions.GetQuestion(ctx, actor, questionID)
	if err != nil {
		return QuestionTranslation{}, err
	}
	if question.ID != questionID || strings.TrimSpace(question.SessionID) == "" ||
		strings.TrimSpace(question.Content) == "" {
		return QuestionTranslation{}, ErrInvalidContext
	}
	session, err := application.sessions.GetByID(ctx, actor, question.SessionID)
	if err != nil {
		return QuestionTranslation{}, err
	}
	if session.ID != question.SessionID || !session.QuestionTranslationAllowed {
		return QuestionTranslation{}, ErrInvalidContext
	}
	content, err := application.translator.TranslateQuestion(
		ctx,
		QuestionTranslationRequest{Question: question.Content},
	)
	if err != nil {
		return QuestionTranslation{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 2000 {
		return QuestionTranslation{}, ErrInvalidContext
	}
	return QuestionTranslation{
		QuestionID:     question.ID,
		TargetLanguage: "zh-CN",
		Content:        content,
	}, nil
}

func (application *SessionApplication) EnsureQuestionTip(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	questionID string,
	idempotencyKey string,
) (QuestionTipResult, error) {
	if application.tips == nil || validateVoiceActor(ctx, actor) != nil ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(questionID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return QuestionTipResult{}, ErrInvalidRequest
	}
	session, err := application.sessions.GetByID(ctx, actor, sessionID)
	if err != nil {
		return QuestionTipResult{}, err
	}
	if session.ID != sessionID || session.SceneFamily != "INTERVIEW" ||
		session.Completed || session.Status == "paused" ||
		session.Status == "ended_early" {
		return QuestionTipResult{}, ErrInvalidContext
	}
	state, err := application.state(ctx, actor, session)
	if err != nil {
		return QuestionTipResult{}, err
	}
	if state.Question == nil || state.Question.ID != questionID {
		return QuestionTipResult{}, ErrInvalidContext
	}
	return application.tips.EnsureQuestionTip(
		ctx,
		actor,
		state.Session,
		*state.Question,
		state.TurnHistory,
		idempotencyKey,
	)
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
	command TranscribeVoiceCommand,
) (TranscriptionCandidate, error) {
	return application.orchestrator.Transcribe(ctx, actor, command)
}

func (application *SessionApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmVoiceTurnCommand,
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
	command SubmitTextAnswerCommand,
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
) (QuestionSpeech, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(questionID) == "" {
		return QuestionSpeech{}, ErrInvalidRequest
	}
	question, err := application.questions.GetQuestion(
		ctx,
		actor,
		questionID,
	)
	if err != nil {
		return QuestionSpeech{}, err
	}
	if question.ID != questionID || strings.TrimSpace(question.Content) == "" {
		return QuestionSpeech{}, ErrInvalidContext
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
		!validVoiceTurnPolicy(session.TurnPolicyRef) ||
		!validVoiceScenePrompt(session) ||
		session.SessionVersion < 1 ||
		session.TurnLimit < 1 ||
		session.TurnLimit > 24 ||
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

func validVoiceTurnPolicy(reference string) bool {
	_, err := practice.ResolveTurnPolicy(reference)
	return err == nil
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
