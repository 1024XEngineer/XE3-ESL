package api

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
	evaluationreport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

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
			scoring.InterviewReadinessNotAssessed,
		) {
		t.Fatalf("projection governance = %#v", body)
	}
	dimensions, ok := body["dimensions"].([]any)
	if !ok || len(dimensions) != 5 {
		t.Fatalf("dimensions = %#v", body["dimensions"])
	}
	interaction := dimensions[4].(map[string]any)
	if interaction["scoreability_status"] !=
		string(scoring.InterviewScoreabilityInsufficient) ||
		interaction["gate_status"] !=
			string(scoring.InterviewGateBlocked) {
		t.Fatalf("interaction projection = %#v", interaction)
	}
}

func TestInterviewShadowProjectionRejectsUnmappedReason(t *testing.T) {
	t.Parallel()
	result := evaluationTestInterviewShadowResult()
	result.Dimensions[0].ReasonCodes = []scoring.InterviewReasonCode{
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
	application := &Application{
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
			SceneStrategyRef:  scoring.InterviewShadowStrategyRef,
			PipelineVersion:   scoring.InterviewShadowPipelineVersion,
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
	application := &Application{
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
			SceneStrategyRef:  scoring.InterviewShadowStrategyRef,
			PipelineVersion:   scoring.InterviewShadowPipelineVersion,
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
	application := &Application{
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
			SceneStrategyRef: scoring.InterviewShadowStrategyRef,
			PipelineVersion:  scoring.InterviewShadowPipelineVersion,
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

func TestEvaluationHTTPApplicationReevaluatesIELTSSpeakingReport(
	t *testing.T,
) {
	t.Parallel()
	current := evaluationTestIELTSValue(evaluation.StatusFailed)
	queued := evaluationTestIELTSValue(evaluation.StatusQueued)
	queued.Revision.ID = "30000000-0000-4000-8000-000000000001"
	queued.Revision.Number = 2
	queued.Revision.SupersedesRevisionID = current.Revision.ID
	service := &evaluationServiceStub{
		getResult:        current,
		reevaluateResult: queued,
	}
	application := &Application{
		evaluations:        service,
		ieltsConfiguration: evaluationTestIELTSRuntimeConfiguration(t),
	}
	accepted, err := application.Reevaluate(
		context.Background(),
		requestcontext.Actor{
			UserID: "00000000-0000-4000-8000-000000000001",
		},
		current.ID,
		evaluation.ReevaluateRequest{
			Channels: []evaluation.Channel{
				evaluation.ChannelScene,
			},
			SceneStrategyRef: scoring.IELTSSpeakingShadowStrategyRef,
			PipelineVersion: scoring.
				IELTSSpeakingShadowPipelineVersion,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if service.reevaluateCalls != 1 || accepted.Replayed ||
		accepted.EvaluationRevisionID != queued.Revision.ID ||
		accepted.SupersedesRevisionID != current.Revision.ID ||
		accepted.EvaluationStatus != evaluation.StatusQueued {
		t.Fatalf("accepted = %#v, calls = %d", accepted, service.reevaluateCalls)
	}
}

func TestIELTSSpeakingShadowAcceptedAllowsValidatingFreshAndReplay(
	t *testing.T,
) {
	t.Parallel()
	value := evaluationTestIELTSValue(evaluation.StatusValidating)
	for _, replayed := range []bool{false, true} {
		accepted, err := ieltsSpeakingShadowAccepted(value, replayed)
		if err != nil || accepted.EvaluationStatus != evaluation.StatusValidating ||
			accepted.Replayed != replayed {
			t.Fatalf(
				"replayed=%t accepted=%#v err=%v",
				replayed,
				accepted,
				err,
			)
		}
	}
}

func TestEvaluationHTTPApplicationRejectsReevaluationBeforeMutation(
	t *testing.T,
) {
	t.Parallel()
	notShadow := evaluationTestValue(evaluation.StatusReady)
	notShadow.SceneType = evaluation.SceneIELTSSpeaking
	service := &evaluationServiceStub{getResult: notShadow}
	application := &Application{
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
			SceneStrategyRef: scoring.InterviewShadowStrategyRef,
			PipelineVersion:  scoring.InterviewShadowPipelineVersion,
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
		result: evaluationreport.InterviewReadState{
			Evaluation: evaluationTestValue(evaluation.StatusQueued),
			Runtime: scoring.InterviewShadowReadState{
				ModuleStatus: scoring.InterviewShadowRuntimePending,
			},
		},
	}
	application := &Application{
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
	application := &Application{
		interviewReports: &interviewReportReaderStub{
			err: evaluationreport.ErrInterviewConfigurationConflict,
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
		result: evaluationreport.IELTSSpeakingReadState{
			Evaluation: evaluationTestIELTSValue(
				evaluation.StatusQueued,
			),
			Runtime: scoring.IELTSSpeakingShadowReadState{
				ModuleStatus: scoring.IELTSSpeakingShadowRuntimePending,
			},
		},
	}
	application := &Application{
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

func TestEvaluationHTTPApplicationProjectsValidatingIELTSSpeakingReport(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	application := &Application{
		ieltsReports: &ieltsSpeakingReportReaderStub{
			result: evaluationreport.IELTSSpeakingReadState{
				Evaluation: evaluationTestIELTSValue(
					evaluation.StatusValidating,
				),
				Runtime: scoring.IELTSSpeakingShadowReadState{
					ModuleStatus: scoring.IELTSSpeakingShadowRuntimePending,
				},
			},
		},
		ieltsConfiguration: evaluationTestIELTSRuntimeConfiguration(t),
	}
	resource, err := application.GetIELTSSpeakingReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_ielts_001",
	)
	if err != nil || resource.EvaluationStatus != evaluation.StatusValidating ||
		resource.Report != nil || resource.StableFailure != nil {
		t.Fatalf("validating resource=%#v err=%v", resource, err)
	}
}

func TestEvaluationHTTPApplicationGetsOwnerScopedQueuedSessionReport(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	reader := &sessionReportReaderStub{
		result: evaluationreport.SessionReportReadState{
			PracticeMode:      "PART_2",
			AvailableSections: []string{"PART_2", "PART_3"},
			Status:            evaluation.StatusQueued,
		},
	}
	application := &Application{sessionReports: reader}
	resource, err := application.GetSessionReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_ielts_001",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.ownerUserID != actor.UserID ||
		reader.practiceSessionID != "session_ielts_001" ||
		resource.PracticeMode != "PART_2" ||
		resource.ReportScope != "PART_2_3" ||
		resource.DetailSchema != ieltsSpeakingPracticeReportSchemaVersion ||
		resource.EvaluationStatus != evaluation.StatusQueued ||
		len(resource.AvailableSections) != 2 {
		t.Fatalf("reader=%#v resource=%#v", reader, resource)
	}
}

func TestEvaluationHTTPApplicationProjectsValidatingFullMockSessionReport(
	t *testing.T,
) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	value := evaluationTestIELTSValue(evaluation.StatusValidating)
	application := &Application{
		sessionReports: &sessionReportReaderStub{
			result: evaluationreport.SessionReportReadState{
				PracticeMode: "FULL_MOCK",
				AvailableSections: []string{
					"PART_1", "PART_2", "PART_3",
				},
				Status:     evaluation.StatusValidating,
				Evaluation: &value,
			},
		},
	}
	resource, err := application.GetSessionReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_ielts_001",
	)
	if err != nil || resource.EvaluationStatus != evaluation.StatusValidating ||
		resource.ReportID != "" || resource.StableFailure != nil {
		t.Fatalf("validating resource=%#v err=%v", resource, err)
	}
}

func TestSessionReportResourceReadsSupportedReadyPracticeSchemas(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name         string
		detailSchema string
		legacy       bool
		wantError    error
	}{
		{
			name:         "dedicated IELTS practice report",
			detailSchema: ieltsSpeakingPracticeReportSchemaVersion,
		},
		{
			name:         "legacy general scene report",
			detailSchema: legacyIELTSSpeakingPracticeReportSchemaVersion,
			legacy:       true,
		},
		{
			name:         "legacy strategy with dedicated practice report",
			detailSchema: ieltsSpeakingPracticeReportSchemaVersion,
			legacy:       true,
		},
		{
			name:         "unknown report",
			detailSchema: "unknown-report/v1",
			wantError:    evaluation.ErrInvalidRequest,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ownerUserID, state := readyPart1SessionReportState(
				test.detailSchema,
			)
			if test.legacy {
				state.Evaluation.Revision.SceneStrategyRef =
					scoring.GeneralSceneStrategyRef
				state.Evaluation.Revision.PipelineVersion =
					scoring.GeneralScenePipelineVersion
			}
			resource, err := sessionReportResource(
				"session_ielts_001",
				ownerUserID,
				state,
			)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if test.wantError != nil {
				return
			}
			if resource.DetailSchema != test.detailSchema ||
				resource.EvaluationStatus != evaluation.StatusReady ||
				resource.ReportID != "30000000-0000-4000-8000-000000000001" {
				t.Fatalf("resource = %#v", resource)
			}
		})
	}
}

func readyPart1SessionReportState(
	detailSchema string,
) (string, evaluationreport.SessionReportReadState) {
	value := evaluationTestIELTSValue(evaluation.StatusReady)
	stored := evaluationreport.StoredFormalReport{
		ReportID:             "30000000-0000-4000-8000-000000000001",
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		OwnerUserID:          value.OwnerUserID,
		PracticeSessionID:    value.PracticeSessionID,
		Revision:             value.Revision.Number,
		CreatedAt:            value.CreatedAt,
		Report: evaluationreport.FormalReport{
			SchemaVersion:      evaluationreport.FormalReportSchemaVersion,
			SceneType:          evaluation.SceneIELTSSpeaking,
			PracticeExperience: "IELTS_SPEAKING",
			SceneCategory:      "IELTS_SPEAKING",
			PracticeMode:       "PART_1",
			ScoreabilityStatus: evaluationreport.ReportScoreabilityProvisional,
			Summary:            "本次专项练习已生成报告。",
			Dimensions: []evaluationreport.ReportDimension{
				{
					Key:          "IELTS_FC",
					Scale:        evaluationreport.ReportScaleIELTSBand,
					Coverage:     1,
					Confidence:   1,
					ReasonCodes:  []string{},
					EvidenceRefs: []string{},
					Strengths:    []evaluationreport.ReportFinding{},
					Improvements: []evaluationreport.ReportFinding{},
					Examples:     []evaluationreport.ReportFinding{},
				},
			},
			PriorityActions: []evaluationreport.ReportPriorityAction{},
			DetailSchema:    detailSchema,
			Detail:          json.RawMessage(`{"schema_version":"` + detailSchema + `"}`),
		},
	}
	return value.OwnerUserID, evaluationreport.SessionReportReadState{
		PracticeMode:      "PART_1",
		AvailableSections: []string{"PART_1"},
		Status:            evaluation.StatusReady,
		Evaluation:        &value,
		FormalReport:      &stored,
	}
}

func TestEvaluationHTTPApplicationMapsSessionReportConflict(t *testing.T) {
	t.Parallel()
	actor := requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "session-authenticated",
	}
	application := &Application{
		sessionReports: &sessionReportReaderStub{
			err: evaluationreport.ErrSessionReportConfigurationConflict,
		},
	}
	_, err := application.GetSessionReport(
		requestcontext.WithActor(context.Background(), actor),
		actor,
		"session_ielts_001",
	)
	appError, ok := apperror.From(err)
	if !ok || appError.Code() != "evaluation_version_conflict" {
		t.Fatalf("error = %v", err)
	}
}

func TestInterviewShadowFailureDerivesStableRetryability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code          string
		reason        ReasonCode
		wantRetryable bool
	}{
		{
			code:   "provider_invalid_json",
			reason: ReasonPolicyViolation,
		},
		{
			code:   "provider_schema_mismatch",
			reason: ReasonPolicyViolation,
		},
		{
			code:   "provider_invalid_response",
			reason: ReasonPolicyViolation,
		},
		{
			code:   "evidence_ref_invalid",
			reason: ReasonEvidenceRefInvalid,
		},
		{
			code:   "version_conflict",
			reason: ReasonVersionConflict,
		},
		{
			code:          "runtime_configuration_changed",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "provider_canceled",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "provider_timeout",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "rate_limited",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "timeout",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "provider_unavailable",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "invalid_response",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "cancelled",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "dependency_error",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:          "attempts_exhausted",
			reason:        ReasonInternalRetryable,
			wantRetryable: true,
		},
		{
			code:   "authentication",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "authorization",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "configuration",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "invalid_request",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "quota_exhausted",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "provider_error",
			reason: ReasonInternalNonRetryable,
		},
		{
			code:   "unknown_failure",
			reason: ReasonInternalNonRetryable,
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
		evaluationreport.InterviewReadState{
			Evaluation: failed,
			Runtime: scoring.InterviewShadowReadState{
				ModuleStatus: scoring.InterviewShadowRuntimeFailed,
				Failure: &scoring.InterviewShadowFailure{
					Code: "provider_timeout",
				},
			},
		},
	)
	if err != nil ||
		resource.StableFailure == nil ||
		resource.StableFailure.ReasonCode !=
			ReasonInternalRetryable ||
		!resource.StableFailure.Retryable {
		t.Fatalf("failed report resource = %#v, %v", resource, err)
	}
	genericResource, err := interviewShadowResource(
		failed,
		scoring.InterviewShadowReadState{
			ModuleStatus: scoring.InterviewShadowRuntimeFailed,
			Failure: &scoring.InterviewShadowFailure{
				Code:      "authentication",
				Retryable: true,
			},
		},
		evaluationTestRuntimeConfiguration(t),
	)
	if err != nil ||
		genericResource.StableFailure == nil ||
		genericResource.StableFailure.ReasonCode !=
			ReasonInternalNonRetryable ||
		genericResource.StableFailure.Retryable {
		t.Fatalf("generic failed resource = %#v, %v", genericResource, err)
	}
	ieltsFailed := evaluationTestIELTSValue(evaluation.StatusFailed)
	ieltsResource, err := ieltsSpeakingReportResource(
		ieltsFailed.PracticeSessionID,
		evaluationreport.IELTSSpeakingReadState{
			Evaluation: ieltsFailed,
			Runtime: scoring.IELTSSpeakingShadowReadState{
				ModuleStatus: scoring.IELTSSpeakingShadowRuntimeFailed,
				Failure: &scoring.IELTSSpeakingShadowFailure{
					Code:      "provider_timeout",
					Retryable: true,
				},
			},
		},
	)
	if err != nil ||
		ieltsResource.StableFailure == nil ||
		ieltsResource.StableFailure.ReasonCode !=
			ReasonInternalRetryable ||
		!ieltsResource.StableFailure.Retryable {
		t.Fatalf(
			"IELTS failed report resource = %#v, %v",
			ieltsResource,
			err,
		)
	}
	sessionResource, err := sessionReportResource(
		"session_ielts_001",
		ieltsFailed.OwnerUserID,
		evaluationreport.SessionReportReadState{
			PracticeMode:      "PART_1",
			AvailableSections: []string{"PART_1"},
			Status:            evaluation.StatusFailed,
			Failure: &evaluationreport.SessionReportFailure{
				Code: "provider_timeout",
			},
		},
	)
	if err != nil || sessionResource.StableFailure == nil ||
		sessionResource.StableFailure.ReasonCode != ReasonInternalRetryable ||
		!sessionResource.StableFailure.Retryable {
		t.Fatalf(
			"IELTS Session failed report resource = %#v, %v",
			sessionResource,
			err,
		)
	}
	providerFailed := evaluationTestIELTSValue(evaluation.StatusFailed)
	providerResource, err := sessionReportResource(
		providerFailed.PracticeSessionID,
		providerFailed.OwnerUserID,
		evaluationreport.SessionReportReadState{
			PracticeMode:      "PART_1",
			AvailableSections: []string{"PART_1"},
			Status:            evaluation.StatusFailed,
			Evaluation:        &providerFailed,
			Failure: &evaluationreport.SessionReportFailure{
				Code: "provider_invalid_response",
			},
		},
	)
	if err != nil || providerResource.StableFailure == nil ||
		providerResource.StableFailure.ReasonCode != ReasonInternalRetryable ||
		!providerResource.StableFailure.Retryable {
		t.Fatalf(
			"provider Session failure resource = %#v, %v",
			providerResource,
			err,
		)
	}
}

func evaluationTestRuntimeConfiguration(
	t *testing.T,
) scoring.InterviewShadowRuntimeConfiguration {
	t.Helper()
	return scoring.InterviewShadowRuntimeConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   30 * time.Second,
		StrategyRef:     scoring.InterviewShadowStrategyRef,
		PipelineVersion: scoring.InterviewShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256([]byte("interview-config")),
		PromptVersion:   scoring.InterviewShadowPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
}

func evaluationTestIELTSRuntimeConfiguration(
	t *testing.T,
) scoring.IELTSSpeakingShadowRuntimeConfiguration {
	t.Helper()
	return scoring.IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:          3,
		LeaseDuration:        30 * time.Second,
		AcousticWaitDuration: 15 * time.Second,
		StrategyRef:          scoring.IELTSSpeakingShadowStrategyRef,
		PipelineVersion:      scoring.IELTSSpeakingShadowPipelineVersion,
		FullConfigHash:       sha256.Sum256([]byte("ielts-config")),
		PromptVersion:        scoring.IELTSSpeakingShadowPromptVersion,
		Provider:             "qianwen",
		Model:                "qwen-plus",
	}
}

func evaluationTestInterviewShadowResult() scoring.InterviewShadowResult {
	dimensions := make(
		[]scoring.InterviewShadowDimensionResult,
		0,
		5,
	)
	for _, dimensionID := range scoring.InterviewDimensions() {
		dimension := scoring.InterviewShadowDimensionResult{
			DimensionID:  dimensionID,
			Scoreability: scoring.InterviewScoreabilityProvisional,
			Gate:         scoring.InterviewGateFeedbackOnly,
			Coverage:     1,
			Confidence:   0,
			ReasonCodes: []scoring.InterviewReasonCode{
				scoring.InterviewReasonASRConfidenceUnavailable,
			},
			EvidenceRefIDs:         []string{},
			Strengths:              []scoring.InterviewShadowFinding{},
			Improvements:           []scoring.InterviewShadowFinding{},
			RecommendedExpressions: []scoring.InterviewShadowFinding{},
		}
		if dimensionID == scoring.InterviewDimensionInteraction {
			dimension.Scoreability =
				scoring.InterviewScoreabilityInsufficient
			dimension.Gate = scoring.InterviewGateBlocked
			dimension.Coverage = 0
			dimension.ReasonCodes = []scoring.InterviewReasonCode{
				scoring.InterviewReasonOpportunityNotProvided,
			}
		}
		dimensions = append(dimensions, dimension)
	}
	return scoring.InterviewShadowResult{
		SchemaVersion:   scoring.InterviewShadowSchemaVersion,
		SnapshotID:      "snapshot_demo_001",
		SceneType:       evaluation.SceneInterview,
		Scope:           evaluation.ScopeSession,
		Channel:         evaluation.ChannelScene,
		Scoreability:    scoring.InterviewScoreabilityProvisional,
		Gate:            scoring.InterviewGateFeedbackOnly,
		Readiness:       scoring.InterviewReadinessNotAssessed,
		ReadinessNotice: scoring.InterviewShadowReadinessNotice,
		ReasonCodes: []scoring.InterviewReasonCode{
			scoring.InterviewReasonASRConfidenceUnavailable,
		},
		Dimensions:      dimensions,
		QuestionResults: []scoring.InterviewShadowQuestionResult{},
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
			SceneStrategyRef: scoring.InterviewShadowStrategyRef,
			PipelineVersion:  scoring.InterviewShadowPipelineVersion,
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
		scoring.IELTSSpeakingShadowStrategyRef
	value.Revision.PipelineVersion =
		scoring.IELTSSpeakingShadowPipelineVersion
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
) (scoring.InterviewShadowReadState, error) {
	return scoring.InterviewShadowReadState{}, evaluation.ErrNotFound
}

type interviewReportReaderStub struct {
	result            evaluationreport.InterviewReadState
	err               error
	ownerUserID       string
	practiceSessionID string
}

func (stub *interviewReportReaderStub) GetCurrentInterviewReportState(
	_ context.Context,
	ownerUserID string,
	practiceSessionID string,
) (evaluationreport.InterviewReadState, error) {
	stub.ownerUserID = ownerUserID
	stub.practiceSessionID = practiceSessionID
	return stub.result, stub.err
}

type ieltsSpeakingReportReaderStub struct {
	result            evaluationreport.IELTSSpeakingReadState
	err               error
	ownerUserID       string
	practiceSessionID string
}

type sessionReportReaderStub struct {
	result            evaluationreport.SessionReportReadState
	err               error
	ownerUserID       string
	practiceSessionID string
}

func (stub *sessionReportReaderStub) GetCurrentSessionReportState(
	_ context.Context,
	ownerUserID string,
	practiceSessionID string,
) (evaluationreport.SessionReportReadState, error) {
	stub.ownerUserID = ownerUserID
	stub.practiceSessionID = practiceSessionID
	return stub.result, stub.err
}

func (stub *ieltsSpeakingReportReaderStub) GetCurrentIELTSSpeakingReportState(
	_ context.Context,
	ownerUserID string,
	practiceSessionID string,
) (evaluationreport.IELTSSpeakingReadState, error) {
	stub.ownerUserID = ownerUserID
	stub.practiceSessionID = practiceSessionID
	return stub.result, stub.err
}

var _ HTTPApplication = (*Application)(nil)
