package preparation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCatalogHTTPRoutesReturnCanonicalStableResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	catalog := mustBuiltinCatalog(t)
	NewCatalogHTTPHandler(catalog).RegisterRoutes(router)

	tests := []struct {
		path string
	}{
		{"/v1/scenario-definitions"},
		{"/v1/ielts-speaking/question-bank"},
		{"/v1/scenario-definitions/" + ProgrammerInterviewScenarioID},
		{"/v1/scenario-definitions/" + ProgrammerInterviewScenarioID + "/role-definitions"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			first := serveCatalogRequest(router, test.path)
			second := serveCatalogRequest(router, test.path)
			if first.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", first.Code, first.Body.String())
			}
			if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
				t.Fatalf("repeated response changed:\nfirst=%s\nsecond=%s", first.Body, second.Body)
			}
			if strings.Contains(first.Body.String(), "display_order") {
				t.Fatalf("internal display order leaked: %s", first.Body.String())
			}
		})
	}

	listResponse := serveCatalogRequest(router, "/v1/scenario-definitions")
	var list ScenarioDefinitionList
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Scenarios) != 31 {
		t.Fatalf("unexpected list: %#v", list)
	}
	listIDs := make(map[string]struct{}, len(list.Scenarios))
	for _, scenario := range list.Scenarios {
		listIDs[scenario.ID] = struct{}{}
	}
	for _, id := range []string{
		ProgrammerInterviewScenarioID,
		IELTSSpeakingPart2ScenarioID,
		WorkplaceProgressRiskScenarioID,
		DailyHotelCheckinScenarioID,
		"scn_interview_self_introduction",
		"scn_ielts_speaking_full",
		"scn_workplace_custom",
		"scn_daily_custom",
	} {
		if _, ok := listIDs[id]; !ok {
			t.Fatalf("list is missing %q", id)
		}
	}
	var rawList struct {
		Scenarios []map[string]any `json:"scenarios"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &rawList); err != nil {
		t.Fatalf("decode raw list: %v", err)
	}
	summaries := make(map[string]string, len(rawList.Scenarios))
	for _, scenario := range rawList.Scenarios {
		id, idOK := scenario["scenario_definition_id"].(string)
		summary, summaryOK := scenario["summary"].(string)
		if !idOK || !summaryOK || strings.TrimSpace(summary) == "" {
			t.Fatalf("scenario list item has no summary: %#v", scenario)
		}
		summaries[id] = summary
	}
	if summaries["scn_interview_self_introduction"] ==
		summaries["scn_interview_recruiter_screening"] {
		t.Fatalf("distinct scenarios share one summary: %#v", summaries)
	}

	questionBankResponse := serveCatalogRequest(
		router,
		"/v1/ielts-speaking/question-bank",
	)
	var questionBank IELTSQuestionBank
	if err := json.Unmarshal(
		questionBankResponse.Body.Bytes(),
		&questionBank,
	); err != nil {
		t.Fatalf("decode IELTS question bank: %v", err)
	}
	if len(questionBank.Part1Sets) != 38 ||
		len(questionBank.TopicGroups) != 56 {
		t.Fatalf(
			"published IELTS bank counts = (%d, %d)",
			len(questionBank.Part1Sets),
			len(questionBank.TopicGroups),
		)
	}
	for _, group := range questionBank.TopicGroups {
		if !group.Published ||
			group.Region != "mainland" ||
			len(group.Part3Questions) < 1 ||
			len(group.Part3Questions) > 5 ||
			group.SupplementedQuestionCount != 0 {
			t.Fatalf("invalid published topic group: %#v", group)
		}
	}

	detailResponse := serveCatalogRequest(
		router,
		"/v1/scenario-definitions/"+ProgrammerInterviewScenarioID,
	)
	var detail ScenarioDetail
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.ScenarioConfig.ID != BackendEngineerConfigID ||
		detail.ScenarioDefinition.Name != "项目经历深挖" ||
		detail.ScenarioDefinition.Model !=
			ScenarioModelProjectExperienceDeepDive ||
		detail.ScenarioConfig.PromptModel.PublicSceneBrief == "" ||
		len(detail.PracticeOptions) != 5 ||
		detail.PracticeOptions[0].ID != FullSimulationOptionID ||
		detail.PracticeOptions[1].ID != TechnicalFocusOptionID ||
		detail.PracticeOptions[2].ID != HRFocusOptionID {
		t.Fatalf("unexpected detail: %#v", detail)
	}

	roleResponse := serveCatalogRequest(
		router,
		"/v1/scenario-definitions/"+ProgrammerInterviewScenarioID+"/role-definitions",
	)
	var roles RoleDefinitionList
	if err := json.Unmarshal(roleResponse.Body.Bytes(), &roles); err != nil {
		t.Fatalf("decode roles: %v", err)
	}
	if len(roles.Roles) != 4 ||
		roles.Roles[0].ID != TechnicalInterviewerRoleID ||
		roles.Roles[0].DisplayName != "技术面试官" ||
		roles.Roles[1].ID != HRInterviewerRoleID ||
		roles.Roles[3].ID != ExecutiveInterviewerRoleID ||
		roles.Roles[3].DisplayName != "用人经理" {
		t.Fatalf("unexpected roles: %#v", roles)
	}
}

func TestCatalogHTTPRoutesHideUnknownAndInactiveScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	definition := programmerInterviewCatalogDefinition()
	definition.definition.Status = ScenarioStatusInactive
	catalog, err := newCatalog([]catalogScenario{definition})
	if err != nil {
		t.Fatalf("newCatalog: %v", err)
	}
	router := gin.New()
	NewCatalogHTTPHandler(catalog).RegisterRoutes(router)

	list := serveCatalogRequest(router, "/v1/scenario-definitions")
	if list.Code != http.StatusOK || list.Body.String() != `{"scenarios":[]}` {
		t.Fatalf("inactive list status=%d body=%s", list.Code, list.Body.String())
	}

	for _, path := range []string{
		"/v1/scenario-definitions/unknown",
		"/v1/scenario-definitions/unknown/role-definitions",
		"/v1/scenario-definitions/" + ProgrammerInterviewScenarioID,
		"/v1/scenario-definitions/" + ProgrammerInterviewScenarioID + "/role-definitions",
	} {
		response := serveCatalogRequest(router, path)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var body struct {
			Error struct {
				Code          string `json:"code"`
				Message       string `json:"message"`
				Retryable     bool   `json:"retryable"`
				CorrelationID string `json:"correlation_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode error: %v", path, err)
		}
		if body.Error.Code != "scenario_definition_not_found" ||
			body.Error.Message == "" ||
			body.Error.Retryable ||
			body.Error.CorrelationID == "" {
			t.Fatalf("%s unexpected error: %#v", path, body)
		}
	}
}

func serveCatalogRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
