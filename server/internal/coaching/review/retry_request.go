package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrRetryRequestInvalid  = errors.New("review: invalid RepracticeRequest")
	ErrRetryRequestNotFound = errors.New(
		"review: RepracticeRequest not found",
	)
	ErrRetryRequestConflict = errors.New(
		"review: RepracticeRequest idempotency conflict",
	)
	ErrRetryRequestSourceUnavailable = errors.New(
		"review: RepracticeRequest source is no longer available",
	)
)

type RetryRequestStatus string

const (
	RetryRequestPending     RetryRequestStatus = "PENDING"
	RetryRequestTurnCreated RetryRequestStatus = "TURN_CREATED"
	RetryRequestFailed      RetryRequestStatus = "FAILED"
)

type RetryRequestFailureCode string

const (
	RetryRequestFailureSourceUnavailable RetryRequestFailureCode = "SOURCE_NO_LONGER_AVAILABLE"
	RetryRequestFailureTurnCreation      RetryRequestFailureCode = "RETRY_TURN_CREATION_FAILED"
)

type RetryRequestStableFailure struct {
	ReasonCode RetryRequestFailureCode `json:"reason_code"`
	Retryable  bool                    `json:"retryable"`
}

type RepracticeRequest struct {
	RetryRequestID    string                     `json:"retry_request_id"`
	FeedbackItemID    string                     `json:"feedback_item_id"`
	PracticeSessionID string                     `json:"practice_session_id"`
	OriginalTurnID    string                     `json:"original_turn_id"`
	QuestionID        string                     `json:"question_id"`
	NewTurnID         string                     `json:"new_turn_id,omitempty"`
	NewTurnStatus     string                     `json:"new_turn_status,omitempty"`
	AnswerPath        string                     `json:"answer_path,omitempty"`
	RetryStatus       RetryRequestStatus         `json:"retry_status"`
	StableFailure     *RetryRequestStableFailure `json:"stable_failure,omitempty"`
	StatusURL         string                     `json:"status_url"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	CompletedAt       *time.Time                 `json:"completed_at,omitempty"`
}

func RetryRequestStatusURL(retryRequestID string) string {
	if !validUUID(retryRequestID) {
		return ""
	}
	return "/v1/retry-requests/" + retryRequestID
}

func (request RepracticeRequest) valid() bool {
	if !validUUID(request.RetryRequestID) ||
		!validUUID(request.FeedbackItemID) ||
		!validRetryRequestResourceID(request.PracticeSessionID) ||
		!validRetryRequestResourceID(request.OriginalTurnID) ||
		!validRetryRequestResourceID(request.QuestionID) ||
		request.StatusURL !=
			RetryRequestStatusURL(request.RetryRequestID) ||
		request.CreatedAt.IsZero() ||
		request.UpdatedAt.Before(request.CreatedAt) {
		return false
	}
	switch request.RetryStatus {
	case RetryRequestPending:
		return request.NewTurnID == "" &&
			request.NewTurnStatus == "" &&
			request.AnswerPath == "" &&
			request.StableFailure == nil &&
			request.CompletedAt == nil
	case RetryRequestTurnCreated:
		return validRetryRequestResourceID(request.NewTurnID) &&
			request.NewTurnStatus == "ANSWERING" &&
			request.AnswerPath ==
				RetryTurnAnswerPath(request.NewTurnID) &&
			request.StableFailure == nil &&
			request.CompletedAt != nil &&
			!request.CompletedAt.Before(request.CreatedAt)
	case RetryRequestFailed:
		return request.NewTurnID == "" &&
			request.NewTurnStatus == "" &&
			request.AnswerPath == "" &&
			request.StableFailure != nil &&
			request.StableFailure.valid() &&
			request.CompletedAt != nil &&
			!request.CompletedAt.Before(request.CreatedAt)
	default:
		return false
	}
}

func RetryTurnAnswerPath(retryTurnID string) string {
	if !validRetryRequestResourceID(retryTurnID) {
		return ""
	}
	return "/v1/retry-turns/" + retryTurnID +
		"/transcription-candidates"
}

func (failure RetryRequestStableFailure) valid() bool {
	switch failure.ReasonCode {
	case RetryRequestFailureSourceUnavailable:
		return !failure.Retryable
	case RetryRequestFailureTurnCreation:
		return failure.Retryable
	default:
		return false
	}
}

type RetryRequestRepository interface {
	FindRetryRequestByKey(
		context.Context,
		string,
		string,
	) (RepracticeRequest, bool, error)
	ReserveRetryRequest(
		context.Context,
		string,
		RepracticeSource,
		string,
	) (RepracticeRequest, bool, error)
	GetRetryRequest(
		context.Context,
		string,
		string,
	) (RepracticeRequest, error)
	CompleteRetryRequest(
		context.Context,
		string,
		string,
		string,
	) (RepracticeRequest, error)
	FailRetryRequest(
		context.Context,
		string,
		string,
		RetryRequestStableFailure,
	) (RepracticeRequest, error)
}

type RepracticeSource struct {
	FeedbackItemID    string
	SourceFeedbackID  string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
	SourceGeneration  int64
}

func (source RepracticeSource) valid() bool {
	return validUUID(source.FeedbackItemID) &&
		validUUID(source.SourceFeedbackID) &&
		validRetryRequestResourceID(source.PracticeSessionID) &&
		validRetryRequestResourceID(source.OriginalTurnID) &&
		validRetryRequestResourceID(source.QuestionID) &&
		source.SourceGeneration >= 0
}

type RepracticeSourceReader interface {
	ReadSameQuestionRepracticeSource(
		context.Context,
		requestcontext.Actor,
		string,
	) (RepracticeSource, error)
}

type SameQuestionRetryPracticePort interface {
	AuthorizeSameQuestionRetry(
		context.Context,
		requestcontext.Actor,
		SameQuestionRetrySource,
	) error
}

type SameQuestionRetryConversationPort interface {
	CreateSameQuestionRetryTurn(
		context.Context,
		requestcontext.Actor,
		SameQuestionRetrySource,
	) (string, error)
}

type SameQuestionRetrySource struct {
	RetryRequestID    string
	PracticeSessionID string
	OriginalTurnID    string
	QuestionID        string
}

type RetryRequestService struct {
	repository   RetryRequestRepository
	sources      RepracticeSourceReader
	practice     SameQuestionRetryPracticePort
	conversation SameQuestionRetryConversationPort
}

func NewRetryRequestService(
	repository RetryRequestRepository,
	sources RepracticeSourceReader,
	practice SameQuestionRetryPracticePort,
	conversation SameQuestionRetryConversationPort,
) (*RetryRequestService, error) {
	if repository == nil || sources == nil || practice == nil ||
		conversation == nil {
		return nil, errors.New(
			"review: RepracticeRequest dependency is required",
		)
	}
	return &RetryRequestService{
		repository:   repository,
		sources:      sources,
		practice:     practice,
		conversation: conversation,
	}, nil
}

func (service *RetryRequestService) Request(
	ctx context.Context,
	actor requestcontext.Actor,
	feedbackItemID string,
	idempotencyKey string,
) (RepracticeRequest, bool, error) {
	if service == nil || service.repository == nil ||
		service.sources == nil || service.practice == nil ||
		service.conversation == nil ||
		ctx == nil || !actor.Valid() ||
		!validUUID(feedbackItemID) ||
		!validRetryRequestIdempotencyKey(idempotencyKey) {
		return RepracticeRequest{}, false, ErrRetryRequestInvalid
	}
	if err := ctx.Err(); err != nil {
		return RepracticeRequest{}, false, err
	}
	existing, found, err := service.repository.FindRetryRequestByKey(
		ctx,
		actor.UserID,
		idempotencyKey,
	)
	if err != nil {
		return RepracticeRequest{}, false, err
	}
	if found {
		if existing.FeedbackItemID != feedbackItemID {
			return RepracticeRequest{}, false, ErrRetryRequestConflict
		}
		return existing, false, nil
	}
	resolvedSource, err := service.sources.ReadSameQuestionRepracticeSource(
		ctx,
		actor,
		feedbackItemID,
	)
	if err != nil {
		return RepracticeRequest{}, false, err
	}
	if !resolvedSource.valid() ||
		resolvedSource.FeedbackItemID != feedbackItemID {
		return RepracticeRequest{}, false, ErrRetryRequestInvalid
	}
	request, created, err := service.repository.ReserveRetryRequest(
		ctx,
		actor.UserID,
		resolvedSource,
		idempotencyKey,
	)
	if err != nil || request.RetryStatus != RetryRequestPending {
		return request, created, err
	}
	retrySource := SameQuestionRetrySource{
		RetryRequestID:    request.RetryRequestID,
		PracticeSessionID: request.PracticeSessionID,
		OriginalTurnID:    request.OriginalTurnID,
		QuestionID:        request.QuestionID,
	}
	if err := service.practice.AuthorizeSameQuestionRetry(
		ctx,
		actor,
		retrySource,
	); err != nil {
		failure := retryRequestFailure(err)
		failed, failErr := service.repository.FailRetryRequest(
			ctx,
			actor.UserID,
			request.RetryRequestID,
			failure,
		)
		return failed, created, failErr
	}
	newTurnID, err := service.conversation.CreateSameQuestionRetryTurn(
		ctx,
		actor,
		retrySource,
	)
	if err != nil || !validRetryRequestResourceID(newTurnID) {
		if err == nil {
			err = ErrRetryRequestInvalid
		}
		failure := retryRequestFailure(err)
		failed, failErr := service.repository.FailRetryRequest(
			ctx,
			actor.UserID,
			request.RetryRequestID,
			failure,
		)
		return failed, created, failErr
	}
	completed, err := service.repository.CompleteRetryRequest(
		ctx,
		actor.UserID,
		request.RetryRequestID,
		newTurnID,
	)
	return completed, created, err
}

func (service *RetryRequestService) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	retryRequestID string,
) (RepracticeRequest, error) {
	if service == nil || service.repository == nil || ctx == nil ||
		!actor.Valid() || !validUUID(retryRequestID) {
		return RepracticeRequest{}, ErrRetryRequestInvalid
	}
	if err := ctx.Err(); err != nil {
		return RepracticeRequest{}, err
	}
	return service.repository.GetRetryRequest(
		ctx,
		actor.UserID,
		retryRequestID,
	)
}

func retryRequestFailure(err error) RetryRequestStableFailure {
	if errors.Is(err, ErrRetryRequestSourceUnavailable) {
		return RetryRequestStableFailure{
			ReasonCode: RetryRequestFailureSourceUnavailable,
			Retryable:  false,
		}
	}
	return RetryRequestStableFailure{
		ReasonCode: RetryRequestFailureTurnCreation,
		Retryable:  true,
	}
}

func validRetryRequestResourceID(value string) bool {
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

func validRetryRequestIdempotencyKey(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func retryRequestFingerprint(feedbackItemID string) [sha256.Size]byte {
	return sha256.Sum256([]byte(
		"speech-feedback-retry-request/v1\x00" + feedbackItemID,
	))
}
