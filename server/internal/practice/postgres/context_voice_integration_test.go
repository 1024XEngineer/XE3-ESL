package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
)

func TestContextVoiceExactActivationRestartAndConcurrentSixthTurn(
	t *testing.T,
) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	other := contextOwnerB()
	seedContextOwner(t, pool, &owner)
	seedContextOwner(t, pool, &other)

	plan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "plan-voice-six", "plan-voice-six-key"),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	created, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		contextSessionCommand(
			owner,
			plan,
			"session-voice-six",
			"snapshot-voice-six",
			"session-voice-six-key",
		),
	)
	if err != nil {
		t.Fatalf("CreateContextSession: %v", err)
	}
	resolved, err := repository.ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		owner.MatterID,
	)
	if err != nil || resolved.Session.ID != created.Session.ID {
		t.Fatalf("ResolveContextSession = (%+v, %v)", resolved, err)
	}
	if _, err := repository.ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		other.MatterID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("wrong Matter resolver error = %v", err)
	}
	if _, err := repository.ResolveContextSession(
		ctx,
		other.Actor,
		owner.ThreadID,
		owner.MatterID,
	); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("cross-owner resolver error = %v", err)
	}

	activated, err := repository.ActivateContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
		owner.ThreadID,
		owner.MatterID,
	)
	if err != nil ||
		activated.Session.Status != persistence.ContextSessionProgress ||
		activated.Session.Version != 2 ||
		activated.Session.StartedAt == nil {
		t.Fatalf("ActivateContextSession = (%+v, %v)", activated, err)
	}
	replayedActivation, err := repository.ActivateContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
		owner.ThreadID,
		owner.MatterID,
	)
	if err != nil ||
		replayedActivation.Session.Version != activated.Session.Version ||
		replayedActivation.Session.StartedAt == nil ||
		!replayedActivation.Session.StartedAt.Equal(
			*activated.Session.StartedAt,
		) {
		t.Fatalf(
			"replayed ActivateContextSession = (%+v, %v)",
			replayedActivation,
			err,
		)
	}

	restarted := practicepostgres.New(pool)
	for turn := 1; turn <= 5; turn++ {
		result, advanceErr := restarted.AdvanceContextVoiceTurn(
			ctx,
			owner.Actor,
			contextVoiceTurnCommand(
				created.Session.ID,
				fmt.Sprintf("turn-six-%d", turn),
			),
		)
		if advanceErr != nil || result.EffectiveTurns != turn ||
			result.TurnLimit != 6 || result.Completed {
			t.Fatalf("Advance turn %d = (%+v, %v)", turn, result, advanceErr)
		}
	}

	const workers = 16
	results := make(chan persistence.TurnResult, workers)
	failures := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, advanceErr := restarted.AdvanceContextVoiceTurn(
				ctx,
				owner.Actor,
				contextVoiceTurnCommand(
					created.Session.ID,
					"turn-six-final",
				),
			)
			if advanceErr != nil {
				failures <- advanceErr
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for advanceErr := range failures {
		t.Errorf("concurrent final Turn: %v", advanceErr)
	}
	var canonical persistence.TurnResult
	for result := range results {
		if canonical.TurnID == "" {
			canonical = result
		}
		if result.TurnID != "turn-six-final" ||
			result.EffectiveTurns != 6 ||
			result.TurnLimit != 6 ||
			!result.Completed ||
			result.SessionVersion != 8 ||
			result.CompletionToken != canonical.CompletionToken {
			t.Errorf("concurrent final result = %+v", result)
		}
	}
	if canonical.TurnID == "" {
		t.Fatal("concurrent final Turn returned no success")
	}

	recovered, err := practicepostgres.New(pool).GetContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
	)
	if err != nil ||
		recovered.Status != persistence.ContextSessionCompleted ||
		recovered.EffectiveTurns != 6 ||
		recovered.Version != 8 ||
		recovered.StartedAt == nil ||
		recovered.EndedAt == nil ||
		recovered.EndReason != "TURN_LIMIT_REACHED" {
		t.Fatalf("recovered completed Session = (%+v, %v)", recovered, err)
	}
	var storedFinalTurns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM practice_turn_results
		WHERE owner_user_id = $1
		  AND session_id = $2
		  AND turn_id = 'turn-six-final'
	`, owner.Actor.UserID, created.Session.ID).Scan(
		&storedFinalTurns,
	); err != nil || storedFinalTurns != 1 {
		t.Fatalf("stored final Turns = (%d, %v)", storedFinalTurns, err)
	}
	replayedFinal, err := restarted.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "turn-six-final"),
	)
	if err != nil || replayedFinal.CompletionToken != canonical.CompletionToken {
		t.Fatalf("replayed final Turn = (%+v, %v)", replayedFinal, err)
	}
	conflicting := contextVoiceTurnCommand(created.Session.ID, "turn-six-final")
	conflicting.Payload = []byte("different")
	if _, err := restarted.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		conflicting,
	); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("conflicting final replay error = %v", err)
	}
	if _, err := restarted.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "turn-after-complete"),
	); !errors.Is(err, persistence.ErrSessionCompleted) {
		t.Fatalf("post-completion Turn error = %v", err)
	}
}

func TestContextVoiceResolverRestoresOnlyUniqueCompletedSession(
	t *testing.T,
) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)

	plan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(
			owner,
			"plan-voice-completed",
			"plan-voice-completed-key",
		),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	first, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		contextSessionCommand(
			owner,
			plan,
			"session-voice-completed-1",
			"snapshot-voice-completed-1",
			"session-voice-completed-key-1",
		),
	)
	if err != nil {
		t.Fatalf("Create first Context Session: %v", err)
	}
	completeContextVoiceSession(
		t,
		repository,
		owner.Actor,
		first.Session.ID,
		"completed-first",
	)

	restarted := practicepostgres.New(pool)
	resolved, err := restarted.ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		owner.MatterID,
	)
	if err != nil ||
		resolved.Session.ID != first.Session.ID ||
		resolved.Session.Status != persistence.ContextSessionCompleted {
		t.Fatalf("restart completed resolver = (%+v, %v)", resolved, err)
	}

	second, _, err := restarted.CreateContextSession(
		ctx,
		owner.Actor,
		contextSessionCommand(
			owner,
			plan,
			"session-voice-completed-2",
			"snapshot-voice-completed-2",
			"session-voice-completed-key-2",
		),
	)
	if err != nil {
		t.Fatalf("Create second Context Session: %v", err)
	}
	resolved, err = restarted.ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		owner.MatterID,
	)
	if err != nil || resolved.Session.ID != second.Session.ID {
		t.Fatalf("effective Session did not win: (%+v, %v)", resolved, err)
	}
	completeContextVoiceSession(
		t,
		restarted,
		owner.Actor,
		second.Session.ID,
		"completed-second",
	)

	if _, err := practicepostgres.New(pool).ResolveContextSession(
		ctx,
		owner.Actor,
		owner.ThreadID,
		owner.MatterID,
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("two completed Sessions resolver error = %v", err)
	}
}

func completeContextVoiceSession(
	t *testing.T,
	repository *practicepostgres.Repository,
	actor persistence.Actor,
	sessionID string,
	keyPrefix string,
) {
	t.Helper()
	for turn := 1; turn <= 6; turn++ {
		result, err := repository.AdvanceContextVoiceTurn(
			context.Background(),
			actor,
			contextVoiceTurnCommand(
				sessionID,
				fmt.Sprintf("%s-turn-%d", keyPrefix, turn),
			),
		)
		if err != nil ||
			result.Completed != (turn == 6) ||
			result.EffectiveTurns != turn {
			t.Fatalf("complete Context Session turn %d = (%+v, %v)",
				turn, result, err)
		}
	}
}

func TestContextVoiceStartingThreeTurnAndPausedLifecycle(t *testing.T) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)

	plan, _, err := repository.CreatePlan(
		ctx,
		owner.Actor,
		contextPlanCommand(owner, "plan-voice-focus", "plan-voice-focus-key"),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	command := contextSessionCommand(
		owner,
		plan,
		"session-voice-focus",
		"snapshot-voice-focus",
		"session-voice-focus-key",
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

	// A worker that reaches the formal Session before the Start response may
	// atomically activate it while consuming the first effective Turn.
	first, err := repository.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "focus-turn-1"),
	)
	if err != nil || first.EffectiveTurns != 1 ||
		first.TurnLimit != 3 || first.Completed {
		t.Fatalf("starting first Turn = (%+v, %v)", first, err)
	}
	started, err := repository.GetContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
	)
	if err != nil ||
		started.Status != persistence.ContextSessionProgress ||
		started.StartedAt == nil ||
		started.Version != 2 {
		t.Fatalf("started Session = (%+v, %v)", started, err)
	}

	paused, _, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		contextTransitionCommand(
			created.Session.ID,
			started.Version,
			persistence.ContextSessionPause,
			"focus-pause-key",
		),
	)
	if err != nil || paused.Status != persistence.ContextSessionPaused {
		t.Fatalf("Pause = (%+v, %v)", paused, err)
	}
	if _, err := repository.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "paused-turn"),
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("paused Turn error = %v", err)
	}
	resumed, _, err := repository.TransitionContextSession(
		ctx,
		owner.Actor,
		contextTransitionCommand(
			created.Session.ID,
			paused.Version,
			persistence.ContextSessionResume,
			"focus-resume-key",
		),
	)
	if err != nil || resumed.Status != persistence.ContextSessionProgress {
		t.Fatalf("Resume = (%+v, %v)", resumed, err)
	}
	second, err := repository.AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "focus-turn-2"),
	)
	if err != nil || second.EffectiveTurns != 2 || second.Completed {
		t.Fatalf("second Turn = (%+v, %v)", second, err)
	}
	third, err := practicepostgres.New(pool).AdvanceContextVoiceTurn(
		ctx,
		owner.Actor,
		contextVoiceTurnCommand(created.Session.ID, "focus-turn-3"),
	)
	if err != nil || third.EffectiveTurns != 3 ||
		third.TurnLimit != 3 || !third.Completed {
		t.Fatalf("third Turn = (%+v, %v)", third, err)
	}
}

func contextVoiceTurnCommand(
	sessionID string,
	turnID string,
) persistence.ConsumeTurnCommand {
	return persistence.ConsumeTurnCommand{
		SessionID: sessionID,
		TurnID:    turnID,
		Payload:   []byte("conversation-turn:" + turnID),
	}
}
