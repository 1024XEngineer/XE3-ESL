package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	practiceinteractionpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/preparationsource"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/repository/postgres"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var cleanBaselineTables = []string{
	"agent_message_attachments",
	"agent_messages",
	"agent_runs",
	"agent_threads",
	"agent_voice_drafts",
	"auth_sessions",
	"coaching_user_profiles",
	"credentials",
	"evaluation_feedback_items",
	"evaluations",
	"interview_preparations",
	"media_assets",
	"pending_practice_actions",
	"practice_plans",
	"practice_questions",
	"practice_sessions",
	"practice_turns",
	"users",
}

func TestMigrationHistoryFreshUpDownUp(t *testing.T) {
	config, admin, schema := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	changed, err := runner.Up()
	if err != nil || !changed {
		t.Fatalf("fresh Up = %t, %v", changed, err)
	}
	assertCleanBaselineSchema(t, admin, schema)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v6 User profile avatar = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v5 Scene selection source = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v4 Question Tip translation = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v3 Practice Plan archive = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v2 Agent domain completion = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to v1 baseline = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables)-1)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to empty = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, 0)

	changed, err = runner.Up()
	if err != nil || !changed {
		t.Fatalf("second Up = %t, %v", changed, err)
	}
	assertCleanBaselineSchema(t, admin, schema)
}

func TestSceneSelectionSourceMigrationTransformsPlansAndPreservesSessions(
	t *testing.T,
) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("initial Up = %t, %v", changed, upErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v6 User profile avatar = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v5 Scene selection source = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v4 legacy Scene selection = %t, %v", changed, downErr)
	}

	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect migration data database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	const (
		userID    = "10000000-0000-4000-8000-000000000011"
		planID    = "30000000-0000-4000-8000-000000000011"
		sessionID = "40000000-0000-4000-8000-000000000011"
	)
	selection, policy, objectives := catalogPlanMigrationFixture(t)
	oldSelection := legacySelectionJSON(t, selection)
	policyJSON := mustJSON(t, policy)
	objectivesJSON := mustJSON(t, objectives)
	if _, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email) VALUES ($1, 'migration@example.com')
