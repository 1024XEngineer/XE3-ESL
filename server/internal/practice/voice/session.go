package voice

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxVoiceReviewMetadataUTF8Bytes = 128
	maxVoiceReviewResultJSONBytes   = 12 * 1024
	maxVoiceReviewSummaryUTF8Bytes  = 2048
	maxVoiceReviewConclusions       = 8
	maxVoiceReviewLabelUTF8Bytes    = 64
	maxVoiceReviewTextUTF8Bytes     = 2048
	ieltsSpeakingFullMockModel      = "IELTS_SPEAKING_FULL_MOCK"
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
	) (conversation.VoiceQuestion, error)
	GetQuestion(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.VoiceQuestion, error)
}

type CheckpointPort interface {
	LatestTurn(
		context.Context,
		requestcontext.Actor,
		string,
	) (conversation.ConfirmedVoiceTurn, bool, error)
	ListTurnHistory(
		context.Context,
		requestcontext.Actor,
		string,
	) ([]TurnExchange, error)
}

type TurnExchange struct {
	Question conversation.VoiceQuestion
	Turn     conversation.ConfirmedVoiceTurn
}

type ReviewReader interface {
	GetReview(
		context.Context,
		requestcontext.Actor,
		string,
	) (SessionReview, error)
	ListReviews(
		context.Context,
		requestcontext.Actor,
		ReviewHistoryQuery,
	) (ReviewHistoryPage, error)
}

