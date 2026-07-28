package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
)

func TestContextRepositoryPersistsExactRecoverableAggregate(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	other := contextOwnerB()
	seedContextOwner(t, pool, &owner)
	seedContextOwner(t, pool, &other)

	planCommand := contextPlanCommand(owner, "plan-a", "plan-intent-0001")
	plan, replayed, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		planCommand,
	)
	if err != nil || replayed {
		t.Fatalf("CreatePlan = (%+v, %t, %v)", plan, replayed, err)
	}
	if plan.AgentThreadID != owner.ThreadID ||
		plan.MatterID != owner.MatterID ||
		plan.Status != persistence.PlanStatusReady ||
		plan.Revision != 1 ||
		plan.CreatedAt.Location() != time.UTC ||
		plan.UpdatedAt.Location() != time.UTC {
		t.Fatalf("persisted Plan = %+v", plan)
	}
	replayedPlan, found, err := repository.ReplayPlan(
		ctx,
		owner.Actor,
		planCommand.Intent,
	)
	if err != nil || !found || !reflect.DeepEqual(replayedPlan, plan) {
		t.Fatalf("ReplayPlan = (%+v, %t, %v)", replayedPlan, found, err)
	}
	storedPlan, err := repository.GetPlan(ctx, owner.Actor, plan.ID)
	if err != nil || !reflect.DeepEqual(storedPlan, plan) {
		t.Fatalf("GetPlan = (%+v, %v), want %+v", storedPlan, err, plan)
	}
	differentIntent := planCommand.Intent
	differentIntent.PayloadFingerprint = sha256.Sum256([]byte("different"))
	if _, _, err := repository.ReplayPlan(
		ctx,
		owner.Actor,
		differentIntent,
	); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("different Plan replay error = %v", err)
	}
	if _, err := repository.GetPlan(
		ctx,
		other.Actor,
		plan.ID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("cross-owner GetPlan error = %v", err)
	}
	crossOwnerCommand := contextPlanCommand(
		owner,
		"plan-cross-owner",
		"plan-cross-owner-intent",
	)
	crossOwnerCommand.AgentThreadID = other.ThreadID
	crossOwnerCommand.MatterID = other.MatterID
	if _, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		crossOwnerCommand,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("cross-owner Plan anchor error = %v", err)
	}

	sessionCommand := contextSessionCommand(
		owner,
		plan,
		"session-a",
		"session-snapshot-a",
		"session-intent-0001",
	)
	bootstrap, replayed, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		sessionCommand,
	)
	if err != nil || replayed {
		t.Fatalf(
			"CreateContextSession = (%+v, %t, %v)",
			bootstrap,
			replayed,
			err,
		)
	}
	if bootstrap.Session.Status != persistence.ContextSessionStarting ||
		bootstrap.Session.PlanID != plan.ID ||
		bootstrap.Snapshot.ID != bootstrap.Session.SnapshotID ||
		bootstrap.Snapshot.SessionID != bootstrap.Session.ID ||
		bootstrap.Snapshot.SessionPolicy.MaxEffectiveTurns != 6 {
		t.Fatalf("persisted Session bootstrap = %+v", bootstrap)
	}
	legacySession, err := repository.GetSession(
		ctx,
		owner.Actor,
		bootstrap.Session.ID,
	)
	if err != nil {
		t.Fatalf("decode formal Session through legacy projection: %v", err)
	}
	if !legacySession.StartedAt.IsZero() ||
		len(legacySession.Snapshot.Participants) != 2 ||
		legacySession.Snapshot.Participants[0].ParticipantID !=
			bootstrap.Snapshot.Participants[0].ID ||
		legacySession.Snapshot.Participants[0].Order !=
			bootstrap.Snapshot.Participants[0].Order ||
		legacySession.Snapshot.Participants[0].
			RoleDefinition["role_definition_id"] !=
			bootstrap.Snapshot.Participants[0].RoleDefinitionID ||
		legacySession.Snapshot.Participants[1].ParticipantID !=
			bootstrap.Snapshot.Participants[1].ID {
		t.Fatalf("legacy formal Session projection = %+v", legacySession)
	}

	restarted := practicepostgres.New(pool)
	recovered, err := restarted.GetContextSession(
		ctx,
		owner.Actor,
		bootstrap.Session.ID,
	)
	if err != nil || recovered.ID != bootstrap.Session.ID {
		t.Fatalf("recovered Session = (%+v, %v)", recovered, err)
	}
	recoveredSnapshot, err := restarted.GetContextSessionSnapshot(
		ctx,
		owner.Actor,
		bootstrap.Session.ID,
	)
	if err != nil ||
		recoveredSnapshot.ID != bootstrap.Snapshot.ID ||
		recoveredSnapshot.Preparation.BackgroundSnapshot !=
			owner.BackgroundSummary ||
		recoveredSnapshot.Participants[1].SubjectRef.SubjectID !=
			owner.Actor.UserID {
		t.Fatalf("recovered Snapshot = (%+v, %v)", recoveredSnapshot, err)
	}
	resolved, err := restarted.ResolveContextSessionByThread(
		ctx,
		owner.Actor,
		owner.ThreadID,
	)
	if err != nil || resolved.Session.ID != bootstrap.Session.ID {
		t.Fatalf("ResolveContextSessionByThread = (%+v, %v)", resolved, err)
	}
	forgedSnapshot := sessionCommand
	forgedSnapshot.SessionID = "session-forged-preparation"
	forgedSnapshot.SnapshotID = "snapshot-forged-preparation"
	forgedSnapshot.Intent = contextIntent(
		"/v1/practice-plans/"+plan.ID+"/practice-sessions",
		"session-forged-intent",
		"forged-session-payload",
	)
	forgedSnapshot.Snapshot.ID = forgedSnapshot.SnapshotID
	forgedSnapshot.Snapshot.SessionID = forgedSnapshot.SessionID
	forgedSnapshot.Snapshot.Preparation.BackgroundSnapshot = "forged"
	for index := range forgedSnapshot.Snapshot.Participants {
		forgedSnapshot.Snapshot.Participants[index].SessionID =
			forgedSnapshot.SessionID
		forgedSnapshot.Snapshot.Participants[index].ID =
			fmt.Sprintf("forged-participant-%d", index)
	}
	if _, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		forgedSnapshot,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("forged Preparation Snapshot error = %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, status,
			version, effective_turns, started_at
		) VALUES (
			$1, 'legacy-session-a', $2, 'active',
			1, 0, transaction_timestamp()
		)
	`, owner.Actor.UserID, "agent-thread:"+owner.ThreadID); err != nil {
		t.Fatalf("insert legacy Session: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, mode, target_ids,
			participants, turn_limit
		) VALUES (
			$1, 'legacy-session-a', 'INTERVIEW',
			'["legacy-target"]'::jsonb, '[]'::jsonb, 4
		)
	`, owner.Actor.UserID); err != nil {
		t.Fatalf("insert legacy Session Snapshot: %v", err)
	}
	historical, err := restarted.GetSession(
		ctx,
		owner.Actor,
		"legacy-session-a",
	)
	if err != nil ||
		historical.ID != "legacy-session-a" ||
		historical.Status != persistence.SessionStatusActive {
		t.Fatalf("recover historical #111 Session = (%+v, %v)", historical, err)
	}
	resolved, err = restarted.ResolveContextSessionByThread(
		ctx,
		owner.Actor,
		owner.ThreadID,
	)
	if err != nil || resolved.Session.ID != bootstrap.Session.ID {
		t.Fatalf(
			"resolver selected legacy/prefix Session: (%+v, %v)",
			resolved,
			err,
		)
	}

	_, err = pool.Exec(ctx, `
		UPDATE practice_session_snapshots
		SET snapshot_document = '{}'::jsonb
		WHERE owner_user_id = $1 AND session_id = $2
	`, owner.Actor.UserID, bootstrap.Session.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "55000" {
		t.Fatalf("immutable Snapshot update error = %v", err)
	}
	_, err = pool.Exec(ctx, `
		DELETE FROM practice_plans
		WHERE owner_user_id = $1 AND plan_id = $2
	`, owner.Actor.UserID, plan.ID)
	if !errors.As(err, &postgresError) ||
		(postgresError.Code != "23001" &&
			postgresError.Code != "23503") ||
		postgresError.ConstraintName !=
			"practice_sessions_context_plan_fkey" {
		t.Fatalf("delete referenced Plan error = %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE matters SET status = 'archived'
		WHERE owner_user_id = $1 AND id = $2
	`, owner.Actor.UserID, owner.MatterID); err != nil {
		t.Fatalf("archive Matter: %v", err)
	}
	replayedPlan, replayed, err = repository.CreatePlan(
		ctx,
		owner.Actor,
		planCommand,
	)
	if err != nil || !replayed || replayedPlan.ID != plan.ID {
		t.Fatalf(
			"Plan replay after Matter change = (%+v, %t, %v)",
			replayedPlan,
			replayed,
			err,
		)
	}
	if _, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(
			owner,
			"plan-inactive-matter",
			"plan-inactive-matter-intent",
		),
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("new Plan with inactive Matter error = %v", err)
	}
	replayedBootstrap, replayed, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		sessionCommand,
	)
	if err != nil || !replayed ||
		replayedBootstrap.Session.ID != bootstrap.Session.ID {
		t.Fatalf(
			"Session replay after Matter change = (%+v, %t, %v)",
			replayedBootstrap,
			replayed,
			err,
		)
	}
	if _, err := repository.ResolveContextSessionByThread(
		ctx,
		owner.Actor,
		owner.ThreadID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("resolver with archived Matter error = %v", err)
	}
}

func TestContextRepositoryPersistsPreviewRevisionAndExplicitStart(
	t *testing.T,
) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	preparation := seedTargetedContextPreparation(t, pool, &owner)
	command := targetedContextPlanCommand(
		owner,
		preparation,
		"plan-targeted",
		"plan-targeted-intent",
	)
	multiRoleCreate := command
	multiRoleCreate.PlanID = "plan-targeted-multi-role"
	multiRoleCreate.Intent = contextIntent(
		"/v1/practice-plans",
		"plan-targeted-multi-role-intent",
		"create-targeted-multi-role-plan",
	)
	multiRoleCatalog := *command.CatalogSnapshot
	multiRoleCatalog.SelectedRoles = append(
		append(
			[]persistence.RoleSnapshot(nil),
			command.CatalogSnapshot.SelectedRoles...,
		),
		contextHRRoleSnapshot(command.ScenarioDefinitionID),
	)
	multiRoleCreate.SelectedRoleIDs = []string{
		"role_technical_interviewer",
		"role_hr_interviewer",
	}
	multiRoleCreate.CatalogSnapshot = &multiRoleCatalog
	multiRoleCreate.PracticeFocuses = append(
		append(
			[]persistence.PracticeObjective(nil),
			command.PracticeFocuses...,
		),
		persistence.PracticeObjective{
			ID:          "communication",
			Description: "Practice communication with concrete evidence.",
		},
	)
	if _, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		multiRoleCreate,
	); !errors.Is(err, persistence.ErrInvalidArgument) {
		t.Fatalf("multi-role targeted CreatePlan error = %v", err)
	}

	plan, replayed, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		command,
	)
	if err != nil || replayed || plan.PreparationSnapshot == nil ||
		plan.CatalogSnapshot == nil || plan.SessionPolicy == nil ||
		plan.Revision != 1 {
		t.Fatalf("Create targeted Plan = (%+v, %t, %v)", plan, replayed, err)
	}
	assertContextRowCount(
		t,
		pool,
		"practice_sessions",
		owner.Actor.UserID,
		0,
	)
	recovered, err := repository.GetPlan(ctx, owner.Actor, plan.ID)
	if err != nil ||
		!reflect.DeepEqual(recovered.PreparationSnapshot, &preparation) ||
		!reflect.DeepEqual(recovered.CatalogSnapshot, plan.CatalogSnapshot) {
		t.Fatalf("recover targeted Plan = (%+v, %v)", recovered, err)
	}

	revisedPolicy := *plan.SessionPolicy
	revisedPolicy.MaxEffectiveTurns = 5
	multiRoleRevision := persistence.UpdatePlanCommand{
		PlanID:               plan.ID,
		ExpectedPlanRevision: 1,
		SelectedRoleIDs:      append([]string(nil), multiRoleCreate.SelectedRoleIDs...),
		CatalogSnapshot:      multiRoleCatalog,
		SessionPolicy:        *plan.SessionPolicy,
		PracticeFocuses: append(
			[]persistence.PracticeObjective(nil),
			multiRoleCreate.PracticeFocuses...,
		),
		Intent: persistence.ContextIdempotencyIntent{
			Method:             "PUT",
			CanonicalPath:      "/v1/practice-plans/" + plan.ID,
			Key:                "plan-targeted-multi-revision",
			PayloadFingerprint: sha256.Sum256([]byte("multi-revision")),
		},
	}
	if _, _, err := repository.UpdatePlan(
		ctx,
		owner.Actor,
		multiRoleRevision,
	); !errors.Is(err, persistence.ErrInvalidArgument) {
		t.Fatalf("multi-role targeted UpdatePlan error = %v", err)
	}
	update := persistence.UpdatePlanCommand{
		PlanID:               plan.ID,
		ExpectedPlanRevision: 1,
		SelectedRoleIDs:      append([]string(nil), plan.SelectedRoleIDs...),
		CatalogSnapshot:      *plan.CatalogSnapshot,
		SessionPolicy:        revisedPolicy,
		PracticeFocuses: append(
			[]persistence.PracticeObjective(nil),
			plan.PracticeFocuses...,
		),
		Intent: persistence.ContextIdempotencyIntent{
			Method:             "PUT",
			CanonicalPath:      "/v1/practice-plans/" + plan.ID,
			Key:                "plan-targeted-revision",
			PayloadFingerprint: sha256.Sum256([]byte("targeted-revision")),
		},
	}
	revised, replayed, err := repository.UpdatePlan(
		ctx,
		owner.Actor,
		update,
	)
	if err != nil || replayed || revised.Revision != 2 ||
		revised.SessionPolicy == nil ||
		revised.SessionPolicy.MaxEffectiveTurns != 5 ||
		!reflect.DeepEqual(
			revised.PreparationSnapshot,
			plan.PreparationSnapshot,
		) {
		t.Fatalf("Update targeted Plan = (%+v, %t, %v)", revised, replayed, err)
	}
	replayedRevision, replayed, err := repository.UpdatePlan(
		ctx,
		owner.Actor,
		update,
	)
	if err != nil || !replayed ||
		!reflect.DeepEqual(replayedRevision, revised) {
		t.Fatalf(
			"replay targeted Plan revision = (%+v, %t, %v)",
			replayedRevision,
			replayed,
			err,
		)
	}
	stale := update
	stale.Intent.Key = "plan-targeted-stale-revision"
	stale.Intent.PayloadFingerprint = sha256.Sum256([]byte("stale-revision"))
	if _, _, err := repository.UpdatePlan(
		ctx,
		owner.Actor,
		stale,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("stale Plan revision error = %v", err)
	}
	assertContextRowCount(
		t,
		pool,
		"practice_sessions",
		owner.Actor.UserID,
		0,
	)

	sessionCommand := targetedContextSessionCommand(
		owner,
		revised,
		"session-targeted",
		"session-targeted-snapshot",
		"session-targeted-intent",
	)
	bootstrap, replayed, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		sessionCommand,
	)
	if err != nil || replayed ||
		bootstrap.Snapshot.PlanRevision != revised.Revision ||
		!reflect.DeepEqual(
			bootstrap.Snapshot.Preparation,
			*revised.PreparationSnapshot,
		) ||
		!reflect.DeepEqual(
			bootstrap.Snapshot.SessionPolicy,
			*revised.SessionPolicy,
		) {
		t.Fatalf(
			"explicit targeted start = (%+v, %t, %v)",
			bootstrap,
			replayed,
			err,
		)
	}
	replayedBootstrap, replayed, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		sessionCommand,
	)
	if err != nil || !replayed ||
		replayedBootstrap.Session.ID != bootstrap.Session.ID {
		t.Fatalf(
			"replay targeted start = (%+v, %t, %v)",
			replayedBootstrap,
			replayed,
			err,
		)
	}
	assertContextRowCount(
		t,
		pool,
		"practice_sessions",
		owner.Actor.UserID,
		1,
	)
}

func TestContextRepositoryConcurrentCreatesAreExactlyOnce(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)

	const callers = 8
	planResults := make(chan persistence.Plan, callers)
	planReplays := make(chan bool, callers)
	planErrors := make(chan error, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			command := contextPlanCommand(
				owner,
				fmt.Sprintf("plan-concurrent-%d", index),
				"plan-concurrent-intent",
			)
			plan, replayed, err := repository.CreatePlan(
				ctx,
				owner.Actor,
				command,
			)
			planResults <- plan
			planReplays <- replayed
			planErrors <- err
		}(index)
	}
	wait.Wait()
	close(planResults)
	close(planReplays)
	close(planErrors)

	for err := range planErrors {
		if err != nil {
			t.Fatalf("concurrent CreatePlan: %v", err)
		}
	}
	var canonicalPlan persistence.Plan
	for result := range planResults {
		if canonicalPlan.ID == "" {
			canonicalPlan = result
		}
		if result.ID != canonicalPlan.ID {
			t.Fatalf(
				"concurrent Plan IDs = %q and %q",
				canonicalPlan.ID,
				result.ID,
			)
		}
	}
	replayCount := 0
	for replayed := range planReplays {
		if replayed {
			replayCount++
		}
	}
	if replayCount != callers-1 {
		t.Fatalf("concurrent Plan replay count = %d", replayCount)
	}
	assertContextRowCount(
		t,
		pool,
		"practice_plans",
		owner.Actor.UserID,
		1,
	)

	sessionResults := make(chan persistence.ContextSessionBootstrap, callers)
	sessionReplays := make(chan bool, callers)
	sessionErrors := make(chan error, callers)
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			command := contextSessionCommand(
				owner,
				canonicalPlan,
				fmt.Sprintf("session-concurrent-%d", index),
				fmt.Sprintf("session-snapshot-concurrent-%d", index),
				"session-concurrent-intent",
			)
			result, replayed, err := repository.CreateContextSession(
				ctx,
				owner.Actor,
				command,
			)
			sessionResults <- result
			sessionReplays <- replayed
			sessionErrors <- err
		}(index)
	}
	wait.Wait()
	close(sessionResults)
	close(sessionReplays)
	close(sessionErrors)
	for err := range sessionErrors {
		if err != nil {
			t.Fatalf("concurrent CreateContextSession: %v", err)
		}
	}
	var canonicalSession persistence.ContextSessionBootstrap
	for result := range sessionResults {
		if canonicalSession.Session.ID == "" {
			canonicalSession = result
		}
		if result.Session.ID != canonicalSession.Session.ID ||
			result.Snapshot.ID != canonicalSession.Snapshot.ID {
			t.Fatalf(
				"concurrent Session aggregates differ: %+v vs %+v",
				canonicalSession,
				result,
			)
		}
	}
	replayCount = 0
	for replayed := range sessionReplays {
		if replayed {
			replayCount++
		}
	}
	if replayCount != callers-1 {
		t.Fatalf("concurrent Session replay count = %d", replayCount)
	}
	assertContextRowCount(
		t,
		pool,
		"practice_sessions",
		owner.Actor.UserID,
		1,
	)
	assertContextRowCount(
		t,
		pool,
		"practice_session_snapshots",
		owner.Actor.UserID,
		1,
	)
	var idempotencyCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM practice_idempotency_records
		WHERE owner_user_id = $1
	`, owner.Actor.UserID).Scan(&idempotencyCount); err != nil {
		t.Fatalf("count idempotency records: %v", err)
	}
	if idempotencyCount != 2 {
		t.Fatalf("idempotency record count = %d, want 2", idempotencyCount)
	}
}

func TestContextRepositoryCreationCannotCrossAnchorStateChange(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)

	stateChange, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin anchor state change: %v", err)
	}
	if _, err := stateChange.Exec(ctx, `
		UPDATE agent_thread_matter_links
		SET is_active = false,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1
		  AND thread_id = $2
		  AND matter_id = $3
	`, owner.Actor.UserID, owner.ThreadID, owner.MatterID); err != nil {
		_ = stateChange.Rollback(ctx)
		t.Fatalf("stage anchor state change: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, err := repository.CreatePlan(
			context.Background(),
			owner.Actor,
			contextPlanCommand(
				owner,
				"plan-during-anchor-change",
				"plan-anchor-change-intent",
			),
		)
		result <- err
	}()
	select {
	case err := <-result:
		_ = stateChange.Rollback(ctx)
		t.Fatalf("CreatePlan crossed uncommitted anchor change: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := stateChange.Commit(ctx); err != nil {
		t.Fatalf("commit anchor state change: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, persistence.ErrNotFound) {
			t.Fatalf("CreatePlan after inactive anchor error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreatePlan remained blocked after anchor state commit")
	}
	assertContextRowCount(t, pool, "practice_plans", owner.Actor.UserID, 0)
	assertContextRowCount(
		t,
		pool,
		"practice_idempotency_records",
		owner.Actor.UserID,
		0,
	)
}

func TestContextRepositoryTransitionsResolveAmbiguityAndDelete(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	other := contextOwnerB()
	seedContextOwner(t, pool, &owner)
	seedContextOwner(t, pool, &other)

	plan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "plan-transition", "plan-transition-intent"),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	command := contextSessionCommand(
		owner,
		plan,
		"session-transition",
		"snapshot-transition",
		"session-transition-intent",
	)
	created, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		command,
	)
	if err != nil {
		t.Fatalf("CreateContextSession: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE practice_sessions
		SET status = 'in_progress',
		    version = 2,
		    started_at = transaction_timestamp(),
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND session_id = $2
	`, owner.Actor.UserID, created.Session.ID); err != nil {
		t.Fatalf("start formal Session fixture: %v", err)
	}

	pause := contextTransitionCommand(
		created.Session.ID,
		2,
		persistence.ContextSessionPause,
		"pause-transition-intent",
	)
	paused, replayed, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		pause,
	)
	if err != nil || replayed ||
		paused.Status != persistence.ContextSessionPaused ||
		paused.Version != 3 {
		t.Fatalf("Pause = (%+v, %t, %v)", paused, replayed, err)
	}
	replayedPause, replayed, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		pause,
	)
	if err != nil || !replayed ||
		replayedPause.Version != paused.Version {
		t.Fatalf(
			"Pause replay = (%+v, %t, %v)",
			replayedPause,
			replayed,
			err,
		)
	}
	conflictingPause := pause
	conflictingPause.Intent.PayloadFingerprint =
		sha256.Sum256([]byte("different pause"))
	if _, _, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		conflictingPause,
	); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("conflicting Pause replay error = %v", err)
	}

	resume := contextTransitionCommand(
		created.Session.ID,
		3,
		persistence.ContextSessionResume,
		"resume-transition-intent",
	)
	resumed, _, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		resume,
	)
	if err != nil || resumed.Status != persistence.ContextSessionProgress ||
		resumed.Version != 4 {
		t.Fatalf("Resume = (%+v, %v)", resumed, err)
	}
	end := contextTransitionCommand(
		created.Session.ID,
		4,
		persistence.ContextSessionEndEarly,
		"end-transition-intent",
	)
	ended, _, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		end,
	)
	if err != nil ||
		ended.Status != persistence.ContextSessionEndedEarly ||
		ended.Version != 5 ||
		ended.StartedAt == nil ||
		ended.EndedAt == nil ||
		ended.EndReason != "USER_ENDED" {
		t.Fatalf("EndEarly = (%+v, %v)", ended, err)
	}
	if _, err := repository.ResolveContextSessionByThread(
		ctx,
		owner.Actor,
		owner.ThreadID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("terminal resolver error = %v", err)
	}

	firstAmbiguousPlan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "plan-ambiguous-1", "plan-ambiguous-intent-1"),
	)
	if err != nil {
		t.Fatalf("create first ambiguous Plan: %v", err)
	}
	secondAmbiguousPlan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "plan-ambiguous-2", "plan-ambiguous-intent-2"),
	)
	if err != nil {
		t.Fatalf("create second ambiguous Plan: %v", err)
	}
	firstAmbiguousSession := contextSessionCommand(
		owner,
		firstAmbiguousPlan,
		"session-ambiguous-1",
		"snapshot-ambiguous-1",
		"session-ambiguous-intent-1",
	)
	secondAmbiguousSession := contextSessionCommand(
		owner,
		secondAmbiguousPlan,
		"session-ambiguous-2",
		"snapshot-ambiguous-2",
		"session-ambiguous-intent-2",
	)
	if _, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		firstAmbiguousSession,
	); err != nil {
		t.Fatalf("create first ambiguous Session: %v", err)
	}
	if _, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		secondAmbiguousSession,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("second Thread Session error = %v, want conflict", err)
	}
	if _, err := pool.Exec(
		ctx,
		`DROP INDEX practice_one_effective_session_per_agent_thread`,
	); err != nil {
		t.Fatalf("drop Thread uniqueness for ambiguity fixture: %v", err)
	}
	if _, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		secondAmbiguousSession,
	); err != nil {
		t.Fatalf("create second ambiguous Session: %v", err)
	}
	if _, err := repository.ResolveContextSessionByThread(
		ctx,
		owner.Actor,
		owner.ThreadID,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("ambiguous resolver error = %v", err)
	}
	if _, err := repository.ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		owner.MatterID,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("ambiguous exact resolver error = %v", err)
	}

	otherPlan, _, err := repository.CreatePlan(
		ctx,
		other.Actor,
		contextPlanCommand(other, "plan-other", "plan-other-intent"),
	)
	if err != nil || otherPlan.ID == "" {
		t.Fatalf("create other-owner Plan = (%+v, %v)", otherPlan, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users SET account_status = 'deleting'
		WHERE id = $1
	`, owner.Actor.UserID); err != nil {
		t.Fatalf("mark owner deleting: %v", err)
	}
	if err := repository.DeleteUserData(ctx, persistence.DeletionContext{
		UserID:     owner.Actor.UserID,
		Generation: 1,
	}); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}
	assertContextRowCount(t, pool, "practice_sessions", owner.Actor.UserID, 0)
	assertContextRowCount(t, pool, "practice_plans", owner.Actor.UserID, 0)
	assertContextRowCount(
		t,
		pool,
		"practice_idempotency_records",
		owner.Actor.UserID,
		0,
	)
	assertContextRowCount(t, pool, "practice_plans", other.Actor.UserID, 1)
	if _, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "resurrected", "resurrection-intent"),
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("stale Actor resurrection error = %v", err)
	}
	if err := repository.DeleteUserData(ctx, persistence.DeletionContext{
		UserID:     owner.Actor.UserID,
		Generation: 1,
	}); err != nil {
		t.Fatalf("replayed DeleteUserData: %v", err)
	}
}

