package smoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	eventProtocol     = "speakup.events.v1"
	eventWriteTimeout = 5 * time.Second
	maxRequestBody    = 1 << 20
)

type Server struct {
	router       *gin.Engine
	preparation  *preparation.Service
	practice     *practice.Service
	conversation *conversation.Service
	review       *review.Service
	application  *Application
	idempotency  *idempotencyStore
}

func NewServer(logger *slog.Logger) *Server {
	runtime := NewRuntime()
	provider := NewDeterministicProvider()
	preparationService := preparation.NewService(preparationBackend{runtime: runtime})
	practiceService := practice.NewService(practiceBackend{runtime: runtime})
	conversationService := conversation.NewService(
		conversationBackend{runtime: runtime},
		provider,
	)
	reviewService := review.NewService(reviewBackend{runtime: runtime}, provider)
	router := bootstrap.NewRouter(logger,
		preparation.New(),
		practice.New(),
		conversation.New(),
		review.New(),
	)
	server := &Server{
		router:       router,
		preparation:  preparationService,
		practice:     practiceService,
		conversation: conversationService,
		review:       reviewService,
		application: NewApplication(
			preparationService,
			practiceService,
			conversationService,
			reviewService,
			provider,
		),
		idempotency: newIdempotencyStore(),
	}
	server.registerRoutes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) registerRoutes() {
	s.router.GET("/v1/scenario-definitions/:scenario_definition_id", s.getScenario)
	s.router.GET("/v1/scenario-definitions/:scenario_definition_id/role-definitions", s.listRoles)
	s.router.POST("/v1/preparation-profiles", s.createProfile)
	s.router.POST("/v1/preparation-profiles/:preparation_profile_id/snapshots", s.createSnapshot)
	s.router.POST("/v1/practice-plans", s.createPlan)
	s.router.POST("/v1/practice-plans/:practice_plan_id/practice-sessions", s.createSession)
	s.router.GET("/v1/practice-sessions/:practice_session_id", s.getSession)
	s.router.GET("/v1/practice-sessions/:practice_session_id/snapshot", s.getSessionSnapshot)
	s.router.GET("/v1/practice-sessions/:practice_session_id/bootstrap", s.bootstrapConversation)
	s.router.POST("/v1/practice-sessions/:practice_session_id/questions", s.ensureCurrentQuestion)
	s.router.POST("/v1/questions/:question_id/turns", s.submitTurn)
	s.router.GET("/v1/turns/:turn_id", s.getTurn)
	s.router.POST("/v1/turns/:turn_id/turn-analyses", s.analyzeTurn)
	s.router.GET("/v1/turns/:turn_id/turn-analyses", s.listAnalyses)
	s.router.GET("/v1/turn-analyses/:turn_analysis_id/feedback-items", s.listFeedback)
	s.router.POST("/v1/feedback-items/:feedback_item_id/retry-requests", s.createRetry)
	s.router.GET("/v1/retry-requests/:retry_request_id", s.getRetry)
	s.router.GET("/v1/history-records", s.listHistory)
	s.router.GET("/v1/practice-sessions/:practice_session_id/events", s.streamEvents)
}

func (s *Server) getScenario(c *gin.Context) {
	result, ok := s.preparation.GetScenario(c.Param("scenario_definition_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "scenario_definition_not_found", "Scenario definition was not found.", false)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) listRoles(c *gin.Context) {
	roles, ok := s.preparation.ListRoles(c.Param("scenario_definition_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "scenario_definition_not_found", "Scenario definition was not found.", false)
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

func (s *Server) createProfile(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		var request struct {
			ResumeRef         *string `json:"resume_ref"`
			JobDescriptionRef *string `json:"job_description_ref"`
			BackgroundSummary *string `json:"background_summary"`
		}
		if err := decodeStrict(raw, &request); err != nil {
			return invalidRequest()
		}
		if request.BackgroundSummary == nil || strings.TrimSpace(*request.BackgroundSummary) == "" ||
			(request.ResumeRef != nil && strings.TrimSpace(*request.ResumeRef) == "") ||
			(request.JobDescriptionRef != nil && strings.TrimSpace(*request.JobDescriptionRef) == "") {
			return invalidRequest()
		}
		input := preparation.CreateProfileRequest{BackgroundSummary: *request.BackgroundSummary}
		if request.ResumeRef != nil {
			input.ResumeRef = *request.ResumeRef
		}
		if request.JobDescriptionRef != nil {
			input.JobDescriptionRef = *request.JobDescriptionRef
		}
		result, err := s.preparation.CreateProfile(input)
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusCreated, payload: result}
	})
}

func (s *Server) createSnapshot(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		var request preparation.CreateSnapshotRequest
		if err := decodeStrict(raw, &request); err != nil || request.SourceVersion < 1 {
			return invalidRequest()
		}
		result, err := s.preparation.CreateSnapshot(c.Param("preparation_profile_id"), request)
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusCreated, payload: result}
	})
}

