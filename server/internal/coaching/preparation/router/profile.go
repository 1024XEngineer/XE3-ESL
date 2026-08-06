// Package router binds Preparation HTTP handlers to public routes. It owns no
// request decoding, application logic, or persistence dependency.
package router

import (
	"errors"

	preparationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/transport/http"
	"github.com/gin-gonic/gin"
)

type ProfileRouter struct {
	handler *preparationhttp.ProfileHTTPHandler
}

func NewProfile(handler *preparationhttp.ProfileHTTPHandler) (*ProfileRouter, error) {
	if handler == nil {
		return nil, errors.New("preparation: profile HTTP handler is required")
	}
	return &ProfileRouter{handler: handler}, nil
}

func (router *ProfileRouter) RegisterRoutes(routes gin.IRoutes) {
	router.handler.RegisterRoutes(routes)
}
