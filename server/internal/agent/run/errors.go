package run

import "errors"

var (
	ErrInvalidRequest      = errors.New("agent run: invalid request")
	ErrNotFound            = errors.New("agent run: not found")
	ErrConflict            = errors.New("agent run: conflict")
	ErrIdempotencyConflict = errors.New("agent run: idempotency conflict")
	ErrRepository          = errors.New("agent run repository: operation failed")
)

// loopFailure stops one Run without exposing model output or replaying tools.
// These failures are not retryable because earlier iterations may have
// already completed write capabilities.
type loopFailure struct {
	kind string
}

func (failure *loopFailure) Error() string {
	return "agent run failed: " + failure.kind
}
