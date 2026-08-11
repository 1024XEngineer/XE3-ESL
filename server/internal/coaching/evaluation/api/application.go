package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationreport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type evaluationService interface {
	Create(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (evaluation.Evaluation, bool, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (evaluation.Evaluation, error)
	Reevaluate(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.ReevaluateRequest,
	) (evaluation.Evaluation, bool, error)
}

type evaluationRuntimeReader interface {
	GetInterviewShadowState(
		context.Context,
		string,
		string,
		string,
	) (scoring.InterviewShadowReadState, error)
}

type interviewReportReader interface {
	GetCurrentInterviewReportState(
		context.Context,
		string,
		string,
	) (evaluationreport.InterviewReadState, error)
}

type ieltsSpeakingReportReader interface {
	GetCurrentIELTSSpeakingReportState(
		context.Context,
		string,
		string,
	) (evaluationreport.IELTSSpeakingReadState, error)
}

type sessionReportReader interface {
	GetCurrentSessionReportState(
		context.Context,
		string,
		string,
	) (evaluationreport.SessionReportReadState, error)
}

// Identifies the detail shape emitted by IELTS practice FormalReports.
const ieltsSpeakingPracticeReportSchemaVersion = "ielts-speaking-practice-report/v1"

// Identifies IELTS practice FormalReports emitted before the dedicated
// practice-report projection is deployed.
const legacyIELTSSpeakingPracticeReportSchemaVersion = scoring.GeneralSceneSchemaVersion

type Application struct {
	evaluations        evaluationService
	runtime            evaluationRuntimeReader
	interviewReports   interviewReportReader
	ieltsReports       ieltsSpeakingReportReader
	sessionReports     sessionReportReader
	configuration      scoring.InterviewShadowRuntimeConfiguration
	ieltsConfiguration scoring.IELTSSpeakingShadowRuntimeConfiguration
}

func NewApplication(
	evaluations evaluationService,
	runtime evaluationRuntimeReader,
	interviewReports interviewReportReader,
	ieltsReports ieltsSpeakingReportReader,
	sessionReports sessionReportReader,
	configuration scoring.InterviewShadowRuntimeConfiguration,
	ieltsConfiguration scoring.IELTSSpeakingShadowRuntimeConfiguration,
) (*Application, error) {
	if evaluations == nil || runtime == nil || interviewReports == nil ||
		ieltsReports == nil || sessionReports == nil || !configuration.Valid() ||
		!ieltsConfiguration.Valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Application{
		evaluations:        evaluations,
		runtime:            runtime,
		interviewReports:   interviewReports,
		ieltsReports:       ieltsReports,
		sessionReports:     sessionReports,
		configuration:      configuration,
		ieltsConfiguration: ieltsConfiguration,
	}, nil
}

func (application *Application) GetSessionReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (SessionReportResource, error) {
	if application == nil || application.sessionReports == nil ||
		ctx == nil || !actor.Valid() {
		return SessionReportResource{}, evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return SessionReportResource{}, evaluation.ErrInvalidRequest
	}
	state, err := application.sessionReports.GetCurrentSessionReportState(
		ctx,
		actor.UserID,
		practiceSessionID,
	)
	if err != nil {
		if errors.Is(err, evaluationreport.ErrSessionReportConfigurationConflict) {
			return SessionReportResource{}, interviewShadowVersionConflictError()
		}
		return SessionReportResource{}, err
	}
	return sessionReportResource(practiceSessionID, actor.UserID, state)
}

func (application *Application) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	request evaluation.CreateRequest,
) (EvaluationAccepted, error) {
	if application == nil || application.evaluations == nil ||
		!application.configuration.Valid() {
		return EvaluationAccepted{},
			evaluation.ErrInvalidRequest
	}
	if err := scoring.ValidateInterviewShadowCreateRequest(
		request,
	); err != nil {
		return EvaluationAccepted{},
			interviewShadowStrategyError()
	}
	value, replayed, err := application.evaluations.Create(ctx, actor, request)
	if err != nil {
		return EvaluationAccepted{}, err
	}
	return interviewShadowAccepted(value, replayed)
}

func (application *Application) Reevaluate(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
	request evaluation.ReevaluateRequest,
) (EvaluationAccepted, error) {
	if application == nil || application.evaluations == nil {
		return EvaluationAccepted{},
			evaluation.ErrInvalidRequest
	}
	current, err := application.evaluations.Get(
		ctx,
		actor,
		evaluationID,
	)
	if err != nil {
		return EvaluationAccepted{}, err
	}
	var accept func(
		evaluation.Evaluation,
		bool,
	) (EvaluationAccepted, error)
	switch {
	case interviewShadowEvaluation(current):
		if !application.configuration.Valid() ||
			scoring.ValidateInterviewShadowReevaluateRequest(request) != nil {
			return EvaluationAccepted{},
				interviewShadowStrategyError()
		}
		accept = interviewShadowAccepted
	case ieltsSpeakingShadowEvaluation(current):
		if !application.ieltsConfiguration.Valid() ||
			scoring.ValidateIELTSSpeakingShadowReevaluateRequest(request) != nil {
			return EvaluationAccepted{},
				interviewShadowStrategyError()
		}
		accept = ieltsSpeakingShadowAccepted
	default:
		return EvaluationAccepted{},
			interviewShadowStrategyError()
	}
	value, replayed, err := application.evaluations.Reevaluate(
		ctx,
		actor,
		evaluationID,
		request,
	)
	if err != nil {
		return EvaluationAccepted{}, err
	}
	return accept(value, replayed)
}

func (application *Application) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
) (EvaluationResource, error) {
	if application == nil || application.evaluations == nil ||
		application.runtime == nil ||
		!application.configuration.Valid() {
		return EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	value, err := application.evaluations.Get(
		ctx,
		actor,
		evaluationID,
	)
	if err != nil {
		return EvaluationResource{}, err
	}
	if !interviewShadowEvaluation(value) {
		return EvaluationResource{},
			evaluation.ErrNotFound
	}
	state, err := application.runtime.GetInterviewShadowState(
		ctx,
		actor.UserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		return EvaluationResource{}, err
	}
	return interviewShadowResource(
		value,
		state,
		application.configuration,
	)
}

func (application *Application) GetInterviewReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (InterviewReportResource, error) {
	if application == nil || application.interviewReports == nil ||
		ctx == nil || !actor.Valid() ||
		!application.configuration.Valid() {
		return InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	state, err := application.interviewReports.GetCurrentInterviewReportState(
		ctx,
		actor.UserID,
		practiceSessionID,
	)
	if err != nil {
		if errors.Is(
			err,
			evaluationreport.ErrInterviewConfigurationConflict,
		) {
			return InterviewReportResource{},
				interviewShadowVersionConflictError()
		}
		return InterviewReportResource{}, err
	}
	return interviewReportResource(
		practiceSessionID,
		state,
	)
}

func (application *Application) GetIELTSSpeakingReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (IELTSSpeakingReportResource, error) {
	if application == nil ||
		application.ieltsReports == nil ||
		ctx == nil ||
		!actor.Valid() ||
		!application.ieltsConfiguration.Valid() {
		return IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	state, err := application.ieltsReports.
		GetCurrentIELTSSpeakingReportState(
			ctx,
			actor.UserID,
			practiceSessionID,
		)
	if err != nil {
		if errors.Is(
			err,
			evaluationreport.ErrIELTSSpeakingConfigurationConflict,
		) {
			return IELTSSpeakingReportResource{},
				interviewShadowVersionConflictError()
		}
		return IELTSSpeakingReportResource{}, err
	}
	return ieltsSpeakingReportResource(practiceSessionID, state)
}

func sessionReportResource(
	practiceSessionID string,
	ownerUserID string,
	state evaluationreport.SessionReportReadState,
) (SessionReportResource, error) {
	reportScope, detailSchema, strategyRef, pipelineVersion, ok :=
		sessionReportAuthority(state.PracticeMode)
	if !ok || practiceSessionID == "" ||
		!validSessionReportShape(
			state.PracticeMode,
			reportScope,
			state.AvailableSections,
			detailSchema,
			state.Status,
		) {
		return SessionReportResource{}, evaluation.ErrInvalidRequest
	}
	resource := SessionReportResource{
		PracticeSessionID: practiceSessionID,
		PracticeMode:      state.PracticeMode,
		ReportScope:       reportScope,
		AvailableSections: append([]string(nil), state.AvailableSections...),
		DetailSchema:      detailSchema,
		EvaluationStatus:  state.Status,
	}
	if state.Evaluation != nil {
		value := *state.Evaluation
		if !value.Valid() || value.OwnerUserID != ownerUserID ||
			value.PracticeSessionID != practiceSessionID ||
			value.Scope != evaluation.ScopeSession ||
			value.SceneType != evaluation.SceneIELTSSpeaking ||
			len(value.Revision.Channels) != 1 ||
			value.Revision.Channels[0] != evaluation.ChannelScene ||
			value.Revision.SceneStrategyRef != strategyRef ||
			value.Revision.Core4DStrategyRef != "" ||
			value.Revision.PipelineVersion != pipelineVersion ||
			value.Revision.Status != state.Status || value.Revision.IsFinal {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
		resource.EvaluationID = value.ID
		resource.EvaluationRevisionID = value.Revision.ID
		resource.Revision = value.Revision.Number
	}
	switch state.Status {
	case evaluation.StatusValidating, evaluation.StatusQueued:
		if state.FormalReport != nil || state.Failure != nil {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if state.Evaluation == nil || state.FormalReport != nil ||
			state.Failure != nil {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.Evaluation == nil || state.FormalReport == nil ||
			state.Failure != nil {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
		stored := *state.FormalReport
		if !stored.Valid() || stored.OwnerUserID != ownerUserID ||
			stored.PracticeSessionID != practiceSessionID ||
			stored.EvaluationID != resource.EvaluationID ||
			stored.EvaluationRevisionID != resource.EvaluationRevisionID ||
			stored.Revision != resource.Revision ||
			stored.Report.SceneType != evaluation.SceneIELTSSpeaking ||
			stored.Report.PracticeMode != state.PracticeMode ||
			!validSessionReportShape(
				state.PracticeMode,
				reportScope,
				state.AvailableSections,
				stored.Report.DetailSchema,
				evaluation.StatusReady,
			) {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
		resource.DetailSchema = stored.Report.DetailSchema
		resource.ReportID = stored.ReportID
		resource.ScoreabilityStatus = stored.Report.ScoreabilityStatus
		resource.Summary = stored.Report.Summary
	case evaluation.StatusFailed:
		if state.FormalReport != nil || state.Failure == nil {
			return SessionReportResource{}, evaluation.ErrInvalidRequest
		}
		failure := sessionReportFailure(*state.Failure)
		resource.StableFailure = &failure
	default:
		return SessionReportResource{}, evaluation.ErrInvalidRequest
	}
	if !resource.valid() {
		return SessionReportResource{}, evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func sessionReportAuthority(mode string) (
	reportScope string,
	detailSchema string,
	strategyRef string,
	pipelineVersion string,
	ok bool,
) {
	switch mode {
	case "PART_1":
		reportScope = "PART_1"
	case "PART_2":
		reportScope = "PART_2_3"
	case "PART_3":
		reportScope = "PART_3"
	case "FULL_MOCK":
		return "FULL_MOCK",
			evaluationreport.IELTSSpeakingReportSchemaVersion,
			scoring.IELTSSpeakingShadowStrategyRef,
			scoring.IELTSSpeakingShadowPipelineVersion,
			true
	default:
		return "", "", "", "", false
	}
	return reportScope,
		ieltsSpeakingPracticeReportSchemaVersion,
		scoring.GeneralSceneStrategyRef,
		scoring.GeneralScenePipelineVersion,
		true
}

func sessionReportFailure(
	failure evaluationreport.SessionReportFailure,
) EvaluationFailure {
	switch failure.Code {
	case "strategy_not_available":
		return EvaluationFailure{ReasonCode: ReasonStrategyNotAvailable}
	case "invalid_completion":
		return EvaluationFailure{ReasonCode: ReasonPolicyViolation}
	case "source_not_found":
		return EvaluationFailure{ReasonCode: ReasonEvidenceRefInvalid}
	case "evaluation_unavailable":
		return EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	}
	if failure.Retryable {
		return EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	}
	return interviewShadowFailure(failure.Code)
}

func interviewShadowAccepted(
	value evaluation.Evaluation,
	replayed bool,
) (EvaluationAccepted, error) {
	if !interviewShadowEvaluation(value) {
		return EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	if replayed {
		switch value.Revision.Status {
		case evaluation.StatusQueued,
			evaluation.StatusRunning,
			evaluation.StatusReady,
			evaluation.StatusFailed:
		default:
			return EvaluationAccepted{},
				interviewShadowVersionConflictError()
		}
	} else if value.Revision.Status != evaluation.StatusQueued {
		return EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	return EvaluationAccepted{
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		SupersedesRevisionID: value.Revision.SupersedesRevisionID,
		EvaluationStatus:     value.Revision.Status,
		Replayed:             replayed,
	}, nil
}

func ieltsSpeakingShadowAccepted(
	value evaluation.Evaluation,
	replayed bool,
) (EvaluationAccepted, error) {
	if !ieltsSpeakingShadowEvaluation(value) {
		return EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	if replayed {
		switch value.Revision.Status {
		case evaluation.StatusValidating,
			evaluation.StatusQueued,
			evaluation.StatusRunning,
			evaluation.StatusReady,
			evaluation.StatusFailed:
		default:
			return EvaluationAccepted{},
				interviewShadowVersionConflictError()
		}
	} else if value.Revision.Status != evaluation.StatusValidating &&
		value.Revision.Status != evaluation.StatusQueued {
		return EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	return EvaluationAccepted{
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		SupersedesRevisionID: value.Revision.SupersedesRevisionID,
		EvaluationStatus:     value.Revision.Status,
		Replayed:             replayed,
	}, nil
}

func interviewShadowEvaluation(value evaluation.Evaluation) bool {
	return value.Valid() &&
		value.Scope == evaluation.ScopeSession &&
		value.SceneType == evaluation.SceneInterview &&
		len(value.Revision.Channels) == 1 &&
		value.Revision.Channels[0] == evaluation.ChannelScene &&
		value.Revision.SceneStrategyRef ==
			scoring.InterviewShadowStrategyRef &&
		value.Revision.Core4DStrategyRef == "" &&
		value.Revision.PipelineVersion ==
			scoring.InterviewShadowPipelineVersion
}

func interviewShadowResource(
	value evaluation.Evaluation,
	state scoring.InterviewShadowReadState,
	configuration scoring.InterviewShadowRuntimeConfiguration,
) (EvaluationResource, error) {
	if !interviewShadowEvaluation(value) || !configuration.Valid() {
		return EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	resource := EvaluationResource{
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		SupersedesRevisionID: value.Revision.SupersedesRevisionID,
		PracticeSessionID:    value.PracticeSessionID,
		InputSnapshotID:      value.InputSnapshotID,
		InputRevision:        value.InputRevision,
		Scope:                value.Scope,
		SceneType:            value.SceneType,
		Channels: append(
			[]evaluation.Channel(nil),
			value.Revision.Channels...,
		),
		SceneStrategyRef: value.Revision.SceneStrategyRef,
		PipelineVersion:  value.Revision.PipelineVersion,
		SchemaVersion:    value.Revision.SchemaVersion,
		EvaluationStatus: value.Revision.Status,
		ModuleStatuses: map[string]ModuleStatus{
			"scene": interviewShadowModuleStatus(state.ModuleStatus),
		},
		IsFinal:     value.Revision.IsFinal,
		CreatedAt:   value.Revision.CreatedAt,
		UpdatedAt:   value.Revision.UpdatedAt,
		CompletedAt: value.Revision.CompletedAt,
	}
	switch value.Revision.Status {
	case evaluation.StatusQueued:
		if state.ModuleStatus != scoring.InterviewShadowRuntimePending {
			return EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if state.ModuleStatus != scoring.InterviewShadowRuntimePending &&
			state.ModuleStatus != scoring.InterviewShadowRuntimeRunning {
			return EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.ModuleStatus != scoring.InterviewShadowRuntimeReady ||
			state.Result == nil || state.Failure != nil ||
			value.Revision.IsFinal {
			return EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
		projected, err := projectInterviewShadowSceneResult(
			*state.Result,
			configuration,
			state.FullConfigHash,
		)
		if err != nil {
			return EvaluationResource{}, err
		}
		resource.ScoreabilityStatus =
			ScoreabilityStatus(
				state.Result.Scoreability,
			)
		resource.GateStatus = GateStatus(
			state.Result.Gate,
		)
		resource.SceneResult = projected
	case evaluation.StatusFailed:
		if state.ModuleStatus != scoring.InterviewShadowRuntimeFailed ||
			state.Result != nil || state.Failure == nil {
			return EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
		failure := interviewShadowFailure(state.Failure.Code)
		resource.StableFailure = &failure
	default:
		return EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func interviewReportResource(
	practiceSessionID string,
	state evaluationreport.InterviewReadState,
) (InterviewReportResource, error) {
	value := state.Evaluation
	if practiceSessionID == "" ||
		value.PracticeSessionID != practiceSessionID ||
		!interviewShadowEvaluation(value) ||
		value.Revision.IsFinal {
		return InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	resource := InterviewReportResource{
		PracticeSessionID:    value.PracticeSessionID,
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		EvaluationStatus:     value.Revision.Status,
		IsFinal:              false,
	}
	switch value.Revision.Status {
	case evaluation.StatusQueued:
		if state.Runtime.ModuleStatus !=
			scoring.InterviewShadowRuntimePending ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if (state.Runtime.ModuleStatus !=
			scoring.InterviewShadowRuntimePending &&
			state.Runtime.ModuleStatus !=
				scoring.InterviewShadowRuntimeRunning) ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.Runtime.ModuleStatus !=
			scoring.InterviewShadowRuntimeReady ||
			state.Runtime.Result == nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot == nil ||
			state.Snapshot.ID != value.InputSnapshotID ||
			state.Snapshot.OwnerUserID != value.OwnerUserID ||
			state.Snapshot.PracticeSessionID !=
				value.PracticeSessionID {
			return InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
		report, err := evaluationreport.ProjectInterviewReport(
			*state.Snapshot,
			*state.Runtime.Result,
		)
		if err != nil {
			return InterviewReportResource{}, err
		}
		resource.Report = &report
	case evaluation.StatusFailed:
		if state.Runtime.ModuleStatus !=
			scoring.InterviewShadowRuntimeFailed ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure == nil ||
			state.Snapshot != nil {
			return InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
		failure := interviewShadowFailure(state.Runtime.Failure.Code)
		resource.StableFailure = &failure
	default:
		return InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func ieltsSpeakingShadowEvaluation(
	value evaluation.Evaluation,
) bool {
	return value.Valid() &&
		value.Scope == evaluation.ScopeSession &&
		value.SceneType == evaluation.SceneIELTSSpeaking &&
		len(value.Revision.Channels) == 1 &&
		value.Revision.Channels[0] == evaluation.ChannelScene &&
		value.Revision.SceneStrategyRef ==
			scoring.IELTSSpeakingShadowStrategyRef &&
		value.Revision.Core4DStrategyRef == "" &&
		value.Revision.PipelineVersion ==
			scoring.IELTSSpeakingShadowPipelineVersion
}

func ieltsSpeakingReportResource(
	practiceSessionID string,
	state evaluationreport.IELTSSpeakingReadState,
) (IELTSSpeakingReportResource, error) {
	value := state.Evaluation
	if practiceSessionID == "" ||
		value.PracticeSessionID != practiceSessionID ||
		!ieltsSpeakingShadowEvaluation(value) ||
		value.Revision.IsFinal {
		return IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	resource := IELTSSpeakingReportResource{
		PracticeSessionID:    value.PracticeSessionID,
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		EvaluationStatus:     value.Revision.Status,
		IsFinal:              false,
	}
	switch value.Revision.Status {
	case evaluation.StatusValidating, evaluation.StatusQueued:
		if state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimePending ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if (state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimePending &&
			state.Runtime.ModuleStatus !=
				scoring.IELTSSpeakingShadowRuntimeRunning) ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimeReady ||
			state.Runtime.Result == nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot == nil ||
			state.Snapshot.ID != value.InputSnapshotID ||
			state.Snapshot.OwnerUserID != value.OwnerUserID ||
			state.Snapshot.PracticeSessionID !=
				value.PracticeSessionID {
			return IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
		report, err := evaluationreport.ProjectIELTSSpeakingReport(
			*state.Snapshot,
			*state.Runtime.Result,
		)
		if err != nil {
			return IELTSSpeakingReportResource{},
				err
		}
		resource.Report = &report
	case evaluation.StatusFailed:
		if state.Runtime.ModuleStatus !=
			scoring.IELTSSpeakingShadowRuntimeFailed ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure == nil ||
			state.Snapshot != nil {
			return IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
		failure := interviewShadowFailure(state.Runtime.Failure.Code)
		resource.StableFailure = &failure
	default:
		return IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func interviewShadowModuleStatus(
	status scoring.InterviewShadowRuntimeStatus,
) ModuleStatus {
	switch status {
	case scoring.InterviewShadowRuntimePending:
		return ModulePending
	case scoring.InterviewShadowRuntimeRunning:
		return ModuleRunning
	case scoring.InterviewShadowRuntimeReady:
		return ModuleReady
	case scoring.InterviewShadowRuntimeFailed:
		return ModuleFailed
	default:
		return ""
	}
}

type interviewSceneResultProjection struct {
	SceneType            evaluation.SceneType                `json:"scene_type"`
	Dimensions           []interviewDimensionProjection      `json:"dimensions"`
	TaskResults          []any                               `json:"task_results"`
	QuestionResults      []any                               `json:"question_results"`
	Core4ObservationRefs []string                            `json:"core4_observation_refs"`
	EvaluationStatus     evaluation.Status                   `json:"evaluation_status"`
	ScoreabilityStatus   scoring.InterviewScoreabilityStatus `json:"scoreability_status"`
	GateStatus           scoring.InterviewGateStatus         `json:"gate_status"`
	ModuleStatuses       map[string]string                   `json:"module_statuses"`
	StrategyRef          string                              `json:"strategy_ref"`
	AggregationVersion   string                              `json:"aggregation_version"`
	CalibrationVersion   string                              `json:"calibration_version"`
	FullConfigHash       string                              `json:"full_config_hash"`
	IsFinal              bool                                `json:"is_final"`
	ReadinessLevel       scoring.InterviewReadinessLevel     `json:"readiness_level"`
}

type interviewDimensionProjection struct {
	DimensionID        scoring.InterviewDimension          `json:"dimension_id"`
	RoundingRule       string                              `json:"rounding_rule"`
	ScoreabilityStatus scoring.InterviewScoreabilityStatus `json:"scoreability_status"`
	GateStatus         scoring.InterviewGateStatus         `json:"gate_status"`
	Confidence         float64                             `json:"confidence"`
	Coverage           float64                             `json:"coverage"`
	ReasonCodes        []ReasonCode                        `json:"reason_codes"`
	EvidenceRefs       []any                               `json:"evidence_refs"`
}

func projectInterviewShadowSceneResult(
	result scoring.InterviewShadowResult,
	configuration scoring.InterviewShadowRuntimeConfiguration,
	fullConfigHash [sha256.Size]byte,
) (json.RawMessage, error) {
	if !configuration.Valid() ||
		fullConfigHash == [sha256.Size]byte{} ||
		result.SceneType != evaluation.SceneInterview ||
		result.Scope != evaluation.ScopeSession ||
		result.Channel != evaluation.ChannelScene ||
		result.Readiness != scoring.InterviewReadinessNotAssessed ||
		len(result.Dimensions) != 5 ||
		result.QuestionResults == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	dimensions := make(
		[]interviewDimensionProjection,
		len(result.Dimensions),
	)
	expectedDimensions := scoring.InterviewDimensions()
	for index, dimension := range result.Dimensions {
		if dimension.DimensionID != expectedDimensions[index] ||
			!validInterviewShadowDimensionGate(
				result.Scoreability,
				result.Gate,
				dimension.Scoreability,
				dimension.Gate,
			) ||
			!unitInterval(dimension.Confidence) ||
			!unitInterval(dimension.Coverage) ||
			len(dimension.ReasonCodes) == 0 {
			return nil, evaluation.ErrInvalidRequest
		}
		reasons := make(
			[]ReasonCode,
			len(dimension.ReasonCodes),
		)
		for reasonIndex, reason := range dimension.ReasonCodes {
			mapped, ok := interviewShadowReason(reason)
			if !ok {
				return nil, evaluation.ErrInvalidRequest
			}
			reasons[reasonIndex] = mapped
		}
		dimensions[index] = interviewDimensionProjection{
			DimensionID:        dimension.DimensionID,
			RoundingRule:       "HALF_UP_TO_INTEGER",
			ScoreabilityStatus: dimension.Scoreability,
			GateStatus:         dimension.Gate,
			Confidence:         dimension.Confidence,
			Coverage:           dimension.Coverage,
			ReasonCodes:        reasons,
			EvidenceRefs:       []any{},
		}
	}
	projection := interviewSceneResultProjection{
		SceneType:            result.SceneType,
		Dimensions:           dimensions,
		TaskResults:          []any{},
		QuestionResults:      []any{},
		Core4ObservationRefs: []string{},
		EvaluationStatus:     evaluation.StatusReady,
		ScoreabilityStatus:   result.Scoreability,
		GateStatus:           result.Gate,
		ModuleStatuses:       map[string]string{"scene": "READY"},
		StrategyRef:          configuration.StrategyRef,
		AggregationVersion:   scoring.InterviewShadowAggregationVersion,
		CalibrationVersion:   scoring.InterviewShadowCalibrationVersion,
		FullConfigHash: "sha256:" +
			hex.EncodeToString(fullConfigHash[:]),
		IsFinal:        false,
		ReadinessLevel: result.Readiness,
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func interviewShadowReason(
	reason scoring.InterviewReasonCode,
) (ReasonCode, bool) {
	switch reason {
	case scoring.InterviewReasonOpportunityNotProvided:
		return ReasonOpportunityNotProvided, true
	case scoring.InterviewReasonASRConfidenceUnavailable,
		scoring.InterviewReasonInsufficientEvidence:
		return ReasonInsufficientEvidence, true
	default:
		return "", false
	}
}

func unitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= 0 && value <= 1
}

func validInterviewShadowDimensionGate(
	resultScoreability scoring.InterviewScoreabilityStatus,
	resultGate scoring.InterviewGateStatus,
	dimensionScoreability scoring.InterviewScoreabilityStatus,
	dimensionGate scoring.InterviewGateStatus,
) bool {
	switch {
	case resultScoreability == scoring.InterviewScoreabilityInsufficient &&
		resultGate == scoring.InterviewGateBlocked:
		return dimensionScoreability ==
			scoring.InterviewScoreabilityInsufficient &&
			dimensionGate == scoring.InterviewGateBlocked
	case resultScoreability == scoring.InterviewScoreabilityProvisional &&
		resultGate == scoring.InterviewGateFeedbackOnly:
		return (dimensionScoreability ==
			scoring.InterviewScoreabilityProvisional &&
			dimensionGate == scoring.InterviewGateFeedbackOnly) ||
			(dimensionScoreability ==
				scoring.InterviewScoreabilityInsufficient &&
				dimensionGate == scoring.InterviewGateBlocked)
	default:
		return false
	}
}

func interviewShadowFailure(
	code string,
) EvaluationFailure {
	switch code {
	case "provider_invalid_json",
		"provider_schema_mismatch",
		"provider_invalid_response":
		return EvaluationFailure{
			ReasonCode: ReasonPolicyViolation,
		}
	case "evidence_ref_invalid":
		return EvaluationFailure{
			ReasonCode: ReasonEvidenceRefInvalid,
		}
	case "version_conflict":
		return EvaluationFailure{
			ReasonCode: ReasonVersionConflict,
		}
	case "runtime_configuration_changed":
		return EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	case "provider_canceled",
		"provider_timeout",
		"rate_limited",
		"timeout",
		"provider_unavailable",
		"invalid_response",
		"cancelled",
		"dependency_error",
		"attempts_exhausted":
		return EvaluationFailure{
			ReasonCode: ReasonInternalRetryable,
			Retryable:  true,
		}
	default:
		return EvaluationFailure{
			ReasonCode: ReasonInternalNonRetryable,
		}
	}
}

func interviewShadowStrategyError() error {
	return apperror.New(
		apperror.UnprocessableEntity,
		"evaluation_strategy_not_available",
		"The requested Evaluation strategy is not available.",
	)
}

func interviewShadowVersionConflictError() error {
	return apperror.New(
		apperror.Conflict,
		"evaluation_version_conflict",
		"Evaluation state changed before the operation completed.",
	)
}

var _ HTTPApplication = (*Application)(nil)
