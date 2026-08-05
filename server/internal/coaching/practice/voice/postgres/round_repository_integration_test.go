package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

const (
	testUserA = "10000000-0000-4000-8000-000000000001"
	testUserB = "10000000-0000-4000-8000-000000000002"
)

func TestRepositoryQuestionTipHasOneGenerationLeasePerQuestion(t *testing.T) {
	repository, _ := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-tip", "session-tip")
	command := practicevoice.ClaimQuestionTipCommand{
		SessionID: question.SessionID, QuestionID: question.ID,
		IdempotencyKey: "question-tip-request-1", LeaseDuration: time.Minute,
	}
	claimed, err := repository.ClaimQuestionTip(context.Background(), actor, command)
	if err != nil || !claimed.LeaseAcquired || claimed.Status != practicevoice.QuestionTipProcessing {
		t.Fatalf("first claim = %+v, %v", claimed, err)
	}
	concurrentCommand := command
	concurrentCommand.IdempotencyKey = "question-tip-request-2"
	concurrent, err := repository.ClaimQuestionTip(context.Background(), actor, concurrentCommand)
	if err != nil || concurrent.ID != claimed.ID || concurrent.LeaseAcquired {
		t.Fatalf("concurrent claim = %+v, %v", concurrent, err)
	}
	completed, err := repository.CompleteQuestionTip(
		context.Background(), actor, practicevoice.CompleteQuestionTipCommand{
			TipID: claimed.ID, FencingToken: claimed.FencingToken,
			DeletionGeneration: claimed.DeletionGeneration,
			Content:            "I would clarify the goal first. Then I would explain my approach clearly.",
			Provider:           "fake", Model: "fake-model", ProviderRequestID: "provider-request-1",
		},
	)
	if err != nil || completed.Status != practicevoice.QuestionTipCompleted || completed.Content == "" {
		t.Fatalf("completed Tip = %+v, %v", completed, err)
	}
	replayed, err := repository.ClaimQuestionTip(context.Background(), actor, concurrentCommand)
	if err != nil || replayed.ID != claimed.ID || replayed.Content != completed.Content || replayed.LeaseAcquired {
		t.Fatalf("completed replay = %+v, %v", replayed, err)
	}

	otherQuestion := saveTestQuestion(t, repository, actor, "question-tip-other", "session-tip-other")
	_, err = repository.ClaimQuestionTip(
		context.Background(), actor, practicevoice.ClaimQuestionTipCommand{
			SessionID: otherQuestion.SessionID, QuestionID: otherQuestion.ID,
			IdempotencyKey: command.IdempotencyKey, LeaseDuration: time.Minute,
		},
	)
	if !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("cross-question idempotency error = %v", err)
	}
}

func TestRepositoryRecoversQuestionCandidateAttemptAndTurn(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-recovery", "session-recovery")
	replayedQuestion, err := repository.SaveQuestion(
		context.Background(),
		actor,
		testQuestion(question.ID, question.SessionID),
	)
	if err != nil || !reflect.DeepEqual(replayedQuestion, question) {
		t.Fatalf("question replay = %#v, %v; want %#v", replayedQuestion, err, question)
	}
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-recovery",
	)
	candidate := completeTestTranscription(t, repository, reservation, "transcript-recovery")
	if candidate.RespondentParticipantID != "actual-candidate" {
		t.Fatalf("candidate respondent = %q", candidate.RespondentParticipantID)
	}
	completedReplay, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(question, "reserve-recovery"),
	)
	if err != nil ||
		completedReplay.Status != practicevoice.StoredTranscriptionCompleted ||
		completedReplay.LeaseAcquired ||
		completedReplay.CandidateID != candidate.ID {
		t.Fatalf("completed reservation replay = %#v, %v", completedReplay, err)
	}

	restarted, err := New(pool)
	if err != nil {
		t.Fatalf("new restarted repository: %v", err)
	}
	recoveredQuestion, err := restarted.GetQuestion(
		context.Background(),
		actor,
		question.ID,
	)
	if err != nil || !reflect.DeepEqual(recoveredQuestion, question) {
		t.Fatalf("recovered question = %#v, %v; want %#v", recoveredQuestion, err, question)
	}
	recoveredCandidate, err := restarted.GetCandidate(
		context.Background(),
		actor,
		candidate.ID,
	)
	if err != nil || !reflect.DeepEqual(recoveredCandidate, candidate) {
		t.Fatalf("recovered candidate = %#v, %v; want %#v", recoveredCandidate, err, candidate)
	}
	if recoveredCandidate.ReservationID != reservation.ID {
		t.Fatalf(
			"recovered candidate reservation = %q, want %q",
			recoveredCandidate.ReservationID,
			reservation.ID,
		)
	}
	recoveredReservation, err := restarted.GetReservation(
		context.Background(),
		actor,
		recoveredCandidate.ReservationID,
	)
	if err != nil {
		t.Fatalf("recover candidate reservation: %v", err)
	}
	if recoveredReservation.IdempotencyKey != reservation.IdempotencyKey ||
		recoveredReservation.CandidateID != recoveredCandidate.ID {
		t.Fatalf(
			"recovered candidate correlation = reservation %#v, candidate %#v",
			recoveredReservation,
			recoveredCandidate,
		)
	}
	attempts, err := restarted.ListProcessingAttempts(
		context.Background(),
		actor,
		reservation.ID,
	)
	if err != nil || len(attempts) != 1 || attempts[0].Status != "completed" {
		t.Fatalf("recovered attempts = %#v, %v", attempts, err)
	}

	turn, err := restarted.ConfirmTurn(
		context.Background(),
		actor,
		practicevoice.ConfirmTurnCommand{
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ConfirmedText:   "I led the recovery.",
			IdempotencyKey:  "confirm-recovery",
		},
	)
	if err != nil {
		t.Fatalf("confirm turn: %v", err)
	}
	if turn.RespondentParticipantID != candidate.RespondentParticipantID {
		t.Fatalf("turn respondent = %q, want %q", turn.RespondentParticipantID, candidate.RespondentParticipantID)
	}
	if turn.EffectiveTurns != 1 || turn.SessionCompleted {
		t.Fatalf("atomic turn progress = %#v", turn)
	}
	restartedAgain, err := New(pool)
	if err != nil {
		t.Fatalf("new second restarted repository: %v", err)
	}
	recoveredTurn, err := restartedAgain.GetTurn(
		context.Background(),
		actor,
		turn.ID,
	)
	if err != nil || !reflect.DeepEqual(recoveredTurn, turn) {
		t.Fatalf("recovered turn = %#v, %v; want %#v", recoveredTurn, err, turn)
	}
	if recoveredTurn.CandidateID != recoveredCandidate.ID {
		t.Fatalf(
			"recovered turn candidate = %q, want %q",
			recoveredTurn.CandidateID,
			recoveredCandidate.ID,
		)
	}
}

