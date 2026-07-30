package agenttool

import (
	"context"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

type previewApplicationStub struct {
	calls          int
	actor          requestcontext.Actor
	idempotencyKey string
	request        practice.CreatePlanRequest
	plan           persistence.Plan
	replayed       bool
	err            error
}

func (stub *previewApplicationStub) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request practice.CreatePlanRequest,
) (persistence.Plan, bool, error) {
	stub.calls++
	stub.actor = actor
	stub.idempotencyKey = idempotencyKey
	stub.request = request
	return stub.plan, stub.replayed, stub.err
}

type previewCatalogStub struct {
	items []preparation.PreviewCatalogCandidate
	err   error
}

func (stub previewCatalogStub) ResolvePreviewCatalog(
	string,
) ([]preparation.PreviewCatalogCandidate, error) {
	return stub.items, stub.err
}

func TestServicePortNeedsInputDoesNotPersistPlan(t *testing.T) {
	application := &previewApplicationStub{}
	port, err := NewServicePort(application, previewCatalogStub{
		items: []preparation.PreviewCatalogCandidate{
			previewCatalogCandidate(),
		},
	})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	result, err := port.PreviewPractice(
		context.Background(),
		previewCallContext(),
		PreviewInput{ScenarioQuery: "项目经历深挖"},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	wantMissing := []string{
		"preparation_profile_id_or_snapshot_id",
		"max_effective_turns",
	}
	if result.Status != "needs_input" ||
		!reflect.DeepEqual(result.RequiredMissingFields, wantMissing) ||
		len(result.Candidates) != 1 ||
		application.calls != 0 {
		t.Fatalf("needs_input result = %#v, calls = %d", result, application.calls)
	}
}

func TestServicePortCreatesReadyPlanFromTrustedContext(t *testing.T) {
	candidate := previewCatalogCandidate()
	application := &previewApplicationStub{
		replayed: true,
		plan: persistence.Plan{
			ID:                   "plan-1",
			SelectedRoleIDs:      append([]string(nil), candidate.DefaultRoleIDs...),
			ScenarioType:         persistence.ScenarioFamilyInterview,
			ScenarioModel:        persistence.ScenarioModelProjectExperienceDeepDive,
			Revision:             1,
			Status:               persistence.PlanStatusReady,
			PreparationProfileID: "profile-1",
			CatalogSnapshot: &persistence.PlanCatalogSnapshot{
				ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
					Name: "项目经历深挖",
				},
				PracticeOption: persistence.PracticeOptionSnapshot{
					ID: candidate.DefaultOption.ID,
				},
			},
			SessionPolicy: &persistence.ContextSessionPolicy{
				MaxEffectiveTurns: 4,
			},
		},
	}
	port, err := NewServicePort(application, previewCatalogStub{})
	if err != nil {
		t.Fatalf("NewServicePort() error = %v", err)
	}
	call := previewCallContext()
	result, err := port.PreviewPractice(
		context.Background(),
		call,
		PreviewInput{
			MatterID:                  "matter-1",
			PreparationProfileID:      "profile-1",
			ScenarioDefinitionID:      candidate.ScenarioDefinition.ID,
			ScenarioDefinitionVersion: candidate.ScenarioDefinition.Version,
			ScenarioConfigID:          candidate.ScenarioConfig.ID,
			ScenarioConfigVersion:     candidate.ScenarioConfig.Version,
			SelectedRoleIDs:           candidate.DefaultRoleIDs,
			PracticeOptionID:          candidate.DefaultOption.ID,
			PracticeOptionVersion:     candidate.DefaultOption.Version,
			MaxEffectiveTurns:         4,
		},
	)
	if err != nil {
		t.Fatalf("PreviewPractice() error = %v", err)
	}
	if result.Status != "preview_ready" || result.PracticePlanID != "plan-1" ||
		!result.Replayed || len(result.SourceRefs) != 2 {
		t.Fatalf("ready result = %#v", result)
	}
	if application.actor != call.Actor ||
		application.idempotencyKey != call.RequestID ||
		application.request.AgentThreadID != call.ThreadID ||
		application.request.MaxEffectiveTurns != 4 {
		t.Fatalf(
			"trusted delegation = actor %#v key %q request %#v",
			application.actor,
			application.idempotencyKey,
			application.request,
		)
	}
}

func previewCallContext() tool.CallContext {
	return tool.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "user-1",
			SessionID: "session-1",
		},
		ThreadID:   "thread-1",
		RunID:      "run-1",
		ToolCallID: "tool-call-1",
		RequestID:  "request-1",
	}
}

func previewCatalogCandidate() preparation.PreviewCatalogCandidate {
	return preparation.PreviewCatalogCandidate{
		ScenarioDefinition: preparation.ScenarioDefinition{
			ID:      "scenario-1",
			Type:    preparation.ScenarioFamilyInterview,
			Model:   preparation.ScenarioModelProjectExperienceDeepDive,
			Name:    "项目经历深挖",
			Version: 1,
		},
		ScenarioConfig: preparation.ScenarioConfig{
			ID:      "config-1",
			Version: 1,
		},
		DefaultRoleIDs: []string{"role-1"},
		DefaultOption: preparation.PracticeOptionDefinition{
			ID:      "option-1",
			Version: 1,
		},
	}
}
