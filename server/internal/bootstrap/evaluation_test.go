package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationtransport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/transport"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestInterviewShadowRuntimeConfigurationIsDeterministic(t *testing.T) {
	t.Parallel()
	configuration := EvaluationConfiguration{
		Provider:        "qianwen",
		Model:           "qwen-plus",
		MaxOutputTokens: 2048,
		LeaseDuration:   30 * time.Second,
		MaxAttempts:     3,
	}
	first, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Valid() {
		t.Fatalf("runtime configuration is unstable: %#v %#v", first, second)
	}
	configuration.Model = "qwen-max"
	changed, err := interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("model change did not alter full config hash")
	}
	configuration.Model = "qwen-plus"
	configuration.MaxOutputTokens++
	changed, err = interviewShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("output budget change did not alter full config hash")
	}
}

func TestIELTSSpeakingShadowRuntimeConfigurationIsDeterministic(
	t *testing.T,
) {
	t.Parallel()
	configuration := EvaluationConfiguration{
		Provider:        "qianwen",
		Model:           "qwen-plus",
		MaxOutputTokens: 2048,
		LeaseDuration:   30 * time.Second,
		MaxAttempts:     3,
	}
	first, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Valid() {
		t.Fatalf("runtime configuration is unstable: %#v %#v", first, second)
	}
	configuration.Model = "qwen-max"
	changed, err := ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if changed.FullConfigHash == first.FullConfigHash {
		t.Fatal("model change did not alter full config hash")
	}
}

func TestInterviewShadowProjectionNeverPublishesNumericScores(
	t *testing.T,
) {
	t.Parallel()
	configuration := evaluationTestRuntimeConfiguration(t)
	result := evaluationTestInterviewShadowResult()
	persistedConfigHash := sha256.Sum256(
		[]byte("persisted-interview-shadow-config"),
	)
	projected, err := projectInterviewShadowSceneResult(
		result,
		configuration,
		persistedConfigHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(projected, &body); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"overall_raw",
		"overall_display",
		"overall_rounding_rule",
		"raw",
		"display",
		"weights",
	} {
		if strings.Contains(string(projected), `"`+forbidden+`"`) {
			t.Fatalf("projection exposed %q: %s", forbidden, projected)
		}
	}
	if body["is_final"] != false ||
		body["full_config_hash"] != "sha256:"+
			hex.EncodeToString(persistedConfigHash[:]) ||
		body["readiness_level"] != string(
			evaluation.InterviewReadinessNotAssessed,
		) {
		t.Fatalf("projection governance = %#v", body)
	}
	dimensions, ok := body["dimensions"].([]any)
	if !ok || len(dimensions) != 5 {
		t.Fatalf("dimensions = %#v", body["dimensions"])
	}
	interaction := dimensions[4].(map[string]any)
	if interaction["scoreability_status"] !=
		string(evaluation.InterviewScoreabilityInsufficient) ||
		interaction["gate_status"] !=
			string(evaluation.InterviewGateBlocked) {
		t.Fatalf("interaction projection = %#v", interaction)
	}
}

