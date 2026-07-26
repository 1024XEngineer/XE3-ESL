package matter

import "errors"

var (
	ErrInvalidRequest = errors.New("matter: invalid request")
	ErrNotFound       = errors.New("matter: not found")
	ErrConflict       = errors.New("matter: conflict")
	ErrRepository     = errors.New("matter repository: operation failed")
)
