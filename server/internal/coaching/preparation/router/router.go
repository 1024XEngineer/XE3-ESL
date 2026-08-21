// Package router binds Preparation HTTP handlers to public routes. It owns no
// request decoding, application logic, or persistence dependency.
package router

import (
	"errors"

	preparationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/transport/http"
	"github.com/gin-gonic/gin"
)

type Router struct {
	interviews *preparationhttp.InterviewPreparationHTTPHandler
	plans      *preparationhttp.PlanHTTPHandler
}

func New(
	interviews *preparationhttp.InterviewPreparationHTTPHandler,
	plans *preparationhttp.PlanHTTPHandler,
) (*Router, error) {
	if interviews == nil || plans == nil {
		return nil, errors.New("preparation: HTTP handlers are required")
	}
	return &Router{
		interviews: interviews,
		plans:      plans,
	}, nil
}

func (router *Router) RegisterRoutes(routes gin.IRoutes) {
	router.interviews.RegisterRoutes(routes)
	router.plans.RegisterRoutes(routes)
}