func TestReservationLeaseTakeoverRejectsOldWorkerCompleteAndFail(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-fence", "session-fence")
	first := reserveTestTranscription(t, repository, actor, question, "reserve-fence")

	replayed, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(question, "reserve-fence"),
	)
	if err != nil ||
		replayed.ID != first.ID ||
		replayed.FencingToken != first.FencingToken ||
		replayed.LeaseAcquired {
		t.Fatalf("live lease replay = %#v, %v; first %#v", replayed, err, first)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE practice_transcription_reservations
		 SET lease_expires_at = clock_timestamp() - interval '1 second'
		 WHERE owner_user_id = $1 AND reservation_id = $2`,
		actor.UserID,
		first.ID,
	); err != nil {
		t.Fatalf("expire reservation: %v", err)
	}
	second, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(question, "reserve-fence"),
	)
	if err != nil {
		t.Fatalf("take over reservation: %v", err)
	}
	if second.ID != first.ID ||
		second.FencingToken != first.FencingToken+1 ||
		second.CurrentAttemptID == first.CurrentAttemptID ||
		!second.LeaseAcquired {
		t.Fatalf("takeover = %#v, first = %#v", second, first)
	}

	oldJob := jobFromReservation(actor.UserID, first)
	_, err = repository.CompleteTranscription(
		context.Background(),
		oldJob,
		completeCommand("old-transcript"),
	)
	if !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("old Complete error = %v, want conflict", err)
	}
	err = repository.FailTranscription(
		context.Background(),
		oldJob,
		practicevoice.ProcessingFailure{Code: "provider_timeout", Retryable: true},
	)
	if !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("old Fail error = %v, want conflict", err)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		jobFromReservation(actor.UserID, second),
		completeCommand("new-transcript"),
	); err != nil {
		t.Fatalf("new worker Complete: %v", err)
	}
	attempts, err := repository.ListProcessingAttempts(
		context.Background(),
		actor,
		first.ID,
	)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 2 ||
		attempts[0].Status != "expired" ||
		attempts[1].Status != "completed" {
		t.Fatalf("attempt lifecycle = %#v", attempts)
	}
}

func TestLeaseUsesDatabaseTimeNotApplicationClock(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-db-clock", "session-db-clock")

	repository.now = func() time.Time {
		return time.Date(2200, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-db-clock",
	)
	if reservation.LeaseExpiresAt.After(time.Now().Add(2 * time.Minute)) {
		t.Fatalf(
			"lease used future application clock: %s",
			reservation.LeaseExpiresAt,
		)
	}
	replayed, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(question, "reserve-db-clock"),
	)
	if err != nil || replayed.LeaseAcquired ||
		replayed.FencingToken != reservation.FencingToken {
		t.Fatalf("future clock caused early takeover: %#v, %v", replayed, err)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		jobFromReservation(actor.UserID, reservation),
		completeCommand("future-clock-active"),
	); err != nil {
		t.Fatalf("future clock rejected active lease: %v", err)
	}

	secondQuestion := testQuestion("question-db-clock-expired", "session-db-clock-expired")
	secondReservationQuestion, err := repository.SaveQuestion(
		context.Background(),
		actor,
		secondQuestion,
	)
	if err != nil {
		t.Fatalf("save expired-lease question: %v", err)
	}
	expired := reserveTestTranscription(
		t,
		repository,
		actor,
		secondReservationQuestion,
		"reserve-db-clock-expired",
	)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE practice_transcription_reservations
		 SET lease_expires_at = transaction_timestamp() - interval '1 second'
		 WHERE owner_user_id = $1 AND reservation_id = $2`,
		actor.UserID,
		expired.ID,
	); err != nil {
		t.Fatalf("expire lease with database clock: %v", err)
	}
	repository.now = func() time.Time {
		return time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		jobFromReservation(actor.UserID, expired),
		completeCommand("past-clock-expired"),
	); !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("past clock accepted expired lease: %v", err)
	}
	takeover, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(secondReservationQuestion, "reserve-db-clock-expired"),
	)
	if err != nil || !takeover.LeaseAcquired ||
		takeover.FencingToken != expired.FencingToken+1 {
		t.Fatalf("past clock blocked database-expired takeover: %#v, %v", takeover, err)
	}
}

