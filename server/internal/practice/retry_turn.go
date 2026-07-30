package practice

import (
	"context"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

var (
	ErrRetryTurnInvalid      = errors.New("practice: invalid retry Turn")
	ErrRetryTurnNotAvailable = errors.New(
		"practice: retry Turn source is not available",
	)
	ErrRetryTurnConflict = errors.New("practice: retry Turn conflict")
)

type AuthorizeSameQuestionRetryCommand struct {
	RetryRequestID    string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
}

type RetryTurnApplication struct {
	repository            persistence.RetryTurnRepository
	actorSubjectNamespace string
}

func NewRetryTurnApplication(
	repository persistence.RetryTurnRepository,
	actorSubjectNamespace string,
) (*RetryTurnApplication, error) {
	namespace := strings.TrimSpace(actorSubjectNamespace)
	if repository == nil || namespace != actorSubjectNamespace ||
		!validSubjectNamespace(namespace) {
		return nil, errors.New(
			"practice: retry Turn dependency is required",
		)
	}
	return &RetryTurnApplication{
		repository:            repository,
		actorSubjectNamespace: namespace,
	}, nil
}

func (application *RetryTurnApplication) AuthorizeSameQuestionRetry(
	ctx context.Context,
	actor requestcontext.Actor,
	command AuthorizeSameQuestionRetryCommand,
) (persistence.RetryTurnAuthorization, error) {
	if application == nil || application.repository == nil ||
		ctx == nil || !actor.Valid() ||
		!validRetryTurnUUID(command.RetryRequestID) ||
		!validRetryTurnID(command.PracticeSessionID) ||
		!validRetryTurnID(command.OriginalTurnID) ||
		!validRetryTurnID(command.QuestionID) {
		return persistence.RetryTurnAuthorization{},
			ErrRetryTurnInvalid
	}
	if err := ctx.Err(); err != nil {
		return persistence.RetryTurnAuthorization{}, err
	}
	authorization, err := application.repository.AuthorizeRetryTurn(
		ctx,
		contextActor(actor),
		persistence.AuthorizeRetryTurnCommand(command),
	)
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		return persistence.RetryTurnAuthorization{},
			ErrRetryTurnNotAvailable
	case errors.Is(err, persistence.ErrConflict),
		errors.Is(err, persistence.ErrIdempotencyConflict):
		return persistence.RetryTurnAuthorization{},
			ErrRetryTurnConflict
	case errors.Is(err, persistence.ErrInvalidArgument):
		return persistence.RetryTurnAuthorization{},
			ErrRetryTurnInvalid
	default:
		return authorization, err
	}
}

// ResolveAuthorizedParticipant is separate from VoiceApplication's ordinary
// progression gate. It requires a durable retry authorization and deliberately
// allows both in-progress and completed eligible Sessions.
func (application *RetryTurnApplication) ResolveAuthorizedParticipant(
	ctx context.Context,
	actor requestcontext.Actor,
	retryRequestID string,
) (string, error) {
	if application == nil || application.repository == nil ||
		ctx == nil || !actor.Valid() ||
		!validRetryTurnUUID(retryRequestID) {
		return "", ErrRetryTurnInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	participantID, err := application.repository.ResolveRetryParticipant(
		ctx,
		contextActor(actor),
		persistence.ResolveRetryParticipantCommand{
			RetryRequestID: retryRequestID,
			ActorSubjectNamespace: application.
				actorSubjectNamespace,
		},
	)
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		return "", ErrRetryTurnNotAvailable
	case errors.Is(err, persistence.ErrConflict):
		return "", ErrRetryTurnConflict
	case errors.Is(err, persistence.ErrInvalidArgument):
		return "", ErrRetryTurnInvalid
	default:
		return participantID, err
	}
}

func validRetryTurnID(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '.', '_', ':', '-':
		default:
			return false
		}
	}
	return true
}

func validRetryTurnUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := range len(value) {
		switch index {
		case 8, 13, 18, 23:
			if value[index] != '-' {
				return false
			}
		default:
			character := value[index]
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}
