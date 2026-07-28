package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	persistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
	migrationfiles "github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestVoiceApplicationUsesDurableSnapshotAndTurnIdempotency(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)
	plan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(
			owner,
			"plan-voice-application",
			"plan-voice-application-key",
		),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	command := contextSessionCommand(
		owner,
		plan,
		"voice-application-session",
		"voice-application-snapshot",
		"voice-application-session-key",
	)
	command.Snapshot.PracticeOption.Type = "FOCUS"
	command.Snapshot.PracticeOption.RoleDefinitionID =
		"role_technical_interviewer"
	command.Snapshot.PracticeOption.DisplayName = "Technical focus"
	command.Snapshot.SessionPolicy.SuggestedDurationSeconds = 600
	command.Snapshot.SessionPolicy.MinEffectiveTurns = 1
	command.Snapshot.SessionPolicy.MaxEffectiveTurns = 3
	command.Snapshot.SessionPolicy.CoverageCheckpointTurn = 1
	created, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		command,
	)
	if err != nil {
		t.Fatalf("CreateContextSession: %v", err)
	}
	activated, err := repository.ActivateContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
		owner.ThreadID,
		owner.MatterID,
		contextIntent(
			"/v1/agent-threads/"+owner.ThreadID+"/voice-practice-sessions",
			"voice-application-start-key",
			"",
		),
	)
	if err != nil {
		t.Fatalf("ActivateContextSession: %v", err)
	}

	application, err := practice.NewVoiceApplication(
		repository,
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewVoiceApplication: %v", err)
	}
	participantID, err := application.ResolveActorParticipant(
		ctx,
		owner.Actor,
		created.Session.ID,
	)
	if err != nil {
		t.Fatalf("ResolveActorParticipant: %v", err)
	}
	if participantID != created.Snapshot.Participants[1].ID {
		t.Fatalf("participant ID = %q", participantID)
	}
	first, err := application.ApplyEffectiveTurn(
		ctx,
		owner.Actor,
		created.Session.ID,
		"voice-turn-1",
	)
	if err != nil {
		t.Fatalf("ApplyEffectiveTurn: %v", err)
	}
	if first.EffectiveTurns != 1 || first.SessionCompleted {
		t.Fatalf("first progress = %#v", first)
	}
	if first.SessionVersion != activated.Session.Version+1 ||
		first.TurnLimit != 3 {
		t.Fatalf("first progress evidence = %#v", first)
	}

	restarted, err := practice.NewVoiceApplication(
		practicepostgres.New(pool),
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("restart NewVoiceApplication: %v", err)
	}
	replayed, err := restarted.ApplyEffectiveTurn(
		ctx,
		owner.Actor,
		created.Session.ID,
		"voice-turn-1",
	)
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if replayed != first {
		t.Fatalf("replayed progress = %#v, want %#v", replayed, first)
	}
	if recoveredID, err := restarted.ResolveActorParticipant(
		ctx,
		owner.Actor,
		created.Session.ID,
	); err != nil || recoveredID != participantID {
		t.Fatalf("resolved after restart = %q, %v", recoveredID, err)
	}
}

