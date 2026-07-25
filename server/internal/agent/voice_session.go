package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// VoicePracticeSession is the Agent application view of Practice state. It
// carries only the immutable routing data needed to create Conversation
// Questions and the authoritative progress returned by Practice.
type VoicePracticeSession struct {
	ID                       string
	PlanID                   string
	ThreadID                 string
	MatterID                 string
	MatterTitle              string
	SessionVersion           int
	EffectiveTurns           int
	TurnLimit                int
	Completed                bool
	InterviewerParticipantID string
	CandidateParticipantID   string
}

type VoiceSessionPort interface {
	Start(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		string,
	) (VoicePracticeSession, error)
	GetByThread(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (VoicePracticeSession, error)
	GetByID(
		context.Context,
		requestcontext.Actor,
		string,
	) (VoicePracticeSession, error)
}

type VoiceQuestionPort interface {
	EnsureQuestion(
		context.Context,
		requestcontext.Actor,
		VoicePracticeSession,
		int,
	) (conversation.VoiceQuestion, error)
	GetQuestion(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.VoiceQuestion, error)
}

type VoiceCheckpointPort interface {
	LatestTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.ConfirmedVoiceTurn, bool, error)
}

type VoiceReviewReader interface {
	GetReview(
		context.Context,
		requestcontext.Actor,
		string,
	) (VoiceSessionReview, error)
}

