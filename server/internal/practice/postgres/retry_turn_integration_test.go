package postgres_test

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestRetryAuthorizationAllowsEligibleProgressAndCompletedSession(
	t *testing.T,
) {
	repository, pool := newContextRepository(t)
	ctx := context.Background()
	owner := contextOwnerA()
	seedContextOwner(t, pool, &owner)

	planCommand := contextPlanCommand(
		owner,
		"plan-daily-retry",
		"plan-daily-retry-key",
	)
	planCommand.ScenarioDefinitionID = "scn_daily_retry"
	planCommand.ScenarioType = persistence.ScenarioFamilyDaily
	planCommand.ScenarioModel =
		persistence.ScenarioModelDailyBasicDialogue
	planCommand.ScenarioConfigID = "scfg_daily_retry"
	plan, _, err := repository.CreatePlan(ctx, owner.Actor, planCommand)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	sessionCommand := contextSessionCommand(
		owner,
		plan,
		"session-daily-retry",
		"snapshot-daily-retry",
		"session-daily-retry-key",
	)
	sessionCommand.Snapshot.ScenarioConfig.JobTitle = ""
	sessionCommand.Snapshot.ScenarioConfig.JobDescription = ""
	created, _, err := repository.CreateContextSession(
		ctx,
		owner.Actor,
		sessionCommand,
	)
	if err != nil {
		t.Fatalf("CreateContextSession: %v", err)
	}
	if _, err := repository.ActivateContextSession(
		ctx,
		owner.Actor,
		created.Session.ID,
		owner.ThreadID,
		owner.MatterID,
		contextIntent(
			"/v1/agent-threads/"+owner.ThreadID+
				"/voice-practice-sessions",
			"start-daily-retry-key",
			"",
		),
	); err != nil {
		t.Fatalf("ActivateContextSession: %v", err)
	}

	application, err := practice.NewRetryTurnApplication(
		repository,
		"speakup.user",
	)
	if err != nil {
		t.Fatal(err)
	}
	actor := requestcontext.Actor{
		UserID:    owner.Actor.UserID,
		SessionID: owner.Actor.SessionID,
	}
	firstCommand := practice.AuthorizeSameQuestionRetryCommand{
		RetryRequestID:    "91000000-0000-4000-8000-000000000001",
		PracticeSessionID: created.Session.ID,
		OriginalTurnID:    "turn-daily-original",
		QuestionID:        "question-daily-original",
	}
	first, err := application.AuthorizeSameQuestionRetry(
		ctx,
		actor,
		firstCommand,
	)
	if err != nil {
		t.Fatalf("authorize in-progress retry: %v", err)
	}
	if first.CountsTowardEffectiveLimit ||
		first.ScenarioType != persistence.ScenarioFamilyDaily ||
		first.ScenarioModel !=
			persistence.ScenarioModelDailyBasicDialogue ||
		first.SessionStatusAtAuthorization !=
			persistence.ContextSessionProgress {
		t.Fatalf("in-progress authorization = %#v", first)
	}
	participantID, err := application.ResolveAuthorizedParticipant(
		ctx,
		actor,
		firstCommand.RetryRequestID,
	)
	if err != nil ||
		participantID != created.Snapshot.Participants[1].ID {
		t.Fatalf("in-progress retry participant = %q, %v", participantID, err)
	}
	replayed, err := application.AuthorizeSameQuestionRetry(
		ctx,
		actor,
		firstCommand,
	)
	if err != nil || replayed != first {
		t.Fatalf("replayed authorization = %#v, %v", replayed, err)
	}

	for turn := 1; turn <=
		created.Snapshot.SessionPolicy.MaxEffectiveTurns; turn++ {
		if _, err := repository.AdvanceContextVoiceTurn(
			ctx,
			owner.Actor,
			persistence.ConsumeTurnCommand{
				SessionID: created.Session.ID,
				TurnID:    "effective-turn-" + string(rune('a'+turn)),
				Payload:   []byte("agent.voice_effective_turn/v1"),
			},
		); err != nil {
			t.Fatalf("complete effective Turn %d: %v", turn, err)
		}
	}
	completedCommand := practice.AuthorizeSameQuestionRetryCommand{
		RetryRequestID:    "91000000-0000-4000-8000-000000000002",
		PracticeSessionID: created.Session.ID,
		OriginalTurnID:    "turn-daily-completed",
		QuestionID:        "question-daily-completed",
	}
	completed, err := application.AuthorizeSameQuestionRetry(
		ctx,
		actor,
		completedCommand,
	)
	if err != nil {
		t.Fatalf("authorize completed retry: %v", err)
	}
	if completed.CountsTowardEffectiveLimit ||
		completed.SessionStatusAtAuthorization !=
			persistence.ContextSessionCompleted {
		t.Fatalf("completed authorization = %#v", completed)
	}
	participantID, err = application.ResolveAuthorizedParticipant(
		ctx,
		actor,
		completedCommand.RetryRequestID,
	)
	if err != nil ||
		participantID != created.Snapshot.Participants[1].ID {
		t.Fatalf("completed retry participant = %q, %v", participantID, err)
	}
}