// SessionReview is the voice-practice view of Review. Review owns the resource
// and maps its domain model into this stable response boundary.
type SessionReview struct {
	ID                    string
	SessionID             string
	Status                string
	ImplementationVersion string
	SourceTurnID          string
	SourceTurnVersion     string
	EvaluationContextType string
	EvaluationContext     json.RawMessage
	Result                *ReviewResult
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

type ReviewResult struct {
	SummaryEligibility          string               `json:"summary_eligibility,omitempty"`
	OverallScore                int                  `json:"overall_score"`
	OverallScorePresent         bool                 `json:"-"`
	Summary                     string               `json:"summary"`
	Conclusions                 []ReviewConclusion   `json:"conclusions"`
	FeedbackItems               []ReviewFeedbackItem `json:"feedback_items,omitempty"`
	RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs,omitempty"`
	InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons,omitempty"`
}

type ReviewConclusion struct {
	Key          string `json:"key"`
	Category     string `json:"category"`
	Score        int    `json:"score,omitempty"`
	ScorePresent bool   `json:"-"`
	Message      string `json:"message"`
	Suggestion   string `json:"suggestion,omitempty"`
}

func (c ReviewConclusion) MarshalJSON() ([]byte, error) {
	type wireConclusion struct {
		Key        string `json:"key"`
		Category   string `json:"category"`
		Score      *int   `json:"score,omitempty"`
		Message    string `json:"message"`
		Suggestion string `json:"suggestion,omitempty"`
	}
	var score *int
	if c.ScorePresent || c.Score != 0 {
		value := c.Score
		score = &value
	}
	return json.Marshal(wireConclusion{
		Key:        c.Key,
		Category:   c.Category,
		Score:      score,
		Message:    c.Message,
		Suggestion: c.Suggestion,
	})
}

type ReviewFeedbackItem struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func (r ReviewResult) MarshalJSON() ([]byte, error) {
	type wireResult struct {
		SummaryEligibility          string               `json:"summary_eligibility,omitempty"`
		OverallScore                *int                 `json:"overall_score,omitempty"`
		Summary                     string               `json:"summary"`
		Conclusions                 []ReviewConclusion   `json:"conclusions"`
		FeedbackItems               []ReviewFeedbackItem `json:"feedback_items,omitempty"`
		RepracticeSuggestionRefs    []string             `json:"repractice_suggestion_refs,omitempty"`
		InsufficientEvidenceReasons []string             `json:"insufficient_evidence_reasons,omitempty"`
	}
	var score *int
	if r.OverallScorePresent || r.OverallScore != 0 {
		value := r.OverallScore
		score = &value
	}
	return json.Marshal(wireResult{
		SummaryEligibility:          r.SummaryEligibility,
		OverallScore:                score,
		Summary:                     r.Summary,
		Conclusions:                 r.Conclusions,
		FeedbackItems:               r.FeedbackItems,
		RepracticeSuggestionRefs:    r.RepracticeSuggestionRefs,
		InsufficientEvidenceReasons: r.InsufficientEvidenceReasons,
	})
}

type ReviewHistoryCursor struct {
	CreatedAt time.Time
	ReviewID  string
}

type ReviewHistoryQuery struct {
	Limit  int
	Before *ReviewHistoryCursor
}

type ReviewHistoryPage struct {
	Items []SessionReview
	Next  *ReviewHistoryCursor
}

type SessionState struct {
	Session     Session
	Question    *conversation.VoiceQuestion
	Turn        *conversation.ConfirmedVoiceTurn
	TurnHistory []TurnExchange
	Review      *SessionReview
}

type SessionApplication struct {
	sessions     SessionPort
	questions    QuestionPort
	checkpoints  CheckpointPort
	orchestrator *RoundOrchestrator
	reviews      ReviewReader
}

func NewSessionApplication(
	sessions SessionPort,
	questions QuestionPort,
	checkpoints CheckpointPort,
	orchestrator *RoundOrchestrator,
	reviews ReviewReader,
) (*SessionApplication, error) {
	if sessions == nil || questions == nil || checkpoints == nil ||
		orchestrator == nil || reviews == nil {
		return nil, errors.New("practice voice: session dependency is required")
	}
	return &SessionApplication{
		sessions:     sessions,
		questions:    questions,
		checkpoints:  checkpoints,
		orchestrator: orchestrator,
		reviews:      reviews,
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
	return application.state(ctx, actor, session, true)
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
	return application.state(ctx, actor, session, true)
}

func (application *SessionApplication) Transcribe(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.TranscribeVoiceCommand,
) (conversation.TranscriptionCandidate, error) {
	return application.orchestrator.Transcribe(ctx, actor, command)
}

func (application *SessionApplication) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
) (SessionState, error) {
	turn, err := application.orchestrator.Confirm(ctx, actor, command)
	if err != nil {
		return SessionState{}, err
	}
	session, err := application.sessions.GetByID(ctx, actor, turn.SessionID)
	if err != nil {
		return SessionState{}, err
	}
	state, err := application.state(ctx, actor, session, false)
	if err != nil {
		return SessionState{}, err
	}
	state.Turn = &turn
	return state, nil
}

func (application *SessionApplication) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.SubmitTextAnswerCommand,
) (SessionState, error) {
	turn, err := application.orchestrator.SubmitText(ctx, actor, command)
	if err != nil {
		return SessionState{}, err
	}
	session, err := application.sessions.GetByID(ctx, actor, turn.SessionID)
	if err != nil {
		return SessionState{}, err
	}
	state, err := application.state(ctx, actor, session, false)
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

func (application *SessionApplication) GetReview(
	ctx context.Context,
	actor requestcontext.Actor,
	reviewID string,
) (SessionReview, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		strings.TrimSpace(reviewID) == "" {
		return SessionReview{}, ErrInvalidRequest
	}
	item, err := application.reviews.GetReview(ctx, actor, reviewID)
	if err != nil {
		return SessionReview{}, err
	}
	if !validPersistedVoiceSessionReview(item, reviewID) {
		return SessionReview{}, ErrInvalidContext
	}
	return item, nil
}

func (application *SessionApplication) ListReviews(
	ctx context.Context,
	actor requestcontext.Actor,
	query ReviewHistoryQuery,
) (ReviewHistoryPage, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		query.Limit < 1 || query.Limit > 50 ||
		(query.Before != nil &&
			(query.Before.CreatedAt.IsZero() ||
				!validUUID(query.Before.ReviewID))) {
		return ReviewHistoryPage{}, ErrInvalidRequest
	}
	page, err := application.reviews.ListReviews(ctx, actor, query)
	if err != nil {
		return ReviewHistoryPage{}, err
	}
	if len(page.Items) > query.Limit ||
		(page.Next != nil && len(page.Items) != query.Limit) {
		return ReviewHistoryPage{}, ErrInvalidContext
	}
	var previous *SessionReview
	seen := make(map[string]struct{}, len(page.Items))
	for index := range page.Items {
		item := &page.Items[index]
		if !validUUID(item.ID) ||
			!validPersistedVoiceSessionReview(*item, item.ID) ||
			item.Status != "completed" {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
		if _, exists := seen[item.ID]; exists {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
		seen[item.ID] = struct{}{}
		if previous != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				previous.CreatedAt,
				previous.ID,
			) {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
		if query.Before != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
		previous = item
	}
	if page.Next != nil {
		if len(page.Items) == 0 {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
		last := page.Items[len(page.Items)-1]
		if !validUUID(page.Next.ReviewID) ||
			!page.Next.CreatedAt.Equal(last.CreatedAt) ||
			page.Next.ReviewID != last.ID {
			return ReviewHistoryPage{}, ErrInvalidContext
		}
	}
	return page, nil
}

func (application *SessionApplication) state(
	ctx context.Context,
	actor requestcontext.Actor,
	session Session,
	recoverCompletion bool,
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
	if found && (latest.EffectiveTurns == 0 ||
		(latest.SessionCompleted && latest.ReviewID == "" &&
			recoverCompletion)) {
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
			return SessionState{}, err
		}
		state.Session = session
		latest, found, err = application.checkpoints.LatestTurn(
			ctx,
			actor,
			session.ID,
		)
		if err != nil {
			return SessionState{}, err
		}
		if found {
			state.Turn = &latest
		} else if recoveryErr != nil {
			return SessionState{}, recoveryErr
		} else {
			state.Turn = &recovered
		}
		if recoveryErr != nil &&
			(state.Turn == nil || state.Turn.ReviewID == "") {
			return SessionState{}, recoveryErr
		}
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
	if state.Turn != nil && state.Turn.ReviewID != "" {
		formalReview, reviewErr := application.GetReview(
			ctx,
			actor,
			state.Turn.ReviewID,
		)
		if reviewErr != nil {
			return SessionState{}, reviewErr
		}
		if formalReview.SessionID != session.ID ||
			formalReview.SourceTurnID != state.Turn.ID {
			return SessionState{}, ErrInvalidContext
		}
		state.Review = &formalReview
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
			state.Session.PreviousQuestion = history[len(history)-1].Question.Text
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
		strings.TrimSpace(question.Text) == "" ||
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

func validVoiceSessionReview(
	item SessionReview,
	expectedID string,
) bool {
	return validVoiceSessionReviewWithResult(
		item,
		expectedID,
		validVoiceReviewResult,
	)
}

func validPersistedVoiceSessionReview(
	item SessionReview,
	expectedID string,
) bool {
	return validVoiceSessionReviewWithResult(
		item,
		expectedID,
		validPersistedVoiceReviewResult,
	)
}

func validVoiceSessionReviewWithResult(
	item SessionReview,
	expectedID string,
	validResult func(*ReviewResult) bool,
) bool {
	if item.ID != expectedID ||
		!validVoiceReviewMetadata(item.ID) ||
		!validVoiceReviewMetadata(item.SessionID) ||
		!validVoiceReviewMetadata(item.ImplementationVersion) ||
		!validVoiceReviewMetadata(item.SourceTurnID) ||
		!validVoiceReviewMetadata(item.SourceTurnVersion) ||
		!validVoiceSourceTurnVersion(item.SourceTurnVersion) ||
		(item.ImplementationVersion == "qianwen-scenario-review-v2" &&
			(!validEvaluationContextType(item.EvaluationContextType) ||
				!validVoiceEvaluationContext(item.EvaluationContext))) ||
		(item.ImplementationVersion != "qianwen-scenario-review-v2" &&
			((item.EvaluationContextType != "" &&
				!validEvaluationContextType(item.EvaluationContextType)) ||
				(len(item.EvaluationContext) > 0 &&
					!validVoiceEvaluationContext(item.EvaluationContext)))) ||
		item.CreatedAt.IsZero() ||
		item.UpdatedAt.Before(item.CreatedAt) {
		return false
	}
	switch item.Status {
	case "pending", "generating", "failed":
		return item.Result == nil && item.CompletedAt == nil
	case "completed":
	default:
		return false
	}
	if !validResult(item.Result) ||
		item.CompletedAt == nil ||
		item.CompletedAt.Before(item.CreatedAt) {
		return false
	}
	return true
}

func validPersistedVoiceReviewResult(result *ReviewResult) bool {
	if result == nil {
		return false
	}
	if result.SummaryEligibility == "insufficient_evidence" {
		return !result.OverallScorePresent &&
			result.OverallScore == 0 &&
			strings.TrimSpace(result.Summary) != "" &&
			len(result.Conclusions) == 0 &&
			len(result.FeedbackItems) == 0 &&
			len(result.InsufficientEvidenceReasons) > 0
	}
	if result.SummaryEligibility != "" &&
		result.SummaryEligibility != "eligible" &&
		result.SummaryEligibility != "provisional" {
		return false
	}
	if result.OverallScore < 0 ||
		result.OverallScore > 100 ||
		strings.TrimSpace(result.Summary) == "" ||
		len(result.Conclusions) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(result.Conclusions))
	for _, conclusion := range result.Conclusions {
		key := strings.TrimSpace(conclusion.Key)
		if key == "" ||
			key != conclusion.Key ||
			strings.TrimSpace(conclusion.Category) == "" ||
			strings.TrimSpace(conclusion.Message) == "" {
			return false
		}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validVoiceReviewResult(result *ReviewResult) bool {
	if result == nil {
		return false
	}
	if result.SummaryEligibility == "insufficient_evidence" {
		return !result.OverallScorePresent &&
			result.OverallScore == 0 &&
			validVoiceReviewText(
				result.Summary,
				maxVoiceReviewSummaryUTF8Bytes,
			) &&
			len(result.Conclusions) == 0 &&
			len(result.FeedbackItems) == 0 &&
			len(result.InsufficientEvidenceReasons) > 0
	}
	if result.SummaryEligibility != "" &&
		result.SummaryEligibility != "eligible" &&
		result.SummaryEligibility != "provisional" {
		return false
	}
	if result.OverallScore < 0 ||
		result.OverallScore > 100 ||
		!validVoiceReviewText(
			result.Summary,
			maxVoiceReviewSummaryUTF8Bytes,
		) ||
		len(result.Conclusions) == 0 ||
		len(result.Conclusions) > maxVoiceReviewConclusions {
		return false
	}
	seen := make(map[string]struct{}, len(result.Conclusions))
	for _, conclusion := range result.Conclusions {
		key := strings.TrimSpace(conclusion.Key)
		if key == "" ||
			key != conclusion.Key ||
			!validVoiceReviewText(
				conclusion.Key,
				maxVoiceReviewLabelUTF8Bytes,
			) ||
			!validVoiceReviewText(
				conclusion.Category,
				maxVoiceReviewLabelUTF8Bytes,
			) ||
			!validVoiceReviewText(
				conclusion.Message,
				maxVoiceReviewTextUTF8Bytes,
			) ||
			!validOptionalVoiceReviewText(
				conclusion.Suggestion,
				maxVoiceReviewTextUTF8Bytes,
			) {
			return false
		}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	encoded, err := json.Marshal(result)
	return err == nil && len(encoded) <= maxVoiceReviewResultJSONBytes
}

func validEvaluationContextType(value string) bool {
	switch value {
	case "interview.project_deep_dive",
		"ielts.speaking_part2",
		"workplace.progress_risk_update",
		"daily.hotel_checkin_issue",
		"generic.practice":
		return true
	default:
		return false
	}
}

func validVoiceEvaluationContext(value json.RawMessage) bool {
	return len(value) > 0 &&
		len(value) <= maxVoiceReviewResultJSONBytes &&
		json.Valid(value)
}

func validVoiceReviewMetadata(value string) bool {
	return validVoiceReviewText(value, maxVoiceReviewMetadataUTF8Bytes)
}

func validVoiceReviewText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		len(value) <= maximumBytes &&
		strings.TrimSpace(value) != ""
}

func validOptionalVoiceReviewText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		len(value) <= maximumBytes &&
		(value == "" || strings.TrimSpace(value) != "")
}

func validVoiceSourceTurnVersion(value string) bool {
	const prefix = "conversation-turn:evidence-v"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	version, err := strconv.ParseInt(
		strings.TrimPrefix(value, prefix),
		10,
		64,
	)
	return err == nil && version >= 1
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
