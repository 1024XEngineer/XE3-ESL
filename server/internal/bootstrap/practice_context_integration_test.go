package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	evaluationagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/evaluation/agenttool"
	matteragenttool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	practiceagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/practice/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	reviewagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentityAgentPracticeCompositionPersistsAndResolvesContext(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	composition := newPracticeContextIntegrationComposition(t, pool, catalog)
	wantTools := map[string]bool{
		matteragenttool.ScenarioCreateToolName:           true,
		matteragenttool.ScenarioSearchToolName:           true,
		reviewagenttool.ReviewSearchToolName:             true,
		reviewagenttool.ReviewGetToolName:                true,
		practiceagenttool.PracticePreviewToolName:        true,
		practiceagenttool.PracticeStartToolName:          true,
		evaluationagenttool.LatestPracticeReportToolName: true,
	}
	if composition.productionTools == nil {
		t.Fatal("production Agent Tool Registry is nil")
	}
	for _, definition := range composition.productionTools.Definitions() {
		delete(wantTools, definition.Name)
	}
	if len(wantTools) != 0 {
		t.Fatalf("production Agent tools missing = %#v", wantTools)
	}
	protectedRoutes, err := composition.ProtectedRoutes()
	if err != nil {
		t.Fatalf("ProtectedRoutes: %v", err)
	}
	router := NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{
			composition.IdentityModule(),
			composition.AgentModule(),
			protectedRoutes,
		},
	)
	RegisterPreparationCatalog(router, catalog)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	unauthenticated, err := voiceRawRequest(
		server.URL,
		"",
		http.MethodPost,
		"/v1/preparation-profiles",
		strings.NewReader(`{"background_summary":"not trusted"}`),
		"unauthenticated-profile",
		"application/json",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized ||
		unauthenticated.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf(
			"unauthenticated profile response = %d, WWW-Authenticate %q",
			unauthenticated.StatusCode,
			unauthenticated.Header.Get("WWW-Authenticate"),
		)
	}

	token := registerAndLoginVoiceUser(
		t,
		server.URL,
		"context-composition-a@example.test",
	)
	currentUser := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/me",
		"",
		"",
		http.StatusOK,
	)
	userID := currentUser["user_id"].(string)
	var sessionID string
	if err := pool.QueryRow(
		context.Background(),
		`
			SELECT id::text
			FROM identity_auth_sessions
			WHERE user_id = $1
			  AND revoked_at IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`,
		userID,
	).Scan(&sessionID); err != nil {
		t.Fatalf("read trusted Session ID: %v", err)
	}
	actor := requestcontext.Actor{UserID: userID, SessionID: sessionID}

	matterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Backend interview"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	threadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, matterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	profile := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/preparation-profiles",
		`{
			"resume_ref":"resume-v1",
			"job_description_ref":"job-v1",
			"background_summary":"Go engineer preparing for a backend interview."
		}`,
		"profile-create-0001",
		http.StatusCreated,
	)
	profileID := profile["preparation_profile_id"].(string)
	snapshot := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/preparation-profiles/"+profileID+"/snapshots",
		`{"source_version":1}`,
		"snapshot-create-0001",
		http.StatusCreated,
	)
	snapshotID := snapshot["preparation_snapshot_id"].(string)

	previewExecutor := tool.NewExecutor(composition.productionTools)
	previewCall := tool.CallContext{
		Actor:      actor,
		ThreadID:   threadID,
		RunID:      "preview-run-0001",
		ToolCallID: "preview-call-0001",
		RequestID:  "preview-needs-input-0001",
	}
	needsInput, err := previewExecutor.Execute(
		context.Background(),
		previewCall,
		tool.Invocation{
			Name: practiceagenttool.PracticePreviewToolName,
			Input: json.RawMessage(fmt.Sprintf(
				`{"scenario_query":%q,"max_effective_turns":4}`,
				preparation.ProgrammerInterviewScenarioID,
			)),
		},
	)
	if err != nil || needsInput.Content["status"] != "needs_input" {
		t.Fatalf("needs_input Preview = (%#v, %v)", needsInput, err)
	}
	var planCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM practice_plans WHERE owner_user_id = $1`,
		userID,
	).Scan(&planCount); err != nil {
		t.Fatalf("count plans after needs_input: %v", err)
	}
	if planCount != 0 {
		t.Fatalf("needs_input plan count = %d, want 0", planCount)
	}

	previewCall.RequestID = "preview-ready-0001"
	previewInput := json.RawMessage(fmt.Sprintf(`{
		"matter_id":%q,
		"background_summary":"Go engineer preparing for a backend interview.",
		"scenario_definition_id":%q,
		"scenario_definition_version":1,
		"scenario_config_id":%q,
		"scenario_config_version":1,
		"selected_role_ids":[%q],
		"practice_option_id":%q,
		"practice_option_version":1,
		"max_effective_turns":4
	}`,
		matterID,
		preparation.ProgrammerInterviewScenarioID,
		preparation.BackendEngineerConfigID,
		preparation.TechnicalInterviewerRoleID,
		preparation.FullSimulationOptionID,
	))
	ready, err := previewExecutor.Execute(
		context.Background(),
		previewCall,
		tool.Invocation{
			Name:  practiceagenttool.PracticePreviewToolName,
			Input: previewInput,
		},
	)
	if err != nil || ready.Content["status"] != "preview_ready" {
		t.Fatalf("ready Preview input=%s result=(%#v, %v)", previewInput, ready, err)
	}
	previewPlanID, ok := ready.Content["practice_plan_id"].(string)
	if !ok {
		t.Fatalf("ready Preview plan id = %#v", ready.Content["practice_plan_id"])
	}
	storedPreviewPlan, err := composition.PracticeApplication().GetPlan(
		context.Background(),
		actor,
		previewPlanID,
	)
	if err != nil {
		t.Fatalf("get stored Preview plan: %v", err)
	}
	if storedPreviewPlan.SessionPolicy == nil ||
		storedPreviewPlan.SessionPolicy.MaxEffectiveTurns != 4 ||
		storedPreviewPlan.CatalogSnapshot == nil ||
		storedPreviewPlan.CatalogSnapshot.PracticeOption.ID !=
			preparation.FullSimulationOptionID {
		t.Fatalf("stored Preview plan = %#v", storedPreviewPlan)
	}
	replayedPreview, err := previewExecutor.Execute(
		context.Background(),
		previewCall,
		tool.Invocation{
			Name:  practiceagenttool.PracticePreviewToolName,
			Input: previewInput,
		},
	)
	if err != nil ||
		replayedPreview.Content["practice_plan_id"] !=
			ready.Content["practice_plan_id"] {
		t.Fatalf(
			"replayed Preview = (%#v, %v), first %#v",
			replayedPreview,
			err,
			ready,
		)
	}
	var sessionCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM practice_sessions WHERE owner_user_id = $1`,
		userID,
	).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions after Preview: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("Preview session count = %d, want 0", sessionCount)
	}

	planBody := fmt.Sprintf(`{
		"agent_thread_id":%q,
		"matter_id":%q,
		"scenario_definition_id":%q,
		"scenario_definition_version":1,
		"scenario_config_id":%q,
		"scenario_config_version":1,
		"preparation_profile_id":%q,
		"selected_role_ids":[%q]
	}`,
		threadID,
		matterID,
		preparation.ProgrammerInterviewScenarioID,
		preparation.BackendEngineerConfigID,
		profileID,
		preparation.TechnicalInterviewerRoleID,
	)
	startInput := json.RawMessage(fmt.Sprintf(`{
		"practice_plan_id":%q,
		"expected_plan_revision":1,
		"user_confirmed":true
	}`, previewPlanID))
	untrustedStart, err := previewExecutor.Execute(
		context.Background(),
		tool.CallContext{
			Actor:      actor,
			ThreadID:   threadID,
			RunID:      "start-run-untrusted-0001",
			ToolCallID: "start-call-untrusted-0001",
			RequestID:  "start-request-untrusted-0001",
		},
		tool.Invocation{
			Name:  practiceagenttool.PracticeStartToolName,
			Input: startInput,
		},
	)
	if err != nil || untrustedStart.Content["status"] !=
		"confirmation_required" {
		t.Fatalf("untrusted Start = (%#v, %v)", untrustedStart, err)
	}
	planID := previewPlanID
	startBody := fmt.Sprintf(`{
		"practice_plan_id":%q,
		"expected_plan_revision":1,
		"user_confirmed":true
	}`, planID)
	sessionBootstrap := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/practice-start-confirmations",
		startBody,
		"session-create-0001",
		http.StatusCreated,
	)
	session := sessionBootstrap["practice_session"].(map[string]any)
	practiceSessionID := session["practice_session_id"].(string)
	replayedSession := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/practice-start-confirmations",
		startBody,
		"session-create-0001",
		http.StatusCreated,
	)
	if replayedSession["practice_session"].(map[string]any)["practice_session_id"] !=
		practiceSessionID {
		t.Fatalf("Session replay changed resource: %#v", replayedSession)
	}
	activeConflict := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/practice-start-confirmations",
		startBody,
		"session-create-active-conflict-0001",
		http.StatusConflict,
	)
	if activeConflict["error"].(map[string]any)["code"] !=
		"active_session_conflict" ||
		activeConflict["practice_session"].(map[string]any)["practice_session_id"] !=
			practiceSessionID {
		t.Fatalf("active Session conflict = %#v", activeConflict)
	}
	versionConflict := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/practice-start-confirmations",
		fmt.Sprintf(`{
			"practice_plan_id":%q,
			"expected_plan_revision":2,
			"user_confirmed":true
		}`, planID),
		"session-create-version-conflict-0001",
		http.StatusConflict,
	)
	if versionConflict["error"].(map[string]any)["code"] !=
		"version_conflict" {
		t.Fatalf("version conflict = %#v", versionConflict)
	}

	resolved, err := composition.ResolveSessionByThread(
		context.Background(),
		actor,
		threadID,
	)
	if err != nil {
		t.Fatalf("ResolveSessionByThread: %v", err)
	}
	if resolved.Session.ID != practiceSessionID ||
		resolved.Session.PlanID != planID ||
		resolved.Snapshot.SessionID != practiceSessionID {
		t.Fatalf("resolved aggregate = %+v", resolved)
	}

	restarted := newPracticeContextIntegrationComposition(t, pool, catalog)
	recovered, err := restarted.ResolveSessionByThread(
		context.Background(),
		actor,
		threadID,
	)
	if err != nil || recovered.Session.ID != practiceSessionID {
		t.Fatalf("restart recovery = (%+v, %v)", recovered, err)
	}

	otherMatterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Different active context"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	wrongActiveBody := strings.Replace(
		planBody,
		fmt.Sprintf(`"matter_id":%q`, matterID),
		fmt.Sprintf(`"matter_id":%q`, otherMatterID),
		1,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		wrongActiveBody,
		"plan-wrong-active-0001",
		http.StatusConflict,
	)

	otherToken := registerAndLoginVoiceUser(
		t,
		server.URL,
		"context-composition-b@example.test",
	)
	crossUserConfirmation := voiceJSONRequest(
		t,
		server.URL,
		otherToken,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/practice-start-confirmations",
		startBody,
		"session-create-cross-user-0001",
		http.StatusNotFound,
	)
	if crossUserConfirmation["error"].(map[string]any)["code"] !=
		"practice_plan_not_found" {
		t.Fatalf("cross-user confirmation = %#v", crossUserConfirmation)
	}
	crossMatterID := voiceJSONRequest(
		t,
		server.URL,
		otherToken,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Other account interview"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	crossThreadID := voiceJSONRequest(
		t,
		server.URL,
		otherToken,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, crossMatterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	crossAccountBody := strings.Replace(
		strings.Replace(
			planBody,
			fmt.Sprintf(`"agent_thread_id":%q`, threadID),
			fmt.Sprintf(`"agent_thread_id":%q`, crossThreadID),
			1,
		),
		fmt.Sprintf(`"matter_id":%q`, matterID),
		fmt.Sprintf(`"matter_id":%q`, crossMatterID),
		1,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		crossAccountBody,
		"plan-cross-account-0001",
		http.StatusNotFound,
	)

	staleCatalogBody := strings.Replace(
		planBody,
		`"scenario_config_version":1`,
		`"scenario_config_version":2`,
		1,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		staleCatalogBody,
		"plan-stale-catalog-0001",
		http.StatusConflict,
	)
	forgedRoleBody := strings.Replace(
		planBody,
		fmt.Sprintf(`"selected_role_ids":[%q]`,
			preparation.TechnicalInterviewerRoleID),
		`"selected_role_ids":["role_forged"]`,
		1,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		forgedRoleBody,
		"plan-forged-role-0001",
		http.StatusNotFound,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans/"+planID+"/practice-sessions",
		fmt.Sprintf(`{
			"expected_plan_revision":1,
			"user_confirmed":true,
			"preparation_snapshot_id":%q,
			"practice_option_id":"option_forged",
			"role_definition_ids":[%q]
		}`,
			snapshotID,
			preparation.TechnicalInterviewerRoleID,
		),
		"session-forged-option-0001",
		http.StatusConflict,
	)
}

