package voicehttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
	"github.com/gin-gonic/gin"
)

const (
	defaultRealtimeReadTimeout = 15 * time.Second
	defaultRecordedReadTimeout = 60 * time.Second
)

type Application interface {
	Start(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (practiceinteraction.SessionState, error)
	Resume(
		context.Context,
		requestcontext.Actor,
		string,
	) (practiceinteraction.SessionState, error)
	Transcribe(
		context.Context,
		requestcontext.Actor,
		practiceinteraction.TranscribeVoiceCommand,
	) (practiceinteraction.TranscriptionCandidate, error)
	TranscribeStream(
		context.Context,
		requestcontext.Actor,
		practiceinteraction.TranscribeVoiceStreamCommand,
		practiceinteraction.TranscriptionObserver,
	) (practiceinteraction.TranscriptionCandidate, error)
	SubmitText(
		context.Context,
		requestcontext.Actor,
		practiceinteraction.SubmitTextAnswerCommand,
	) (practiceinteraction.SessionState, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		practiceinteraction.ConfirmVoiceTurnCommand,
	) (practiceinteraction.SessionState, error)
	QuestionSpeech(
		context.Context,
		requestcontext.Actor,
		string,
	) (practiceinteraction.QuestionSpeech, error)
	QuestionTranslation(
		context.Context,
		requestcontext.Actor,
		string,
	) (practiceinteraction.QuestionTranslation, error)
	EnsureQuestionTip(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		string,
	) (practiceinteraction.QuestionTipResult, error)
}

type questionTextApplication interface {
	QuestionText(context.Context, requestcontext.Actor, string) (string, error)
}

type Options struct {
	RealtimeReadTimeout time.Duration
	RecordedReadTimeout time.Duration
	SameQuestionRetry   *practiceinteraction.SameQuestionRetryApplication
	Recordings          RecordingHTTPService
	RealtimeSpeech      agentconversation.AssistantSpeechSynthesizer
}

type Handler struct {
	application         Application
	retry               *practiceinteraction.SameQuestionRetryApplication
	recordings          RecordingHTTPService
	realtimeSpeech      agentconversation.AssistantSpeechSynthesizer
	realtimeReadTimeout time.Duration
	recordedReadTimeout time.Duration
	errors              *httpresponse.Renderer
}

func NewHandler(
	application Application,
	options Options,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil || options.RealtimeReadTimeout < 0 ||
		options.RecordedReadTimeout < 0 {
		return nil, errors.New("practice interaction: HTTP dependencies are required")
	}
	if options.RealtimeReadTimeout == 0 {
		options.RealtimeReadTimeout = defaultRealtimeReadTimeout
	}
	if options.RecordedReadTimeout == 0 {
		options.RecordedReadTimeout = defaultRecordedReadTimeout
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{
		application:         application,
		retry:               options.SameQuestionRetry,
		recordings:          options.Recordings,
		realtimeSpeech:      options.RealtimeSpeech,
		realtimeReadTimeout: options.RealtimeReadTimeout,
		recordedReadTimeout: options.RecordedReadTimeout,
		errors:              errorRenderer,
	}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/activation",
		handler.startSession,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/interaction-state",
		handler.resumeSession,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/questions/:question_id/transcription-candidates",
		handler.transcribeCandidate,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/questions/:question_id/transcription-candidates/realtime",
		handler.transcribeCandidateRealtime,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/questions/:question_id/text-answers",
		handler.submitText,
	)
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/questions/:question_id/tips",
		handler.ensureQuestionTip,
	)
	routes.POST(
		"/v1/transcription-candidates/:candidate_id/confirmations",
		handler.confirmCandidate,
	)
	routes.GET("/v1/practice-questions/:question_id/speech", handler.questionSpeech)
	if handler.realtimeSpeech != nil {
		routes.GET(
			"/v1/practice-questions/:question_id/speech/realtime",
			handler.questionSpeechRealtime,
		)
	}
	routes.GET(
		"/v1/practice-questions/:question_id/translation",
		handler.questionTranslation,
	)
	if handler.retry != nil {
		routes.POST(
			"/v1/retry-turns/:retry_turn_id/transcription-candidates",
			handler.transcribeRetryCandidate,
		)
		routes.POST(
			"/v1/retry-turns/:retry_turn_id/transcription-candidates/:candidate_id/confirmations",
			handler.confirmRetryCandidate,
		)
	}
	if handler.recordings != nil {
		routes.GET(recordingPlaybackPath, handler.recordingPlayback)
		routes.DELETE(recordingDeletePath, handler.deleteRecording)
	}
}

