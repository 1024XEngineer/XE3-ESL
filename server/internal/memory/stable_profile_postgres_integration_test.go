package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPostgresRepositoryListsStableProfileDeterministically(
	t *testing.T,
) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	ctx := context.Background()
	actorA := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	actorB := requestcontext.Actor{
		UserID:    integrationUserB,
		SessionID: integrationSessionB,
	}

	create := func(
		actor requestcontext.Actor,
		memoryType Type,
		key string,
		content string,
	) Memory {
		t.Helper()
		command := createCommand(key, content)
		command.Type = memoryType
		item, createErr := repository.Create(ctx, actor, command)
		if createErr != nil {
			t.Fatalf("Create %s: %v", key, createErr)
		}
		return item
	}

	occupation := create(
		actorA,
		TypeProfile,
		CanonicalCareerOccupation,
		"Java 后端工程师",
	)
	preferredName := create(
		actorA,
		TypeProfile,
		CanonicalProfilePreferredName,
		"小花",
	)
	formOfAddress := create(
		actorA,
		TypePreference,
		CanonicalPreferenceFormOfAddress,
		"花花",
	)
	experience := create(
		actorA,
		TypeProfile,
		CanonicalCareerExperienceYears,
		"5 年",
	)
	gender := create(
		actorA,
		TypeProfile,
		CanonicalProfileGender,
		"女性",
	)
	coachingStyle := create(
		actorA,
		TypePreference,
		CanonicalCoachingStyle,
		"回答简短，先给修改稿",
	)
	create(actorA, TypeInterest, "interest.running", "喜欢跑步")
	foreignName := create(
		actorB,
		TypeProfile,
		CanonicalProfilePreferredName,
		"小雨",
	)

	matterCommand := createCommand(
		CanonicalProfilePreferredName,
		"Matter 中的错误姓名",
	)
	matterCommand.Scope = ScopeMatter
	matterCommand.MatterID = integrationMatterA
	matterCommand.Source = evidence(
		SourceAgentMessage,
		"matter-profile-name",
		1,
		"matter profile name",
	)
	if _, err := repository.Create(
		ctx,
		actorA,
		matterCommand,
	); err != nil {
		t.Fatalf("Create Matter-scoped profile: %v", err)
	}
	if _, err := repository.Inactivate(ctx, actorA, InactivateCommand{
		MemoryID:        gender.ID,
		ExpectedVersion: gender.Version,
		Source: evidence(
			SourceAgentMessage,
			"forget-profile-gender",
			1,
			"forget profile gender",
		),
	}); err != nil {
		t.Fatalf("Inactivate gender: %v", err)
	}
	if _, err := database.Exec(ctx, `
UPDATE agent_memories
SET expires_at = created_at + INTERVAL '1 microsecond'
WHERE id = $1`,
		coachingStyle.ID,
	); err != nil {
		t.Fatalf("expire coaching style: %v", err)
	}

	items, err := repository.ListStableProfile(ctx, actorA)
	if err != nil {
		t.Fatalf("ListStableProfile actor A: %v", err)
	}
	expectedIDs := []string{
		preferredName.ID,
		formOfAddress.ID,
		occupation.ID,
		experience.ID,
	}
	if len(items) != len(expectedIDs) {
		t.Fatalf("Stable Profile actor A = %#v", items)
	}
	for index, expectedID := range expectedIDs {
		if items[index].ID != expectedID {
			t.Fatalf(
				"Stable Profile actor A item %d = %#v",
				index,
				items[index],
			)
		}
	}

	foreignItems, err := repository.ListStableProfile(ctx, actorB)
	if err != nil {
		t.Fatalf("ListStableProfile actor B: %v", err)
	}
	if len(foreignItems) != 1 || foreignItems[0].ID != foreignName.ID {
		t.Fatalf("Stable Profile actor B = %#v", foreignItems)
	}

	if _, err := database.Exec(ctx, `
UPDATE identity_users
SET account_status = 'deleting'
WHERE id = $1`,
		actorB.UserID,
	); err != nil {
		t.Fatalf("disable actor B: %v", err)
	}
	disabledItems, err := repository.ListStableProfile(ctx, actorB)
	if err != nil {
		t.Fatalf("ListStableProfile disabled actor B: %v", err)
	}
	if len(disabledItems) != 0 {
		t.Fatalf("disabled actor Stable Profile = %#v", disabledItems)
	}

	if _, err := repository.ListStableProfile(ctx, requestcontext.Actor{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid actor error = %v", err)
	}
}

func TestPostgresRepositoryListsEmptyStableProfile(t *testing.T) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	items, err := repository.ListStableProfile(
		context.Background(),
		requestcontext.Actor{
			UserID:    integrationUserA,
			SessionID: integrationSessionA,
		},
	)
	if err != nil {
		t.Fatalf("ListStableProfile: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty Stable Profile = %#v", items)
	}
}