func TestRepositoryContract(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	owner := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	ensureIdentityUsers(t, pool, owner)

	session, err := repository.CreateSession(
		ctx,
		owner,
		newSession("session-contract", "plan-contract", 3),
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.OwnerUserID != owner.UserID ||
		session.Status != persistence.SessionStatusActive ||
		session.EffectiveTurns != 0 ||
		session.Snapshot.TurnLimit != 3 {
		t.Fatalf("created session = %+v", session)
	}

	if _, err := repository.CreateSession(
		ctx,
		owner,
		newSession("session-other", "plan-contract", 3),
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("second active session error = %v, want conflict", err)
	}

	first, err := repository.ConsumeTurn(ctx, owner, persistence.ConsumeTurnCommand{
		SessionID: session.ID,
		TurnID:    "turn-1",
		Payload:   []byte(`{"answer":"first"}`),
	})
	if err != nil {
		t.Fatalf("ConsumeTurn first: %v", err)
	}
	replayed, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-1",
			Payload:   []byte(`{"answer":"first"}`),
		},
	)
	if err != nil {
		t.Fatalf("ConsumeTurn replay: %v", err)
	}
	if replayed != first {
		t.Fatalf("replayed result = %+v, want %+v", replayed, first)
	}

	if _, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-1",
			Payload:   []byte(`{"answer":"changed"}`),
		},
	); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v, want idempotency conflict", err)
	}

	if _, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-2",
			Payload:   []byte("second"),
		},
	); err != nil {
		t.Fatalf("ConsumeTurn second: %v", err)
	}
	completed, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-3",
			Payload:   []byte("third"),
		},
	)
	if err != nil {
		t.Fatalf("ConsumeTurn third: %v", err)
	}
	if !completed.Completed || completed.Round != 3 ||
		completed.EffectiveTurns != 3 ||
		completed.CompletionToken == "" {
		t.Fatalf("completion result = %+v", completed)
	}

	replayedThird, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-3",
			Payload:   []byte("third"),
		},
	)
	if err != nil {
		t.Fatalf("replay third turn: %v", err)
	}
	if replayedThird != completed {
		t.Fatalf("third replay = %+v, want %+v", replayedThird, completed)
	}
	if _, err := repository.ConsumeTurn(
		ctx,
		owner,
		persistence.ConsumeTurnCommand{
			SessionID: session.ID,
			TurnID:    "turn-4",
			Payload:   []byte("fourth"),
		},
	); !errors.Is(err, persistence.ErrSessionCompleted) {
		t.Fatalf("fourth turn error = %v, want completed", err)
	}

	recovered, err := practicepostgres.New(pool).GetSession(
		ctx,
		owner,
		session.ID,
	)
	if err != nil {
		t.Fatalf("recover session with restarted repository: %v", err)
	}
	if recovered.Status != persistence.SessionStatusCompleted ||
		recovered.EffectiveTurns != 3 ||
		recovered.Version != 4 ||
		recovered.Snapshot.TurnLimit != 3 {
		t.Fatalf("recovered session = %+v", recovered)
	}
}

func TestTurnIDCannotAdvanceTwoSessions(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000d",
		SessionID: "20000000-0000-4000-8000-00000000000d",
	}
	ensureIdentityUsers(t, pool, actor)
	firstSession := newSession("turn-scope-first", "turn-scope-plan-first", 3)
	secondSession := newSession("turn-scope-second", "turn-scope-plan-second", 3)
	for _, command := range []persistence.CreateSessionCommand{
		firstSession,
		secondSession,
	} {
		if _, err := repository.CreateSession(ctx, actor, command); err != nil {
			t.Fatalf("CreateSession %s: %v", command.SessionID, err)
		}
	}

	turnID := "globally-scoped-turn"
	if _, err := repository.ConsumeTurn(
		ctx,
		actor,
		persistence.ConsumeTurnCommand{
			SessionID: firstSession.SessionID,
			TurnID:    turnID,
			Payload:   []byte("first-session"),
		},
	); err != nil {
		t.Fatalf("first ConsumeTurn: %v", err)
	}
	if _, err := repository.ConsumeTurn(
		ctx,
		actor,
		persistence.ConsumeTurnCommand{
			SessionID: secondSession.SessionID,
			TurnID:    turnID,
			Payload:   []byte("second-session"),
		},
	); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf(
			"cross-session ConsumeTurn error = %v, want idempotency conflict",
			err,
		)
	}
	second, err := repository.GetSession(ctx, actor, secondSession.SessionID)
	if err != nil {
		t.Fatalf("GetSession second: %v", err)
	}
	if second.EffectiveTurns != 0 || second.Version != 1 {
		t.Fatalf("second session advanced: %+v", second)
	}
}

func TestConcurrentSameTurnAdvancesExactlyOnce(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000002",
		SessionID: "20000000-0000-4000-8000-000000000002",
	}
	ensureIdentityUsers(t, pool, actor)
	command := newSession("concurrent-session", "concurrent-plan", 3)
	if _, err := repository.CreateSession(ctx, actor, command); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const workers = 16
	start := make(chan struct{})
	results := make(chan persistence.TurnResult, workers)
	errs := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		go func() {
			ready.Done()
			<-start
			result, err := repository.ConsumeTurn(
				ctx,
				actor,
				persistence.ConsumeTurnCommand{
					SessionID: command.SessionID,
					TurnID:    "same-turn",
					Payload:   []byte("same-payload"),
				},
			)
			results <- result
			errs <- err
		}()
	}
	ready.Wait()
	close(start)

	var first persistence.TurnResult
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent ConsumeTurn: %v", err)
		}
		result := <-results
		if i == 0 {
			first = result
		} else if result != first {
			t.Fatalf("result %d = %+v, want %+v", i, result, first)
		}
	}
	if first.Round != 1 || first.SessionVersion != 2 {
		t.Fatalf("first result = %+v", first)
	}

	session, err := repository.GetSession(ctx, actor, command.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.EffectiveTurns != 1 || session.Version != 2 {
		t.Fatalf("session after concurrency = %+v", session)
	}
}

