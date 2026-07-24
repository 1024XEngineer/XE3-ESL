package identity

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	sessionLifetime = 30 * 24 * time.Hour
	logoutReason    = "logout"
)

type Authenticator interface {
	AuthenticateSession(
		ctx context.Context,
		rawToken string,
	) (requestcontext.Actor, error)
}

type Service struct {
	repository Repository
	passwords  PasswordHasher
	tokens     SessionTokens
	clock      Clock
	dummyHash  string
}

func NewService(
	repository Repository,
	passwords PasswordHasher,
	tokens SessionTokens,
	clock Clock,
	dummyHash string,
) (*Service, error) {
	if repository == nil || passwords == nil || tokens == nil || clock == nil ||
		dummyHash == "" {
		return nil, errors.New("identity: service dependency is required")
	}
	return &Service{
		repository: repository,
		passwords:  passwords,
		tokens:     tokens,
		clock:      clock,
		dummyHash:  dummyHash,
	}, nil
}

func (s *Service) Register(
	ctx context.Context,
	email string,
	password string,
) (User, error) {
	canonicalEmail, err := NormalizeEmail(email)
	if err != nil || ValidatePassword(password) != nil {
		return User{}, ErrInvalidRequest
	}
	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return User{}, err
	}

	user, err := s.repository.CreateUserWithCredential(
		ctx,
		canonicalEmail,
		passwordHash,
		s.clock.Now().UTC(),
	)
	if errors.Is(err, ErrConflict) {
		return User{}, ErrRegistrationUnavailable
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) Login(
	ctx context.Context,
	email string,
	password string,
) (LoginResult, error) {
	canonicalEmail, err := NormalizeEmail(email)
	if err != nil || ValidatePassword(password) != nil {
		return LoginResult{}, ErrInvalidRequest
	}

	credential, repositoryErr := s.repository.FindCredentialByEmail(
		ctx,
		canonicalEmail,
	)
	encodedHash := credential.PasswordHash
	if errors.Is(repositoryErr, ErrNotFound) {
		encodedHash = s.dummyHash
	} else if repositoryErr != nil {
		return LoginResult{}, repositoryErr
	}

	valid, needsRehash, err := s.passwords.Verify(password, encodedHash)
	if err != nil {
		return LoginResult{}, err
	}
	if repositoryErr != nil || !valid || credential.User.Status != AccountActive {
		return LoginResult{}, ErrInvalidCredentials
	}

	var replacementHash string
	if needsRehash {
		replacementHash, err = s.passwords.Hash(password)
		if err != nil {
			return LoginResult{}, err
		}
	}

	rawToken, tokenDigest, err := s.tokens.Generate()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.clock.Now().UTC()
	session, err := s.repository.CreateSession(ctx, CreateSessionParams{
		UserID:          credential.User.ID,
		TokenDigest:     tokenDigest,
		CreatedAt:       now,
		ExpiresAt:       now.Add(sessionLifetime),
		PreviousHash:    credential.PasswordHash,
		ReplacementHash: replacementHash,
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		User:      credential.User,
		Token:     rawToken,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (s *Service) AuthenticateSession(
	ctx context.Context,
	rawToken string,
) (requestcontext.Actor, error) {
	if !s.tokens.ValidWireFormat(rawToken) {
		return requestcontext.Actor{}, ErrAuthenticationRequired
	}
	now := s.clock.Now().UTC()
	session, err := s.repository.FindSessionByTokenDigest(
		ctx,
		s.tokens.Digest(rawToken),
		now,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return requestcontext.Actor{}, ErrAuthenticationRequired
		}
		return requestcontext.Actor{}, err
	}
	if session.User.Status != AccountActive ||
		session.SessionID == "" ||
		!session.ExpiresAt.After(now) {
		return requestcontext.Actor{}, ErrAuthenticationRequired
	}
	actor := requestcontext.Actor{
		UserID:    session.User.ID,
		SessionID: session.SessionID,
	}
	if !actor.Valid() {
		return requestcontext.Actor{}, ErrAuthenticationRequired
	}
	return actor, nil
}

func (s *Service) Logout(
	ctx context.Context,
	actor requestcontext.Actor,
) error {
	if !actor.Valid() {
		return ErrAuthenticationRequired
	}
	return s.repository.RevokeSession(
		ctx,
		actor.UserID,
		actor.SessionID,
		s.clock.Now().UTC(),
		logoutReason,
	)
}

func (s *Service) CurrentUser(
	ctx context.Context,
	actor requestcontext.Actor,
) (User, error) {
	if !actor.Valid() {
		return User{}, ErrAuthenticationRequired
	}
	user, err := s.repository.FindUserByID(ctx, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrAuthenticationRequired
	}
	if err != nil {
		return User{}, err
	}
	if user.Status != AccountActive {
		return User{}, ErrAuthenticationRequired
	}
	return user, nil
}

func (s *Service) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	reason string,
) error {
	if userID == "" || reason == "" {
		return ErrInvalidRequest
	}
	return s.repository.RevokeAllSessionsForUser(
		ctx,
		userID,
		s.clock.Now().UTC(),
		reason,
	)
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
