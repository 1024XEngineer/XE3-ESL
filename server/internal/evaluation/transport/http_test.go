package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testEvaluationID = "12345678-1234-4234-8234-123456789abc"
	testRevisionID   = "22345678-1234-4234-8234-123456789abc"
	testNextRevision = "32345678-1234-4234-8234-123456789abc"
	testOtherID      = "52345678-1234-4234-8234-123456789abc"
)

var testActor = requestcontext.Actor{
	UserID:    "42345678-1234-4234-8234-123456789abc",
	SessionID: "authenticated-session",
}

type applicationStub struct {
	create func(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (EvaluationAccepted, error)
	get func(
		context.Context,
		requestcontext.Actor,
		string,
	) (EvaluationResource, error)
	getInterviewReport func(
		context.Context,
		requestcontext.Actor,
		string,
	) (InterviewReportResource, error)
	getIELTSSpeakingReport func(
		context.Context,
		requestcontext.Actor,
		string,
	) (IELTSSpeakingReportResource, error)
	listIELTSSpeakingReports func(
		context.Context,
		requestcontext.Actor,
		IELTSSpeakingReportIndexQuery,
	) (IELTSSpeakingReportIndexPageResource, error)
	reevaluate func(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.ReevaluateRequest,
	) (EvaluationAccepted, error)
}

func (stub applicationStub) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	request evaluation.CreateRequest,
) (EvaluationAccepted, error) {
	if stub.create == nil {
		return EvaluationAccepted{}, errors.New("unexpected Create")
	}
	return stub.create(ctx, actor, request)
}

func (stub applicationStub) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
) (EvaluationResource, error) {
	if stub.get == nil {
		return EvaluationResource{}, errors.New("unexpected Get")
	}
	return stub.get(ctx, actor, evaluationID)
}

func (stub applicationStub) GetInterviewReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (InterviewReportResource, error) {
	if stub.getInterviewReport == nil {
		return InterviewReportResource{},
			errors.New("unexpected GetInterviewReport")
	}
	return stub.getInterviewReport(ctx, actor, practiceSessionID)
}

func (stub applicationStub) GetIELTSSpeakingReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (IELTSSpeakingReportResource, error) {
	if stub.getIELTSSpeakingReport == nil {
		return IELTSSpeakingReportResource{},
			errors.New("unexpected GetIELTSSpeakingReport")
	}
	return stub.getIELTSSpeakingReport(
		ctx,
		actor,
		practiceSessionID,
	)
}

func (stub applicationStub) ListIELTSSpeakingReports(
	ctx context.Context,
	actor requestcontext.Actor,
	query IELTSSpeakingReportIndexQuery,
) (IELTSSpeakingReportIndexPageResource, error) {
	if stub.listIELTSSpeakingReports == nil {
		return IELTSSpeakingReportIndexPageResource{},
			errors.New("unexpected ListIELTSSpeakingReports")
	}
	return stub.listIELTSSpeakingReports(ctx, actor, query)
}

func (stub applicationStub) Reevaluate(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
	request evaluation.ReevaluateRequest,
) (EvaluationAccepted, error) {
	if stub.reevaluate == nil {
		return EvaluationAccepted{}, errors.New("unexpected Reevaluate")
	}
	return stub.reevaluate(ctx, actor, evaluationID, request)
}

func TestNewHTTPHandlerRequiresApplication(t *testing.T) {
	handler, err := NewHTTPHandler(nil, testCursorSigningKey())
	if err == nil || handler != nil {
		t.Fatal("expected a missing application to be rejected")
	}
	handler, err = NewHTTPHandler(applicationStub{}, nil)
	if err == nil || handler != nil {
		t.Fatal("expected a missing cursor signing key to be rejected")
	}
}

func TestCreateUsesTrustedActorAndReturnsQueuedIdentity(t *testing.T) {
	expectedRequest := evaluation.CreateRequest{
		PracticeSessionID: "session_demo_001",
		InputSnapshotID:   "snapshot_demo_001",
		InputRevision:     3,
		Scope:             evaluation.ScopeSession,
		SceneType:         evaluation.SceneOverseasDaily,
		Channels:          []evaluation.Channel{evaluation.ChannelScene},
		SceneStrategyRef:  "daily-scene/1.0.0",
		PipelineVersion:   "evaluation-pipeline/1.0.0",
		ClientRequestID:   "trace_create_001",
	}
	application := applicationStub{
		create: func(
			ctx context.Context,
			actor requestcontext.Actor,
			request evaluation.CreateRequest,
		) (EvaluationAccepted, error) {
			if actor != testActor {
				t.Fatalf("actor = %#v, want trusted actor %#v", actor, testActor)
			}
			fromContext, ok := requestcontext.ActorFromContext(ctx)
			if !ok || fromContext != testActor {
				t.Fatal("trusted actor was not preserved in request context")
			}
			if !reflect.DeepEqual(request, expectedRequest) {
				t.Fatalf("request = %#v, want %#v", request, expectedRequest)
			}
			return queuedAccepted(), nil
		},
	}
	router := newTestRouter(t, application, &testActor)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/evaluations",
		validCreateBody(),
		"application/json; charset=UTF-8",
	)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	assertPrivateResponse(t, response)
	assertJSONEquals(t, response.Body.String(), map[string]any{
		"evaluation_id":          testEvaluationID,
		"evaluation_revision_id": testRevisionID,
		"revision":               float64(1),
		"evaluation_status":      "QUEUED",
		"status_url":             "/v1/evaluations/" + testEvaluationID,
	})
}

func TestCreateAcceptsDigitLeadingPracticeSessionIdentifier(t *testing.T) {
	const practiceSessionID = "20000000-0000-4000-8000-000000000001"
	router := newTestRouter(t, applicationStub{
		create: func(
			_ context.Context,
			_ requestcontext.Actor,
			request evaluation.CreateRequest,
		) (EvaluationAccepted, error) {
			if request.PracticeSessionID != practiceSessionID ||
				request.InputSnapshotID != "snapshot_demo_001" {
				t.Fatalf("request = %#v", request)
			}
			return queuedAccepted(), nil
		},
	}, &testActor)
	body := strings.Replace(
		validCreateBody(),
		"session_demo_001",
		practiceSessionID,
		1,
	)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/evaluations",
		body,
		"application/json",
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestReevaluateUsesTrustedActorAndReturnsReplacementIdentity(t *testing.T) {
	expectedRequest := evaluation.ReevaluateRequest{
		Channels:         []evaluation.Channel{evaluation.ChannelScene},
		SceneStrategyRef: "daily-scene/1.0.0",
		PipelineVersion:  "evaluation-pipeline/1.0.1",
		ClientRequestID:  "trace_reevaluate_001",
	}
	application := applicationStub{
		reevaluate: func(
			ctx context.Context,
			actor requestcontext.Actor,
			evaluationID string,
			request evaluation.ReevaluateRequest,
		) (EvaluationAccepted, error) {
			if actor != testActor || evaluationID != testEvaluationID {
				t.Fatal("re-evaluate did not receive trusted route input")
			}
			fromContext, ok := requestcontext.ActorFromContext(ctx)
			if !ok || fromContext != testActor {
				t.Fatal("trusted actor was not preserved in request context")
			}
			if !reflect.DeepEqual(request, expectedRequest) {
				t.Fatalf("request = %#v, want %#v", request, expectedRequest)
			}
			return queuedReevaluation(), nil
		},
	}
	router := newTestRouter(t, application, &testActor)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/evaluations/"+testEvaluationID+"/re-evaluate",
		validReevaluateBody(),
		"application/json",
	)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	assertPrivateResponse(t, response)
	assertJSONEquals(t, response.Body.String(), map[string]any{
		"evaluation_id":          testEvaluationID,
		"evaluation_revision_id": testNextRevision,
		"revision":               float64(2),
		"supersedes_revision_id": testRevisionID,
		"evaluation_status":      "QUEUED",
		"status_url":             "/v1/evaluations/" + testEvaluationID,
	})
}

