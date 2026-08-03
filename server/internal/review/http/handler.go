package reviewhttp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/gin-gonic/gin"
)

const (
	maxHistoryBody    = 768 * 1024
	cursorVersion     = 1
	cursorKind        = "formal_reviews"
	minCursorKeyBytes = 32
)

type History interface {
	Get(
		context.Context,
		review.Actor,
		string,
	) (review.FormalReview, error)
	ListCompleted(
		context.Context,
		review.Actor,
		review.HistoryQuery,
	) (review.HistoryPage, error)
}

type Handler struct {
	history   History
	cursorKey []byte
	errors    *httpresponse.Renderer
}

func NewHandler(
	history History,
	cursorKey []byte,
	errorRenderer *httpresponse.Renderer,
) (*Handler, error) {
	if history == nil || len(cursorKey) < minCursorKeyBytes {
		return nil, errors.New("review: HTTP history dependencies are required")
	}
	if errorRenderer == nil {
		errorRenderer = httpresponse.NewRenderer(nil)
	}
	return &Handler{
		history:   history,
		cursorKey: append([]byte(nil), cursorKey...),
		errors:    errorRenderer,
	}, nil
}

func (handler *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/formal-reviews", handler.list)
	routes.GET("/v1/formal-reviews/:review_id", handler.get)
}

