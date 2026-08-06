package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type planHTTPApplicationStub struct {
	list func(
		context.Context,
		requestcontext.Actor,
		scene.PracticeExperience,
	) ([]PracticePlanSummary, error)
	create func(
		context.Context,
		requestcontext.Actor,
		string,
		CreatePlanRequest,
	) (PracticePlan, bool, error)
	read func(
		context.Context,
		requestcontext.Actor,
		string,
	) (PracticePlan, error)
	revise func(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		RevisePlanRequest,
	) (PracticePlan, bool, error)
	archive func(context.Context, requestcontext.Actor, string) error
}

func (s planHTTPApplicationStub) ListPlans(
	ctx context.Context,
	actor requestcontext.Actor,
	experience scene.PracticeExperience,
) ([]PracticePlanSummary, error) {
	if s.list == nil {
		return nil, errors.New("unexpected ListPlans call")
	}
	return s.list(ctx, actor, experience)
}

func TestPlanHTTPListsInterviewPlans(t *testing.T) {
	actor := profileHTTPActor()
	want := []PracticePlanSummary{{
		ID:                 "plan-1",
		Revision:           1,
		Status:             PlanStatusReady,
		PracticeExperience: scene.PracticeExperienceInterview,
		JobTitle:           "Backend Engineer",
	}}
	router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{
		list: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			experience scene.PracticeExperience,
		) ([]PracticePlanSummary, error) {
			if gotActor != actor || experience != scene.PracticeExperienceInterview {
				t.Fatalf("ListPlans = (%#v, %q)", gotActor, experience)
			}
			return want, nil
		},
	})
	response := serveProfileHTTPRequest(
		router,
		http.MethodGet,
		"/v1/practice-plans?practice_experience=INTERVIEW",
		"",
		"",
		"",
		&actor,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body)
	}
	var body struct {
		Plans []PracticePlanSummary `json:"practice_plans"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !reflect.DeepEqual(body.Plans, want) {
		t.Fatalf("plans = %#v, want %#v", body.Plans, want)
	}
}

func (s planHTTPApplicationStub) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	key string,
	request CreatePlanRequest,
) (PracticePlan, bool, error) {
	if s.create == nil {
		return PracticePlan{}, false, errors.New("unexpected CreatePlan call")
	}
	return s.create(ctx, actor, key, request)
}

func (s planHTTPApplicationStub) ReadPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (PracticePlan, error) {
	if s.read == nil {
		return PracticePlan{}, errors.New("unexpected ReadPlan call")
	}
	return s.read(ctx, actor, planID)
}

func (s planHTTPApplicationStub) RevisePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	key string,
	request RevisePlanRequest,
) (PracticePlan, bool, error) {
	if s.revise == nil {
		return PracticePlan{}, false, errors.New("unexpected RevisePlan call")
	}
	return s.revise(ctx, actor, planID, key, request)
}

func (s planHTTPApplicationStub) ArchivePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) error {
	if s.archive == nil {
		return errors.New("unexpected ArchivePlan call")
	}
	return s.archive(ctx, actor, planID)
}

func TestPlanHTTPCreateUsesOnlyCompletePreparationInput(t *testing.T) {
	actor := profileHTTPActor()
	wantRequest := CreatePlanRequest{
		SourceThreadID:        "thread-1",
		GoalID:                "goal-1",
		PreparationSnapshotID: "snapshot-1",
		SceneID:               "scene-1",
		SceneVersion:          2,
		SelectedRoleIDs:       []string{"role-1"},
		PracticeOptionID:      "option-1",
		MaxEffectiveTurns:     5,
	}
	wantPlan := PracticePlan{ID: "plan-1", UserID: actor.UserID}
	called := false
	router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{
		create: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			key string,
			request CreatePlanRequest,
		) (PracticePlan, bool, error) {
			called = true
			if gotActor != actor || key != "plan-create-key" ||
				!reflect.DeepEqual(request, wantRequest) {
				t.Fatalf("create input = (%+v, %q, %+v)", gotActor, key, request)
			}
			return wantPlan, true, nil
		},
	})

	response := serveProfileHTTPRequest(
		router,
		http.MethodPost,
		"/v1/practice-plans",
		`{"source_thread_id":"thread-1","goal_id":"goal-1",`+
			`"preparation_snapshot_id":"snapshot-1","scene_id":"scene-1",`+
			`"scene_version":2,"selected_role_ids":["role-1"],`+
			`"practice_option_id":"option-1","max_effective_turns":5}`,
		"application/json",
		"plan-create-key",
		&actor,
	)
	if !called || response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, called = %t, body = %s", response.Code, called, response.Body)
	}
	assertProfileHTTPNoStore(t, response)
}

func TestPlanHTTPReadAndReviseUseActorOwnedPlan(t *testing.T) {
	actor := profileHTTPActor()
	plan := PracticePlan{ID: "plan-1", UserID: actor.UserID, Revision: 2}
	router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{
		read: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			planID string,
		) (PracticePlan, error) {
			if gotActor != actor || planID != plan.ID {
				t.Fatalf("read input = (%+v, %q)", gotActor, planID)
			}
			return plan, nil
		},
		revise: func(
			_ context.Context,
			gotActor requestcontext.Actor,
			planID string,
			key string,
			request RevisePlanRequest,
		) (PracticePlan, bool, error) {
			if gotActor != actor || planID != plan.ID || key != "plan-revise-key" ||
				request.ExpectedPlanRevision != 2 ||
				!reflect.DeepEqual(request.SelectedRoleIDs, []string{"role-2"}) ||
				request.PracticeOptionID != "option-2" ||
				request.MaxEffectiveTurns != 3 {
				t.Fatalf("revise input = (%+v, %q, %q, %+v)", gotActor, planID, key, request)
			}
			plan.Revision = 3
			return plan, false, nil
		},
	})

	readResponse := serveProfileHTTPRequest(
		router,
		http.MethodGet,
		"/v1/practice-plans/plan-1",
		"",
		"",
		"",
		&actor,
	)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", readResponse.Code, readResponse.Body)
	}
	reviseResponse := serveProfileHTTPRequest(
		router,
		http.MethodPut,
		"/v1/practice-plans/plan-1",
		`{"expected_plan_revision":2,"selected_role_ids":["role-2"],`+
			`"practice_option_id":"option-2","max_effective_turns":3}`,
		"application/json",
		"plan-revise-key",
		&actor,
	)
	if reviseResponse.Code != http.StatusOK {
		t.Fatalf("revise status = %d, body = %s", reviseResponse.Code, reviseResponse.Body)
	}
}

func TestPlanHTTPRejectsPartialAndReplacedInputs(t *testing.T) {
	actor := profileHTTPActor()
	router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{})
	tests := []string{
		`{"source_thread_id":"thread-1","preparation_snapshot_id":"snapshot-1"}`,
		`{"source_thread_id":"thread-1","preparation_profile_id":"profile-1",` +
			`"scene_id":"scene-1","scene_version":1,"selected_role_ids":["role-1"],` +
			`"practice_option_id":"option-1"}`,
		`{"source_thread_id":"thread-1","preparation_snapshot_id":"snapshot-1",` +
			`"scene_id":"scene-1","scene_version":1,"selected_role_ids":["role-1","role-1"],` +
			`"practice_option_id":"option-1"}`,
	}
	for _, body := range tests {
		response := serveProfileHTTPRequest(
			router,
			http.MethodPost,
			"/v1/practice-plans",
			body,
			"application/json",
			"plan-invalid-key",
			&actor,
		)
		assertProfileHTTPError(t, response, http.StatusBadRequest, "invalid_request")
	}
}

func TestPlanHTTPMapsPlanErrors(t *testing.T) {
	actor := profileHTTPActor()
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{ErrPlanNotFound, http.StatusNotFound, "practice_plan_not_found"},
		{ErrPlanConflict, http.StatusConflict, "practice_plan_revision_conflict"},
		{ErrPlanIdempotencyConflict, http.StatusConflict, "idempotency_key_conflict"},
		{ErrPlanRepository, http.StatusInternalServerError, "internal_error"},
	} {
		router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{
			read: func(
				context.Context,
				requestcontext.Actor,
				string,
			) (PracticePlan, error) {
				return PracticePlan{}, test.err
			},
		})
		response := serveProfileHTTPRequest(
			router,
			http.MethodGet,
			"/v1/practice-plans/plan-1",
			"",
			"",
			"",
			&actor,
		)
		assertProfileHTTPError(t, response, test.status, test.code)
	}
}

func TestPlanHTTPRequiresAuthenticatedActor(t *testing.T) {
	router := newPlanHTTPTestRouter(t, planHTTPApplicationStub{})
	response := serveProfileHTTPRequest(
		router,
		http.MethodGet,
		"/v1/practice-plans/plan-1",
		"",
		"",
		"",
		nil,
	)
	assertProfileHTTPError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
	)
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
	}
}

func newPlanHTTPTestRouter(
	t *testing.T,
	application PlanHTTPApplication,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewPlanHTTPHandler(application)
	if err != nil {
		t.Fatalf("new Plan HTTP handler: %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}
