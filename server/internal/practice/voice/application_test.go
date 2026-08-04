package voice

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestVoiceApplicationResolvesActorFromFrozenSnapshot(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	repository := &voiceRepositoryStub{
		session: persistence.ContextSession{
			ID:     "practice-session",
			Status: persistence.ContextSessionProgress,
		},
		snapshot: persistence.ContextSessionSnapshot{
			ID:        "snapshot-1",
			SessionID: "practice-session",
			Participants: []persistence.ContextParticipant{
				{
					ID:               "agent-participant",
					SessionID:        "practice-session",
					Role:             "FACILITATOR",
					RoleDefinitionID: "role-interviewer",
					RoleSnapshot: &scene.RoleDefinition{
						ID: "role-interviewer",
					},
					Order: 1,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.role",
						SubjectID: "role-interviewer",
					},
				},
				{
					ID:        "actor-participant",
					SessionID: "practice-session",
					Role:      "LEARNER",
					Order:     2,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: actor.UserID,
					},
				},
			},
		},
	}
	repository.session.SnapshotID = repository.snapshot.ID
	application, err := NewApplication(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	participantID, err := application.ResolveActorParticipant(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil {
		t.Fatalf("ResolveActorParticipant: %v", err)
	}
	if participantID != "actor-participant" {
		t.Fatalf("participant ID = %q", participantID)
	}
}

func TestVoiceApplicationHidesMissingActorParticipant(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000002",
		SessionID: "20000000-0000-4000-8000-000000000002",
	}
	application, err := NewApplication(
		&voiceRepositoryStub{
			session: persistence.ContextSession{
				ID:         "practice-session",
				SnapshotID: "snapshot-1",
				Status:     persistence.ContextSessionProgress,
			},
			snapshot: persistence.ContextSessionSnapshot{
				ID:        "snapshot-1",
				SessionID: "practice-session",
				Participants: []persistence.ContextParticipant{{
					ID:        "another-user",
					SessionID: "practice-session",
					Role:      "LEARNER",
					Order:     1,
					SubjectRef: persistence.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: "10000000-0000-4000-8000-000000000003",
					},
				}},
			},
		},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	if _, err := application.ResolveActorParticipant(
		context.Background(),
		actor,
		"practice-session",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveActorParticipant error = %v", err)
	}
}

func TestVoiceApplicationAppliesStableTurnWithoutContent(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000004",
		SessionID: "20000000-0000-4000-8000-000000000004",
	}
	repository := &voiceRepositoryStub{
		turnResult: persistence.TurnResult{
			SessionID:      "practice-session",
			TurnID:         "confirmed-turn",
			EffectiveTurns: 3,
			SessionVersion: 4,
			TurnLimit:      3,
			Completed:      true,
		},
	}
	application, err := NewApplication(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	progress, err := application.ApplyEffectiveTurn(
		context.Background(),
		actor,
		"practice-session",
		"confirmed-turn",
		true,
	)
	if err != nil {
		t.Fatalf("ApplyEffectiveTurn: %v", err)
	}
	if progress.EffectiveTurns != 3 ||
		progress.SessionVersion != 4 ||
		progress.TurnLimit != 3 ||
		!progress.SessionCompleted {
		t.Fatalf("progress = %#v", progress)
	}
	if repository.consumed.SessionID != "practice-session" ||
		repository.consumed.TurnID != "confirmed-turn" ||
		string(repository.consumed.Payload) != voiceEffectiveTurnPayload {
		t.Fatalf("ConsumeTurn command = %#v", repository.consumed)
	}
}

func TestVoiceApplicationAppliesFollowUpWithoutAdvancingEffectiveTurns(
	t *testing.T,
) {
	repository := &voiceRepositoryStub{turnResult: persistence.TurnResult{
		SessionID:      "practice-session",
		TurnID:         "follow-up-turn",
		Round:          1,
		EffectiveTurns: 1,
		SessionVersion: 3,
		TurnLimit:      3,
	}}
	application, err := NewApplication(repository, "speakup.user")
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}
	progress, err := application.ApplyEffectiveTurn(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000005",
			SessionID: "20000000-0000-4000-8000-000000000005",
		},
		"practice-session",
		"follow-up-turn",
		false,
	)
	if err != nil {
		t.Fatalf("ApplyEffectiveTurn: %v", err)
	}
	if progress.EffectiveTurns != 1 || progress.SessionCompleted ||
		repository.consumed.CountsTowardTurnLimit ||
		string(repository.consumed.Payload) != voiceFollowUpTurnPayload {
		t.Fatalf(
			"follow-up progress = %#v, command = %#v",
			progress,
			repository.consumed,
		)
	}
}

func TestNewVoiceApplicationRejectsInvalidNamespace(t *testing.T) {
	for _, namespace := range []string{"", " speakup.user", "SpeakUp.User", "user/name"} {
		if _, err := NewApplication(
			&voiceRepositoryStub{},
			namespace,
		); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("namespace %q error = %v", namespace, err)
		}
	}
}

func TestVoiceApplicationRejectsIncompleteProgressEvidence(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000005",
		SessionID: "20000000-0000-4000-8000-000000000005",
	}
	application, err := NewApplication(
		&voiceRepositoryStub{turnResult: persistence.TurnResult{
			SessionID:      "practice-session",
			TurnID:         "confirmed-turn",
			EffectiveTurns: 1,
			SessionVersion: 2,
		}},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	if _, err := application.ApplyEffectiveTurn(
		context.Background(),
		actor,
		"practice-session",
		"confirmed-turn",
		true,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("ApplyEffectiveTurn error = %v", err)
	}
}

