package evaluationapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type HTTPHandler struct {
	application *Application
	errors      *httpresponse.Renderer
}

func NewHTTPHandler(application *Application) (*HTTPHandler, error) {
	if application == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &HTTPHandler{
		application: application,
		errors:      httpresponse.NewRenderer(nil),
	}, nil
}

func (handler *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/practice-sessions/:practice_session_id/evaluation", handler.getSession)
	routes.GET("/v1/practice-turns/:turn_id/evaluation", handler.getTurn)
	routes.GET("/v1/agent-messages/:message_id/evaluation", handler.getAgentMessage)
	routes.GET("/v1/evaluation-reports", handler.listReports)
	routes.GET("/v1/evaluation-reports/:report_id", handler.getReport)
}

func (handler *HTTPHandler) getSession(c *gin.Context) {
	handler.writeSource(
		c, evaluation.KindSessionReport, c.Param("practice_session_id"),
	)
}

func (handler *HTTPHandler) getTurn(c *gin.Context) {
	handler.writeSource(
		c, evaluation.KindPracticeTurnFeedback, c.Param("turn_id"),
	)
}

func (handler *HTTPHandler) getAgentMessage(c *gin.Context) {
	handler.writeSource(
		c, evaluation.KindAgentMessageFeedback, c.Param("message_id"),
	)
}

func (handler *HTTPHandler) writeSource(
	c *gin.Context,
	kind evaluation.Kind,
	sourceID string,
) {
	handler.writeResource(c, func(userID string) (Resource, error) {
		return handler.application.GetBySource(
			c.Request.Context(), userID, kind, sourceID,
		)
	})
}

func (handler *HTTPHandler) writeResource(
	c *gin.Context,
	load func(string) (Resource, error),
) {
	privateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok || !actor.Valid() {
		handler.writeError(c, authenticationRequired())
		return
	}
	resource, err := load(actor.UserID)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	response, err := resourceResponse(resource)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (handler *HTTPHandler) getReport(c *gin.Context) {
	privateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok || !actor.Valid() {
		handler.writeError(c, authenticationRequired())
		return
	}
	stored, err := handler.application.GetReport(
		c.Request.Context(), actor.UserID, c.Param("report_id"),
	)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, reportResponse(stored))
}

func (handler *HTTPHandler) listReports(c *gin.Context) {
	privateHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok || !actor.Valid() {
		handler.writeError(c, authenticationRequired())
		return
	}
	query, err := historyQuery(c)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	page, err := handler.application.ListReports(
		c.Request.Context(), actor.UserID, query,
	)
	if err != nil {
		handler.writeError(c, err)
		return
	}
	items := make([]gin.H, len(page.Items))
	for index, item := range page.Items {
		items[index] = reportResponse(item)
	}
	response := gin.H{"items": items}
	if page.HasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		response["next_cursor"] = encodeCursor(report.HistoryBoundary{
			CreatedAt: last.CreatedAt,
			ReportID:  last.ReportID,
		})
	}
	c.JSON(http.StatusOK, response)
}

func resourceResponse(resource Resource) (gin.H, error) {
	record := resource.Evaluation
	response := gin.H{
		"evaluation_id":  record.ID,
		"kind":           record.Kind,
		"source_id":      record.SourceID,
		"context_id":     record.ContextID,
		"status":         record.Status,
		"created_at":     record.CreatedAt,
		"updated_at":     record.UpdatedAt,
		"feedback_items": resource.FeedbackItems,
	}
	if record.Status == evaluation.JobReady {
		result, err := publicResult(record)
		if err != nil {
			return nil, err
		}
		response["result"] = result
	}
	if record.Status == evaluation.JobFailed && record.Error != nil {
		response["error"] = record.Error
	}
	return response, nil
}

type publicSpeechResult struct {
	SchemaVersion      string         `json:"schema_version"`
	ScoreabilityStatus string         `json:"scoreability_status"`
	Summary            string         `json:"summary"`
	ReasonCodes        []string       `json:"reason_codes"`
	Acoustic           publicAcoustic `json:"acoustic"`
}

type publicAcoustic struct {
	Status           evaluation.AcousticAssessmentStatus `json:"status"`
	Reason           string                              `json:"reason,omitempty"`
	Pronunciation    *float64                            `json:"pronunciation,omitempty"`
	Fluency          *float64                            `json:"fluency,omitempty"`
	Integrity        *float64                            `json:"integrity,omitempty"`
	SpeakingSpeedWPM *float64                            `json:"speaking_speed_wpm,omitempty"`
}

