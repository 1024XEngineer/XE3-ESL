package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
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
	plan := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		planBody,
		"plan-create-0001",
		http.StatusCreated,
	)
	replayedPlan := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		planBody,
		"plan-create-0001",
		http.StatusCreated,
	)
	if replayedPlan["practice_plan_id"] != plan["practice_plan_id"] {
		t.Fatalf("Plan replay changed resource: first=%#v replay=%#v", plan, replayedPlan)
	}
	planID := plan["practice_plan_id"].(string)
	sessionBootstrap := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-plans/"+planID+"/practice-sessions",
		fmt.Sprintf(`{
			"expected_plan_revision":1,
			"preparation_snapshot_id":%q,
			"practice_option_id":%q,
			"role_definition_ids":[%q]
		}`,
			snapshotID,
			preparation.FullSimulationOptionID,
			preparation.TechnicalInterviewerRoleID,
		),
		"session-create-0001",
		http.StatusCreated,
	)
	session := sessionBootstrap["practice_session"].(map[string]any)
	practiceSessionID := session["practice_session_id"].(string)

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
			"preparation_snapshot_id":%q,
			"practice_option_id":"option_forged",
			"role_definition_ids":[%q]
		}`,
			snapshotID,
			preparation.TechnicalInterviewerRoleID,
		),
		"session-forged-option-0001",
		http.StatusNotFound,
	)
}

func newPracticeContextIntegrationComposition(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog preparation.CatalogReader,
) *IdentityAgentPracticeComposition {
	t.Helper()
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