func (s *Server) createPlan(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		var request practice.CreatePlanRequest
		if err := decodeStrict(raw, &request); err != nil ||
			!validID(request.ScenarioDefinitionID) ||
			request.ScenarioDefinitionVersion < 1 ||
			!validID(request.ScenarioConfigID) ||
			request.ScenarioConfigVersion < 1 ||
			!validID(request.PreparationProfileID) ||
			!validUniqueIDs(request.SelectedRoleIDs) {
			return invalidRequest()
		}
		result, err := s.application.CreatePlan(request)
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusCreated, payload: result}
	})
}

func (s *Server) createSession(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		var request practice.CreateSessionRequest
		if err := decodeStrict(raw, &request); err != nil ||
			request.ExpectedPlanRevision < 1 ||
			!validID(request.PreparationSnapshotID) ||
			!validID(request.PracticeOptionID) ||
			!validUniqueIDs(request.RoleDefinitionIDs) {
			return invalidRequest()
		}
		result, err := s.application.CreateSession(c.Param("practice_plan_id"), request)
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusCreated, payload: result}
	})
}

func (s *Server) getSession(c *gin.Context) {
	result, ok := s.practice.GetSession(c.Param("practice_session_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "practice_session_not_found", "Practice session was not found.", false)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) getSessionSnapshot(c *gin.Context) {
	result, ok := s.practice.GetSnapshot(c.Param("practice_session_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "practice_session_not_found", "Practice session was not found.", false)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) bootstrapConversation(c *gin.Context) {
	result, err := s.application.Bootstrap(c.Param("practice_session_id"))
	if err != nil {
		writeAPIResponse(c, serviceError(err))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) ensureCurrentQuestion(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		if !emptyBody(raw) {
			return invalidRequest()
		}
		question, err := s.application.EnsureCurrentQuestion(c.Param("practice_session_id"))
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusOK, payload: question}
	})
}

func (s *Server) submitTurn(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		var request conversation.SubmitTurnRequest
		if err := decodeStrict(raw, &request); err != nil {
			return invalidRequest()
		}
		if request.InteractionMode != "PUSH_TO_TALK" && request.InteractionMode != "REALTIME" {
			return invalidRequest()
		}
		if strings.TrimSpace(request.AnswerText) == "" {
			return errorResponse(http.StatusBadRequest, "answer_invalid", "Answer text must not be empty.", false)
		}
		if (fieldPresent(raw, "audio_asset_id") && !validID(request.AudioAssetID)) ||
			(fieldPresent(raw, "retry_request_id") && !validID(request.RetryRequestID)) {
			return invalidRequest()
		}
		turn, err := s.application.SubmitTurn(
			c.Param("question_id"),
			request,
			c.GetHeader("X-Mock-Fail-Once") == "true",
		)
		if errors.Is(err, ErrRecoverableFailure) {
			return errorResponse(
				http.StatusUnprocessableEntity,
				"transcript_unavailable",
				"Answer processing is temporarily unavailable.",
				true,
			)
		}
		if err != nil {
			return serviceError(err)
		}
		submitted := turn
		submitted.Status = "submitted"
		submitted.CompletedAt = ""
		return apiResponse{status: http.StatusAccepted, payload: submitted}
	})
}

func (s *Server) getTurn(c *gin.Context) {
	turn, ok := s.conversation.GetTurn(c.Param("turn_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "turn_not_found", "Turn was not found.", false)
		return
	}
	c.JSON(http.StatusOK, turn)
}

func (s *Server) analyzeTurn(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		if !emptyBody(raw) {
			return invalidRequest()
		}
		analysis, err := s.application.AnalyzeTurn(c.Param("turn_id"))
		if err != nil {
			return serviceError(err)
		}
		pending := pendingAnalysisResponse{
			ID:               analysis.ID,
			TurnID:           analysis.TurnID,
			EvaluatorVersion: analysis.EvaluatorVersion,
			Status:           "pending",
			CreatedAt:        analysis.CreatedAt,
		}
		return apiResponse{status: http.StatusAccepted, payload: pending}
	})
}

