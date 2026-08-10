package scoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	InterviewShadowSchemaVersion         = "interview-scene-shadow/v1"
	InterviewShadowProviderSchemaVersion = "interview-scene-shadow-provider/v2"
	InterviewShadowPromptVersion         = "interview-scene-shadow-prompt/v2"
	InterviewShadowReadinessNotice       = "Practice feedback only; not a hiring decision or probability."

	interviewShadowMinimumWords       = 3
	interviewShadowMaximumPayload     = 64 * 1024
	interviewShadowMaximumTextBytes   = 2048
	interviewShadowMaximumFindings    = 3
	interviewShadowMaximumAnchors     = 4
	interviewShadowMaximumOccurrence  = 16
	interviewShadowMaximumIdentifier  = 128
	interviewShadowMaximumInputTurns  = 128
	interviewShadowMaximumInputTasks  = 64
	interviewShadowMaximumInputString = 16 * 1024
)

var ErrInvalidInterviewShadow = errors.New(
	"evaluation: invalid Interview shadow",
)

type InterviewDimension string

const (
	InterviewDimensionRelevance    InterviewDimension = "INTERVIEW_RELEVANCE"
	InterviewDimensionStructure    InterviewDimension = "INTERVIEW_STRUCTURE"
	InterviewDimensionEvidence     InterviewDimension = "INTERVIEW_EVIDENCE"
	InterviewDimensionProfessional InterviewDimension = "INTERVIEW_PROFESSIONAL"
	InterviewDimensionInteraction  InterviewDimension = "INTERVIEW_INTERACTION"
)

var interviewDimensionOrder = [...]InterviewDimension{
	InterviewDimensionRelevance,
	InterviewDimensionStructure,
	InterviewDimensionEvidence,
	InterviewDimensionProfessional,
	InterviewDimensionInteraction,
}

func InterviewDimensions() []InterviewDimension {
	return slices.Clone(interviewDimensionOrder[:])
}

type InterviewScoreabilityStatus string

const (
	InterviewScoreabilityProvisional  InterviewScoreabilityStatus = "PROVISIONAL"
	InterviewScoreabilityInsufficient InterviewScoreabilityStatus = "INSUFFICIENT"
)

type InterviewGateStatus string

const (
	InterviewGateFeedbackOnly InterviewGateStatus = "FEEDBACK_ONLY"
	InterviewGateBlocked      InterviewGateStatus = "BLOCKED"
)

type InterviewReadinessLevel string

const InterviewReadinessNotAssessed InterviewReadinessLevel = "NOT_ASSESSED"

type InterviewReasonCode string

const (
	InterviewReasonASRConfidenceUnavailable InterviewReasonCode = "ASR_CONFIDENCE_UNAVAILABLE"
	InterviewReasonInsufficientEvidence     InterviewReasonCode = "INSUFFICIENT_EVIDENCE"
	InterviewReasonOpportunityNotProvided   InterviewReasonCode = "OPPORTUNITY_NOT_PROVIDED"
)

type InterviewOpportunityStatus string

const (
	InterviewOpportunityProvided    InterviewOpportunityStatus = "PROVIDED"
	InterviewOpportunityNotProvided InterviewOpportunityStatus = "NOT_PROVIDED"
)

type InterviewShadowProvider interface {
	AnalyzeInterview(
		context.Context,
		InterviewShadowProviderInput,
	) (InterviewShadowProviderResult, error)
}

type InterviewShadowProviderResult struct {
	Payload   json.RawMessage
	Provider  string
	Model     string
	RequestID string
}

type InterviewShadowProviderInput struct {
	SchemaVersion        string                         `json:"schema_version"`
	PromptVersion        string                         `json:"prompt_version"`
	SceneType            evaluation.SceneType           `json:"scene_type"`
	PracticeGoal         string                         `json:"practice_goal"`
	TaskBlueprints       []string                       `json:"task_blueprints"`
	AssessableDimensions []InterviewDimension           `json:"assessable_dimensions"`
	Opportunities        []InterviewProviderOpportunity `json:"opportunities"`
}

type InterviewProviderOpportunity struct {
	QuestionID       string                     `json:"question_id"`
	QuestionType     string                     `json:"question_type"`
	ParentQuestionID string                     `json:"parent_question_id,omitempty"`
	QuestionText     string                     `json:"question_text"`
	Response         *InterviewProviderResponse `json:"response,omitempty"`
}

type InterviewProviderResponse struct {
	TurnID        string `json:"turn_id"`
	EvidenceRefID string `json:"evidence_ref_id"`
	Transcript    string `json:"confirmed_transcript"`
}

type InterviewShadowResult struct {
	SchemaVersion   string                           `json:"schema_version"`
	SnapshotID      string                           `json:"snapshot_id"`
	SceneType       evaluation.SceneType             `json:"scene_type"`
	Scope           evaluation.Scope                 `json:"scope"`
	Channel         evaluation.Channel               `json:"channel"`
	Scoreability    InterviewScoreabilityStatus      `json:"scoreability_status"`
	Gate            InterviewGateStatus              `json:"gate_status"`
	Readiness       InterviewReadinessLevel          `json:"readiness_level"`
	ReadinessNotice string                           `json:"readiness_notice"`
	ReasonCodes     []InterviewReasonCode            `json:"reason_codes"`
	Dimensions      []InterviewShadowDimensionResult `json:"dimensions"`
	QuestionResults []InterviewShadowQuestionResult  `json:"question_results"`
	Provider        *InterviewShadowProviderLineage  `json:"provider_lineage,omitempty"`
}

type InterviewShadowProviderLineage struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	RequestID      string `json:"request_id"`
	PromptVersion  string `json:"prompt_version"`
	ResponseSchema string `json:"response_schema"`
}

type InterviewShadowDimensionResult struct {
	DimensionID            InterviewDimension          `json:"dimension_id"`
	Score                  int                         `json:"score"`
	Scoreability           InterviewScoreabilityStatus `json:"scoreability_status"`
	Gate                   InterviewGateStatus         `json:"gate_status"`
	Coverage               float64                     `json:"coverage"`
	Confidence             float64                     `json:"confidence"`
	ReasonCodes            []InterviewReasonCode       `json:"reason_codes"`
	EvidenceRefIDs         []string                    `json:"evidence_ref_ids"`
	Strengths              []InterviewShadowFinding    `json:"strengths"`
	Improvements           []InterviewShadowFinding    `json:"improvements"`
	RecommendedExpressions []InterviewShadowFinding    `json:"recommended_expressions"`
}

