package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	practicepersistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

func TestAgentPracticeContextReaderRequiresExactActiveOwnedMatter(
	t *testing.T,
) {
	t.Parallel()

	actor := contextCompositionActor()
	tests := []struct {
		name        string
		thread      agent.Thread
		threadError error
		item        matter.Matter
		matterError error
		wantError   error
	}{
		{
			name: "exact active anchor",
			thread: agent.Thread{
				ID:             "thread-1",
				OwnerID:        actor.UserID,
				ActiveMatterID: "matter-1",
			},
			item: matter.Matter{
				ID:      "matter-1",
				OwnerID: actor.UserID,
				Status:  matter.StatusActive,
			},
		},
		{
			name: "different active matter",
			thread: agent.Thread{
				ID:             "thread-1",
				OwnerID:        actor.UserID,
				ActiveMatterID: "matter-other",
			},
			wantError: practicepersistence.ErrConflict,
		},
		{
			name: "thread from different owner",
			thread: agent.Thread{
				ID:             "thread-1",
				OwnerID:        "user-other",
				ActiveMatterID: "matter-1",
			},
			wantError: practicepersistence.ErrNotFound,
		},
		{
			name: "archived matter",
			thread: agent.Thread{
				ID:             "thread-1",
				OwnerID:        actor.UserID,
				ActiveMatterID: "matter-1",
			},
			item: matter.Matter{
				ID:      "matter-1",
				OwnerID: actor.UserID,
				Status:  matter.StatusArchived,
			},
			wantError: practicepersistence.ErrConflict,
		},
		{
			name:        "cross account thread is hidden",
			threadError: agent.ErrNotFound,
			wantError:   practicepersistence.ErrNotFound,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader, err := newAgentPracticeContextReader(
				agentThreadReaderStub{
					thread: test.thread,
					err:    test.threadError,
				},
				matterReaderStub{
					item: test.item,
					err:  test.matterError,
				},
			)
			if err != nil {
				t.Fatalf("newAgentPracticeContextReader: %v", err)
			}
			anchor, err := reader.ValidatePracticeAnchor(
				context.Background(),
				actor,
				"thread-1",
				"matter-1",
			)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ValidatePracticeAnchor error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePracticeAnchor: %v", err)
			}
			if anchor.ThreadID != "thread-1" ||
				anchor.MatterID != "matter-1" {
				t.Fatalf("anchor = %+v", anchor)
			}
		})
	}
}

