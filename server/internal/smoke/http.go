package smoke

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	runtime *Runtime
	router  *gin.Engine
}

func NewServer(logger *slog.Logger) *Server {
	runtime := NewRuntime()
	router := bootstrap.NewRouter(logger,
		preparation.New(),
		practice.New(),
		conversation.New(),
		review.New(),
	)
	server := &Server{runtime: runtime, router: router}
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
	s.router.POST("/v1/practice-sessions/:practice_session_id/questions", s.getCurrentQuestion)
	s.router.POST("/v1/questions/:question_id/turns", s.submitTurn)
	s.router.GET("/v1/turns/:turn_id", s.getTurn)
	s.router.GET("/v1/turns/:turn_id/turn-analyses", s.listAnalyses)
	s.router.GET("/v1/turn-analyses/:turn_analysis_id/feedback-items", s.listFeedback)
	s.router.POST("/v1/feedback-items/:feedback_item_id/retry-requests", s.createRetry)
	s.router.GET("/v1/retry-requests/:retry_request_id", s.getRetry)
	s.router.GET("/v1/history-records", s.listHistory)
	s.router.GET("/v1/practice-sessions/:practice_session_id/events", s.streamEvents)
}

func (s *Server) getScenario(c *gin.Context) {
	if c.Param("scenario_definition_id") != DemoScenarioDefinition {
		writeError(c, http.StatusNotFound, "scenario_definition_not_found", "scenario definition not found", false)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"scenario_definition": gin.H{
			"scenario_definition_id": DemoScenarioDefinition,
			"scenario_type":          "INTERVIEW",
			"name":                   "Programmer English Interview",
			"version":                1,
			"status":                 "active",
		},
		"scenario_config": gin.H{
			"scenario_config_id":     "scenario_config_backend",
			"scenario_definition_id": DemoScenarioDefinition,
			"config_type":            "INTERVIEW",
			"version":                1,
			"job_title":              "Backend Engineer",
			"job_description":        "Build reliable APIs and explain engineering trade-offs.",
			"focus_areas":            []string{"reliability", "ownership", "collaboration"},
		},
		"practice_options": []map[string]any{practiceOption()},
	})
}

func (s *Server) listRoles(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"roles": []map[string]any{roleDefinition()}})
}

func (s *Server) createProfile(c *gin.Context) {
	if !requireIdempotencyKey(c) {
		return
	}
	if err := discardJSON(c); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
		return
	}
	c.JSON(http.StatusCreated, s.runtime.createProfile())
}

func (s *Server) createSnapshot(c *gin.Context) {
	if !requireIdempotencyKey(c) {
		return
	}
	if c.Param("preparation_profile_id") != demoPreparationProfile {
		writeError(c, http.StatusNotFound, "preparation_profile_not_found", "preparation profile not found", false)
		return
	}
	result, err := s.runtime.createSnapshot()
	if err != nil {
		writeError(c, http.StatusConflict, "resource_conflict", err.Error(), false)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) createPlan(c *gin.Context) {
	if !requireIdempotencyKey(c) {
		return
	}
	if err := discardJSON(c); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
		return
	}
	result, err := s.runtime.createPlan()
	if err != nil {
		writeError(c, http.StatusConflict, "resource_conflict", err.Error(), false)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) createSession(c *gin.Context) {
	if !requireIdempotencyKey(c) {
		return
	}
	if c.Param("practice_plan_id") != demoPracticePlan {
		writeError(c, http.StatusNotFound, "practice_plan_not_found", "practice plan not found", false)
		return
	}
	if err := discardJSON(c); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
		return
	}
	result, err := s.runtime.createSession()
	if err != nil {
		writeError(c, http.StatusConflict, "resource_conflict", err.Error(), false)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) getSession(c *gin.Context) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	if !s.runtime.sessionCreated || c.Param("practice_session_id") != demoPracticeSession {
		writeError(c, http.StatusNotFound, "practice_session_not_found", "practice session not found", false)
		return
	}
	c.JSON(http.StatusOK, s.runtime.sessionLocked())
}

func (s *Server) getSessionSnapshot(c *gin.Context) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	c.JSON(http.StatusOK, s.runtime.snapshotLocked())
}

