package goal

import "errors"

var (
	ErrInvalidRequest = errors.New("goal: invalid request")
	ErrNotFound       = errors.New("goal: not found")
	ErrConflict       = errors.New("goal: conflict")
	ErrRepository     = errors.New("goal repository: operation failed")
)
