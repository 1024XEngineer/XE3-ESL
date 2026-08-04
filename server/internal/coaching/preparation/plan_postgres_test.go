package preparation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPostgresPlanRepositoryRejectsInvalidBoundaryInput(t *testing.T) {
	t.Parallel()

	repository := NewPostgresPlanRepository(nil)
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000111",
		SessionID: "20000000-0000-4000-8000-000000000111",
	}
	if _, _, err := repository.ReplayPlan(
		context.Background(),
		actor,
		IdempotencyIntent{},
	); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("ReplayPlan error = %v", err)
	}
	if _, _, err := repository.CreatePlan(
		context.Background(),
		actor,
		CreatePlanCommand{},
	); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("CreatePlan error = %v", err)
	}
	if _, err := repository.ReadCurrentPlan(
		context.Background(),
		actor,
		"plan-1",
	); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("ReadCurrentPlan error = %v", err)
	}
	if _, _, err := repository.RevisePlan(
		context.Background(),
		actor,
		RevisePlanCommand{},
	); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("RevisePlan error = %v", err)
	}
	if _, err := repository.ReadExecutablePlan(
		context.Background(),
		actor,
		"plan-1",
		1,
	); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("ReadExecutablePlan error = %v", err)
	}
}

func TestDecodeStrictPlanJSONRejectsUnknownAndTrailingValues(t *testing.T) {
	t.Parallel()

	for _, document := range [][]byte{
		[]byte(`{"objective_id":"clarity","description":"clarity","extra":true}`),
		[]byte(`{"objective_id":"clarity","description":"clarity"}{}`),
	} {
		var objective PracticeObjective
		if err := decodeStrictPlanJSON(document, &objective); err == nil {
			t.Fatalf("decodeStrictPlanJSON(%s) succeeded", document)
		}
	}
}

func TestCanonicalStoredSceneSelectionDropsStorageOnlyOrder(t *testing.T) {
	t.Parallel()

	selection := planSelectionFixture()
	selection.Scene.DisplayOrder = 10
	selection.Scene.Roles[0].DisplayOrder = 20
	selection.Scene.PracticeOptions[0].DisplayOrder = 30
	stored, err := canonicalStoredSceneSelection(selection)
	if err != nil {
		t.Fatalf("canonicalStoredSceneSelection: %v", err)
	}
	if stored.Scene.DisplayOrder != 0 ||
		stored.Scene.Roles[0].DisplayOrder != 0 ||
		stored.Scene.PracticeOptions[0].DisplayOrder != 0 {
		t.Fatalf("stored selection retained display order: %#v", stored)
	}
	if stored.Scene.ID != selection.Scene.ID ||
		stored.PracticeOptionID != selection.PracticeOptionID {
		t.Fatalf("stored selection changed authority fields: %#v", stored)
	}
}

func TestClassifyPlanWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		want error
	}{
		{code: "23503", want: ErrPlanNotFound},
		{code: "23505", want: ErrPlanConflict},
		{code: "23514", want: ErrPlanConflict},
		{code: "08006", want: ErrPlanRepository},
	}
	for _, test := range tests {
		test := test
		t.Run(test.code, func(t *testing.T) {
			t.Parallel()
			err := classifyPlanWriteError(&pgconn.PgError{Code: test.code})
			if !errors.Is(err, test.want) {
				t.Fatalf("classifyPlanWriteError(%s) = %v", test.code, err)
			}
		})
	}
}