func TestInterviewShadowProjectionRejectsUnmappedReason(t *testing.T) {
	t.Parallel()
	result := evaluationTestInterviewShadowResult()
	result.Dimensions[0].ReasonCodes = []evaluation.InterviewReasonCode{
		"UNKNOWN_PROVIDER_REASON",
	}
	_, err := projectInterviewShadowSceneResult(
		result,
		evaluationTestRuntimeConfiguration(t),
		evaluationTestRuntimeConfiguration(t).FullConfigHash,
	)
	if !errors.Is(err, evaluation.ErrInvalidRequest) {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluationHTTPApplicationReturnsCompletedReplayHonestly(
	t *testing.T,
) {
	t.Parallel()
	value := evaluationTestValue(evaluation.StatusReady)
	service := &evaluationServiceStub{
		createResult:   value,
		createReplayed: true,
	}
	application := &evaluationHTTPApplication{
		evaluations:   service,
		runtime:       &evaluationRuntimeReaderStub{},
		configuration: evaluationTestRuntimeConfiguration(t),
	}
	accepted, err := application.Create(
		context.Background(),
		requestcontext.Actor{
			UserID: "00000000-0000-4000-8000-000000000001",
		},
		evaluation.CreateRequest{
			PracticeSessionID: "session_demo_001",
			InputSnapshotID:   "snapshot_demo_001",
			InputRevision:     1,
			Scope:             evaluation.ScopeSession,
			SceneType:         evaluation.SceneInterview,
			Channels:          []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef:  evaluation.InterviewShadowStrategyRef,
			PipelineVersion:   evaluation.InterviewShadowPipelineVersion,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.Replayed ||
		accepted.EvaluationID != value.ID ||
		accepted.EvaluationRevisionID != value.Revision.ID ||
		accepted.EvaluationStatus != evaluation.StatusReady {
		t.Fatalf("accepted = %#v", accepted)
	}
}

func TestEvaluationHTTPApplicationRejectsFreshCompletedProjection(
	t *testing.T,
) {
	t.Parallel()
	value := evaluationTestValue(evaluation.StatusReady)
	service := &evaluationServiceStub{createResult: value}
	application := &evaluationHTTPApplication{
		evaluations:   service,
		runtime:       &evaluationRuntimeReaderStub{},
		configuration: evaluationTestRuntimeConfiguration(t),
	}
	_, err := application.Create(
		context.Background(),
		requestcontext.Actor{
			UserID: "00000000-0000-4000-8000-000000000001",
		},
		evaluation.CreateRequest{
			PracticeSessionID: "session_demo_001",
			InputSnapshotID:   "snapshot_demo_001",
			InputRevision:     1,
			Scope:             evaluation.ScopeSession,
			SceneType:         evaluation.SceneInterview,
			Channels:          []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef:  evaluation.InterviewShadowStrategyRef,
			PipelineVersion:   evaluation.InterviewShadowPipelineVersion,
		},
	)
	appError, ok := apperror.From(err)
	if !ok || appError.Code() != "evaluation_version_conflict" {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluationHTTPApplicationReturnsReevaluationReplayHonestly(
	t *testing.T,
) {
	t.Parallel()
	current := evaluationTestValue(evaluation.StatusReady)
	replayed := evaluationTestValue(evaluation.StatusFailed)
	replayed.Revision.ID = "30000000-0000-4000-8000-000000000001"
	replayed.Revision.Number = 2
	replayed.Revision.SupersedesRevisionID = current.Revision.ID
	service := &evaluationServiceStub{
		getResult:          current,
		reevaluateResult:   replayed,
		reevaluateReplayed: true,
	}
	application := &evaluationHTTPApplication{
		evaluations:   service,
		runtime:       &evaluationRuntimeReaderStub{},
		configuration: evaluationTestRuntimeConfiguration(t),
	}
	accepted, err := application.Reevaluate(
		context.Background(),
		requestcontext.Actor{
			UserID: "00000000-0000-4000-8000-000000000001",
		},
		current.ID,
		evaluation.ReevaluateRequest{
			Channels:         []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef: evaluation.InterviewShadowStrategyRef,
			PipelineVersion:  evaluation.InterviewShadowPipelineVersion,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if service.reevaluateCalls != 1 ||
		!accepted.Replayed ||
		accepted.EvaluationRevisionID != replayed.Revision.ID ||
		accepted.SupersedesRevisionID != current.Revision.ID ||
		accepted.EvaluationStatus != evaluation.StatusFailed {
		t.Fatalf("accepted = %#v, calls = %d", accepted, service.reevaluateCalls)
	}
}

func TestEvaluationHTTPApplicationRejectsReevaluationBeforeMutation(
	t *testing.T,
) {
	t.Parallel()
	notShadow := evaluationTestValue(evaluation.StatusReady)
	notShadow.SceneType = evaluation.SceneIELTSSpeaking
	service := &evaluationServiceStub{getResult: notShadow}
	application := &evaluationHTTPApplication{
		evaluations:   service,
		runtime:       &evaluationRuntimeReaderStub{},
		configuration: evaluationTestRuntimeConfiguration(t),
	}
	_, err := application.Reevaluate(
		context.Background(),
		requestcontext.Actor{
			UserID: "00000000-0000-4000-8000-000000000001",
		},
		notShadow.ID,
		evaluation.ReevaluateRequest{
			Channels:         []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef: evaluation.InterviewShadowStrategyRef,
			PipelineVersion:  evaluation.InterviewShadowPipelineVersion,
		},
	)
	appError, ok := apperror.From(err)
	if !ok || appError.Code() != "evaluation_strategy_not_available" {
		t.Fatalf("error = %v", err)
	}
	if service.reevaluateCalls != 0 {
		t.Fatalf("re-evaluate mutations = %d", service.reevaluateCalls)
	}
}

func TestEvaluationHTTPApplicationGetsOwnerScopedInterviewReport(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	reader := &interviewReportReaderStub{
		result: evaluation.InterviewReportReadState{
			Evaluation: evaluationTestValue(evaluation.StatusQueued),
			Runtime: evaluation.InterviewShadowReadState{
				ModuleStatus: evaluation.InterviewShadowRuntimePending,
			},
		},
	}
	application := &evaluationHTTPApplication{
		interviewReports: reader,
		configuration:    evaluationTestRuntimeConfiguration(t),
	}
	resource, err := application.GetInterviewReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_demo_001",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.ownerUserID != actor.UserID ||
		reader.practiceSessionID != "session_demo_001" ||
		resource.PracticeSessionID != "session_demo_001" ||
		resource.EvaluationStatus != evaluation.StatusQueued ||
		resource.Report != nil ||
		resource.StableFailure != nil ||
		resource.IsFinal {
		t.Fatalf("reader=%#v resource=%#v", reader, resource)
	}
}

func TestEvaluationHTTPApplicationMapsAmbiguousInterviewReportToConflict(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	application := &evaluationHTTPApplication{
		interviewReports: &interviewReportReaderStub{
			err: evaluation.ErrInterviewShadowConfigurationConflict,
		},
		configuration: evaluationTestRuntimeConfiguration(t),
	}
	_, err := application.GetInterviewReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_demo_001",
	)
	appError, ok := apperror.From(err)
	if !ok || appError.Code() != "evaluation_version_conflict" {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluationHTTPApplicationGetsOwnerScopedIELTSSpeakingReport(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	reader := &ieltsSpeakingReportReaderStub{
		result: evaluation.IELTSSpeakingReportReadState{
			Evaluation: evaluationTestIELTSValue(
				evaluation.StatusQueued,
			),
			Runtime: evaluation.IELTSSpeakingShadowReadState{
				ModuleStatus: evaluation.
					IELTSSpeakingShadowRuntimePending,
			},
		},
	}
	application := &evaluationHTTPApplication{
		ieltsReports:       reader,
		ieltsConfiguration: evaluationTestIELTSRuntimeConfiguration(t),
	}
	resource, err := application.GetIELTSSpeakingReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_ielts_001",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.ownerUserID != actor.UserID ||
		reader.practiceSessionID != "session_ielts_001" ||
		resource.PracticeSessionID != "session_ielts_001" ||
		resource.EvaluationStatus != evaluation.StatusQueued ||
		resource.Report != nil ||
		resource.StableFailure != nil ||
		resource.IsFinal {
		t.Fatalf("reader=%#v resource=%#v", reader, resource)
	}
}

func TestEvaluationHTTPApplicationListsOwnerScopedIELTSSpeakingReports(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	value := evaluationTestIELTSValue(evaluation.StatusReady)
	reader := &ieltsSpeakingReportReaderStub{
		page: evaluation.IELTSSpeakingReportIndexPage{
			Items: []evaluation.IELTSSpeakingReportIndexEntry{{
				PracticeSessionID:    value.PracticeSessionID,
				EvaluationID:         value.ID,
				EvaluationRevisionID: value.Revision.ID,
				Revision:             value.Revision.Number,
				EvaluationStatus:     value.Revision.Status,
				IsFinal:              value.Revision.IsFinal,
				CreatedAt:            value.CreatedAt,
				UpdatedAt:            value.Revision.UpdatedAt,
			}},
			HasMore: true,
		},
	}
	application := &evaluationHTTPApplication{
		ieltsReports:       reader,
		ieltsConfiguration: evaluationTestIELTSRuntimeConfiguration(t),
	}
	boundary := &evaluationtransport.IELTSSpeakingReportIndexBoundary{
		UpdatedAt:    value.Revision.UpdatedAt.Add(time.Minute),
		EvaluationID: "30000000-0000-4000-8000-000000000001",
	}
	page, err := application.ListIELTSSpeakingReports(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		evaluationtransport.IELTSSpeakingReportIndexQuery{
			Limit:  1,
			Before: boundary,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.ownerUserID != actor.UserID ||
		reader.limit != 1 ||
		reader.boundary == nil ||
		!reader.boundary.UpdatedAt.Equal(boundary.UpdatedAt) ||
		reader.boundary.EvaluationID != boundary.EvaluationID ||
		len(page.Items) != 1 ||
		!page.HasMore ||
		page.Items[0].PracticeSessionID != value.PracticeSessionID {
		t.Fatalf("reader=%#v page=%#v", reader, page)
	}
}

func TestInterviewShadowFailureDerivesStableRetryability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code          string
		reason        evaluationtransport.ReasonCode
		wantRetryable bool
	}{
		{
			code:   "provider_invalid_response",
			reason: evaluationtransport.ReasonPolicyViolation,
		},
		{
			code:   "evidence_ref_invalid",
			reason: evaluationtransport.ReasonEvidenceRefInvalid,
		},
		{
			code:   "version_conflict",
			reason: evaluationtransport.ReasonVersionConflict,
		},
		{
			code:   "runtime_configuration_changed",
			reason: evaluationtransport.ReasonVersionConflict,
		},
		{
			code:          "provider_canceled",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "provider_timeout",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "rate_limited",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "timeout",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "provider_unavailable",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "invalid_response",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "cancelled",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "dependency_error",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "attempts_exhausted",
			reason:        evaluationtransport.ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:   "authentication",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "authorization",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "configuration",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "invalid_request",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "quota_exhausted",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "provider_error",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
		{
			code:   "unknown_failure",
			reason: evaluationtransport.ReasonInternalNonRetryable,
		},
	}
	for _, test := range tests {
		failure := interviewShadowFailure(test.code)
		if failure.ReasonCode != test.reason ||
			failure.Retryable != test.wantRetryable {
			t.Errorf("%q failure = %#v", test.code, failure)
		}
	}
	failed := evaluationTestValue(evaluation.StatusFailed)
	resource, err := interviewReportResource(
		failed.PracticeSessionID,
		evaluation.InterviewReportReadState{
			Evaluation: failed,
			Runtime: evaluation.InterviewShadowReadState{
				ModuleStatus: evaluation.InterviewShadowRuntimeFailed,
				Failure: &evaluation.InterviewShadowFailure{
					Code: "provider_timeout",
				},
			},
		},
	)
	if err != nil ||
		resource.StableFailure == nil ||
		resource.StableFailure.ReasonCode !=
			evaluationtransport.ReasonInternalRetryable ||
		!resource.StableFailure.Retryable {
		t.Fatalf("failed report resource = %#v, %v", resource, err)
	}
	genericResource, err := interviewShadowResource(
		failed,
		evaluation.InterviewShadowReadState{
			ModuleStatus: evaluation.InterviewShadowRuntimeFailed,
			Failure: &evaluation.InterviewShadowFailure{
				Code:      "authentication",
				Retryable: true,
			},
		},
		evaluationTestRuntimeConfiguration(t),
	)
	if err != nil ||
		genericResource.StableFailure == nil ||
		genericResource.StableFailure.ReasonCode !=
			evaluationtransport.ReasonInternalNonRetryable ||
		genericResource.StableFailure.Retryable {
		t.Fatalf("generic failed resource = %#v, %v", genericResource, err)
	}
	ieltsFailed := evaluationTestIELTSValue(evaluation.StatusFailed)
	ieltsResource, err := ieltsSpeakingReportResource(
		ieltsFailed.PracticeSessionID,
		evaluation.IELTSSpeakingReportReadState{
			Evaluation: ieltsFailed,
			Runtime: evaluation.IELTSSpeakingShadowReadState{
				ModuleStatus: evaluation.
					IELTSSpeakingShadowRuntimeFailed,
				Failure: &evaluation.IELTSSpeakingShadowFailure{
					Code:      "authentication",
					Retryable: true,
				},
			},
		},
	)
	if err != nil ||
		ieltsResource.StableFailure == nil ||
		ieltsResource.StableFailure.ReasonCode !=
			evaluationtransport.ReasonInternalNonRetryable ||
		ieltsResource.StableFailure.Retryable {
		t.Fatalf(
			"IELTS failed report resource = %#v, %v",
			ieltsResource,
			err,
		)
	}
}

func evaluationTestRuntimeConfiguration(
	t *testing.T,
) evaluation.InterviewShadowRuntimeConfiguration {
	t.Helper()
	value, err := interviewShadowRuntimeConfiguration(
		EvaluationConfiguration{
			Provider:        "qianwen",
			Model:           "qwen-plus",
			MaxOutputTokens: 2048,
			LeaseDuration:   30 * time.Second,
			MaxAttempts:     3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func evaluationTestIELTSRuntimeConfiguration(
	t *testing.T,
) evaluation.IELTSSpeakingShadowRuntimeConfiguration {
	t.Helper()
	value, err := ieltsSpeakingShadowRuntimeConfiguration(
		EvaluationConfiguration{
			Provider:        "qianwen",
			Model:           "qwen-plus",
			MaxOutputTokens: 2048,
			LeaseDuration:   30 * time.Second,
			MaxAttempts:     3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func evaluationTestInterviewShadowResult() evaluation.InterviewShadowResult {
	dimensions := make(
		[]evaluation.InterviewShadowDimensionResult,
		0,
		5,
	)
	for _, dimensionID := range evaluation.InterviewDimensions() {
		dimension := evaluation.InterviewShadowDimensionResult{
			DimensionID:  dimensionID,
			Scoreability: evaluation.InterviewScoreabilityProvisional,
			Gate:         evaluation.InterviewGateFeedbackOnly,
			Coverage:     1,
			Confidence:   0,
			ReasonCodes: []evaluation.InterviewReasonCode{
				evaluation.InterviewReasonASRConfidenceUnavailable,
			},
			EvidenceRefIDs:         []string{},
			Strengths:              []evaluation.InterviewShadowFinding{},
			Improvements:           []evaluation.InterviewShadowFinding{},
			RecommendedExpressions: []evaluation.InterviewShadowFinding{},
		}
		if dimensionID == evaluation.InterviewDimensionInteraction {
			dimension.Scoreability =
				evaluation.InterviewScoreabilityInsufficient
			dimension.Gate = evaluation.InterviewGateBlocked
			dimension.Coverage = 0
			dimension.ReasonCodes = []evaluation.InterviewReasonCode{
				evaluation.InterviewReasonOpportunityNotProvided,
			}
		}
		dimensions = append(dimensions, dimension)
	}
	return evaluation.InterviewShadowResult{
		SchemaVersion:   evaluation.InterviewShadowSchemaVersion,
		SnapshotID:      "snapshot_demo_001",
		SceneType:       evaluation.SceneInterview,
		Scope:           evaluation.ScopeSession,
		Channel:         evaluation.ChannelScene,
		Scoreability:    evaluation.InterviewScoreabilityProvisional,
		Gate:            evaluation.InterviewGateFeedbackOnly,
		Readiness:       evaluation.InterviewReadinessNotAssessed,
		ReadinessNotice: evaluation.InterviewShadowReadinessNotice,
		ReasonCodes: []evaluation.InterviewReasonCode{
			evaluation.InterviewReasonASRConfidenceUnavailable,
		},
		Dimensions:      dimensions,
		QuestionResults: []evaluation.InterviewShadowQuestionResult{},
	}
}

func evaluationTestValue(status evaluation.Status) evaluation.Evaluation {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	var completedAt *time.Time
	if status == evaluation.StatusReady || status == evaluation.StatusFailed {
		completedAt = &now
	}
	return evaluation.Evaluation{
		ID:                "10000000-0000-4000-8000-000000000001",
		OwnerUserID:       "00000000-0000-4000-8000-000000000001",
		PracticeSessionID: "session_demo_001",
		InputSnapshotID:   "snapshot_demo_001",
		InputRevision:     1,
		Scope:             evaluation.ScopeSession,
		SceneType:         evaluation.SceneInterview,
		CreatedAt:         now,
		Revision: evaluation.Revision{
			ID:               "20000000-0000-4000-8000-000000000001",
			EvaluationID:     "10000000-0000-4000-8000-000000000001",
			OwnerUserID:      "00000000-0000-4000-8000-000000000001",
			Number:           1,
			Channels:         []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef: evaluation.InterviewShadowStrategyRef,
			PipelineVersion:  evaluation.InterviewShadowPipelineVersion,
			SchemaVersion:    evaluation.SchemaVersion,
			Status:           status,
			CreatedAt:        now,
			UpdatedAt:        now,
			CompletedAt:      completedAt,
		},
	}
}

func evaluationTestIELTSValue(
	status evaluation.Status,
) evaluation.Evaluation {
	value := evaluationTestValue(status)
	value.PracticeSessionID = "session_ielts_001"
	value.SceneType = evaluation.SceneIELTSSpeaking
	value.Revision.SceneStrategyRef =
		evaluation.IELTSSpeakingShadowStrategyRef
	value.Revision.PipelineVersion =
		evaluation.IELTSSpeakingShadowPipelineVersion
	return value
}

type evaluationServiceStub struct {
	createResult       evaluation.Evaluation
	createReplayed     bool
	createError        error
	getResult          evaluation.Evaluation
	getError           error
	reevaluateResult   evaluation.Evaluation
	reevaluateReplayed bool
	reevaluateError    error
	reevaluateCalls    int
}

func (stub *evaluationServiceStub) Create(
	context.Context,
	requestcontext.Actor,
	evaluation.CreateRequest,
) (evaluation.Evaluation, bool, error) {
	return stub.createResult, stub.createReplayed, stub.createError
}

func (stub *evaluationServiceStub) Get(
	context.Context,
	requestcontext.Actor,
	string,
) (evaluation.Evaluation, error) {
	return stub.getResult, stub.getError
}

func (stub *evaluationServiceStub) Reevaluate(
	context.Context,
	requestcontext.Actor,
	string,
	evaluation.ReevaluateRequest,
) (evaluation.Evaluation, bool, error) {
	stub.reevaluateCalls++
	return stub.reevaluateResult,
		stub.reevaluateReplayed,
		stub.reevaluateError
}

type evaluationRuntimeReaderStub struct{}

func (*evaluationRuntimeReaderStub) GetInterviewShadowState(
	context.Context,
	string,
	string,
	string,
) (evaluation.InterviewShadowReadState, error) {
	return evaluation.InterviewShadowReadState{}, evaluation.ErrNotFound
}

type interviewReportReaderStub struct {
	result            evaluation.InterviewReportReadState
	err               error
	ownerUserID       string
	practiceSessionID string
}

func (stub *interviewReportReaderStub) GetCurrentInterviewReportState(
	_ context.Context,
	ownerUserID string,
	practiceSessionID string,
) (evaluation.InterviewReportReadState, error) {
	stub.ownerUserID = ownerUserID
	stub.practiceSessionID = practiceSessionID
	return stub.result, stub.err
}

type ieltsSpeakingReportReaderStub struct {
	result            evaluation.IELTSSpeakingReportReadState
	page              evaluation.IELTSSpeakingReportIndexPage
	err               error
	ownerUserID       string
	practiceSessionID string
	boundary          *evaluation.IELTSSpeakingReportIndexBoundary
	limit             int
}

func (stub *ieltsSpeakingReportReaderStub) GetCurrentIELTSSpeakingReportState(
	_ context.Context,
	ownerUserID string,
	practiceSessionID string,
) (evaluation.IELTSSpeakingReportReadState, error) {
	stub.ownerUserID = ownerUserID
	stub.practiceSessionID = practiceSessionID
	return stub.result, stub.err
}

func (stub *ieltsSpeakingReportReaderStub) ListCurrentIELTSSpeakingReportIndex(
	_ context.Context,
	ownerUserID string,
	boundary *evaluation.IELTSSpeakingReportIndexBoundary,
	limit int,
) (evaluation.IELTSSpeakingReportIndexPage, error) {
	stub.ownerUserID = ownerUserID
	stub.boundary = boundary
	stub.limit = limit
	return stub.page, stub.err
}

var _ evaluationtransport.Application = (*evaluationHTTPApplication)(nil)
