package voicehttp

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const defaultReadTimeout = 15 * time.Second

type Options struct {
	AudioReadTimeout  time.Duration
	SameQuestionRetry *practicevoice.SameQuestionRetryApplication
	AudioAssets       AudioAssetHTTPService
}

type Handler struct {
	application *practicevoice.SessionApplication
	retry       *practicevoice.SameQuestionRetryApplication
	audioAssets AudioAssetHTTPService
	readTimeout time.Duration
	errors      *httpresponse.Renderer
}

func NewHandler(
	application *practicevoice.SessionApplication,
	options Options,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if application == nil || options.AudioReadTimeout < 0 {
		return nil, errors.New("practice voice: HTTP dependencies are required")
	}
	if options.AudioReadTimeout == 0 {
		options.AudioReadTimeout = defaultReadTimeout
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{
		application: application,
		retry:       options.SameQuestionRetry,
		audioAssets: options.AudioAssets,
		readTimeout: options.AudioReadTimeout,
		errors:      errorRenderer,
	}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST(
		"/v1/practice-sessions/:practice_session_id/voice-activation",
		handler.startSession,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/voice-state",
		handler.resumeSession,
	)
	routes.POST(
		"/v1/voice-practice-sessions/:practice_session_id/questions/:question_id/transcription-candidates",
		handler.transcribeCandidate,
	)
	routes.POST(
		"/v1/voice-practice-sessions/:practice_session_id/questions/:question_id/text-answers",
		handler.submitText,
	)
	routes.POST(
		"/v1/transcription-candidates/:candidate_id/confirmations",
		handler.confirmCandidate,
	)
	routes.GET("/v1/voice-questions/:question_id/speech", handler.questionSpeech)
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
	if handler.audioAssets != nil {
		_ = RegisterAudioAssetRoutes(
			routes,
			handler.audioAssets,
			AudioAssetActorResolverFunc(
				func(request *http.Request) (
					practicevoice.AudioAssetActor,
					bool,
				) {
					actor, ok := requestcontext.ActorFromContext(request.Context())
					return practicevoice.AudioAssetActor{UserID: actor.UserID},
						ok && actor.Valid()
				},
			),
		)
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
		practicevoice.TranscribeVoiceCommand{
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
	if err := controller.SetReadDeadline(time.Now().Add(handler.readTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
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
		practicevoice.ConfirmVoiceTurnCommand{
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
		practicevoice.SubmitTextAnswerCommand{
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

func SessionStateResponse(state practicevoice.SessionState) gin.H {
	result := gin.H{
		"practice_session_id": state.Session.ID,
		"practice_plan_id":    state.Session.PlanID,
		"scene_id":            state.Session.SceneID,
		"scene_version":       state.Session.SceneVersion,
		"scene_family":        state.Session.SceneFamily,
		"scene_model":         state.Session.SceneModel,
		"session_version":     state.Session.SessionVersion,
		"effective_turns":     state.Session.EffectiveTurns,
		"turn_limit":          state.Session.TurnLimit,
		"session_completed":   state.Session.Completed,
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
		"speech_path":               "/v1/voice-questions/" + question.ID + "/speech",
	}
	if question.ParentQuestionID != "" {
		result["parent_question_id"] = question.ParentQuestionID
	}
	return result
}

func TranscriptionCandidateResponse(
	candidate practicevoice.TranscriptionCandidate,
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
	var providerFailure *practicevoice.ProviderError
	switch {
	case errors.Is(err, practicevoice.ErrInvalidRequest),
		errors.Is(err, practicevoice.ErrInvalidContext),
		errors.Is(err, practicevoice.ErrVoiceRoundInvalid):
		return invalidRequest(err)
	case errors.Is(err, practicevoice.ErrNotFound),
		errors.Is(err, practicevoice.ErrVoiceRoundNotFound):
		return resourceNotFound(err)
	case errors.Is(err, practicevoice.ErrIdempotencyConflict),
		errors.Is(err, practicevoice.ErrVoiceRoundConflict):
		return apperror.New(
			apperror.Conflict, "idempotency_key_conflict",
			"Idempotency key conflicts with the original request.",
			apperror.WithCause(err),
		)
	case errors.Is(err, practicevoice.ErrConflict):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	case errors.Is(err, practicevoice.ErrVoiceRoundProcessing):
		return apperror.New(
			apperror.Conflict, "resource_processing",
			"Resource processing is still in progress.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	case errors.Is(err, practicevoice.ErrVoiceRoundCapacity):
		return apperror.New(
			apperror.Unavailable, "voice_capacity_exhausted",
			"Voice processing capacity is temporarily exhausted.",
			apperror.WithRetryable(true), apperror.WithCause(err),
		)
	case errors.As(err, &providerFailure):
		return providerError(providerFailure.Kind, err)
	default:
		return internalError(err)
	}
}

func providerError(kind practicevoice.ProviderErrorKind, cause error) error {
	code := "provider_unavailable"
	message := "The configured provider is temporarily unavailable."
	if kind == practicevoice.ProviderErrorQuotaExhausted {
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