`, userID); err != nil {
		t.Fatalf("seed migration owner: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $2, $1, '{"background_summary":"A complete legacy Catalog Plan."}'::jsonb,
    $3::jsonb, $4::jsonb, $5::jsonb,
    'LIFE_AND_TRAVEL', 'ready', 'request-migration-plan',
    decode(repeat('11', 32), 'hex')
)
`, userID, planID, oldSelection, policyJSON, objectivesJSON); err != nil {
		t.Fatalf("seed legacy Practice Plan: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    plan_snapshot, participants, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    $3, $1, $2, 1, 'LIFE_AND_TRAVEL', 'LIFE_DAILY', 'FULL_SIMULATION',
    'daily.general.evaluation.v1', 'starting',
    jsonb_build_object('scene_selection', $4::jsonb), '[{}]'::jsonb,
    'request-migration-session', decode(repeat('12', 32), 'hex')
)
`, userID, planID, sessionID, oldSelection); err != nil {
		t.Fatalf("seed legacy Practice Session: %v", err)
	}

	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("apply v5 = %t, %v", changed, upErr)
	}
	assertMigratedSceneSelection := func(query string, id string) {
		t.Helper()
		var sourceType, sourceID, sceneKey, roleKey, optionKey string
		var sourceVersion, sceneRevision int
		if err := database.QueryRow(context.Background(), query, id).Scan(
			&sourceType,
			&sourceID,
			&sourceVersion,
			&sceneKey,
			&sceneRevision,
			&roleKey,
			&optionKey,
		); err != nil {
			t.Fatalf("read migrated snapshot: %v", err)
		}
		if sourceType != "CATALOG" || sourceID != selection.Source.SceneID ||
			sourceVersion != selection.Source.SceneVersion || sceneKey != sourceID ||
			sceneRevision != selection.Scene.Revision ||
			roleKey != sceneKey || optionKey != sceneKey {
			t.Fatalf("migrated snapshot = %q %q %d %q %d %q %q", sourceType, sourceID, sourceVersion, sceneKey, sceneRevision, roleKey, optionKey)
		}
	}
	assertMigratedSceneSelection(`
SELECT scene_selection #>> '{source,type}',
       scene_selection #>> '{source,scene_id}',
       (scene_selection #>> '{source,scene_version}')::integer,
       scene_selection #>> '{scene,scene_key}',
       (scene_selection #>> '{scene,scene_revision}')::integer,
       scene_selection #>> '{scene,roles,0,scene_key}',
       scene_selection #>> '{scene,practice_options,0,scene_key}'
FROM practice_plans WHERE plan_id = $1
	`, planID)
	poolConfig, err := pgxpool.ParseConfig(config.ConnString())
	if err != nil {
		t.Fatalf("parse migrated repository pool config: %v", err)
	}
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	for key, value := range config.RuntimeParams {
		poolConfig.ConnConfig.RuntimeParams[key] = value
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatalf("open migrated repository pool: %v", err)
	}
	t.Cleanup(pool.Close)
	actor := requestcontext.Actor{
		UserID: userID, SessionID: "20000000-0000-4000-8000-000000000011",
	}
	migratedPlan, err := preparationpostgres.NewPostgresPlanRepository(pool).
		ReadCurrentPlan(context.Background(), actor, planID)
	if err != nil {
		t.Fatalf("strict Repository read of migrated Catalog Plan: %v", err)
	}
	if !preparationservice.ValidReturnedPlan(migratedPlan, actor, planID) ||
		migratedPlan.SceneSelection.Source.Type != scene.SceneSourceCatalog ||
		migratedPlan.SceneSelection.Scene.Key != selection.Scene.Key {
		t.Fatalf("migrated Catalog Plan is not executable: %#v", migratedPlan)
	}
	var sessionSelectionUnchanged bool
	if err := database.QueryRow(context.Background(), `
SELECT plan_snapshot->'scene_selection' = $2::jsonb
FROM practice_sessions WHERE session_id = $1
`, sessionID, oldSelection).Scan(&sessionSelectionUnchanged); err != nil {
		t.Fatalf("read preserved Practice Session snapshot: %v", err)
	}
	if !sessionSelectionUnchanged {
		t.Fatal("Practice Session execution snapshot changed during Plan migration")
	}

	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("roll back v7 = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("roll back v6 = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("roll back v5 = %t, %v", changed, downErr)
	}
	var restoredID, restoredRoleID, restoredOptionID, restoredStatus string
	var restoredVersion int
	if err := database.QueryRow(context.Background(), `
SELECT scene_selection #>> '{scene,scene_id}',
       (scene_selection #>> '{scene,scene_version}')::integer,
       scene_selection #>> '{scene,status}',
       scene_selection #>> '{scene,roles,0,scene_id}',
       scene_selection #>> '{scene,practice_options,0,scene_id}'
FROM practice_plans WHERE plan_id = $1
`, planID).Scan(&restoredID, &restoredVersion, &restoredStatus, &restoredRoleID, &restoredOptionID); err != nil {
		t.Fatalf("read restored legacy snapshot: %v", err)
	}
	if restoredID != selection.Source.SceneID ||
		restoredVersion != selection.Source.SceneVersion ||
		restoredStatus != "active" || restoredRoleID != restoredID ||
		restoredOptionID != restoredID {
		t.Fatalf("restored snapshot = %q %d %q %q %q", restoredID, restoredVersion, restoredStatus, restoredRoleID, restoredOptionID)
	}
}

func TestMigratedLegacyCatalogPlanCompletesThroughFormalReport(t *testing.T) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("initial Up = %t, %v", changed, upErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v6 User profile avatar = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v5 Scene selection source = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v4 legacy Scene selection = %t, %v", changed, downErr)
	}

	ctx := context.Background()
	database, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect migration data database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	const (
		userID       = "10000000-0000-4000-8000-000000000013"
		actorSession = "20000000-0000-4000-8000-000000000013"
		planID       = "30000000-0000-4000-8000-000000000013"
	)
	selection, policy, objectives := catalogPlanMigrationFixture(t)
	if _, err := database.Exec(ctx, `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'migration-lifecycle@example.com')
`, userID); err != nil {
		t.Fatalf("seed migrated Catalog Plan owner: %v", err)
	}
	if _, err := database.Exec(ctx, `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $2, $1, '{"background_summary":"Confirm an existing hotel booking."}'::jsonb,
    $3::jsonb, $4::jsonb, $5::jsonb,
    'LIFE_AND_TRAVEL', 'draft', 'request-migration-lifecycle-plan',
    decode(repeat('14', 32), 'hex')
)
`, userID, planID, legacySelectionJSON(t, selection), mustJSON(t, policy),
		mustJSON(t, objectives)); err != nil {
		t.Fatalf("seed legacy Catalog Plan lifecycle fixture: %v", err)
	}
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("apply v5 = %t, %v", changed, upErr)
	}

	poolConfig, err := pgxpool.ParseConfig(config.ConnString())
	if err != nil {
		t.Fatalf("parse migrated lifecycle pool config: %v", err)
	}
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	for key, value := range config.RuntimeParams {
		poolConfig.ConnConfig.RuntimeParams[key] = value
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("open migrated lifecycle pool: %v", err)
	}
	t.Cleanup(pool.Close)
	actor := requestcontext.Actor{UserID: userID, SessionID: actorSession}
	ids := identity.NewUUIDv4Generator(nil)
	planService, err := preparationservice.NewPlanService(
		preparationpostgres.NewPostgresPlanRepository(pool),
		ids,
		migrationUnusedPlanDependencies{},
		migrationUnusedPlanDependencies{},
		migrationUnusedPlanDependencies{},
		migrationUnusedPlanDependencies{},
		planpolicy.NewResolver(),
	)
	if err != nil {
		t.Fatalf("compose migrated Catalog Plan service: %v", err)
	}
	migratedPlan, err := planService.ReadPlan(ctx, actor, planID)
	if err != nil || migratedPlan.Status != preparation.PlanStatusDraft ||
		migratedPlan.SceneSelection.Source.Type != scene.SceneSourceCatalog ||
		migratedPlan.SceneSelection.Scene.Key != selection.Scene.Key {
		t.Fatalf("strict read of migrated Catalog Plan = %#v, %v", migratedPlan, err)
	}
	confirmed, replayed, err := planService.ConfirmPlan(
		ctx,
		actor,
		planID,
		"migration-lifecycle-plan-confirm-0001",
		preparation.ConfirmPlanRequest{ExpectedVersion: migratedPlan.Version},
	)
	if err != nil || replayed || confirmed.Status != preparation.PlanStatusReady ||
		confirmed.Version != migratedPlan.Version+1 {
		t.Fatalf("confirm migrated Catalog Plan = %#v, replayed=%t, err=%v", confirmed, replayed, err)
	}

	evaluationComposition, err := app.NewEvaluationComposition(
		pool,
		migrationReportGenerator{},
		migrationSpeechFeedbackGenerator{},
		nil,
		app.EvaluationConfiguration{
			Provider:     "qianwen",
			SessionModel: "qwen-plus",
			SpeechModel:  "qwen-plus",
			Worker:       migrationEvaluationWorkerConfiguration(),
		},
	)
	if err != nil {
		t.Fatalf("compose Evaluation runtime: %v", err)
	}
	schedulers := evaluationComposition.PracticeSchedulers()
	practiceRepository, err := practiceinteractionpostgres.New(
		pool,
		schedulers.Completion,
		schedulers.TurnFeedback,
		ids,
	)
	if err != nil {
		t.Fatalf("compose Practice repository: %v", err)
	}
	planSource, err := preparationsource.New(planService)
	if err != nil {
		t.Fatalf("compose Preparation Plan projection: %v", err)
	}
	sessionService, err := practice.NewSessionApplication(
		practiceRepository,
		ids,
		planSource,
	)
	if err != nil {
		t.Fatalf("compose Practice Session service: %v", err)
	}
	bootstrap, replayed, err := sessionService.CreateSession(
		ctx,
		actor,
		confirmed.ID,
		"migration-lifecycle-session-create-0001",
		practice.CreateSessionRequest{ExpectedPlanVersion: confirmed.Version},
	)
	if err != nil || replayed || bootstrap.Session.ID == "" ||
		bootstrap.Snapshot.SceneSelection.Scene.ID != selection.Scene.Key {
		t.Fatalf("create Session from migrated Catalog Plan = %#v, replayed=%t, err=%v", bootstrap, replayed, err)
	}

	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatalf("compose Practice temporary audio vault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	practiceProviders := migrationPracticeProviders{}
	newInteractionRuntime := func() *practiceinteraction.SessionApplication {
		t.Helper()
		application, _, runtimeErr := practiceinteraction.NewRuntimeApplications(
			practiceinteraction.RuntimeConfiguration{
				Repository:         practiceRepository,
				IDs:                ids,
				TemporaryAudio:     vault,
				Recognizer:         practiceProviders,
				RecordedRecognizer: practiceProviders,
				Synthesizer:        practiceProviders,
				QuestionGenerator:  practiceProviders,
				Recordings:         practiceProviders,
				ASRLease:           time.Minute,
			},
		)
		if runtimeErr != nil {
			t.Fatalf("compose Practice Interaction runtime: %v", runtimeErr)
		}
		return application
	}
	started, err := newInteractionRuntime().Start(
		ctx,
		actor,
		bootstrap.Session.ID,
		"migration-lifecycle-session-start-0001",
	)
	if err != nil || started.Session.Status != string(practice.SessionInProgress) ||
		started.Question == nil {
		t.Fatalf("start migrated Catalog Session = %#v, %v", started, err)
	}
	recovered, err := newInteractionRuntime().Resume(ctx, actor, bootstrap.Session.ID)
	if err != nil || recovered.Session.ID != bootstrap.Session.ID ||
		recovered.Session.Status != string(practice.SessionInProgress) ||
		recovered.Question == nil || recovered.Question.ID != started.Question.ID {
		t.Fatalf("recover migrated Catalog Session = %#v, %v", recovered, err)
	}
	answered, err := newInteractionRuntime().SubmitText(
		ctx,
		actor,
		practiceinteraction.SubmitTextAnswerCommand{
			SessionID:      bootstrap.Session.ID,
			QuestionID:     recovered.Question.ID,
			IdempotencyKey: "migration-lifecycle-text-turn-0001",
			AnswerText:     "I booked a double room for two nights and would like to confirm breakfast is included.",
		},
	)
	if err != nil || answered.Turn == nil || answered.Turn.EffectiveTurns != 1 ||
		answered.Session.SessionVersion <= recovered.Session.SessionVersion {
		t.Fatalf("answer migrated Catalog Session = %#v, %v", answered, err)
	}
	completed, replayed, err := sessionService.TransitionSession(
		ctx,
		actor,
		bootstrap.Session.ID,
		"migration-lifecycle-session-complete-0001",
		answered.Session.SessionVersion,
		practice.SessionComplete,
	)
	if err != nil || replayed || completed.Status != practice.SessionCompleted {
		t.Fatalf("complete migrated Catalog Session = %#v, replayed=%t, err=%v", completed, replayed, err)
	}
	record, err := evaluationComposition.Store().GetRecordBySource(
		ctx,
		userID,
		evaluation.KindSessionReport,
		bootstrap.Session.ID,
	)
	if err != nil || record.Status != evaluation.JobQueued {
		t.Fatalf("queued migrated Catalog Evaluation = %#v, %v", record, err)
	}
	processed, err := evaluationComposition.Worker().ProcessSession(ctx)
	if err != nil || !processed {
		t.Fatalf("process migrated Catalog Evaluation = %t, %v", processed, err)
	}
	record, err = evaluationComposition.Store().GetRecordBySource(
		ctx,
		userID,
		evaluation.KindSessionReport,
		bootstrap.Session.ID,
	)
	if err != nil || record.Status != evaluation.JobReady {
		t.Fatalf("ready migrated Catalog Evaluation = %#v, %v", record, err)
	}
	formal, err := evaluationComposition.Store().GetFormalReport(ctx, userID, record.ID)
	if err != nil || !formal.Valid() || formal.PracticeSessionID != bootstrap.Session.ID ||
		formal.Report.SceneCategory != string(selection.Scene.Category) {
		t.Fatalf("read migrated Catalog formal Report = %#v, %v", formal, err)
	}
}

func TestSceneSelectionSourceMigrationRejectsDownWithCustomPlan(t *testing.T) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("initial Up = %t, %v", changed, upErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v6 User profile avatar = %t, %v", changed, downErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v5 Scene selection source = %t, %v", changed, downErr)
	}

	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect migration data database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	const (
		userID = "10000000-0000-4000-8000-000000000012"
		planID = "30000000-0000-4000-8000-000000000012"
	)
	selection, err := scene.NewCustomSelection(planID, scene.CustomSceneSpec{
		Scenario:       "向社区活动负责人提议举办英语角",
		UserRole:       "活动发起人",
		AIRole:         "社区活动负责人",
		PracticeGoal:   "说明活动价值并协商时间、场地和报名方式",
		ExperienceHint: scene.PracticeExperienceLifeAndTravel,
	})
	if err != nil {
		t.Fatalf("build valid Custom selection: %v", err)
	}
	policy, objectives := customPlanMigrationExecution(t, selection)
	if _, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email) VALUES ($1, 'custom-down@example.com')
`, userID); err != nil {
		t.Fatalf("seed Custom Plan owner: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $2, $1, '{}'::jsonb, $3::jsonb, $4::jsonb, $5::jsonb,
    'LIFE_AND_TRAVEL', 'draft', 'request-custom-down-plan',
    decode(repeat('13', 32), 'hex')
)
`, userID, planID, mustJSON(t, selection), mustJSON(t, policy), mustJSON(t, objectives)); err != nil {
		t.Fatalf("seed valid Custom Plan: %v", err)
	}

	changed, downErr := runner.DownOne()
	if downErr == nil || changed ||
		!strings.Contains(downErr.Error(), "cannot roll back scene selection source while CUSTOM snapshots exist") {
		t.Fatalf("DownOne with Custom Plan = %t, %v", changed, downErr)
	}
	var sourceType string
	if err := database.QueryRow(context.Background(), `
SELECT scene_selection #>> '{source,type}'
FROM practice_plans WHERE plan_id=$1
`, planID).Scan(&sourceType); err != nil {
		t.Fatalf("read Custom Plan after rejected DownOne: %v", err)
	}
	if sourceType != "CUSTOM" {
		t.Fatalf("Custom Plan source after rejected DownOne = %q", sourceType)
	}
}

func catalogPlanMigrationFixture(
	t *testing.T,
) (scene.SelectionSnapshot, preparation.SessionPolicy, []preparation.PracticeObjective) {
	t.Helper()
	const (
		sceneID  = "scn_migration_hotel_checkin"
		roleID   = "role_migration_hotel_front_desk"
		optionID = "option_migration_hotel_full"
	)
	selection := scene.SelectionSnapshot{
		Source: scene.SceneSource{
			Type: scene.SceneSourceCatalog, SceneID: sceneID, SceneVersion: 2,
		},
		Scene: scene.ExecutableSceneSnapshot{
			Key:        sceneID,
			Revision:   2,
			Experience: scene.PracticeExperienceLifeAndTravel,
			Category:   scene.SceneCategoryLifeTravel,
			Name:       "迁移测试酒店入住",
			Prompt: scene.ScenePrompt{
				PublicSceneBrief: "在酒店前台核对预订并办理入住。",
				PracticeGoal:     "清楚确认预订、房型和入住安排。",
				UserRole:         "住客",
				AIRole:           "酒店前台",
				PersonaSummary:   "专业且乐于协助的酒店前台。",
				FocusAreas:       []string{"预订确认", "入住安排"},
				TurnBlueprints:   []string{"核对预订", "确认房型", "完成入住"},
			},
			Roles: []scene.RoleSnapshot{{
				ID:               roleID,
				SceneKey:         sceneID,
				Type:             "HOTEL_FRONT_DESK",
				DisplayName:      "酒店前台",
				Responsibilities: "核验预订并完成入住。",
				Style:            "专业、清晰。",
				PracticeObjectives: []scene.PracticeObjectiveDefinition{{
					ID: "confirm_booking", Description: "准确确认预订和入住信息。",
				}},
			}},
			PracticeOptions: []scene.PracticeOptionSnapshot{{
				ID:                       optionID,
				SceneKey:                 sceneID,
				Mode:                     scene.PracticeModeFullSimulation,
				DisplayName:              "完整模拟",
				SuggestedDurationSeconds: 480,
				TurnPolicyRef:            "generic.practice.turn.v1",
				SessionPolicyRef:         "daily.practice.session.v1",
				EvaluationPolicyRef:      "daily.general.evaluation.v1",
			}},
		},
		SelectedRoleIDs:  []string{roleID},
		PracticeOptionID: optionID,
	}
	if !scene.ValidSelectionSnapshot(selection) {
		t.Fatal("Catalog migration fixture is not a valid current selection")
	}
	policy, objectives := customPlanMigrationExecution(t, selection)
	return selection, policy, objectives
}

func customPlanMigrationExecution(
	t *testing.T,
	selection scene.SelectionSnapshot,
) (preparation.SessionPolicy, []preparation.PracticeObjective) {
	t.Helper()
	option, err := selection.PracticeOption()
	if err != nil {
		t.Fatalf("read migration fixture option: %v", err)
	}
	policy, err := planpolicy.NewResolver().ResolveSessionPolicy(
		selection.Scene, option, 0,
	)
	if err != nil {
		t.Fatalf("resolve migration fixture policy: %v", err)
	}
	roles, err := selection.SelectedRoles()
	if err != nil {
		t.Fatalf("read migration fixture roles: %v", err)
	}
	objectives := make([]preparation.PracticeObjective, 0)
	seen := make(map[string]struct{})
	for _, role := range roles {
		for _, objective := range role.PracticeObjectives {
			if _, duplicate := seen[objective.ID]; duplicate {
				continue
			}
			seen[objective.ID] = struct{}{}
			objectives = append(objectives, preparation.PracticeObjective{
				ID: objective.ID, Description: objective.Description,
			})
		}
	}
	return policy, objectives
}

func legacySelectionJSON(t *testing.T, selection scene.SelectionSnapshot) string {
	t.Helper()
	raw := mustJSON(t, selection)
	var document map[string]any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("decode current selection fixture: %v", err)
	}
	delete(document, "source")
	sceneDocument, ok := document["scene"].(map[string]any)
	if !ok {
		t.Fatal("current selection fixture has no scene object")
	}
	sceneDocument["scene_id"] = sceneDocument["scene_key"]
	sceneDocument["scene_version"] = sceneDocument["scene_revision"]
	sceneDocument["status"] = "active"
	delete(sceneDocument, "scene_key")
	delete(sceneDocument, "scene_revision")
	for _, field := range []string{"roles", "practice_options"} {
		values, ok := sceneDocument[field].([]any)
		if !ok {
			t.Fatalf("current selection fixture %s is not an array", field)
		}
		for _, value := range values {
			item, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("current selection fixture %s item is not an object", field)
			}
			item["scene_id"] = item["scene_key"]
			delete(item, "scene_key")
		}
	}
	return mustJSON(t, document)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return string(raw)
}

type migrationUnusedPlanDependencies struct{}

func (migrationUnusedPlanDependencies) ReadConfirmed(
	context.Context,
	requestcontext.Actor,
	string,
	int,
) (preparation.InterviewPreparationSnapshot, error) {
	return preparation.InterviewPreparationSnapshot{}, errors.New(
		"unused migrated Catalog Plan dependency",
	)
}

func (migrationUnusedPlanDependencies) ReadOwnedThread(
	context.Context,
	requestcontext.Actor,
	string,
) (preparation.SourceThread, error) {
	return preparation.SourceThread{}, errors.New(
		"unused migrated Catalog Plan dependency",
	)
}

func (migrationUnusedPlanDependencies) ResolveAccessibleSelection(
	context.Context,
	string,
	string,
	int,
	[]string,
	string,
) (scene.SelectionSnapshot, error) {
	return scene.SelectionSnapshot{}, errors.New(
		"unused migrated Catalog Plan dependency",
	)
}

func (migrationUnusedPlanDependencies) ResolveQuestionSet(
	context.Context,
	ielts.QuestionSetSelection,
) (ielts.ResolvedQuestionSet, error) {
	return ielts.ResolvedQuestionSet{}, errors.New(
		"unused migrated Catalog Plan dependency",
	)
}

func (migrationUnusedPlanDependencies) AssignQuestionSet(
	context.Context,
	ielts.PracticeMode,
	string,
) (ielts.ResolvedQuestionSet, error) {
	return ielts.ResolvedQuestionSet{}, errors.New(
		"unused migrated Catalog Plan dependency",
	)
}

type migrationPracticeProviders struct{}

func (migrationPracticeProviders) GenerateQuestion(
	context.Context,
	practiceinteraction.QuestionGenerationRequest,
) (string, error) {
	return "Could you confirm the booking details and the dates of your stay?", nil
}

func (migrationPracticeProviders) Transcribe(
	context.Context,
	practiceinteraction.TranscriptionRequest,
) (practiceinteraction.TranscriptionResult, error) {
	return practiceinteraction.TranscriptionResult{}, errors.New(
		"unexpected speech recognition in migrated Catalog text lifecycle",
	)
}

func (migrationPracticeProviders) Synthesize(
	context.Context,
	practiceinteraction.SynthesisRequest,
) (practiceinteraction.SynthesisResult, error) {
	return practiceinteraction.SynthesisResult{}, errors.New(
		"unexpected speech synthesis in migrated Catalog text lifecycle",
	)
}

func (migrationPracticeProviders) Upload(
	context.Context,
	requestcontext.Actor,
	string,
	platformmedia.AudioSource,
) (string, error) {
	return "", errors.New(
		"unexpected recording upload in migrated Catalog text lifecycle",
	)
}

type migrationReportGenerator struct{}

func (migrationReportGenerator) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	var input struct {
		DimensionKeys []string `json:"dimension_keys"`
	}
	if err := json.Unmarshal([]byte(request.UserPrompt), &input); err != nil {
		return textgeneration.Result{}, err
	}
	dimensions := make([]map[string]any, len(input.DimensionKeys))
	for index, key := range input.DimensionKeys {
		dimensions[index] = map[string]any{
			"key":                  key,
			"score":                75.0,
			"coverage":             1.0,
			"confidence":           0.8,
			"reason_codes":         []string{},
			"strengths":            []any{},
			"improvements":         []any{},
			"recommended_examples": []any{},
		}
	}
	content, err := json.Marshal(map[string]any{
		"scoreability_status": "PROVISIONAL",
		"summary":             "The hotel check-in response is clear and task-focused.",
		"dimensions":          dimensions,
		"priority_actions":    []any{},
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: "migration-report-request-1",
		Content:   string(content),
		Provider:  "qianwen",
		Model:     "qwen-plus",
	}, nil
}

type migrationSpeechFeedbackGenerator struct{}

func (migrationSpeechFeedbackGenerator) Generate(
	context.Context,
	speechfeedback.TextGenerationRequest,
) (speechfeedback.TextGenerationResult, error) {
	return speechfeedback.TextGenerationResult{
		RequestID: "migration-speech-request-1",
		Content: `{"items":[{"kind":"STRENGTH","explanation":"The answer is clear.",` +
			`"suggested_text":null}]}`,
		Provider: "qianwen",
		Model:    "qwen-plus",
	}, nil
}

func migrationEvaluationWorkerConfiguration() evaluation.WorkerConfiguration {
	return evaluation.WorkerConfiguration{
		SessionLane: evaluation.ClaimLane{
			Kinds:         []evaluation.Kind{evaluation.KindSessionReport},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		SpeechLane: evaluation.ClaimLane{
			Kinds: []evaluation.Kind{
				evaluation.KindPracticeTurnFeedback,
				evaluation.KindAgentMessageFeedback,
			},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		InterviewDeadline: 30 * time.Second,
		IELTSDeadline:     110 * time.Second,
		GeneralDeadline:   30 * time.Second,
		SpeechDeadline:    30 * time.Second,
		RetryDelay:        time.Second,
		DependencyDelay:   time.Second,
		FinalizeTimeout:   5 * time.Second,
	}
}

func TestCleanBaselineOwnershipStateAndPartialUniqueness(t *testing.T) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, err := runner.Up(); err != nil || !changed {
		t.Fatalf("Up = %t, %v", changed, err)
	}

	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect baseline: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })

	const (
		userA     = "10000000-0000-4000-8000-000000000001"
		userB     = "10000000-0000-4000-8000-000000000002"
		threadA   = "20000000-0000-4000-8000-000000000001"
		planA     = "30000000-0000-4000-8000-000000000001"
		sessionA  = "40000000-0000-4000-8000-000000000001"
		questionA = "50000000-0000-4000-8000-000000000001"
		questionB = "50000000-0000-4000-8000-000000000002"
	)
	if _, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'a@example.com'), ($2, 'b@example.com')
`, userA, userB); err != nil {
		t.Fatalf("seed owners: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO agent_threads (id, user_id) VALUES ($1, $2)
`, threadA, userA); err != nil {
		t.Fatalf("seed Agent Thread: %v", err)
	}

	_, err = database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, source_thread_id, preparation_snapshot,
    scene_selection, session_policy, practice_objectives,
    practice_experience, status, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    gen_random_uuid(), $1, $2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
    '[{}]'::jsonb, 'conversation', 'draft', 'request-bad-owner',
    decode(repeat('00', 32), 'hex')
)`, userB, threadA)
	expectPostgresCode(t, err, "23503")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $1, $2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'conversation', 'ready', 'request-plan-a',
    decode(repeat('01', 32), 'hex')
)
`, planA, userA); err != nil {
		t.Fatalf("seed Practice Plan: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    plan_snapshot, participants, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    $1, $2, $3, 1, 'conversation', 'general', 'voice',
    'general.evaluation.v1', 'starting', '{}'::jsonb, '[{}]'::jsonb,
    'request-session-a', decode(repeat('02', 32), 'hex')
)
`, sessionA, userA, planA); err != nil {
		t.Fatalf("seed Practice Session: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence
) VALUES (
    $1, $2, 'objective-a', 'prompt', 'Tell me about yourself.',
    'coach', ARRAY['learner'], 1
)
`, questionA, sessionA); err != nil {
		t.Fatalf("seed Practice Question: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence,
    parent_question_id
) VALUES (
    $1, $2, 'objective-a', 'FOLLOW_UP', 'What trade-off did you make?',
    'coach', ARRAY['learner'], 2, $3
)
`, questionB, sessionA, questionA); err != nil {
		t.Fatalf("seed follow-up Practice Question: %v", err)
	}

	_, err = database.Exec(context.Background(), `
UPDATE practice_sessions
SET status = 'completed', updated_at = CURRENT_TIMESTAMP
WHERE session_id = $1
`, sessionA)
	expectPostgresCode(t, err, "23514")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 1, 'EFFECTIVE', 'confirmed',
    true, gen_random_uuid(), 'transcript-a', 1, 'First answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionA); err != nil {
		t.Fatalf("seed confirmed effective Turn: %v", err)
	}
	_, err = database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 2, 'EFFECTIVE', 'confirmed',
    true, gen_random_uuid(), 'transcript-b', 1, 'Second answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionA)
	expectPostgresCode(t, err, "23505")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 3, 'EFFECTIVE', 'confirmed',
    false, gen_random_uuid(), 'transcript-follow-up', 1, 'Follow-up answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionB); err != nil {
		t.Fatalf("seed non-counting effective follow-up Turn: %v", err)
	}

	var evaluationID string
	if err := database.QueryRow(context.Background(), `
INSERT INTO evaluations (
    user_id, kind, source_id, context_id, input_snapshot, input_hash,
    config_lineage, config_hash
) VALUES (
    $1, 'SESSION_REPORT', gen_random_uuid(), $2, '{}'::json,
    decode(repeat('03', 32), 'hex'), '{}'::json,
    decode(repeat('04', 32), 'hex')
) RETURNING id::text
`, userA, sessionA).Scan(&evaluationID); err != nil || evaluationID == "" {
		t.Fatalf("built-in gen_random_uuid = %q, %v", evaluationID, err)
	}
}