type contextOwnerFixture struct {
	Actor             persistence.Actor
	MatterID          string
	ThreadID          string
	ProfileID         string
	PreparationID     string
	PreparationAt     time.Time
	BackgroundSummary string
}

func contextOwnerA() contextOwnerFixture {
	return contextOwnerFixture{
		Actor: persistence.Actor{
			UserID:    "10000000-0000-4000-8000-000000000112",
			SessionID: "11000000-0000-4000-8000-000000000112",
		},
		MatterID:          "12000000-0000-4000-8000-000000000112",
		ThreadID:          "13000000-0000-4000-8000-000000000112",
		ProfileID:         "profile-owner-a",
		PreparationID:     "preparation-owner-a",
		BackgroundSummary: "Backend engineer preparing for an interview.",
	}
}

func contextOwnerB() contextOwnerFixture {
	return contextOwnerFixture{
		Actor: persistence.Actor{
			UserID:    "20000000-0000-4000-8000-000000000112",
			SessionID: "21000000-0000-4000-8000-000000000112",
		},
		MatterID:          "22000000-0000-4000-8000-000000000112",
		ThreadID:          "23000000-0000-4000-8000-000000000112",
		ProfileID:         "profile-owner-b",
		PreparationID:     "preparation-owner-b",
		BackgroundSummary: "Product engineer preparing for an interview.",
	}
}

