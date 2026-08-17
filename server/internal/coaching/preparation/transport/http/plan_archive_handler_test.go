package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const archivePlanID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type archivePlanApplication struct {
	archiveActor requestcontext.Actor
	archiveID    string
	archiveErr   error
}

func (*archivePlanApplication) ListPlans(context.Context, requestcontext.Actor, scene.PracticeExperience) ([]PracticePlanSummary, error) {
	return nil, nil
}

func (*archivePlanApplication) CreatePlan(context.Context, requestcontext.Actor, string, CreatePlanRequest) (PracticePlan, bool, error) {
	return PracticePlan{}, false, nil
}

func (*archivePlanApplication) ReadPlan(context.Context, requestcontext.Actor, string) (PracticePlan, error) {
	return PracticePlan{}, nil
}

func (application *archivePlanApplication) ArchivePlan(_ context.Context, actor requestcontext.Actor, id string) error {
	application.archiveActor = actor
	application.archiveID = id
	return application.archiveErr
}

func (*archivePlanApplication) ConfirmPlan(context.Context, requestcontext.Actor, string, string, ConfirmPlanRequest) (PracticePlan, bool, error) {
	return PracticePlan{}, false, nil
}

func TestArchivePlanRouteReturnsNoContent(t *testing.T) {
	application := &archivePlanApplication{}
	response := performArchivePlanRequest(t, application, archivePlanID)

	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("response status=%d body=%q", response.Code, response.Body.String())
	}
	if application.archiveActor.UserID != "11111111-1111-4111-8111-111111111111" || application.archiveID != archivePlanID {
		t.Fatalf("archive actor=%#v id=%q", application.archiveActor, application.archiveID)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers=%v", response.Header())
	}
}

func TestArchivePlanRouteHidesMissingOrForeignPlans(t *testing.T) {
	application := &archivePlanApplication{archiveErr: ErrPlanNotFound}
	response := performArchivePlanRequest(t, application, archivePlanID)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestArchivePlanRouteRejectsMalformedIDWithoutCallingApplication(t *testing.T) {
	application := &archivePlanApplication{}
	response := performArchivePlanRequest(t, application, strings.Repeat("a", 129))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if application.archiveID != "" {
		t.Fatalf("application unexpectedly called with %q", application.archiveID)
	}
}

func performArchivePlanRequest(t *testing.T, application *archivePlanApplication, id string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewPlanHTTPHandler(application)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		actor := requestcontext.Actor{UserID: "11111111-1111-4111-8111-111111111111", SessionID: "test-session"}
		c.Request = c.Request.WithContext(requestcontext.WithActor(c.Request.Context(), actor))
		c.Next()
	})
	handler.RegisterRoutes(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/v1/practice-plans/"+id, nil))
	return response
}
