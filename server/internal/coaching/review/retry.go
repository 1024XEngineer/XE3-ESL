package review

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

var (
	ErrInvalidRequest   = errors.New("review: invalid request")
	ErrRetryUnavailable = errors.New("review: retry unavailable")
)

// RetryTurnCreator is the only application port used by Review HTTP. The
// PostgreSQL integration owns the same-database transaction and lock order.
type RetryTurnCreator interface {
	CreateTurn(context.Context, string, string, string) (practice.Turn, bool, error)
}