func TestCompletedCandidateRequiresStableProviderRequestID(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-provider-request",
		"session-provider-request",
	)
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-provider-request",
	)
	job := jobFromReservation(actor.UserID, reservation)

	for _, providerRequestID := range []string{"", " \t"} {
		command := completeCommand("missing-provider-request")
		command.ProviderRequestID = providerRequestID
		if _, err := repository.CompleteTranscription(
			context.Background(),
			job,
			command,
		); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
			t.Fatalf(
				"provider request ID %q error = %v, want invalid",
				providerRequestID,
				err,
			)
		}
	}

	var candidateCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)
		 FROM practice_transcript_candidates
		 WHERE owner_user_id = $1 AND reservation_id = $2`,
		actor.UserID,
		reservation.ID,
	).Scan(&candidateCount); err != nil {
		t.Fatalf("count candidates after invalid completion: %v", err)
	}
	if candidateCount != 0 {
		t.Fatalf("invalid completion created %d candidates", candidateCount)
	}

	candidate := completeTestTranscription(
		t,
		repository,
		reservation,
		"stable-provider-request",
	)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE practice_transcript_candidates
		 SET provider_request_id = ''
		 WHERE owner_user_id = $1 AND candidate_id = $2`,
		actor.UserID,
		candidate.ID,
	); err == nil {
		t.Fatal("database accepted an empty successful Provider request ID")
	}
}

func TestConcurrentConfirmationCreatesExactlyOneTurn(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-confirm", "session-confirm")
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-confirm",
	)
	candidate := completeTestTranscription(t, repository, reservation, "transcript-confirm")
	command := practicevoice.ConfirmTurnCommand{
		CandidateID:     candidate.ID,
		EvidenceVersion: candidate.EvidenceVersion,
		ConfirmedText:   "My confirmed answer.",
		IdempotencyKey:  "confirm-once",
	}

	const callers = 16
	start := make(chan struct{})
	results := make(chan practice.Turn, callers)
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			turn, err := repository.ConfirmTurn(context.Background(), actor, command)
			results <- turn
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errs)

	var turnID string
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent ConfirmTurn: %v", err)
		}
	}
	for turn := range results {
		if turnID == "" {
			turnID = turn.ID
		}
		if turn.ID != turnID || turn.AnswerText != command.ConfirmedText {
			t.Errorf("concurrent result = %#v, want turn %q", turn, turnID)
		}
	}
	var count int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM practice_turns
		 WHERE owner_user_id = $1 AND candidate_id = $2`,
		actor.UserID,
		candidate.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if count != 1 {
		t.Fatalf("turn count = %d, want 1", count)
	}

	conflicting := command
	conflicting.ConfirmedText = "A changed answer."
	_, err := repository.ConfirmTurn(context.Background(), actor, conflicting)
	if !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("changed payload error = %v, want conflict", err)
	}

}

func TestConfirmTurnWaitsForEvidenceSnapshotSourceFence(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-evidence-fence",
		"session-evidence-fence",
	)
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-evidence-fence",
	)
	candidate := completeTestTranscription(
		t,
		repository,
		reservation,
		"transcript-evidence-fence",
	)
	ctx := context.Background()
	fence, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin EvidenceSnapshot source fence: %v", err)
	}
	defer func() { _ = fence.Rollback(ctx) }()
	if err := lockEvidenceSourceSession(
		ctx,
		fence,
		actor.UserID,
		question.SessionID,
	); err != nil {
		t.Fatalf("lock EvidenceSnapshot source set: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, confirmErr := repository.ConfirmTurn(
			context.Background(),
			actor,
			practicevoice.ConfirmTurnCommand{
				CandidateID:     candidate.ID,
				EvidenceVersion: candidate.EvidenceVersion,
				ConfirmedText:   "I fenced the source write.",
				IdempotencyKey:  "confirm-evidence-fence",
			},
		)
		result <- confirmErr
	}()
	select {
	case confirmErr := <-result:
		t.Fatalf(
			"ConfirmTurn bypassed EvidenceSnapshot source fence: %v",
			confirmErr,
		)
	case <-time.After(100 * time.Millisecond):
	}
	if err := fence.Commit(ctx); err != nil {
		t.Fatalf("commit EvidenceSnapshot source fence: %v", err)
	}
	select {
	case confirmErr := <-result:
		if confirmErr != nil {
			t.Fatalf("ConfirmTurn after source fence commit: %v", confirmErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ConfirmTurn did not resume after source fence commit")
	}
}

func TestConcurrentCandidatesForOneQuestionCreateOneFormalTurn(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-candidate-race",
		"session-candidate-race",
	)
	const candidateCount = 4
	candidates := make([]practicevoice.StoredTranscriptCandidate, 0, candidateCount)
	for index := 1; index <= candidateCount; index++ {
		reservation := reserveTestTranscription(
			t,
			repository,
			actor,
			question,
			fmt.Sprintf("reserve-candidate-race-%d", index),
		)
		candidates = append(
			candidates,
			completeTestTranscription(
				t,
				repository,
				reservation,
				fmt.Sprintf("transcript-candidate-race-%d", index),
			),
		)
	}
	commands := make(
		[]practicevoice.ConfirmTurnCommand,
		0,
		len(candidates),
	)
	for index, candidate := range candidates {
		commands = append(commands, practicevoice.ConfirmTurnCommand{
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ConfirmedText:   candidate.Text,
			IdempotencyKey: fmt.Sprintf(
				"confirm-candidate-race-%d",
				index+1,
			),
		})
	}

	type result struct {
		index int
		turn  practice.Turn
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, len(commands))
	for index, command := range commands {
		index := index
		command := command
		go func() {
			<-start
			turn, err := repository.ConfirmTurn(
				context.Background(),
				actor,
				command,
			)
			results <- result{index: index, turn: turn, err: err}
		}()
	}
	close(start)

	successes := 0
	conflicts := 0
	var winner result
	for range commands {
		outcome := <-results
		switch {
		case outcome.err == nil:
			successes++
			winner = outcome
		case errors.Is(outcome.err, practicevoice.ErrPersistenceConflict):
			conflicts++
		default:
			t.Fatalf("candidate confirmation error = %v", outcome.err)
		}
	}
	if successes != 1 || conflicts != candidateCount-1 {
		t.Fatalf(
			"candidate race successes=%d conflicts=%d, want 1/%d",
			successes,
			conflicts,
			candidateCount-1,
		)
	}
	turns, err := repository.ListSessionTurns(
		context.Background(),
		actor,
		question.SessionID,
	)
	if err != nil {
		t.Fatalf("list candidate-race turns: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != winner.turn.ID {
		t.Fatalf("candidate-race turns = %#v, winner = %#v", turns, winner.turn)
	}
	replayed, err := repository.ConfirmTurn(
		context.Background(),
		actor,
		commands[winner.index],
	)
	if err != nil || !reflect.DeepEqual(replayed, winner.turn) {
		t.Fatalf("winner replay = %#v, %v; want %#v", replayed, err, winner.turn)
	}
	var turnCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)
		 FROM practice_turns
		 WHERE owner_user_id = $1
		   AND practice_session_id = $2
		   AND question_id = $3`,
		actor.UserID,
		question.SessionID,
		question.ID,
	).Scan(&turnCount); err != nil {
		t.Fatalf("count formal turns: %v", err)
	}
	if turnCount != 1 {
		t.Fatalf("formal turn count = %d, want 1", turnCount)
	}
}

