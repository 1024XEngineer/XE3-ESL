package conversation

import "errors"

var (
	ErrInvalidRequest      = errors.New("agent conversation: invalid request")
	ErrNotFound            = errors.New("agent conversation: not found")
	ErrConflict            = errors.New("agent conversation: conflict")
	ErrIdempotencyConflict = errors.New("agent conversation: idempotency conflict")
	ErrRepository          = errors.New("agent conversation repository: operation failed")
)