func (s *Server) listAnalyses(c *gin.Context) {
	analyses, err := s.application.ListAnalyses(c.Param("turn_id"))
	if err != nil {
		writeAPIResponse(c, serviceError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"analyses": analyses})
}

func (s *Server) listFeedback(c *gin.Context) {
	feedback, ok := s.review.ListFeedback(c.Param("turn_analysis_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "turn_analysis_not_found", "Turn analysis was not found.", false)
		return
	}
	c.JSON(http.StatusOK, gin.H{"feedback_items": feedback})
}

func (s *Server) createRetry(c *gin.Context) {
	raw, ok := readBody(c)
	if !ok {
		return
	}
	s.executeIdempotent(c, raw, func() apiResponse {
		if !emptyBody(raw) {
			return invalidRequest()
		}
		retry, err := s.application.CreateRetry(c.Param("feedback_item_id"))
		if err != nil {
			return serviceError(err)
		}
		return apiResponse{status: http.StatusCreated, payload: retry}
	})
}

func (s *Server) getRetry(c *gin.Context) {
	retry, ok := s.review.GetRetry(c.Param("retry_request_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "retry_request_not_found", "Retry request was not found.", false)
		return
	}
	c.JSON(http.StatusOK, retry)
}

func (s *Server) listHistory(c *gin.Context) {
	values, present := c.Request.URL.Query()["practice_session_id"]
	if !present || len(values) != 1 || !validID(values[0]) {
		writeError(c, http.StatusBadRequest, "invalid_request", "practice_session_id is required.", false)
		return
	}
	history, err := s.application.ListHistory(values[0])
	if err != nil {
		writeAPIResponse(c, serviceError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"history_records": history})
}

func (s *Server) streamEvents(c *gin.Context) {
	sessionID := c.Param("practice_session_id")
	if _, ok := s.practice.GetSession(sessionID); !ok {
		writeError(c, http.StatusNotFound, "practice_session_not_found", "Practice session was not found.", false)
		return
	}
	afterSequence := 0
	if values, present := c.Request.URL.Query()["after_sequence"]; present {
		if len(values) != 1 || values[0] == "" {
			writeError(c, http.StatusBadRequest, "invalid_request", "after_sequence must be a non-negative integer.", false)
			return
		}
		parsed, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || parsed < 0 || parsed > int64(^uint(0)>>1) {
			writeError(c, http.StatusBadRequest, "invalid_request", "after_sequence must be a non-negative integer.", false)
			return
		}
		afterSequence = int(parsed)
	}
	if !containsString(websocket.Subprotocols(c.Request), eventProtocol) {
		writeError(c, http.StatusBadRequest, "unsupported_message", "The required WebSocket protocol was not offered.", false)
		return
	}
	replay, live, unsubscribe, err := s.conversation.Subscribe(sessionID, afterSequence)
	if err != nil {
		writeAPIResponse(c, serviceError(err))
		return
	}
	defer unsubscribe()
	ready, err := s.conversation.StreamReady(sessionID)
	if err != nil {
		writeAPIResponse(c, serviceError(err))
		return
	}
	upgrader := websocket.Upgrader{
		Subprotocols: []string{eventProtocol},
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" {
				return true
			}
			parsed, parseErr := url.Parse(origin)
			return parseErr == nil && parsed.Host == request.Host
		},
	}
	connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	if err := writeWebSocketJSON(connection, ready); err != nil {
		return
	}
	for _, event := range replay {
		if err := writeWebSocketJSON(connection, event); err != nil {
			return
		}
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, open := <-live:
			if !open {
				return
			}
			if err := writeWebSocketJSON(connection, event); err != nil {
				return
			}
		}
	}
}

type pendingAnalysisResponse struct {
	ID               string `json:"turn_analysis_id"`
	TurnID           string `json:"turn_id"`
	EvaluatorVersion string `json:"evaluator_version"`
	Status           string `json:"analysis_status"`
	CreatedAt        string `json:"created_at"`
}