func TestPreparationPracticeContextReaderUsesPublicSnapshotPort(
	t *testing.T,
) {
	t.Parallel()

	actor := contextCompositionActor()
	createdAt := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	reader, err := newPreparationPracticeContextReader(
		preparationReaderStub{
			profile: preparation.Profile{
				ID:      "profile-1",
				UserID:  actor.UserID,
				Version: 3,
			},
			snapshot: preparation.Snapshot{
				ID:                     "snapshot-1",
				SourceProfileID:        "profile-1",
				SourceVersion:          3,
				ResumeSnapshot:         "resume",
				JobDescriptionSnapshot: "job",
				BackgroundSnapshot:     "background",
				CreatedAt:              createdAt,
			},
		},
	)
	if err != nil {
		t.Fatalf("newPreparationPracticeContextReader: %v", err)
	}

	profile, err := reader.ReadPreparationProfile(
		context.Background(),
		actor,
		"profile-1",
	)
	if err != nil || profile.ID != "profile-1" || profile.Version != 3 {
		t.Fatalf("ReadPreparationProfile = (%+v, %v)", profile, err)
	}
	snapshot, err := reader.ReadPreparationSnapshot(
		context.Background(),
		actor,
		"snapshot-1",
	)
	if err != nil {
		t.Fatalf("ReadPreparationSnapshot: %v", err)
	}
	if snapshot.ID != "snapshot-1" ||
		snapshot.SourceProfileID != "profile-1" ||
		snapshot.SourceVersion != 3 ||
		snapshot.BackgroundSnapshot != "background" ||
		!snapshot.CreatedAt.Equal(createdAt) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestPracticeCatalogContextReaderRejectsStaleAndForgedSelections(
	t *testing.T,
) {
	t.Parallel()

	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := newPracticeCatalogContextReader(catalog)
	if err != nil {
		t.Fatal(err)
	}
	validPlan := practice.PlanCatalogRequest{
		ScenarioDefinitionID:      preparation.ProgrammerInterviewScenarioID,
		ScenarioDefinitionVersion: 1,
		ScenarioConfigID:          preparation.BackendEngineerConfigID,
		ScenarioConfigVersion:     1,
		SelectedRoleIDs: []string{
			preparation.TechnicalInterviewerRoleID,
		},
	}
	selection, err := reader.ReadPlanCatalog(validPlan)
	if err != nil {
		t.Fatalf("ReadPlanCatalog: %v", err)
	}
	if selection.ScenarioDefinition.ID !=
		preparation.ProgrammerInterviewScenarioID ||
		selection.ScenarioConfig.ID != preparation.BackendEngineerConfigID ||
		len(selection.SelectedRoles) != 1 ||
		selection.SelectedRoles[0].ID !=
			preparation.TechnicalInterviewerRoleID {
		t.Fatalf("selection = %+v", selection)
	}

	stale := validPlan
	stale.ScenarioConfigVersion = 2
	if _, err := reader.ReadPlanCatalog(stale); !errors.Is(
		err,
		practicepersistence.ErrConflict,
	) {
		t.Fatalf("stale catalog error = %v", err)
	}
	forged := validPlan
	forged.SelectedRoleIDs = []string{"role_forged"}
	if _, err := reader.ReadPlanCatalog(forged); !errors.Is(
		err,
		practicepersistence.ErrNotFound,
	) {
		t.Fatalf("forged role error = %v", err)
	}

	plan := practicepersistence.Plan{
		ScenarioDefinitionID:      preparation.ProgrammerInterviewScenarioID,
		ScenarioDefinitionVersion: 1,
		ScenarioConfigID:          preparation.BackendEngineerConfigID,
		ScenarioConfigVersion:     1,
		SelectedRoleIDs: []string{
			preparation.TechnicalInterviewerRoleID,
		},
	}
	session, err := reader.ReadSessionCatalog(
		practice.SessionCatalogRequest{
			Plan:             plan,
			PracticeOptionID: preparation.TechnicalFocusOptionID,
			RoleDefinitionIDs: []string{
				preparation.TechnicalInterviewerRoleID,
			},
		},
	)
	if err != nil {
		t.Fatalf("ReadSessionCatalog: %v", err)
	}
	if session.PracticeOption.ID != preparation.TechnicalFocusOptionID ||
		session.PracticeOption.Version != 1 {
		t.Fatalf("session selection = %+v", session)
	}
	if _, err := reader.ReadSessionCatalog(
		practice.SessionCatalogRequest{
			Plan:             plan,
			PracticeOptionID: "option_forged",
			RoleDefinitionIDs: []string{
				preparation.TechnicalInterviewerRoleID,
			},
		},
	); !errors.Is(err, practicepersistence.ErrNotFound) {
		t.Fatalf("forged option error = %v", err)
	}
}

type agentThreadReaderStub struct {
	thread agent.Thread
	err    error
}

func (s agentThreadReaderStub) GetThread(
	context.Context,
	requestcontext.Actor,
	string,
) (agent.Thread, error) {
	return s.thread, s.err
}

type matterReaderStub struct {
	item matter.Matter
	err  error
}

func (s matterReaderStub) ReadOwned(
	context.Context,
	requestcontext.Actor,
	string,
) (matter.Matter, error) {
	return s.item, s.err
}

type preparationReaderStub struct {
	profile  preparation.Profile
	snapshot preparation.Snapshot
	err      error
}

func (s preparationReaderStub) ReadProfile(
	context.Context,
	requestcontext.Actor,
	string,
) (preparation.Profile, error) {
	return s.profile, s.err
}

func (s preparationReaderStub) ReadSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (preparation.Snapshot, error) {
	return s.snapshot, s.err
}

func contextCompositionActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000112",
		SessionID: "20000000-0000-4000-8000-000000000112",
	}
}
