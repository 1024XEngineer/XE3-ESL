package voice

import (
	"errors"
	"regexp"
)

var (
	ErrInvalidRequest      = errors.New("practice voice: invalid request")
	ErrInvalidContext      = errors.New("practice voice: invalid context")
	ErrConflict            = errors.New("practice voice: conflict")
	ErrIdempotencyConflict = errors.New(
		"practice voice: idempotency conflict",
	)
	ErrNotFound   = errors.New("practice voice: not found")
	ErrRepository = errors.New(
		"practice voice: repository operation failed",
	)
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