type InterviewShadowQuestionResult struct {
	QuestionID        string                                   `json:"question_id"`
	QuestionType      string                                   `json:"question_type"`
	ParentQuestionID  string                                   `json:"parent_question_id,omitempty"`
	OpportunityStatus InterviewOpportunityStatus               `json:"opportunity_status"`
	ResponseTurnID    string                                   `json:"response_turn_id,omitempty"`
	EvidenceRefIDs    []string                                 `json:"evidence_ref_ids"`
	DimensionResults  []InterviewShadowQuestionDimensionResult `json:"dimension_results"`
}

type InterviewShadowQuestionDimensionResult struct {
	DimensionID                     InterviewDimension          `json:"dimension_id"`
	Scoreability                    InterviewScoreabilityStatus `json:"scoreability_status"`
	Gate                            InterviewGateStatus         `json:"gate_status"`
	Coverage                        float64                     `json:"coverage"`
	Confidence                      float64                     `json:"confidence"`
	ReasonCodes                     []InterviewReasonCode       `json:"reason_codes"`
	EvidenceRefIDs                  []string                    `json:"evidence_ref_ids"`
	StrengthFindingIDs              []string                    `json:"strength_finding_ids"`
	ImprovementFindingIDs           []string                    `json:"improvement_finding_ids"`
	RecommendedExpressionFindingIDs []string                    `json:"recommended_expression_finding_ids"`
}

type InterviewShadowFinding struct {
	ID         string                    `json:"finding_id"`
	Message    string                    `json:"message"`
	Suggestion string                    `json:"suggestion,omitempty"`
	Evidence   []InterviewShadowEvidence `json:"evidence"`
}

type InterviewShadowEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	TurnID          string `json:"turn_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

type InterviewShadowEngine struct {
	provider InterviewShadowProvider
}

func NewInterviewShadowEngine(
	provider InterviewShadowProvider,
) *InterviewShadowEngine {
	return &InterviewShadowEngine{provider: provider}
}

func (e *InterviewShadowEngine) Evaluate(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
) (InterviewShadowResult, error) {
	if e == nil || e.provider == nil || ctx == nil {
		return InterviewShadowResult{}, evaluation.ErrInvalidRequest
	}
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		return InterviewShadowResult{}, err
	}
	if prepared.result.Scoreability == InterviewScoreabilityInsufficient {
		return prepared.result, nil
	}
	generated, err := e.provider.AnalyzeInterview(ctx, prepared.input)
	if err != nil {
		return InterviewShadowResult{}, err
	}
	result, err := normalizeInterviewShadowProviderResult(
		prepared,
		generated,
	)
	if err != nil {
		return InterviewShadowResult{}, err
	}
	if err := ValidateInterviewShadowResult(snapshot, result); err != nil {
		return InterviewShadowResult{}, err
	}
	return result, nil
}

type preparedInterviewShadow struct {
	evidence       evidence.SnapshotPayload
	input          InterviewShadowProviderInput
	result         InterviewShadowResult
	turnsByID      map[string]evidence.ConfirmedTurn
	refsByID       map[string]evidence.Ref
	refsByTurnID   map[string]evidence.Ref
	assessable     map[InterviewDimension]struct{}
	allowedRefs    map[InterviewDimension]map[string]struct{}
	dimensionCover map[InterviewDimension]float64
	hasFollowUp    bool
}

func prepareInterviewShadow(
	snapshot evidence.EvidenceSnapshot,
) (preparedInterviewShadow, error) {
	if !snapshot.Valid() ||
		snapshot.Scope != evaluation.ScopeSession ||
		snapshot.SceneType != evaluation.SceneInterview {
		return preparedInterviewShadow{}, evaluation.ErrInvalidRequest
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return preparedInterviewShadow{}, evaluation.ErrInvalidRequest
	}
	turnsByID := make(
		map[string]evidence.ConfirmedTurn,
		len(payload.ConfirmedTurns),
	)
	for _, turn := range payload.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}
	refsByID := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	refsByTurnID := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		refsByID[ref.EvidenceRefID] = ref
		refsByTurnID[ref.TurnID] = ref
	}

	totalOpportunities := len(payload.OpportunityManifest)
	answeredOpportunities := 0
	totalFollowUps := 0
	answeredFollowUps := 0
	wordCount := 0
	allResponseRefs := make(map[string]struct{}, len(payload.EvidenceRefs))
	followUpResponseRefs := make(map[string]struct{})
	opportunities := make(
		[]InterviewProviderOpportunity,
		0,
		totalOpportunities,
	)
	for _, opportunity := range payload.OpportunityManifest {
		providerOpportunity := InterviewProviderOpportunity{
			QuestionID:       opportunity.QuestionID,
			QuestionType:     opportunity.QuestionType,
			ParentQuestionID: opportunity.ParentQuestionID,
			QuestionText:     opportunity.QuestionText,
		}
		if opportunity.QuestionType == "FOLLOW_UP" {
			totalFollowUps++
		}
		if opportunity.ResponseTurnID != "" {
			turn, turnExists := turnsByID[opportunity.ResponseTurnID]
			ref, refExists := refsByTurnID[opportunity.ResponseTurnID]
			if !turnExists || !refExists {
				return preparedInterviewShadow{}, evaluation.ErrInvalidRequest
			}
			answeredOpportunities++
			if opportunity.QuestionType == "FOLLOW_UP" {
				answeredFollowUps++
			}
			wordCount += interviewWordCount(turn.Transcript.Text)
			allResponseRefs[ref.EvidenceRefID] = struct{}{}
			if opportunity.QuestionType == "FOLLOW_UP" {
				followUpResponseRefs[ref.EvidenceRefID] = struct{}{}
			}
			providerOpportunity.Response = &InterviewProviderResponse{
				TurnID:        turn.TurnID,
				EvidenceRefID: ref.EvidenceRefID,
				Transcript:    turn.Transcript.Text,
			}
		}
		opportunities = append(opportunities, providerOpportunity)
	}
	if totalOpportunities == 0 || answeredOpportunities == 0 {
		return preparedInterviewShadow{}, evaluation.ErrInvalidRequest
	}

	baseCoverage := ratio(answeredOpportunities, totalOpportunities)
	dimensionCover := make(map[InterviewDimension]float64, 5)
	for _, dimension := range interviewDimensionOrder[:4] {
		dimensionCover[dimension] = baseCoverage
	}
	if totalFollowUps > 0 {
		dimensionCover[InterviewDimensionInteraction] = ratio(
			answeredFollowUps,
			totalFollowUps,
		)
	}
	allowedRefs := make(
		map[InterviewDimension]map[string]struct{},
		len(interviewDimensionOrder),
	)
	for _, dimension := range interviewDimensionOrder[:4] {
		allowedRefs[dimension] = allResponseRefs
	}
	allowedRefs[InterviewDimensionInteraction] = followUpResponseRefs

	result := interviewShadowResultSkeleton(snapshot)
	if wordCount < interviewShadowMinimumWords {
		result.Scoreability = InterviewScoreabilityInsufficient
		result.Gate = InterviewGateBlocked
		result.ReasonCodes = []InterviewReasonCode{
			InterviewReasonInsufficientEvidence,
		}
		for _, dimension := range interviewDimensionOrder {
			result.Dimensions = append(
				result.Dimensions,
				blockedInterviewDimension(
					dimension,
					dimensionCover[dimension],
					InterviewReasonInsufficientEvidence,
				),
			)
		}
		prepared := preparedInterviewShadow{
			evidence:       payload,
			result:         result,
			turnsByID:      turnsByID,
			refsByID:       refsByID,
			refsByTurnID:   refsByTurnID,
			allowedRefs:    allowedRefs,
			dimensionCover: dimensionCover,
			hasFollowUp:    totalFollowUps > 0,
		}
		prepared.result.QuestionResults = interviewShadowQuestionResults(
			prepared,
			prepared.result.Dimensions,
		)
		return prepared, nil
	}

	assessable := make(map[InterviewDimension]struct{}, 5)
	assessableOrder := make([]InterviewDimension, 0, 5)
	for _, dimension := range interviewDimensionOrder[:4] {
		assessable[dimension] = struct{}{}
		assessableOrder = append(assessableOrder, dimension)
	}
	if answeredFollowUps > 0 {
		assessable[InterviewDimensionInteraction] = struct{}{}
		assessableOrder = append(
			assessableOrder,
			InterviewDimensionInteraction,
		)
	}
	result.Scoreability = InterviewScoreabilityProvisional
	result.Gate = InterviewGateFeedbackOnly
	result.ReasonCodes = []InterviewReasonCode{
		InterviewReasonASRConfidenceUnavailable,
	}

	input := InterviewShadowProviderInput{
		SchemaVersion: InterviewShadowProviderSchemaVersion,
		PromptVersion: InterviewShadowPromptVersion,
		SceneType:     evaluation.SceneInterview,
		PracticeGoal:  payload.PracticeContext.PracticeGoal,
		TaskBlueprints: slices.Clone(
			payload.PracticeContext.TaskBlueprints,
		),
		AssessableDimensions: assessableOrder,
		Opportunities:        opportunities,
	}
	if !validInterviewShadowProviderInput(input) {
		return preparedInterviewShadow{}, evaluation.ErrInvalidRequest
	}
	return preparedInterviewShadow{
		evidence:       payload,
		input:          input,
		result:         result,
		turnsByID:      turnsByID,
		refsByID:       refsByID,
		refsByTurnID:   refsByTurnID,
		assessable:     assessable,
		allowedRefs:    allowedRefs,
		dimensionCover: dimensionCover,
		hasFollowUp:    totalFollowUps > 0,
	}, nil
}

func interviewShadowResultSkeleton(
	snapshot evidence.EvidenceSnapshot,
) InterviewShadowResult {
	return InterviewShadowResult{
		SchemaVersion:   InterviewShadowSchemaVersion,
		SnapshotID:      snapshot.ID,
		SceneType:       evaluation.SceneInterview,
		Scope:           evaluation.ScopeSession,
		Channel:         evaluation.ChannelScene,
		Readiness:       InterviewReadinessNotAssessed,
		ReadinessNotice: InterviewShadowReadinessNotice,
		ReasonCodes:     []InterviewReasonCode{},
		Dimensions:      []InterviewShadowDimensionResult{},
		QuestionResults: []InterviewShadowQuestionResult{},
	}
}

func blockedInterviewDimension(
	dimension InterviewDimension,
	coverage float64,
	reason InterviewReasonCode,
) InterviewShadowDimensionResult {
	return InterviewShadowDimensionResult{
		DimensionID:            dimension,
		Scoreability:           InterviewScoreabilityInsufficient,
		Gate:                   InterviewGateBlocked,
		Coverage:               coverage,
		Confidence:             0,
		ReasonCodes:            []InterviewReasonCode{reason},
		EvidenceRefIDs:         []string{},
		Strengths:              []InterviewShadowFinding{},
		Improvements:           []InterviewShadowFinding{},
		RecommendedExpressions: []InterviewShadowFinding{},
	}
}

type interviewProviderPayload struct {
	SchemaVersion string                       `json:"schema_version"`
	Dimensions    []interviewProviderDimension `json:"dimensions"`
}

type interviewProviderDimension struct {
	DimensionID            InterviewDimension         `json:"dimension_id"`
	Score                  int                        `json:"score"`
	Strengths              []interviewProviderFinding `json:"strengths"`
	Improvements           []interviewProviderFinding `json:"improvements"`
	RecommendedExpressions []interviewProviderFinding `json:"recommended_expressions"`
}

type interviewProviderFinding struct {
	TemplateID string                    `json:"template_id"`
	Evidence   []interviewProviderAnchor `json:"evidence"`
}

type interviewProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

type interviewFindingKind string

const (
	interviewFindingStrength              interviewFindingKind = "strength"
	interviewFindingImprovement           interviewFindingKind = "improvement"
	interviewFindingRecommendedExpression interviewFindingKind = "recommended_expression"
)

type interviewFeedbackTemplate struct {
	ID         string
	Message    string
	Suggestion string
}

func interviewShadowFeedbackTemplate(
	dimension InterviewDimension,
	kind interviewFindingKind,
) (interviewFeedbackTemplate, bool) {
	template := interviewFeedbackTemplate{
		ID: string(dimension) + ":" +
			strings.ToUpper(string(kind)) + ":v1",
	}
	switch dimension {
	case InterviewDimensionRelevance:
		switch kind {
		case interviewFindingStrength:
			template.Message = "The response directly addresses the question with a relevant point."
		case interviewFindingImprovement:
			template.Message = "The response needs a clearer connection to the question."
			template.Suggestion = "State the answer first, then connect each supporting detail to the question."
		case interviewFindingRecommendedExpression:
			template.Message = "Use a direct opening before the supporting detail."
			template.Suggestion = "My main reason is ..., and the experience that best shows it is ...."
		default:
			return interviewFeedbackTemplate{}, false
		}
	case InterviewDimensionStructure:
		switch kind {
		case interviewFindingStrength:
			template.Message = "The response presents its main point and support in a clear order."
		case interviewFindingImprovement:
			template.Message = "The response's main point and support are difficult to follow."
			template.Suggestion = "Use a situation-action-result order and keep one main point per sentence."
		case interviewFindingRecommendedExpression:
			template.Message = "Use clear sequence markers."
			template.Suggestion = "First, .... Then, .... As a result, ...."
		default:
			return interviewFeedbackTemplate{}, false
		}
	case InterviewDimensionEvidence:
		switch kind {
		case interviewFindingStrength:
			template.Message = "The response supports its point with a concrete example."
		case interviewFindingImprovement:
			template.Message = "The response needs a more specific example or outcome."
			template.Suggestion = "Name the situation, your action, and the observable outcome."
		case interviewFindingRecommendedExpression:
			template.Message = "Use a concise evidence pattern."
			template.Suggestion = "For example, I ..., which resulted in ...."
		default:
			return interviewFeedbackTemplate{}, false
		}
	case InterviewDimensionProfessional:
		switch kind {
		case interviewFindingStrength:
			template.Message = "The response uses precise, professional wording for the context."
		case interviewFindingImprovement:
			template.Message = "The response could use more precise, professional wording."
			template.Suggestion = "Replace broad claims with specific actions and neutral workplace language."
		case interviewFindingRecommendedExpression:
			template.Message = "Use a precise ownership statement."
			template.Suggestion = "I was responsible for ..., so I ...."
		default:
			return interviewFeedbackTemplate{}, false
		}
	case InterviewDimensionInteraction:
		switch kind {
		case interviewFindingStrength:
			template.Message = "The response connects to the follow-up and advances the exchange."
		case interviewFindingImprovement:
			template.Message = "The response needs to address the follow-up more directly."
			template.Suggestion = "Acknowledge the follow-up, answer it directly, then add one supporting detail."
		case interviewFindingRecommendedExpression:
			template.Message = "Use a follow-up bridge."
			template.Suggestion = "Building on that, the key change was ...."
		default:
			return interviewFeedbackTemplate{}, false
		}
	default:
		return interviewFeedbackTemplate{}, false
	}
	return template, true
}

func normalizeInterviewShadowProviderResult(
	prepared preparedInterviewShadow,
	generated InterviewShadowProviderResult,
) (InterviewShadowResult, error) {
	if len(generated.Payload) == 0 ||
		len(generated.Payload) > interviewShadowMaximumPayload ||
		!validProviderIdentifier(generated.Provider) ||
		!validModelIdentifier(generated.Model) ||
		!validProviderIdentifier(generated.RequestID) {
		return InterviewShadowResult{}, ErrInvalidInterviewShadow
	}
	decoder := json.NewDecoder(bytes.NewReader(generated.Payload))
	decoder.DisallowUnknownFields()
	var payload interviewProviderPayload
	if err := decoder.Decode(&payload); err != nil ||
		ensureInterviewJSONEOF(decoder) != nil ||
		payload.SchemaVersion != InterviewShadowProviderSchemaVersion ||
		payload.Dimensions == nil ||
		len(payload.Dimensions) != len(prepared.assessable) {
		return InterviewShadowResult{}, ErrInvalidInterviewShadow
	}
	byDimension := make(
		map[InterviewDimension]interviewProviderDimension,
		len(payload.Dimensions),
	)
	for _, dimension := range payload.Dimensions {
		if _, expected := prepared.assessable[dimension.DimensionID]; !expected {
			return InterviewShadowResult{}, ErrInvalidInterviewShadow
		}
		if _, duplicate := byDimension[dimension.DimensionID]; duplicate {
			return InterviewShadowResult{}, ErrInvalidInterviewShadow
		}
		byDimension[dimension.DimensionID] = dimension
	}

	result := prepared.result
	result.Provider = &InterviewShadowProviderLineage{
		Provider:       generated.Provider,
		Model:          generated.Model,
		RequestID:      generated.RequestID,
		PromptVersion:  InterviewShadowPromptVersion,
		ResponseSchema: InterviewShadowProviderSchemaVersion,
	}
	for _, dimensionID := range interviewDimensionOrder {
		providerDimension, assessable := byDimension[dimensionID]
		if !assessable {
			reason := InterviewReasonOpportunityNotProvided
			if prepared.hasFollowUp {
				reason = InterviewReasonInsufficientEvidence
			}
			result.Dimensions = append(
				result.Dimensions,
				blockedInterviewDimension(
					dimensionID,
					prepared.dimensionCover[dimensionID],
					reason,
				),
			)
			continue
		}
		dimension, err := normalizeInterviewProviderDimension(
			prepared,
			providerDimension,
		)
		if err != nil {
			return InterviewShadowResult{}, err
		}
		result.Dimensions = append(result.Dimensions, dimension)
	}
	result.QuestionResults = interviewShadowQuestionResults(
		prepared,
		result.Dimensions,
	)
	return result, nil
}

func DecodeInterviewShadowResult(payload []byte) (InterviewShadowResult, error) {
	var result InterviewShadowResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || ensureJSONEOF(decoder) != nil {
		return InterviewShadowResult{}, ErrInvalidInterviewShadow
	}
	return result, nil
}

func interviewShadowQuestionResults(
	prepared preparedInterviewShadow,
	dimensions []InterviewShadowDimensionResult,
) []InterviewShadowQuestionResult {
	results := make(
		[]InterviewShadowQuestionResult,
		0,
		len(prepared.evidence.OpportunityManifest),
	)
	for _, opportunity := range prepared.evidence.OpportunityManifest {
		question := InterviewShadowQuestionResult{
			QuestionID:        opportunity.QuestionID,
			QuestionType:      opportunity.QuestionType,
			ParentQuestionID:  opportunity.ParentQuestionID,
			OpportunityStatus: InterviewOpportunityNotProvided,
			EvidenceRefIDs:    []string{},
			DimensionResults: make(
				[]InterviewShadowQuestionDimensionResult,
				0,
				len(dimensions),
			),
		}
		var responseRef evidence.Ref
		if opportunity.ResponseTurnID != "" {
			question.OpportunityStatus = InterviewOpportunityProvided
			question.ResponseTurnID = opportunity.ResponseTurnID
			responseRef = prepared.refsByTurnID[opportunity.ResponseTurnID]
			question.EvidenceRefIDs = []string{responseRef.EvidenceRefID}
		}
		for _, dimension := range dimensions {
			question.DimensionResults = append(
				question.DimensionResults,
				interviewShadowQuestionDimensionResult(
					prepared,
					question.OpportunityStatus,
					responseRef,
					dimension,
				),
			)
		}
		results = append(results, question)
	}
	return results
}

func interviewShadowQuestionDimensionResult(
	prepared preparedInterviewShadow,
	opportunityStatus InterviewOpportunityStatus,
	responseRef evidence.Ref,
	dimension InterviewShadowDimensionResult,
) InterviewShadowQuestionDimensionResult {
	result := InterviewShadowQuestionDimensionResult{
		DimensionID:  dimension.DimensionID,
		Scoreability: InterviewScoreabilityInsufficient,
		Gate:         InterviewGateBlocked,
		Confidence:   0,
		ReasonCodes: []InterviewReasonCode{
			InterviewReasonOpportunityNotProvided,
		},
		EvidenceRefIDs:                  []string{},
		StrengthFindingIDs:              []string{},
		ImprovementFindingIDs:           []string{},
		RecommendedExpressionFindingIDs: []string{},
	}
	if opportunityStatus == InterviewOpportunityNotProvided {
		return result
	}
	if _, relevant := prepared.allowedRefs[dimension.DimensionID][responseRef.EvidenceRefID]; !relevant {
		return result
	}

	result.Coverage = 1
	result.EvidenceRefIDs = []string{responseRef.EvidenceRefID}
	if dimension.Scoreability == InterviewScoreabilityInsufficient {
		result.ReasonCodes = []InterviewReasonCode{
			InterviewReasonInsufficientEvidence,
		}
		return result
	}

	result.Scoreability = InterviewScoreabilityProvisional
	result.Gate = InterviewGateFeedbackOnly
	result.ReasonCodes = []InterviewReasonCode{
		InterviewReasonASRConfidenceUnavailable,
	}
	result.StrengthFindingIDs = interviewFindingIDsForEvidenceRef(
		dimension.Strengths,
		responseRef.EvidenceRefID,
	)
	result.ImprovementFindingIDs = interviewFindingIDsForEvidenceRef(
		dimension.Improvements,
		responseRef.EvidenceRefID,
	)
	result.RecommendedExpressionFindingIDs =
		interviewFindingIDsForEvidenceRef(
			dimension.RecommendedExpressions,
			responseRef.EvidenceRefID,
		)
	return result
}

func interviewFindingIDsForEvidenceRef(
	findings []InterviewShadowFinding,
	evidenceRefID string,
) []string {
	result := make([]string, 0, len(findings))
	for _, finding := range findings {
		for _, evidence := range finding.Evidence {
			if evidence.EvidenceRefID == evidenceRefID {
				result = append(result, finding.ID)
				break
			}
		}
	}
	return result
}

func normalizeInterviewProviderDimension(
	prepared preparedInterviewShadow,
	source interviewProviderDimension,
) (InterviewShadowDimensionResult, error) {
	if source.Score < 0 || source.Score > 100 ||
		source.Strengths == nil ||
		source.Improvements == nil ||
		source.RecommendedExpressions == nil ||
		len(source.Strengths) > interviewShadowMaximumFindings ||
		len(source.Improvements) > interviewShadowMaximumFindings ||
		len(source.RecommendedExpressions) >
			interviewShadowMaximumFindings ||
		len(source.Strengths)+len(source.Improvements) == 0 {
		return InterviewShadowDimensionResult{}, ErrInvalidInterviewShadow
	}
	result := InterviewShadowDimensionResult{
		DimensionID:  source.DimensionID,
		Score:        source.Score,
		Scoreability: InterviewScoreabilityProvisional,
		Gate:         InterviewGateFeedbackOnly,
		Coverage:     prepared.dimensionCover[source.DimensionID],
		Confidence:   0,
		ReasonCodes: []InterviewReasonCode{
			InterviewReasonASRConfidenceUnavailable,
		},
		EvidenceRefIDs:         []string{},
		Strengths:              []InterviewShadowFinding{},
		Improvements:           []InterviewShadowFinding{},
		RecommendedExpressions: []InterviewShadowFinding{},
	}
	var err error
	result.Strengths, err = normalizeInterviewFindings(
		prepared,
		source.DimensionID,
		interviewFindingStrength,
		source.Strengths,
	)
	if err != nil {
		return InterviewShadowDimensionResult{}, err
	}
	result.Improvements, err = normalizeInterviewFindings(
		prepared,
		source.DimensionID,
		interviewFindingImprovement,
		source.Improvements,
	)
	if err != nil {
		return InterviewShadowDimensionResult{}, err
	}
	result.RecommendedExpressions, err = normalizeInterviewFindings(
		prepared,
		source.DimensionID,
		interviewFindingRecommendedExpression,
		source.RecommendedExpressions,
	)
	if err != nil {
		return InterviewShadowDimensionResult{}, err
	}
	refSet := make(map[string]struct{})
	for _, collection := range [][]InterviewShadowFinding{
		result.Strengths,
		result.Improvements,
		result.RecommendedExpressions,
	} {
		for _, finding := range collection {
			for _, evidence := range finding.Evidence {
				refSet[evidence.EvidenceRefID] = struct{}{}
			}
		}
	}
	for refID := range refSet {
		result.EvidenceRefIDs = append(result.EvidenceRefIDs, refID)
	}
	slices.Sort(result.EvidenceRefIDs)
	return result, nil
}

func normalizeInterviewFindings(
	prepared preparedInterviewShadow,
	dimension InterviewDimension,
	kind interviewFindingKind,
	source []interviewProviderFinding,
) ([]InterviewShadowFinding, error) {
	result := make([]InterviewShadowFinding, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	for _, item := range source {
		template, exists := interviewShadowFeedbackTemplate(
			dimension,
			kind,
		)
		if !exists ||
			item.TemplateID != template.ID ||
			len(item.Evidence) == 0 ||
			len(item.Evidence) > interviewShadowMaximumAnchors {
			return nil, ErrInvalidInterviewShadow
		}
		evidence := make(
			[]InterviewShadowEvidence,
			0,
			len(item.Evidence),
		)
		seenAnchors := make(map[string]struct{}, len(item.Evidence))
		for _, anchor := range item.Evidence {
			resolved, err := resolveInterviewProviderAnchor(
				prepared,
				dimension,
				anchor,
			)
			if err != nil {
				return nil, err
			}
			key := resolved.EvidenceRefID + "\x00" +
				strconv.Itoa(resolved.StartUTF8Byte) + "\x00" +
				strconv.Itoa(resolved.EndUTF8Byte)
			if _, duplicate := seenAnchors[key]; duplicate {
				return nil, ErrInvalidInterviewShadow
			}
			seenAnchors[key] = struct{}{}
			evidence = append(evidence, resolved)
		}
		finding := InterviewShadowFinding{
			Message:    template.Message,
			Suggestion: template.Suggestion,
			Evidence:   evidence,
		}
		finding.ID = stableInterviewFindingID(
			prepared.result.SnapshotID,
			dimension,
			kind,
			finding,
		)
		if _, duplicate := seen[finding.ID]; duplicate {
			return nil, ErrInvalidInterviewShadow
		}
		seen[finding.ID] = struct{}{}
		result = append(result, finding)
	}
	return result, nil
}

func resolveInterviewProviderAnchor(
	prepared preparedInterviewShadow,
	dimension InterviewDimension,
	anchor interviewProviderAnchor,
) (InterviewShadowEvidence, error) {
	ref, exists := prepared.refsByID[anchor.EvidenceRefID]
	_, allowed := prepared.allowedRefs[dimension][anchor.EvidenceRefID]
	turn, turnExists := prepared.turnsByID[ref.TurnID]
	if !exists || !allowed || !turnExists ||
		anchor.Occurrence < 1 ||
		anchor.Occurrence > interviewShadowMaximumOccurrence ||
		!validInterviewText(anchor.Quote, interviewShadowMaximumTextBytes) {
		return InterviewShadowEvidence{}, ErrInvalidInterviewShadow
	}
	start := nthInterviewOccurrence(
		turn.Transcript.Text,
		anchor.Quote,
		anchor.Occurrence,
	)
	end := start + len(anchor.Quote)
	if start < ref.TranscriptSpan.StartUTF8Byte ||
		end > ref.TranscriptSpan.EndUTF8Byte ||
		start < 0 ||
		end <= start ||
		!utf8.ValidString(turn.Transcript.Text[start:end]) {
		return InterviewShadowEvidence{}, ErrInvalidInterviewShadow
	}
	return InterviewShadowEvidence{
		EvidenceRefID:   ref.EvidenceRefID,
		TurnID:          ref.TurnID,
		StartUTF8Byte:   start,
		EndUTF8Byte:     end,
		OriginalExcerpt: turn.Transcript.Text[start:end],
	}, nil
}

func ValidateInterviewShadowResult(
	snapshot evidence.EvidenceSnapshot,
	result InterviewShadowResult,
) error {
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		return err
	}
	if result.SchemaVersion != InterviewShadowSchemaVersion ||
		result.SnapshotID != snapshot.ID ||
		result.SceneType != evaluation.SceneInterview ||
		result.Scope != evaluation.ScopeSession ||
		result.Channel != evaluation.ChannelScene ||
		result.Readiness != InterviewReadinessNotAssessed ||
		result.ReadinessNotice != InterviewShadowReadinessNotice ||
		result.ReasonCodes == nil ||
		result.Dimensions == nil ||
		len(result.Dimensions) != len(interviewDimensionOrder) ||
		result.QuestionResults == nil {
		return ErrInvalidInterviewShadow
	}
	if prepared.result.Scoreability == InterviewScoreabilityInsufficient {
		if result.Scoreability != InterviewScoreabilityInsufficient ||
			result.Gate != InterviewGateBlocked ||
			result.Provider != nil ||
			!slices.Equal(
				result.ReasonCodes,
				[]InterviewReasonCode{
					InterviewReasonInsufficientEvidence,
				},
			) {
			return ErrInvalidInterviewShadow
		}
	} else if result.Scoreability != InterviewScoreabilityProvisional ||
		result.Gate != InterviewGateFeedbackOnly ||
		result.Provider == nil ||
		!validInterviewProviderLineage(*result.Provider) ||
		!slices.Equal(
			result.ReasonCodes,
			[]InterviewReasonCode{
				InterviewReasonASRConfidenceUnavailable,
			},
		) {
		return ErrInvalidInterviewShadow
	}

	seenFindingIDs := make(map[string]struct{})
	for index, dimension := range result.Dimensions {
		expectedID := interviewDimensionOrder[index]
		if dimension.DimensionID != expectedID ||
			dimension.Score < 0 || dimension.Score > 100 ||
			!sameRatio(
				dimension.Coverage,
				prepared.dimensionCover[expectedID],
			) ||
			dimension.Confidence != 0 {
			return ErrInvalidInterviewShadow
		}
		_, assessable := prepared.assessable[expectedID]
		if prepared.result.Scoreability == InterviewScoreabilityInsufficient ||
			!assessable {
			expectedReason := InterviewReasonInsufficientEvidence
			if prepared.result.Scoreability !=
				InterviewScoreabilityInsufficient &&
				!prepared.hasFollowUp {
				expectedReason = InterviewReasonOpportunityNotProvided
			}
			if !validBlockedInterviewDimension(
				dimension,
				expectedReason,
			) {
				return ErrInvalidInterviewShadow
			}
			continue
		}
		if dimension.Scoreability != InterviewScoreabilityProvisional ||
			dimension.Gate != InterviewGateFeedbackOnly ||
			dimension.EvidenceRefIDs == nil ||
			dimension.Strengths == nil ||
			dimension.Improvements == nil ||
			dimension.RecommendedExpressions == nil ||
			!slices.Equal(
				dimension.ReasonCodes,
				[]InterviewReasonCode{
					InterviewReasonASRConfidenceUnavailable,
				},
			) ||
			len(dimension.Strengths)+len(dimension.Improvements) == 0 {
			return ErrInvalidInterviewShadow
		}
		refSet := make(map[string]struct{})
		for kind, findings := range map[interviewFindingKind][]InterviewShadowFinding{
			interviewFindingStrength:              dimension.Strengths,
			interviewFindingImprovement:           dimension.Improvements,
			interviewFindingRecommendedExpression: dimension.RecommendedExpressions,
		} {
			if len(findings) > interviewShadowMaximumFindings {
				return ErrInvalidInterviewShadow
			}
			for _, finding := range findings {
				if !validInterviewShadowFinding(
					prepared,
					dimension.DimensionID,
					kind,
					finding,
				) {
					return ErrInvalidInterviewShadow
				}
				if _, duplicate := seenFindingIDs[finding.ID]; duplicate {
					return ErrInvalidInterviewShadow
				}
				seenFindingIDs[finding.ID] = struct{}{}
				for _, evidence := range finding.Evidence {
					refSet[evidence.EvidenceRefID] = struct{}{}
				}
			}
		}
		expectedRefs := make([]string, 0, len(refSet))
		for refID := range refSet {
			expectedRefs = append(expectedRefs, refID)
		}
		slices.Sort(expectedRefs)
		if !slices.Equal(dimension.EvidenceRefIDs, expectedRefs) {
			return ErrInvalidInterviewShadow
		}
	}
	expectedQuestions := interviewShadowQuestionResults(
		prepared,
		result.Dimensions,
	)
	if !sameInterviewShadowQuestionResults(
		result.QuestionResults,
		expectedQuestions,
	) {
		return ErrInvalidInterviewShadow
	}
	return nil
}

func sameInterviewShadowQuestionResults(
	actual []InterviewShadowQuestionResult,
	expected []InterviewShadowQuestionResult,
) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		left := actual[index]
		right := expected[index]
		if left.QuestionID != right.QuestionID ||
			left.QuestionType != right.QuestionType ||
			left.ParentQuestionID != right.ParentQuestionID ||
			left.OpportunityStatus != right.OpportunityStatus ||
			left.ResponseTurnID != right.ResponseTurnID ||
			left.EvidenceRefIDs == nil ||
			!slices.Equal(left.EvidenceRefIDs, right.EvidenceRefIDs) ||
			left.DimensionResults == nil ||
			len(left.DimensionResults) != len(right.DimensionResults) {
			return false
		}
		for dimensionIndex := range right.DimensionResults {
			if !sameInterviewShadowQuestionDimensionResult(
				left.DimensionResults[dimensionIndex],
				right.DimensionResults[dimensionIndex],
			) {
				return false
			}
		}
	}
	return true
}

func sameInterviewShadowQuestionDimensionResult(
	actual InterviewShadowQuestionDimensionResult,
	expected InterviewShadowQuestionDimensionResult,
) bool {
	return actual.DimensionID == expected.DimensionID &&
		actual.Scoreability == expected.Scoreability &&
		actual.Gate == expected.Gate &&
		sameRatio(actual.Coverage, expected.Coverage) &&
		actual.Confidence == expected.Confidence &&
		actual.ReasonCodes != nil &&
		slices.Equal(actual.ReasonCodes, expected.ReasonCodes) &&
		actual.EvidenceRefIDs != nil &&
		slices.Equal(actual.EvidenceRefIDs, expected.EvidenceRefIDs) &&
		actual.StrengthFindingIDs != nil &&
		slices.Equal(
			actual.StrengthFindingIDs,
			expected.StrengthFindingIDs,
		) &&
		actual.ImprovementFindingIDs != nil &&
		slices.Equal(
			actual.ImprovementFindingIDs,
			expected.ImprovementFindingIDs,
		) &&
		actual.RecommendedExpressionFindingIDs != nil &&
		slices.Equal(
			actual.RecommendedExpressionFindingIDs,
			expected.RecommendedExpressionFindingIDs,
		)
}

func validInterviewShadowFinding(
	prepared preparedInterviewShadow,
	dimension InterviewDimension,
	kind interviewFindingKind,
	finding InterviewShadowFinding,
) bool {
	template, exists := interviewShadowFeedbackTemplate(dimension, kind)
	if !exists ||
		finding.Message != template.Message ||
		finding.Suggestion != template.Suggestion ||
		len(finding.Evidence) == 0 ||
		len(finding.Evidence) > interviewShadowMaximumAnchors ||
		finding.ID != stableInterviewFindingID(
			prepared.result.SnapshotID,
			dimension,
			kind,
			finding,
		) {
		return false
	}
	seen := make(map[string]struct{}, len(finding.Evidence))
	for _, evidence := range finding.Evidence {
		ref, exists := prepared.refsByID[evidence.EvidenceRefID]
		_, allowed := prepared.allowedRefs[dimension][evidence.EvidenceRefID]
		turn, turnExists := prepared.turnsByID[evidence.TurnID]
		if !exists || !allowed || !turnExists ||
			ref.TurnID != evidence.TurnID ||
			evidence.StartUTF8Byte < ref.TranscriptSpan.StartUTF8Byte ||
			evidence.EndUTF8Byte > ref.TranscriptSpan.EndUTF8Byte ||
			evidence.StartUTF8Byte < 0 ||
			evidence.EndUTF8Byte <= evidence.StartUTF8Byte ||
			evidence.EndUTF8Byte > len(turn.Transcript.Text) ||
			!utf8.ValidString(
				turn.Transcript.Text[evidence.StartUTF8Byte:evidence.EndUTF8Byte],
			) ||
			evidence.OriginalExcerpt !=
				turn.Transcript.Text[evidence.StartUTF8Byte:evidence.EndUTF8Byte] {
			return false
		}
		key := evidence.EvidenceRefID + "\x00" +
			strconv.Itoa(evidence.StartUTF8Byte) + "\x00" +
			strconv.Itoa(evidence.EndUTF8Byte)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validBlockedInterviewDimension(
	dimension InterviewShadowDimensionResult,
	expectedReason InterviewReasonCode,
) bool {
	if dimension.Scoreability != InterviewScoreabilityInsufficient ||
		dimension.Gate != InterviewGateBlocked ||
		len(dimension.ReasonCodes) != 1 ||
		dimension.ReasonCodes[0] != expectedReason ||
		dimension.EvidenceRefIDs == nil ||
		dimension.Strengths == nil ||
		dimension.Improvements == nil ||
		dimension.RecommendedExpressions == nil ||
		len(dimension.EvidenceRefIDs) != 0 ||
		len(dimension.Strengths) != 0 ||
		len(dimension.Improvements) != 0 ||
		len(dimension.RecommendedExpressions) != 0 {
		return false
	}
	return true
}

func validInterviewShadowProviderInput(
	input InterviewShadowProviderInput,
) bool {
	if input.SchemaVersion != InterviewShadowProviderSchemaVersion ||
		input.PromptVersion != InterviewShadowPromptVersion ||
		input.SceneType != evaluation.SceneInterview ||
		!validInterviewText(
			input.PracticeGoal,
			interviewShadowMaximumInputString,
		) ||
		len(input.TaskBlueprints) == 0 ||
		len(input.TaskBlueprints) > interviewShadowMaximumInputTasks ||
		len(input.AssessableDimensions) < 4 ||
		len(input.AssessableDimensions) > 5 ||
		len(input.Opportunities) == 0 ||
		len(input.Opportunities) > interviewShadowMaximumInputTurns {
		return false
	}
	for _, value := range input.TaskBlueprints {
		if !validInterviewText(
			value,
			interviewShadowMaximumInputString,
		) {
			return false
		}
	}
	for index, dimension := range input.AssessableDimensions {
		if dimension != interviewDimensionOrder[index] {
			return false
		}
	}
	seenQuestions := make(map[string]struct{}, len(input.Opportunities))
	for _, opportunity := range input.Opportunities {
		if !validIdentifier(opportunity.QuestionID) ||
			(opportunity.QuestionType != "PRIMARY" &&
				opportunity.QuestionType != "FOLLOW_UP") ||
			!validInterviewText(
				opportunity.QuestionText,
				interviewShadowMaximumInputString,
			) {
			return false
		}
		if _, duplicate := seenQuestions[opportunity.QuestionID]; duplicate {
			return false
		}
		seenQuestions[opportunity.QuestionID] = struct{}{}
		if opportunity.QuestionType == "PRIMARY" {
			if opportunity.ParentQuestionID != "" {
				return false
			}
		} else if _, exists := seenQuestions[opportunity.ParentQuestionID]; !exists {
			return false
		}
		if opportunity.Response != nil &&
			(!validIdentifier(opportunity.Response.TurnID) ||
				!validIdentifier(opportunity.Response.EvidenceRefID) ||
				!validInterviewText(
					opportunity.Response.Transcript,
					interviewShadowMaximumInputString,
				)) {
			return false
		}
	}
	return true
}

func validInterviewProviderLineage(
	lineage InterviewShadowProviderLineage,
) bool {
	return validProviderIdentifier(lineage.Provider) &&
		validModelIdentifier(lineage.Model) &&
		validProviderIdentifier(lineage.RequestID) &&
		lineage.PromptVersion == InterviewShadowPromptVersion &&
		lineage.ResponseSchema == InterviewShadowProviderSchemaVersion
}

func validProviderIdentifier(value string) bool {
	return validInterviewText(value, interviewShadowMaximumIdentifier)
}

func validInterviewText(value string, maximumBytes int) bool {
	return utf8.ValidString(value) &&
		len(value) <= maximumBytes &&
		!strings.ContainsRune(value, '\x00') &&
		value == strings.TrimSpace(value) &&
		value != ""
}

func interviewWordCount(value string) int {
	count := 0
	for _, field := range strings.Fields(value) {
		if strings.IndexFunc(field, unicode.IsLetter) >= 0 {
			count++
		}
	}
	return count
}

func nthInterviewOccurrence(value string, quote string, occurrence int) int {
	offset := 0
	for current := 1; current <= occurrence; current++ {
		index := strings.Index(value[offset:], quote)
		if index < 0 {
			return -1
		}
		offset += index
		if current == occurrence {
			return offset
		}
		offset += len(quote)
	}
	return -1
}

func stableInterviewFindingID(
	snapshotID string,
	dimension InterviewDimension,
	kind interviewFindingKind,
	finding InterviewShadowFinding,
) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("interview-shadow-finding:v1\x00"))
	_, _ = hash.Write([]byte(snapshotID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(dimension))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(finding.Message))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(finding.Suggestion))
	for _, evidence := range finding.Evidence {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(evidence.EvidenceRefID))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.Itoa(evidence.StartUTF8Byte)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.Itoa(evidence.EndUTF8Byte)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(evidence.OriginalExcerpt))
	}
	return "interview_finding_" + hex.EncodeToString(hash.Sum(nil)[:16])
}

func ratio(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func sameRatio(left, right float64) bool {
	return math.Abs(left-right) <= 1e-12
}

func ensureInterviewJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return ErrInvalidInterviewShadow
}
