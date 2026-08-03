package run

import "errors"

var (
	ErrInvalidRequest      = errors.New("agent run: invalid request")
	ErrNotFound            = errors.New("agent run: not found")
	ErrConflict            = errors.New("agent run: conflict")
	ErrIdempotencyConflict = errors.New("agent run: idempotency conflict")
	ErrRepository          = errors.New("agent run repository: operation failed")
)