func TestVoiceApplicationMapsRepositoryErrors(t *testing.T) {
	tests := []struct {
		name  string
		input error
		want  error
	}{
		{
			name:  "invalid argument",
			input: persistence.ErrInvalidArgument,
			want:  ErrRepository,
		},
		{
			name:  "not found",
			input: persistence.ErrNotFound,
			want:  ErrRepository,
		},
		{
			name:  "idempotency conflict",
			input: persistence.ErrIdempotencyConflict,
			want:  ErrRepository,
		},
		{
			name:  "conflict",
			input: persistence.ErrConflict,
			want:  ErrRepository,
		},
		{
			name:  "active session conflict",
			input: persistence.ErrActiveSessionConflict,
			want:  ErrRepository,
		},
		{
			name:  "completed session",
			input: persistence.ErrSessionCompleted,
			want:  ErrRepository,
		},
		{
			name:  "unknown repository failure",
			input: errors.New("driver unavailable"),
			want:  ErrRepository,
		},
		{
			name:  "canceled context",
			input: context.Canceled,
			want:  context.Canceled,
		},
		{
			name:  "expired context",
			input: context.DeadlineExceeded,
			want:  context.DeadlineExceeded,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application, err := NewApplication(
				&voiceRepositoryStub{consumeErr: test.input},
				"speakup.user",
			)
			if err != nil {
				t.Fatalf("NewApplication: %v", err)
			}
			_, err = application.ApplyEffectiveTurn(
				context.Background(),
				requestcontext.Actor{
					UserID:    "10000000-0000-4000-8000-000000000009",
					SessionID: "20000000-0000-4000-8000-000000000009",
				},
				"practice-session",
				"confirmed-turn",
				true,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("ApplyEffectiveTurn error = %v, want %v", err, test.want)
			}
			if test.input != test.want && errors.Is(err, test.input) {
				t.Fatalf("persistence error crossed application boundary: %v", err)
			}
		})
	}
}

func TestVoiceApplicationDoesNotRequireSynchronousIELTSReview(t *testing.T) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000006",
		SessionID: "20000000-0000-4000-8000-000000000006",
	}
	for _, model := range []scene.SceneModel{
		scene.SceneModelIELTSSpeakingFullMock,
		scene.SceneModelIELTSSpeakingPart1,
		scene.SceneModelIELTSSpeakingPart2,
		scene.SceneModelIELTSSpeakingPart3,
	} {
		t.Run(string(model), func(t *testing.T) {
			application, err := NewApplication(
				&voiceRepositoryStub{session: persistence.ContextSession{
					ID:         "practice-session",
					SceneModel: model,
				}},
				"speakup.user",
			)
			if err != nil {
				t.Fatalf("NewApplication: %v", err)
			}

			required, err := application.RequiresSessionReview(
				context.Background(),
				actor,
				"practice-session",
			)
			if err != nil {
				t.Fatalf("RequiresSessionReview: %v", err)
			}
			if required {
				t.Fatalf("IELTS model %q unexpectedly requires review", model)
			}
		})
	}
}

func TestVoiceApplicationRequiresReviewForSynchronousNonInterviewModels(
	t *testing.T,
) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000007",
		SessionID: "20000000-0000-4000-8000-000000000007",
	}
	application, err := NewApplication(
		&voiceRepositoryStub{session: persistence.ContextSession{
			ID:          "practice-session",
			SceneFamily: scene.SceneFamilyWorkplace,
			SceneModel:  scene.SceneModelProjectExperienceDeepDive,
		}},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	required, err := application.RequiresSessionReview(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil {
		t.Fatalf("RequiresSessionReview: %v", err)
	}
	if !required {
		t.Fatal("non-interview model must keep synchronous review")
	}
}

func TestVoiceApplicationDoesNotRequireSynchronousInterviewReview(
	t *testing.T,
) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000008",
		SessionID: "20000000-0000-4000-8000-000000000008",
	}
	application, err := NewApplication(
		&voiceRepositoryStub{session: persistence.ContextSession{
			ID:          "practice-session",
			SceneFamily: scene.SceneFamilyInterview,
			SceneModel:  scene.SceneModelProjectExperienceDeepDive,
		}},
		"speakup.user",
	)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}

	required, err := application.RequiresSessionReview(
		context.Background(),
		actor,
		"practice-session",
	)
	if err != nil {
		t.Fatalf("RequiresSessionReview: %v", err)
	}
	if required {
		t.Fatal("interview model unexpectedly requires synchronous review")
	}
}

type voiceRepositoryStub struct {
	session    persistence.ContextSession
	snapshot   persistence.ContextSessionSnapshot
	getErr     error
	turnResult persistence.TurnResult
	consumeErr error
	consumed   persistence.ConsumeTurnCommand
}

func (r *voiceRepositoryStub) GetContextSession(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSession, error) {
	return r.session, r.getErr
}

func (r *voiceRepositoryStub) GetContextSessionSnapshot(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSessionSnapshot, error) {
	return r.snapshot, r.getErr
}

func (r *voiceRepositoryStub) AdvanceContextVoiceTurn(
	_ context.Context,
	_ persistence.Actor,
	command persistence.ConsumeTurnCommand,
) (persistence.TurnResult, error) {
	r.consumed = command
	return r.turnResult, r.consumeErr
}

var _ repository = (*voiceRepositoryStub)(nil)
