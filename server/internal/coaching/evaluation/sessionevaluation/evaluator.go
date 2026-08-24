package sessionevaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

var ErrProviderResponse error = providerResponseFailure{
	reason: normalizeReasonNormalizedReportInvalid,
}

const pipelineVersion = "session-evaluation/v1"

const minimumPronunciationCoverage = 0.6

const (
	interviewPromptVersionV1 = "interview-report/v1"
	interviewPromptVersionV2 = "interview-report/v2"
	ieltsPromptVersionV1     = "ielts-report/v1"
	ieltsPromptVersionV2     = "ielts-report/v2"
	ieltsPromptVersionV3     = "ielts-report/v3"
	ieltsPromptVersionV4     = "ielts-report/v4"
	generalPromptVersionV1   = "general-report/v1"
	generalPromptVersionV2   = "general-report/v2"

	ieltsInputSchemaVersionV4            = "ielts-report-input/v4"
	ieltsInputCumulativeParts12PlusPart3 = "CUMULATIVE_PARTS_1_2_PLUS_PART_3"
	ieltsInputFullRawFallback            = "FULL_RAW_FALLBACK"
)

func Lineages(
	provider string,
	model string,
) (evaluation.SessionLineages, error) {
	lineages := evaluation.SessionLineages{
		Interview: evaluation.ConfigLineage{
			SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
			StrategyRef:     evaluation.InterviewStrategyRef,
			PipelineVersion: pipelineVersion,
			PromptVersion:   interviewPromptVersionV2,
			ResultSchema:    report.FormalReportSchemaVersion,
			Provider:        provider,
			Model:           model,
		},
		IELTS: evaluation.ConfigLineage{
			SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
			StrategyRef:     evaluation.IELTSStrategyRef,
			PipelineVersion: pipelineVersion,
			PromptVersion:   ieltsPromptVersionV4,
			ResultSchema:    report.FormalReportSchemaVersion,
			Provider:        provider,
			Model:           model,
		},
		IELTSPractice: evaluation.ConfigLineage{
			SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
			StrategyRef:     evaluation.IELTSStrategyRef,
			PipelineVersion: pipelineVersion,
			PromptVersion:   ieltsPromptVersionV2,
			ResultSchema:    report.FormalReportSchemaVersion,
			Provider:        provider,
			Model:           model,
		},
		General: evaluation.ConfigLineage{
			SchemaVersion:   evaluation.ConfigLineageSchemaVersion,
			StrategyRef:     evaluation.GeneralStrategyRef,
			PipelineVersion: pipelineVersion,
			PromptVersion:   generalPromptVersionV2,
			ResultSchema:    report.FormalReportSchemaVersion,
			Provider:        provider,
			Model:           model,
		},
	}
	if !lineages.Valid() {
		return evaluation.SessionLineages{}, evaluation.ErrInvalidRequest
	}
	return lineages, nil
}

// Evaluators is only the worker-facing bundle. Each product policy below has
// its own evaluator type, prompt and result rules; the bundle does not select
// a strategy dynamically.
type Evaluators struct {
	interview *InterviewEvaluator
	ielts     *IELTSEvaluator
	general   *GeneralEvaluator
}

type InterviewEvaluator struct{ generator textgeneration.Generator }
type IELTSEvaluator struct{ generator textgeneration.Generator }
type GeneralEvaluator struct{ generator textgeneration.Generator }

func New(generator textgeneration.Generator) (*Evaluators, error) {
	if generator == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &Evaluators{
		interview: &InterviewEvaluator{generator: generator},
		ielts:     &IELTSEvaluator{generator: generator},
		general:   &GeneralEvaluator{generator: generator},
	}, nil
}

func (evaluators *Evaluators) EvaluateInterview(
	ctx context.Context,
	record evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	if evaluators == nil || evaluators.interview == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return evaluators.interview.Evaluate(ctx, record, snapshot, lineage)
}

func (evaluators *Evaluators) EvaluateIELTS(
	ctx context.Context,
	record evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	if evaluators == nil || evaluators.ielts == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return evaluators.ielts.Evaluate(ctx, record, snapshot, lineage)
}

func (evaluators *Evaluators) EvaluateGeneral(
	ctx context.Context,
	record evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	if evaluators == nil || evaluators.general == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return evaluators.general.Evaluate(ctx, record, snapshot, lineage)
}

func (evaluator *InterviewEvaluator) Evaluate(
	ctx context.Context,
	_ evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	prompt, err := selectReportPrompt(
		lineage.PromptVersion,
		interviewPromptVersionV1,
		interviewSystemPromptV1,
		interviewPromptVersionV2,
		interviewSystemPromptV2,
	)
	if err != nil {
		return nil, err
	}
	return evaluate(ctx, evaluator.generator, snapshot, lineage,
		prompt.system, prompt.insufficientSummary, evaluation.SceneInterview,
		[]string{
			"INTERVIEW_RELEVANCE",
			"INTERVIEW_STRUCTURE",
			"INTERVIEW_EVIDENCE",
			"INTERVIEW_PROFESSIONAL",
			"INTERVIEW_INTERACTION",
		}, report.ReportScalePercentage100, false, nil)
}