func newPracticeContextIntegrationComposition(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog preparation.CatalogReader,
) *IdentityAgentPracticeComposition {
	t.Helper()
	t.Setenv("AGENT_TOOL_MODE", "real")
	t.Setenv("AGENT_TOOL_FIXTURES", "")
	t.Setenv("APP_ENV", "development")
	composition, err := NewIdentityAgentAndPracticeComposition(
		context.Background(),
		pool,
		nil,
		"",
		practiceContextTextGenerator{},
		agent.RunConfiguration{
			Provider:           "test",
			Model:              "test-context-v1",
			MaxOutputTokens:    128,
			MaxInputCharacters: 12000,
		},
		emptyBootstrapMemorySearcher{},
		catalog,
	)
	if err != nil {
		t.Fatalf("NewIdentityAgentAndPracticeComposition: %v", err)
	}
	return composition
}

type practiceContextTextGenerator struct{}

func (practiceContextTextGenerator) Generate(
	context.Context,
	ai.TextRequest,
) (ai.TextResult, error) {
	return ai.TextResult{
		ID:           "context-composition-result",
		Provider:     "test",
		Model:        "test-context-v1",
		Content:      "test result",
		FinishReason: "stop",
		Usage: ai.TokenUsage{
			InputTokens:  1,
			OutputTokens: 1,
			TotalTokens:  2,
		},
	}, nil
}
