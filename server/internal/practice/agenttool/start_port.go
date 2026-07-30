package agenttool

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type StartApplication interface {
	ConfirmAndStartPractice(
		context.Context,
		requestcontext.Actor,
		string,
		practice.StartConfirmation,
	) (practice.ConfirmAndStartResult, error)
}

type StartServicePort struct {
	practice StartApplication
}

func NewStartServicePort(
	application StartApplication,
) (*StartServicePort, error) {
	if application == nil {
		return nil, errors.New(
			"practice agenttool: Start application is required",
		)
	}
	return &StartServicePort{practice: application}, nil
}

func (port *StartServicePort) StartPractice(
	ctx context.Context,
	call tool.CallContext,
	input StartInput,
) (StartResult, error) {
	if port == nil || port.practice == nil || !call.Actor.Valid() ||
		call.ThreadID == "" || call.RequestID == "" {
		return StartResult{}, tool.ErrExecutionRejected
	}
	confirmation := call.Confirmation
	if !input.UserConfirmed || confirmation == nil ||
		confirmation.Kind != practiceStartConfirmationKind ||
		confirmation.ResourceID != input.PracticePlanID ||
		confirmation.Revision != input.ExpectedPlanRevision {
		return StartResult{Status: "confirmation_required"}, nil
	}
	started, err := port.practice.ConfirmAndStartPractice(
		ctx,
		call.Actor,
		call.RequestID,
		practice.StartConfirmation{
			AgentThreadID:        call.ThreadID,
			PracticePlanID:       input.PracticePlanID,
			ExpectedPlanRevision: input.ExpectedPlanRevision,
		},
	)
	if err != nil {
		return startPracticeError(err), nil
	}
	session := started.Bootstrap.Session
	status := "started"
	if started.ActiveConflict {
		status = "active_session_conflict"
	}
	return StartResult{
		Status:            status,
		PracticeSessionID: session.ID,
		PracticePlanID:    session.PlanID,
		PlanRevision:      started.Bootstrap.Snapshot.PlanRevision,
		SessionStatus:     string(session.Status),
		StartTarget:       "/v1/practice-sessions/" + session.ID,
		Replayed:          started.Replayed,
		SourceRefs: []tool.SourceRef{
			{Type: "practice_plan", ID: session.PlanID},
			{Type: "practice_session", ID: session.ID},
		},
	}, nil
}

func startPracticeError(err error) StartResult {
	switch {
	case errors.Is(err, persistence.ErrInvalidArgument),
		errors.Is(err, persistence.ErrIdempotencyConflict):
		return StartResult{Status: "invalid_input"}
	case errors.Is(err, persistence.ErrNotFound):
		return StartResult{Status: "not_found"}
	case errors.Is(err, persistence.ErrConflict):
		return StartResult{Status: "version_conflict"}
	default:
		return StartResult{Status: "unavailable"}
	}
}
