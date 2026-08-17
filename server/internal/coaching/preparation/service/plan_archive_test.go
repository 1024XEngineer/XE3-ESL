package service

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type archivePlanRepository struct {
	actor requestcontext.Actor
	id    string
	err   error
}

func (*archivePlanRepository) CreatePlan(context.Context, requestcontext.Actor, preparation.CreatePlanCommand) (preparation.PracticePlan, bool, error) {
	return preparation.PracticePlan{}, false, nil
}

func (*archivePlanRepository) ReadCurrentPlan(context.Context, requestcontext.Actor, string) (preparation.PracticePlan, error) {
	return preparation.PracticePlan{}, nil
}

func (*archivePlanRepository) ListCurrentPlans(context.Context, requestcontext.Actor, scene.PracticeExperience) ([]preparation.PracticePlan, error) {
	return nil, nil
}

func (repository *archivePlanRepository) ArchivePlan(_ context.Context, actor requestcontext.Actor, id string) error {
	repository.actor = actor
	repository.id = id
	return repository.err
}

func (*archivePlanRepository) ConfirmPlan(context.Context, requestcontext.Actor, preparation.ConfirmPlanCommand) (preparation.PracticePlan, bool, error) {
	return preparation.PracticePlan{}, false, nil
}

func (*archivePlanRepository) ReadExecutablePlan(context.Context, requestcontext.Actor, string, int) (preparation.PracticePlan, error) {
	return preparation.PracticePlan{}, nil
}

func TestArchivePlanDelegatesOwnedAggregate(t *testing.T) {
	repository := &archivePlanRepository{}
	service := &PlanService{repository: repository}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

	if err := service.ArchivePlan(context.Background(), actor, id); err != nil {
		t.Fatal(err)
	}
	if repository.actor != actor || repository.id != id {
		t.Fatalf("archive actor=%#v id=%q", repository.actor, repository.id)
	}
}

func TestArchivePlanRejectsInvalidIdentityOrIDAsNotFound(t *testing.T) {
	tests := []struct {
		name  string
		ctx   context.Context
		actor requestcontext.Actor
		id    string
	}{
		{"nil context", nil, requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{"invalid actor", context.Background(), requestcontext.Actor{}, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{"invalid id", context.Background(), requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}, "not-an-id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &archivePlanRepository{}
			service := &PlanService{repository: repository}
			if err := service.ArchivePlan(test.ctx, test.actor, test.id); !errors.Is(err, preparation.ErrPlanNotFound) {
				t.Fatalf("error=%v", err)
			}
			if repository.id != "" {
				t.Fatalf("repository unexpectedly called with %q", repository.id)
			}
		})
	}
}

func TestArchivePlanPreservesRepositoryNotFound(t *testing.T) {
	repository := &archivePlanRepository{err: preparation.ErrPlanNotFound}
	service := &PlanService{repository: repository}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	if err := service.ArchivePlan(context.Background(), actor, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("error=%v", err)
	}
}