func TestWriteEndpointsReturnIdempotentReplayStateHonestly(t *testing.T) {
	statuses := []struct {
		status     evaluation.Status
		wantStatus int
	}{
		{status: evaluation.StatusQueued, wantStatus: http.StatusAccepted},
		{status: evaluation.StatusRunning, wantStatus: http.StatusOK},
		{status: evaluation.StatusReady, wantStatus: http.StatusOK},
		{status: evaluation.StatusFailed, wantStatus: http.StatusOK},
	}

	for _, endpoint := range []string{"create", "re-evaluate"} {
		for _, status := range statuses {
			t.Run(endpoint+" "+string(status.status), func(t *testing.T) {
				var (
					accepted EvaluationAccepted
					path     string
					body     string
					app      applicationStub
				)
				if endpoint == "create" {
					accepted = queuedAccepted()
					// A create replay may expose a later current revision
					// after the logical Evaluation has been re-evaluated.
					accepted.EvaluationRevisionID = testOtherID
					accepted.Revision = 3
					accepted.SupersedesRevisionID = testNextRevision
					path = "/v1/evaluations"
					body = validCreateBody()
				} else {
					accepted = queuedReevaluation()
					path = "/v1/evaluations/" + testEvaluationID +
						"/re-evaluate"
					body = validReevaluateBody()
				}
				accepted.EvaluationStatus = status.status
				accepted.Replayed = true
				if endpoint == "create" {
					app.create = fixedCreate(accepted)
				} else {
					app.reevaluate = fixedReevaluation(accepted)
				}

				router := newTestRouter(t, app, &testActor)
				response := performRequest(
					router,
					http.MethodPost,
					path,
					body,
					"application/json",
				)

				if response.Code != status.wantStatus {
					t.Fatalf(
						"status = %d, want %d, body = %s",
						response.Code,
						status.wantStatus,
						response.Body,
					)
				}
				assertPrivateResponse(t, response)
				assertJSONEquals(t, response.Body.String(), map[string]any{
					"evaluation_id": accepted.EvaluationID,
					"evaluation_revision_id": accepted.
						EvaluationRevisionID,
					"revision": float64(accepted.Revision),
					"supersedes_revision_id": accepted.
						SupersedesRevisionID,
					"evaluation_status": string(status.status),
					"status_url": "/v1/evaluations/" +
						accepted.EvaluationID,
				})
			})
		}
	}
}

func TestGetPublishesEveryLifecycleStateHonestly(t *testing.T) {
	statuses := []evaluation.Status{
		evaluation.StatusReceived,
		evaluation.StatusValidating,
		evaluation.StatusQueued,
		evaluation.StatusRunning,
		evaluation.StatusPartialReady,
		evaluation.StatusReady,
		evaluation.StatusFailed,
		evaluation.StatusSuperseded,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			resource := resourceForStatus(status)
			router := newTestRouter(t, applicationStub{
				get: func(
					ctx context.Context,
					actor requestcontext.Actor,
					evaluationID string,
				) (EvaluationResource, error) {
					if actor != testActor || evaluationID != testEvaluationID {
						t.Fatal("Get did not receive trusted route input")
					}
					fromContext, ok := requestcontext.ActorFromContext(ctx)
					if !ok || fromContext != testActor {
						t.Fatal("trusted actor was not preserved in request context")
					}
					return resource, nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodGet,
				"/v1/evaluations/"+testEvaluationID,
				"",
				"",
			)
			if response.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, body = %s",
					response.Code,
					response.Body,
				)
			}
			assertPrivateResponse(t, response)
			body := decodeJSONObjectForTest(t, response.Body.String())
			if body["evaluation_status"] != string(status) {
				t.Fatalf(
					"evaluation_status = %#v, want %q",
					body["evaluation_status"],
					status,
				)
			}
			if _, exists := body["owner_user_id"]; exists {
				t.Fatal("response exposed owner_user_id")
			}
			assertLifecycleFields(t, status, body)
		})
	}
}

func TestGetInterviewReportPublishesStrictLifecycleEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		status   evaluation.Status
		wantKey  string
		omitKeys []string
	}{
		{
			name:     "queued",
			status:   evaluation.StatusQueued,
			omitKeys: []string{"report", "stable_failure"},
		},
		{
			name:     "running",
			status:   evaluation.StatusRunning,
			omitKeys: []string{"report", "stable_failure"},
		},
		{
			name:     "ready",
			status:   evaluation.StatusReady,
			wantKey:  "report",
			omitKeys: []string{"stable_failure"},
		},
		{
			name:     "failed",
			status:   evaluation.StatusFailed,
			wantKey:  "stable_failure",
			omitKeys: []string{"report"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := interviewReportResourceForStatus(test.status)
			router := newTestRouter(t, applicationStub{
				getInterviewReport: func(
					ctx context.Context,
					actor requestcontext.Actor,
					practiceSessionID string,
				) (InterviewReportResource, error) {
					if actor != testActor ||
						practiceSessionID != "session_demo_001" {
						t.Fatalf(
							"report input actor=%#v session=%q",
							actor,
							practiceSessionID,
						)
					}
					if trusted, ok := requestcontext.ActorFromContext(ctx); !ok ||
						trusted != testActor {
						t.Fatal("trusted actor missing from report context")
					}
					return want, nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodGet,
				"/v1/practice-sessions/session_demo_001/interview-report",
				"",
				"",
			)
			if response.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, body = %s",
					response.Code,
					response.Body,
				)
			}
			if response.Header().Get("Cache-Control") !=
				"private, no-store" ||
				response.Header().Get("Pragma") != "no-cache" {
				t.Fatalf("report headers = %#v", response.Header())
			}
			body := decodeJSONObjectForTest(t, response.Body.String())
			for key, expected := range map[string]any{
				"practice_session_id":    "session_demo_001",
				"evaluation_id":          testEvaluationID,
				"evaluation_revision_id": testRevisionID,
				"revision":               float64(1),
				"evaluation_status":      string(test.status),
				"is_final":               false,
				"status_url": "/v1/practice-sessions/" +
					"session_demo_001/interview-report",
			} {
				if body[key] != expected {
					t.Errorf("%s = %#v, want %#v", key, body[key], expected)
				}
			}
			if test.wantKey != "" {
				if _, exists := body[test.wantKey]; !exists {
					t.Errorf("response missing %q: %s", test.wantKey, response.Body)
				}
			}
			for _, key := range test.omitKeys {
				if _, exists := body[key]; exists {
					t.Errorf("response fabricated %q: %s", key, response.Body)
				}
			}
			if report, exists := body["report"].(map[string]any); exists {
				if report["schema_version"] !=
					evaluation.InterviewReportSchemaVersion {
					t.Errorf("report = %#v", report)
				}
				if _, exposed := report["snapshot_id"]; exposed {
					t.Fatal("report exposed snapshot_id")
				}
			}
		})
	}
}