func (evaluator *IELTSEvaluator) Evaluate(
	ctx context.Context,
	_ evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	prompt, err := selectReportPrompt(
		lineage.PromptVersion,
		ieltsPromptVersionV1,
		ieltsSystemPromptV1,
		ieltsPromptVersionV2,
		ieltsSystemPromptV2,
	)
	if lineage.PromptVersion == ieltsPromptVersionV3 {
		prompt = reportPrompt{
			system:              ieltsSystemPromptV3,
			insufficientSummary: "本次练习的有效证据不足，暂时无法形成可靠的评估结论。",
		}
		err = nil
	}
	if lineage.PromptVersion == ieltsPromptVersionV4 {
		prompt = reportPrompt{
			system:              ieltsSystemPromptV4,
			insufficientSummary: "本次练习的有效证据不足，暂时无法形成可靠的评估结论。",
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	pronunciationAssessed, _, _ := pronunciationAvailability(snapshot)
	dimensions := []string{
		"FLUENCY_COHERENCE",
		"LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY",
	}
	if pronunciationAssessed {
		dimensions = append(dimensions, "PRONUNCIATION")
	}
	var payloadOverride any
	if lineage.PromptVersion == ieltsPromptVersionV4 {
		payloadOverride, err = ieltsV4Payload(snapshot, dimensions)
		if err != nil {
			return nil, err
		}
	} else if snapshot.CumulativeProfile != nil {
		payloadOverride, err = incrementalIELTSPayload(snapshot, dimensions)
		if err != nil {
			return nil, err
		}
	}
	return evaluate(ctx, evaluator.generator, snapshot, lineage,
		prompt.system, prompt.insufficientSummary, evaluation.SceneIELTSSpeaking,
		dimensions, report.ReportScaleIELTSBand, true, payloadOverride)
}

func (evaluator *GeneralEvaluator) Evaluate(
	ctx context.Context,
	_ evaluation.Record,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
) (json.RawMessage, error) {
	prompt, err := selectReportPrompt(
		lineage.PromptVersion,
		generalPromptVersionV1,
		generalSystemPromptV1,
		generalPromptVersionV2,
		generalSystemPromptV2,
	)
	if err != nil {
		return nil, err
	}
	var sceneType evaluation.SceneType
	switch snapshot.EvaluationPolicyRef {
	case evaluation.WorkplaceEvaluationPolicyRef:
		if snapshot.SceneCategory != "WORKPLACE_GENERAL" {
			return nil, evaluation.ErrInvalidRequest
		}
		sceneType = evaluation.SceneOverseasWorkplace
	case evaluation.DailyEvaluationPolicyRef:
		if snapshot.SceneCategory != "LIFE_DAILY" &&
			snapshot.SceneCategory != "LIFE_TRAVEL" {
			return nil, evaluation.ErrInvalidRequest
		}
		sceneType = evaluation.SceneOverseasDaily
	default:
		return nil, evaluation.ErrInvalidRequest
	}
	return evaluate(ctx, evaluator.generator, snapshot, lineage,
		prompt.system, prompt.insufficientSummary, sceneType,
		[]string{
			"TASK_ACHIEVEMENT",
			"CLARITY_COHERENCE",
			"LANGUAGE_CONTROL",
			"INTERACTION",
		}, report.ReportScalePercentage100, false, nil)
}

func evaluate(
	ctx context.Context,
	generator textgeneration.Generator,
	snapshot evaluation.SessionInputSnapshot,
	lineage evaluation.ConfigLineage,
	systemPrompt string,
	insufficientSummary string,
	sceneType evaluation.SceneType,
	dimensionKeys []string,
	scale report.ReportScoreScale,
	allowRepair bool,
	payloadOverride any,
) (json.RawMessage, error) {
	if generator == nil || ctx == nil || strings.TrimSpace(systemPrompt) == "" ||
		strings.TrimSpace(insufficientSummary) == "" ||
		!snapshot.Valid() || !lineage.Valid() || len(dimensionKeys) == 0 {
		return nil, evaluation.ErrInvalidRequest
	}
	effectiveTurns := make([]evaluation.SessionEvidenceTurn, 0, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if turn.Effective {
			effectiveTurns = append(effectiveTurns, turn)
		}
	}
	if len(effectiveTurns) == 0 {
		return encodeReport(insufficientReport(
			snapshot, sceneType, dimensionKeys, scale, insufficientSummary,
		))
	}
	payloadSource := payloadOverride
	if payloadSource == nil {
		payloadSource = providerInput{
			SceneType: sceneType, PracticeMode: snapshot.PracticeMode,
			DimensionKeys: dimensionKeys, Questions: snapshot.Questions,
			Turns: effectiveTurns,
		}
	}
	payload, err := json.Marshal(payloadSource)
	if err != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	generated, err := generator.Generate(ctx, textgeneration.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   string(payload),
	})
	if err != nil {
		return nil, err
	}
	formal, normalizeErr := normalizeProviderReport(
		generated, snapshot, sceneType, dimensionKeys, scale,
		lineage.Provider, lineage.Model,
	)
	if normalizeErr == nil {
		return encodeReport(formal)
	}
	if !allowRepair {
		return nil, normalizeErr
	}
	repairPayload, err := json.Marshal(struct {
		Input          json.RawMessage `json:"input"`
		RejectedOutput string          `json:"rejected_output"`
		Violation      normalizeReason `json:"violation"`
		Instruction    string          `json:"instruction"`
	}{
		Input:          payload,
		RejectedOutput: generated.Content,
		Violation:      normalizeReasonFromError(normalizeErr),
		Instruction: "Return a complete corrected JSON object that obeys the contract exactly. " +
			"For INSUFFICIENT, every score must be null and priority_actions must be empty. " +
			"For PROVISIONAL, each priority action must reference an existing improvement in the same dimension.",
	})
	if err != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	repaired, err := generator.Generate(ctx, textgeneration.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   string(repairPayload),
	})
	if err != nil {
		return nil, err
	}
	formal, err = normalizeProviderReport(
		repaired, snapshot, sceneType, dimensionKeys, scale,
		lineage.Provider, lineage.Model,
	)
	if err != nil {
		return nil, err
	}
	return encodeReport(formal)
}

