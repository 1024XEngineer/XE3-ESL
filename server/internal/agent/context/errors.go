package context

import "errors"

var (
	ErrInvalidContext               = errors.New("agent context: invalid context")
	ErrNotFound                     = errors.New("agent context: not found")
	ErrConflict                     = errors.New("agent context: conflict")
	ErrRepository                   = errors.New("agent context repository: operation failed")
	ErrMemoryConsistencyUnavailable = errors.New(
		"agent context: memory consistency unavailable",
	)
	ErrMemoryConsistencyRejected = errors.New(
		"agent context: memory consistency rejected",
	)
)
