package preparation

import (
	"context"
	"errors"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

// PlanHTTPApplication is Preparation's transport-facing Plan boundary.
// Practice consumes only PlanReader and never registers these write routes.
type PlanHTTPApplication interface {
	ListPlans(
		context.Context,
		requestcontext.Actor,
		scene.PracticeExperience,
	) ([]PracticePlanSummary, error)
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		CreatePlanRequest,
	) (PracticePlan, bool, error)
	ReadPlan(
		context.Context,
		requestcontext.Actor,
		string,
	) (PracticePlan, error)
	RevisePlan(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		RevisePlanRequest,
	) (PracticePlan, bool, error)
	ArchivePlan(context.Context, requestcontext.Actor, string) error
}

type PlanHTTPHandler struct {
	application PlanHTTPApplication
}

func NewPlanHTTPHandler(
	application PlanHTTPApplication,
) (*PlanHTTPHandler, error) {
	if application == nil {
		return nil, errors.New("preparation: plan HTTP application is required")
	}
	return &PlanHTTPHandler{application: application}, nil
}

func (h *PlanHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/practice-plans", h.list)
	routes.POST("/v1/practice-plans", h.create)
	routes.GET("/v1/practice-plans/:practice_plan_id", h.read)
	routes.PUT("/v1/practice-plans/:practice_plan_id", h.revise)
	routes.DELETE("/v1/practice-plans/:practice_plan_id", h.archive)
}

func (h *PlanHTTPHandler) archive(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writePlanAuthenticationRequired(c)
		return
	}
	planID := c.Param("practice_plan_id")
	if !validPlanResourceID(planID) {
		writePlanHTTPError(c, http.StatusNotFound, "practice_plan_not_found")
		return
	}
	if err := h.application.ArchivePlan(
		c.Request.Context(),
		actor,
		planID,
	); err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PlanHTTPHandler) list(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writePlanAuthenticationRequired(c)
		return
	}
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query["practice_experience"]) != 1 {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	experience := scene.PracticeExperience(c.Query("practice_experience"))
	plans, err := h.application.ListPlans(
		c.Request.Context(),
		actor,
		experience,
	)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"practice_plans": plans})
}

func (h *PlanHTTPHandler) create(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writePlanAuthenticationRequired(c)
		return
	}
	key, ok := profileIdempotencyKey(c)
	if !ok {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var request CreatePlanRequest
	if !decodeProfileJSONObject(c, &request) ||
		!validCreatePlanRequest(request) {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	plan, _, err := h.application.CreatePlan(
		c.Request.Context(),
		actor,
		key,
		request,
	)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, plan)
}

func (h *PlanHTTPHandler) read(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writePlanAuthenticationRequired(c)
		return
	}
	planID := c.Param("practice_plan_id")
	if !validPlanResourceID(planID) {
		writePlanHTTPError(c, http.StatusNotFound, "practice_plan_not_found")
		return
	}
	plan, err := h.application.ReadPlan(c.Request.Context(), actor, planID)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *PlanHTTPHandler) revise(c *gin.Context) {
	setProfilePrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writePlanAuthenticationRequired(c)
		return
	}
	planID := c.Param("practice_plan_id")
	if !validPlanResourceID(planID) {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	key, ok := profileIdempotencyKey(c)
	if !ok {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var request RevisePlanRequest
	if !decodeProfileJSONObject(c, &request) ||
		!validRevisePlanRequest(request) {
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	plan, _, err := h.application.RevisePlan(
		c.Request.Context(),
		actor,
		planID,
		key,
		request,
	)
	if err != nil {
		writePlanServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func writePlanAuthenticationRequired(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	writePlanHTTPError(c, http.StatusUnauthorized, "authentication_required")
}

func writePlanServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPlanInvalid):
		writePlanHTTPError(c, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrPlanNotFound):
		writePlanHTTPError(c, http.StatusNotFound, "practice_plan_not_found")
	case errors.Is(err, ErrPlanIdempotencyConflict):
		writePlanHTTPError(c, http.StatusConflict, "idempotency_key_conflict")
	case errors.Is(err, ErrPlanConflict):
		writePlanHTTPError(
			c,
			http.StatusConflict,
			"practice_plan_revision_conflict",
		)
	default:
		writePlanHTTPError(c, http.StatusInternalServerError, "internal_error")
	}
}

func writePlanHTTPError(c *gin.Context, status int, code string) {
	messages := map[string]string{
		"invalid_request":                 "Request validation failed.",
		"authentication_required":         "Authentication is required.",
		"practice_plan_not_found":         "Practice plan was not found.",
		"practice_plan_revision_conflict": "Practice plan revision conflicts with this operation.",
		"idempotency_key_conflict":        "Idempotency key conflicts with the original request.",
		"internal_error":                  "An internal error occurred.",
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        messages[code],
			"retryable":      false,
			"correlation_id": newPreparationCorrelationID(),
		},
	})
}

var _ PlanHTTPApplication = (*PlanService)(nil)