func assertCleanBaselineSchema(
	t *testing.T,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	rows, err := database.Query(context.Background(), `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = $1
  AND table_type = 'BASE TABLE'
  AND table_name <> 'schema_migrations'
ORDER BY table_name
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tables, cleanBaselineTables) {
		t.Fatalf("application tables = %v, want %v", tables, cleanBaselineTables)
	}

	var functions, triggers, extensions, customTypes int
	if err := database.QueryRow(context.Background(), `
SELECT
    (SELECT count(*) FROM pg_proc p
     JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = $1),
    (SELECT count(*) FROM pg_trigger t
     JOIN pg_class c ON c.oid = t.tgrelid
     JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = $1 AND NOT t.tgisinternal),
    (SELECT count(*) FROM pg_extension e
     JOIN pg_namespace n ON n.oid = e.extnamespace
     WHERE n.nspname = $1),
    (SELECT count(*) FROM pg_type ty
     JOIN pg_namespace n ON n.oid = ty.typnamespace
     WHERE n.nspname = $1 AND ty.typtype IN ('d', 'e'))
`, schema).Scan(&functions, &triggers, &extensions, &customTypes); err != nil {
		t.Fatal(err)
	}
	if functions != 0 || triggers != 0 || extensions != 0 || customTypes != 0 {
		t.Fatalf(
			"database programming objects = functions:%d triggers:%d extensions:%d custom types:%d",
			functions, triggers, extensions, customTypes,
		)
	}
}

func assertApplicationTableCount(
	t *testing.T,
	database *pgx.Conn,
	schema string,
	want int,
) {
	t.Helper()
	var count int
	if err := database.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.tables
WHERE table_schema = $1
  AND table_type = 'BASE TABLE'
  AND table_name <> 'schema_migrations'
`, schema).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("application table count = %d, want %d", count, want)
	}
}

func expectPostgresCode(t *testing.T, err error, code string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("PostgreSQL error = %v, want SQLSTATE %s", err, code)
	}
}

func isolatedMigrationConfig(
	t *testing.T,
) (*pgx.ConnConfig, *pgx.Conn, string) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnectTimeout = ConnectTimeout

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		dropContext, dropCancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer dropCancel()
		if _, err := admin.Exec(
			dropContext, "DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	migrationConfig := config.Copy()
	if migrationConfig.RuntimeParams == nil {
		migrationConfig.RuntimeParams = make(map[string]string)
	}
	migrationConfig.RuntimeParams["search_path"] = schema
	return migrationConfig, admin, schema
}
