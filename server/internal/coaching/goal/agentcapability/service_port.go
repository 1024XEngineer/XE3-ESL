package agentcapability

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const defaultGoalSearchLimit = 10

// ServicePort adapts Goal's application service to Agent Tool DTOs.
// Ownership always comes from the trusted CallContext Actor.
type ServicePort struct {
	service *goal.Service
	links   ActiveGoalLinker
}

type ActiveGoalLinker interface {
	SetActiveGoal(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (agentconversation.ThreadGoalLink, error)
}

func NewServicePort(
	service *goal.Service,
	links ActiveGoalLinker,
) (*ServicePort, error) {
	if service == nil || links == nil {
		return nil, errors.New(
			"goal capability: service and active Goal linker are required",
		)
	}
	return &ServicePort{service: service, links: links}, nil
}

func (port *ServicePort) CreateGoal(
	ctx context.Context,
	call capability.CallContext,
	input GoalCreateInput,
) (GoalResult, error) {
	if port == nil || port.service == nil || port.links == nil ||
		!call.Actor.Valid() || call.ThreadID == "" || call.RequestID == "" {
		return GoalResult{}, capability.ErrExecutionRejected
	}
	item, err := port.service.CreateIdempotent(
		ctx,
		call.Actor,
		call.RequestID,
		input.Title,
	)
	if err != nil {
		return GoalResult{}, mapGoalToolError(err)
	}
	if _, err := port.links.SetActiveGoal(
		ctx,
		call.Actor,
		call.ThreadID,
		item.ID,
	); err != nil {
		return GoalResult{}, capability.ErrExecutionRejected
	}
	return mapGoalResult(item), nil
}

func (port *ServicePort) SearchGoals(
	ctx context.Context,
	call capability.CallContext,
	input GoalSearchInput,
) ([]GoalResult, error) {
	if port == nil || port.service == nil || !call.Actor.Valid() {
		return nil, capability.ErrExecutionRejected
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultGoalSearchLimit
	}
	items, err := port.service.Search(ctx, call.Actor, goal.SearchQuery{
		Query: input.Query,
		Limit: limit,
	})
	if err != nil {
		return nil, mapGoalToolError(err)
	}
	result := make([]GoalResult, 0, len(items))
	for _, item := range items {
		result = append(result, mapGoalResult(item))
	}
	return result, nil
}

func mapGoalResult(item goal.Goal) GoalResult {
	return GoalResult{
		GoalID:    item.ID,
		Title:     item.Title,
		Status:    string(item.Status),
		Version:   item.Version,
		UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []capability.SourceRef{{
			Type: "goal",
			ID:   item.ID,
		}},
	}
}

func mapGoalToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, goal.ErrInvalidRequest):
		return capability.ErrInvalidInput
	case errors.Is(err, goal.ErrNotFound),
		errors.Is(err, goal.ErrConflict):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
