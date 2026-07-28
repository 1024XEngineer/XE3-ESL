package core

import "errors"

var (
	ErrInvalidRequest      = errors.New("agent: invalid request")
	ErrNotFound            = errors.New("agent: not found")
	ErrConflict            = errors.New("agent: conflict")
	ErrIdempotencyConflict = errors.New("agent: idempotency conflict")
	ErrInvalidContext      = errors.New("agent: invalid context")
	ErrRepository          = errors.New("agent repository: operation failed")
)