type providerInput struct {
	SceneType     evaluation.SceneType                 `json:"scene_type"`
	PracticeMode  string                               `json:"practice_mode"`
	DimensionKeys []string                             `json:"dimension_keys"`
	Questions     []evaluation.SessionEvidenceQuestion `json:"questions"`
	Turns         []evaluation.SessionEvidenceTurn     `json:"effective_turns"`
}

type incrementalIELTSProviderInput struct {
	SceneType         evaluation.SceneType                 `json:"scene_type"`
	PracticeMode      string                               `json:"practice_mode"`
	DimensionKeys     []string                             `json:"dimension_keys"`
	CumulativeProfile evaluation.IELTSCumulativeProfile    `json:"cumulative_profile"`
	Questions         []evaluation.SessionEvidenceQuestion `json:"part_3_questions"`
	Turns             []evaluation.SessionEvidenceTurn     `json:"part_3_effective_turns"`
}

type resolvedIELTSProviderInputV4 struct {
	SchemaVersion     string                               `json:"schema_version"`
	EvidenceMode      string                               `json:"evidence_mode"`
	SceneType         evaluation.SceneType                 `json:"scene_type"`
	PracticeMode      string                               `json:"practice_mode"`
	DimensionKeys     []string                             `json:"dimension_keys"`
	CumulativeProfile evaluation.IELTSCumulativeProfile    `json:"cumulative_profile"`
	Questions         []evaluation.SessionEvidenceQuestion `json:"part_3_questions"`
	Turns             []evaluation.SessionEvidenceTurn     `json:"part_3_effective_turns"`
}

type fallbackIELTSProviderInputV4 struct {
	SchemaVersion string                               `json:"schema_version"`
	EvidenceMode  string                               `json:"evidence_mode"`
	SceneType     evaluation.SceneType                 `json:"scene_type"`
	PracticeMode  string                               `json:"practice_mode"`
	DimensionKeys []string                             `json:"dimension_keys"`
	Questions     []evaluation.SessionEvidenceQuestion `json:"questions"`
	Turns         []evaluation.SessionEvidenceTurn     `json:"effective_turns"`
}

func ieltsV4Payload(
	snapshot evaluation.SessionInputSnapshot,
	dimensionKeys []string,
) (any, error) {
	switch snapshot.ProfileResolution {
	case evaluation.IELTSFinalProfileResolved:
		incremental, err := incrementalIELTSPayload(snapshot, dimensionKeys)
		if err != nil {
			return nil, err
		}
		return resolvedIELTSProviderInputV4{
			SchemaVersion: ieltsInputSchemaVersionV4,
			EvidenceMode:  ieltsInputCumulativeParts12PlusPart3,
			SceneType:     incremental.SceneType, PracticeMode: incremental.PracticeMode,
			DimensionKeys:     incremental.DimensionKeys,
			CumulativeProfile: incremental.CumulativeProfile,
			Questions:         incremental.Questions, Turns: incremental.Turns,
		}, nil
	case evaluation.IELTSFinalProfileFallback:
		effectiveTurns := make([]evaluation.SessionEvidenceTurn, 0, len(snapshot.Turns))
		for _, turn := range snapshot.Turns {
			if turn.Effective {
				effectiveTurns = append(effectiveTurns, turn)
			}
		}
		return fallbackIELTSProviderInputV4{
			SchemaVersion: ieltsInputSchemaVersionV4,
			EvidenceMode:  ieltsInputFullRawFallback,
			SceneType:     evaluation.SceneIELTSSpeaking,
			PracticeMode:  snapshot.PracticeMode, DimensionKeys: dimensionKeys,
			Questions: snapshot.Questions, Turns: effectiveTurns,
		}, nil
	default:
		return nil, evaluation.ErrInvalidRequest
	}
}