func TestGetInterviewReportRejectsInvalidRouteAndProjection(t *testing.T) {
	calls := 0
	router := newTestRouter(t, applicationStub{
		getInterviewReport: func(
			context.Context,
			requestcontext.Actor,
			string,
		) (InterviewReportResource, error) {
			calls++
			invalid := interviewReportResourceForStatus(
				evaluation.StatusReady,
			)
			invalid.Report.SchemaVersion = "unknown-report/v1"
			return invalid, nil
		},
	}, &testActor)
	response := performRequest(
		router,
		http.MethodGet,
		"/v1/practice-sessions/not.valid/interview-report",
		"",
		"",
	)
	assertAPIError(
		t,
		response,
		http.StatusNotFound,
		"evaluation_not_found",
	)
	if calls != 0 {
		t.Fatalf("invalid report path called application %d times", calls)
	}

	response = performRequest(
		router,
		http.MethodGet,
		"/v1/practice-sessions/session_demo_001/interview-report",
		"",
		"",
	)
	assertAPIError(
		t,
		response,
		http.StatusInternalServerError,
		"internal_error",
	)
	if calls != 1 {
		t.Fatalf("valid report path called application %d times", calls)
	}
}

func TestGetInterviewReportRejectsContradictoryFailureRetryability(
	t *testing.T,
) {
	if !validInterviewReportFailure(&EvaluationFailure{
		ReasonCode: ReasonInternalNonRetryable,
	}) {
		t.Fatal("non-retryable internal failure was rejected")
	}
	tests := []EvaluationFailure{
		{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  false,
		},
		{
			ReasonCode: ReasonInternalNonRetryable,
			Retryable:  true,
		},
		{
			ReasonCode: ReasonPolicyViolation,
			Retryable:  true,
		},
	}
	for _, failure := range tests {
		failure := failure
		t.Run(string(failure.ReasonCode), func(t *testing.T) {
			resource := interviewReportResourceForStatus(
				evaluation.StatusFailed,
			)
			resource.StableFailure = &failure
			router := newTestRouter(t, applicationStub{
				getInterviewReport: func(
					context.Context,
					requestcontext.Actor,
					string,
				) (InterviewReportResource, error) {
					return resource, nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodGet,
				"/v1/practice-sessions/session_demo_001/interview-report",
				"",
				"",
			)
			assertAPIError(
				t,
				response,
				http.StatusInternalServerError,
				"internal_error",
			)
		})
	}
}

func TestGetIELTSSpeakingReportPublishesPollableEnvelope(t *testing.T) {
	resource := ieltsSpeakingReportResourceForStatus(
		evaluation.StatusQueued,
	)
	router := newTestRouter(t, applicationStub{
		getIELTSSpeakingReport: func(
			ctx context.Context,
			actor requestcontext.Actor,
			practiceSessionID string,
		) (IELTSSpeakingReportResource, error) {
			if actor != testActor ||
				practiceSessionID != "session_ielts_001" {
				t.Fatalf(
					"report input actor=%#v session=%q",
					actor,
					practiceSessionID,
				)
			}
			trusted, ok := requestcontext.ActorFromContext(ctx)
			if !ok || trusted != testActor {
				t.Fatal("trusted actor missing from report context")
			}
			return resource, nil
		},
	}, &testActor)
	response := performRequest(
		router,
		http.MethodGet,
		"/v1/practice-sessions/session_ielts_001/ielts-speaking-report",
		"",
		"",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	assertPrivateResponse(t, response)
	assertJSONEquals(t, response.Body.String(), map[string]any{
		"practice_session_id":    "session_ielts_001",
		"evaluation_id":          testEvaluationID,
		"evaluation_revision_id": testRevisionID,
		"revision":               float64(1),
		"evaluation_status":      "QUEUED",
		"is_final":               false,
		"status_url": "/v1/practice-sessions/session_ielts_001/" +
			"ielts-speaking-report",
	})
}

func TestListIELTSSpeakingReportsUsesActorBoundSignedCursor(
	t *testing.T,
) {
	createdAt := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	items := []IELTSSpeakingReportIndexEntryResource{
		{
			PracticeSessionID:    "session_ielts_002",
			EvaluationID:         testNextRevision,
			EvaluationRevisionID: testRevisionID,
			Revision:             1,
			EvaluationStatus:     evaluation.StatusReady,
			CreatedAt:            createdAt,
			UpdatedAt:            createdAt.Add(2 * time.Minute),
		},
		{
			PracticeSessionID:    "session_ielts_001",
			EvaluationID:         testEvaluationID,
			EvaluationRevisionID: testRevisionID,
			Revision:             1,
			EvaluationStatus:     evaluation.StatusRunning,
			CreatedAt:            createdAt,
			UpdatedAt:            createdAt.Add(time.Minute),
		},
	}
	calls := 0
	application := applicationStub{
		listIELTSSpeakingReports: func(
			ctx context.Context,
			actor requestcontext.Actor,
			query IELTSSpeakingReportIndexQuery,
		) (IELTSSpeakingReportIndexPageResource, error) {
			calls++
			if actor != testActor {
				t.Fatalf("actor = %#v", actor)
			}
			trusted, ok := requestcontext.ActorFromContext(ctx)
			if !ok || trusted != testActor {
				t.Fatal("trusted actor missing from index context")
			}
			if query.Limit != 2 {
				t.Fatalf("limit = %d", query.Limit)
			}
			if calls == 1 {
				if query.Before != nil {
					t.Fatalf("first boundary = %#v", query.Before)
				}
				return IELTSSpeakingReportIndexPageResource{
					Items:   items,
					HasMore: true,
				}, nil
			}
			if query.Before == nil ||
				!query.Before.UpdatedAt.Equal(items[1].UpdatedAt) ||
				query.Before.EvaluationID != items[1].EvaluationID {
				t.Fatalf("second boundary = %#v", query.Before)
			}
			return IELTSSpeakingReportIndexPageResource{
				Items: []IELTSSpeakingReportIndexEntryResource{},
			}, nil
		},
	}
	router := newTestRouter(t, application, &testActor)
	first := performRequest(
		router,
		http.MethodGet,
		"/v1/ielts-speaking-reports?limit=2",
		"",
		"",
	)
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body)
	}
	assertPrivateResponse(t, first)
	body := decodeJSONObjectForTest(t, first.Body.String())
	cursor, ok := body["next_cursor"].(string)
	if !ok || cursor == "" || strings.Contains(cursor, ".") {
		t.Fatalf("cursor = %#v", body["next_cursor"])
	}
	responseItems, ok := body["items"].([]any)
	if !ok || len(responseItems) != 2 {
		t.Fatalf("items = %#v", body["items"])
	}
	for index, raw := range responseItems {
		item := raw.(map[string]any)
		if item["report_kind"] != "IELTS_SPEAKING_FULL_MOCK" ||
			item["practice_session_id"] != items[index].PracticeSessionID ||
			item["is_final"] != false {
			t.Fatalf("item %d = %#v", index, item)
		}
	}
	second := performRequest(
		router,
		http.MethodGet,
		"/v1/ielts-speaking-reports?limit=2&cursor="+cursor,
		"",
		"",
	)
	if second.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", second.Code, second.Body)
	}
	secondBody := decodeJSONObjectForTest(t, second.Body.String())
	if _, exists := secondBody["next_cursor"]; exists {
		t.Fatalf("terminal page exposed cursor: %s", second.Body)
	}
}

