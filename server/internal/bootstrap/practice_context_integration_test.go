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

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	evaluationagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agenttool"
	goalagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcapability"
	preparationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	reviewagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentityAgentPracticeCompositionPersistsAndResolvesContext(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	catalog, err := scene.NewPostgresCatalog(pool)
	if err != nil {
		t.Fatal(err)
	}
	composition := newPracticeContextIntegrationComposition(t, pool, catalog)
	wantTools := map[string]bool{
		goalagentcapability.GoalCreateCapabilityName:       true,
		goalagentcapability.GoalSearchCapabilityName:       true,
		reviewagenttool.ReviewSearchToolName:               true,
		reviewagenttool.ReviewGetToolName:                  true,
		preparationagentcapability.PracticePreviewToolName: true,
		evaluationagenttool.LatestPracticeReportToolName:   true,
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
	RegisterSceneCatalog(router, catalog)
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

	goalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Backend interview"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	threadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, goalID),
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
	if profileID == "" || snapshotID == "" {
		t.Fatalf("Preparation resources = %#v / %#v", profile, snapshot)
	}

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
			Name: preparationagentcapability.PracticePreviewToolName,
			Input: json.RawMessage(fmt.Sprintf(
				`{"scene_query":%q,"max_effective_turns":4}`,
				testProgrammerInterviewSceneID,
			)),
		},
	)
	if err != nil || needsInput.Content["status"] != "needs_input" {
		t.Fatalf("needs_input Preview = (%#v, %v)", needsInput, err)
	}
	var planCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM preparation_practice_plans WHERE owner_user_id = $1`,
		userID,
	).Scan(&planCount); err != nil {
		t.Fatalf("count plans after needs_input: %v", err)
	}
	if planCount != 0 {
		t.Fatalf("needs_input plan count = %d, want 0", planCount)
	}

	previewCall.RequestID = "preview-ready-0001"
	previewInput := json.RawMessage(fmt.Sprintf(`{
		"goal_id":%q,
		"background_summary":"Go engineer preparing for a backend interview.",
		"scene_id":%q,
		"scene_version":1,
		"selected_role_ids":[%q],
		"practice_option_id":%q,
		"max_effective_turns":4
	}`,
		goalID,
		testProgrammerInterviewSceneID,
		testTechnicalInterviewerRoleID,
		testFullSimulationOptionID,
	))
	ready, err := previewExecutor.Execute(
		context.Background(),
		previewCall,
		tool.Invocation{
			Name:  preparationagentcapability.PracticePreviewToolName,
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
	storedPreviewPlan, err := composition.PlanApplication().ReadPlan(
		context.Background(),
		actor,
		previewPlanID,
	)
	if err != nil {
		t.Fatalf("get stored Preview plan: %v", err)
	}
	if storedPreviewPlan.SessionPolicy.MaxEffectiveTurns != 4 ||
		storedPreviewPlan.SceneSelection.PracticeOptionID !=
			testFullSimulationOptionID {
		t.Fatalf("stored Preview plan = %#v", storedPreviewPlan)
	}
	replayedPreview, err := previewExecutor.Execute(
		context.Background(),
		previewCall,
		tool.Invocation{
			Name:  preparationagentcapability.PracticePreviewToolName,
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

	planID := previewPlanID
	startBody := `{
		"expected_plan_revision":1,
		"user_confirmed":true
	}`
	startPath := "/v1/practice-plans/" + planID + "/practice-sessions"
	sessionBootstrap := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
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
		startPath,
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
		startPath,
		startBody,
		"session-create-active-conflict-0001",
		http.StatusConflict,
	)
	if activeConflict["error"].(map[string]any)["code"] !=
		"active_session_conflict" {
		t.Fatalf("active Session conflict = %#v", activeConflict)
	}
	versionConflict := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		`{
			"expected_plan_revision":2,
			"user_confirmed":true
		}`,
		"session-create-version-conflict-0001",
		http.StatusConflict,
	)
	if versionConflict["error"].(map[string]any)["code"] !=
		"version_conflict" {
		t.Fatalf("version conflict = %#v", versionConflict)
	}

	resolved, err := composition.ResolveSessionByPlan(
		context.Background(),
		actor,
		planID,
	)
	if err != nil {
		t.Fatalf("ResolveSessionByPlan: %v", err)
	}
	if resolved.Session.ID != practiceSessionID ||
		resolved.Session.PlanID != planID ||
		resolved.Snapshot.SessionID != practiceSessionID {
		t.Fatalf("resolved aggregate = %+v", resolved)
	}

	restarted := newPracticeContextIntegrationComposition(t, pool, catalog)
	recovered, err := restarted.ResolveSessionByPlan(
		context.Background(),
		actor,
		planID,
	)
	if err != nil || recovered.Session.ID != practiceSessionID {
		t.Fatalf("restart recovery = (%+v, %v)", recovered, err)
	}

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
		startPath,
		startBody,
		"session-create-cross-user-0001",
		http.StatusNotFound,
	)
	if crossUserConfirmation["error"].(map[string]any)["code"] !=
		"practice_plan_not_found" {
		t.Fatalf("cross-user confirmation = %#v", crossUserConfirmation)
	}
}

func newPracticeContextIntegrationComposition(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog scene.CatalogReader,
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
		agentrun.Configuration{
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