func TestConcurrentSessionCreationKeepsOneActivePerPlan(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000a",
		SessionID: "20000000-0000-4000-8000-00000000000a",
	}
	ensureIdentityUsers(t, pool, actor)

	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for index := range workers {
		go func() {
			ready.Done()
			<-start
			_, err := repository.CreateSession(
				ctx,
				actor,
				newSession(
					fmt.Sprintf("concurrent-create-%02d", index),
					"one-active-plan",
					3,
				),
			)
			errs <- err
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	conflicts := 0
	for range workers {
		switch err := <-errs; {
		case err == nil:
			successes++
		case errors.Is(err, persistence.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent CreateSession error = %v", err)
		}
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf(
			"successes/conflicts = %d/%d, want 1/%d",
			successes,
			conflicts,
			workers-1,
		)
	}
	sessions, err := repository.ListSessions(ctx, actor)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("active session count = %d, want 1", len(sessions))
	}
}

func TestActorIsolationAndDeletion(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	ownerA := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000003",
		SessionID: "20000000-0000-4000-8000-000000000003",
	}
	ownerB := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000004",
		SessionID: "20000000-0000-4000-8000-000000000004",
	}
	ensureIdentityUsers(t, pool, ownerA, ownerB)

	// Composite ownership permits the same opaque IDs for different users
	// without allowing either user to observe the other's rows.
	for _, actor := range []persistence.Actor{ownerA, ownerB} {
		if _, err := repository.CreateSession(
			ctx,
			actor,
			newSession("shared-session-id", "shared-plan-id", 3),
		); err != nil {
			t.Fatalf("CreateSession for %s: %v", actor.UserID, err)
		}
	}

	if _, err := repository.ConsumeTurn(
		ctx,
		ownerA,
		persistence.ConsumeTurnCommand{
			SessionID: "shared-session-id",
			TurnID:    "a-turn",
			Payload:   []byte("owner-a"),
		},
	); err != nil {
		t.Fatalf("owner A ConsumeTurn: %v", err)
	}
	sessionB, err := repository.GetSession(ctx, ownerB, "shared-session-id")
	if err != nil {
		t.Fatalf("owner B GetSession: %v", err)
	}
	if sessionB.EffectiveTurns != 0 {
		t.Fatalf("owner B turns = %d, want 0", sessionB.EffectiveTurns)
	}

	listA, err := repository.ListSessions(ctx, ownerA)
	if err != nil {
		t.Fatalf("owner A ListSessions: %v", err)
	}
	listB, err := repository.ListSessions(ctx, ownerB)
	if err != nil {
		t.Fatalf("owner B ListSessions: %v", err)
	}
	if len(listA) != 1 || len(listB) != 1 ||
		listA[0].OwnerUserID != ownerA.UserID ||
		listB[0].OwnerUserID != ownerB.UserID {
		t.Fatalf("isolated lists = %+v / %+v", listA, listB)
	}

	deletion := persistence.DeletionContext{
		UserID:     ownerA.UserID,
		Generation: 7,
	}
	if err := repository.DeleteUserData(ctx, deletion); !errors.Is(
		err,
		persistence.ErrNotFound,
	) {
		t.Fatalf("active-user DeleteUserData error = %v, want not found", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users SET account_status = 'deleting' WHERE id = $1
	`, ownerA.UserID); err != nil {
		t.Fatalf("fence owner A for deletion: %v", err)
	}
	if _, err := repository.GetSession(
		ctx,
		ownerA,
		"shared-session-id",
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("deleting owner GetSession error = %v, want not found", err)
	}
	hidden, err := repository.ListSessions(ctx, ownerA)
	if err != nil {
		t.Fatalf("deleting owner ListSessions: %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("deleting owner list length = %d, want 0", len(hidden))
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM identity_users WHERE id = $1
	`, ownerA.UserID); err == nil {
		t.Fatal("Identity physical delete bypassed Practice owner cleanup")
	}
	if err := repository.DeleteUserData(ctx, deletion); err != nil {
		t.Fatalf("first DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(ctx, deletion); err != nil {
		t.Fatalf("repeated DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(ctx, persistence.DeletionContext{
		UserID:     ownerA.UserID,
		Generation: 6,
	}); !errors.Is(err, persistence.ErrDeletionGeneration) {
		t.Fatalf("stale DeleteUserData error = %v, want generation conflict", err)
	}
	var storedGeneration int64
	if err := pool.QueryRow(ctx, `
		SELECT deletion_generation
		FROM practice_deletion_fences
		WHERE owner_user_id = $1
	`, ownerA.UserID).Scan(&storedGeneration); err != nil {
		t.Fatalf("read deletion generation: %v", err)
	}
	if storedGeneration != 7 {
		t.Fatalf("deletion generation = %d, want 7", storedGeneration)
	}
	if _, err := repository.GetSession(
		ctx,
		ownerA,
		"shared-session-id",
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("owner A recovery after deletion error = %v", err)
	}
	if _, err := repository.ConsumeTurn(
		ctx,
		ownerA,
		persistence.ConsumeTurnCommand{
			SessionID: "shared-session-id",
			TurnID:    "after-delete",
			Payload:   []byte("forbidden"),
		},
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("owner A turn after deletion error = %v", err)
	}
	if _, err := repository.GetSession(
		ctx,
		ownerB,
		"shared-session-id",
	); err != nil {
		t.Fatalf("owner B session was affected by deletion: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM identity_users WHERE id = $1
	`, ownerA.UserID); err != nil {
		t.Fatalf("physical delete after Practice cleanup: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT deletion_generation
		FROM practice_deletion_fences
		WHERE owner_user_id = $1
	`, ownerA.UserID).Scan(&storedGeneration); err != nil {
		t.Fatalf("deletion fence did not survive Identity removal: %v", err)
	}
	if err := repository.DeleteUserData(ctx, deletion); err != nil {
		t.Fatalf("same generation replay after Identity removal: %v", err)
	}
	if err := repository.DeleteUserData(ctx, persistence.DeletionContext{
		UserID:     ownerA.UserID,
		Generation: 8,
	}); err != nil {
		t.Fatalf("higher generation replay after Identity removal: %v", err)
	}
	if err := repository.DeleteUserData(ctx, deletion); !errors.Is(
		err,
		persistence.ErrDeletionGeneration,
	) {
		t.Fatalf("lower generation after Identity removal error = %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT deletion_generation
		FROM practice_deletion_fences
		WHERE owner_user_id = $1
	`, ownerA.UserID).Scan(&storedGeneration); err != nil {
		t.Fatalf("read advanced deletion generation: %v", err)
	}
	if storedGeneration != 8 {
		t.Fatalf("advanced deletion generation = %d, want 8", storedGeneration)
	}
}

func TestConcurrentFirstDeletionGenerationIsIdempotent(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000f",
		SessionID: "20000000-0000-4000-8000-00000000000f",
	}
	ensureIdentityUsers(t, pool, actor)
	if _, err := repository.CreateSession(
		ctx,
		actor,
		newSession("concurrent-delete-session", "concurrent-delete-plan", 3),
	); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users SET account_status = 'deleting' WHERE id = $1
	`, actor.UserID); err != nil {
		t.Fatalf("set deleting status: %v", err)
	}

	deletion := persistence.DeletionContext{
		UserID:     actor.UserID,
		Generation: 11,
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- repository.DeleteUserData(context.Background(), deletion)
		}()
	}
	close(start)
	for call := 1; call <= 2; call++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent DeleteUserData call %d: %v", call, err)
		}
	}

	var generation, sessionCount int64
	if err := pool.QueryRow(ctx, `
		SELECT
		    (SELECT deletion_generation
		     FROM practice_deletion_fences
		     WHERE owner_user_id = $1),
		    (SELECT count(*)
		     FROM practice_sessions
		     WHERE owner_user_id = $1)
	`, actor.UserID).Scan(&generation, &sessionCount); err != nil {
		t.Fatalf("inspect concurrent deletion: %v", err)
	}
	if generation != 11 || sessionCount != 0 {
		t.Fatalf(
			"generation/session count = %d/%d, want 11/0",
			generation,
			sessionCount,
		)
	}
}