func TestListSessionQuestionsIsOwnerScopedAndStable(t *testing.T) {
	repository, _ := newIntegrationRepository(t)
	actorA := testActor(testUserA)
	actorB := testActor(testUserB)
	createdAt := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)

	questions := []practice.Question{
		testQuestion("question-third", "session-list-questions"),
		testQuestion("question-first", "session-list-questions"),
		testQuestion("question-second", "session-list-questions"),
	}
	questions[0].Sequence = 3
	questions[0].CreatedAt = createdAt.Add(-time.Hour)
	questions[1].Sequence = 1
	questions[1].CreatedAt = createdAt.Add(time.Hour)
	questions[2].Sequence = 2
	questions[2].CreatedAt = createdAt
	for _, question := range questions {
		if _, err := repository.SaveQuestion(
			context.Background(),
			actorA,
			question,
		); err != nil {
			t.Fatalf("save %s: %v", question.ID, err)
		}
	}

	otherSession := testQuestion("question-other-session", "session-other")
	if _, err := repository.SaveQuestion(
		context.Background(),
		actorA,
		otherSession,
	); err != nil {
		t.Fatalf("save other-session question: %v", err)
	}
	otherOwner := testQuestion("question-other-owner", "session-list-questions")
	if _, err := repository.SaveQuestion(
		context.Background(),
		actorB,
		otherOwner,
	); err != nil {
		t.Fatalf("save other-owner question: %v", err)
	}

	got, err := repository.ListSessionQuestions(
		context.Background(),
		actorA,
		"session-list-questions",
	)
	if err != nil {
		t.Fatalf("list session questions: %v", err)
	}
	gotIDs := make([]string, 0, len(got))
	for _, question := range got {
		gotIDs = append(gotIDs, question.ID)
	}
	wantIDs := []string{"question-first", "question-second", "question-third"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("question IDs = %#v, want %#v", gotIDs, wantIDs)
	}

	otherOwnerQuestions, err := repository.ListSessionQuestions(
		context.Background(),
		actorB,
		"session-list-questions",
	)
	if err != nil {
		t.Fatalf("list other-owner questions: %v", err)
	}
	if len(otherOwnerQuestions) != 1 ||
		otherOwnerQuestions[0].ID != otherOwner.ID {
		t.Fatalf("other-owner questions = %#v", otherOwnerQuestions)
	}
	empty, err := repository.ListSessionQuestions(
		context.Background(),
		actorA,
		"session-without-questions",
	)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty session questions = %#v, %v", empty, err)
	}
}

