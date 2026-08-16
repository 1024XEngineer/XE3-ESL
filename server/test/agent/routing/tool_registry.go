package routing

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func newEvaluationRegistry() (*capability.Registry, error) {
	tools := capabilityfixture.Tools(capabilityfixture.NewStore())
	ports := routingPorts{}
	tools = append(
		tools,
		preparationcapability.NewIELTSWarmUpTool(),
		preparationcapability.NewPreviewTool(ports),
	)
	return capability.NewRegistry(tools...)
}

type routingPorts struct{}

func (routingPorts) PreviewPractice(
	context.Context,
	capability.CallContext,
	preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	action, err := agentclientaction.New(
		preparationcapability.ConfirmPracticePlanActionType,
		json.RawMessage(`{"practice_plan_id":"00000000-0000-4000-8000-000000000001"}`),
	)
	if err != nil {
		return preparationcapability.PreviewResult{}, err
	}
	return preparationcapability.PreviewResult{
		Status:       "preview_ready",
		ClientAction: action,
	}, nil
}
