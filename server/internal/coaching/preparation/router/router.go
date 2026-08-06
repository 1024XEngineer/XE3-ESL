// Package router binds Preparation HTTP handlers to public routes. It owns no
// request decoding, application logic, or persistence dependency.
package router

import (
	"errors"

	preparationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/transport/http"
	"github.com/gin-gonic/gin"
)

type Router struct {
	profiles   *preparationhttp.ProfileHTTPHandler
	jobTargets *preparationhttp.JobTargetHTTPHandler
	plans      *preparationhttp.PlanHTTPHandler
}

func New(
	profiles *preparationhttp.ProfileHTTPHandler,
	jobTargets *preparationhttp.JobTargetHTTPHandler,
	plans *preparationhttp.PlanHTTPHandler,
) (*Router, error) {
	if profiles == nil || jobTargets == nil || plans == nil {
		return nil, errors.New("preparation: HTTP handlers are required")
	}
	return &Router{
		profiles:   profiles,
		jobTargets: jobTargets,
		plans:      plans,
	}, nil
}

func (router *Router) RegisterRoutes(routes gin.IRoutes) {
	router.profiles.RegisterRoutes(routes)
	router.jobTargets.RegisterRoutes(routes)
	router.plans.RegisterRoutes(routes)
}