func TestIELTSSpeakingReportCursorRejectsTamperAndCrossActorReplay(
	t *testing.T,
) {
	createdAt := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	item := IELTSSpeakingReportIndexEntryResource{
		PracticeSessionID:    "session_ielts_001",
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testRevisionID,
		Revision:             1,
		EvaluationStatus:     evaluation.StatusQueued,
		CreatedAt:            createdAt,
		UpdatedAt:            createdAt.Add(time.Minute),
	}
	calls := 0
	application := applicationStub{
		listIELTSSpeakingReports: func(
			context.Context,
			requestcontext.Actor,
			IELTSSpeakingReportIndexQuery,
		) (IELTSSpeakingReportIndexPageResource, error) {
			calls++
			return IELTSSpeakingReportIndexPageResource{
				Items:   []IELTSSpeakingReportIndexEntryResource{item},
				HasMore: true,
			}, nil
		},
	}
	router := newTestRouter(t, application, &testActor)
	first := performRequest(
		router,
		http.MethodGet,
		"/v1/ielts-speaking-reports?limit=1",
		"",
		"",
	)
	body := decodeJSONObjectForTest(t, first.Body.String())
	cursor := body["next_cursor"].(string)
	tamperedPrefix := "A"
	if strings.HasPrefix(cursor, tamperedPrefix) {
		tamperedPrefix = "B"
	}
	tampered := tamperedPrefix + cursor[1:]
	response := performRequest(
		router,
		http.MethodGet,
		"/v1/ielts-speaking-reports?limit=1&cursor="+tampered,
		"",
		"",
	)
	assertAPIError(
		t,
		response,
		http.StatusBadRequest,
		"invalid_request",
	)
	otherActor := requestcontext.Actor{
		UserID:    "52345678-1234-4234-8234-123456789abc",
		SessionID: "other-session",
	}
	otherRouter := newTestRouter(t, application, &otherActor)
	response = performRequest(
		otherRouter,
		http.MethodGet,
		"/v1/ielts-speaking-reports?limit=1&cursor="+cursor,
		"",
		"",
	)
	assertAPIError(
		t,
		response,
		http.StatusBadRequest,
		"invalid_request",
	)
	if calls != 1 {
		t.Fatalf("application was called %d times", calls)
	}
}

func TestIELTSSpeakingReportIndexRejectsNonCanonicalQuery(t *testing.T) {
	calls := 0
	router := newTestRouter(t, applicationStub{
		listIELTSSpeakingReports: func(
			context.Context,
			requestcontext.Actor,
			IELTSSpeakingReportIndexQuery,
		) (IELTSSpeakingReportIndexPageResource, error) {
			calls++
			return IELTSSpeakingReportIndexPageResource{}, nil
		},
	}, &testActor)
	for _, query := range []string{
		"limit=01",
		"limit=101",
		"limit=1&limit=2",
		"cursor=",
		"unknown=value",
		"cursor=%zz",
	} {
		response := performRequest(
			router,
			http.MethodGet,
			"/v1/ielts-speaking-reports?"+query,
			"",
			"",
		)
		assertAPIError(
			t,
			response,
			http.StatusBadRequest,
			"invalid_request",
		)
	}
	if calls != 0 {
		t.Fatalf("application was called %d times", calls)
	}
}

func TestReadyProjectionMatchesEveryRequestedChannelExactly(t *testing.T) {
	sceneOnly := resourceForStatus(evaluation.StatusReady)

	coreOnly := resourceForStatus(evaluation.StatusReady)
	coreOnly.Channels = []evaluation.Channel{evaluation.ChannelCore4D}
	coreOnly.SceneStrategyRef = ""
	coreOnly.Core4DStrategyRef = "speaking-core4d/1.0.0"
	coreOnly.ModuleStatuses = map[string]ModuleStatus{"core_4d": ModuleReady}
	coreOnly.SceneResult = nil
	coreOnly.Core4DObservations = json.RawMessage(`[]`)

	dual := resourceForStatus(evaluation.StatusReady)
	dual.Channels = []evaluation.Channel{
		evaluation.ChannelScene,
		evaluation.ChannelCore4D,
	}
	dual.Core4DStrategyRef = "speaking-core4d/1.0.0"
	dual.ModuleStatuses = map[string]ModuleStatus{
		"scene":   ModuleReady,
		"core_4d": ModuleReady,
	}
	dual.Core4DObservations = json.RawMessage(`[]`)

	tests := []struct {
		name       string
		resource   EvaluationResource
		wantScene  bool
		wantCore4D bool
	}{
		{name: "scene", resource: sceneOnly, wantScene: true},
		{name: "core 4d", resource: coreOnly, wantCore4D: true},
		{
			name:       "dual",
			resource:   dual,
			wantScene:  true,
			wantCore4D: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newTestRouter(t, applicationStub{
				get: func(
					context.Context,
					requestcontext.Actor,
					string,
				) (EvaluationResource, error) {
					return test.resource, nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodGet,
				"/v1/evaluations/"+testEvaluationID,
				"",
				"",
			)
			if response.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, body = %s",
					response.Code,
					response.Body,
				)
			}
			body := decodeJSONObjectForTest(t, response.Body.String())
			_, hasScene := body["scene_result"]
			_, hasCore4D := body["core_4d_observations"]
			if hasScene != test.wantScene || hasCore4D != test.wantCore4D {
				t.Fatalf(
					"result presence scene=%v core4d=%v, body=%s",
					hasScene,
					hasCore4D,
					response.Body,
				)
			}
		})
	}
}

func TestEndpointsRequireActorFromRequestContext(t *testing.T) {
	calls := 0
	application := applicationStub{
		create: func(
			context.Context,
			requestcontext.Actor,
			evaluation.CreateRequest,
		) (EvaluationAccepted, error) {
			calls++
			return queuedAccepted(), nil
		},
		get: func(
			context.Context,
			requestcontext.Actor,
			string,
		) (EvaluationResource, error) {
			calls++
			return resourceForStatus(evaluation.StatusQueued), nil
		},
		getInterviewReport: func(
			context.Context,
			requestcontext.Actor,
			string,
		) (InterviewReportResource, error) {
			calls++
			return interviewReportResourceForStatus(
				evaluation.StatusQueued,
			), nil
		},
		getIELTSSpeakingReport: func(
			context.Context,
			requestcontext.Actor,
			string,
		) (IELTSSpeakingReportResource, error) {
			calls++
			return ieltsSpeakingReportResourceForStatus(
				evaluation.StatusQueued,
			), nil
		},
		listIELTSSpeakingReports: func(
			context.Context,
			requestcontext.Actor,
			IELTSSpeakingReportIndexQuery,
		) (IELTSSpeakingReportIndexPageResource, error) {
			calls++
			return IELTSSpeakingReportIndexPageResource{
				Items: []IELTSSpeakingReportIndexEntryResource{},
			}, nil
		},
		reevaluate: func(
			context.Context,
			requestcontext.Actor,
			string,
			evaluation.ReevaluateRequest,
		) (EvaluationAccepted, error) {
			calls++
			return queuedReevaluation(), nil
		},
	}
	router := newTestRouter(t, application, nil)
	requests := []struct {
		method      string
		path        string
		body        string
		contentType string
	}{
		{
			method: http.MethodPost,
			path:   "/v1/evaluations",
			body: strings.Replace(
				validCreateBody(),
				"{",
				`{"owner_user_id":"`+testActor.UserID+`",`,
				1,
			),
			contentType: "application/json",
		},
		{
			method: http.MethodGet,
			path:   "/v1/evaluations/" + testEvaluationID,
		},
		{
			method: http.MethodGet,
			path: "/v1/practice-sessions/session_demo_001/" +
				"interview-report",
		},
		{
			method: http.MethodGet,
			path: "/v1/practice-sessions/session_ielts_001/" +
				"ielts-speaking-report",
		},
		{
			method: http.MethodGet,
			path:   "/v1/ielts-speaking-reports",
		},
		{
			method:      http.MethodPost,
			path:        "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body:        validReevaluateBody(),
			contentType: "application/json",
		},
	}
	for _, request := range requests {
		response := performRequest(
			router,
			request.method,
			request.path,
			request.body,
			request.contentType,
		)
		assertAPIError(
			t,
			response,
			http.StatusUnauthorized,
			"authentication_required",
		)
		if response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatal("missing Bearer challenge")
		}
	}
	if calls != 0 {
		t.Fatalf("application was called %d times without a trusted actor", calls)
	}
}