func incrementalIELTSPayload(
	snapshot evaluation.SessionInputSnapshot,
	dimensionKeys []string,
) (incrementalIELTSProviderInput, error) {
	if snapshot.CumulativeProfile == nil ||
		!snapshot.CumulativeProfile.Valid() {
		return incrementalIELTSProviderInput{}, evaluation.ErrInvalidRequest
	}
	var plan struct {
		IELTSAssignment *struct {
			Parts []struct {
				TurnBlueprints []string `json:"turn_blueprints"`
			} `json:"parts"`
		} `json:"ielts_assignment"`
	}
	if json.Unmarshal(snapshot.PlanSnapshot, &plan) != nil ||
		plan.IELTSAssignment == nil || len(plan.IELTSAssignment.Parts) != 3 {
		return incrementalIELTSProviderInput{}, evaluation.ErrInvalidRequest
	}
	part2Boundary := len(plan.IELTSAssignment.Parts[0].TurnBlueprints) +
		len(plan.IELTSAssignment.Parts[1].TurnBlueprints)
	effectivePosition := 0
	turns := make([]evaluation.SessionEvidenceTurn, 0)
	questionIDs := make(map[string]struct{})
	for _, turn := range snapshot.Turns {
		if !turn.Effective {
			continue
		}
		effectivePosition++
		if effectivePosition <= part2Boundary {
			continue
		}
		turns = append(turns, turn)
		questionIDs[turn.QuestionID] = struct{}{}
	}
	if len(turns) == 0 {
		return incrementalIELTSProviderInput{}, evaluation.ErrInvalidRequest
	}
	questions := make([]evaluation.SessionEvidenceQuestion, 0, len(questionIDs))
	for _, question := range snapshot.Questions {
		if _, exists := questionIDs[question.ID]; exists {
			questions = append(questions, question)
		}
	}
	return incrementalIELTSProviderInput{
		SceneType:         evaluation.SceneIELTSSpeaking,
		PracticeMode:      snapshot.PracticeMode,
		DimensionKeys:     dimensionKeys,
		CumulativeProfile: *snapshot.CumulativeProfile,
		Questions:         questions, Turns: turns,
	}, nil
}

type providerReport struct {
	ScoreabilityStatus report.ReportScoreability `json:"scoreability_status"`
	Summary            string                    `json:"summary"`
	Dimensions         []providerDimension       `json:"dimensions"`
	PriorityActions    []providerPriorityAction  `json:"priority_actions"`
}

type providerDimension struct {
	Key          string            `json:"key"`
	Score        *float64          `json:"score"`
	Coverage     float64           `json:"coverage"`
	Confidence   float64           `json:"confidence"`
	ReasonCodes  []string          `json:"reason_codes"`
	Strengths    []providerFinding `json:"strengths"`
	Improvements []providerFinding `json:"improvements"`
	Examples     []providerFinding `json:"recommended_examples"`
}

type providerFinding struct {
	Message    string             `json:"message"`
	Suggestion string             `json:"suggestion"`
	Evidence   []providerEvidence `json:"evidence"`
}

type providerEvidence struct {
	TurnID     string `json:"turn_id"`
	Quote      string `json:"quote"`
	Occurrence int    `json:"occurrence"`
}

type providerPriorityAction struct {
	DimensionKey     string `json:"dimension_key"`
	ImprovementIndex int    `json:"improvement_index"`
}