func writeWebSocketJSON(connection *websocket.Conn, value any) error {
	if err := connection.SetWriteDeadline(time.Now().Add(eventWriteTimeout)); err != nil {
		return err
	}
	return connection.WriteJSON(value)
}

type apiResponse struct {
	status  int
	payload any
}

func invalidRequest() apiResponse {
	return errorResponse(http.StatusBadRequest, "invalid_request", "Request validation failed.", false)
}

func serviceError(err error) apiResponse {
	switch {
	case errors.Is(err, ErrInvalidAnswer):
		return errorResponse(http.StatusBadRequest, "answer_invalid", "Answer text must not be empty.", false)
	case errors.Is(err, ErrSessionCompleted):
		return errorResponse(http.StatusConflict, "turn_conflict", "Practice session is already completed.", false)
	}
	status, code := mapServiceError(err)
	return errorResponse(status, code, http.StatusText(status), false)
}

func errorResponse(status int, code, message string, retryable bool) apiResponse {
	return apiResponse{status: status, payload: gin.H{"error": gin.H{
		"code":           code,
		"message":        message,
		"retryable":      retryable,
		"correlation_id": "correlation_mock_request",
	}}}
}

func writeError(c *gin.Context, status int, code, message string, retryable bool) {
	writeAPIResponse(c, errorResponse(status, code, message, retryable))
}

func writeAPIResponse(c *gin.Context, response apiResponse) {
	c.JSON(response.status, response.payload)
}

func readBody(c *gin.Context) ([]byte, bool) {
	if c.Request.Body == nil {
		return nil, true
	}
	limited := io.LimitReader(c.Request.Body, maxRequestBody+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > maxRequestBody {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request body is invalid.", false)
		return nil, false
	}
	return body, true
}

func decodeStrict(body []byte, target any) error {
	if emptyBody(body) {
		return errors.New("request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return errors.New("request body must be a JSON object")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("null is not accepted")
		}
	}
	return nil
}

func emptyBody(body []byte) bool {
	return len(bytes.TrimSpace(body)) == 0
}

func fieldPresent(body []byte, field string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}

func validID(value string) bool {
	length := len(value)
	return length >= 1 && length <= 128 && strings.TrimSpace(value) == value
}

func validUniqueIDs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validID(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func WebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

type idempotencyRecord struct {
	fingerprint [sha256.Size]byte
	status      int
	body        []byte
}

type idempotencyStore struct {
	mu      sync.Mutex
	records map[string]idempotencyRecord
}

func newIdempotencyStore() *idempotencyStore {
	return &idempotencyStore{records: make(map[string]idempotencyRecord)}
}

func (s *Server) executeIdempotent(
	c *gin.Context,
	requestBody []byte,
	action func() apiResponse,
) {
	key := c.GetHeader("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Idempotency-Key must contain 8 to 128 characters.", false)
		return
	}
	scope := DemoUserID + "\x00" + c.Request.Method + "\x00" + c.Request.URL.Path
	recordKey := scope + "\x00" + key
	fingerprint := bodyFingerprint(requestBody)

	s.idempotency.mu.Lock()
	defer s.idempotency.mu.Unlock()
	if existing, ok := s.idempotency.records[recordKey]; ok {
		if existing.fingerprint != fingerprint {
			writeError(c, http.StatusConflict, "idempotency_key_conflict", "Idempotency key was reused with a different request.", false)
			return
		}
		c.Data(existing.status, "application/json; charset=utf-8", append([]byte(nil), existing.body...))
		return
	}

	response := action()
	body, err := json.Marshal(response.payload)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error.", false)
		return
	}
	s.idempotency.records[recordKey] = idempotencyRecord{
		fingerprint: fingerprint,
		status:      response.status,
		body:        append([]byte(nil), body...),
	}
	c.Data(response.status, "application/json; charset=utf-8", body)
}

func bodyFingerprint(body []byte) [sha256.Size]byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return sha256.Sum256(nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err == nil {
		var extra any
		if errors.Is(decoder.Decode(&extra), io.EOF) {
			if canonical, marshalErr := json.Marshal(value); marshalErr == nil {
				return sha256.Sum256(canonical)
			}
		}
	}
	return sha256.Sum256(trimmed)
}
