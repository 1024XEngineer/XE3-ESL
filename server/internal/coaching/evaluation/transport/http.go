package transport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	maxEvaluationRequestBody       = 32 * 1024
	ieltsReportIndexCursorVersion  = 1
	ieltsReportIndexCursorKind     = "ielts-speaking-report-index"
	ieltsReportIndexDefaultLimit   = 20
	ieltsReportIndexMaximumLimit   = 100
	ieltsReportIndexMaximumCursor  = 512
	ieltsReportIndexMaximumPayload = 384
)

// Application is the authenticated application boundary used by the
// Evaluation HTTP transport. Implementations must join the immutable ledger
// with any persisted result or failure before returning an EvaluationResource.
type Application interface {
	Create(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (EvaluationAccepted, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (EvaluationResource, error)
	GetInterviewReport(
		context.Context,
		requestcontext.Actor,
		string,
	) (InterviewReportResource, error)
	GetIELTSSpeakingReport(
		context.Context,
		requestcontext.Actor,
		string,
	) (IELTSSpeakingReportResource, error)
	ListIELTSSpeakingReports(
		context.Context,
		requestcontext.Actor,
		IELTSSpeakingReportIndexQuery,
	) (IELTSSpeakingReportIndexPageResource, error)
	Reevaluate(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.ReevaluateRequest,
	) (EvaluationAccepted, error)
}

// EvaluationAccepted is the immutable revision identity returned by a write.
// Fresh writes must be genuinely QUEUED. Idempotent replays may expose the
// persisted current state without requeuing work.
type EvaluationAccepted struct {
	EvaluationID         string
	EvaluationRevisionID string
	Revision             int
	SupersedesRevisionID string
	EvaluationStatus     evaluation.Status
	Replayed             bool `json:"-"`
}

type ScoreabilityStatus string

const (
	ScoreabilityReliable     ScoreabilityStatus = "RELIABLE"
	ScoreabilityProvisional  ScoreabilityStatus = "PROVISIONAL"
	ScoreabilityInsufficient ScoreabilityStatus = "INSUFFICIENT"
)

type GateStatus string

const (
	GatePass         GateStatus = "PASS"
	GateFeedbackOnly GateStatus = "FEEDBACK_ONLY"
	GateBlocked      GateStatus = "BLOCKED"
)

type ModuleStatus string

const (
	ModulePending ModuleStatus = "PENDING"
	ModuleRunning ModuleStatus = "RUNNING"
	ModuleReady   ModuleStatus = "READY"
	ModuleFailed  ModuleStatus = "FAILED"
	ModuleSkipped ModuleStatus = "SKIPPED"
)

type ReasonCode string

const (
	ReasonStrategyNotAvailable     ReasonCode = "STRATEGY_NOT_AVAILABLE"
	ReasonAudioUnusable            ReasonCode = "AUDIO_UNUSABLE"
	ReasonSpeakerAttributionFailed ReasonCode = "SPEAKER_ATTRIBUTION_FAILED"
	ReasonInsufficientEvidence     ReasonCode = "INSUFFICIENT_EVIDENCE"
	ReasonASRLowConfidence         ReasonCode = "ASR_LOW_CONFIDENCE"
	ReasonRequiredDimensionMissing ReasonCode = "REQUIRED_DIMENSION_MISSING"
	ReasonScoreInconsistent        ReasonCode = "SCORE_INCONSISTENT"
	ReasonEvidenceRefInvalid       ReasonCode = "EVIDENCE_REF_INVALID"
	ReasonVersionConflict          ReasonCode = "VERSION_CONFLICT"
	ReasonDuplicateRequest         ReasonCode = "DUPLICATE_REQUEST"
	ReasonPolicyViolation          ReasonCode = "POLICY_VIOLATION"
	ReasonInternalRetryable        ReasonCode = "INTERNAL_RETRYABLE"
	ReasonInternalNonRetryable     ReasonCode = "INTERNAL_NON_RETRYABLE"
	ReasonOpportunityNotProvided   ReasonCode = "OPPORTUNITY_NOT_PROVIDED"
)

type EvaluationFailure struct {
	ReasonCode ReasonCode `json:"reason_code"`
	Retryable  bool       `json:"retryable"`
}

// EvaluationResource is an already-authorized, publishable projection. Result
// payloads remain raw only at the polymorphic result boundary; the application
// owns their schema validation before constructing this projection.
type EvaluationResource struct {
	EvaluationID           string
	EvaluationRevisionID   string
	Revision               int
	SupersedesRevisionID   string
	SupersededByRevisionID string
	PracticeSessionID      string
	InputSnapshotID        string
	InputRevision          int
	Scope                  evaluation.Scope
	SceneType              evaluation.SceneType
	Channels               []evaluation.Channel
	SceneStrategyRef       string
	Core4DStrategyRef      string
	PipelineVersion        string
	SchemaVersion          string
	EvaluationStatus       evaluation.Status
	ScoreabilityStatus     ScoreabilityStatus
	GateStatus             GateStatus
	ModuleStatuses         map[string]ModuleStatus
	SceneResult            json.RawMessage
	Core4DObservations     json.RawMessage
	StableFailure          *EvaluationFailure
	IsFinal                bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
	CompletedAt            *time.Time
}

type InterviewReportResource struct {
	PracticeSessionID    string
	EvaluationID         string
	EvaluationRevisionID string
	Revision             int
	EvaluationStatus     evaluation.Status
	IsFinal              bool
	Report               *evaluation.InterviewReport
	StableFailure        *EvaluationFailure
}

type IELTSSpeakingReportResource struct {
	PracticeSessionID    string
	EvaluationID         string
	EvaluationRevisionID string
	Revision             int
	EvaluationStatus     evaluation.Status
	IsFinal              bool
	Report               *evaluation.IELTSSpeakingReport
	StableFailure        *EvaluationFailure
}

type IELTSSpeakingReportIndexBoundary struct {
	UpdatedAt    time.Time
	EvaluationID string
}

type IELTSSpeakingReportIndexQuery struct {
	Limit  int
	Before *IELTSSpeakingReportIndexBoundary
}

type IELTSSpeakingReportIndexEntryResource struct {
	SceneType            evaluation.SceneType
	PracticeSessionID    string
	EvaluationID         string
	EvaluationRevisionID string
	Revision             int
	EvaluationStatus     evaluation.Status
	IsFinal              bool
	Title                string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type IELTSSpeakingReportIndexPageResource struct {
	Items   []IELTSSpeakingReportIndexEntryResource
	HasMore bool
}

type HTTPHandler struct {
	application Application
	errors      *httpresponse.Renderer
	cursorKey   []byte
}

func NewHTTPHandler(
	application Application,
	cursorSigningKey []byte,
) (*HTTPHandler, error) {
	if application == nil ||
		len(cursorSigningKey) < 32 ||
		len(cursorSigningKey) > 128 {
		return nil, errors.New(
			"evaluation: HTTP application and cursor signing key are required",
		)
	}
	return &HTTPHandler{
		application: application,
		errors:      httpresponse.NewRenderer(nil),
		cursorKey:   append([]byte(nil), cursorSigningKey...),
	}, nil
}

func (h *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/evaluations", h.create)
	routes.GET("/v1/evaluations/:evaluation_id", h.get)
	routes.POST("/v1/evaluations/:evaluation_id/re-evaluate", h.reevaluate)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/interview-report",
		h.getInterviewReport,
	)
	routes.GET(
		"/v1/practice-sessions/:practice_session_id/ielts-speaking-report",
		h.getIELTSSpeakingReport,
	)
	routes.GET(
		"/v1/ielts-speaking-reports",
		h.listIELTSSpeakingReports,
	)
}

func (h *HTTPHandler) create(c *gin.Context) {
	setPrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}

	var request createEvaluationRequest
	if !decodeJSONObject(c, &request) || !request.valid() {
		h.writeInvalidRequest(c)
		return
	}
	accepted, err := h.application.Create(
		c.Request.Context(),
		actor,
		request.applicationRequest(),
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !accepted.valid() || (!accepted.Replayed && accepted.Revision != 1) {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(accepted.responseStatus(), accepted.response())
}

func (h *HTTPHandler) get(c *gin.Context) {
	setPrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	evaluationID := c.Param("evaluation_id")
	if !validEvaluationID(evaluationID) {
		h.errors.Write(c, evaluationNotFoundError())
		return
	}
	resource, err := h.application.Get(
		c.Request.Context(),
		actor,
		evaluationID,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !resource.valid() || resource.EvaluationID != evaluationID {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(http.StatusOK, resource.response())
}

func (h *HTTPHandler) reevaluate(c *gin.Context) {
	setPrivateResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	evaluationID := c.Param("evaluation_id")
	if !validEvaluationID(evaluationID) {
		h.errors.Write(c, evaluationNotFoundError())
		return
	}

	var request reevaluateEvaluationRequest
	if !decodeJSONObject(c, &request) || !request.valid() {
		h.writeInvalidRequest(c)
		return
	}
	accepted, err := h.application.Reevaluate(
		c.Request.Context(),
		actor,
		evaluationID,
		request.applicationRequest(),
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !accepted.valid() ||
		accepted.EvaluationID != evaluationID ||
		accepted.Revision < 2 {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(accepted.responseStatus(), accepted.response())
}

func (h *HTTPHandler) getInterviewReport(c *gin.Context) {
	setInterviewReportResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	practiceSessionID := c.Param("practice_session_id")
	if !stableIdentifierPattern.MatchString(practiceSessionID) {
		h.errors.Write(c, evaluationNotFoundError())
		return
	}
	resource, err := h.application.GetInterviewReport(
		c.Request.Context(),
		actor,
		practiceSessionID,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !resource.valid() ||
		resource.PracticeSessionID != practiceSessionID {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(http.StatusOK, resource.response())
}

func (h *HTTPHandler) getIELTSSpeakingReport(c *gin.Context) {
	setInterviewReportResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	practiceSessionID := c.Param("practice_session_id")
	if !stableIdentifierPattern.MatchString(practiceSessionID) {
		h.errors.Write(c, evaluationNotFoundError())
		return
	}
	resource, err := h.application.GetIELTSSpeakingReport(
		c.Request.Context(),
		actor,
		practiceSessionID,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !resource.valid() ||
		resource.PracticeSessionID != practiceSessionID {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(http.StatusOK, resource.response())
}

func (h *HTTPHandler) listIELTSSpeakingReports(c *gin.Context) {
	setInterviewReportResponseHeaders(c)
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	query, ok := h.decodeIELTSSpeakingReportIndexQuery(
		actor.UserID,
		c.Request,
	)
	if !ok {
		h.writeInvalidRequest(c)
		return
	}
	resource, err := h.application.ListIELTSSpeakingReports(
		c.Request.Context(),
		actor,
		query,
	)
	if err != nil {
		h.writeApplicationError(c, err)
		return
	}
	if !resource.valid(query.Limit) {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	response, ok := h.ieltsSpeakingReportIndexResponse(
		actor.UserID,
		resource,
	)
	if !ok {
		h.errors.Write(c, errInvalidApplicationProjection)
		return
	}
	c.JSON(http.StatusOK, response)
}

type optionalString struct {
	value   string
	present bool
}

func (value *optionalString) UnmarshalJSON(raw []byte) error {
	value.present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("null string")
	}
	return json.Unmarshal(raw, &value.value)
}

type createEvaluationRequest struct {
	PracticeSessionID string               `json:"practice_session_id"`
	InputSnapshotID   string               `json:"input_snapshot_id"`
	InputRevision     int                  `json:"input_revision"`
	Scope             evaluation.Scope     `json:"scope"`
	SceneType         evaluation.SceneType `json:"scene_type"`
	Channels          []evaluation.Channel `json:"channels"`
	SceneStrategyRef  optionalString       `json:"scene_strategy_ref"`
	Core4DStrategyRef optionalString       `json:"core_4d_strategy_ref"`
	PipelineVersion   string               `json:"pipeline_version"`
	ClientRequestID   optionalString       `json:"client_request_id"`
}

func (request createEvaluationRequest) valid() bool {
	channels, ok := requestedChannels(request.Channels)
	if !ok ||
		!stableIdentifierPattern.MatchString(request.PracticeSessionID) ||
		!stableIdentifierPattern.MatchString(request.InputSnapshotID) ||
		request.InputRevision < 1 ||
		!validScope(request.Scope) ||
		!validSceneType(request.SceneType) ||
		!validVersionReference(request.PipelineVersion) ||
		!validOptionalVersion(request.SceneStrategyRef) ||
		!validOptionalVersion(request.Core4DStrategyRef) ||
		!validOptionalClientRequestID(request.ClientRequestID) {
		return false
	}
	return (!channels[evaluation.ChannelScene] ||
		request.SceneStrategyRef.present) &&
		(!channels[evaluation.ChannelCore4D] ||
			request.Core4DStrategyRef.present)
}

func (request createEvaluationRequest) applicationRequest() evaluation.CreateRequest {
	return evaluation.CreateRequest{
		PracticeSessionID: request.PracticeSessionID,
		InputSnapshotID:   request.InputSnapshotID,
		InputRevision:     request.InputRevision,
		Scope:             request.Scope,
		SceneType:         request.SceneType,
		Channels:          append([]evaluation.Channel(nil), request.Channels...),
		SceneStrategyRef:  request.SceneStrategyRef.value,
		Core4DStrategyRef: request.Core4DStrategyRef.value,
		PipelineVersion:   request.PipelineVersion,
		ClientRequestID:   request.ClientRequestID.value,
	}
}

type reevaluateEvaluationRequest struct {
	Channels          []evaluation.Channel `json:"channels"`
	SceneStrategyRef  optionalString       `json:"scene_strategy_ref"`
	Core4DStrategyRef optionalString       `json:"core_4d_strategy_ref"`
	PipelineVersion   string               `json:"pipeline_version"`
	ClientRequestID   optionalString       `json:"client_request_id"`
}

func (request reevaluateEvaluationRequest) valid() bool {
	channels, ok := requestedChannels(request.Channels)
	if !ok ||
		!validVersionReference(request.PipelineVersion) ||
		!validOptionalVersion(request.SceneStrategyRef) ||
		!validOptionalVersion(request.Core4DStrategyRef) ||
		!validOptionalClientRequestID(request.ClientRequestID) {
		return false
	}
	return (!channels[evaluation.ChannelScene] ||
		request.SceneStrategyRef.present) &&
		(!channels[evaluation.ChannelCore4D] ||
			request.Core4DStrategyRef.present)
}

func (request reevaluateEvaluationRequest) applicationRequest() evaluation.ReevaluateRequest {
	return evaluation.ReevaluateRequest{
		Channels:          append([]evaluation.Channel(nil), request.Channels...),
		SceneStrategyRef:  request.SceneStrategyRef.value,
		Core4DStrategyRef: request.Core4DStrategyRef.value,
		PipelineVersion:   request.PipelineVersion,
		ClientRequestID:   request.ClientRequestID.value,
	}
}

type evaluationAcceptedResponse struct {
	EvaluationID         string            `json:"evaluation_id"`
	EvaluationRevisionID string            `json:"evaluation_revision_id"`
	Revision             int               `json:"revision"`
	SupersedesRevisionID string            `json:"supersedes_revision_id,omitempty"`
	EvaluationStatus     evaluation.Status `json:"evaluation_status"`
	StatusURL            string            `json:"status_url"`
}

func (accepted EvaluationAccepted) valid() bool {
	if !validEvaluationID(accepted.EvaluationID) ||
		!validEvaluationID(accepted.EvaluationRevisionID) ||
		accepted.Revision < 1 {
		return false
	}
	if accepted.Replayed {
		if !validEvaluationStatus(accepted.EvaluationStatus) {
			return false
		}
	} else if accepted.EvaluationStatus != evaluation.StatusQueued {
		return false
	}
	if accepted.Revision == 1 {
		return accepted.SupersedesRevisionID == ""
	}
	return validEvaluationID(accepted.SupersedesRevisionID) &&
		accepted.SupersedesRevisionID != accepted.EvaluationRevisionID
}

func (accepted EvaluationAccepted) responseStatus() int {
	if accepted.Replayed &&
		accepted.EvaluationStatus != evaluation.StatusQueued {
		return http.StatusOK
	}
	return http.StatusAccepted
}

func (accepted EvaluationAccepted) response() evaluationAcceptedResponse {
	return evaluationAcceptedResponse{
		EvaluationID:         accepted.EvaluationID,
		EvaluationRevisionID: accepted.EvaluationRevisionID,
		Revision:             accepted.Revision,
		SupersedesRevisionID: accepted.SupersedesRevisionID,
		EvaluationStatus:     accepted.EvaluationStatus,
		StatusURL:            "/v1/evaluations/" + accepted.EvaluationID,
	}
}

type evaluationResponse struct {
	EvaluationID           string                  `json:"evaluation_id"`
	EvaluationRevisionID   string                  `json:"evaluation_revision_id"`
	Revision               int                     `json:"revision"`
	SupersedesRevisionID   string                  `json:"supersedes_revision_id,omitempty"`
	SupersededByRevisionID string                  `json:"superseded_by_revision_id,omitempty"`
	PracticeSessionID      string                  `json:"practice_session_id"`
	InputSnapshotID        string                  `json:"input_snapshot_id"`
	InputRevision          int                     `json:"input_revision"`
	Scope                  evaluation.Scope        `json:"scope"`
	SceneType              evaluation.SceneType    `json:"scene_type"`
	Channels               []evaluation.Channel    `json:"channels"`
	SceneStrategyRef       string                  `json:"scene_strategy_ref,omitempty"`
	Core4DStrategyRef      string                  `json:"core_4d_strategy_ref,omitempty"`
	PipelineVersion        string                  `json:"pipeline_version"`
	SchemaVersion          string                  `json:"schema_version"`
	EvaluationStatus       evaluation.Status       `json:"evaluation_status"`
	ScoreabilityStatus     ScoreabilityStatus      `json:"scoreability_status,omitempty"`
	GateStatus             GateStatus              `json:"gate_status,omitempty"`
	ModuleStatuses         map[string]ModuleStatus `json:"module_statuses"`
	SceneResult            json.RawMessage         `json:"scene_result,omitempty"`
	Core4DObservations     json.RawMessage         `json:"core_4d_observations,omitempty"`
	StableFailure          *EvaluationFailure      `json:"stable_failure,omitempty"`
	IsFinal                bool                    `json:"is_final"`
	CreatedAt              string                  `json:"created_at"`
	UpdatedAt              string                  `json:"updated_at"`
	CompletedAt            string                  `json:"completed_at,omitempty"`
}

type interviewReportResponse struct {
	PracticeSessionID    string                      `json:"practice_session_id"`
	EvaluationID         string                      `json:"evaluation_id"`
	EvaluationRevisionID string                      `json:"evaluation_revision_id"`
	Revision             int                         `json:"revision"`
	EvaluationStatus     evaluation.Status           `json:"evaluation_status"`
	IsFinal              bool                        `json:"is_final"`
	StatusURL            string                      `json:"status_url"`
	Report               *evaluation.InterviewReport `json:"report,omitempty"`
	StableFailure        *EvaluationFailure          `json:"stable_failure,omitempty"`
}

func (resource InterviewReportResource) response() interviewReportResponse {
	return interviewReportResponse{
		PracticeSessionID:    resource.PracticeSessionID,
		EvaluationID:         resource.EvaluationID,
		EvaluationRevisionID: resource.EvaluationRevisionID,
		Revision:             resource.Revision,
		EvaluationStatus:     resource.EvaluationStatus,
		IsFinal:              resource.IsFinal,
		StatusURL: "/v1/practice-sessions/" +
			resource.PracticeSessionID +
			"/interview-report",
		Report:        resource.Report,
		StableFailure: resource.StableFailure,
	}
}

func (resource InterviewReportResource) valid() bool {
	if !stableIdentifierPattern.MatchString(resource.PracticeSessionID) ||
		!validEvaluationID(resource.EvaluationID) ||
		!validEvaluationID(resource.EvaluationRevisionID) ||
		resource.Revision < 1 ||
		resource.IsFinal {
		return false
	}
	switch resource.EvaluationStatus {
	case evaluation.StatusQueued, evaluation.StatusRunning:
		return resource.Report == nil &&
			resource.StableFailure == nil
	case evaluation.StatusReady:
		return resource.Report != nil &&
			resource.Report.Valid() &&
			resource.StableFailure == nil
	case evaluation.StatusFailed:
		return resource.Report == nil &&
			validInterviewReportFailure(resource.StableFailure)
	default:
		return false
	}
}

func validInterviewReportFailure(failure *EvaluationFailure) bool {
	if failure == nil {
		return false
	}
	switch failure.ReasonCode {
	case ReasonPolicyViolation,
		ReasonEvidenceRefInvalid,
		ReasonVersionConflict,
		ReasonInternalNonRetryable:
		return !failure.Retryable
	case ReasonInternalRetryable:
		return failure.Retryable
	default:
		return false
	}
}

type ieltsSpeakingReportResponse struct {
	PracticeSessionID    string                          `json:"practice_session_id"`
	EvaluationID         string                          `json:"evaluation_id"`
	EvaluationRevisionID string                          `json:"evaluation_revision_id"`
	Revision             int                             `json:"revision"`
	EvaluationStatus     evaluation.Status               `json:"evaluation_status"`
	IsFinal              bool                            `json:"is_final"`
	StatusURL            string                          `json:"status_url"`
	Report               *evaluation.IELTSSpeakingReport `json:"report,omitempty"`
	StableFailure        *EvaluationFailure              `json:"stable_failure,omitempty"`
}

func (resource IELTSSpeakingReportResource) response() ieltsSpeakingReportResponse {
	return ieltsSpeakingReportResponse{
		PracticeSessionID:    resource.PracticeSessionID,
		EvaluationID:         resource.EvaluationID,
		EvaluationRevisionID: resource.EvaluationRevisionID,
		Revision:             resource.Revision,
		EvaluationStatus:     resource.EvaluationStatus,
		IsFinal:              resource.IsFinal,
		StatusURL: "/v1/practice-sessions/" +
			resource.PracticeSessionID +
			"/ielts-speaking-report",
		Report:        resource.Report,
		StableFailure: resource.StableFailure,
	}
}

func (resource IELTSSpeakingReportResource) valid() bool {
	if !stableIdentifierPattern.MatchString(resource.PracticeSessionID) ||
		!validEvaluationID(resource.EvaluationID) ||
		!validEvaluationID(resource.EvaluationRevisionID) ||
		resource.Revision < 1 ||
		resource.IsFinal {
		return false
	}
	switch resource.EvaluationStatus {
	case evaluation.StatusQueued, evaluation.StatusRunning:
		return resource.Report == nil &&
			resource.StableFailure == nil
	case evaluation.StatusReady:
		return resource.Report != nil &&
			resource.Report.Valid() &&
			resource.StableFailure == nil
	case evaluation.StatusFailed:
		return resource.Report == nil &&
			validInterviewReportFailure(resource.StableFailure)
	default:
		return false
	}
}

type ieltsSpeakingReportIndexEntryResponse struct {
	ReportKind           string            `json:"report_kind"`
	PracticeSessionID    string            `json:"practice_session_id"`
	EvaluationID         string            `json:"evaluation_id"`
	EvaluationRevisionID string            `json:"evaluation_revision_id"`
	Revision             int               `json:"revision"`
	EvaluationStatus     evaluation.Status `json:"evaluation_status"`
	IsFinal              bool              `json:"is_final"`
	Title                string            `json:"title,omitempty"`
	StatusURL            string            `json:"status_url"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
}

type ieltsSpeakingReportIndexResponse struct {
	Items      []ieltsSpeakingReportIndexEntryResponse `json:"items"`
	NextCursor string                                  `json:"next_cursor,omitempty"`
}

func (resource IELTSSpeakingReportIndexEntryResource) valid() bool {
	return (resource.SceneType == "" || resource.SceneType == evaluation.SceneIELTSSpeaking || resource.SceneType == evaluation.SceneInterview) && stableIdentifierPattern.MatchString(
		resource.PracticeSessionID,
	) &&
		validEvaluationID(resource.EvaluationID) &&
		validEvaluationID(resource.EvaluationRevisionID) &&
		resource.Revision >= 1 &&
		validIELTSSpeakingReportStatus(resource.EvaluationStatus) &&
		!resource.IsFinal &&
		!resource.CreatedAt.IsZero() &&
		!resource.UpdatedAt.Before(resource.CreatedAt)
}

func validIELTSSpeakingReportStatus(status evaluation.Status) bool {
	switch status {
	case evaluation.StatusQueued,
		evaluation.StatusRunning,
		evaluation.StatusReady,
		evaluation.StatusFailed:
		return true
	default:
		return false
	}
}

func (resource IELTSSpeakingReportIndexPageResource) valid(
	limit int,
) bool {
	if limit < 1 || limit > 100 ||
		resource.Items == nil ||
		len(resource.Items) > limit ||
		(resource.HasMore && len(resource.Items) != limit) {
		return false
	}
	for index, item := range resource.Items {
		if !item.valid() {
			return false
		}
		if index == 0 {
			continue
		}
		previous := resource.Items[index-1]
		if item.UpdatedAt.After(previous.UpdatedAt) ||
			(item.UpdatedAt.Equal(previous.UpdatedAt) &&
				item.EvaluationID >= previous.EvaluationID) {
			return false
		}
	}
	return true
}

func (h *HTTPHandler) decodeIELTSSpeakingReportIndexQuery(
	actorUserID string,
	request *http.Request,
) (IELTSSpeakingReportIndexQuery, bool) {
	if h == nil || request == nil ||
		!validEvaluationID(actorUserID) {
		return IELTSSpeakingReportIndexQuery{}, false
	}
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return IELTSSpeakingReportIndexQuery{}, false
	}
	for key := range values {
		if key != "limit" && key != "cursor" {
			return IELTSSpeakingReportIndexQuery{}, false
		}
	}
	query := IELTSSpeakingReportIndexQuery{
		Limit: ieltsReportIndexDefaultLimit,
	}
	if rawLimits, exists := values["limit"]; exists {
		if len(rawLimits) != 1 {
			return IELTSSpeakingReportIndexQuery{}, false
		}
		limit, err := strconv.Atoi(rawLimits[0])
		if err != nil ||
			limit < 1 ||
			limit > ieltsReportIndexMaximumLimit ||
			strconv.Itoa(limit) != rawLimits[0] {
			return IELTSSpeakingReportIndexQuery{}, false
		}
		query.Limit = limit
	}
	if rawCursors, exists := values["cursor"]; exists {
		if len(rawCursors) != 1 {
			return IELTSSpeakingReportIndexQuery{}, false
		}
		boundary, ok := h.decodeIELTSSpeakingReportIndexCursor(
			actorUserID,
			rawCursors[0],
		)
		if !ok {
			return IELTSSpeakingReportIndexQuery{}, false
		}
		query.Before = &boundary
	}
	return query, true
}

type ieltsSpeakingReportIndexCursorEnvelope struct {
	Version      int    `json:"v"`
	Kind         string `json:"kind"`
	UpdatedAt    string `json:"updated_at"`
	EvaluationID string `json:"evaluation_id"`
}

func (h *HTTPHandler) encodeIELTSSpeakingReportIndexCursor(
	actorUserID string,
	boundary IELTSSpeakingReportIndexBoundary,
) (string, bool) {
	if h == nil ||
		len(h.cursorKey) < 32 ||
		len(h.cursorKey) > 128 ||
		!validEvaluationID(actorUserID) ||
		boundary.UpdatedAt.IsZero() ||
		!validEvaluationID(boundary.EvaluationID) {
		return "", false
	}
	payload, err := json.Marshal(
		ieltsSpeakingReportIndexCursorEnvelope{
			Version:      ieltsReportIndexCursorVersion,
			Kind:         ieltsReportIndexCursorKind,
			UpdatedAt:    boundary.UpdatedAt.UTC().Format(time.RFC3339Nano),
			EvaluationID: boundary.EvaluationID,
		},
	)
	if err != nil || len(payload) > ieltsReportIndexMaximumPayload {
		return "", false
	}
	signature := ieltsSpeakingReportIndexCursorMAC(
		h.cursorKey,
		actorUserID,
		payload,
	)
	encoded := make([]byte, 0, len(payload)+len(signature))
	encoded = append(encoded, payload...)
	encoded = append(encoded, signature...)
	value := base64.RawURLEncoding.EncodeToString(encoded)
	if len(value) < 16 || len(value) > ieltsReportIndexMaximumCursor {
		return "", false
	}
	return value, true
}

func (h *HTTPHandler) decodeIELTSSpeakingReportIndexCursor(
	actorUserID string,
	value string,
) (IELTSSpeakingReportIndexBoundary, bool) {
	if h == nil ||
		len(h.cursorKey) < 32 ||
		len(h.cursorKey) > 128 ||
		!validEvaluationID(actorUserID) ||
		len(value) < 16 ||
		len(value) > ieltsReportIndexMaximumCursor {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil ||
		len(decoded) <= sha256.Size ||
		len(decoded)-sha256.Size > ieltsReportIndexMaximumPayload {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	payload := decoded[:len(decoded)-sha256.Size]
	signature := decoded[len(decoded)-sha256.Size:]
	if !hmac.Equal(
		signature,
		ieltsSpeakingReportIndexCursorMAC(
			h.cursorKey,
			actorUserID,
			payload,
		),
	) {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope ieltsSpeakingReportIndexCursorEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Version != ieltsReportIndexCursorVersion ||
		envelope.Kind != ieltsReportIndexCursorKind ||
		!validEvaluationID(envelope.EvaluationID) {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, envelope.UpdatedAt)
	if err != nil || updatedAt.IsZero() {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	boundary := IELTSSpeakingReportIndexBoundary{
		UpdatedAt:    updatedAt,
		EvaluationID: envelope.EvaluationID,
	}
	canonical, ok := h.encodeIELTSSpeakingReportIndexCursor(
		actorUserID,
		boundary,
	)
	if !ok || canonical != value {
		return IELTSSpeakingReportIndexBoundary{}, false
	}
	return boundary, true
}

func ieltsSpeakingReportIndexCursorMAC(
	key []byte,
	actorUserID string,
	payload []byte,
) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(ieltsReportIndexCursorKind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(actorUserID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (h *HTTPHandler) ieltsSpeakingReportIndexResponse(
	actorUserID string,
	page IELTSSpeakingReportIndexPageResource,
) (ieltsSpeakingReportIndexResponse, bool) {
	items := make(
		[]ieltsSpeakingReportIndexEntryResponse,
		len(page.Items),
	)
	for index, item := range page.Items {
		if !item.valid() {
			return ieltsSpeakingReportIndexResponse{}, false
		}
		sceneType := item.SceneType
		if sceneType == "" {
			sceneType = evaluation.SceneIELTSSpeaking
		}
		items[index] = ieltsSpeakingReportIndexEntryResponse{
			ReportKind: map[evaluation.SceneType]string{
				evaluation.SceneIELTSSpeaking: "IELTS_SPEAKING_FULL_MOCK",
				evaluation.SceneInterview:     "INTERVIEW",
			}[sceneType],
			PracticeSessionID:    item.PracticeSessionID,
			EvaluationID:         item.EvaluationID,
			EvaluationRevisionID: item.EvaluationRevisionID,
			Revision:             item.Revision,
			EvaluationStatus:     item.EvaluationStatus,
			IsFinal:              item.IsFinal,
			Title:                item.Title,
			StatusURL: "/v1/practice-sessions/" + item.PracticeSessionID + map[evaluation.SceneType]string{
				evaluation.SceneIELTSSpeaking: "/ielts-speaking-report",
				evaluation.SceneInterview:     "/interview-report",
			}[sceneType],
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	response := ieltsSpeakingReportIndexResponse{Items: items}
	if page.HasMore {
		if len(page.Items) == 0 {
			return ieltsSpeakingReportIndexResponse{}, false
		}
		last := page.Items[len(page.Items)-1]
		cursor, ok := h.encodeIELTSSpeakingReportIndexCursor(
			actorUserID,
			IELTSSpeakingReportIndexBoundary{
				UpdatedAt:    last.UpdatedAt,
				EvaluationID: last.EvaluationID,
			},
		)
		if !ok {
			return ieltsSpeakingReportIndexResponse{}, false
		}
		response.NextCursor = cursor
	}
	return response, true
}

func (resource EvaluationResource) response() evaluationResponse {
	response := evaluationResponse{
		EvaluationID:           resource.EvaluationID,
		EvaluationRevisionID:   resource.EvaluationRevisionID,
		Revision:               resource.Revision,
		SupersedesRevisionID:   resource.SupersedesRevisionID,
		SupersededByRevisionID: resource.SupersededByRevisionID,
		PracticeSessionID:      resource.PracticeSessionID,
		InputSnapshotID:        resource.InputSnapshotID,
		InputRevision:          resource.InputRevision,
		Scope:                  resource.Scope,
		SceneType:              resource.SceneType,
		Channels: append(
			[]evaluation.Channel(nil),
			resource.Channels...,
		),
		SceneStrategyRef:   resource.SceneStrategyRef,
		Core4DStrategyRef:  resource.Core4DStrategyRef,
		PipelineVersion:    resource.PipelineVersion,
		SchemaVersion:      resource.SchemaVersion,
		EvaluationStatus:   resource.EvaluationStatus,
		ScoreabilityStatus: resource.ScoreabilityStatus,
		GateStatus:         resource.GateStatus,
		ModuleStatuses:     resource.ModuleStatuses,
		SceneResult:        resource.SceneResult,
		Core4DObservations: resource.Core4DObservations,
		StableFailure:      resource.StableFailure,
		IsFinal:            resource.IsFinal,
		CreatedAt:          resource.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:          resource.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if resource.CompletedAt != nil {
		response.CompletedAt = resource.CompletedAt.UTC().
			Format(time.RFC3339Nano)
	}
	return response
}

func (resource EvaluationResource) valid() bool {
	channels, channelsOK := requestedChannels(resource.Channels)
	if !channelsOK ||
		!validEvaluationID(resource.EvaluationID) ||
		!validEvaluationID(resource.EvaluationRevisionID) ||
		resource.Revision < 1 ||
		!validRevisionLineage(
			resource.Revision,
			resource.EvaluationRevisionID,
			resource.SupersedesRevisionID,
		) ||
		!stableIdentifierPattern.MatchString(resource.PracticeSessionID) ||
		!stableIdentifierPattern.MatchString(resource.InputSnapshotID) ||
		resource.InputRevision < 1 ||
		!validScope(resource.Scope) ||
		!validSceneType(resource.SceneType) ||
		!validSelectedStrategies(resource, channels) ||
		!validVersionReference(resource.PipelineVersion) ||
		!validVersionReference(resource.SchemaVersion) ||
		!validEvaluationStatus(resource.EvaluationStatus) ||
		!validModuleStatuses(resource.ModuleStatuses) ||
		resource.CreatedAt.IsZero() ||
		resource.UpdatedAt.Before(resource.CreatedAt) ||
		!validCompletedAt(resource) {
		return false
	}

	hasSceneResult := len(resource.SceneResult) > 0
	hasCore4DResult := len(resource.Core4DObservations) > 0
	if hasSceneResult &&
		(!channels[evaluation.ChannelScene] ||
			!validJSONObject(resource.SceneResult)) {
		return false
	}
	if hasCore4DResult &&
		(!channels[evaluation.ChannelCore4D] ||
			!validJSONArray(resource.Core4DObservations, 4)) {
		return false
	}
	if resource.ScoreabilityStatus != "" &&
		!validScoreabilityStatus(resource.ScoreabilityStatus) {
		return false
	}
	if resource.GateStatus != "" && !validGateStatus(resource.GateStatus) {
		return false
	}
	if (resource.ScoreabilityStatus == "") != (resource.GateStatus == "") {
		return false
	}

	switch resource.EvaluationStatus {
	case evaluation.StatusReceived,
		evaluation.StatusValidating,
		evaluation.StatusQueued,
		evaluation.StatusRunning:
		return resource.ScoreabilityStatus == "" &&
			resource.GateStatus == "" &&
			!hasSceneResult &&
			!hasCore4DResult &&
			resource.StableFailure == nil &&
			resource.SupersededByRevisionID == "" &&
			!resource.IsFinal
	case evaluation.StatusPartialReady:
		return (hasSceneResult || hasCore4DResult) &&
			resource.StableFailure == nil &&
			resource.SupersededByRevisionID == "" &&
			!resource.IsFinal
	case evaluation.StatusReady:
		return resource.ScoreabilityStatus != "" &&
			resource.GateStatus != "" &&
			(resource.ScoreabilityStatus != ScoreabilityProvisional ||
				!resource.IsFinal) &&
			hasSceneResult == channels[evaluation.ChannelScene] &&
			hasCore4DResult == channels[evaluation.ChannelCore4D] &&
			resource.StableFailure == nil &&
			resource.SupersededByRevisionID == ""
	case evaluation.StatusFailed:
		return resource.ScoreabilityStatus == "" &&
			resource.GateStatus == "" &&
			!hasSceneResult &&
			!hasCore4DResult &&
			validFailure(resource.StableFailure) &&
			resource.SupersededByRevisionID == ""
	case evaluation.StatusSuperseded:
		return validEvaluationID(resource.SupersededByRevisionID) &&
			resource.SupersededByRevisionID !=
				resource.EvaluationRevisionID &&
			resource.SupersededByRevisionID !=
				resource.SupersedesRevisionID &&
			(resource.StableFailure == nil ||
				validFailure(resource.StableFailure))
	default:
		return false
	}
}

func validRevisionLineage(
	revision int,
	evaluationRevisionID string,
	supersedesRevisionID string,
) bool {
	if revision == 1 {
		return supersedesRevisionID == ""
	}
	return validEvaluationID(supersedesRevisionID) &&
		supersedesRevisionID != evaluationRevisionID
}

func validSelectedStrategies(
	resource EvaluationResource,
	channels map[evaluation.Channel]bool,
) bool {
	if channels[evaluation.ChannelScene] {
		if !validVersionReference(resource.SceneStrategyRef) {
			return false
		}
	} else if resource.SceneStrategyRef != "" {
		return false
	}
	if channels[evaluation.ChannelCore4D] {
		if !validVersionReference(resource.Core4DStrategyRef) {
			return false
		}
	} else if resource.Core4DStrategyRef != "" {
		return false
	}
	return true
}

func validCompletedAt(resource EvaluationResource) bool {
	terminal := resource.EvaluationStatus == evaluation.StatusReady ||
		resource.EvaluationStatus == evaluation.StatusFailed ||
		resource.EvaluationStatus == evaluation.StatusSuperseded
	if !terminal {
		return resource.CompletedAt == nil
	}
	return resource.CompletedAt != nil &&
		!resource.CompletedAt.IsZero() &&
		!resource.CompletedAt.Before(resource.CreatedAt)
}

func validFailure(failure *EvaluationFailure) bool {
	return failure != nil && validReasonCode(failure.ReasonCode)
}

func validReasonCode(reason ReasonCode) bool {
	switch reason {
	case ReasonStrategyNotAvailable,
		ReasonAudioUnusable,
		ReasonSpeakerAttributionFailed,
		ReasonInsufficientEvidence,
		ReasonASRLowConfidence,
		ReasonRequiredDimensionMissing,
		ReasonScoreInconsistent,
		ReasonEvidenceRefInvalid,
		ReasonVersionConflict,
		ReasonDuplicateRequest,
		ReasonPolicyViolation,
		ReasonInternalRetryable,
		ReasonInternalNonRetryable,
		ReasonOpportunityNotProvided:
		return true
	default:
		return false
	}
}

func validScoreabilityStatus(status ScoreabilityStatus) bool {
	return status == ScoreabilityReliable ||
		status == ScoreabilityProvisional ||
		status == ScoreabilityInsufficient
}

func validGateStatus(status GateStatus) bool {
	return status == GatePass ||
		status == GateFeedbackOnly ||
		status == GateBlocked
}

func validModuleStatuses(statuses map[string]ModuleStatus) bool {
	if len(statuses) < 1 || len(statuses) > 32 {
		return false
	}
	for name, status := range statuses {
		if !moduleNamePattern.MatchString(name) {
			return false
		}
		switch status {
		case ModulePending,
			ModuleRunning,
			ModuleReady,
			ModuleFailed,
			ModuleSkipped:
		default:
			return false
		}
	}
	return true
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil && object != nil
}

func validJSONArray(raw json.RawMessage, maximumItems int) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' || !json.Valid(trimmed) {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(trimmed, &items) != nil ||
		items == nil || len(items) > maximumItems {
		return false
	}
	for _, item := range items {
		if !validJSONObject(item) {
			return false
		}
	}
	return true
}

func requestedChannels(
	channels []evaluation.Channel,
) (map[evaluation.Channel]bool, bool) {
	if len(channels) < 1 || len(channels) > 2 {
		return nil, false
	}
	result := make(map[evaluation.Channel]bool, len(channels))
	for _, channel := range channels {
		if channel != evaluation.ChannelScene &&
			channel != evaluation.ChannelCore4D {
			return nil, false
		}
		if result[channel] {
			return nil, false
		}
		result[channel] = true
	}
	return result, true
}

func validScope(scope evaluation.Scope) bool {
	return scope == evaluation.ScopeTurn || scope == evaluation.ScopeSession
}

func validSceneType(sceneType evaluation.SceneType) bool {
	switch sceneType {
	case evaluation.SceneIELTSSpeaking,
		evaluation.SceneInterview,
		evaluation.SceneOverseasDaily,
		evaluation.SceneOverseasWorkplace:
		return true
	default:
		return false
	}
}

func validEvaluationStatus(status evaluation.Status) bool {
	switch status {
	case evaluation.StatusReceived,
		evaluation.StatusValidating,
		evaluation.StatusQueued,
		evaluation.StatusRunning,
		evaluation.StatusPartialReady,
		evaluation.StatusReady,
		evaluation.StatusFailed,
		evaluation.StatusSuperseded:
		return true
	default:
		return false
	}
}

func validOptionalVersion(value optionalString) bool {
	return !value.present || validVersionReference(value.value)
}

func validOptionalClientRequestID(value optionalString) bool {
	return !value.present || clientRequestIDPattern.MatchString(value.value)
}

func validVersionReference(value string) bool {
	return versionReferencePattern.MatchString(value)
}

func validEvaluationID(value string) bool {
	return evaluationIDPattern.MatchString(value)
}

func decodeJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil ||
		c.Request.ContentLength > maxEvaluationRequestBody ||
		!validJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxEvaluationRequestBody,
	)
	raw, err := io.ReadAll(body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || !utf8.Valid(raw) || len(trimmed) == 0 ||
		trimmed[0] != '{' {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func validJSONContentType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	for name, parameter := range parameters {
		if !strings.EqualFold(name, "charset") ||
			!strings.EqualFold(parameter, "utf-8") {
			return false
		}
	}
	return true
}

func setPrivateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func setInterviewReportResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
}

func (h *HTTPHandler) writeAuthenticationRequired(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	h.errors.Write(c, apperror.New(
		apperror.Unauthenticated,
		"authentication_required",
		"Authentication is required.",
	))
}

func (h *HTTPHandler) writeInvalidRequest(c *gin.Context) {
	h.errors.Write(c, invalidRequestError())
}

func (h *HTTPHandler) writeApplicationError(c *gin.Context, err error) {
	h.errors.Write(c, publicApplicationError(err))
}

func publicApplicationError(err error) error {
	if appError, ok := apperror.From(err); ok {
		switch {
		case appError.Code() == "invalid_request" &&
			appError.Category() == apperror.InvalidArgument:
			return invalidRequestError()
		case appError.Code() == "evaluation_not_found" &&
			appError.Category() == apperror.NotFound:
			return evaluationNotFoundError()
		case appError.Code() == "evaluation_version_conflict" &&
			appError.Category() == apperror.Conflict:
			return evaluationVersionConflictError()
		case appError.Code() == "evaluation_strategy_not_available" &&
			appError.Category() == apperror.UnprocessableEntity:
			return evaluationStrategyNotAvailableError()
		case appError.Code() == "evaluation_policy_violation" &&
			appError.Category() == apperror.UnprocessableEntity:
			return evaluationPolicyViolationError()
		case appError.Code() == "evaluation_retryable_failure" &&
			appError.Category() == apperror.Unavailable:
			return evaluationRetryableFailureError()
		default:
			return errInvalidApplicationError
		}
	}
	switch {
	case errors.Is(err, evaluation.ErrInvalidRequest):
		return invalidRequestError()
	case errors.Is(err, evaluation.ErrNotFound):
		return evaluationNotFoundError()
	case errors.Is(err, evaluation.ErrIdempotencyConflict),
		errors.Is(err, evaluation.ErrAccountUnavailable),
		errors.Is(err, evaluation.ErrDeletionGenerationStale):
		return evaluationVersionConflictError()
	default:
		return err
	}
}

func invalidRequestError() error {
	return apperror.New(
		apperror.InvalidArgument,
		"invalid_request",
		"Request validation failed.",
	)
}

func evaluationNotFoundError() error {
	return apperror.New(
		apperror.NotFound,
		"evaluation_not_found",
		"Evaluation was not found.",
	)
}

func evaluationVersionConflictError() error {
	return apperror.New(
		apperror.Conflict,
		"evaluation_version_conflict",
		"Evaluation state changed before the operation completed.",
	)
}

func evaluationStrategyNotAvailableError() error {
	return apperror.New(
		apperror.UnprocessableEntity,
		"evaluation_strategy_not_available",
		"The requested Evaluation strategy is not available.",
	)
}

func evaluationPolicyViolationError() error {
	return apperror.New(
		apperror.UnprocessableEntity,
		"evaluation_policy_violation",
		"The requested Evaluation policy is not available.",
	)
}

func evaluationRetryableFailureError() error {
	return apperror.New(
		apperror.Unavailable,
		"evaluation_retryable_failure",
		"Evaluation is temporarily unavailable.",
		apperror.WithRetryable(true),
	)
}

var (
	stableIdentifierPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`,
	)
	evaluationIDPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
	versionReferencePattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9._:/-]{2,159}$`,
	)
	clientRequestIDPattern = regexp.MustCompile(
		`^[A-Za-z0-9._:-]{1,128}$`,
	)
	moduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

	errInvalidApplicationProjection = errors.New(
		"evaluation: invalid application projection",
	)
	errInvalidApplicationError = errors.New(
		"evaluation: application error is outside HTTP contract",
	)
)