func (handler *Handler) startSession(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	state, err := handler.application.Start(
		c.Request.Context(), actor, c.Param("practice_session_id"), key,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, SessionStateResponse(state))
}

func (handler *Handler) resumeSession(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	state, err := handler.application.Resume(
		c.Request.Context(), actor, c.Param("practice_session_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, SessionStateResponse(state))
}

func (handler *Handler) transcribeCandidate(c *gin.Context) {
	key, body, actor, cleanup, ok := handler.prepareAudio(c)
	if !ok {
		return
	}
	defer cleanup()
	candidate, err := handler.application.Transcribe(
		c.Request.Context(),
		actor,
		practiceinteraction.TranscribeVoiceCommand{
			SessionID:      c.Param("practice_session_id"),
			QuestionID:     c.Param("question_id"),
			IdempotencyKey: key,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          body,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusCreated, TranscriptionCandidateResponse(candidate))
}

func (handler *Handler) prepareAudio(c *gin.Context) (
	string,
	io.Reader,
	requestcontext.Actor,
	func(),
	bool,
) {
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok || c.Request.Body == nil ||
		c.Request.ContentLength > platformmedia.MaxAudioBytes ||
		!strings.EqualFold(
			strings.TrimSpace(c.GetHeader("Content-Type")),
			platformmedia.ContentTypeWAV,
		) {
		handler.write(c, invalidRequest(nil))
		return "", nil, requestcontext.Actor{}, nil, false
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return "", nil, requestcontext.Actor{}, nil, false
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(time.Now().Add(handler.recordedReadTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		handler.write(c, internalError(err))
		return "", nil, requestcontext.Actor{}, nil, false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		platformmedia.MaxAudioBytes,
	)
	return key, body, actor,
		func() { _ = controller.SetReadDeadline(time.Time{}) }, true
}

func (handler *Handler) confirmCandidate(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	state, err := handler.application.Confirm(
		c.Request.Context(),
		actor,
		practiceinteraction.ConfirmVoiceTurnCommand{
			CandidateID: c.Param("candidate_id"), IdempotencyKey: key,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, SessionStateResponse(state))
}

func (handler *Handler) submitText(c *gin.Context) {
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	values, ok := httpinput.DecodeObject(
		c,
		[]string{"answer_text"},
		[]string{"answer_text"},
		httpinput.DefaultJSONBodyLimit,
		httpinput.DefaultReadTimeout,
	)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	answerText, ok := httpinput.String(values["answer_text"])
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	state, err := handler.application.SubmitText(
		c.Request.Context(), actor,
		practiceinteraction.SubmitTextAnswerCommand{
			SessionID:      c.Param("practice_session_id"),
			QuestionID:     c.Param("question_id"),
			IdempotencyKey: key, AnswerText: answerText,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, SessionStateResponse(state))
}

func (handler *Handler) questionSpeech(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	speech, err := handler.application.QuestionSpeech(
		c.Request.Context(), actor, c.Param("question_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	if speech.Audio == nil {
		if speech.Failure != nil {
			handler.write(c, providerError(speech.Failure.Kind, nil))
		} else {
			handler.write(c, providerUnavailable(nil))
		}
		return
	}
	defer func() { _ = speech.Audio.Close() }()
	reader, err := speech.Audio.Open()
	if err != nil {
		handler.write(c, internalError(err))
		return
	}
	defer reader.Close()
	c.DataFromReader(
		http.StatusOK, speech.Audio.Size(), speech.Audio.MediaType(), reader, nil,
	)
}

func (handler *Handler) questionTranslation(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	translation, err := handler.application.QuestionTranslation(
		c.Request.Context(), actor, c.Param("question_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"question_id":     translation.QuestionID,
		"target_language": translation.TargetLanguage,
		"translation":     translation.Content,
	})
}

func (handler *Handler) ensureQuestionTip(c *gin.Context) {
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	tip, err := handler.application.EnsureQuestionTip(
		c.Request.Context(),
		actor,
		c.Param("practice_session_id"),
		c.Param("question_id"),
		key,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, QuestionTipResponse(tip))
}

func SessionStateResponse(state practiceinteraction.SessionState) gin.H {
	result := gin.H{
		"practice_session_id":     state.Session.ID,
		"practice_plan_id":        state.Session.PlanID,
		"scene_id":                state.Session.SceneID,
		"scene_version":           state.Session.SceneVersion,
		"practice_experience":     state.Session.PracticeExperience,
		"scene_category":          state.Session.SceneCategory,
		"practice_mode":           state.Session.PracticeMode,
		"practice_session_status": state.Session.Status,
		"session_version":         state.Session.SessionVersion,
		"effective_turns":         state.Session.EffectiveTurns,
		"turn_limit":              state.Session.TurnLimit,
		"completion_mode":         state.Session.CompletionMode,
		"session_completed": state.Session.Completed ||
			state.Session.Status == string(practice.SessionEndedEarly),
		"practice_capabilities": gin.H{
			"retry_allowed":                state.Session.RetryAllowed,
			"question_translation_allowed": state.Session.QuestionTranslationAllowed,
			"question_tips_allowed":        state.Session.QuestionTipsAllowed,
			"speech_feedback_allowed":      state.Session.SpeechFeedbackAllowed,
		},
	}
	if state.Session.IELTSAssignment != nil {
		result["ielts_assignment"] = state.Session.IELTSAssignment
	}
	if state.Question != nil {
		result["current_question"] = QuestionResponse(*state.Question)
	}
	if state.Turn != nil {
		result["current_turn"] = ConfirmedTurnResponse(*state.Turn)
	}
	if len(state.TurnHistory) > 0 {
		history := make([]gin.H, len(state.TurnHistory))
		for index, exchange := range state.TurnHistory {
			history[index] = gin.H{
				"question": QuestionResponse(exchange.Question),
				"turn":     ConfirmedTurnResponse(exchange.Turn),
			}
		}
		result["turn_history"] = history
	}
	return result
}

func QuestionResponse(question practice.Question) gin.H {
	result := gin.H{
		"question_id":         question.ID,
		"practice_session_id": question.SessionID,
		"question_type":       question.Type, "content": question.Content,
		"speaker_participant_id":    question.SpeakerParticipantID,
		"addressee_participant_ids": question.AddresseeParticipantIDs,
		"speech_path":               "/v1/practice-questions/" + question.ID + "/speech",
	}
	if question.ParentQuestionID != "" {
		result["parent_question_id"] = question.ParentQuestionID
	}
	return result
}

func QuestionTipResponse(tip practiceinteraction.QuestionTipResult) gin.H {
	return gin.H{
		"tip_id":              tip.ID,
		"practice_session_id": tip.SessionID,
		"question_id":         tip.QuestionID,
		"content":             tip.Content,
		"created_at":          tip.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func TranscriptionCandidateResponse(
	candidate practiceinteraction.TranscriptionCandidate,
) gin.H {
	return gin.H{
		"candidate_id":              candidate.ID,
		"practice_session_id":       candidate.SessionID,
		"question_id":               candidate.QuestionID,
		"respondent_participant_id": candidate.RespondentParticipantID,
		"transcript_id":             candidate.TranscriptID,
		"evidence_version":          candidate.EvidenceVersion,
		"transcript":                candidate.Transcript,
		"created_at":                candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func ConfirmedTurnResponse(turn practice.Turn) gin.H {
	result := gin.H{
		"turn_id": turn.ID, "practice_session_id": turn.SessionID,
		"question_id":               turn.QuestionID,
		"respondent_participant_id": turn.RespondentParticipantID,
		"candidate_id":              turn.CandidateID, "answer_text": turn.AnswerText,
		"evidence_version":                   turn.EvidenceVersion,
		"effective_turns":                    turn.EffectiveTurns,
		"counts_toward_effective_turn_limit": turn.CountsTowardTurnLimit,
		"session_completed":                  turn.SessionCompleted,
	}
	if turn.AudioAssetID != "" {
		result["audio_asset_id"] = turn.AudioAssetID
	}
	if turn.SpeechFeedbackStatusURL != "" {
		result["speech_feedback_status_url"] = turn.SpeechFeedbackStatusURL
	}
	return result
}

func mapError(err error) error {
	var providerFailure *practiceinteraction.ProviderError
	var translationFailure *sharedtranslation.ProviderError
	switch {
	case errors.Is(err, practiceinteraction.ErrInvalidRequest),
		errors.Is(err, practiceinteraction.ErrInvalidContext),
		errors.Is(err, practiceinteraction.ErrVoiceRoundInvalid),
		errors.Is(err, sharedmedia.ErrInvalidRequest):
		return invalidRequest(err)
	case errors.Is(err, practiceinteraction.ErrNotFound),
		errors.Is(err, practiceinteraction.ErrVoiceRoundNotFound),
		errors.Is(err, sharedmedia.ErrNotFound):
		return resourceNotFound(err)
	case errors.Is(err, practiceinteraction.ErrIdempotencyConflict),
		errors.Is(err, practiceinteraction.ErrVoiceRoundConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, practiceinteraction.ErrConflict),
		errors.Is(err, sharedmedia.ErrConflict):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	case errors.Is(err, practiceinteraction.ErrVoiceRoundProcessing):
		return apperror.New(
			apperror.Conflict, "resource_processing",
			"Resource processing is still in progress.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	case errors.Is(err, practiceinteraction.ErrVoiceRoundCapacity):
		return apperror.New(
			apperror.Unavailable, "voice_capacity_exhausted",
			"Voice processing capacity is temporarily exhausted.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	case errors.As(err, &providerFailure):
		return providerError(providerFailure.Kind, err)
	case errors.As(err, &translationFailure):
		return translationProviderError(translationFailure.Kind, err)
	default:
		return internalError(err)
	}
}

func translationProviderError(
	kind sharedtranslation.ProviderErrorKind,
	cause error,
) error {
	code := "provider_unavailable"
	message := "The configured provider is temporarily unavailable."
	if kind == sharedtranslation.ProviderErrorQuotaExhausted {
		code = "quota_exhausted"
		message = "The configured provider quota is exhausted."
	}
	return apperror.New(
		apperror.Unavailable, code, message,
		apperror.WithRetryable(kind.Retryable()), apperror.WithCause(cause),
	)
}

func providerError(kind practiceinteraction.ProviderErrorKind, cause error) error {
	code := "provider_unavailable"
	message := "The configured provider is temporarily unavailable."
	if kind == practiceinteraction.ProviderErrorQuotaExhausted {
		code = "quota_exhausted"
		message = "The configured provider quota is exhausted."
	}
	return apperror.New(
		apperror.Unavailable, code, message,
		apperror.WithRetryable(kind.Retryable()), apperror.WithCause(cause),
	)
}

func providerUnavailable(cause error) error {
	return apperror.New(
		apperror.Unavailable, "provider_unavailable",
		"The configured provider is temporarily unavailable.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
}

func resourceNotFound(cause error) error {
	return apperror.New(
		apperror.NotFound, "resource_not_found", "Resource was not found.",
		apperror.WithCause(cause),
	)
}

func (handler *Handler) write(c *gin.Context, err error) {
	if appError, ok := apperror.From(err); ok {
		if appError.Category() == apperror.Unauthenticated {
			c.Header("WWW-Authenticate", "Bearer")
		}
		if appError.Retryable() &&
			(appError.Category() == apperror.Unavailable ||
				appError.Code() == "resource_processing") {
			c.Header("Retry-After", "1")
		}
	}
	handler.errors.Write(c, err)
}

func invalidRequest(cause error) error {
	return apperror.New(
		apperror.InvalidArgument, "invalid_request", "Request validation failed.",
		apperror.WithCause(cause),
	)
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated, "authentication_required",
		"Authentication is required.",
	)
}

func internalError(cause error) error {
	return apperror.New(
		apperror.Internal, "internal_error", "An internal error occurred.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
}