func TestInvalidOwnerAndDuplicateAddresseesAreRejected(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	invalidActor := practicevoice.Actor{
		UserID:    "not-a-uuid",
		SessionID: "trusted-session",
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		invalidActor,
		"question",
	); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
		t.Fatalf("invalid Actor owner error = %v", err)
	}
	if _, err := repository.ListSessionQuestions(
		context.Background(),
		invalidActor,
		"session",
	); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
		t.Fatalf("invalid Actor question list error = %v", err)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		practicevoice.JobContext{
			OwnerUserID:        "not-a-uuid",
			DeletionGeneration: 0,
			ReservationID:      "reservation",
			FencingToken:       1,
		},
		completeCommand("invalid-owner"),
	); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
		t.Fatalf("invalid Job owner error = %v", err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		practice.DeletionContext{
			UserID:     "not-a-uuid",
			Generation: 1,
		},
	); !errors.Is(err, practice.ErrInvalidArgument) {
		t.Fatalf("invalid Deletion owner error = %v", err)
	}
	question := testQuestion("duplicate-addressee", "session-duplicate-addressee")
	question.AddresseeParticipantIDs = []string{"candidate", "candidate"}
	if _, err := repository.SaveQuestion(
		context.Background(),
		testActor(testUserA),
		question,
	); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
		t.Fatalf("duplicate addressee error = %v", err)
	}

	actor := testActor(testUserA)
	validQuestion := saveTestQuestion(
		t,
		repository,
		actor,
		"unrelated-respondent",
		"session-unrelated-respondent",
	)
	command := reserveCommand(validQuestion, "reserve-unrelated-respondent")
	command.RespondentParticipantID = "unrelated-participant"
	if _, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		command,
	); !errors.Is(err, practicevoice.ErrPersistenceNotFound) {
		t.Fatalf("unrelated respondent error = %v, want not found", err)
	}
	var reservationCount int
	var attemptCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT count(*) FROM practice_transcription_reservations
			 WHERE owner_user_id = $1 AND question_id = $2),
			(SELECT count(*) FROM practice_processing_attempts
			 WHERE owner_user_id = $1)`,
		actor.UserID,
		validQuestion.ID,
	).Scan(&reservationCount, &attemptCount); err != nil {
		t.Fatalf("count rejected respondent state: %v", err)
	}
	if reservationCount != 0 || attemptCount != 0 {
		t.Fatalf(
			"rejected respondent created reservations=%d attempts=%d",
			reservationCount,
			attemptCount,
		)
	}
}

func TestFailedAttemptCreatesNoTurnAndStoresOnlyNormalizedAudit(t *testing.T) {
	repository, _ := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(t, repository, actor, "question-failure", "session-failure")
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-failure",
	)
	err := repository.FailTranscription(
		context.Background(),
		jobFromReservation(actor.UserID, reservation),
		practicevoice.ProcessingFailure{
			Code:              "provider_timeout",
			Retryable:         true,
			ProviderRequestID: "safe-request-id",
			Duration:          125 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("fail transcription: %v", err)
	}
	turns, err := repository.ListSessionTurns(
		context.Background(),
		actor,
		question.SessionID,
	)
	if err != nil || len(turns) != 0 {
		t.Fatalf("turns after ASR failure = %#v, %v", turns, err)
	}
	attempts, err := repository.ListProcessingAttempts(
		context.Background(),
		actor,
		reservation.ID,
	)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
	attempt := attempts[0]
	if attempt.ErrorCode != "provider_timeout" ||
		attempt.ProviderRequestID != "safe-request-id" ||
		attempt.Duration != 125*time.Millisecond {
		t.Fatalf("normalized attempt = %#v", attempt)
	}
}

func TestFailureCodeBoundaryRejectsUnnormalizedValues(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-failure-code",
		"session-failure-code",
	)
	reservation := reserveTestTranscription(
		t,
		repository,
		actor,
		question,
		"reserve-failure-code",
	)
	job := jobFromReservation(actor.UserID, reservation)

	for _, code := range []string{
		"",
		"provider timeout",
		"Provider_Timeout",
		"provider_timeout\ncredential",
		"sk-secret-value",
		strings.Repeat("a", 65),
	} {
		if err := repository.FailTranscription(
			context.Background(),
			job,
			practicevoice.ProcessingFailure{Code: code, Retryable: true},
		); !errors.Is(err, practicevoice.ErrPersistenceInvalid) {
			t.Fatalf("failure code %q error = %v, want invalid", code, err)
		}
	}

	if _, err := pool.Exec(
		context.Background(),
		`UPDATE practice_processing_attempts
		 SET status = 'failed', error_code = 'raw provider response'
		 WHERE owner_user_id = $1 AND attempt_id = $2`,
		actor.UserID,
		reservation.CurrentAttemptID,
	); err == nil {
		t.Fatal("database accepted an unnormalized failure code")
	}

	if err := repository.FailTranscription(
		context.Background(),
		job,
		practicevoice.ProcessingFailure{
			Code:      "provider_timeout",
			Retryable: true,
		},
	); err != nil {
		t.Fatalf("normalized failure code rejected: %v", err)
	}
}

func TestActorIsolationDeletionAndGenerationFence(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actorA := testActor(testUserA)
	actorB := testActor(testUserB)
	questionA := testQuestion("shared-question", "shared-session")
	questionA.Content = "User A question"
	questionB := questionA
	questionB.Content = "User B question"
	if _, err := repository.SaveQuestion(context.Background(), actorA, questionA); err != nil {
		t.Fatalf("save A question: %v", err)
	}
	if _, err := repository.SaveQuestion(context.Background(), actorB, questionB); err != nil {
		t.Fatalf("save B question: %v", err)
	}
	gotA, err := repository.GetQuestion(context.Background(), actorA, questionA.ID)
	if err != nil || gotA.Content != questionA.Content {
		t.Fatalf("A question = %#v, %v", gotA, err)
	}
	gotB, err := repository.GetQuestion(context.Background(), actorB, questionB.ID)
	if err != nil || gotB.Content != questionB.Content {
		t.Fatalf("B question = %#v, %v", gotB, err)
	}

	reservationA := reserveTestTranscription(
		t,
		repository,
		actorA,
		questionA,
		"shared-reservation-key",
	)
	candidateA := completeTestTranscription(t, repository, reservationA, "transcript-a")
	if _, err := repository.GetCandidate(
		context.Background(),
		actorB,
		candidateA.ID,
	); !errors.Is(err, practicevoice.ErrPersistenceNotFound) {
		t.Fatalf("B read A candidate error = %v, want not found", err)
	}
	if _, err := repository.ConfirmTurn(
		context.Background(),
		actorB,
		practicevoice.ConfirmTurnCommand{
			CandidateID:     candidateA.ID,
			EvidenceVersion: candidateA.EvidenceVersion,
			ConfirmedText:   candidateA.Text,
			IdempotencyKey:  "foreign-confirm",
		},
	); !errors.Is(err, practicevoice.ErrPersistenceNotFound) {
		t.Fatalf("B confirm A candidate error = %v, want not found", err)
	}

	reservationB := reserveTestTranscription(
		t,
		repository,
		actorB,
		questionB,
		"shared-reservation-key",
	)
	oldJobB := jobFromReservation(actorB.UserID, reservationB)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE identity_users SET account_status = 'deleting' WHERE id = $1`,
		actorB.UserID,
	); err != nil {
		t.Fatalf("start B deletion: %v", err)
	}
	staleReadErrors := []error{}
	_, err = repository.GetQuestion(context.Background(), actorB, questionB.ID)
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.GetReservation(context.Background(), actorB, reservationB.ID)
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.GetCandidate(context.Background(), actorB, candidateA.ID)
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.GetTurn(context.Background(), actorB, "guessed-turn")
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.ListProcessingAttempts(
		context.Background(),
		actorB,
		reservationB.ID,
	)
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.ListSessionTurns(
		context.Background(),
		actorB,
		questionB.SessionID,
	)
	staleReadErrors = append(staleReadErrors, err)
	_, err = repository.ListSessionQuestions(
		context.Background(),
		actorB,
		questionB.SessionID,
	)
	staleReadErrors = append(staleReadErrors, err)
	for index, staleReadErr := range staleReadErrors {
		if !errors.Is(staleReadErr, practicevoice.ErrActorDeleted) {
			t.Fatalf(
				"stale read %d error = %v, want actor deleted",
				index,
				staleReadErr,
			)
		}
	}
	deletion := practice.DeletionContext{
		UserID:     actorB.UserID,
		Generation: 1,
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("delete B: %v", err)
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("repeat delete B: %v", err)
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		actorB,
		questionB.ID,
	); !errors.Is(err, practicevoice.ErrActorDeleted) {
		t.Fatalf("B question after deletion error = %v", err)
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		actorA,
		questionA.ID,
	); err != nil {
		t.Fatalf("B deletion affected A: %v", err)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		oldJobB,
		completeCommand("late-b"),
	); !errors.Is(err, practicevoice.ErrPersistenceConflict) {
		t.Fatalf("old B worker error = %v, want conflict", err)
	}
	if _, err := repository.SaveQuestion(
		context.Background(),
		actorB,
		testQuestion("resurrected", "shared-session"),
	); !errors.Is(err, practicevoice.ErrActorDeleted) {
		t.Fatalf("B resurrection error = %v, want actor deleted", err)
	}
}

