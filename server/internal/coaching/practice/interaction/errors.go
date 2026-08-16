package interaction

import (
	"errors"
	"regexp"
)

var (
	ErrInvalidRequest      = errors.New("practice interaction: invalid request")
	ErrInvalidContext      = errors.New("practice interaction: invalid context")
	ErrConflict            = errors.New("practice interaction: conflict")
	ErrIdempotencyConflict = errors.New(
		"practice interaction: idempotency conflict",
	)
	ErrNotFound   = errors.New("practice interaction: not found")
	ErrRepository = errors.New(
		"practice interaction: repository operation failed",
	)
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
