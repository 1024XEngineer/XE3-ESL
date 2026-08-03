package agenttool

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
)

type LatestInterviewReportReader interface {
	GetLatestInterviewReportState(
		context.Context,
		string,
	) (evaluation.InterviewReportReadState, error)
}

type ServicePort struct {
	reports LatestInterviewReportReader
}

func NewServicePort(reports LatestInterviewReportReader) (*ServicePort, error) {
	if reports == nil {
		return nil, errors.New(
			"evaluation agenttool: report reader is required",
		)
	}
	return &ServicePort{reports: reports}, nil
}

func (port *ServicePort) LatestPracticeReport(
	ctx context.Context,
	call tool.CallContext,
) (LatestPracticeReport, error) {
	if port == nil || port.reports == nil || !call.Actor.Valid() {
		return LatestPracticeReport{}, tool.ErrExecutionRejected
	}
	state, err := port.reports.GetLatestInterviewReportState(
		ctx,
		call.Actor.UserID,
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) {
			return LatestPracticeReport{}, tool.ErrExecutionRejected
		}
		return LatestPracticeReport{}, err
	}
	if state.Evaluation.Revision.Status != evaluation.StatusReady ||
		state.Runtime.ModuleStatus != evaluation.InterviewShadowRuntimeReady ||
		state.Runtime.Result == nil || state.Snapshot == nil {
		return LatestPracticeReport{}, tool.ErrExecutionRejected
	}
	report, err := evaluation.ProjectInterviewReport(
		*state.Snapshot,
		*state.Runtime.Result,
	)
	if err != nil {
		return LatestPracticeReport{}, err
	}
	return mapLatestInterviewReport(
		report,
		state.Evaluation.Revision.CompletedAt,
	), nil
}

func mapLatestInterviewReport(
	report evaluation.InterviewReport,
	completedAt *time.Time,
) LatestPracticeReport {
	result := LatestPracticeReport{
		Scene:           "面试英语",
		AssessmentMode:  "反馈模式",
		Dimensions:      make([]ReportDimension, len(report.Dimensions)),
		Answers:         make([]ReportAnswer, 0, len(report.Questions)),
		PriorityActions: make([]ReportFinding, 0, len(report.PriorityActions)),
	}
	if completedAt != nil && !completedAt.IsZero() {
		result.CompletedAt = completedAt.UTC().Format(time.RFC3339Nano)
	}
	if report.ScoreabilityStatus == evaluation.InterviewScoreabilityProvisional {
		result.AssessmentMode = "评分与反馈"
	}
	findings := make(map[string]ReportFinding)
	for index, dimension := range report.Dimensions {
		mapped := ReportDimension{
			Name:                   interviewDimensionName(dimension.DimensionID),
			Score:                  dimension.Score,
			Strengths:              mapReportFindings(dimension.Strengths),
			Improvements:           mapReportFindings(dimension.Improvements),
			RecommendedExpressions: mapReportFindings(dimension.RecommendedExpressions),
		}
		result.Dimensions[index] = mapped
		for findingIndex, finding := range dimension.Improvements {
			findings[string(dimension.DimensionID)+":"+finding.FindingID] =
				mapped.Improvements[findingIndex]
		}
	}
	for _, question := range report.Questions {
		if question.AssessmentStatus != evaluation.InterviewAssessmentAssessed {
			continue
		}
		result.Answers = append(result.Answers, ReportAnswer{
			Question:   question.QuestionText,
			Transcript: question.ConfirmedTranscript,
		})
	}
	for _, action := range report.PriorityActions {
		finding, ok := findings[string(action.DimensionID)+":"+action.FindingID]
		if ok {
			result.PriorityActions = append(result.PriorityActions, finding)
		}
	}
	return result
}

func mapReportFindings(
	items []evaluation.InterviewReportFinding,
) []ReportFinding {
	result := make([]ReportFinding, len(items))
	for index, item := range items {
		excerpts := make([]string, 0, len(item.Evidence))
		seen := make(map[string]struct{}, len(item.Evidence))
		for _, evidence := range item.Evidence {
			if evidence.OriginalExcerpt == "" {
				continue
			}
			if _, exists := seen[evidence.OriginalExcerpt]; exists {
				continue
			}
			seen[evidence.OriginalExcerpt] = struct{}{}
			excerpts = append(excerpts, evidence.OriginalExcerpt)
		}
		result[index] = ReportFinding{
			Message:          item.Message,
			Suggestion:       item.Suggestion,
			OriginalExcerpts: excerpts,
		}
	}
	return result
}

func interviewDimensionName(dimension evaluation.InterviewDimension) string {
	switch dimension {
	case evaluation.InterviewDimensionRelevance:
		return "回答相关性"
	case evaluation.InterviewDimensionStructure:
		return "回答结构"
	case evaluation.InterviewDimensionEvidence:
		return "证据与说服力"
	case evaluation.InterviewDimensionProfessional:
		return "职业表达"
	case evaluation.InterviewDimensionInteraction:
		return "追问应对能力"
	default:
		return "面试表现"
	}
}