func TestAccountDeletionSerializesWithVoiceWrites(t *testing.T) {
	t.Run("business write commits before deletion cleanup", func(t *testing.T) {
		repository, pool := newIntegrationRepository(t)
		actor := testActor(testUserA)
		question := saveTestQuestion(
			t,
			repository,
			actor,
			"question-write-first",
			"session-write-first",
		)
		reservation := reserveTestTranscription(
			t,
			repository,
			actor,
			question,
			"reserve-write-first",
		)

		fenceReached := make(chan struct{})
		releaseWrite := make(chan struct{})
		repository.afterWriteFence = func() {
			close(fenceReached)
			<-releaseWrite
		}
		writeResult := make(chan error, 1)
		go func() {
			writeResult <- repository.FailTranscription(
				context.Background(),
				jobFromReservation(actor.UserID, reservation),
				practicevoice.ProcessingFailure{
					Code:      "provider_timeout",
					Retryable: true,
				},
			)
		}()
		<-fenceReached

		deletionStarted := make(chan error, 1)
		go func() {
			_, err := pool.Exec(
				context.Background(),
				`UPDATE identity_users
				 SET account_status = 'deleting'
				 WHERE id = $1`,
				actor.UserID,
			)
			deletionStarted <- err
		}()
		select {
		case err := <-deletionStarted:
			t.Fatalf("deletion crossed active business write fence: %v", err)
		case <-time.After(100 * time.Millisecond):
		}

		close(releaseWrite)
		if err := <-writeResult; err != nil {
			t.Fatalf("business write before deletion: %v", err)
		}
		if err := <-deletionStarted; err != nil {
			t.Fatalf("start deletion after business commit: %v", err)
		}
		repository.afterWriteFence = nil
		if err := repository.DeleteUserData(
			context.Background(),
			practice.DeletionContext{
				UserID:     actor.UserID,
				Generation: 1,
			},
		); err != nil {
			t.Fatalf("delete committed business data: %v", err)
		}
		var reservationCount int
		if err := pool.QueryRow(
			context.Background(),
			`SELECT count(*)
			 FROM practice_transcription_reservations
			 WHERE owner_user_id = $1 AND reservation_id = $2`,
			actor.UserID,
			reservation.ID,
		).Scan(&reservationCount); err != nil {
			t.Fatalf("count reservation after cleanup: %v", err)
		}
		if reservationCount != 0 {
			t.Fatalf("reservation count after cleanup = %d, want 0", reservationCount)
		}
	})

	t.Run("deletion commits before old worker", func(t *testing.T) {
		repository, pool := newIntegrationRepository(t)
		actor := testActor(testUserA)
		question := saveTestQuestion(
			t,
			repository,
			actor,
			"question-delete-first",
			"session-delete-first",
		)
		reservation := reserveTestTranscription(
			t,
			repository,
			actor,
			question,
			"reserve-delete-first",
		)

		deletionTx, err := pool.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin deletion transaction: %v", err)
		}
		defer func() { _ = deletionTx.Rollback(context.Background()) }()
		if _, err := deletionTx.Exec(
			context.Background(),
			`UPDATE identity_users
			 SET account_status = 'deleting'
			 WHERE id = $1`,
			actor.UserID,
		); err != nil {
			t.Fatalf("lock deleting account: %v", err)
		}

		workerResult := make(chan error, 1)
		go func() {
			workerResult <- repository.FailTranscription(
				context.Background(),
				jobFromReservation(actor.UserID, reservation),
				practicevoice.ProcessingFailure{
					Code:      "provider_timeout",
					Retryable: true,
				},
			)
		}()
		select {
		case err := <-workerResult:
			t.Fatalf("old worker crossed uncommitted deletion fence: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := deletionTx.Commit(context.Background()); err != nil {
			t.Fatalf("commit deletion state: %v", err)
		}
		if err := <-workerResult; !errors.Is(err, practicevoice.ErrPersistenceConflict) {
			t.Fatalf("old worker after deletion error = %v, want conflict", err)
		}
	})
}

