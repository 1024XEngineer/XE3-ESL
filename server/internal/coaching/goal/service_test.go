package goal

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type serviceRepositoryStub struct {
	createOwner   string
	createRequest string
	createTitle   string
	searchOwner   string
	searchQuery   SearchQuery
}

func (stub *serviceRepositoryStub) Create(
	context.Context,
	string,
	string,
) (Goal, error) {
	return Goal{}, nil
}

func (stub *serviceRepositoryStub) CreateIdempotent(
	ctx context.Context,
	ownerID string,
	requestID string,
	title string,
) (Goal, error) {
	stub.createOwner = ownerID
	stub.createRequest = requestID
	stub.createTitle = title
	return Goal{ID: "goal-1", OwnerID: ownerID, Title: title}, nil
}

func (stub *serviceRepositoryStub) ListOwned(
	context.Context,
	string,
) ([]Goal, error) {
	return nil, nil
}

func (stub *serviceRepositoryStub) SearchOwned(
	ctx context.Context,
	ownerID string,
	query SearchQuery,
) ([]Goal, error) {
	stub.searchOwner = ownerID
	stub.searchQuery = query
	return []Goal{{ID: "goal-1", OwnerID: ownerID}}, nil
}

func (stub *serviceRepositoryStub) FindOwned(
	context.Context,
	string,
	string,
) (Goal, error) {
	return Goal{}, ErrNotFound
}

func (stub *serviceRepositoryStub) UpdateStatus(
	context.Context,
	string,
	string,
	int64,
	Status,
) (Goal, error) {
	return Goal{}, nil
}

func TestServiceCreateIdempotentNormalizesAndScopesRequest(t *testing.T) {
	repository := &serviceRepositoryStub{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	if _, err := service.CreateIdempotent(
		context.Background(),
		actor,
		"request-1",
		"  Customer meeting  ",
	); err != nil {
		t.Fatalf("CreateIdempotent() error = %v", err)
	}
	if repository.createOwner != actor.UserID ||
		repository.createRequest != "request-1" ||
		repository.createTitle != "Customer meeting" {
		t.Fatalf(
			"create delegation = owner %q request %q title %q",
			repository.createOwner,
			repository.createRequest,
			repository.createTitle,
		)
	}
}

func TestServiceSearchNormalizesAndBoundsQuery(t *testing.T) {
	repository := &serviceRepositoryStub{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	if _, err := service.Search(context.Background(), actor, SearchQuery{
		Query: "  interview  ",
	}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if repository.searchOwner != actor.UserID ||
		repository.searchQuery.Query != "interview" ||
		repository.searchQuery.Limit != defaultGoalSearchLimit {
		t.Fatalf(
			"search delegation = owner %q query %#v",
			repository.searchOwner,
			repository.searchQuery,
		)
	}
	if _, err := service.Search(context.Background(), actor, SearchQuery{
		Query: "interview",
		Limit: MaxGoalSearchLimit + 1,
	}); err != ErrInvalidRequest {
		t.Fatalf("over-limit Search() error = %v, want invalid request", err)
	}
}
