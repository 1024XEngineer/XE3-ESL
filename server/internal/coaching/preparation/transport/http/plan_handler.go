package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type PlanHTTPApplication interface {
	ListPlans(context.Context, requestcontext.Actor, scene.PracticeExperience) ([]PracticePlanSummary, error)
	CreatePlan(context.Context, requestcontext.Actor, string, CreatePlanRequest) (PracticePlan, bool, error)
	ReadPlan(context.Context, requestcontext.Actor, string) (PracticePlan, error)
	ConfirmPlan(context.Context, requestcontext.Actor, string, string, ConfirmPlanRequest) (PracticePlan, bool, error)
}

type PlanHTTPHandler struct{ application PlanHTTPApplication }

func NewPlanHTTPHandler(application PlanHTTPApplication) (*PlanHTTPHandler, error) {
	if application == nil {
		return nil, errors.New("preparation: plan HTTP application is required")
	}
	return &PlanHTTPHandler{application: application}, nil
}

func (h *PlanHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/practice-plans", h.list)
	routes.POST("/v1/practice-plans", h.create)
	routes.GET("/v1/practice-plans/:practice_plan_id", h.read)
	routes.POST("/v1/practice-plans/:practice_plan_id/confirm", h.confirm)
}

func (h *PlanHTTPHandler) list(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query["practice_experience"]) != 1 {
		writePlanError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	plans, err := h.application.ListPlans(c.Request.Context(), actor, scene.PracticeExperience(c.Query("practice_experience")))
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"practice_plans": plans})
}

func (h *PlanHTTPHandler) create(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	requestID, ok := clientRequestID(c)
	if !ok {
		writePlanError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var request CreatePlanRequest
	if !decodeJSONObject(c, &request) || !validCreatePlanRequest(request) {
		writePlanError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	plan, _, err := h.application.CreatePlan(c.Request.Context(), actor, requestID, request)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, plan)
}

func (h *PlanHTTPHandler) read(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	id := c.Param("practice_plan_id")
	if !validPlanResourceID(id) {
		writePlanError(c, http.StatusNotFound, "practice_plan_not_found")
		return
	}
	plan, err := h.application.ReadPlan(c.Request.Context(), actor, id)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *PlanHTTPHandler) confirm(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	id := c.Param("practice_plan_id")
	requestID, idOK := clientRequestID(c)
	var request ConfirmPlanRequest
	if !validPlanResourceID(id) || !idOK || !decodeJSONObject(c, &request) || request.ExpectedVersion < 1 {
		writePlanError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	plan, _, err := h.application.ConfirmPlan(c.Request.Context(), actor, id, requestID, request)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func writePlanServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPlanInvalid):
		writePlanError(c, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrPlanNotFound):
		writePlanError(c, http.StatusNotFound, "practice_plan_not_found")
	case errors.Is(err, ErrPlanIdempotencyConflict):
		writePlanError(c, http.StatusConflict, "idempotency_key_conflict")
	case errors.Is(err, ErrPlanConflict):
		writePlanError(c, http.StatusConflict, "resource_conflict")
	default:
		writePlanError(c, http.StatusInternalServerError, "internal_error")
	}
}

func writePlanError(c *gin.Context, status int, code string) {
	writeHTTPError(c, status, code, false)
}

var _ PlanHTTPApplication = (*PlanService)(nil)
