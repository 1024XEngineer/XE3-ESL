package evaluation

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
)

const (
	InterviewReportSchemaVersion   = "interview-report/v1"
	InterviewReportReadinessNotice = InterviewShadowReadinessNotice
)

type InterviewAssessmentStatus string

const (
	InterviewAssessmentAssessed    InterviewAssessmentStatus = "ASSESSED"
	InterviewAssessmentNotAssessed InterviewAssessmentStatus = "NOT_ASSESSED"
)

type InterviewReport struct {
	SchemaVersion      string                       `json:"schema_version"`
	ScoreabilityStatus InterviewScoreabilityStatus  `json:"scoreability_status"`
	GateStatus         InterviewGateStatus          `json:"gate_status"`
	ReadinessLevel     InterviewReadinessLevel      `json:"readiness_level"`
	ReadinessNotice    string                       `json:"readiness_notice"`
	Dimensions         []InterviewReportDimension   `json:"dimensions"`
	Questions          []InterviewReportQuestion    `json:"questions"`
	PriorityActions    []InterviewReportPriorityRef `json:"priority_actions"`
}

type InterviewReportDimension struct {
	DimensionID            InterviewDimension          `json:"dimension_id"`
	ScoreabilityStatus     InterviewScoreabilityStatus `json:"scoreability_status"`
	GateStatus             InterviewGateStatus         `json:"gate_status"`
	Coverage               float64                     `json:"coverage"`
	Confidence             float64                     `json:"confidence"`
	ReasonCodes            []InterviewReasonCode       `json:"reason_codes"`
	EvidenceRefIDs         []string                    `json:"evidence_ref_ids"`
	Strengths              []InterviewReportFinding    `json:"strengths"`
	Improvements           []InterviewReportFinding    `json:"improvements"`
	RecommendedExpressions []InterviewReportFinding    `json:"recommended_expressions"`
}

type InterviewReportFinding struct {
	FindingID  string                    `json:"finding_id"`
	Message    string                    `json:"message"`
	Suggestion string                    `json:"suggestion,omitempty"`
	Evidence   []InterviewReportEvidence `json:"evidence"`
}

type InterviewReportEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	TurnID          string `json:"turn_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

type InterviewReportQuestion struct {
	QuestionID          string                           `json:"question_id"`
	QuestionType        string                           `json:"question_type"`
	ParentQuestionID    string                           `json:"parent_question_id,omitempty"`
	OpportunityStatus   InterviewOpportunityStatus       `json:"opportunity_status"`
	AssessmentStatus    InterviewAssessmentStatus        `json:"assessment_status"`
	QuestionText        string                           `json:"question_text"`
	ConfirmedTranscript string                           `json:"confirmed_transcript,omitempty"`
	ResponseTurnID      string                           `json:"response_turn_id,omitempty"`
	EvidenceRefIDs      []string                         `json:"evidence_ref_ids"`
	DimensionFindings   []InterviewQuestionDimensionRefs `json:"dimension_findings"`
}

type InterviewQuestionDimensionRefs struct {
	DimensionID                     InterviewDimension `json:"dimension_id"`
	StrengthFindingIDs              []string           `json:"strength_finding_ids"`
	ImprovementFindingIDs           []string           `json:"improvement_finding_ids"`
	RecommendedExpressionFindingIDs []string           `json:"recommended_expression_finding_ids"`
}

type InterviewReportPriorityRef struct {
	DimensionID InterviewDimension `json:"dimension_id"`
	FindingID   string             `json:"finding_id"`
}

func ProjectInterviewReport(
	snapshot EvidenceSnapshot,
	result InterviewShadowResult,
) (InterviewReport, error) {
	if err := ValidateInterviewShadowResult(snapshot, result); err != nil {
		return InterviewReport{}, err
	}
	var payload evidencePayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return InterviewReport{}, ErrInvalidInterviewShadow
	}
	turns := make(
		map[string]evidenceConfirmedTurn,
		len(payload.ConfirmedTurns),
	)
	for _, turn := range payload.ConfirmedTurns {
		turns[turn.TurnID] = turn
	}

	report := InterviewReport{
		SchemaVersion:      InterviewReportSchemaVersion,
		ScoreabilityStatus: result.Scoreability,
		GateStatus:         result.Gate,
		ReadinessLevel:     result.Readiness,
		ReadinessNotice:    result.ReadinessNotice,
		Dimensions: make(
			[]InterviewReportDimension,
			len(result.Dimensions),
		),
		Questions: make(
			[]InterviewReportQuestion,
			len(result.QuestionResults),
		),
		PriorityActions: []InterviewReportPriorityRef{},
	}
	for index, dimension := range result.Dimensions {
		report.Dimensions[index] = projectInterviewReportDimension(
			dimension,
		)
		for _, finding := range dimension.Improvements {
			if len(report.PriorityActions) == 3 {
				break
			}
			report.PriorityActions = append(
				report.PriorityActions,
				InterviewReportPriorityRef{
					DimensionID: dimension.DimensionID,
					FindingID:   finding.ID,
				},
			)
		}
	}
	for index, question := range result.QuestionResults {
		opportunity := payload.OpportunityManifest[index]
		projected := InterviewReportQuestion{
			QuestionID:        question.QuestionID,
			QuestionType:      question.QuestionType,
			ParentQuestionID:  question.ParentQuestionID,
			OpportunityStatus: question.OpportunityStatus,
			AssessmentStatus:  InterviewAssessmentNotAssessed,
			QuestionText:      opportunity.QuestionText,
			ResponseTurnID:    question.ResponseTurnID,
			EvidenceRefIDs: slices.Clone(
				question.EvidenceRefIDs,
			),
			DimensionFindings: make(
				[]InterviewQuestionDimensionRefs,
				len(question.DimensionResults),
			),
		}
		if question.OpportunityStatus == InterviewOpportunityProvided {
			turn, exists := turns[question.ResponseTurnID]
			if !exists || turn.QuestionID != question.QuestionID {
				return InterviewReport{}, ErrInvalidInterviewShadow
			}
			projected.AssessmentStatus = InterviewAssessmentAssessed
			projected.ConfirmedTranscript = turn.Transcript.Text
		}
		for dimensionIndex, dimension := range question.DimensionResults {
			projected.DimensionFindings[dimensionIndex] =
				InterviewQuestionDimensionRefs{
					DimensionID: dimension.DimensionID,
					StrengthFindingIDs: slices.Clone(
						dimension.StrengthFindingIDs,
					),
					ImprovementFindingIDs: slices.Clone(
						dimension.ImprovementFindingIDs,
					),
					RecommendedExpressionFindingIDs: slices.Clone(
						dimension.
							RecommendedExpressionFindingIDs,
					),
				}
		}
		report.Questions[index] = projected
	}
	if !report.Valid() {
		return InterviewReport{}, ErrInvalidInterviewShadow
	}
	return report, nil
}

func projectInterviewReportDimension(
	source InterviewShadowDimensionResult,
) InterviewReportDimension {
	return InterviewReportDimension{
		DimensionID:        source.DimensionID,
		ScoreabilityStatus: source.Scoreability,
		GateStatus:         source.Gate,
		Coverage:           source.Coverage,
		Confidence:         source.Confidence,
		ReasonCodes:        slices.Clone(source.ReasonCodes),
		EvidenceRefIDs:     slices.Clone(source.EvidenceRefIDs),
		Strengths: projectInterviewReportFindings(
			source.Strengths,
		),
		Improvements: projectInterviewReportFindings(
			source.Improvements,
		),
		RecommendedExpressions: projectInterviewReportFindings(
			source.RecommendedExpressions,
		),
	}
}

func projectInterviewReportFindings(
	source []InterviewShadowFinding,
) []InterviewReportFinding {
	result := make([]InterviewReportFinding, len(source))
	for index, finding := range source {
		evidence := make(
			[]InterviewReportEvidence,
			len(finding.Evidence),
		)
		for evidenceIndex, ref := range finding.Evidence {
			evidence[evidenceIndex] = InterviewReportEvidence{
				EvidenceRefID:   ref.EvidenceRefID,
				TurnID:          ref.TurnID,
				StartUTF8Byte:   ref.StartUTF8Byte,
				EndUTF8Byte:     ref.EndUTF8Byte,
				OriginalExcerpt: ref.OriginalExcerpt,
			}
		}
		result[index] = InterviewReportFinding{
			FindingID:  finding.ID,
			Message:    finding.Message,
			Suggestion: finding.Suggestion,
			Evidence:   evidence,
		}
	}
	return result
}

func (report InterviewReport) Valid() bool {
	if report.SchemaVersion != InterviewReportSchemaVersion ||
		!validInterviewReportGate(
			report.ScoreabilityStatus,
			report.GateStatus,
		) ||
		report.ReadinessLevel != InterviewReadinessNotAssessed ||
		report.ReadinessNotice != InterviewReportReadinessNotice ||
		len(report.Dimensions) != len(interviewDimensionOrder) ||
		len(report.Questions) == 0 ||
		len(report.Questions) > 64 ||
		report.PriorityActions == nil ||
		len(report.PriorityActions) > 3 {
		return false
	}
	improvements := make(map[InterviewDimension]map[string]struct{}, 5)
	strengths := make(map[InterviewDimension]map[string]struct{}, 5)
	recommended := make(map[InterviewDimension]map[string]struct{}, 5)
	allFindingIDs := make(map[string]struct{})
	findingEvidenceRefs := make(map[string]map[string]struct{})
	for index, dimension := range report.Dimensions {
		if dimension.DimensionID != interviewDimensionOrder[index] ||
			!validInterviewReportGate(
				dimension.ScoreabilityStatus,
				dimension.GateStatus,
			) ||
			!validInterviewReportRatio(dimension.Coverage) ||
			!validInterviewReportRatio(dimension.Confidence) ||
			dimension.ReasonCodes == nil ||
			len(dimension.ReasonCodes) != 1 ||
			dimension.EvidenceRefIDs == nil ||
			!validInterviewReportIDList(
				dimension.EvidenceRefIDs,
				64,
			) ||
			dimension.Strengths == nil ||
			len(dimension.Strengths) > interviewShadowMaximumFindings ||
			dimension.Improvements == nil ||
			len(dimension.Improvements) > interviewShadowMaximumFindings ||
			dimension.RecommendedExpressions == nil {
			return false
		}
		if len(dimension.RecommendedExpressions) >
			interviewShadowMaximumFindings {
			return false
		}
		if report.ScoreabilityStatus ==
			InterviewScoreabilityInsufficient &&
			dimension.ScoreabilityStatus !=
				InterviewScoreabilityInsufficient {
			return false
		}
		for _, reason := range dimension.ReasonCodes {
			if !validInterviewReportReason(reason) {
				return false
			}
		}
		strengths[dimension.DimensionID] = make(map[string]struct{})
		improvements[dimension.DimensionID] = make(map[string]struct{})
		recommended[dimension.DimensionID] = make(map[string]struct{})
		evidenceRefSet := make(map[string]struct{})
		for _, collection := range []struct {
			findings []InterviewReportFinding
			ids      map[string]struct{}
		}{
			{dimension.Strengths, strengths[dimension.DimensionID]},
			{dimension.Improvements, improvements[dimension.DimensionID]},
			{
				dimension.RecommendedExpressions,
				recommended[dimension.DimensionID],
			},
		} {
			for _, finding := range collection.findings {
				if !validInterviewReportFinding(finding) {
					return false
				}
				if _, duplicate := allFindingIDs[finding.FindingID]; duplicate {
					return false
				}
				allFindingIDs[finding.FindingID] = struct{}{}
				collection.ids[finding.FindingID] =
					struct{}{}
				findingEvidenceRefs[finding.FindingID] =
					make(map[string]struct{}, len(finding.Evidence))
				for _, evidence := range finding.Evidence {
					evidenceRefSet[evidence.EvidenceRefID] =
						struct{}{}
					findingEvidenceRefs[finding.FindingID][evidence.EvidenceRefID] =
						struct{}{}
				}
			}
		}
		expectedEvidenceRefs := make(
			[]string,
			0,
			len(evidenceRefSet),
		)
		for refID := range evidenceRefSet {
			expectedEvidenceRefs = append(expectedEvidenceRefs, refID)
		}
		slices.Sort(expectedEvidenceRefs)
		if !slices.Equal(
			dimension.EvidenceRefIDs,
			expectedEvidenceRefs,
		) {
			return false
		}
		switch dimension.ScoreabilityStatus {
		case InterviewScoreabilityProvisional:
			if dimension.ReasonCodes[0] !=
				InterviewReasonASRConfidenceUnavailable ||
				len(dimension.EvidenceRefIDs) == 0 ||
				len(dimension.Strengths)+
					len(dimension.Improvements) == 0 {
				return false
			}
		case InterviewScoreabilityInsufficient:
			if (dimension.ReasonCodes[0] !=
				InterviewReasonInsufficientEvidence &&
				dimension.ReasonCodes[0] !=
					InterviewReasonOpportunityNotProvided) ||
				len(dimension.EvidenceRefIDs) != 0 ||
				len(dimension.Strengths) != 0 ||
				len(dimension.Improvements) != 0 ||
				len(dimension.RecommendedExpressions) != 0 {
				return false
			}
		default:
			return false
		}
	}

	questions := make(map[string]struct{}, len(report.Questions))
	for _, question := range report.Questions {
		if !validIdentifier(question.QuestionID) ||
			(question.QuestionType != "PRIMARY" &&
				question.QuestionType != "FOLLOW_UP") ||
			!validInterviewText(
				question.QuestionText,
				interviewShadowMaximumInputString,
			) ||
			question.EvidenceRefIDs == nil ||
			!validInterviewReportIDList(
				question.EvidenceRefIDs,
				64,
			) ||
			len(question.DimensionFindings) !=
				len(interviewDimensionOrder) {
			return false
		}
		if _, duplicate := questions[question.QuestionID]; duplicate {
			return false
		}
		if question.QuestionType == "PRIMARY" {
			if question.ParentQuestionID != "" {
				return false
			}
		} else if _, exists := questions[question.ParentQuestionID]; !exists {
			return false
		}
		questions[question.QuestionID] = struct{}{}
		switch question.OpportunityStatus {
		case InterviewOpportunityProvided:
			if question.AssessmentStatus !=
				InterviewAssessmentAssessed ||
				!validIdentifier(question.ResponseTurnID) ||
				!validInterviewText(
					question.ConfirmedTranscript,
					interviewShadowMaximumInputString,
				) ||
				len(question.EvidenceRefIDs) == 0 {
				return false
			}
		case InterviewOpportunityNotProvided:
			if question.AssessmentStatus !=
				InterviewAssessmentNotAssessed ||
				question.ResponseTurnID != "" ||
				question.ConfirmedTranscript != "" ||
				len(question.EvidenceRefIDs) != 0 {
				return false
			}
		default:
			return false
		}
		for index, dimension := range question.DimensionFindings {
			if dimension.DimensionID != interviewDimensionOrder[index] ||
				dimension.StrengthFindingIDs == nil ||
				dimension.ImprovementFindingIDs == nil ||
				dimension.RecommendedExpressionFindingIDs == nil ||
				!validInterviewQuestionFindingRefs(
					dimension.StrengthFindingIDs,
					strengths[dimension.DimensionID],
				) ||
				!validInterviewQuestionFindingRefs(
					dimension.ImprovementFindingIDs,
					improvements[dimension.DimensionID],
				) ||
				!validInterviewQuestionFindingRefs(
					dimension.RecommendedExpressionFindingIDs,
					recommended[dimension.DimensionID],
				) ||
				!interviewQuestionFindingsOverlapEvidence(
					dimension.StrengthFindingIDs,
					question.EvidenceRefIDs,
					findingEvidenceRefs,
				) ||
				!interviewQuestionFindingsOverlapEvidence(
					dimension.ImprovementFindingIDs,
					question.EvidenceRefIDs,
					findingEvidenceRefs,
				) ||
				!interviewQuestionFindingsOverlapEvidence(
					dimension.RecommendedExpressionFindingIDs,
					question.EvidenceRefIDs,
					findingEvidenceRefs,
				) {
				return false
			}
		}
	}

	expectedActions := make([]InterviewReportPriorityRef, 0, 3)
	for _, dimension := range report.Dimensions {
		for _, finding := range dimension.Improvements {
			if len(expectedActions) == 3 {
				break
			}
			expectedActions = append(
				expectedActions,
				InterviewReportPriorityRef{
					DimensionID: dimension.DimensionID,
					FindingID:   finding.FindingID,
				},
			)
		}
	}
	return slices.Equal(report.PriorityActions, expectedActions)
}

func validInterviewReportGate(
	scoreability InterviewScoreabilityStatus,
	gate InterviewGateStatus,
) bool {
	return (scoreability == InterviewScoreabilityProvisional &&
		gate == InterviewGateFeedbackOnly) ||
		(scoreability == InterviewScoreabilityInsufficient &&
			gate == InterviewGateBlocked)
}

func validInterviewReportReason(reason InterviewReasonCode) bool {
	switch reason {
	case InterviewReasonASRConfidenceUnavailable,
		InterviewReasonInsufficientEvidence,
		InterviewReasonOpportunityNotProvided:
		return true
	default:
		return false
	}
}

func validInterviewReportRatio(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= 0 && value <= 1
}

func validInterviewReportFinding(finding InterviewReportFinding) bool {
	if !validIdentifier(finding.FindingID) ||
		!validInterviewText(
			finding.Message,
			interviewShadowMaximumTextBytes,
		) ||
		finding.Evidence == nil ||
		len(finding.Evidence) == 0 ||
		len(finding.Evidence) > interviewShadowMaximumAnchors {
		return false
	}
	if finding.Suggestion != "" &&
		!validInterviewText(
			finding.Suggestion,
			interviewShadowMaximumTextBytes,
		) {
		return false
	}
	for _, evidence := range finding.Evidence {
		if !validIdentifier(evidence.EvidenceRefID) ||
			!validIdentifier(evidence.TurnID) ||
			evidence.StartUTF8Byte < 0 ||
			evidence.EndUTF8Byte <= evidence.StartUTF8Byte ||
			evidence.EndUTF8Byte-evidence.StartUTF8Byte !=
				len(evidence.OriginalExcerpt) ||
			!validInterviewText(
				evidence.OriginalExcerpt,
				interviewShadowMaximumTextBytes,
			) {
			return false
		}
	}
	return true
}

func validInterviewQuestionFindingRefs(
	refs []string,
	allowed map[string]struct{},
) bool {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, exists := allowed[ref]; !exists {
			return false
		}
		if _, duplicate := seen[ref]; duplicate {
			return false
		}
		seen[ref] = struct{}{}
	}
	return true
}

func interviewQuestionFindingsOverlapEvidence(
	findingIDs []string,
	questionEvidenceRefIDs []string,
	findingEvidenceRefs map[string]map[string]struct{},
) bool {
	questionRefs := make(
		map[string]struct{},
		len(questionEvidenceRefIDs),
	)
	for _, refID := range questionEvidenceRefIDs {
		questionRefs[refID] = struct{}{}
	}
	for _, findingID := range findingIDs {
		overlaps := false
		for refID := range findingEvidenceRefs[findingID] {
			if _, exists := questionRefs[refID]; exists {
				overlaps = true
				break
			}
		}
		if !overlaps {
			return false
		}
	}
	return true
}

func validInterviewReportIDList(values []string, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validIdentifier(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