func TestCreateRejectsMalformedOrOutOfContractBodies(t *testing.T) {
	oversized := strings.Repeat("x", maxEvaluationRequestBody+1)
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "missing content type", body: validCreateBody()},
		{
			name:        "wrong content type",
			body:        validCreateBody(),
			contentType: "text/plain",
		},
		{
			name:        "unsupported charset",
			body:        validCreateBody(),
			contentType: "application/json; charset=iso-8859-1",
		},
		{
			name:        "unsupported content type parameter",
			body:        validCreateBody(),
			contentType: "application/json; profile=testing",
		},
		{name: "empty body", contentType: "application/json"},
		{name: "array", body: `[]`, contentType: "application/json"},
		{name: "malformed", body: `{`, contentType: "application/json"},
		{
			name:        "invalid utf8",
			body:        string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}),
			contentType: "application/json",
		},
		{
			name: "unknown owner property",
			body: strings.Replace(
				validCreateBody(),
				"{",
				`{"owner_user_id":"ignored",`,
				1,
			),
			contentType: "application/json",
		},
		{
			name:        "trailing document",
			body:        validCreateBody() + `{}`,
			contentType: "application/json",
		},
		{
			name: "null optional string",
			body: strings.Replace(
				validCreateBody(),
				`"trace_create_001"`,
				`null`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "missing selected strategy",
			body: strings.Replace(
				validCreateBody(),
				`"scene_strategy_ref":"daily-scene/1.0.0",`,
				"",
				1,
			),
			contentType: "application/json",
		},
		{
			name: "duplicate channels",
			body: strings.Replace(
				validCreateBody(),
				`["SCENE"]`,
				`["SCENE","SCENE"]`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "invalid channel",
			body: strings.Replace(
				validCreateBody(),
				`["SCENE"]`,
				`["UNKNOWN"]`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "invalid stable identifier",
			body: strings.Replace(
				validCreateBody(),
				`session_demo_001`,
				`session.demo.001`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "invalid scope",
			body: strings.Replace(
				validCreateBody(),
				`"SESSION"`,
				`"UNKNOWN"`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "short pipeline version",
			body: strings.Replace(
				validCreateBody(),
				`evaluation-pipeline/1.0.0`,
				`v1`,
				1,
			),
			contentType: "application/json",
		},
		{
			name: "oversized body",
			body: `{"practice_session_id":"session_demo_001","padding":"` +
				oversized + `"}`,
			contentType: "application/json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			router := newTestRouter(t, applicationStub{
				create: func(
					context.Context,
					requestcontext.Actor,
					evaluation.CreateRequest,
				) (EvaluationAccepted, error) {
					calls++
					return queuedAccepted(), nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodPost,
				"/v1/evaluations",
				test.body,
				test.contentType,
			)
			assertAPIError(
				t,
				response,
				http.StatusBadRequest,
				"invalid_request",
			)
			if calls != 0 {
				t.Fatalf("application was called %d times", calls)
			}
		})
	}
}

func TestReevaluateRejectsMalformedOrOutOfContractBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body"},
		{
			name: "unknown field",
			body: strings.Replace(
				validReevaluateBody(),
				"{",
				`{"scene_type":"OVERSEAS_DAILY_LIFE",`,
				1,
			),
		},
		{
			name: "null selected strategy",
			body: strings.Replace(
				validReevaluateBody(),
				`"daily-scene/1.0.0"`,
				`null`,
				1,
			),
		},
		{
			name: "missing selected strategy",
			body: strings.Replace(
				validReevaluateBody(),
				`"scene_strategy_ref":"daily-scene/1.0.0",`,
				"",
				1,
			),
		},
		{
			name: "trailing document",
			body: validReevaluateBody() + `{}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			router := newTestRouter(t, applicationStub{
				reevaluate: func(
					context.Context,
					requestcontext.Actor,
					string,
					evaluation.ReevaluateRequest,
				) (EvaluationAccepted, error) {
					calls++
					return queuedReevaluation(), nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodPost,
				"/v1/evaluations/"+testEvaluationID+"/re-evaluate",
				test.body,
				"application/json",
			)
			assertAPIError(
				t,
				response,
				http.StatusBadRequest,
				"invalid_request",
			)
			if calls != 0 {
				t.Fatalf("application was called %d times", calls)
			}
		})
	}
}

func TestApplicationErrorsUseEvaluationContract(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid",
			err:        evaluation.ErrInvalidRequest,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "not found",
			err:        evaluation.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "evaluation_not_found",
		},
		{
			name:       "version conflict",
			err:        evaluation.ErrIdempotencyConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "evaluation_version_conflict",
		},
		{
			name: "strategy unavailable is rewritten safely",
			err: apperror.New(
				apperror.UnprocessableEntity,
				"evaluation_strategy_not_available",
				"Provider token and secret must not escape.",
				apperror.WithDetails(apperror.Detail{
					Field:  "provider_token",
					Reason: "secret",
				}),
			),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "evaluation_strategy_not_available",
		},
		{
			name: "policy violation",
			err: apperror.New(
				apperror.UnprocessableEntity,
				"evaluation_policy_violation",
				"The requested Evaluation policy is not available.",
			),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "evaluation_policy_violation",
		},
		{
			name: "retryable failure",
			err: apperror.New(
				apperror.Unavailable,
				"evaluation_retryable_failure",
				"Evaluation is temporarily unavailable.",
				apperror.WithRetryable(true),
			),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "evaluation_retryable_failure",
		},
		{
			name: "foreign module code is sanitized",
			err: apperror.New(
				apperror.NotFound,
				"practice_session_not_found",
				"Practice Session was not found.",
			),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
		{
			name: "application internal error is sanitized",
			err: apperror.New(
				apperror.Internal,
				"internal_error",
				"Provider token and secret must not escape.",
				apperror.WithDetails(apperror.Detail{
					Field:  "provider_token",
					Reason: "secret",
				}),
			),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
		{
			name:       "unknown error is sanitized",
			err:        errors.New("database DSN and secret"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newTestRouter(t, applicationStub{
				create: func(
					context.Context,
					requestcontext.Actor,
					evaluation.CreateRequest,
				) (EvaluationAccepted, error) {
					return EvaluationAccepted{}, test.err
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodPost,
				"/v1/evaluations",
				validCreateBody(),
				"application/json",
			)
			assertAPIError(
				t,
				response,
				test.wantStatus,
				test.wantCode,
			)
			if strings.Contains(response.Body.String(), "DSN") ||
				strings.Contains(response.Body.String(), "secret") ||
				strings.Contains(response.Body.String(), "provider_token") {
				t.Fatal("internal error was exposed")
			}
		})
	}
}

func TestPolicyIsDelegatedToApplication(t *testing.T) {
	calls := 0
	application := applicationStub{
		create: func(
			_ context.Context,
			_ requestcontext.Actor,
			request evaluation.CreateRequest,
		) (EvaluationAccepted, error) {
			calls++
			if request.Scope != evaluation.ScopeTurn ||
				request.SceneType != evaluation.SceneOverseasWorkplace ||
				!reflect.DeepEqual(
					request.Channels,
					[]evaluation.Channel{evaluation.ChannelCore4D},
				) ||
				request.Core4DStrategyRef != "speaking-core4d/1.0.0" {
				t.Fatalf("transport altered policy-owned input: %#v", request)
			}
			return EvaluationAccepted{}, apperror.New(
				apperror.UnprocessableEntity,
				"evaluation_policy_violation",
				"The requested Evaluation policy is not available.",
			)
		},
	}
	router := newTestRouter(t, application, &testActor)
	response := performRequest(
		router,
		http.MethodPost,
		"/v1/evaluations",
		`{
			"practice_session_id":"session_demo_001",
			"input_snapshot_id":"snapshot_demo_001",
			"input_revision":3,
			"scope":"TURN",
			"scene_type":"OVERSEAS_WORKPLACE",
			"channels":["CORE_4D"],
			"core_4d_strategy_ref":"speaking-core4d/1.0.0",
			"pipeline_version":"evaluation-pipeline/1.0.0"
		}`,
		"application/json",
	)
	assertAPIError(
		t,
		response,
		http.StatusUnprocessableEntity,
		"evaluation_policy_violation",
	)
	if calls != 1 {
		t.Fatalf("application calls = %d, want 1", calls)
	}
}

func TestInvalidApplicationResourceProjectionsFailClosed(t *testing.T) {
	readyWithoutResult := resourceForStatus(evaluation.StatusReady)
	readyWithoutResult.SceneResult = nil

	readyWithUnrequestedCore := resourceForStatus(evaluation.StatusReady)
	readyWithUnrequestedCore.Core4DObservations = json.RawMessage(`[]`)

	failedWithoutFailure := resourceForStatus(evaluation.StatusFailed)
	failedWithoutFailure.StableFailure = nil

	runningWithResult := resourceForStatus(evaluation.StatusRunning)
	runningWithResult.SceneResult = genericSceneResult()

	partialWithoutResult := resourceForStatus(evaluation.StatusPartialReady)
	partialWithoutResult.SceneResult = nil

	queuedWithCompletion := resourceForStatus(evaluation.StatusQueued)
	completedAt := queuedWithCompletion.UpdatedAt
	queuedWithCompletion.CompletedAt = &completedAt

	supersededWithoutReplacement := resourceForStatus(evaluation.StatusSuperseded)
	supersededWithoutReplacement.SupersededByRevisionID = ""

	revisionWithoutParent := resourceForStatus(evaluation.StatusQueued)
	revisionWithoutParent.Revision = 2

	revisionSupersedesItself := resourceForStatus(evaluation.StatusQueued)
	revisionSupersedesItself.Revision = 2
	revisionSupersedesItself.SupersedesRevisionID =
		revisionSupersedesItself.EvaluationRevisionID

	supersededByItself := resourceForStatus(evaluation.StatusSuperseded)
	supersededByItself.SupersededByRevisionID =
		supersededByItself.EvaluationRevisionID

	invalidModule := resourceForStatus(evaluation.StatusRunning)
	invalidModule.ModuleStatuses = map[string]ModuleStatus{
		"Scene": ModuleRunning,
	}

	invalidRawResult := resourceForStatus(evaluation.StatusReady)
	invalidRawResult.SceneResult = json.RawMessage(`[]`)

	provisionalMarkedFinal := resourceForStatus(evaluation.StatusReady)
	provisionalMarkedFinal.ScoreabilityStatus = ScoreabilityProvisional
	provisionalMarkedFinal.IsFinal = true

	runningMarkedFinal := resourceForStatus(evaluation.StatusRunning)
	runningMarkedFinal.IsFinal = true

	partialMarkedFinal := resourceForStatus(evaluation.StatusPartialReady)
	partialMarkedFinal.IsFinal = true

	gateWithoutScoreability := resourceForStatus(evaluation.StatusPartialReady)
	gateWithoutScoreability.ScoreabilityStatus = ""

	wrongEvaluationID := resourceForStatus(evaluation.StatusQueued)
	wrongEvaluationID.EvaluationID = testOtherID

	tests := []struct {
		name     string
		resource EvaluationResource
	}{
		{name: "READY missing requested result", resource: readyWithoutResult},
		{
			name:     "READY has unrequested Core4D result",
			resource: readyWithUnrequestedCore,
		},
		{name: "FAILED missing stable failure", resource: failedWithoutFailure},
		{name: "RUNNING has result", resource: runningWithResult},
		{name: "PARTIAL_READY missing result", resource: partialWithoutResult},
		{name: "QUEUED has completion", resource: queuedWithCompletion},
		{
			name:     "SUPERSEDED missing replacement",
			resource: supersededWithoutReplacement,
		},
		{name: "revision lineage missing parent", resource: revisionWithoutParent},
		{
			name:     "revision lineage points to itself",
			resource: revisionSupersedesItself,
		},
		{
			name:     "superseded replacement points to itself",
			resource: supersededByItself,
		},
		{name: "invalid module map", resource: invalidModule},
		{name: "invalid raw result kind", resource: invalidRawResult},
		{name: "PROVISIONAL is marked final", resource: provisionalMarkedFinal},
		{name: "RUNNING is marked final", resource: runningMarkedFinal},
		{name: "PARTIAL_READY is marked final", resource: partialMarkedFinal},
		{name: "gate without scoreability", resource: gateWithoutScoreability},
		{name: "path ID mismatch", resource: wrongEvaluationID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newTestRouter(t, applicationStub{
				get: func(
					context.Context,
					requestcontext.Actor,
					string,
				) (EvaluationResource, error) {
					return test.resource, nil
				},
			}, &testActor)
			response := performRequest(
				router,
				http.MethodGet,
				"/v1/evaluations/"+testEvaluationID,
				"",
				"",
			)
			assertAPIError(
				t,
				response,
				http.StatusInternalServerError,
				"internal_error",
			)
		})
	}
}

func TestWriteEndpointsRejectInvalidFreshAndReplayProjections(t *testing.T) {
	createRunning := queuedAccepted()
	createRunning.EvaluationStatus = evaluation.StatusRunning

	createRevisionTwo := queuedAccepted()
	createRevisionTwo.EvaluationRevisionID = testNextRevision
	createRevisionTwo.Revision = 2
	createRevisionTwo.SupersedesRevisionID = testRevisionID

	createReplayMissingParent := createRevisionTwo
	createReplayMissingParent.Replayed = true
	createReplayMissingParent.SupersedesRevisionID = ""

	createReplaySupersedesItself := createRevisionTwo
	createReplaySupersedesItself.Replayed = true
	createReplaySupersedesItself.SupersedesRevisionID =
		createReplaySupersedesItself.EvaluationRevisionID

	reevaluateRunning := queuedReevaluation()
	reevaluateRunning.EvaluationStatus = evaluation.StatusRunning

	reevaluateRevisionOne := queuedAccepted()

	reevaluateMissingParent := queuedReevaluation()
	reevaluateMissingParent.SupersedesRevisionID = ""

	reevaluateWrongID := queuedReevaluation()
	reevaluateWrongID.EvaluationID = testOtherID

	reevaluateSupersedesItself := queuedReevaluation()
	reevaluateSupersedesItself.SupersedesRevisionID =
		reevaluateSupersedesItself.EvaluationRevisionID

	reevaluateReplayRevisionOne := reevaluateRevisionOne
	reevaluateReplayRevisionOne.Replayed = true

	reevaluateReplayMissingParent := reevaluateMissingParent
	reevaluateReplayMissingParent.Replayed = true

	reevaluateReplayWrongID := reevaluateWrongID
	reevaluateReplayWrongID.Replayed = true

	reevaluateReplaySupersedesItself := reevaluateSupersedesItself
	reevaluateReplaySupersedesItself.Replayed = true

	tests := []struct {
		name        string
		path        string
		body        string
		application applicationStub
	}{
		{
			name: "create returned RUNNING",
			path: "/v1/evaluations",
			body: validCreateBody(),
			application: applicationStub{
				create: fixedCreate(createRunning),
			},
		},
		{
			name: "create returned revision two",
			path: "/v1/evaluations",
			body: validCreateBody(),
			application: applicationStub{
				create: fixedCreate(createRevisionTwo),
			},
		},
		{
			name: "create replay omitted immutable lineage",
			path: "/v1/evaluations",
			body: validCreateBody(),
			application: applicationStub{
				create: fixedCreate(createReplayMissingParent),
			},
		},
		{
			name: "create replay revision supersedes itself",
			path: "/v1/evaluations",
			body: validCreateBody(),
			application: applicationStub{
				create: fixedCreate(createReplaySupersedesItself),
			},
		},
		{
			name: "re-evaluate returned RUNNING",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(reevaluateRunning),
			},
		},
		{
			name: "re-evaluate returned revision one",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(reevaluateRevisionOne),
			},
		},
		{
			name: "re-evaluate omitted immutable lineage",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(reevaluateMissingParent),
			},
		},
		{
			name: "re-evaluate returned another evaluation",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(reevaluateWrongID),
			},
		},
		{
			name: "re-evaluate revision supersedes itself",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(reevaluateSupersedesItself),
			},
		},
		{
			name: "re-evaluate replay returned revision one",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(
					reevaluateReplayRevisionOne,
				),
			},
		},
		{
			name: "re-evaluate replay omitted immutable lineage",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(
					reevaluateReplayMissingParent,
				),
			},
		},
		{
			name: "re-evaluate replay returned another evaluation",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(
					reevaluateReplayWrongID,
				),
			},
		},
		{
			name: "re-evaluate replay revision supersedes itself",
			path: "/v1/evaluations/" + testEvaluationID + "/re-evaluate",
			body: validReevaluateBody(),
			application: applicationStub{
				reevaluate: fixedReevaluation(
					reevaluateReplaySupersedesItself,
				),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newTestRouter(t, test.application, &testActor)
			response := performRequest(
				router,
				http.MethodPost,
				test.path,
				test.body,
				"application/json",
			)
			assertAPIError(
				t,
				response,
				http.StatusInternalServerError,
				"internal_error",
			)
		})
	}
}

func TestInvalidEvaluationPathsAreNotPassedToApplication(t *testing.T) {
	calls := 0
	application := applicationStub{
		get: func(
			context.Context,
			requestcontext.Actor,
			string,
		) (EvaluationResource, error) {
			calls++
			return EvaluationResource{}, nil
		},
		reevaluate: func(
			context.Context,
			requestcontext.Actor,
			string,
			evaluation.ReevaluateRequest,
		) (EvaluationAccepted, error) {
			calls++
			return EvaluationAccepted{}, nil
		},
	}
	router := newTestRouter(t, application, &testActor)
	requests := []struct {
		method      string
		path        string
		body        string
		contentType string
	}{
		{
			method: http.MethodGet,
			path:   "/v1/evaluations/not-a-uuid",
		},
		{
			method:      http.MethodPost,
			path:        "/v1/evaluations/not-a-uuid/re-evaluate",
			body:        validReevaluateBody(),
			contentType: "application/json",
		},
	}
	for _, request := range requests {
		response := performRequest(
			router,
			request.method,
			request.path,
			request.body,
			request.contentType,
		)
		assertAPIError(
			t,
			response,
			http.StatusNotFound,
			"evaluation_not_found",
		)
	}
	if calls != 0 {
		t.Fatalf("application was called %d times", calls)
	}
}

func fixedCreate(
	accepted EvaluationAccepted,
) func(
	context.Context,
	requestcontext.Actor,
	evaluation.CreateRequest,
) (EvaluationAccepted, error) {
	return func(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (EvaluationAccepted, error) {
		return accepted, nil
	}
}

func fixedReevaluation(
	accepted EvaluationAccepted,
) func(
	context.Context,
	requestcontext.Actor,
	string,
	evaluation.ReevaluateRequest,
) (EvaluationAccepted, error) {
	return func(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.ReevaluateRequest,
	) (EvaluationAccepted, error) {
		return accepted, nil
	}
}

func newTestRouter(
	t *testing.T,
	application Application,
	actor *requestcontext.Actor,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewHTTPHandler(application, testCursorSigningKey())
	if err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := httpresponse.WithCorrelationID(
			c.Request.Context(),
			"corr_evaluation_http_test",
		)
		if actor != nil {
			ctx = requestcontext.WithActor(ctx, *actor)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	handler.RegisterRoutes(router)
	return router
}

func testCursorSigningKey() []byte {
	return []byte("evaluation-http-test-cursor-key-32")
}

func performRequest(
	router http.Handler,
	method string,
	path string,
	body string,
	contentType string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func queuedAccepted() EvaluationAccepted {
	return EvaluationAccepted{
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testRevisionID,
		Revision:             1,
		EvaluationStatus:     evaluation.StatusQueued,
	}
}

func queuedReevaluation() EvaluationAccepted {
	return EvaluationAccepted{
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testNextRevision,
		Revision:             2,
		SupersedesRevisionID: testRevisionID,
		EvaluationStatus:     evaluation.StatusQueued,
	}
}

func resourceForStatus(status evaluation.Status) EvaluationResource {
	createdAt := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Second)
	resource := EvaluationResource{
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testRevisionID,
		Revision:             1,
		PracticeSessionID:    "session_demo_001",
		InputSnapshotID:      "snapshot_demo_001",
		InputRevision:        3,
		Scope:                evaluation.ScopeSession,
		SceneType:            evaluation.SceneOverseasDaily,
		Channels:             []evaluation.Channel{evaluation.ChannelScene},
		SceneStrategyRef:     "daily-scene/1.0.0",
		PipelineVersion:      "evaluation-pipeline/1.0.0",
		SchemaVersion:        evaluation.SchemaVersion,
		EvaluationStatus:     status,
		ModuleStatuses: map[string]ModuleStatus{
			"scene": ModulePending,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	switch status {
	case evaluation.StatusRunning:
		resource.ModuleStatuses["scene"] = ModuleRunning
	case evaluation.StatusPartialReady:
		resource.ModuleStatuses["scene"] = ModuleReady
		resource.ScoreabilityStatus = ScoreabilityProvisional
		resource.GateStatus = GateFeedbackOnly
		resource.SceneResult = genericSceneResult()
	case evaluation.StatusReady:
		resource.ModuleStatuses["scene"] = ModuleReady
		resource.ScoreabilityStatus = ScoreabilityReliable
		resource.GateStatus = GatePass
		resource.SceneResult = genericSceneResult()
		resource.IsFinal = true
		resource.CompletedAt = &updatedAt
	case evaluation.StatusFailed:
		resource.ModuleStatuses["scene"] = ModuleFailed
		resource.StableFailure = &EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
		resource.IsFinal = true
		resource.CompletedAt = &updatedAt
	case evaluation.StatusSuperseded:
		resource.ModuleStatuses["scene"] = ModuleSkipped
		resource.SupersededByRevisionID = testNextRevision
		resource.CompletedAt = &updatedAt
	}
	return resource
}

func genericSceneResult() json.RawMessage {
	return json.RawMessage(`{"summary":"provider-independent result"}`)
}

func interviewReportResourceForStatus(
	status evaluation.Status,
) InterviewReportResource {
	resource := InterviewReportResource{
		PracticeSessionID:    "session_demo_001",
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testRevisionID,
		Revision:             1,
		EvaluationStatus:     status,
		IsFinal:              false,
	}
	switch status {
	case evaluation.StatusReady:
		report := insufficientInterviewReport()
		resource.Report = &report
	case evaluation.StatusFailed:
		resource.StableFailure = &EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	}
	return resource
}

func ieltsSpeakingReportResourceForStatus(
	status evaluation.Status,
) IELTSSpeakingReportResource {
	resource := IELTSSpeakingReportResource{
		PracticeSessionID:    "session_ielts_001",
		EvaluationID:         testEvaluationID,
		EvaluationRevisionID: testRevisionID,
		Revision:             1,
		EvaluationStatus:     status,
		IsFinal:              false,
	}
	if status == evaluation.StatusFailed {
		resource.StableFailure = &EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	}
	return resource
}

func insufficientInterviewReport() evaluation.InterviewReport {
	dimensions := make(
		[]evaluation.InterviewReportDimension,
		0,
		5,
	)
	dimensionFindings := make(
		[]evaluation.InterviewQuestionDimensionRefs,
		0,
		5,
	)
	for _, dimensionID := range evaluation.InterviewDimensions() {
		dimensions = append(
			dimensions,
			evaluation.InterviewReportDimension{
				DimensionID: dimensionID,
				ScoreabilityStatus: evaluation.
					InterviewScoreabilityInsufficient,
				GateStatus: evaluation.InterviewGateBlocked,
				ReasonCodes: []evaluation.InterviewReasonCode{
					evaluation.InterviewReasonInsufficientEvidence,
				},
				EvidenceRefIDs:         []string{},
				Strengths:              []evaluation.InterviewReportFinding{},
				Improvements:           []evaluation.InterviewReportFinding{},
				RecommendedExpressions: []evaluation.InterviewReportFinding{},
			},
		)
		dimensionFindings = append(
			dimensionFindings,
			evaluation.InterviewQuestionDimensionRefs{
				DimensionID:                     dimensionID,
				StrengthFindingIDs:              []string{},
				ImprovementFindingIDs:           []string{},
				RecommendedExpressionFindingIDs: []string{},
			},
		)
	}
	return evaluation.InterviewReport{
		SchemaVersion: evaluation.InterviewReportSchemaVersion,
		ScoreabilityStatus: evaluation.
			InterviewScoreabilityInsufficient,
		GateStatus:     evaluation.InterviewGateBlocked,
		ReadinessLevel: evaluation.InterviewReadinessNotAssessed,
		ReadinessNotice: evaluation.
			InterviewReportReadinessNotice,
		Dimensions: dimensions,
		Questions: []evaluation.InterviewReportQuestion{{
			QuestionID:        "question-1",
			QuestionType:      "PRIMARY",
			OpportunityStatus: evaluation.InterviewOpportunityNotProvided,
			AssessmentStatus:  evaluation.InterviewAssessmentNotAssessed,
			QuestionText:      "Tell me about a migration you led.",
			EvidenceRefIDs:    []string{},
			DimensionFindings: dimensionFindings,
		}},
		PriorityActions: []evaluation.InterviewReportPriorityRef{},
	}
}

func validCreateBody() string {
	return `{
		"practice_session_id":"session_demo_001",
		"input_snapshot_id":"snapshot_demo_001",
		"input_revision":3,
		"scope":"SESSION",
		"scene_type":"OVERSEAS_DAILY_LIFE",
		"channels":["SCENE"],
		"scene_strategy_ref":"daily-scene/1.0.0",
		"pipeline_version":"evaluation-pipeline/1.0.0",
		"client_request_id":"trace_create_001"
	}`
}

func validReevaluateBody() string {
	return `{
		"channels":["SCENE"],
		"scene_strategy_ref":"daily-scene/1.0.0",
		"pipeline_version":"evaluation-pipeline/1.0.1",
		"client_request_id":"trace_reevaluate_001"
	}`
}

func assertLifecycleFields(
	t *testing.T,
	status evaluation.Status,
	body map[string]any,
) {
	t.Helper()
	_, hasCompletedAt := body["completed_at"]
	_, hasScoreability := body["scoreability_status"]
	_, hasGate := body["gate_status"]
	_, hasSceneResult := body["scene_result"]
	_, hasFailure := body["stable_failure"]
	_, hasSupersededBy := body["superseded_by_revision_id"]

	switch status {
	case evaluation.StatusReceived,
		evaluation.StatusValidating,
		evaluation.StatusQueued,
		evaluation.StatusRunning:
		if hasCompletedAt || hasScoreability || hasGate ||
			hasSceneResult || hasFailure || hasSupersededBy {
			t.Fatalf("active lifecycle fabricated terminal fields: %#v", body)
		}
	case evaluation.StatusPartialReady:
		if hasCompletedAt || !hasScoreability || !hasGate ||
			!hasSceneResult || hasFailure || hasSupersededBy {
			t.Fatalf("partial lifecycle fields are inconsistent: %#v", body)
		}
	case evaluation.StatusReady:
		if !hasCompletedAt || !hasScoreability || !hasGate ||
			!hasSceneResult || hasFailure || hasSupersededBy {
			t.Fatalf("ready lifecycle fields are inconsistent: %#v", body)
		}
	case evaluation.StatusFailed:
		if !hasCompletedAt || hasScoreability || hasGate ||
			hasSceneResult || !hasFailure || hasSupersededBy {
			t.Fatalf("failed lifecycle fields are inconsistent: %#v", body)
		}
	case evaluation.StatusSuperseded:
		if !hasCompletedAt || !hasSupersededBy {
			t.Fatalf("superseded lifecycle fields are inconsistent: %#v", body)
		}
	}
}

func assertPrivateResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	cacheControl := response.Header().Get("Cache-Control")
	if (cacheControl != "no-store" &&
		cacheControl != "private, no-store") ||
		response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("private response headers = %#v", response.Header())
	}
}

func assertAPIError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			response.Code,
			wantStatus,
			response.Body,
		)
	}
	body := decodeJSONObjectForTest(t, response.Body.String())
	payload, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error payload: %s", response.Body)
	}
	if payload["code"] != wantCode {
		t.Fatalf("error code = %#v, want %q", payload["code"], wantCode)
	}
	if payload["correlation_id"] != "corr_evaluation_http_test" {
		t.Fatalf("correlation_id = %#v", payload["correlation_id"])
	}
	assertPrivateResponse(t, response)
}

func assertJSONEquals(t *testing.T, raw string, want map[string]any) {
	t.Helper()
	got := decodeJSONObjectForTest(t, raw)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON = %#v, want %#v", got, want)
	}
}

func decodeJSONObjectForTest(t *testing.T, raw string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	return result
}
