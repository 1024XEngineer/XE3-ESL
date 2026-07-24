package identity

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type repositoryStub struct {
	createUser       func(context.Context, string, string, time.Time) (User, error)
	findCredential   func(context.Context, string) (Credential, error)
	createSession    func(context.Context, CreateSessionParams) (Session, error)
	findSession      func(context.Context, []byte, time.Time) (SessionIdentity, error)
	findUser         func(context.Context, string) (User, error)
	revokeSession    func(context.Context, string, string, time.Time, string) error
	revokeAllSession func(context.Context, string, time.Time, string) error
}

func (r repositoryStub) CreateUserWithCredential(
	ctx context.Context,
	email string,
	hash string,
	now time.Time,
) (User, error) {
	return r.createUser(ctx, email, hash, now)
}

func (r repositoryStub) FindCredentialByEmail(
	ctx context.Context,
	email string,
) (Credential, error) {
	return r.findCredential(ctx, email)
}

func (r repositoryStub) CreateSession(
	ctx context.Context,
	params CreateSessionParams,
) (Session, error) {
	return r.createSession(ctx, params)
}

func (r repositoryStub) FindSessionByTokenDigest(
	ctx context.Context,
	digest []byte,
	now time.Time,
) (SessionIdentity, error) {
	return r.findSession(ctx, digest, now)
}

func (r repositoryStub) FindUserByID(
	ctx context.Context,
	userID string,
) (User, error) {
	return r.findUser(ctx, userID)
}

func (r repositoryStub) RevokeSession(
	ctx context.Context,
	userID string,
	sessionID string,
	now time.Time,
	reason string,
) error {
	return r.revokeSession(ctx, userID, sessionID, now, reason)
}

func (r repositoryStub) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	now time.Time,
	reason string,
) error {
	return r.revokeAllSession(ctx, userID, now, reason)
}

type passwordHasherStub struct {
	hash   func(string) (string, error)
	verify func(string, string) (bool, bool, error)
}

func (h passwordHasherStub) Hash(password string) (string, error) {
	return h.hash(password)
}

func (h passwordHasherStub) Verify(
	password string,
	hash string,
) (bool, bool, error) {
	return h.verify(password, hash)
}

type tokenStub struct{}

func (tokenStub) Generate() (string, []byte, error) {
	return "sess_raw", []byte("digest"), nil
}
func (tokenStub) Digest(string) []byte        { return []byte("digest") }
func (tokenStub) ValidWireFormat(string) bool { return true }

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

