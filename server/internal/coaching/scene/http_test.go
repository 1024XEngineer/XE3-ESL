package scene

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCatalogHTTPRoutesExposeCanonicalScenes(t *testing.T) {
	router := newCatalogTestRouter(t, mustTestCatalog(t))
	paths := []string{
		"/v1/scenes",
		"/v1/scenes/" + testSceneID,
		"/v1/scenes/" + testSceneID + "/roles",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			first := serveCatalogRequest(router, path)
			second := serveCatalogRequest(router, path)
			if first.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
			}
			if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
				t.Fatal("repeated catalog response changed")
			}
			if strings.Contains(first.Body.String(), "display_order") ||
				strings.Contains(first.Body.String(), "scenario_") ||
				strings.Contains(first.Body.String(), "prompt_model") {
				t.Fatalf("legacy/internal field leaked: %s", first.Body.String())
			}
		})
	}

	listResponse := serveCatalogRequest(router, "/v1/scenes")
	var list struct {
		Scenes []SceneDefinition `json:"scenes"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode scene list: %v", err)
	}
	if len(list.Scenes) != 1 {
		t.Fatalf("Scene count = %d", len(list.Scenes))
	}

	detailResponse := serveCatalogRequest(router, "/v1/scenes/"+testSceneID)
	var definition SceneDefinition
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &definition); err != nil {
		t.Fatalf("decode Scene: %v", err)
	}
	if definition.ID != testSceneID ||
		definition.Experience != PracticeExperienceInterview ||
		definition.Category != SceneCategoryInterviewProfessional ||
		definition.Version != 1 || len(definition.Roles) != 1 ||
		len(definition.PracticeOptions) != 2 || definition.Prompt.PublicSceneBrief == "" {
		t.Fatalf("Scene = %#v", definition)
	}

	rolesResponse := serveCatalogRequest(
		router,
		"/v1/scenes/"+testSceneID+"/roles",
	)
	var roles struct {
		Roles []RoleDefinition `json:"roles"`
	}
	if err := json.Unmarshal(rolesResponse.Body.Bytes(), &roles); err != nil {
		t.Fatalf("decode roles: %v", err)
	}
	if len(roles.Roles) != 1 || roles.Roles[0].ID != testRoleID {
		t.Fatalf("roles = %#v", roles.Roles)
	}

}

func TestCatalogHTTPHidesUnknownAndInactiveScenes(t *testing.T) {
	definition := testSceneDefinition()
	definition.Status = SceneStatusInactive
	catalog, err := NewCatalog(
		[]SceneDefinition{definition},
		testPolicyValidator(),
	)
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	router := newCatalogTestRouter(t, catalog)
	list := serveCatalogRequest(router, "/v1/scenes")
	if list.Code != http.StatusOK || list.Body.String() != `{"scenes":[]}` {
		t.Fatalf("list status/body = %d/%s", list.Code, list.Body.String())
	}

	for _, path := range []string{
		"/v1/scenes/unknown",
		"/v1/scenes/unknown/roles",
		"/v1/scenes/" + definition.ID,
		"/v1/scenes/" + definition.ID + "/roles",
	} {
		response := serveCatalogRequest(router, path)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status/body = %d/%s", path, response.Code, response.Body.String())
		}
		var body struct {
			Error struct {
				Code          string `json:"code"`
				CorrelationID string `json:"correlation_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if body.Error.Code != "scene_not_found" || body.Error.CorrelationID == "" {
			t.Fatalf("error body = %#v", body)
		}
	}
}

func newCatalogTestRouter(t *testing.T, catalog CatalogReader) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewCatalogHTTPHandler(catalog).RegisterRoutes(router)
	return router
}

func serveCatalogRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