func newContextRepository(
	t *testing.T,
) (*practicepostgres.Repository, *pgxpool.Pool) {
	t.Helper()
	databaseURL, cleanup := isolatedDatabaseURL(t)
	t.Cleanup(cleanup)
	runner, err := migration.Open(databaseURL)
	if err != nil {
		t.Fatalf("open context migration runner: %v", err)
	}
	changed, err := runner.Up()
	if err != nil {
		_ = runner.Close()
		t.Fatalf("apply context migrations: %v", err)
	}
	if !changed {
		_ = runner.Close()
		t.Fatal("empty context database reported no migration")
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("close context migration runner: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open context PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return practicepostgres.New(pool), pool
}

func seedContextOwner(
	t *testing.T,
	pool *pgxpool.Pool,
	owner *contextOwnerFixture,
) {
	t.Helper()
	if owner == nil {
		t.Fatal("nil context owner fixture")
	}
	ensureIdentityUsers(t, pool, owner.Actor)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO matters (id, owner_user_id, title)
		VALUES ($1, $2, 'Interview preparation')
	`, owner.MatterID, owner.Actor.UserID); err != nil {
		t.Fatalf("insert Matter: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_threads (id, owner_user_id)
		VALUES ($1, $2)
	`, owner.ThreadID, owner.Actor.UserID); err != nil {
		t.Fatalf("insert AgentThread: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_thread_matter_links (
			owner_user_id, thread_id, matter_id, is_active
		) VALUES ($1, $2, $3, true)
	`, owner.Actor.UserID, owner.ThreadID, owner.MatterID); err != nil {
		t.Fatalf("insert Thread/Matter link: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO preparation_profiles (
			owner_user_id, profile_id, resume_ref,
			job_description_ref, background_summary
		) VALUES ($1, $2, 'resume-v1', 'job-v1', $3)
	`, owner.Actor.UserID, owner.ProfileID, owner.BackgroundSummary); err != nil {
		t.Fatalf("insert Preparation Profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id, snapshot_id, source_profile_id,
			source_version, resume_snapshot,
			job_description_snapshot, background_snapshot
		) VALUES ($1, $2, $3, 1, 'resume-v1', 'job-v1', $4)
		RETURNING created_at
	`,
		owner.Actor.UserID,
		owner.PreparationID,
		owner.ProfileID,
		owner.BackgroundSummary,
	).Scan(&owner.PreparationAt); err != nil {
		t.Fatalf("insert Preparation Snapshot: %v", err)
	}
}

func seedTargetedContextPreparation(
	t *testing.T,
	pool *pgxpool.Pool,
	owner *contextOwnerFixture,
) persistence.PreparationSnapshot {
	t.Helper()
	ctx := context.Background()
	input := persistence.JobTargetInputSnapshot{
		Source:              "quick_start",
		JobTitle:            "Backend engineer",
		Seniority:           "Senior",
		CandidateBackground: owner.BackgroundSummary,
	}
	candidate := persistence.JobTargetCandidateSnapshot{
		Source:             "quick_start",
		GeneralAdviceOnly:  true,
		JobTitle:           "Backend engineer",
		Seniority:          "Senior",
		Responsibilities:   []string{"Build reliable APIs."},
		CoreSkills:         []string{"Go services"},
		CommunicationFocus: []string{"Explain trade-offs."},
		PracticeGoals:      []string{"Practice a technical deep dive."},
		ScopeNotice:        "Limited to the interview content pack.",
		CatalogRecommendation: persistence.
			JobTargetCatalogRecommendationSnapshot{
			ScenarioDefinitionID:      "scn_programmer_interview",
			ScenarioDefinitionVersion: 1,
			SelectedRoleIDs: []string{
				"role_technical_interviewer",
			},
			PracticeOptionID:      "option_full_simulation",
			PracticeOptionVersion: 1,
		},
	}
	inputDocument, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal targeted input: %v", err)
	}
	candidateDocument, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("marshal targeted candidate: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO preparation_job_targets (
			owner_user_id, target_id, source_kind, job_title,
			seniority, candidate_background, input_version, stage
		) VALUES (
			$1, 'target-owner-a', 'quick_start', $2, $3, $4, 1, 'confirmed'
		)
	`,
		owner.Actor.UserID,
		input.JobTitle,
		input.Seniority,
		input.CandidateBackground,
	); err != nil {
		t.Fatalf("insert targeted JobTarget: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO preparation_job_target_analysis_attempts (
			owner_user_id, target_id, input_version, attempt_number,
			status, lease_until, candidate, finished_at
		) VALUES (
			$1, 'target-owner-a', 1, 1, 'succeeded',
			transaction_timestamp(), $2, transaction_timestamp()
		)
	`, owner.Actor.UserID, candidateDocument); err != nil {
		t.Fatalf("insert targeted analysis: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO preparation_job_target_confirmations (
			owner_user_id, target_id, input_version, analysis_version,
			confirmation_version, candidate, input_snapshot
		) VALUES ($1, 'target-owner-a', 1, 1, 1, $2, $3)
	`, owner.Actor.UserID, candidateDocument, inputDocument); err != nil {
		t.Fatalf("insert targeted confirmation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE preparation_profiles
		SET job_target_id = 'target-owner-a',
		    job_target_confirmation_version = 1,
		    version = 2,
		    updated_at = transaction_timestamp()
		WHERE owner_user_id = $1 AND profile_id = $2
	`, owner.Actor.UserID, owner.ProfileID); err != nil {
		t.Fatalf("bind targeted Profile: %v", err)
	}
	var legacyUntouched int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM preparation_snapshots
		WHERE owner_user_id = $1
		  AND snapshot_id = $2
		  AND source_job_target_id IS NULL
		  AND job_target_input_snapshot IS NULL
		  AND job_target_candidate_snapshot IS NULL
	`, owner.Actor.UserID, owner.PreparationID).Scan(&legacyUntouched); err != nil {
		t.Fatalf("verify legacy Snapshot: %v", err)
	}
	if legacyUntouched != 1 {
		t.Fatal("legacy Snapshot was backfilled or reinterpreted")
	}

	snapshot := persistence.PreparationSnapshot{
		ID:                                 "preparation-targeted-owner-a",
		SourceProfileID:                    owner.ProfileID,
		SourceVersion:                      2,
		SourceJobTargetID:                  "target-owner-a",
		SourceJobTargetConfirmationVersion: 1,
		JobTargetInputSnapshot:             &input,
		JobTargetCandidateSnapshot:         &candidate,
		BackgroundSnapshot:                 owner.BackgroundSummary,
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id, snapshot_id, source_profile_id, source_version,
			background_snapshot, source_job_target_id,
			source_job_target_confirmation_version,
			job_target_input_snapshot, job_target_candidate_snapshot
		) VALUES ($1, $2, $3, 2, $4, $5, 1, $6, $7)
		RETURNING created_at
	`,
		owner.Actor.UserID,
		snapshot.ID,
		owner.ProfileID,
		owner.BackgroundSummary,
		snapshot.SourceJobTargetID,
		inputDocument,
		candidateDocument,
	).Scan(&snapshot.CreatedAt); err != nil {
		t.Fatalf("insert targeted Preparation Snapshot: %v", err)
	}
	snapshot.CreatedAt = snapshot.CreatedAt.UTC()
	owner.PreparationID = snapshot.ID
	owner.PreparationAt = snapshot.CreatedAt
	return snapshot
}

func targetedContextPlanCommand(
	owner contextOwnerFixture,
	preparation persistence.PreparationSnapshot,
	planID string,
	idempotencyKey string,
) persistence.CreatePlanCommand {
	role := persistence.RoleSnapshot{
		ID:                   "role_technical_interviewer",
		ScenarioDefinitionID: "scn_programmer_interview",
		Type:                 "TECHNICAL_INTERVIEWER",
		DisplayName:          "Technical depth perspective",
		Responsibilities:     "Probe technical depth and engineering trade-offs.",
		Style:                "Precise and evidence seeking.",
		FocusAreas:           []string{"system_design", "project_depth"},
		Version:              1,
	}
	catalog := persistence.PlanCatalogSnapshot{
		ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
			ID:      "scn_programmer_interview",
			Type:    "INTERVIEW",
			Model:   persistence.ScenarioModelProjectExperienceDeepDive,
			Name:    "English interview for technical roles",
			Version: 1,
			Status:  "active",
		},
		ScenarioConfig: persistence.ScenarioConfigSnapshot{
			ID:                   "scfg_backend_engineer",
			ScenarioDefinitionID: "scn_programmer_interview",
			Type:                 "INTERVIEW",
			Model:                persistence.ScenarioModelProjectExperienceDeepDive,
			Version:              1,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable APIs.",
			PromptModel:          contextScenarioPromptModel(),
		},
		SelectedRoles: []persistence.RoleSnapshot{role},
		PracticeOption: persistence.PracticeOptionSnapshot{
			ID:                   "option_full_simulation",
			ScenarioDefinitionID: "scn_programmer_interview",
			Type:                 "FULL_SIMULATION",
			DisplayName:          "Full simulation",
			Version:              1,
		},
	}
	policy := persistence.ContextSessionPolicy{
		SuggestedDurationSeconds: 900,
		MinEffectiveTurns:        4,
		MaxEffectiveTurns:        6,
		CoverageCheckpointTurn:   4,
		MaxFollowUpsPerQuestion:  1,
		TargetObjectives: []persistence.PracticeObjective{
			{
				ID:          "introduction",
				Description: "Explain current experience clearly.",
			},
			{
				ID:          "system_design",
				Description: "Explain technical trade-offs.",
			},
		},
		EarlyCompletionRule: "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
	}
	focuses := []persistence.PracticeObjective{
		{
			ID:          "system_design",
			Description: "Explain technical trade-offs.",
		},
		{
			ID:          "project_depth",
			Description: "Support decisions with concrete evidence.",
		},
	}
	return persistence.CreatePlanCommand{
		PlanID:                    planID,
		AgentThreadID:             owner.ThreadID,
		MatterID:                  owner.MatterID,
		ScenarioDefinitionID:      catalog.ScenarioDefinition.ID,
		ScenarioDefinitionVersion: catalog.ScenarioDefinition.Version,
		ScenarioType:              catalog.ScenarioDefinition.Type,
		ScenarioModel:             catalog.ScenarioDefinition.Model,
		ScenarioConfigID:          catalog.ScenarioConfig.ID,
		ScenarioConfigVersion:     catalog.ScenarioConfig.Version,
		PreparationProfileID:      owner.ProfileID,
		SelectedRoleIDs:           []string{role.ID},
		PreparationSnapshot:       &preparation,
		CatalogSnapshot:           &catalog,
		SessionPolicy:             &policy,
		PracticeFocuses:           focuses,
		Intent: contextIntent(
			"/v1/practice-plans",
			idempotencyKey,
			"create-targeted-plan-payload",
		),
	}
}

func contextHRRoleSnapshot(
	scenarioDefinitionID string,
) persistence.RoleSnapshot {
	return persistence.RoleSnapshot{
		ID:                   "role_hr_interviewer",
		ScenarioDefinitionID: scenarioDefinitionID,
		Type:                 "HR_INTERVIEWER",
		DisplayName:          "Recruiter and motivation perspective",
		Responsibilities:     "Explore motivation and communication clarity.",
		Style:                "Warm and structured.",
		FocusAreas:           []string{"communication", "motivation"},
		Version:              1,
	}
}

func contextScenarioPromptModel() persistence.ScenarioPromptModel {
	return persistence.ScenarioPromptModel{
		PublicSceneBrief:         "Discuss one backend project.",
		PracticeGoal:             "Explain decisions with evidence.",
		UserRole:                 "Candidate",
		AIRole:                   "Technical interviewer",
		PersonaSummary:           "A precise interviewer.",
		FocusAreas:               []string{"introduction", "system_design"},
		TurnBlueprints:           []string{"Clarify the project"},
		SuggestedDurationSeconds: 900,
	}
}

func targetedContextSessionCommand(
	owner contextOwnerFixture,
	plan persistence.Plan,
	sessionID string,
	snapshotID string,
	idempotencyKey string,
) persistence.CreateContextSessionCommand {
	catalog := plan.CatalogSnapshot
	role := catalog.SelectedRoles[0]
	snapshot := persistence.ContextSessionSnapshot{
		ID:                 snapshotID,
		SessionID:          sessionID,
		PlanRevision:       plan.Revision,
		ScenarioType:       plan.ScenarioType,
		ScenarioModel:      plan.ScenarioModel,
		ScenarioDefinition: catalog.ScenarioDefinition,
		ScenarioConfig:     catalog.ScenarioConfig,
		Preparation:        *plan.PreparationSnapshot,
		Participants: []persistence.ContextParticipant{
			{
				ID:        sessionID + "-interviewer",
				SessionID: sessionID,
				Role:      "INTERVIEWER",
				SubjectRef: persistence.SubjectRef{
					Namespace: "speakup.role",
					SubjectID: role.ID,
				},
				RoleDefinitionID: role.ID,
				RoleSnapshot:     &role,
				Order:            1,
			},
			{
				ID:        sessionID + "-candidate",
				SessionID: sessionID,
				Role:      "CANDIDATE",
				SubjectRef: persistence.SubjectRef{
					Namespace: "speakup.user",
					SubjectID: owner.Actor.UserID,
				},
				Order: 2,
			},
		},
		PracticeOption:  catalog.PracticeOption,
		SessionPolicy:   *plan.SessionPolicy,
		PracticeFocuses: append([]persistence.PracticeObjective(nil), plan.PracticeFocuses...),
	}
	return persistence.CreateContextSessionCommand{
		SessionID:             sessionID,
		SnapshotID:            snapshotID,
		PlanID:                plan.ID,
		ExpectedPlanRevision:  plan.Revision,
		PreparationSnapshotID: plan.PreparationSnapshot.ID,
		Snapshot:              snapshot,
		Intent: contextIntent(
			"/v1/practice-plans/"+plan.ID+"/practice-sessions",
			idempotencyKey,
			"create-targeted-session-payload",
		),
	}
}

func contextPlanCommand(
	owner contextOwnerFixture,
	planID string,
	idempotencyKey string,
) persistence.CreatePlanCommand {
	return persistence.CreatePlanCommand{
		PlanID:                    planID,
		AgentThreadID:             owner.ThreadID,
		MatterID:                  owner.MatterID,
		ScenarioDefinitionID:      "scn_programmer_interview",
		ScenarioDefinitionVersion: 1,
		ScenarioType:              "INTERVIEW",
		ScenarioModel:             persistence.ScenarioModelProjectExperienceDeepDive,
		ScenarioConfigID:          "scfg_backend_engineer",
		ScenarioConfigVersion:     1,
		PreparationProfileID:      owner.ProfileID,
		SelectedRoleIDs: []string{
			"role_technical_interviewer",
		},
		Intent: contextIntent(
			"/v1/practice-plans",
			idempotencyKey,
			"create-plan-payload",
		),
	}
}

func contextSessionCommand(
	owner contextOwnerFixture,
	plan persistence.Plan,
	sessionID string,
	snapshotID string,
	idempotencyKey string,
) persistence.CreateContextSessionCommand {
	role := persistence.RoleSnapshot{
		ID:                   "role_technical_interviewer",
		ScenarioDefinitionID: plan.ScenarioDefinitionID,
		Type:                 "TECHNICAL_INTERVIEWER",
		DisplayName:          "Technical depth perspective",
		Responsibilities:     "Probe technical depth and engineering trade-offs.",
		Style:                "Precise and evidence seeking.",
		FocusAreas:           []string{"system_design", "project_depth"},
		Version:              1,
	}
	preparationAt := owner.PreparationAt
	if preparationAt.IsZero() {
		preparationAt = time.Date(
			2026,
			time.July,
			26,
			10,
			0,
			0,
			0,
			time.UTC,
		)
	}
	snapshot := persistence.ContextSessionSnapshot{
		ID:            snapshotID,
		SessionID:     sessionID,
		PlanRevision:  plan.Revision,
		ScenarioType:  plan.ScenarioType,
		ScenarioModel: plan.ScenarioModel,
		ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
			ID:      plan.ScenarioDefinitionID,
			Type:    plan.ScenarioType,
			Model:   plan.ScenarioModel,
			Name:    "English interview for technical roles",
			Version: plan.ScenarioDefinitionVersion,
			Status:  "active",
		},
		ScenarioConfig: persistence.ScenarioConfigSnapshot{
			ID:                   plan.ScenarioConfigID,
			ScenarioDefinitionID: plan.ScenarioDefinitionID,
			Type:                 plan.ScenarioType,
			Model:                plan.ScenarioModel,
			Version:              plan.ScenarioConfigVersion,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable APIs.",
			PromptModel:          contextScenarioPromptModel(),
		},
		Preparation: persistence.PreparationSnapshot{
			ID:                     owner.PreparationID,
			SourceProfileID:        owner.ProfileID,
			SourceVersion:          1,
			ResumeSnapshot:         "resume-v1",
			JobDescriptionSnapshot: "job-v1",
			BackgroundSnapshot:     owner.BackgroundSummary,
			CreatedAt:              preparationAt,
		},
		Participants: []persistence.ContextParticipant{
			{
				ID:        sessionID + "-interviewer",
				SessionID: sessionID,
				Role:      "INTERVIEWER",
				SubjectRef: persistence.SubjectRef{
					Namespace: "speakup.role",
					SubjectID: role.ID,
				},
				RoleDefinitionID: role.ID,
				RoleSnapshot:     &role,
				Order:            1,
			},
			{
				ID:        sessionID + "-candidate",
				SessionID: sessionID,
				Role:      "CANDIDATE",
				SubjectRef: persistence.SubjectRef{
					Namespace: "speakup.user",
					SubjectID: owner.Actor.UserID,
				},
				Order: 2,
			},
		},
		PracticeOption: persistence.PracticeOptionSnapshot{
			ID:                   "option_full_simulation",
			ScenarioDefinitionID: plan.ScenarioDefinitionID,
			Type:                 "FULL_SIMULATION",
			DisplayName:          "Full simulation",
			Version:              1,
		},
		SessionPolicy: persistence.ContextSessionPolicy{
			SuggestedDurationSeconds: 900,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			TargetObjectives: []persistence.PracticeObjective{
				{
					ID:          "introduction",
					Description: "Explain current experience clearly.",
				},
				{
					ID:          "system_design",
					Description: "Explain technical trade-offs.",
				},
			},
			EarlyCompletionRule: "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
		},
		PracticeFocuses: []persistence.PracticeObjective{{
			ID:          "system_design",
			Description: "Explain technical trade-offs.",
		}},
	}
	return persistence.CreateContextSessionCommand{
		SessionID:             sessionID,
		SnapshotID:            snapshotID,
		PlanID:                plan.ID,
		ExpectedPlanRevision:  plan.Revision,
		PreparationSnapshotID: owner.PreparationID,
		Snapshot:              snapshot,
		Intent: contextIntent(
			"/v1/practice-plans/"+plan.ID+"/practice-sessions",
			idempotencyKey,
			"create-session-payload",
		),
	}
}

func contextTransitionCommand(
	sessionID string,
	expectedVersion int,
	transition persistence.ContextSessionTransition,
	key string,
) persistence.TransitionContextSessionCommand {
	pathTransition := string(transition)
	if transition == persistence.ContextSessionEndEarly {
		pathTransition = "end-early"
	}
	return persistence.TransitionContextSessionCommand{
		SessionID:              sessionID,
		ExpectedSessionVersion: expectedVersion,
		Transition:             transition,
		Intent: contextIntent(
			"/v1/practice-sessions/"+sessionID+"/"+pathTransition,
			key,
			fmt.Sprintf("version:%d", expectedVersion),
		),
	}
}

func contextIntent(
	path string,
	key string,
	payload string,
) persistence.ContextIdempotencyIntent {
	return persistence.ContextIdempotencyIntent{
		Method:             "POST",
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256([]byte(payload)),
	}
}

func assertContextRowCount(
	t *testing.T,
	pool *pgxpool.Pool,
	table string,
	ownerUserID string,
	want int,
) {
	t.Helper()
	var count int
	query := fmt.Sprintf(
		"SELECT count(*) FROM %s WHERE owner_user_id = $1",
		table,
	)
	if err := pool.QueryRow(
		context.Background(),
		query,
		ownerUserID,
	).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}
