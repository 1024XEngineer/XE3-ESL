package bootstrap

import (
	"context"
	"errors"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

type agentLearningProfileReader struct {
	repository *evaluation.PostgresRepository
}

func newAgentLearningProfileReader(
	repository *evaluation.PostgresRepository,
) (*agentLearningProfileReader, error) {
	if repository == nil {
		return nil, errors.New(
			"bootstrap: Learning Profile read dependency is required",
		)
	}
	return &agentLearningProfileReader{repository: repository}, nil
}

func (reader *agentLearningProfileReader) ReadLearningProfile(
	ctx context.Context,
	request agentcontext.LearningProfileReadRequest,
) ([]agentcontext.LearningProfileDimension, error) {
	if reader == nil || reader.repository == nil || ctx == nil ||
		!request.Valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	dimensions, err := reader.repository.ReadLearningProfile(
		ctx,
		request.Actor.UserID,
		evaluation.LearningProfileQuery{
			GoalID: request.GoalID,
			Limit:  request.Limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make(
		[]agentcontext.LearningProfileDimension,
		len(dimensions),
	)
	for index, dimension := range dimensions {
		issues := make(
			[]agentcontext.LearningProfileIssue,
			len(dimension.RecurringIssues),
		)
		for issueIndex, issue := range dimension.RecurringIssues {
			issues[issueIndex] = agentcontext.LearningProfileIssue{
				Key:      issue.Key,
				Label:    issue.Label,
				Count:    issue.Count,
				LastSeen: issue.LastSeen,
			}
		}
		sources := make(
			[]agentcontext.LearningProfileEvaluationSource,
			len(dimension.SourceEvaluations),
		)
		for sourceIndex, source := range dimension.SourceEvaluations {
			sources[sourceIndex] =
				agentcontext.LearningProfileEvaluationSource{
					EvaluationID:         source.EvaluationID,
					EvaluationRevisionID: source.EvaluationRevisionID,
					CreatedAt:            source.CreatedAt,
				}
		}
		result[index] = agentcontext.LearningProfileDimension{
			Key:               dimension.Key,
			Scale:             string(dimension.Scale),
			EstimatedValue:    dimension.EstimatedValue,
			Confidence:        dimension.Confidence,
			Trend:             string(dimension.Trend),
			RecurringIssues:   issues,
			EvaluationSources: sources,
			StrategyVersion:   dimension.StrategyVersion,
			UpdatedAt:         dimension.UpdatedAt,
		}
	}
	return result, nil
}

var _ agentcontext.LearningProfileReader = (*agentLearningProfileReader)(nil)