func normalizeProviderReport(
	generated textgeneration.Result,
	snapshot evaluation.SessionInputSnapshot,
	sceneType evaluation.SceneType,
	dimensionKeys []string,
	scale report.ReportScoreScale,
	expectedProvider string,
	expectedModel string,
) (report.FormalReport, error) {
	if generated.Provider != expectedProvider || generated.Model != expectedModel ||
		generated.RequestID == "" || len(generated.Content) == 0 ||
		len(generated.Content) > 256*1024 {
		return report.FormalReport{}, providerResponseError(
			normalizeReasonResponseMetadataInvalid,
		)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(generated.Content))
	decoder.DisallowUnknownFields()
	var provided providerReport
	if err := decoder.Decode(&provided); err != nil {
		return report.FormalReport{}, providerResponseError(
			normalizeReasonResponseJSONInvalid,
		)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return report.FormalReport{}, providerResponseError(
			normalizeReasonResponseJSONInvalid,
		)
	}
	if len(provided.Dimensions) != len(dimensionKeys) {
		return report.FormalReport{}, providerResponseError(
			normalizeReasonDimensionCountInvalid,
		)
	}
	turns := make(map[string]string, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if turn.Effective {
			turns[turn.ID] = turn.Transcript
		}
	}
	formal := report.FormalReport{
		SchemaVersion:      report.FormalReportSchemaVersion,
		SceneType:          sceneType,
		PracticeExperience: snapshot.PracticeExperience,
		SceneCategory:      snapshot.SceneCategory,
		PracticeMode:       snapshot.PracticeMode,
		ScoreabilityStatus: provided.ScoreabilityStatus,
		Summary:            strings.TrimSpace(provided.Summary),
		Questions:          reportQuestions(snapshot),
		Dimensions:         make([]report.ReportDimension, len(dimensionKeys)),
		PriorityActions:    []report.ReportPriorityAction{},
	}
	improvementIDs := make(map[string][]string, len(dimensionKeys))
	for index, expectedKey := range dimensionKeys {
		providedDimension := provided.Dimensions[index]
		if providedDimension.Key != expectedKey {
			return report.FormalReport{}, providerResponseError(
				normalizeReasonDimensionOrderInvalid,
			)
		}
		score := providedDimension.Score
		if provided.ScoreabilityStatus == report.ReportScoreabilityInsufficient &&
			score != nil {
			score = nil
		}
		dimension := report.ReportDimension{
			Key:          expectedKey,
			Score:        score,
			Scale:        scale,
			Coverage:     providedDimension.Coverage,
			Confidence:   providedDimension.Confidence,
			ReasonCodes:  slices.Clone(providedDimension.ReasonCodes),
			EvidenceRefs: []string{},
			Strengths:    []report.ReportFinding{},
			Improvements: []report.ReportFinding{},
			Examples:     []report.ReportFinding{},
		}
		var err error
		dimension.Strengths, err = normalizeFindings(
			expectedKey, "strength", providedDimension.Strengths, turns,
		)
		if err != nil {
			return report.FormalReport{}, err
		}
		dimension.Improvements, err = normalizeFindings(
			expectedKey, "improvement", providedDimension.Improvements, turns,
		)
		if err != nil {
			return report.FormalReport{}, err
		}
		dimension.Examples, err = normalizeFindings(
			expectedKey, "example", providedDimension.Examples, turns,
		)
		if err != nil {
			return report.FormalReport{}, err
		}
		seenRefs := make(map[string]struct{})
		for _, collection := range [][]report.ReportFinding{
			dimension.Strengths, dimension.Improvements, dimension.Examples,
		} {
			for _, finding := range collection {
				for _, item := range finding.Evidence {
					if _, seen := seenRefs[item.EvidenceRefID]; !seen {
						seenRefs[item.EvidenceRefID] = struct{}{}
						dimension.EvidenceRefs = append(dimension.EvidenceRefs, item.EvidenceRefID)
					}
				}
			}
		}
		improvementIDs[expectedKey] = make([]string, len(dimension.Improvements))
		for itemIndex, finding := range dimension.Improvements {
			improvementIDs[expectedKey][itemIndex] = finding.ID
		}
		formal.Dimensions[index] = dimension
	}
	// Evidence-insufficient reports must not expose provider-generated
	// priorities, even when those references happen to resolve.
	if provided.ScoreabilityStatus != report.ReportScoreabilityInsufficient {
		for _, action := range provided.PriorityActions {
			ids, exists := improvementIDs[action.DimensionKey]
			if !exists || action.ImprovementIndex < 1 || action.ImprovementIndex > len(ids) {
				return report.FormalReport{}, providerResponseError(
					normalizeReasonPriorityActionInvalid,
				)
			}
			formal.PriorityActions = append(formal.PriorityActions, report.ReportPriorityAction{
				DimensionKey: action.DimensionKey,
				FindingID:    ids[action.ImprovementIndex-1],
			})
		}
	}
	if sceneType == evaluation.SceneIELTSSpeaking &&
		slices.Contains(dimensionKeys, "PRONUNCIATION") {
		available, coverage, _ := pronunciationAvailability(snapshot)
		if !available {
			return report.FormalReport{}, providerResponseError(
				normalizeReasonPronunciationCoverageChanged,
			)
		}
		for index := range formal.Dimensions {
			if formal.Dimensions[index].Key != "PRONUNCIATION" {
				continue
			}
			formal.Dimensions[index].Coverage = coverage
			formal.Dimensions[index].Confidence = math.Min(
				formal.Dimensions[index].Confidence,
				coverage,
			)
			if coverage < 1 &&
				!slices.Contains(
					formal.Dimensions[index].ReasonCodes,
					"PARTIAL_ACOUSTIC_COVERAGE",
				) {
				formal.Dimensions[index].ReasonCodes = append(
					formal.Dimensions[index].ReasonCodes,
					"PARTIAL_ACOUSTIC_COVERAGE",
				)
			}
			break
		}
	}
	if sceneType == evaluation.SceneIELTSSpeaking &&
		!slices.Contains(dimensionKeys, "PRONUNCIATION") {
		_, _, reason := pronunciationAvailability(snapshot)
		formal.Dimensions = append(formal.Dimensions, report.ReportDimension{
			Key:          "PRONUNCIATION",
			Scale:        report.ReportScaleIELTSBand,
			Coverage:     0,
			Confidence:   0,
			ReasonCodes:  []string{reason},
			EvidenceRefs: []string{},
			Strengths:    []report.ReportFinding{},
			Improvements: []report.ReportFinding{},
			Examples:     []report.ReportFinding{},
		})
	}
	if !formal.Valid() {
		return report.FormalReport{}, providerResponseError(
			normalizeReasonNormalizedReportInvalid,
		)
	}
	return formal, nil
}

