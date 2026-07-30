package voice

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	ieltsSpeakingFullMockModel      = "IELTS_SPEAKING_FULL_MOCK"
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
	ScenarioType             string
	ScenarioModel            string
	PromptModel              VoiceScenarioPrompt
	PreviousUserResponse     string
	SessionVersion           int
	EffectiveTurns           int
	TurnLimit                int
	Completed                bool
	Status                   string
	InterviewerParticipantID string
	CandidateParticipantID   string
}

// VoiceScenarioPrompt is copied from the immutable Practice Session snapshot.
// Voice uses this contract instead of inferring a dialogue from a Matter title.
type VoiceScenarioPrompt struct {
	PublicSceneBrief string
	PracticeGoal     string
	UserRole         string
	AIRole           string
	PersonaSummary   string
	FocusAreas       []string
	TurnBlueprints   []string
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
	ListReviews(
		context.Context,
		requestcontext.Actor,
		VoiceReviewHistoryQuery,
	) (VoiceReviewHistoryPage, error)
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
	EvaluationContextType string
	EvaluationContext     json.RawMessage
	Result                *VoiceReviewResult
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

type VoiceReviewResult struct {
	SummaryEligibility          string                    `json:"summary_eligibility,omitempty"`
	OverallScore                int                       `json:"overall_score"`
	OverallScorePresent         bool                      `json:"-"`
	Summary                     string                    `json:"summary"`
	Conclusions                 []VoiceReviewConclusion   `json:"conclusions"`
	FeedbackItems               []VoiceReviewFeedbackItem `json:"feedback_items,omitempty"`
	RepracticeSuggestionRefs    []string                  `json:"repractice_suggestion_refs,omitempty"`
	InsufficientEvidenceReasons []string                  `json:"insufficient_evidence_reasons,omitempty"`
}

type VoiceReviewConclusion struct {
	Key          string `json:"key"`
	Category     string `json:"category"`
	Score        int    `json:"score,omitempty"`
	ScorePresent bool   `json:"-"`
	Message      string `json:"message"`
	Suggestion   string `json:"suggestion,omitempty"`
}

func (c VoiceReviewConclusion) MarshalJSON() ([]byte, error) {
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

type VoiceReviewFeedbackItem struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func (r VoiceReviewResult) MarshalJSON() ([]byte, error) {
	type wireResult struct {
		SummaryEligibility          string                    `json:"summary_eligibility,omitempty"`
		OverallScore                *int                      `json:"overall_score,omitempty"`
		Summary                     string                    `json:"summary"`
		Conclusions                 []VoiceReviewConclusion   `json:"conclusions"`
		FeedbackItems               []VoiceReviewFeedbackItem `json:"feedback_items,omitempty"`
		RepracticeSuggestionRefs    []string                  `json:"repractice_suggestion_refs,omitempty"`
		InsufficientEvidenceReasons []string                  `json:"insufficient_evidence_reasons,omitempty"`
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

type VoiceReviewHistoryCursor struct {
	CreatedAt time.Time
	ReviewID  string
}

type VoiceReviewHistoryQuery struct {
	Limit  int
	Before *VoiceReviewHistoryCursor
}

type VoiceReviewHistoryPage struct {
	Items []VoiceSessionReview
	Next  *VoiceReviewHistoryCursor
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
	// Start is idempotent: an existing Session keeps its immutable optional
	// Matter binding even if the Thread selection changes after creation.
	if session.ThreadID != threadID {
		return VoiceSessionState{}, ErrInvalidContext
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
	if session.ThreadID != threadID ||
		(matterID != "" && session.MatterID != matterID) {
		return VoiceSessionState{}, ErrInvalidContext
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

func (application *VoiceSessionApplication) SubmitText(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.SubmitTextAnswerCommand,
) (VoiceSessionState, error) {
	turn, err := application.orchestrator.SubmitText(ctx, actor, command)
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
	item, err := application.reviews.GetReview(ctx, actor, reviewID)
	if err != nil {
		return VoiceSessionReview{}, err
	}
	if !validPersistedVoiceSessionReview(item, reviewID) {
		return VoiceSessionReview{}, ErrInvalidContext
	}
	return item, nil
}

func (application *VoiceSessionApplication) ListReviews(
	ctx context.Context,
	actor requestcontext.Actor,
	query VoiceReviewHistoryQuery,
) (VoiceReviewHistoryPage, error) {
	if err := validateVoiceActor(ctx, actor); err != nil ||
		query.Limit < 1 || query.Limit > 50 ||
		(query.Before != nil &&
			(query.Before.CreatedAt.IsZero() ||
				!validUUID(query.Before.ReviewID))) {
		return VoiceReviewHistoryPage{}, ErrInvalidRequest
	}
	page, err := application.reviews.ListReviews(ctx, actor, query)
	if err != nil {
		return VoiceReviewHistoryPage{}, err
	}
	if len(page.Items) > query.Limit ||
		(page.Next != nil && len(page.Items) != query.Limit) {
		return VoiceReviewHistoryPage{}, ErrInvalidContext
	}
	var previous *VoiceSessionReview
	seen := make(map[string]struct{}, len(page.Items))
	for index := range page.Items {
		item := &page.Items[index]
		if !validUUID(item.ID) ||
			!validPersistedVoiceSessionReview(*item, item.ID) ||
			item.Status != "completed" {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
		if _, exists := seen[item.ID]; exists {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
		seen[item.ID] = struct{}{}
		if previous != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				previous.CreatedAt,
				previous.ID,
			) {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
		if query.Before != nil &&
			!reviewHistoryKeyBefore(
				item.CreatedAt,
				item.ID,
				query.Before.CreatedAt,
				query.Before.ReviewID,
			) {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
		previous = item
	}
	if page.Next != nil {
		if len(page.Items) == 0 {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
		last := page.Items[len(page.Items)-1]
		if !validUUID(page.Next.ReviewID) ||
			!page.Next.CreatedAt.Equal(last.CreatedAt) ||
			page.Next.ReviewID != last.ID {
			return VoiceReviewHistoryPage{}, ErrInvalidContext
		}
	}
	return page, nil
}

func (application *VoiceSessionApplication) state(
	ctx context.Context,
	actor requestcontext.Actor,
	session VoicePracticeSession,
) (VoiceSessionState, error) {
	if session.ID == "" ||
		session.PlanID == "" ||
		session.ThreadID == "" ||
		!validVoiceScenarioPrompt(session) ||
		session.SessionVersion < 1 ||
		session.TurnLimit < 1 ||
		session.TurnLimit > 14 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > session.TurnLimit ||
		!validVoiceSessionLifecycle(session) ||
		session.InterviewerParticipantID == "" ||
		session.CandidateParticipantID == "" ||
		session.InterviewerParticipantID == session.CandidateParticipantID {
		return VoiceSessionState{}, ErrInvalidContext
	}
	state := VoiceSessionState{Session: session}
	if session.MatterID != "" {
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
	}
	if session.Status == "paused" || session.Status == "ended_early" {
		return state, nil
	}
	latest, found, err := application.checkpoints.LatestTurn(
		ctx,
		actor,
		session.ID,
	)
	if err != nil {
		return VoiceSessionState{}, err
	}
	if found {
		if latest.SessionID != session.ID {
			return VoiceSessionState{}, ErrInvalidContext
		}
		state.Turn = &latest
	}
	if found && (latest.EffectiveTurns == 0 ||
		(latest.SessionCompleted && latest.ReviewID == "" &&
			requiresSynchronousSessionReview(session))) {
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
		state.Session.MatterTitle = state.Matter.Title
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
		formalReview, reviewErr := application.GetReview(
			ctx,
			actor,
			state.Turn.ReviewID,
		)
		if reviewErr != nil {
			return VoiceSessionState{}, reviewErr
		}
		if formalReview.SessionID != session.ID ||
			formalReview.SourceTurnID != state.Turn.ID {
			return VoiceSessionState{}, ErrInvalidContext
		}
		state.Review = &formalReview
	}
	if state.Turn != nil &&
		(state.Turn.EffectiveTurns != state.Session.EffectiveTurns ||
			state.Turn.SessionCompleted != state.Session.Completed) {
		return VoiceSessionState{}, ErrInvalidContext
	}
	if state.Session.Completed {
		if state.Turn == nil ||
			(requiresSynchronousSessionReview(session) &&
				state.Review == nil) {
			return VoiceSessionState{}, ErrInvalidContext
		}
		return state, nil
	}
	if state.Turn != nil {
		state.Session.PreviousUserResponse = state.Turn.AnswerText
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
	if state.Question == nil ||
		question.SessionID != session.ID ||
		strings.TrimSpace(question.Text) == "" ||
		question.SpeakerParticipantID !=
			session.InterviewerParticipantID ||
		len(question.AddresseeParticipantIDs) != 1 ||
		question.AddresseeParticipantIDs[0] !=
			session.CandidateParticipantID {
		return VoiceSessionState{}, ErrInvalidContext
	}
	return state, nil
}

func requiresSynchronousSessionReview(session VoicePracticeSession) bool {
	switch session.ScenarioModel {
	case "IELTS_SPEAKING_PART_1",
		"IELTS_SPEAKING_PART_2",
		"IELTS_SPEAKING_PART_3",
		"IELTS_SPEAKING_FULL_MOCK":
		return false
	default:
		return true
	}
}

func validVoiceScenarioPrompt(session VoicePracticeSession) bool {
	prompt := session.PromptModel
	return strings.TrimSpace(session.ScenarioType) != "" &&
		strings.TrimSpace(session.ScenarioModel) != "" &&
		strings.TrimSpace(prompt.PublicSceneBrief) != "" &&
		strings.TrimSpace(prompt.PracticeGoal) != "" &&
		strings.TrimSpace(prompt.UserRole) != "" &&
		strings.TrimSpace(prompt.AIRole) != "" &&
		strings.TrimSpace(prompt.PersonaSummary) != "" &&
		len(prompt.FocusAreas) > 0 &&
		len(prompt.TurnBlueprints) > 0
}

func validVoiceSessionLifecycle(session VoicePracticeSession) bool {
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
	item VoiceSessionReview,
	expectedID string,
) bool {
	return validVoiceSessionReviewWithResult(
		item,
		expectedID,
		validVoiceReviewResult,
	)
}

func validPersistedVoiceSessionReview(
	item VoiceSessionReview,
	expectedID string,
) bool {
	return validVoiceSessionReviewWithResult(
		item,
		expectedID,
		validPersistedVoiceReviewResult,
	)
}

func validVoiceSessionReviewWithResult(
	item VoiceSessionReview,
	expectedID string,
	validResult func(*VoiceReviewResult) bool,
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

func validPersistedVoiceReviewResult(result *VoiceReviewResult) bool {
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

func validVoiceReviewResult(result *VoiceReviewResult) bool {
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