func TestDeletionFenceSerializesWithStaleActorWrite(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000009",
		SessionID: "20000000-0000-4000-8000-000000000009",
	}
	ensureIdentityUsers(t, pool, actor)
	existing := newSession("deletion-race-existing", "deletion-race-plan", 3)
	if _, err := repository.CreateSession(ctx, actor, existing); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	deletionTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deletion transaction: %v", err)
	}
	defer func() {
		_ = deletionTx.Rollback(context.Background())
	}()
	if _, err := deletionTx.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, actor.UserID); err != nil {
		t.Fatalf("set deleting status: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := repository.ConsumeTurn(
			context.Background(),
			actor,
			persistence.ConsumeTurnCommand{
				SessionID: existing.SessionID,
				TurnID:    "stale-turn",
				Payload:   []byte("must-not-commit"),
			},
		)
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("stale Actor write bypassed uncommitted deletion fence: %v", err)
	case <-time.After(150 * time.Millisecond):
		// The Practice FOR SHARE lock must wait for the deletion transaction's
		// account-status update rather than reading stale active state.
	}

	if err := deletionTx.Commit(ctx); err != nil {
		t.Fatalf("commit deletion fence: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, persistence.ErrNotFound) {
			t.Fatalf("stale ConsumeTurn error = %v, want not found", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale Actor write did not finish after deletion committed")
	}

	if _, err := repository.CreateSession(
		ctx,
		actor,
		newSession("deletion-race-new", "deletion-race-new-plan", 3),
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("stale CreateSession error = %v, want not found", err)
	}
	if _, err := repository.GetSession(
		ctx,
		actor,
		existing.SessionID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("GetSession after identity fence error = %v", err)
	}
	var effectiveTurns, version int
	if err := pool.QueryRow(ctx, `
		SELECT effective_turns, version
		FROM practice_sessions
		WHERE owner_user_id = $1 AND session_id = $2
	`, actor.UserID, existing.SessionID).Scan(
		&effectiveTurns,
		&version,
	); err != nil {
		t.Fatalf("inspect session after fenced write: %v", err)
	}
	if effectiveTurns != 0 || version != 1 {
		t.Fatalf(
			"session turns/version changed across deletion fence: %d/%d",
			effectiveTurns,
			version,
		)
	}

	if err := repository.DeleteUserData(ctx, persistence.DeletionContext{
		UserID:     actor.UserID,
		Generation: 9,
	}); err != nil {
		t.Fatalf("DeleteUserData after fence: %v", err)
	}
}

func TestModuleDeletionFenceBlocksReadsAndWrites(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000c",
		SessionID: "20000000-0000-4000-8000-00000000000c",
	}
	ensureIdentityUsers(t, pool, actor)
	session := newSession("module-fenced-session", "module-fenced-plan", 3)
	if _, err := repository.CreateSession(ctx, actor, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_deletion_fences (
			owner_user_id, deletion_generation
		)
		VALUES ($1, 1)
	`, actor.UserID); err != nil {
		t.Fatalf("insert module deletion fence: %v", err)
	}

	if _, err := repository.GetSession(
		ctx,
		actor,
		session.SessionID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("fenced GetSession error = %v, want not found", err)
	}
	sessions, err := repository.ListSessions(ctx, actor)
	if err != nil {
		t.Fatalf("fenced ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("fenced list length = %d, want 0", len(sessions))
	}
	if _, err := repository.ConsumeTurn(
		ctx,
		actor,
		persistence.ConsumeTurnCommand{
			SessionID: session.SessionID,
			TurnID:    "fenced-turn",
			Payload:   []byte("blocked"),
		},
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("fenced ConsumeTurn error = %v, want not found", err)
	}
	if _, err := repository.CreateSession(
		ctx,
		actor,
		newSession("module-fenced-new", "module-fenced-new-plan", 3),
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("fenced CreateSession error = %v, want not found", err)
	}
	if err := repository.DeleteSession(
		ctx,
		actor,
		session.SessionID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("fenced DeleteSession error = %v, want not found", err)
	}
}

func TestUnauthorizedIDGuessIsIndistinguishableFromMissing(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	owner := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000005",
		SessionID: "20000000-0000-4000-8000-000000000005",
	}
	attacker := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000006",
		SessionID: "20000000-0000-4000-8000-000000000006",
	}
	ensureIdentityUsers(t, pool, owner, attacker)
	if _, err := repository.CreateSession(
		ctx,
		owner,
		newSession("private-session", "private-plan", 3),
	); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for name, operation := range map[string]func() error{
		"get guessed": func() error {
			_, err := repository.GetSession(ctx, attacker, "private-session")
			return err
		},
		"get missing": func() error {
			_, err := repository.GetSession(ctx, attacker, "missing-session")
			return err
		},
		"advance guessed": func() error {
			_, err := repository.ConsumeTurn(
				ctx,
				attacker,
				persistence.ConsumeTurnCommand{
					SessionID: "private-session",
					TurnID:    "guessed-turn",
					Payload:   []byte("guess"),
				},
			)
			return err
		},
		"delete guessed": func() error {
			return repository.DeleteSession(ctx, attacker, "private-session")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, persistence.ErrNotFound) {
				t.Fatalf("error = %v, want not found", err)
			}
		})
	}
}

func TestConsumeTurnRollbackLeavesNoPartialData(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000007",
		SessionID: "20000000-0000-4000-8000-000000000007",
	}
	ensureIdentityUsers(t, pool, actor)
	command := newSession("rollback-session", "rollback-plan", 3)
	if _, err := repository.CreateSession(ctx, actor, command); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_practice_session_advance()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
		    RAISE EXCEPTION 'intentional test failure';
		END;
		$$;
		CREATE TRIGGER fail_practice_session_advance
		BEFORE UPDATE ON practice_sessions
		FOR EACH ROW
		EXECUTE FUNCTION fail_practice_session_advance();
	`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	turn := persistence.ConsumeTurnCommand{
		SessionID: command.SessionID,
		TurnID:    "rolled-back-turn",
		Payload:   []byte("rollback"),
	}
	if _, err := repository.ConsumeTurn(ctx, actor, turn); err == nil {
		t.Fatal("ConsumeTurn unexpectedly succeeded")
	}

	var turnCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM practice_turn_results
		WHERE owner_user_id = $1 AND session_id = $2
	`, actor.UserID, command.SessionID).Scan(&turnCount); err != nil {
		t.Fatalf("count turn results: %v", err)
	}
	if turnCount != 0 {
		t.Fatalf("turn result count = %d, want 0", turnCount)
	}
	session, err := repository.GetSession(ctx, actor, command.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.EffectiveTurns != 0 || session.Version != 1 {
		t.Fatalf("session after rollback = %+v", session)
	}
}

func TestCreateSessionRollbackLeavesNoPartialAggregate(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000b",
		SessionID: "20000000-0000-4000-8000-00000000000b",
	}
	ensureIdentityUsers(t, pool, actor)

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_practice_snapshot_insert()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
		    RAISE EXCEPTION 'intentional snapshot failure';
		END;
		$$;
		CREATE TRIGGER fail_practice_snapshot_insert
		BEFORE INSERT ON practice_session_snapshots
		FOR EACH ROW
		EXECUTE FUNCTION fail_practice_snapshot_insert();
	`); err != nil {
		t.Fatalf("install snapshot failure trigger: %v", err)
	}

	command := newSession("rolled-back-session", "rolled-back-plan", 3)
	if _, err := repository.CreateSession(ctx, actor, command); err == nil {
		t.Fatal("CreateSession unexpectedly succeeded")
	}
	var sessionCount, snapshotCount int
	if err := pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM practice_sessions
		     WHERE owner_user_id = $1 AND session_id = $2),
		    (SELECT count(*) FROM practice_session_snapshots
		     WHERE owner_user_id = $1 AND session_id = $2)
	`, actor.UserID, command.SessionID).Scan(
		&sessionCount,
		&snapshotCount,
	); err != nil {
		t.Fatalf("count rolled-back aggregate: %v", err)
	}
	if sessionCount != 0 || snapshotCount != 0 {
		t.Fatalf(
			"session/snapshot count = %d/%d, want 0/0",
			sessionCount,
			snapshotCount,
		)
	}
}

func TestCreateSessionRejectsEmptyOrDuplicateSnapshotMembers(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-00000000000e",
		SessionID: "20000000-0000-4000-8000-00000000000e",
	}
	ensureIdentityUsers(t, pool, actor)

	tests := map[string]func(*persistence.SessionSnapshot){
		"no targets": func(snapshot *persistence.SessionSnapshot) {
			snapshot.TargetIDs = []string{}
		},
		"empty target": func(snapshot *persistence.SessionSnapshot) {
			snapshot.TargetIDs = []string{"target-1", " "}
		},
		"duplicate target": func(snapshot *persistence.SessionSnapshot) {
			snapshot.TargetIDs = []string{"target-1", "target-1"}
		},
		"no participants": func(snapshot *persistence.SessionSnapshot) {
			snapshot.Participants = []persistence.ParticipantSnapshot{}
		},
	}
	index := 0
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			index++
			command := newSession(
				fmt.Sprintf("invalid-snapshot-%d", index),
				fmt.Sprintf("invalid-snapshot-plan-%d", index),
				3,
			)
			mutate(&command.Snapshot)
			if _, err := repository.CreateSession(
				ctx,
				actor,
				command,
			); !errors.Is(err, persistence.ErrInvalidArgument) {
				t.Fatalf("CreateSession error = %v, want invalid argument", err)
			}
		})
	}
}

func TestSnapshotIsImmutable(t *testing.T) {
	repository, pool := newRepository(t)
	ctx := context.Background()
	actor := persistence.Actor{
		UserID:    "10000000-0000-4000-8000-000000000008",
		SessionID: "20000000-0000-4000-8000-000000000008",
	}
	ensureIdentityUsers(t, pool, actor)
	command := newSession("snapshot-session", "snapshot-plan", 5)
	if _, err := repository.CreateSession(ctx, actor, command); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE practice_session_snapshots
		SET turn_limit = 3
		WHERE owner_user_id = $1 AND session_id = $2
	`, actor.UserID, command.SessionID); err == nil {
		t.Fatal("snapshot update unexpectedly succeeded")
	}
	session, err := repository.GetSession(ctx, actor, command.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.Snapshot.TurnLimit != 5 {
		t.Fatalf("turn limit = %d, want 5", session.Snapshot.TurnLimit)
	}

	for round := 1; round <= 5; round++ {
		result, err := repository.ConsumeTurn(
			ctx,
			actor,
			persistence.ConsumeTurnCommand{
				SessionID: command.SessionID,
				TurnID:    fmt.Sprintf("five-round-turn-%d", round),
				Payload:   []byte(fmt.Sprintf("round-%d", round)),
			},
		)
		if err != nil {
			t.Fatalf("ConsumeTurn round %d: %v", round, err)
		}
		if result.Completed != (round == 5) {
			t.Fatalf(
				"round %d completed = %t, want %t",
				round,
				result.Completed,
				round == 5,
			)
		}
	}
}