func reportQuestions(
	snapshot evaluation.SessionInputSnapshot,
) []report.ReportQuestion {
	answers := make(map[string]report.ReportAnswer, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if !turn.Effective {
			continue
		}
		answers[turn.QuestionID] = report.ReportAnswer{
			TurnID:     turn.ID,
			Transcript: turn.Transcript,
		}
	}
	questions := make([]report.ReportQuestion, len(snapshot.Questions))
	for index, question := range snapshot.Questions {
		questions[index] = report.ReportQuestion{
			ID:               question.ID,
			Position:         question.Position,
			ParentQuestionID: question.ParentQuestionID,
			Text:             question.Text,
		}
		if answer, exists := answers[question.ID]; exists {
			answerCopy := answer
			questions[index].Answer = &answerCopy
		}
	}
	return questions
}

func pronunciationAvailability(
	snapshot evaluation.SessionInputSnapshot,
) (bool, float64, string) {
	if snapshot.AcousticCapability == evaluation.AcousticCapabilityNotConfigured {
		return false, 0, "ACOUSTIC_ASSESSMENT_NOT_CONFIGURED"
	}
	effectiveTurns := 0
	assessedTurns := 0
	failureReason := "ACOUSTIC_ASSESSMENT_FAILED"
	for _, turn := range snapshot.Turns {
		if !turn.Effective {
			continue
		}
		effectiveTurns++
		if turn.AudioAssetID == "" {
			failureReason = "PRACTICE_TURN_AUDIO_UNAVAILABLE"
			continue
		}
		if turn.Acoustic == nil {
			continue
		}
		if turn.Acoustic.Status == evaluation.AcousticNotAssessed {
			failureReason = turn.Acoustic.Reason
			continue
		}
		if turn.Acoustic.Status != evaluation.AcousticAssessed {
			continue
		}
		assessedTurns++
	}
	if effectiveTurns == 0 {
		return false, 0, "NO_EFFECTIVE_TURNS"
	}
	coverage := float64(assessedTurns) / float64(effectiveTurns)
	if coverage < minimumPronunciationCoverage {
		return false, coverage, failureReason
	}
	return true, coverage, ""
}

func normalizeFindings(
	dimensionKey string,
	kind string,
	provided []providerFinding,
	turns map[string]string,
) ([]report.ReportFinding, error) {
	if provided == nil || len(provided) > 5 {
		return nil, providerResponseError(normalizeReasonFindingCountInvalid)
	}
	result := make([]report.ReportFinding, len(provided))
	for index, item := range provided {
		finding := report.ReportFinding{
			ID:         strings.ToLower(dimensionKey) + "." + kind + fmt.Sprintf(".%d", index+1),
			Message:    strings.TrimSpace(item.Message),
			Suggestion: strings.TrimSpace(item.Suggestion),
			Evidence:   make([]report.ReportEvidence, len(item.Evidence)),
		}
		if len(item.Evidence) > 8 {
			return nil, providerResponseError(normalizeReasonEvidenceCountInvalid)
		}
		for evidenceIndex, source := range item.Evidence {
			transcript, exists := turns[source.TurnID]
			quote := strings.TrimSpace(source.Quote)
			start := byteOccurrence(transcript, quote, source.Occurrence)
			if !exists || source.Occurrence < 1 || start < 0 {
				return nil, providerResponseError(normalizeReasonEvidenceInvalid)
			}
			finding.Evidence[evidenceIndex] = report.ReportEvidence{
				EvidenceRefID:   source.TurnID,
				TurnID:          source.TurnID,
				StartUTF8Byte:   start,
				EndUTF8Byte:     start + len(quote),
				OriginalExcerpt: quote,
			}
		}
		result[index] = finding
	}
	return result, nil
}

func byteOccurrence(text string, quote string, occurrence int) int {
	if quote == "" || occurrence < 1 {
		return -1
	}
	offset := 0
	for count := 1; count <= occurrence; count++ {
		index := strings.Index(text[offset:], quote)
		if index < 0 {
			return -1
		}
		offset += index
		if count == occurrence {
			return offset
		}
		offset += len(quote)
	}
	return -1
}

