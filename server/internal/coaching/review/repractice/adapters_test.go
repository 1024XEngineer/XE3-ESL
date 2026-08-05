package repractice

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSourceReaderProjectsEvaluationSource(t *testing.T) {
	t.Parallel()

	delegate := &sourceRepositoryStub{source: speechfeedback.SameQuestionRepracticeSource{
		FeedbackItemID:    "10000000-0000-4000-8000-000000000001",
		SpeechFeedbackID:  "20000000-0000-4000-8000-000000000001",
		PracticeSessionID: "session-1",
		OriginalTurnID:    "turn-1",
		QuestionID:        "question-1",
		SourceGeneration:  2,
	}}
	reader, err := NewSourceReader(delegate)
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor()
	source, err := reader.ReadSameQuestionRepracticeSource(
		context.Background(),
		actor,
		delegate.source.FeedbackItemID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.ownerID != actor.UserID ||
		source.FeedbackItemID != delegate.source.FeedbackItemID ||
		source.SourceFeedbackID != delegate.source.SpeechFeedbackID ||
		source.PracticeSessionID != delegate.source.PracticeSessionID ||
		source.OriginalTurnID != delegate.source.OriginalTurnID ||
		source.QuestionID != delegate.source.QuestionID ||
		source.SourceGeneration != delegate.source.SourceGeneration {
		t.Fatalf("Repractice source = %#v", source)
	}
}

func TestSourceReaderHidesMissingEvaluationSource(t *testing.T) {
	t.Parallel()

	reader, err := NewSourceReader(&sourceRepositoryStub{
		err: speechfeedback.ErrSpeechFeedbackNotFound,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.ReadSameQuestionRepracticeSource(
		context.Background(),
		testActor(),
		"10000000-0000-4000-8000-000000000001",
	)
	if !errors.Is(err, review.ErrRetryRequestNotFound) {
		t.Fatalf("ReadSameQuestionRepracticeSource error = %v", err)
	}
}

func TestPracticeAuthorizerMapsReviewCommandAndUnavailableSource(t *testing.T) {
	t.Parallel()

	delegate := &practiceApplicationStub{err: practice.ErrRetryTurnNotAvailable}
	authorizer, err := NewPracticeAuthorizer(delegate)
	if err != nil {
		t.Fatal(err)
	}
	source := testRetrySource()
	err = authorizer.AuthorizeSameQuestionRetry(
		context.Background(),
		testActor(),
		source,
	)
	if !errors.Is(err, review.ErrRetryRequestSourceUnavailable) {
		t.Fatalf("AuthorizeSameQuestionRetry error = %v", err)
	}
	if delegate.command != (practice.AuthorizeSameQuestionRetryCommand{
		RetryRequestID:    source.RetryRequestID,
		PracticeSessionID: source.PracticeSessionID,
		OriginalTurnID:    source.OriginalTurnID,
		QuestionID:        source.QuestionID,
	}) {
		t.Fatalf("Practice command = %#v", delegate.command)
	}
}

func TestTurnCreatorMapsReviewSourceAndReturnsPracticeTurn(t *testing.T) {
	t.Parallel()

	delegate := &voiceServiceStub{draft: practicevoice.RetryTurnDraft{
		TurnID: "retry-turn-1",
	}}
	creator, err := NewTurnCreator(delegate)
	if err != nil {
		t.Fatal(err)
	}
	source := testRetrySource()
	turnID, err := creator.CreateSameQuestionRetryTurn(
		context.Background(),
		testActor(),
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	if turnID != delegate.draft.TurnID ||
		delegate.command != (practicevoice.CreateRetryTurnCommand{
			RetryRequestID:    source.RetryRequestID,
			PracticeSessionID: source.PracticeSessionID,
			OriginalTurnID:    source.OriginalTurnID,
			QuestionID:        source.QuestionID,
		}) {
		t.Fatalf("Turn = %q, command = %#v", turnID, delegate.command)
	}
}

type sourceRepositoryStub struct {
	source         speechfeedback.SameQuestionRepracticeSource
	err            error
	ownerID        string
	feedbackItemID string
}

func (stub *sourceRepositoryStub) ReadSameQuestionRepracticeSource(
	_ context.Context,
	ownerID string,
	feedbackItemID string,
) (speechfeedback.SameQuestionRepracticeSource, error) {
	stub.ownerID = ownerID
	stub.feedbackItemID = feedbackItemID
	return stub.source, stub.err
}

type practiceApplicationStub struct {
	command practice.AuthorizeSameQuestionRetryCommand
	err     error
}

func (stub *practiceApplicationStub) AuthorizeSameQuestionRetry(
	_ context.Context,
	_ requestcontext.Actor,
	command practice.AuthorizeSameQuestionRetryCommand,
) (practice.RetryTurnAuthorization, error) {
	stub.command = command
	return practice.RetryTurnAuthorization{}, stub.err
}

type voiceServiceStub struct {
	command practicevoice.CreateRetryTurnCommand
	draft   practicevoice.RetryTurnDraft
	err     error
}

func (stub *voiceServiceStub) Create(
	_ context.Context,
	_ requestcontext.Actor,
	command practicevoice.CreateRetryTurnCommand,
) (practicevoice.RetryTurnDraft, error) {
	stub.command = command
	return stub.draft, stub.err
}

func testActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "30000000-0000-4000-8000-000000000001",
		SessionID: "40000000-0000-4000-8000-000000000001",
	}
}

func testRetrySource() review.SameQuestionRetrySource {
	return review.SameQuestionRetrySource{
		RetryRequestID:    "50000000-0000-4000-8000-000000000001",
		PracticeSessionID: "session-1",
		OriginalTurnID:    "turn-1",
		QuestionID:        "question-1",
	}
}