func publicResult(record evaluation.Record) (any, error) {
	switch record.Kind {
	case evaluation.KindSessionReport:
		var formal report.FormalReport
		if evaluation.DecodeStrict(record.Result, &formal) != nil || !formal.Valid() {
			return nil, evaluation.ErrInvalidRequest
		}
		return formal, nil
	case evaluation.KindPracticeTurnFeedback, evaluation.KindAgentMessageFeedback:
		var speech evaluation.SpeechResult
		if evaluation.DecodeStrict(record.Result, &speech) != nil || !speech.Valid() {
			return nil, evaluation.ErrInvalidRequest
		}
		return publicSpeechResult{
			SchemaVersion:      speech.SchemaVersion,
			ScoreabilityStatus: speech.ScoreabilityStatus,
			Summary:            speech.Summary,
			ReasonCodes:        speech.ReasonCodes,
			Acoustic: publicAcoustic{
				Status:           speech.Acoustic.Status,
				Reason:           speech.Acoustic.Reason,
				Pronunciation:    speech.Acoustic.Pronunciation,
				Fluency:          speech.Acoustic.Fluency,
				Integrity:        speech.Acoustic.Integrity,
				SpeakingSpeedWPM: speech.Acoustic.SpeakingSpeedWPM,
			},
		}, nil
	default:
		return nil, evaluation.ErrInvalidRequest
	}
}

func reportResponse(stored report.StoredFormalReport) gin.H {
	return gin.H{
		"report_id":           stored.ReportID,
		"evaluation_id":       stored.EvaluationID,
		"practice_session_id": stored.PracticeSessionID,
		"report":              stored.Report,
		"created_at":          stored.CreatedAt,
	}
}

func historyQuery(c *gin.Context) (report.HistoryQuery, error) {
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			return report.HistoryQuery{}, evaluation.ErrInvalidRequest
		}
		limit = parsed
	}
	query := report.HistoryQuery{
		Limit:  limit,
		Search: c.Query("search"),
	}
	if raw := strings.TrimSpace(c.Query("cursor")); raw != "" {
		boundary, err := decodeCursor(raw)
		if err != nil {
			return report.HistoryQuery{}, err
		}
		query.Before = &boundary
	}
	return query, nil
}

type cursorPayload struct {
	CreatedAt string `json:"created_at"`
	ReportID  string `json:"report_id"`
}

func encodeCursor(boundary report.HistoryBoundary) string {
	encoded, _ := json.Marshal(cursorPayload{
		CreatedAt: boundary.CreatedAt.UTC().Format(time.RFC3339Nano),
		ReportID:  boundary.ReportID,
	})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(value string) (report.HistoryBoundary, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(encoded) > 512 {
		return report.HistoryBoundary{}, evaluation.ErrInvalidRequest
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if decoder.Decode(&payload) != nil {
		return report.HistoryBoundary{}, evaluation.ErrInvalidRequest
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return report.HistoryBoundary{}, evaluation.ErrInvalidRequest
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return report.HistoryBoundary{}, evaluation.ErrInvalidRequest
	}
	boundary := report.HistoryBoundary{
		CreatedAt: createdAt.UTC(),
		ReportID:  payload.ReportID,
	}
	if !boundary.Valid() {
		return report.HistoryBoundary{}, evaluation.ErrInvalidRequest
	}
	return boundary, nil
}

func privateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (handler *HTTPHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, evaluation.ErrInvalidRequest):
		handler.errors.Write(c, apperror.New(
			apperror.InvalidArgument, "invalid_request", "The request is invalid.",
		))
	case errors.Is(err, evaluation.ErrNotFound):
		handler.errors.Write(c, apperror.New(
			apperror.NotFound, "resource_not_found", "Evaluation was not found.",
		))
	case errors.Is(err, evaluation.ErrAccountUnavailable):
		handler.errors.Write(c, apperror.New(
			apperror.PermissionDenied, "account_unavailable", "The account is unavailable.",
		))
	default:
		handler.errors.Write(c, err)
	}
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated,
		"authentication_required",
		"Authentication is required.",
	)
}