// VoiceSessionReview is the Agent application view of Review. Review owns the
// resource and maps its domain model into this stable response boundary.
type VoiceSessionReview struct {
	ID                    string
	SessionID             string
	Status                string
	ImplementationVersion string
	SourceTurnID          string
	SourceTurnVersion     string
	Result                *VoiceReviewResult
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

type VoiceReviewResult struct {
	OverallScore int                     `json:"overall_score"`
	Summary      string                  `json:"summary"`
	Conclusions  []VoiceReviewConclusion `json:"conclusions"`
}

type VoiceReviewConclusion struct {
	Key        string `json:"key"`
	Category   string `json:"category"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type VoiceSessionState struct {
	Session  VoicePracticeSession
	Matter   matter.Matter
	Question *conversation.VoiceQuestion
	Turn     *conversation.ConfirmedVoiceTurn
	Review   *VoiceSessionReview
}

type VoiceSessionApplication struct {
	sessions     VoiceSessionPort
	questions    VoiceQuestionPort
	checkpoints  VoiceCheckpointPort
	orchestrator *VoiceRoundOrchestrator
	reviews      VoiceReviewReader
	matters      matter.Reader
}

func NewVoiceSessionApplication(
	sessions VoiceSessionPort,
	questions VoiceQuestionPort,
	checkpoints VoiceCheckpointPort,
	orchestrator *VoiceRoundOrchestrator,
	reviews VoiceReviewReader,
	matters matter.Reader,
) (*VoiceSessionApplication, error) {
	if sessions == nil || questions == nil || checkpoints == nil ||
		orchestrator == nil || reviews == nil || matters == nil {
		return nil, errors.New("agent: voice session dependency is required")
	}
	return &VoiceSessionApplication{
		sessions:     sessions,
		questions:    questions,
		checkpoints:  checkpoints,
		orchestrator: orchestrator,
		reviews:      reviews,
		matters:      matters,
	}, nil
}

func (application *VoiceSessionApplication) Start(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
	idempotencyKey string,
) (VoiceSessionState, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(threadID) == "" ||
		strings.TrimSpace(matterID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return VoiceSessionState{}, ErrInvalidRequest
	}
	session, err := application.sessions.Start(
		ctx,
		actor,
		threadID,
		matterID,
		idempotencyKey,
	)
	if err != nil {
		return VoiceSessionState{}, err
	}
	return application.state(ctx, actor, session)
}

func (application *VoiceSessionApplication) Resume(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
) (VoiceSessionState, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(threadID) == "" {
		return VoiceSessionState{}, ErrInvalidRequest
	}
	session, err := application.sessions.GetByThread(
		ctx,
		actor,
		threadID,
		matterID,
	)
	if err != nil {
		return VoiceSessionState{}, err
	}
	return application.state(ctx, actor, session)
}

func (application *VoiceSessionApplication) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	return application.orchestrator.Transcribe(ctx, actor, command)
}

func (application *VoiceSessionApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (VoiceSessionState, error) {
	turn, err := application.orchestrator.Confirm(ctx, actor, command)
	if err != nil {
		return VoiceSessionState{}, err
	}
	session, err := application.sessions.GetByID(ctx, actor, turn.SessionID)
	if err != nil {
		return VoiceSessionState{}, err
	}
	state, err := application.state(ctx, actor, session)
	if err != nil {
		return VoiceSessionState{}, err
	}
	state.Turn = &turn
	return state, nil
}

func (application *VoiceSessionApplication) QuestionSpeech(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (conversation.QuestionSpeech, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(questionID) == "" {
		return conversation.QuestionSpeech{}, ErrInvalidRequest
	}
	question, err := application.questions.GetQuestion(
		ctx,
		actor,
		questionID,
	)
	if err != nil {
		return conversation.QuestionSpeech{}, err
	}
	if question.ID != questionID || strings.TrimSpace(question.Text) == "" {
		return conversation.QuestionSpeech{}, ErrInvalidContext
	}
	return application.orchestrator.SynthesizeQuestion(ctx, question.Text)
}

func (application *VoiceSessionApplication) GetReview(
	ctx context.Context,
	actor requestcontext.Actor,
	reviewID string,
) (VoiceSessionReview, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(reviewID) == "" {
		return VoiceSessionReview{}, ErrInvalidRequest
	}
	return application.reviews.GetReview(ctx, actor, reviewID)
}

func (application *VoiceSessionApplication) state(
	ctx context.Context,
	actor requestcontext.Actor,
	session VoicePracticeSession,
) (VoiceSessionState, error) {
	if session.ID == "" || session.TurnLimit <= 0 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > session.TurnLimit ||
		session.Completed != (session.EffectiveTurns == session.TurnLimit) {
		return VoiceSessionState{}, ErrInvalidContext
	}
	state := VoiceSessionState{Session: session}
	currentMatter, err := application.matters.ReadOwned(
		ctx,
		actor,
		session.MatterID,
	)
	if err != nil || currentMatter.ID != session.MatterID {
		return VoiceSessionState{}, ErrNotFound
	}
	state.Matter = currentMatter
	state.Session.MatterTitle = currentMatter.Title
	latest, found, err := application.checkpoints.LatestTurn(
		ctx,
		actor,
		session.ID,
	)
	if err != nil {
		return VoiceSessionState{}, err
	}
	if found {
		state.Turn = &latest
	}
	if found && (latest.EffectiveTurns == 0 ||
		(latest.SessionCompleted && latest.ReviewID == "")) {
		recovered, recoveryErr := application.orchestrator.Confirm(
			ctx,
			actor,
			conversation.ConfirmVoiceTurnCommand{
				CandidateID: latest.CandidateID,
				IdempotencyKey: "voice-recovery:" +
					latest.CandidateID,
			},
		)
		session, err = application.sessions.GetByID(ctx, actor, session.ID)
		if err != nil {
			return VoiceSessionState{}, err
		}
		state.Session = session
		state.Session.MatterTitle = currentMatter.Title
		latest, found, err = application.checkpoints.LatestTurn(
			ctx,
			actor,
			session.ID,
		)
		if err != nil {
			return VoiceSessionState{}, err
		}
		if found {
			state.Turn = &latest
		} else if recoveryErr != nil {
			return VoiceSessionState{}, recoveryErr
		} else {
			state.Turn = &recovered
		}
		if recoveryErr != nil &&
			(state.Turn == nil || state.Turn.ReviewID == "") {
			return VoiceSessionState{}, recoveryErr
		}
	}
	if state.Turn != nil && state.Turn.ReviewID != "" {
		formalReview, reviewErr := application.reviews.GetReview(
			ctx,
			actor,
			state.Turn.ReviewID,
		)
		if reviewErr != nil {
			return VoiceSessionState{}, reviewErr
		}
		state.Review = &formalReview
	}
	if state.Turn != nil &&
		(state.Turn.EffectiveTurns != state.Session.EffectiveTurns ||
			state.Turn.SessionCompleted != state.Session.Completed) {
		return VoiceSessionState{}, ErrInvalidContext
	}
	if state.Session.Completed {
		if state.Turn == nil {
			return VoiceSessionState{}, ErrInvalidContext
		}
		return state, nil
	}
	question, err := application.questions.EnsureQuestion(
		ctx,
		actor,
		state.Session,
		state.Session.EffectiveTurns+1,
	)
	if err != nil {
		return VoiceSessionState{}, err
	}
	state.Question = &question
	if state.Question == nil {
		return VoiceSessionState{}, ErrInvalidContext
	}
	return state, nil
}
