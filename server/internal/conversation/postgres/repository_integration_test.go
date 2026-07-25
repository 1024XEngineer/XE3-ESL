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

	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

const (
	testUserA = "10000000-0000-4000-8000-000000000001"
	testUserB = "10000000-0000-4000-8000-000000000002"
)

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
		completedReplay.Status != conversation.TranscriptionCompleted ||
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
		conversation.ConfirmTurnCommand{
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
	turn, err = restarted.SaveTurnProgress(
		context.Background(),
		actor,
		turn.ID,
		conversation.TurnProgress{EffectiveTurns: 3, SessionCompleted: true},
	)
	if err != nil {
		t.Fatalf("save turn progress: %v", err)
	}
	turn, err = restarted.SaveTurnReview(
		context.Background(),
		actor,
		turn.ID,
		conversation.TurnReviewCheckpoint{
			ReviewID:     "review-recovery",
			SourceTurnID: turn.ID,
		},
	)
	if err != nil {
		t.Fatalf("save turn review: %v", err)
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
		`UPDATE conversation_transcription_reservations
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
	if !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("old Complete error = %v, want conflict", err)
	}
	err = repository.FailTranscription(
		context.Background(),
		oldJob,
		conversation.ProcessingFailure{Code: "provider_timeout", Retryable: true},
	)
	if !errors.Is(err, conversation.ErrPersistenceConflict) {
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
		`UPDATE conversation_transcription_reservations
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
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
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
	command := conversation.ConfirmTurnCommand{
		CandidateID:     candidate.ID,
		EvidenceVersion: candidate.EvidenceVersion,
		ConfirmedText:   "My confirmed answer.",
		IdempotencyKey:  "confirm-once",
	}

	const callers = 16
	start := make(chan struct{})
	results := make(chan conversation.ConfirmedTurn, callers)
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
		`SELECT count(*) FROM conversation_confirmed_turns
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
	if !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("changed payload error = %v, want conflict", err)
	}

	checkpoint := conversation.TurnReviewCheckpoint{
		ReviewID:     "review-confirm",
		SourceTurnID: turnID,
	}
	if _, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turnID,
		conversation.TurnReviewCheckpoint{
			ReviewID:     "wrong-source-review",
			SourceTurnID: "another-turn",
		},
	); !errors.Is(err, conversation.ErrPersistenceInvalid) {
		t.Fatalf("wrong review source error = %v, want invalid", err)
	}
	if _, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turnID,
		checkpoint,
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("review before session completion error = %v, want conflict", err)
	}
	completed, err := repository.SaveTurnProgress(
		context.Background(),
		actor,
		turnID,
		conversation.TurnProgress{
			EffectiveTurns:   3,
			SessionCompleted: true,
		},
	)
	if err != nil || !completed.Progress.SessionCompleted {
		t.Fatalf("complete progress = %#v, %v", completed, err)
	}
	replayedProgress, err := repository.SaveTurnProgress(
		context.Background(),
		actor,
		turnID,
		completed.Progress,
	)
	if err != nil || !reflect.DeepEqual(replayedProgress, completed) {
		t.Fatalf("replay progress = %#v, %v; want %#v", replayedProgress, err, completed)
	}
	_, err = repository.SaveTurnProgress(
		context.Background(),
		actor,
		turnID,
		conversation.TurnProgress{EffectiveTurns: 2},
	)
	if !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("changed first progress error = %v, want conflict", err)
	}
	withReview, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turnID,
		checkpoint,
	)
	if err != nil || withReview.Review != checkpoint {
		t.Fatalf("save review checkpoint = %#v, %v", withReview, err)
	}
	replayedReview, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turnID,
		checkpoint,
	)
	if err != nil || !reflect.DeepEqual(replayedReview, withReview) {
		t.Fatalf("replay review = %#v, %v; want %#v", replayedReview, err, withReview)
	}
	conflictingCheckpoint := checkpoint
	conflictingCheckpoint.ReviewID = "different-review"
	if _, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turnID,
		conflictingCheckpoint,
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("changed review error = %v, want conflict", err)
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
	candidates := make([]conversation.TranscriptCandidate, 0, candidateCount)
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
		[]conversation.ConfirmTurnCommand,
		0,
		len(candidates),
	)
	for index, candidate := range candidates {
		commands = append(commands, conversation.ConfirmTurnCommand{
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
		turn  conversation.ConfirmedTurn
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
		case errors.Is(outcome.err, conversation.ErrPersistenceConflict):
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
		 FROM conversation_confirmed_turns
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

func TestInvalidOwnerAndDuplicateAddresseesAreRejected(t *testing.T) {
	repository, pool := newIntegrationRepository(t)
	invalidActor := conversation.Actor{
		UserID:    "not-a-uuid",
		SessionID: "trusted-session",
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		invalidActor,
		"question",
	); !errors.Is(err, conversation.ErrPersistenceInvalid) {
		t.Fatalf("invalid Actor owner error = %v", err)
	}
	if _, err := repository.CompleteTranscription(
		context.Background(),
		conversation.JobContext{
			OwnerUserID:        "not-a-uuid",
			DeletionGeneration: 0,
			ReservationID:      "reservation",
			FencingToken:       1,
		},
		completeCommand("invalid-owner"),
	); !errors.Is(err, conversation.ErrPersistenceInvalid) {
		t.Fatalf("invalid Job owner error = %v", err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		conversation.DeletionContext{
			OwnerUserID:        "not-a-uuid",
			DeletionGeneration: 1,
		},
	); !errors.Is(err, conversation.ErrPersistenceInvalid) {
		t.Fatalf("invalid Deletion owner error = %v", err)
	}
	question := testQuestion("duplicate-addressee", "session-duplicate-addressee")
	question.AddresseeParticipantIDs = []string{"candidate", "candidate"}
	if _, err := repository.SaveQuestion(
		context.Background(),
		testActor(testUserA),
		question,
	); !errors.Is(err, conversation.ErrPersistenceInvalid) {
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
	); !errors.Is(err, conversation.ErrPersistenceNotFound) {
		t.Fatalf("unrelated respondent error = %v, want not found", err)
	}
	var reservationCount int
	var attemptCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
			(SELECT count(*) FROM conversation_transcription_reservations
			 WHERE owner_user_id = $1 AND question_id = $2),
			(SELECT count(*) FROM conversation_processing_attempts
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
		conversation.ProcessingFailure{
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

func TestReviewCheckpointCannotBindTwoTurns(t *testing.T) {
	repository, _ := newIntegrationRepository(t)
	actor := testActor(testUserA)
	turns := make([]conversation.ConfirmedTurn, 0, 2)
	for sequence := 1; sequence <= 2; sequence++ {
		questionInput := testQuestion(
			fmt.Sprintf("question-review-%d", sequence),
			"session-review-unique",
		)
		questionInput.Sequence = sequence
		question, err := repository.SaveQuestion(
			context.Background(),
			actor,
			questionInput,
		)
		if err != nil {
			t.Fatalf("save question %d: %v", sequence, err)
		}
		reservation := reserveTestTranscription(
			t,
			repository,
			actor,
			question,
			fmt.Sprintf("reserve-review-%d", sequence),
		)
		candidate := completeTestTranscription(
			t,
			repository,
			reservation,
			fmt.Sprintf("transcript-review-%d", sequence),
		)
		turn, err := repository.ConfirmTurn(
			context.Background(),
			actor,
			conversation.ConfirmTurnCommand{
				CandidateID:     candidate.ID,
				EvidenceVersion: candidate.EvidenceVersion,
				ConfirmedText:   candidate.Text,
				IdempotencyKey:  fmt.Sprintf("confirm-review-%d", sequence),
			},
		)
		if err != nil {
			t.Fatalf("confirm turn %d: %v", sequence, err)
		}
		turn, err = repository.SaveTurnProgress(
			context.Background(),
			actor,
			turn.ID,
			conversation.TurnProgress{
				EffectiveTurns:   sequence,
				SessionCompleted: true,
			},
		)
		if err != nil {
			t.Fatalf("save progress %d: %v", sequence, err)
		}
		turns = append(turns, turn)
	}
	if _, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turns[0].ID,
		conversation.TurnReviewCheckpoint{
			ReviewID:     "one-review",
			SourceTurnID: turns[0].ID,
		},
	); err != nil {
		t.Fatalf("save first review checkpoint: %v", err)
	}
	if _, err := repository.SaveTurnReview(
		context.Background(),
		actor,
		turns[1].ID,
		conversation.TurnReviewCheckpoint{
			ReviewID:     "one-review",
			SourceTurnID: turns[1].ID,
		},
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("duplicate review checkpoint error = %v, want conflict", err)
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
	); !errors.Is(err, conversation.ErrPersistenceNotFound) {
		t.Fatalf("B read A candidate error = %v, want not found", err)
	}
	if _, err := repository.ConfirmTurn(
		context.Background(),
		actorB,
		conversation.ConfirmTurnCommand{
			CandidateID:     candidateA.ID,
			EvidenceVersion: candidateA.EvidenceVersion,
			ConfirmedText:   candidateA.Text,
			IdempotencyKey:  "foreign-confirm",
		},
	); !errors.Is(err, conversation.ErrPersistenceNotFound) {
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
	for index, staleReadErr := range staleReadErrors {
		if !errors.Is(staleReadErr, conversation.ErrActorDeleted) {
			t.Fatalf(
				"stale read %d error = %v, want actor deleted",
				index,
				staleReadErr,
			)
		}
	}
	deletion := conversation.DeletionContext{
		OwnerUserID:        actorB.UserID,
		DeletionGeneration: 1,
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
	); !errors.Is(err, conversation.ErrActorDeleted) {
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
	); !errors.Is(err, conversation.ErrPersistenceConflict) {
		t.Fatalf("old B worker error = %v, want conflict", err)
	}
	if _, err := repository.SaveQuestion(
		context.Background(),
		actorB,
		testQuestion("resurrected", "shared-session"),
	); !errors.Is(err, conversation.ErrActorDeleted) {
		t.Fatalf("B resurrection error = %v, want actor deleted", err)
	}
}

func TestAccountDeletionSerializesWithConversationWrites(t *testing.T) {
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
				conversation.ProcessingFailure{
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
			conversation.DeletionContext{
				OwnerUserID:        actor.UserID,
				DeletionGeneration: 1,
			},
		); err != nil {
			t.Fatalf("delete committed business data: %v", err)
		}
		var reservationCount int
		if err := pool.QueryRow(
			context.Background(),
			`SELECT count(*)
			 FROM conversation_transcription_reservations
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
				conversation.ProcessingFailure{
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
		if err := <-workerResult; !errors.Is(err, conversation.ErrPersistenceConflict) {
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
		   AND table_name LIKE 'conversation_%'
		 ORDER BY table_name, ordinal_position`,
	)
	if err != nil {
		t.Fatalf("read conversation columns: %v", err)
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
	if ownerColumns != 7 {
		t.Fatalf("owner UUID column count = %d, want 7", ownerColumns)
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
		t.Fatal("Identity deletion bypassed Conversation module cleanup")
	}
	if _, err := repository.GetQuestion(
		context.Background(),
		actor,
		question.ID,
	); err != nil {
		t.Fatalf("restricted Identity delete removed Question: %v", err)
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
	migrationSQL, err := migrations.Files.ReadFile(
		"000007_conversation_persistence.up.sql",
	)
	if err != nil {
		t.Fatalf("read Conversation migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(migrationSQL)); err != nil {
		t.Fatalf("apply Conversation migration: %v", err)
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

func testActor(userID string) conversation.Actor {
	return conversation.Actor{UserID: userID, SessionID: "trusted-session"}
}

func testQuestion(id string, sessionID string) conversation.PersistentQuestion {
	return conversation.PersistentQuestion{
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
	actor conversation.Actor,
	id string,
	sessionID string,
) conversation.PersistentQuestion {
	t.Helper()
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

func reserveCommand(
	question conversation.PersistentQuestion,
	key string,
) conversation.ReserveTranscriptionCommand {
	return conversation.ReserveTranscriptionCommand{
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
	actor conversation.Actor,
	question conversation.PersistentQuestion,
	key string,
) conversation.TranscriptionReservation {
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

func completeCommand(transcriptID string) conversation.CompleteTranscriptionCommand {
	return conversation.CompleteTranscriptionCommand{
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
	reservation conversation.TranscriptionReservation,
	transcriptID string,
) conversation.TranscriptCandidate {
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
	reservation conversation.TranscriptionReservation,
) conversation.JobContext {
	return conversation.JobContext{
		OwnerUserID:        ownerUserID,
		DeletionGeneration: reservation.DeletionGeneration,
		ReservationID:      reservation.ID,
		FencingToken:       reservation.FencingToken,
	}
}
