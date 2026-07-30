package core

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidRequest      = errors.New("agent: invalid request")
	ErrNotFound            = errors.New("agent: not found")
	ErrConflict            = errors.New("agent: conflict")
	ErrIdempotencyConflict = errors.New("agent: idempotency conflict")
	ErrInvalidContext      = errors.New("agent: invalid context")
	ErrRepository          = errors.New("agent repository: operation failed")
	ErrImageTooLarge       = fmt.Errorf("%w: image exceeds limits", ErrInvalidRequest)
	ErrUnsupportedImage    = fmt.Errorf("%w: image format is unsupported", ErrInvalidRequest)
	ErrInvalidImage        = fmt.Errorf("%w: image payload is invalid", ErrInvalidRequest)
)
