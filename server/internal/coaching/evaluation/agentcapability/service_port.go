package agentcapability

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

type LatestReportReader interface {
	ListFormalReports(
		context.Context,
		string,
		evaluation.FormalReportHistoryQuery,
	) (evaluation.FormalReportHistoryPage, error)
}

type ServicePort struct {
	reports LatestReportReader
}

func NewServicePort(reports LatestReportReader) (*ServicePort, error) {
	if reports == nil {
		return nil, errors.New(
			"evaluation capability: report reader is required",
		)
	}
	return &ServicePort{reports: reports}, nil
}

func (port *ServicePort) LatestPracticeReport(
	ctx context.Context,
	call capability.CallContext,
) (LatestPracticeReport, error) {
	if port == nil || port.reports == nil || !call.Actor.Valid() {
		return LatestPracticeReport{}, capability.ErrExecutionRejected
	}
	page, err := port.reports.ListFormalReports(
		ctx,
		call.Actor.UserID,
		evaluation.FormalReportHistoryQuery{Limit: 1},
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) ||
			errors.Is(err, evaluation.ErrAccountUnavailable) {
			return LatestPracticeReport{}, capability.ErrExecutionRejected
		}
		return LatestPracticeReport{}, err
	}
	if len(page.Items) != 1 || !page.Items[0].Valid() {
		return LatestPracticeReport{}, capability.ErrExecutionRejected
	}
	return mapLatestFormalReport(page.Items[0]), nil
}

func mapLatestFormalReport(
	stored evaluation.StoredFormalReport,
) LatestPracticeReport {
	report := stored.Report
	result := LatestPracticeReport{
		Scene:           sceneName(report.SceneType),
		SceneModel:      report.SceneModel,
		AssessmentMode:  assessmentMode(report.ScoreabilityStatus),
		Summary:         report.Summary,
		Dimensions:      make([]ReportDimension, len(report.Dimensions)),
		PriorityActions: make([]ReportFinding, 0, len(report.PriorityActions)),
		CompletedAt:     stored.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	findings := make(map[string]ReportFinding)
	for index, dimension := range report.Dimensions {
		mapped := ReportDimension{
			Key:                    dimension.Key,
			Name:                   dimensionName(dimension.Key),
			Score:                  cloneReportScore(dimension.Score),
			Scale:                  string(dimension.Scale),
			Strengths:              mapReportFindings(dimension.Strengths),
			Improvements:           mapReportFindings(dimension.Improvements),
			RecommendedExpressions: mapReportFindings(dimension.Examples),
		}
		result.Dimensions[index] = mapped
		for findingIndex, finding := range dimension.Strengths {
			findings[dimension.Key+":"+finding.ID] =
				mapped.Strengths[findingIndex]
		}
		for findingIndex, finding := range dimension.Improvements {
			findings[dimension.Key+":"+finding.ID] =
				mapped.Improvements[findingIndex]
		}
		for findingIndex, finding := range dimension.Examples {
			findings[dimension.Key+":"+finding.ID] =
				mapped.RecommendedExpressions[findingIndex]
		}
	}
	for _, action := range report.PriorityActions {
		finding, ok := findings[action.DimensionKey+":"+action.FindingID]
		if ok {
			result.PriorityActions = append(result.PriorityActions, finding)
		}
	}
	return result
}

func mapReportFindings(
	items []evaluation.ReportFinding,
) []ReportFinding {
	result := make([]ReportFinding, len(items))
	for index, item := range items {
		excerpts := make([]string, 0, len(item.Evidence))
		seen := make(map[string]struct{}, len(item.Evidence))
		for _, evidence := range item.Evidence {
			excerpt := strings.TrimSpace(evidence.OriginalExcerpt)
			if excerpt == "" {
				continue
			}
			if _, exists := seen[excerpt]; exists {
				continue
			}
			seen[excerpt] = struct{}{}
			excerpts = append(excerpts, excerpt)
		}
		result[index] = ReportFinding{
			Message:          item.Message,
			Suggestion:       item.Suggestion,
			OriginalExcerpts: excerpts,
		}
	}
	return result
}

func cloneReportScore(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sceneName(sceneType evaluation.SceneType) string {
	switch sceneType {
	case evaluation.SceneInterview:
		return "面试英语"
	case evaluation.SceneIELTSSpeaking:
		return "IELTS 口语"
	case evaluation.SceneOverseasDaily:
		return "海外日常英语"
	case evaluation.SceneOverseasWorkplace:
		return "海外职场英语"
	default:
		return string(sceneType)
	}
}

func assessmentMode(status evaluation.ReportScoreability) string {
	if status == evaluation.ReportScoreabilityInsufficient {
		return "证据不足"
	}
	return "暂定评分与反馈"
}

func dimensionName(key string) string {
	switch key {
	case "INTERVIEW_RELEVANCE":
		return "回答相关性"
	case "INTERVIEW_STRUCTURE":
		return "回答结构"
	case "INTERVIEW_EVIDENCE":
		return "证据与说服力"
	case "INTERVIEW_PROFESSIONAL":
		return "职业表达"
	case "INTERVIEW_INTERACTION":
		return "追问应对能力"
	case "FLUENCY_COHERENCE":
		return "流利度与连贯性"
	case "LEXICAL_RESOURCE":
		return "词汇资源"
	case "GRAMMATICAL_RANGE_ACCURACY":
		return "语法范围与准确性"
	case "PRONUNCIATION":
		return "发音"
	default:
		return strings.ReplaceAll(strings.ToLower(key), "_", " ")
	}
}
