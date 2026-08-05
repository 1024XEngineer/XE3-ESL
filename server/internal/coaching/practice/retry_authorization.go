package practice

import (
	"context"
	"time"
)

type RetryTurnAuthorization struct {
	RetryRequestID               string
	PracticeSessionID            string
	OriginalTurnID               string
	QuestionID                   string
	SceneFamily                  SceneFamily
	SceneModel                   SceneModel
	SessionStatusAtAuthorization SessionStatus
	CountsTowardEffectiveLimit   bool
	CreatedAt                    time.Time
}

type AuthorizeRetryTurnCommand struct {
	RetryRequestID    string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
}

type ResolveRetryParticipantCommand struct {
	RetryRequestID        string
	ActorSubjectNamespace string
}

type RetryTurnRepository interface {
	AuthorizeRetryTurn(
		context.Context,
		Actor,
		AuthorizeRetryTurnCommand,
	) (RetryTurnAuthorization, error)
	ResolveRetryParticipant(
		context.Context,
		Actor,
		ResolveRetryParticipantCommand,
	) (string, error)
}
