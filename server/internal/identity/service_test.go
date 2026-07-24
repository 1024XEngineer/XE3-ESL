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
	createUser       func(context.Context, string, string) (User, error)
	findCredential   func(context.Context, string) (Credential, error)
	createSession    func(context.Context, CreateSessionParams) (Session, error)
	findSession      func(context.Context, []byte) (SessionIdentity, error)
	findUser         func(context.Context, string) (User, error)
	revokeSession    func(context.Context, string, string, string) error
	revokeAllSession func(context.Context, string, string) error
}

func (r repositoryStub) CreateUserWithCredential(
	ctx context.Context,
	email string,
	hash string,
) (User, error) {
	return r.createUser(ctx, email, hash)
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
) (SessionIdentity, error) {
	return r.findSession(ctx, digest)
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
	reason string,
) error {
	return r.revokeSession(ctx, userID, sessionID, reason)
}

func (r repositoryStub) RevokeAllSessionsForUser(
	ctx context.Context,
	userID string,
	reason string,
) error {
	return r.revokeAllSession(ctx, userID, reason)
}

type passwordHasherStub struct {
	hash   func(context.Context, string) (string, error)
	verify func(context.Context, string, string) (bool, bool, error)
}

func (h passwordHasherStub) Hash(
	ctx context.Context,
	password string,
) (string, error) {
	return h.hash(ctx, password)
}

func (h passwordHasherStub) Verify(
	ctx context.Context,
	password string,
	hash string,
) (bool, bool, error) {
	return h.verify(ctx, password, hash)
}

type tokenStub struct{}

func (tokenStub) Generate() (string, []byte, error) {
	return "sess_raw", []byte("digest"), nil
}
func (tokenStub) Digest(string) []byte        { return []byte("digest") }
func (tokenStub) ValidWireFormat(string) bool { return true }

func TestRegisterNormalizesEmailAndMapsConcurrentConflict(t *testing.T) {
	var gotEmail, gotHash string
	repository := completeRepositoryStub()
	repository.createUser = func(
		_ context.Context,
		email string,
		hash string,
	) (User, error) {
		gotEmail, gotHash = email, hash
		return User{}, ErrConflict
	}
	service := mustService(t, repository, passwordHasherStub{
		hash: func(_ context.Context, password string) (string, error) {
			if password != "correct horse battery staple" {
				t.Fatalf("unexpected password")
			}
			return "phc-hash", nil
		},
		verify: func(context.Context, string, string) (bool, bool, error) {
			return false, false, nil
		},
	}, time.Time{})

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
		hash: func(context.Context, string) (string, error) { return "hash", nil },
		verify: func(_ context.Context, _ string, hash string) (bool, bool, error) {
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
		return Credential{
			User:         user,
			PasswordHash: "old-hash",
			UpdatedAt:    now,
		}, nil
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
			ExpiresAt: now.Add(params.Lifetime),
		}, nil
	}
	service := mustService(t, repository, passwordHasherStub{
		hash: func(context.Context, string) (string, error) { return "new-hash", nil },
		verify: func(_ context.Context, _, hash string) (bool, bool, error) {
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
		got.CredentialUpdatedAt != now ||
		got.Lifetime != sessionLifetime ||
		!reflect.DeepEqual(got.TokenDigest, []byte("digest")) {
		t.Fatalf("unexpected login result/params: %#v, %#v", result, got)
	}
}

func TestLoginHidesAuthenticationStateChangedDuringSessionCommit(t *testing.T) {
	repository := completeRepositoryStub()
	repository.findCredential = func(context.Context, string) (Credential, error) {
		return Credential{
			User: User{
				ID:     "user-1",
				Email:  "learner@example.com",
				Status: AccountActive,
			},
			PasswordHash: "stored-hash",
			UpdatedAt:    time.Unix(1_000, 0),
		}, nil
	}
	repository.createSession = func(
		context.Context,
		CreateSessionParams,
	) (Session, error) {
		return Session{}, ErrAuthenticationChanged
	}
	service := mustService(t, repository, defaultHasherStub(), time.Time{})
	if _, err := service.Login(
		context.Background(),
		"learner@example.com",
		"correct horse battery staple",
	); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("authentication state change leaked: %v", err)
	}
}

func TestServicePropagatesPasswordContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := completeRepositoryStub()
	service := mustService(t, repository, passwordHasherStub{
		hash: func(got context.Context, _ string) (string, error) {
			if !errors.Is(got.Err(), context.Canceled) {
				t.Fatalf("hash context was not propagated: %v", got.Err())
			}
			return "", got.Err()
		},
		verify: func(context.Context, string, string) (bool, bool, error) {
			return false, false, nil
		},
	}, time.Time{})
	if _, err := service.Register(
		ctx,
		"learner@example.com",
		"correct horse battery staple",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("register cancellation = %v", err)
	}
}

func TestAuthenticateSessionMapsRepositoryExpiryToAuthenticationRequired(t *testing.T) {
	repository := completeRepositoryStub()
	repository.findSession = func(
		context.Context,
		[]byte,
	) (SessionIdentity, error) {
		return SessionIdentity{}, ErrNotFound
	}
	service := mustService(t, repository, defaultHasherStub(), time.Time{})

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
		reason string,
	) error {
		got = requestcontext.Actor{UserID: userID, SessionID: sessionID}
		if reason != "logout" {
			t.Fatalf("unexpected revoke reason: %q", reason)
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
		"dummy-hash",
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func defaultHasherStub() PasswordHasher {
	return passwordHasherStub{
		hash: func(context.Context, string) (string, error) { return "hash", nil },
		verify: func(context.Context, string, string) (bool, bool, error) {
			return true, false, nil
		},
	}
}

func completeRepositoryStub() repositoryStub {
	return repositoryStub{
		createUser: func(context.Context, string, string) (User, error) {
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
			string,
		) error {
			return nil
		},
		revokeAllSession: func(
			context.Context,
			string,
			string,
		) error {
			return nil
		},
	}
}