func (handler *Handler) get(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	item, err := handler.history.Get(
		c.Request.Context(), review.Actor{UserID: actor.UserID}, c.Param("review_id"),
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	handler.writeBoundedJSON(c, Response(item))
}

func (handler *Handler) list(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	query, ok := handler.decodeHistoryQuery(c.Request, actor.UserID)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	page, err := handler.history.ListCompleted(
		c.Request.Context(), review.Actor{UserID: actor.UserID}, query,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	items := make([]gin.H, len(page.Items))
	for index, item := range page.Items {
		items[index] = Response(item)
	}
	result := gin.H{"items": items}
	if page.Next != nil {
		cursor, ok := handler.encodeCursor(actor.UserID, *page.Next)
		if !ok {
			handler.write(c, internalError(nil))
			return
		}
		result["next_cursor"] = cursor
	}
	handler.writeBoundedJSON(c, result)
}

func Response(item review.FormalReview) gin.H {
	result := gin.H{
		"review_id":              item.ID,
		"practice_session_id":    item.PracticeSessionID,
		"status":                 item.Status,
		"implementation_version": item.ImplementationVersion,
		"source_turn_id":         item.SourceTurnID,
		"source_turn_version":    item.SourceTurnVersion,
		"created_at":             item.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":             item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if item.EvaluationContext.ContextType != "" {
		result["evaluation_context_type"] = item.EvaluationContext.ContextType
		encoded, err := json.Marshal(item.EvaluationContext)
		if err == nil {
			result["evaluation_context"] = json.RawMessage(encoded)
		}
	}
	if item.Result != nil {
		result["result"] = item.Result
	}
	if item.CompletedAt != nil {
		result["completed_at"] = item.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func (handler *Handler) writeBoundedJSON(c *gin.Context, value any) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > maxHistoryBody {
		handler.write(c, internalError(err))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
}

func (handler *Handler) decodeHistoryQuery(
	request *http.Request,
	actorUserID string,
) (review.HistoryQuery, bool) {
	const defaultLimit = 20
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return review.HistoryQuery{}, false
	}
	for key := range values {
		if key != "limit" && key != "cursor" {
			return review.HistoryQuery{}, false
		}
	}
	query := review.HistoryQuery{Limit: defaultLimit}
	if limitValues, exists := values["limit"]; exists {
		if len(limitValues) != 1 {
			return review.HistoryQuery{}, false
		}
		limit, err := strconv.Atoi(limitValues[0])
		if err != nil || limit < 1 || limit > review.MaxHistoryPageSize {
			return review.HistoryQuery{}, false
		}
		query.Limit = limit
	}
	if cursorValues, exists := values["cursor"]; exists {
		if len(cursorValues) != 1 {
			return review.HistoryQuery{}, false
		}
		cursor, ok := handler.decodeCursor(actorUserID, cursorValues[0])
		if !ok {
			return review.HistoryQuery{}, false
		}
		query.Before = &cursor
	}
	return query, true
}

type cursorEnvelope struct {
	Version   int    `json:"v"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	ReviewID  string `json:"review_id"`
}

func (handler *Handler) encodeCursor(
	actorUserID string,
	cursor review.HistoryCursor,
) (string, bool) {
	if handler == nil || len(handler.cursorKey) < minCursorKeyBytes ||
		strings.TrimSpace(actorUserID) == "" || cursor.CreatedAt.IsZero() ||
		!validUUID(cursor.ReviewID) {
		return "", false
	}
	payload, err := json.Marshal(cursorEnvelope{
		Version:   cursorVersion,
		Kind:      cursorKind,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ReviewID:  cursor.ReviewID,
	})
	if err != nil {
		return "", false
	}
	signature := cursorMAC(handler.cursorKey, actorUserID, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature), true
}

func (handler *Handler) decodeCursor(
	actorUserID string,
	value string,
) (review.HistoryCursor, bool) {
	if handler == nil || len(handler.cursorKey) < minCursorKeyBytes ||
		strings.TrimSpace(actorUserID) == "" || value == "" || len(value) > 512 ||
		strings.Count(value, ".") != 1 {
		return review.HistoryCursor{}, false
	}
	parts := strings.SplitN(value, ".", 2)
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(payload) > 256 {
		return review.HistoryCursor{}, false
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size ||
		!hmac.Equal(signature, cursorMAC(handler.cursorKey, actorUserID, payload)) {
		return review.HistoryCursor{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope cursorEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		decoder.Decode(&struct{}{}) != io.EOF || envelope.Version != cursorVersion ||
		envelope.Kind != cursorKind || !validUUID(envelope.ReviewID) {
		return review.HistoryCursor{}, false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return review.HistoryCursor{}, false
	}
	cursor := review.HistoryCursor{CreatedAt: createdAt, ReviewID: envelope.ReviewID}
	canonical, ok := handler.encodeCursor(actorUserID, cursor)
	if !ok || canonical != value {
		return review.HistoryCursor{}, false
	}
	return cursor, true
}

func cursorMAC(key []byte, actorUserID string, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(cursorKind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(actorUserID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !((character >= '0' && character <= '9') ||
				(character >= 'a' && character <= 'f') ||
				(character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}

func mapError(err error) error {
	switch {
	case errors.Is(err, review.ErrReviewNotFound),
		errors.Is(err, review.ErrAccountDeleted):
		return apperror.New(
			apperror.NotFound, "resource_not_found", "Resource was not found.",
			apperror.WithCause(err),
		)
	case errors.Is(err, review.ErrReviewSourceConflict),
		errors.Is(err, review.ErrReviewImplementationConflict),
		errors.Is(err, review.ErrGenerationClaimLost),
		errors.Is(err, review.ErrDeletionGenerationStale):
		return apperror.New(
			apperror.Conflict, "resource_conflict",
			"Resource state conflicts with this operation.",
			apperror.WithCause(err),
		)
	default:
		// Inputs are validated before calling History, so ErrInvalidReview here
		// means persisted state failed the authoritative Review contract.
		return internalError(err)
	}
}

func (handler *Handler) write(c *gin.Context, err error) {
	if appError, ok := apperror.From(err); ok &&
		appError.Category() == apperror.Unauthenticated {
		c.Header("WWW-Authenticate", "Bearer")
	}
	handler.errors.Write(c, err)
}

func invalidRequest(cause error) error {
	return apperror.New(
		apperror.InvalidArgument, "invalid_request", "Request validation failed.",
		apperror.WithCause(cause),
	)
}

func authenticationRequired() error {
	return apperror.New(
		apperror.Unauthenticated, "authentication_required",
		"Authentication is required.",
	)
}

func internalError(cause error) error {
	return apperror.New(
		apperror.Internal, "internal_error", "An internal error occurred.",
		apperror.WithRetryable(true), apperror.WithCause(cause),
	)
}