func TestMigrationUsesIdentityUUIDOwnershipAndPersistsNoAudio(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	rows, err := pool.Query(
		context.Background(),
		`SELECT table_name, column_name, data_type
		 FROM information_schema.columns
		 WHERE table_schema = current_schema()
		   AND table_name IN (
		       'practice_deletion_fences',
		       'practice_questions',
		       'practice_transcription_reservations',
		       'practice_processing_attempts',
		       'practice_transcript_candidates',
		       'practice_turns',
		       'practice_turn_confirmations',
		       'practice_retry_turn_drafts'
		   )
		 ORDER BY table_name, ordinal_position`,
	)
	if err != nil {
		t.Fatalf("read Voice columns: %v", err)
	}
	defer rows.Close()
	ownerColumns := 0
	for rows.Next() {
		var table string
		var column string
		var dataType string
		if err := rows.Scan(&table, &column, &dataType); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if column == "owner_user_id" {
			ownerColumns++
			if dataType != "uuid" {
				t.Errorf("%s.owner_user_id type = %q, want uuid", table, dataType)
			}
		}
		if strings.Contains(column, "audio") {
			t.Errorf("durable pre-Turn audio column exists: %s.%s", table, column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	if ownerColumns != 8 {
		t.Fatalf("owner UUID column count = %d, want 8", ownerColumns)
	}
	actor := testActor(testUserA)
	question := saveTestQuestion(
		t,
		repository,
		actor,
		"question-restrict",
		"session-restrict",
	)
	if _, err := pool.Exec(
		context.Background(),
		`DELETE FROM identity_users WHERE id = $1`,
		actor.UserID,
	); err == nil {
		t.Fatal("Identity deletion bypassed Voice repository cleanup")
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		actor,
		question.ID,
	); err != nil {
		t.Fatalf("restricted Identity delete removed Question: %v", err)
	}
}

func TestDeletionFenceSurvivesPhysicalIdentityRemoval(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	actor := testActor(testUserA)
	saveTestQuestion(
		t,
		repository,
		actor,
		"question-durable-fence",
		"session-durable-fence",
	)
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE identity_users SET account_status = 'deleting' WHERE id = $1`,
		actor.UserID,
	); err != nil {
		t.Fatalf("start account deletion: %v", err)
	}
	deletion := practice.DeletionContext{
		UserID:     actor.UserID,
		Generation: 2,
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("delete Voice data: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`DELETE FROM identity_users WHERE id = $1`,
		actor.UserID,
	); err != nil {
		t.Fatalf("physically delete Identity user: %v", err)
	}

	var generation int64
	if err := pool.QueryRow(
		context.Background(),
		`SELECT deletion_generation
		 FROM practice_deletion_fences
		 WHERE owner_user_id = $1`,
		actor.UserID,
	).Scan(&generation); err != nil {
		t.Fatalf("restore durable deletion fence: %v", err)
	}
	if generation != 2 {
		t.Fatalf("deletion generation = %d, want 2", generation)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		practice.DeletionContext{
			UserID:     actor.UserID,
			Generation: 1,
		},
	); !errors.Is(err, practice.ErrDeletionGeneration) {
		t.Fatalf("stale deletion error = %v, want conflict", err)
	}
	if err := repository.DeleteUserData(context.Background(), deletion); err != nil {
		t.Fatalf("repeat final deletion: %v", err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		practice.DeletionContext{
			UserID:     actor.UserID,
			Generation: 3,
		},
	); err != nil {
		t.Fatalf("advance deletion generation: %v", err)
	}
	if err := pool.QueryRow(
		context.Background(),
		`SELECT deletion_generation
		 FROM practice_deletion_fences
		 WHERE owner_user_id = $1`,
		actor.UserID,
	).Scan(&generation); err != nil {
		t.Fatalf("restore advanced deletion fence: %v", err)
	}
	if generation != 3 {
		t.Fatalf("advanced deletion generation = %d, want 3", generation)
	}
}

func newIntegrationRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	admin, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("open integration admin pool: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("conversation_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})

	testConfig := config.Copy()
	if testConfig.ConnConfig.RuntimeParams == nil {
		testConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), testConfig)
	if err != nil {
		t.Fatalf("open integration pool: %v", err)
	}
	t.Cleanup(pool.Close)

	identityFixture := `CREATE TABLE identity_users (
		id uuid PRIMARY KEY,
		account_status text NOT NULL CHECK (
			account_status IN ('active', 'deleting', 'deleted')
		)
	)`
	if _, err := pool.Exec(context.Background(), identityFixture); err != nil {
		t.Fatalf("create identity dependency fixture: %v", err)
	}
	for _, migrationName := range []string{
		"000009_conversation_persistence.up.sql",
		"000010_conversation_persistence_hardening.up.sql",
	} {
		migrationSQL, err := migrations.Files.ReadFile(migrationName)
		if err != nil {
			t.Fatalf("read Conversation migration %s: %v", migrationName, err)
		}
		if _, err := pool.Exec(
			context.Background(),
			string(migrationSQL),
		); err != nil {
			t.Fatalf("apply Conversation migration %s: %v", migrationName, err)
		}
	}
	if _, err := pool.Exec(
		context.Background(),
		conversationRetryIntegrationSchema,
	); err != nil {
		t.Fatalf("apply Conversation retry integration schema: %v", err)
	}
	tipMigration, err := migrations.Files.ReadFile(
		"000070_practice_question_tips.up.sql",
	)
	if err != nil {
		t.Fatalf("read Practice Tip migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(tipMigration)); err != nil {
		t.Fatalf("apply Practice Tip migration: %v", err)
	}
	for _, userID := range []string{testUserA, testUserB} {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO identity_users (id, account_status) VALUES ($1, 'active')`,
			userID,
		); err != nil {
			t.Fatalf("insert identity user %s: %v", userID, err)
		}
	}
	repository, err := New(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	return repository, pool
}

func testActor(userID string) practicevoice.Actor {
	return practicevoice.Actor{UserID: userID, SessionID: "trusted-session"}
}

func testQuestion(id string, sessionID string) practice.Question {
	return practice.Question{
		ID:                      id,
		SessionID:               sessionID,
		SpeakerParticipantID:    "interviewer",
		AddresseeParticipantIDs: []string{"actual-candidate"},
		ObjectiveID:             "objective-1",
		Type:                    "PRIMARY",
		Content:                 "Tell me about a difficult recovery.",
		Sequence:                1,
	}
}

func saveTestQuestion(
	t *testing.T,
	repository *Repository,
	actor practicevoice.Actor,
	id string,
	sessionID string,
) practice.Question {
	t.Helper()
	ensureTestPracticeSession(t, repository.pool, actor.UserID, sessionID)
	question, err := repository.SaveQuestion(
		context.Background(),
		actor,
		testQuestion(id, sessionID),
	)
	if err != nil {
		t.Fatalf("save question: %v", err)
	}
	return question
}

func ensureTestPracticeSession(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
	sessionID string,
) {
	t.Helper()
	snapshotID := sessionID + "-snapshot"
	snapshot := fmt.Sprintf(`{
		"snapshot_id":%q,
		"practice_session_id":%q,
		"scene_selection":{
			"scene":{
				"turn_policy_ref":"generic.practice.turn.v1",
				"session_policy_ref":"generic.practice.session.v1",
				"prompt":{"turn_blueprints":["Open the practice"]},
				"practice_options":[{
					"practice_option_id":"option-full",
					"practice_option_type":"FULL_SIMULATION"
				}]
			},
			"practice_option_id":"option-full"
		},
		"session_policy":{
			"suggested_duration_seconds":600,
			"min_effective_turns":4,
			"max_effective_turns":6,
			"coverage_checkpoint_turn":4,
			"max_follow_ups_per_question":1,
			"early_completion_rule":"COVERAGE_SATISFIED_AFTER_CHECKPOINT",
			"retry_allowed":false,
			"question_translation_allowed":false
		}
	}`, snapshotID, sessionID)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, status, version,
			effective_turns, snapshot_id
		)
		VALUES ($1, $2, 'starting', 1, 0, $3)
		ON CONFLICT (owner_user_id, session_id) DO NOTHING
	`, ownerUserID, sessionID, snapshotID); err != nil {
		t.Fatalf("ensure Practice Session fixture: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, snapshot_id,
			turn_limit, snapshot_document
		)
		VALUES ($1, $2, $3, 6, $4::jsonb)
		ON CONFLICT (owner_user_id, session_id) DO NOTHING
	`, ownerUserID, sessionID, snapshotID, snapshot); err != nil {
		t.Fatalf("ensure Practice Session snapshot fixture: %v", err)
	}
}

