package agenttool

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
)

const defaultMatterSearchLimit = 10

// ServicePort adapts Matter's application service to Agent Tool DTOs.
// Ownership always comes from the trusted CallContext Actor.
type ServicePort struct {
	service *matter.Service
}

func NewServicePort(service *matter.Service) (*ServicePort, error) {
	if service == nil {
		return nil, errors.New("matter agenttool: service is required")
	}
	return &ServicePort{service: service}, nil
}

func (port *ServicePort) CreateScenario(
	ctx context.Context,
	call tool.CallContext,
	input ScenarioCreateInput,
) (MatterResult, error) {
	if port == nil || port.service == nil || !call.Actor.Valid() ||
		call.RequestID == "" {
		return MatterResult{}, tool.ErrExecutionRejected
	}
	item, err := port.service.CreateIdempotent(
		ctx,
		call.Actor,
		call.RequestID,
		input.Title,
	)
	if err != nil {
		return MatterResult{}, mapMatterToolError(err)
	}
	return mapMatterResult(item), nil
}

func (port *ServicePort) SearchScenarios(
	ctx context.Context,
	call tool.CallContext,
	input ScenarioSearchInput,
) ([]MatterResult, error) {
	if port == nil || port.service == nil || !call.Actor.Valid() {
		return nil, tool.ErrExecutionRejected
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultMatterSearchLimit
	}
	items, err := port.service.Search(ctx, call.Actor, matter.SearchQuery{
		Query: input.Query,
		Limit: limit,
	})
	if err != nil {
		return nil, mapMatterToolError(err)
	}
	result := make([]MatterResult, 0, len(items))
	for _, item := range items {
		result = append(result, mapMatterResult(item))
	}
	return result, nil
}

func mapMatterResult(item matter.Matter) MatterResult {
	return MatterResult{
		MatterID:  item.ID,
		Title:     item.Title,
		Status:    string(item.Status),
		Version:   item.Version,
		UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		SourceRefs: []tool.SourceRef{{
			Type: "matter",
			ID:   item.ID,
		}},
	}
}

func mapMatterToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, matter.ErrInvalidRequest):
		return tool.ErrInvalidInput
	case errors.Is(err, matter.ErrNotFound),
		errors.Is(err, matter.ErrConflict):
		return tool.ErrExecutionRejected
	default:
		return err
	}
}
