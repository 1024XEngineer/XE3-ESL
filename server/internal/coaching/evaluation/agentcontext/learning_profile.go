package agentcontext

import (
	"context"
	"errors"

	agentctx "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/learningprofile"
)

type Source interface {
	ReadLearningProfile(
		context.Context,
		string,
		learningprofile.Query,
	) ([]learningprofile.Dimension, error)
}

type LearningProfileReader struct {
	source Source
}

func NewLearningProfileReader(source Source) (*LearningProfileReader, error) {
	if source == nil {
		return nil, errors.New(
			"evaluation: Agent Context Learning Profile source is required",
		)
	}
	return &LearningProfileReader{source: source}, nil
}

func (reader *LearningProfileReader) ReadLearningProfile(
	ctx context.Context,
	request agentctx.LearningProfileReadRequest,
) ([]agentctx.LearningProfileDimension, error) {
	if reader == nil || reader.source == nil || ctx == nil ||
		!request.Valid() {
		return nil, agentctx.ErrInvalidContext
	}
	dimensions, err := reader.source.ReadLearningProfile(
		ctx,
		request.Actor.UserID,
		learningprofile.Query{
			GoalID: request.GoalID,
			Limit:  request.Limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make(
		[]agentctx.LearningProfileDimension,
		len(dimensions),
	)
	for index, dimension := range dimensions {
		issues := make(
			[]agentctx.LearningProfileIssue,
			len(dimension.RecurringIssues),
		)
		for issueIndex, issue := range dimension.RecurringIssues {
			issues[issueIndex] = agentctx.LearningProfileIssue{
				Key:      issue.Key,
				Label:    issue.Label,
				Count:    issue.Count,
				LastSeen: issue.LastSeen,
			}
		}
		sources := make(
			[]agentctx.LearningProfileEvaluationSource,
			len(dimension.SourceEvaluations),
		)
		for sourceIndex, source := range dimension.SourceEvaluations {
			sources[sourceIndex] =
				agentctx.LearningProfileEvaluationSource{
					EvaluationID:         source.EvaluationID,
					EvaluationRevisionID: source.EvaluationRevisionID,
					CreatedAt:            source.CreatedAt,
				}
		}
		result[index] = agentctx.LearningProfileDimension{
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

var _ agentctx.LearningProfileReader = (*LearningProfileReader)(nil)