func TestMigrationFullChainUpDownAndReapply(t *testing.T) {
	requiredDependencies := []string{
		"000002_identity_schema.up.sql",
		"000002_identity_schema.down.sql",
		"000003_agent_data.up.sql",
		"000003_agent_data.down.sql",
		"000004_agent_runs.up.sql",
		"000004_agent_runs.down.sql",
		"000005_agent_run_trust_boundaries.up.sql",
		"000005_agent_run_trust_boundaries.down.sql",
		"000006_agent_run_worker_leases.up.sql",
		"000006_agent_run_worker_leases.down.sql",
		"000007_agent_thread_focus.up.sql",
		"000007_agent_thread_focus.down.sql",
	}
	for _, name := range requiredDependencies {
		if _, err := fs.Stat(migrationfiles.Files, name); errors.Is(
			err,
			fs.ErrNotExist,
		) {
			t.Skipf(
				"full migration chain requires embedded dependency %s; "+
					"000002-000007 must be available in the stacked base",
				name,
			)
		} else if err != nil {
			t.Fatalf("inspect embedded migration %s: %v", name, err)
		}
	}

	databaseURL, cleanup := isolatedDatabaseURL(t)
	defer cleanup()

	runner, err := migration.Open(databaseURL)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	defer func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	}()
	changed, err := runner.Up()
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !changed {
		t.Fatal("Up from empty schema reported no change")
	}
	status, err := runner.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if !status.Present || status.Dirty || status.Version < 8 {
		t.Fatalf(
			"status after full Up = %+v, want clean version >= 8",
			status,
		)
	}
	latestVersion := status.Version

	assertPracticeTables := func(wantPresent bool) {
		t.Helper()

		pool, err := pgxpool.New(context.Background(), databaseURL)
		if err != nil {
			t.Fatalf("open migration inspection pool: %v", err)
		}
		defer pool.Close()

		var tableName *string
		if err := pool.QueryRow(context.Background(), `
			SELECT to_regclass('practice_sessions')::text
		`).Scan(&tableName); err != nil {
			t.Fatalf("inspect Practice migration tables: %v", err)
		}
		if (tableName != nil) != wantPresent {
			t.Fatalf(
				"Practice table presence = %t, want %t",
				tableName != nil,
				wantPresent,
			)
		}
	}

	for status.Version > 8 {
		changed, err = runner.DownOne()
		if err != nil {
			t.Fatalf("DownOne from version %d: %v", status.Version, err)
		}
		if !changed {
			t.Fatalf(
				"DownOne from version %d reported no change",
				status.Version,
			)
		}
		status, err = runner.Version()
		if err != nil {
			t.Fatalf("Version while returning to 000008: %v", err)
		}
		if !status.Present || status.Dirty || status.Version < 8 {
			t.Fatalf(
				"status while returning to 000008 = %+v",
				status,
			)
		}
	}
	if status.Version != 8 {
		t.Fatalf("version after later downs = %d, want 8", status.Version)
	}
	assertPracticeTables(true)

	changed, err = runner.DownOne()
	if err != nil {
		t.Fatalf("DownOne from version 8: %v", err)
	}
	if !changed {
		t.Fatal("DownOne from version 8 reported no change")
	}
	status, err = runner.Version()
	if err != nil {
		t.Fatalf("Version after DownOne: %v", err)
	}
	if !status.Present || status.Dirty || status.Version != 7 {
		t.Fatalf("status after DownOne = %+v, want clean version 7", status)
	}
	assertPracticeTables(false)

	changed, err = runner.Up()
	if err != nil {
		t.Fatalf("Up after DownOne: %v", err)
	}
	if !changed {
		t.Fatal("Up from version 7 reported no change")
	}
	status, err = runner.Version()
	if err != nil {
		t.Fatalf("Version after re-applying 000006: %v", err)
	}
	if !status.Present || status.Dirty || status.Version != latestVersion {
		t.Fatalf(
			"status after re-applying migrations = %+v, want clean version %d",
			status,
			latestVersion,
		)
	}
	assertPracticeTables(true)
}