func reserveCommand(
	question practice.Question,
	key string,
) practicevoice.StoreReserveTranscriptionCommand {
	return practicevoice.StoreReserveTranscriptionCommand{
		QuestionID:              question.ID,
		SessionID:               question.SessionID,
		IdempotencyKey:          key,
		InputFingerprint:        "sha256:audio-fixture",
		RespondentParticipantID: "actual-candidate",
		LeaseDuration:           time.Minute,
	}
}

func reserveTestTranscription(
	t *testing.T,
	repository *Repository,
	actor practicevoice.Actor,
	question practice.Question,
	key string,
) practicevoice.StoredTranscriptionReservation {
	t.Helper()
	reservation, err := repository.ReserveTranscription(
		context.Background(),
		actor,
		reserveCommand(question, key),
	)
	if err != nil {
		t.Fatalf("reserve transcription: %v", err)
	}
	if !reservation.LeaseAcquired {
		t.Fatal("new transcription reservation did not acquire its lease")
	}
	return reservation
}

func completeCommand(transcriptID string) practicevoice.StoreCompleteTranscriptionCommand {
	return practicevoice.StoreCompleteTranscriptionCommand{
		TranscriptID:      transcriptID,
		EvidenceVersion:   1,
		Provider:          "qianwen",
		Model:             "asr-test",
		ProviderRequestID: "provider-request-" + transcriptID,
		Text:              "Candidate transcript " + transcriptID,
	}
}

func completeTestTranscription(
	t *testing.T,
	repository *Repository,
	reservation practicevoice.StoredTranscriptionReservation,
	transcriptID string,
) practicevoice.StoredTranscriptCandidate {
	t.Helper()
	candidate, err := repository.CompleteTranscription(
		context.Background(),
		jobFromReservation(testUserA, reservation),
		completeCommand(transcriptID),
	)
	if err != nil {
		t.Fatalf("complete transcription: %v", err)
	}
	return candidate
}

func jobFromReservation(
	ownerUserID string,
	reservation practicevoice.StoredTranscriptionReservation,
) practicevoice.JobContext {
	return practicevoice.JobContext{
		OwnerUserID:        ownerUserID,
		DeletionGeneration: reservation.DeletionGeneration,
		ReservationID:      reservation.ID,
		FencingToken:       reservation.FencingToken,
	}
}