func TestRegisterNormalizesEmailAndMapsConcurrentConflict(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	var gotEmail, gotHash string
	repository := completeRepositoryStub()
	repository.createUser = func(
		_ context.Context,
		email string,
		hash string,
		gotNow time.Time,
	) (User, error) {
		gotEmail, gotHash = email, hash
		if gotNow != now {
			t.Fatalf("unexpected time: %v", gotNow)
		}
		return User{}, ErrConflict
	}
	service := mustService(t, repository, passwordHasherStub{
		hash: func(password string) (string, error) {
			if password != "correct horse battery staple" {
				t.Fatalf("unexpected password")
			}
			return "phc-hash", nil
		},
		verify: func(string, string) (bool, bool, error) {
			return false, false, nil
		},
	}, now)

	_, err := service.Register(
		context.Background(),
		" Learner@Example.COM ",
		"correct horse battery staple",
	)
	if !errors.Is(err, ErrRegistrationUnavailable) {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotEmail != "learner@example.com" || gotHash != "phc-hash" {
		t.Fatalf("unexpected repository input: %q, %q", gotEmail, gotHash)
	}
}

func TestLoginUsesDummyHashForUnknownAccount(t *testing.T) {
	repository := completeRepositoryStub()
	repository.findCredential = func(context.Context, string) (Credential, error) {
		return Credential{}, ErrNotFound
	}
	var verifiedHash string
	service := mustService(t, repository, passwordHasherStub{
		hash: func(string) (string, error) { return "hash", nil },
		verify: func(_ string, hash string) (bool, bool, error) {
			verifiedHash = hash
			return true, false, nil
		},
	}, time.Now())

	_, err := service.Login(
		context.Background(),
		"unknown@example.com",
		"correct horse battery staple",
	)
	if !errors.Is(err, ErrInvalidCredentials) || verifiedHash != "dummy-hash" {
		t.Fatalf("login error/hash = %v, %q", err, verifiedHash)
	}
}

func TestLoginHidesUnavailableAccountState(t *testing.T) {
	for _, status := range []AccountStatus{
		AccountDisabled,
		AccountDeleting,
		AccountDeleted,
	} {
		t.Run(string(status), func(t *testing.T) {
			repository := completeRepositoryStub()
			repository.findCredential = func(
				context.Context,
				string,
			) (Credential, error) {
				return Credential{
					User: User{
						ID:     "user-1",
						Email:  "learner@example.com",
						Status: status,
					},
					PasswordHash: "stored-hash",
				}, nil
			}
			service := mustService(t, repository, defaultHasherStub(), time.Now())
			_, err := service.Login(
				context.Background(),
				"learner@example.com",
				"correct horse battery staple",
			)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoginCreatesFiniteSessionAndCarriesRehashAtomically(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	user := User{
		ID:     "user-1",
		Email:  "learner@example.com",
		Status: AccountActive,
	}
	repository := completeRepositoryStub()
	repository.findCredential = func(context.Context, string) (Credential, error) {
		return Credential{User: user, PasswordHash: "old-hash"}, nil
	}
	var got CreateSessionParams
	repository.createSession = func(
		_ context.Context,
		params CreateSessionParams,
	) (Session, error) {
		got = params
		return Session{
			ID:        "session-1",
			UserID:    user.ID,
			ExpiresAt: params.ExpiresAt,
		}, nil
	}
	service := mustService(t, repository, passwordHasherStub{
		hash: func(string) (string, error) { return "new-hash", nil },
		verify: func(_, hash string) (bool, bool, error) {
			return hash == "old-hash", true, nil
		},
	}, now)

	result, err := service.Login(
		context.Background(),
		"learner@example.com",
		"correct horse battery staple",
	)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Token != "sess_raw" ||
		result.ExpiresAt != now.Add(30*24*time.Hour) ||
		got.PreviousHash != "old-hash" ||
		got.ReplacementHash != "new-hash" ||
		!reflect.DeepEqual(got.TokenDigest, []byte("digest")) {
		t.Fatalf("unexpected login result/params: %#v, %#v", result, got)
	}
}

func TestAuthenticateSessionRejectsExpiredSession(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	repository := completeRepositoryStub()
	repository.findSession = func(
		context.Context,
		[]byte,
		time.Time,
	) (SessionIdentity, error) {
		return SessionIdentity{
			User:      User{ID: "user-1", Status: AccountActive},
			SessionID: "session-1",
			ExpiresAt: now,
		}, nil
	}
	service := mustService(t, repository, defaultHasherStub(), now)

	if _, err := service.AuthenticateSession(
		context.Background(),
		"sess_raw",
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogoutUsesOnlyTrustedActor(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	repository := completeRepositoryStub()
	var got requestcontext.Actor
	repository.revokeSession = func(
		_ context.Context,
		userID string,
		sessionID string,
		revokedAt time.Time,
		reason string,
	) error {
		got = requestcontext.Actor{UserID: userID, SessionID: sessionID}
		if revokedAt != now || reason != "logout" {
			t.Fatalf("unexpected revoke metadata: %v, %q", revokedAt, reason)
		}
		return nil
	}
	service := mustService(t, repository, defaultHasherStub(), now)
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	if err := service.Logout(context.Background(), actor); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if got != actor {
		t.Fatalf("unexpected actor: %#v", got)
	}
}

func TestConcurrentRegistrationHasOneWinner(t *testing.T) {
	const attempts = 8
	var created atomic.Int32
	repository := completeRepositoryStub()
	repository.createUser = func(
		context.Context,
		string,
		string,
		time.Time,
	) (User, error) {
		if created.Add(1) == 1 {
			return User{
				ID:     "user-1",
				Email:  "learner@example.com",
				Status: AccountActive,
			}, nil
		}
		return User{}, ErrConflict
	}
	service := mustService(t, repository, defaultHasherStub(), time.Now())

	var successes atomic.Int32
	var unavailable atomic.Int32
	var wait sync.WaitGroup
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.Register(
				context.Background(),
				"learner@example.com",
				"correct horse battery staple",
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrRegistrationUnavailable):
				unavailable.Add(1)
			default:
				t.Errorf("unexpected registration error: %v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || unavailable.Load() != attempts-1 {
		t.Fatalf(
			"successes/unavailable = %d/%d",
			successes.Load(),
			unavailable.Load(),
		)
	}
}

func TestRepeatedLogoutIsIdempotentAtRepositoryBoundary(t *testing.T) {
	repository := completeRepositoryStub()
	var calls atomic.Int32
	repository.revokeSession = func(
		context.Context,
		string,
		string,
		time.Time,
		string,
	) error {
		calls.Add(1)
		return nil
	}
	service := mustService(t, repository, defaultHasherStub(), time.Now())
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	for range 2 {
		if err := service.Logout(context.Background(), actor); err != nil {
			t.Fatalf("logout: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("revoke calls = %d, want 2", calls.Load())
	}
}

func mustService(
	t *testing.T,
	repository Repository,
	hasher PasswordHasher,
	now time.Time,
) *Service {
	t.Helper()
	service, err := NewService(
		repository,
		hasher,
		tokenStub{},
		fixedClock(now),
		"dummy-hash",
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func defaultHasherStub() PasswordHasher {
	return passwordHasherStub{
		hash: func(string) (string, error) { return "hash", nil },
		verify: func(string, string) (bool, bool, error) {
			return true, false, nil
		},
	}
}

func completeRepositoryStub() repositoryStub {
	return repositoryStub{
		createUser: func(context.Context, string, string, time.Time) (User, error) {
			return User{}, nil
		},
		findCredential: func(context.Context, string) (Credential, error) {
			return Credential{}, ErrNotFound
		},
		createSession: func(
			context.Context,
			CreateSessionParams,
		) (Session, error) {
			return Session{}, nil
		},
		findSession: func(
			context.Context,
			[]byte,
			time.Time,
		) (SessionIdentity, error) {
			return SessionIdentity{}, ErrNotFound
		},
		findUser: func(context.Context, string) (User, error) {
			return User{}, ErrNotFound
		},
		revokeSession: func(
			context.Context,
			string,
			string,
			time.Time,
			string,
		) error {
			return nil
		},
		revokeAllSession: func(
			context.Context,
			string,
			time.Time,
			string,
		) error {
			return nil
		},
	}
}