func newRepository(t *testing.T) (*practicepostgres.Repository, *pgxpool.Pool) {
	t.Helper()

	databaseURL, cleanup := isolatedDatabaseURL(t)
	t.Cleanup(cleanup)

	applyStackedMigrations(t, databaseURL)

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return practicepostgres.New(pool), pool
}

func applyStackedMigrations(t *testing.T, databaseURL string) {
	t.Helper()

	dependencyDirectory := strings.TrimSpace(
		os.Getenv("PRACTICE_TEST_DEPENDENCY_MIGRATIONS_DIR"),
	)
	if dependencyDirectory == "" {
		dependencyDirectory = filepath.Clean("../../../migrations")
	}
	required := []string{
		"000002_identity_schema.up.sql",
		"000003_agent_data.up.sql",
		"000004_agent_runs.up.sql",
		"000005_agent_run_trust_boundaries.up.sql",
		"000006_agent_run_worker_leases.up.sql",
		"000007_agent_thread_focus.up.sql",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(dependencyDirectory, name)); err != nil {
			t.Skip(
				"Practice PostgreSQL tests require stacked migrations " +
					"000002-000007; set " +
					"PRACTICE_TEST_DEPENDENCY_MIGRATIONS_DIR",
			)
		}
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open stacked migration pool: %v", err)
	}
	defer pool.Close()

	currentDirectory := filepath.Clean("../../../migrations")
	migrations := []string{
		filepath.Join(currentDirectory, "000001_database_baseline.up.sql"),
	}
	for _, name := range required {
		migrations = append(migrations, filepath.Join(dependencyDirectory, name))
	}
	migrations = append(
		migrations,
		filepath.Join(currentDirectory, "000008_practice_sessions.up.sql"),
	)
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read stacked migration %s: %v", path, err)
		}
		if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
			t.Fatalf("apply stacked migration %s: %v", path, err)
		}
	}
}

func ensureIdentityUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	actors ...persistence.Actor,
) {
	t.Helper()

	for index, actor := range actors {
		email := fmt.Sprintf(
			"practice-%s-%d@example.test",
			strings.ReplaceAll(actor.UserID, "-", ""),
			index,
		)
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO identity_users (id, canonical_email)
			VALUES ($1, $2)
			ON CONFLICT (id) DO NOTHING
		`, actor.UserID, email); err != nil {
			t.Fatalf("insert identity user %s: %v", actor.UserID, err)
		}
	}
}

func isolatedDatabaseURL(t *testing.T) (string, func()) {
	t.Helper()

	baseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}

	adminConfig, err := pgx.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL config: %v", err)
	}
	admin, err := pgx.ConnectConfig(context.Background(), adminConfig)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf(
		"practice_test_%d_%d",
		time.Now().UnixNano(),
		schemaCounter.Add(1),
	)
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		_ = admin.Close(context.Background())
		t.Fatalf("create test schema: %v", err)
	}

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(
			ctx,
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		if err := admin.Close(ctx); err != nil {
			t.Errorf("close PostgreSQL admin connection: %v", err)
		}
	}
	return parsed.String(), cleanup
}

func newSession(
	sessionID string,
	planID string,
	turnLimit int,
) persistence.CreateSessionCommand {
	return persistence.CreateSessionCommand{
		SessionID: sessionID,
		PlanID:    planID,
		Snapshot: persistence.SessionSnapshot{
			Mode:      "FOCUSED",
			TargetIDs: []string{"target-1"},
			Participants: []persistence.ParticipantSnapshot{
				{
					ParticipantID:   "interviewer-1",
					ParticipantRole: "INTERVIEWER",
					SubjectRef: persistence.SubjectRef{
						Namespace: "agent",
						SubjectID: "interviewer",
					},
					Order: 0,
				},
				{
					ParticipantID:   "candidate-1",
					ParticipantRole: "CANDIDATE",
					SubjectRef: persistence.SubjectRef{
						Namespace: "user",
						SubjectID: "candidate",
					},
					Order: 1,
				},
			},
			TurnLimit: turnLimit,
		},
	}
}

var schemaCounter atomic.Uint64