func insufficientReport(
	snapshot evaluation.SessionInputSnapshot,
	sceneType evaluation.SceneType,
	dimensionKeys []string,
	scale report.ReportScoreScale,
	summary string,
) report.FormalReport {
	dimensions := make([]report.ReportDimension, 0, len(dimensionKeys)+1)
	for _, key := range dimensionKeys {
		dimensions = append(dimensions, report.ReportDimension{
			Key: key, Scale: scale, Coverage: 0, Confidence: 0,
			ReasonCodes: []string{"NO_EFFECTIVE_TURNS"}, EvidenceRefs: []string{},
			Strengths: []report.ReportFinding{}, Improvements: []report.ReportFinding{},
			Examples: []report.ReportFinding{},
		})
	}
	if sceneType == evaluation.SceneIELTSSpeaking &&
		!slices.Contains(dimensionKeys, "PRONUNCIATION") {
		_, _, reason := pronunciationAvailability(snapshot)
		dimensions = append(dimensions, report.ReportDimension{
			Key: "PRONUNCIATION", Scale: scale, Coverage: 0, Confidence: 0,
			ReasonCodes: []string{reason}, EvidenceRefs: []string{},
			Strengths: []report.ReportFinding{}, Improvements: []report.ReportFinding{},
			Examples: []report.ReportFinding{},
		})
	}
	return report.FormalReport{
		SchemaVersion: report.FormalReportSchemaVersion,
		SceneType:     sceneType, PracticeExperience: snapshot.PracticeExperience,
		SceneCategory: snapshot.SceneCategory, PracticeMode: snapshot.PracticeMode,
		ScoreabilityStatus: report.ReportScoreabilityInsufficient,
		Summary:            summary,
		Questions:          reportQuestions(snapshot),
		Dimensions:         dimensions, PriorityActions: []report.ReportPriorityAction{},
	}
}

func encodeReport(value report.FormalReport) (json.RawMessage, error) {
	if !value.Valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	encoded, _, err := evaluation.EncodeStrict(value)
	return encoded, err
}

type reportPrompt struct {
	system              string
	insufficientSummary string
}

func selectReportPrompt(
	recordedVersion string,
	v1Version string,
	v1System string,
	v2Version string,
	v2System string,
) (reportPrompt, error) {
	switch recordedVersion {
	case v1Version:
		return reportPrompt{
			system:              v1System,
			insufficientSummary: "There is not enough confirmed practice evidence to produce a reliable evaluation.",
		}, nil
	case v2Version:
		return reportPrompt{
			system:              v2System,
			insufficientSummary: "本次练习的有效证据不足，暂时无法形成可靠的评估结论。",
		}, nil
	default:
		return reportPrompt{}, evaluation.ErrInvalidRequest
	}
}

const interviewSystemPromptV1 = `You are an evidence-bound job interview English evaluator. Score only the requested interview dimensions on PERCENTAGE_100. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Do not infer voice qualities from text.`

const interviewSystemPromptV2 = `You are an evidence-bound job interview English evaluator. Score only the requested interview dimensions on PERCENTAGE_100. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Write summary and every finding message in Simplified Chinese. Write suggestions for strengths and improvements in Simplified Chinese. For each recommended_examples finding, write its message in Simplified Chinese and put only the directly reusable English expression in suggestion. Keep every question, answer, and evidence quote in its original language; never translate or rewrite them. Do not infer voice qualities from text.`

const ieltsSystemPromptV1 = `You are an evidence-bound IELTS Speaking practice evaluator. Score every requested dimension on IELTS_BAND_9 using half-band increments. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. PRONUNCIATION is requested only when assessed acoustic checkpoints are present on effective turns; base that dimension on those checkpoints and their coverage, never on transcript spelling.`

const ieltsSystemPromptV2 = `You are an evidence-bound IELTS Speaking practice evaluator. Score every requested dimension on IELTS_BAND_9 using half-band increments. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Write summary and every finding message in Simplified Chinese. Write suggestions for strengths and improvements in Simplified Chinese. For each recommended_examples finding, write its message in Simplified Chinese and put only the directly reusable English expression in suggestion. Keep every question, answer, and evidence quote in its original language; never translate or rewrite them. PRONUNCIATION is requested only when assessed acoustic checkpoints are present on effective turns; base that dimension on those checkpoints and their coverage, never on transcript spelling.`

const ieltsSystemPromptV3 = `You are an evidence-bound IELTS Speaking practice evaluator. Score the candidate's average performance across the whole test, not separate Part scores. The input may contain a provisional cumulative_profile for Parts 1 and 2 plus raw Part 3 evidence. Treat the profile as provisional evidence: preserve its exact cited quotes, then recalibrate every requested dimension using Part 3. Never mechanically average Parts or copy provisional bands without reconsideration. Score every requested dimension on IELTS_BAND_9 using half-band increments. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote present either in cumulative_profile evidence or Part 3 turns, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Write summary and every finding message in Simplified Chinese. Write suggestions for strengths and improvements in Simplified Chinese. For each recommended_examples finding, write its message in Simplified Chinese and put only the directly reusable English expression in suggestion. Keep every question, answer, and evidence quote in its original language; never translate or rewrite them. Base PRONUNCIATION only on acoustic checkpoints and the provisional pronunciation profile, never on transcript spelling.`

