package identity

import "errors"

var (
	ErrInvalidRequest          = errors.New("identity: invalid request")
	ErrInvalidCredentials      = errors.New("identity: invalid credentials")
	ErrRegistrationUnavailable = errors.New("identity: registration unavailable")
	ErrAuthenticationRequired  = errors.New("identity: authentication required")
	ErrRateLimited             = errors.New("identity: rate limited")
	ErrNotFound                = errors.New("identity repository: not found")
	ErrConflict                = errors.New("identity repository: conflict")
	ErrRepository              = errors.New("identity repository: operation failed")
	ErrAuthenticationChanged   = errors.New("identity repository: authentication state changed")
	ErrPasswordUnavailable     = errors.New("identity: password hashing unavailable")
)
