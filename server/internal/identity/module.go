package identity

import "github.com/gin-gonic/gin"

type Module struct {
	handler *HTTPHandler
}

func NewModule(handler *HTTPHandler) (*Module, error) {
	if handler == nil {
		return nil, ErrInvalidRequest
	}
	return &Module{handler: handler}, nil
}

func (m *Module) RegisterRoutes(router *gin.Engine) {
	m.handler.RegisterRoutes(router)
}