const ieltsSystemPromptV4 = `You are an evidence-bound IELTS Speaking practice evaluator. The input schema_version is ielts-report-input/v4 and evidence_mode is exactly one of CUMULATIVE_PARTS_1_2_PLUS_PART_3 or FULL_RAW_FALLBACK. For CUMULATIVE_PARTS_1_2_PLUS_PART_3, score the candidate's average performance across the whole test using the provisional cumulative_profile for Parts 1 and 2 plus only part_3_questions and part_3_effective_turns as raw evidence. Preserve exact profile quotes, then recalibrate every requested dimension using Part 3; never mechanically average Parts or copy provisional bands without reconsideration. For FULL_RAW_FALLBACK, use only questions and effective_turns as the complete raw evidence for all Parts. Never infer or combine fields from the other evidence mode. Score every requested dimension on IELTS_BAND_9 using half-band increments. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote present in the evidence allowed by evidence_mode, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Write summary and every finding message in Simplified Chinese. Write suggestions for strengths and improvements in Simplified Chinese. For each recommended_examples finding, write its message in Simplified Chinese and put only the directly reusable English expression in suggestion. Keep every question, answer, and evidence quote in its original language; never translate or rewrite them. Base PRONUNCIATION only on acoustic checkpoints and, in CUMULATIVE_PARTS_1_2_PLUS_PART_3 mode, the provisional pronunciation profile; never on transcript spelling.`

const generalSystemPromptV1 = `You are an evidence-bound everyday or workplace English evaluator. Score only the requested communication dimensions on PERCENTAGE_100. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Do not infer voice qualities from text.`

const generalSystemPromptV2 = `You are an evidence-bound everyday or workplace English evaluator. Score only the requested communication dimensions on PERCENTAGE_100. Return one JSON object only, with exactly: scoreability_status, summary, dimensions, priority_actions. Use dimension_keys in the input in the same order. Each dimension must contain key, score, coverage, confidence, reason_codes, strengths, improvements, recommended_examples. Each finding must contain message, suggestion, evidence. Each evidence item must contain turn_id, an exact quote copied from that turn, and its 1-based occurrence. priority_actions contains dimension_key and 1-based improvement_index. Arrays must be present even when empty. Write summary and every finding message in Simplified Chinese. Write suggestions for strengths and improvements in Simplified Chinese. For each recommended_examples finding, write its message in Simplified Chinese and put only the directly reusable English expression in suggestion. Keep every question, answer, and evidence quote in its original language; never translate or rewrite them. Do not infer voice qualities from text.`

type normalizeReason string

const (
	normalizeReasonResponseMetadataInvalid      normalizeReason = "response_metadata_invalid"
	normalizeReasonResponseJSONInvalid          normalizeReason = "response_json_invalid"
	normalizeReasonDimensionCountInvalid        normalizeReason = "dimension_count_invalid"
	normalizeReasonDimensionOrderInvalid        normalizeReason = "dimension_order_invalid"
	normalizeReasonFindingCountInvalid          normalizeReason = "finding_count_invalid"
	normalizeReasonEvidenceCountInvalid         normalizeReason = "evidence_count_invalid"
	normalizeReasonEvidenceInvalid              normalizeReason = "evidence_invalid"
	normalizeReasonPriorityActionInvalid        normalizeReason = "priority_action_invalid"
	normalizeReasonPronunciationCoverageChanged normalizeReason = "pronunciation_coverage_changed"
	normalizeReasonNormalizedReportInvalid      normalizeReason = "normalized_report_invalid"
)

func (reason normalizeReason) valid() bool {
	switch reason {
	case normalizeReasonResponseMetadataInvalid,
		normalizeReasonResponseJSONInvalid,
		normalizeReasonDimensionCountInvalid,
		normalizeReasonDimensionOrderInvalid,
		normalizeReasonFindingCountInvalid,
		normalizeReasonEvidenceCountInvalid,
		normalizeReasonEvidenceInvalid,
		normalizeReasonPriorityActionInvalid,
		normalizeReasonPronunciationCoverageChanged,
		normalizeReasonNormalizedReportInvalid:
		return true
	default:
		return false
	}
}

func providerResponseError(reason normalizeReason) error {
	if !reason.valid() {
		reason = normalizeReasonNormalizedReportInvalid
	}
	return providerResponseFailure{reason: reason}
}

func normalizeReasonFromError(err error) normalizeReason {
	var failure providerResponseFailure
	if errors.As(err, &failure) && failure.reason.valid() {
		return failure.reason
	}
	return normalizeReasonNormalizedReportInvalid
}

type providerResponseFailure struct{ reason normalizeReason }

func (providerResponseFailure) Error() string {
	return "evaluation: report provider response invalid"
}
func (providerResponseFailure) StableCategory() string { return "PROVIDER_RESPONSE_INVALID" }
func (providerResponseFailure) Retryable() bool        { return true }
func (failure providerResponseFailure) EvaluationNormalizeReason() string {
	return string(failure.reason)
}
func (providerResponseFailure) Is(target error) bool {
	_, ok := target.(providerResponseFailure)
	return ok
}

var _ evaluation.SessionEvaluators = (*Evaluators)(nil)
