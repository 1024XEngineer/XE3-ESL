package report

import (
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func ProjectGeneralSceneFormalReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.GeneralSceneResult,
) (FormalReport, error) {
	if err := scoring.ValidateGeneralSceneResult(snapshot, result); err != nil {
		return FormalReport{}, err
	}
	summary := "本次练习已形成场景沟通评估，可按优先行动继续复练。"
	scoreability := ReportScoreabilityProvisional
	if result.ScoreabilityStatus == scoring.GeneralSceneScoreabilityInsufficient {
		summary = "本次练习的有效证据不足，暂不形成能力结论。"
		scoreability = ReportScoreabilityInsufficient
	}
	detail, err := json.Marshal(result)
	if err != nil {
		return FormalReport{}, evaluation.ErrInvalidRequest
	}
	dimensions := make([]ReportDimension, len(result.Dimensions))
	for index, dimension := range result.Dimensions {
		dimensions[index] = projectGeneralSceneDimension(dimension)
	}
	actions := make([]ReportPriorityAction, len(result.PriorityActions))
	for index, action := range result.PriorityActions {
		actions[index] = ReportPriorityAction(action)
	}
	formal := FormalReport{
		SchemaVersion:      FormalReportSchemaVersion,
		SceneType:          result.SceneType,
		SceneModel:         result.SceneModel,
		ScoreabilityStatus: scoreability,
		Summary:            summary,
		Dimensions:         dimensions,
		PriorityActions:    actions,
		DetailSchema:       scoring.GeneralSceneSchemaVersion,
		Detail:             detail,
	}
	if !formal.Valid() {
		return FormalReport{}, evaluation.ErrInvalidRequest
	}
	return formal, nil
}

func projectGeneralSceneDimension(
	dimension scoring.GeneralSceneDimensionResult,
) ReportDimension {
	projected := ReportDimension{
		Key:          dimension.Key,
		Score:        dimension.Score,
		Scale:        ReportScalePercentage100,
		Coverage:     dimension.Coverage,
		Confidence:   dimension.Confidence,
		ReasonCodes:  dimension.ReasonCodes,
		EvidenceRefs: dimension.EvidenceRefs,
		Strengths:    projectGeneralSceneFindings(dimension.Strengths),
		Improvements: projectGeneralSceneFindings(dimension.Improvements),
		Examples:     projectGeneralSceneFindings(dimension.Examples),
	}
	return projected
}

func projectGeneralSceneFindings(
	findings []scoring.GeneralSceneFinding,
) []ReportFinding {
	projected := make([]ReportFinding, len(findings))
	for index, finding := range findings {
		evidenceItems := make([]ReportEvidence, len(finding.Evidence))
		for evidenceIndex, item := range finding.Evidence {
			evidenceItems[evidenceIndex] = ReportEvidence(item)
		}
		projected[index] = ReportFinding{
			ID:         finding.ID,
			Message:    finding.Message,
			Suggestion: finding.Suggestion,
			Evidence:   evidenceItems,
		}
	}
	return projected
}