func (s *Server) bootstrapConversation(c *gin.Context) {
	result, err := s.runtime.bootstrap()
	if err != nil {
		writeError(c, http.StatusConflict, "resource_conflict", err.Error(), false)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) getCurrentQuestion(c *gin.Context) {
	question, err := s.runtime.ensureCurrentQuestion()
	if err != nil {
		writeError(c, http.StatusNotFound, "question_not_found", err.Error(), false)
		return
	}
	c.JSON(http.StatusOK, question)
}

func (s *Server) submitTurn(c *gin.Context) {
	var request struct {
		InteractionMode string `json:"interaction_mode"`
		AnswerText      string `json:"answer_text"`
		RetryRequestID  string `json:"retry_request_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error(), false)
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "Idempotency-Key header is required", false)
		return
	}
	result, err := s.runtime.submitTurn(
		c.Param("question_id"),
		request.AnswerText,
		request.RetryRequestID,
		key,
		c.GetHeader("X-Mock-Fail-Once") == "true",
	)
	switch {
	case errors.Is(err, ErrInvalidAnswer):
		writeError(c, http.StatusBadRequest, "answer_invalid", err.Error(), false)
	case errors.Is(err, ErrRecoverableFailure):
		writeError(c, http.StatusUnprocessableEntity, "transcript_unavailable", err.Error(), true)
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(c, http.StatusConflict, "idempotency_key_conflict", err.Error(), false)
	case err != nil:
		writeError(c, http.StatusConflict, "turn_conflict", err.Error(), false)
	default:
		submitted := result.Turn
		submitted.Status = "submitted"
		submitted.CompletedAt = ""
		c.JSON(http.StatusAccepted, submitted)
	}
}

func (s *Server) getTurn(c *gin.Context) {
	turn, ok := s.runtime.getTurn(c.Param("turn_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "turn_not_found", "turn not found", false)
		return
	}
	c.JSON(http.StatusOK, turn)
}

func (s *Server) listAnalyses(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"analyses": s.runtime.analysesForTurn(c.Param("turn_id"))})
}

func (s *Server) listFeedback(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"feedback_items": s.runtime.feedbackForAnalysis(c.Param("turn_analysis_id"))})
}

func (s *Server) createRetry(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if key == "" {
		writeError(c, http.StatusBadRequest, "invalid_request", "Idempotency-Key header is required", false)
		return
	}
	retry, _, err := s.runtime.createRetry(c.Param("feedback_item_id"), key)
	if err != nil {
		writeError(c, http.StatusNotFound, "feedback_item_not_found", err.Error(), false)
		return
	}
	c.JSON(http.StatusCreated, retry)
}

func (s *Server) getRetry(c *gin.Context) {
	retry, ok := s.runtime.getRetry(c.Param("retry_request_id"))
	if !ok {
		writeError(c, http.StatusNotFound, "retry_request_not_found", "retry request not found", false)
		return
	}
	c.JSON(http.StatusOK, retry)
}

func (s *Server) listHistory(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"history_records": s.runtime.historyRecords()})
}

func (s *Server) streamEvents(c *gin.Context) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			return err == nil && parsed.Host == request.Host
		},
	}
	connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	afterSequence := 0
	if raw := c.Query("after_sequence"); raw != "" {
		afterSequence, err = strconv.Atoi(raw)
		if err != nil || afterSequence < 0 {
			return
		}
	}
	replay, live, unsubscribe := s.runtime.subscribe(afterSequence)
	defer unsubscribe()
	for _, event := range replay {
		if err := connection.WriteJSON(event); err != nil {
			return
		}
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			if err := connection.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

func discardJSON(c *gin.Context) error {
	if c.Request.Body == nil {
		return nil
	}
	var payload map[string]any
	return json.NewDecoder(c.Request.Body).Decode(&payload)
}

func requireIdempotencyKey(c *gin.Context) bool {
	if c.GetHeader("Idempotency-Key") != "" {
		return true
	}
	writeError(c, http.StatusBadRequest, "invalid_request", "Idempotency-Key header is required", false)
	return false
}

func writeError(c *gin.Context, status int, code, message string, retryable bool) {
	c.JSON(status, gin.H{"error": gin.H{
		"code":           code,
		"message":        message,
		"retryable":      retryable,
		"correlation_id": "correlation_mock_request",
	}})
}

func WebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}
