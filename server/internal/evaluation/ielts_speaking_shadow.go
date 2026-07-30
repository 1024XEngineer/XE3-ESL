package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

const (
	IELTSSpeakingShadowSchemaVersion         = "ielts-speaking-full-mock-shadow/v1"
	IELTSSpeakingShadowProviderSchemaVersion = "ielts-speaking-full-mock-shadow-provider/v1"
	IELTSSpeakingShadowPromptVersion         = "ielts-speaking-full-mock-shadow-prompt/v1"
	IELTSSpeakingShadowRubricVersion         = "ielts-speaking-transcript-rubric/v1"

	ieltsFullMockDefinitionID      = "scn_ielts_speaking_full"
	ieltsFullMockDefinitionVersion = 2
	ieltsFullMockConfigID          = "scfg_ielts_speaking_full"
	ieltsFullMockConfigVersion     = 2

	ieltsMaximumProviderPayload = 64 * 1024
	ieltsMaximumFindingText     = 2048
	ieltsMaximumFindings        = 3
	ieltsMaximumAnchors         = 4
	ieltsMaximumOccurrence      = 16
	ieltsQuestionCount          = 14
)

var ErrInvalidIELTSSpeakingShadow = errors.New(
	"evaluation: invalid IELTS Speaking shadow",
)

type IELTSCriterion string

const (
	IELTSCriterionFC  IELTSCriterion = "IELTS_FC"
	IELTSCriterionLR  IELTSCriterion = "IELTS_LR"
	IELTSCriterionGRA IELTSCriterion = "IELTS_GRA"
	IELTSCriterionPR  IELTSCriterion = "IELTS_PR"
)

var ieltsCriterionOrder = [...]IELTSCriterion{
	IELTSCriterionFC,
	IELTSCriterionLR,
	IELTSCriterionGRA,
	IELTSCriterionPR,
}

func IELTSCriteria() []IELTSCriterion {
	return slices.Clone(ieltsCriterionOrder[:])
}

type IELTSPart string

const (
	IELTSPart1 IELTSPart = "PART_1"
	IELTSPart2 IELTSPart = "PART_2"
	IELTSPart3 IELTSPart = "PART_3"
)

var ieltsPartOrder = [...]IELTSPart{
	IELTSPart1,
	IELTSPart2,
	IELTSPart3,
}

func IELTSParts() []IELTSPart {
	return slices.Clone(ieltsPartOrder[:])
}

type IELTSSpeakingScoreabilityStatus string

const (
	IELTSSpeakingScoreabilityProvisional  IELTSSpeakingScoreabilityStatus = "PROVISIONAL"
	IELTSSpeakingScoreabilityInsufficient IELTSSpeakingScoreabilityStatus = "INSUFFICIENT"
)

type IELTSSpeakingGateStatus string

const (
	IELTSSpeakingGateFeedbackOnly IELTSSpeakingGateStatus = "FEEDBACK_ONLY"
	IELTSSpeakingGateBlocked      IELTSSpeakingGateStatus = "BLOCKED"
)

type IELTSSpeakingReasonCode string

const (
	IELTSReasonASRConfidenceUnavailable         IELTSSpeakingReasonCode = "ASR_CONFIDENCE_UNAVAILABLE"
	IELTSReasonFluencyTimingUnavailable         IELTSSpeakingReasonCode = "FLUENCY_TIMING_UNAVAILABLE"
	IELTSReasonPronunciationArtifactUnavailable IELTSSpeakingReasonCode = "PRONUNCIATION_ARTIFACT_UNAVAILABLE"
	IELTSReasonInsufficientEvidence             IELTSSpeakingReasonCode = "INSUFFICIENT_EVIDENCE"
	IELTSReasonOpportunityNotProvided           IELTSSpeakingReasonCode = "OPPORTUNITY_NOT_PROVIDED"
)

type IELTSSpeakingOpportunityStatus string

const (
	IELTSOpportunityProvided    IELTSSpeakingOpportunityStatus = "PROVIDED"
	IELTSOpportunityNotProvided IELTSSpeakingOpportunityStatus = "NOT_PROVIDED"
)

type IELTSSpeakingAssessmentStatus string

const (
	IELTSAssessmentAssessed    IELTSSpeakingAssessmentStatus = "ASSESSED"
	IELTSAssessmentNotAssessed IELTSSpeakingAssessmentStatus = "NOT_ASSESSED"
)

type IELTSRubricDescriptor string

type IELTSRubricDescriptorSet struct {
	CriterionID IELTSCriterion          `json:"criterion_id"`
	Descriptors []IELTSRubricDescriptor `json:"descriptors"`
}

type IELTSSpeakingShadowProvider interface {
	AnalyzeIELTSSpeaking(
		context.Context,
		IELTSSpeakingShadowProviderInput,
	) (IELTSSpeakingShadowProviderResult, error)
}

type IELTSSpeakingShadowProviderResult struct {
	Payload   json.RawMessage
	Provider  string
	Model     string
	RequestID string
}

type IELTSSpeakingShadowProviderInput struct {
	SchemaVersion      string                          `json:"schema_version"`
	PromptVersion      string                          `json:"prompt_version"`
	RubricVersion      string                          `json:"rubric_version"`
	SceneType          SceneType                       `json:"scene_type"`
	ScenarioModel      string                          `json:"scenario_model"`
	RubricDescriptors  []IELTSRubricDescriptorSet      `json:"rubric_descriptors"`
	AssessableCriteria []IELTSCriterion                `json:"assessable_criteria"`
	Questions          []IELTSSpeakingProviderQuestion `json:"questions"`
}

type IELTSSpeakingProviderQuestion struct {
	QuestionID   string                         `json:"question_id"`
	PartID       IELTSPart                      `json:"part_id"`
	Index        int                            `json:"index"`
	QuestionText string                         `json:"question_text"`
	Response     *IELTSSpeakingProviderResponse `json:"response,omitempty"`
}

type IELTSSpeakingProviderResponse struct {
	TurnID        string `json:"turn_id"`
	EvidenceRefID string `json:"evidence_ref_id"`
	Transcript    string `json:"confirmed_transcript"`
}

type IELTSSpeakingShadowProviderLineage struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	RequestID      string `json:"request_id"`
	PromptVersion  string `json:"prompt_version"`
	ResponseSchema string `json:"response_schema"`
	RubricVersion  string `json:"rubric_version"`
}

type IELTSSpeakingShadowResult struct {
	SchemaVersion   string                               `json:"schema_version"`
	SnapshotID      string                               `json:"snapshot_id"`
	SceneType       SceneType                            `json:"scene_type"`
	Scope           Scope                                `json:"scope"`
	Channel         Channel                              `json:"channel"`
	Scoreability    IELTSSpeakingScoreabilityStatus      `json:"scoreability_status"`
	Gate            IELTSSpeakingGateStatus              `json:"gate_status"`
	ReasonCodes     []IELTSSpeakingReasonCode            `json:"reason_codes"`
	Criteria        []IELTSSpeakingShadowCriterionResult `json:"criteria"`
	QuestionResults []IELTSSpeakingShadowQuestionResult  `json:"question_results"`
	Provider        *IELTSSpeakingShadowProviderLineage  `json:"provider_lineage,omitempty"`
}

type IELTSSpeakingShadowCriterionResult struct {
	CriterionID     IELTSCriterion                  `json:"criterion_id"`
	Scoreability    IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	Gate            IELTSSpeakingGateStatus         `json:"gate_status"`
	EstimatedBand   *int                            `json:"estimated_band,omitempty"`
	BandDescriptor  string                          `json:"band_descriptor,omitempty"`
	Coverage        float64                         `json:"coverage"`
	Confidence      float64                         `json:"confidence"`
	ReasonCodes     []IELTSSpeakingReasonCode       `json:"reason_codes"`
	EvidenceRefIDs  []string                        `json:"evidence_ref_ids"`
	Strengths       []IELTSSpeakingShadowFinding    `json:"strengths"`
	Improvements    []IELTSSpeakingShadowFinding    `json:"improvements"`
	UpgradeExamples []IELTSSpeakingShadowFinding    `json:"upgrade_examples"`
}

type IELTSSpeakingShadowQuestionResult struct {
	QuestionID        string                                      `json:"question_id"`
	PartID            IELTSPart                                   `json:"part_id"`
	Index             int                                         `json:"index"`
	OpportunityStatus IELTSSpeakingOpportunityStatus              `json:"opportunity_status"`
	AssessmentStatus  IELTSSpeakingAssessmentStatus               `json:"assessment_status"`
	ResponseTurnID    string                                      `json:"response_turn_id,omitempty"`
	EvidenceRefIDs    []string                                    `json:"evidence_ref_ids"`
	CriterionFindings []IELTSSpeakingQuestionCriterionFindingRefs `json:"criterion_findings"`
}

type IELTSSpeakingQuestionCriterionFindingRefs struct {
	CriterionID              IELTSCriterion `json:"criterion_id"`
	StrengthFindingIDs       []string       `json:"strength_finding_ids"`
	ImprovementFindingIDs    []string       `json:"improvement_finding_ids"`
	UpgradeExampleFindingIDs []string       `json:"upgrade_example_finding_ids"`
}

type IELTSSpeakingShadowFinding struct {
	ID         string                        `json:"finding_id"`
	Message    string                        `json:"message"`
	Suggestion string                        `json:"suggestion,omitempty"`
	Evidence   []IELTSSpeakingShadowEvidence `json:"evidence"`
}

type IELTSSpeakingShadowEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	TurnID          string `json:"turn_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

type IELTSSpeakingShadowEngine struct {
	provider IELTSSpeakingShadowProvider
}

func NewIELTSSpeakingShadowEngine(
	provider IELTSSpeakingShadowProvider,
) *IELTSSpeakingShadowEngine {
	return &IELTSSpeakingShadowEngine{provider: provider}
}

func (engine *IELTSSpeakingShadowEngine) Evaluate(
	ctx context.Context,
	snapshot EvidenceSnapshot,
) (IELTSSpeakingShadowResult, error) {
	if engine == nil || engine.provider == nil || ctx == nil {
		return IELTSSpeakingShadowResult{}, ErrInvalidRequest
	}
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		return IELTSSpeakingShadowResult{}, err
	}
	if prepared.result.Scoreability ==
		IELTSSpeakingScoreabilityInsufficient {
		return prepared.result, nil
	}
	generated, err := engine.provider.AnalyzeIELTSSpeaking(
		ctx,
		prepared.input,
	)
	if err != nil {
		return IELTSSpeakingShadowResult{}, err
	}
	result, err := normalizeIELTSSpeakingProviderResult(
		prepared,
		generated,
	)
	if err != nil {
		return IELTSSpeakingShadowResult{}, err
	}
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		return IELTSSpeakingShadowResult{}, err
	}
	return result, nil
}

type preparedIELTSSpeakingShadow struct {
	evidence     evidencePayload
	input        IELTSSpeakingShadowProviderInput
	result       IELTSSpeakingShadowResult
	turnsByID    map[string]evidenceConfirmedTurn
	refsByID     map[string]evidenceRef
	refsByTurnID map[string]evidenceRef
	responseRefs map[string]struct{}
}

func prepareIELTSSpeakingShadow(
	snapshot EvidenceSnapshot,
) (preparedIELTSSpeakingShadow, error) {
	if !snapshot.Valid() ||
		snapshot.Scope != ScopeSession ||
		snapshot.SceneType != SceneIELTSSpeaking {
		return preparedIELTSSpeakingShadow{}, ErrInvalidRequest
	}
	var evidence evidencePayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&evidence) != nil || ensureJSONEOF(decoder) != nil ||
		!isFrozenIELTSFullMock(evidence.PracticeContext) ||
		len(evidence.OpportunityManifest) != ieltsQuestionCount {
		return preparedIELTSSpeakingShadow{}, ErrInvalidRequest
	}

	turnsByID := make(
		map[string]evidenceConfirmedTurn,
		len(evidence.ConfirmedTurns),
	)
	for _, turn := range evidence.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}
	refsByID := make(map[string]evidenceRef, len(evidence.EvidenceRefs))
	refsByTurnID := make(map[string]evidenceRef, len(evidence.EvidenceRefs))
	for _, ref := range evidence.EvidenceRefs {
		refsByID[ref.EvidenceRefID] = ref
		refsByTurnID[ref.TurnID] = ref
	}

	responseRefs := make(map[string]struct{}, ieltsQuestionCount)
	questions := make(
		[]IELTSSpeakingProviderQuestion,
		0,
		ieltsQuestionCount,
	)
	answered := 0
	for index, opportunity := range evidence.OpportunityManifest {
		sequence := index + 1
		if opportunity.Sequence != sequence {
			return preparedIELTSSpeakingShadow{}, ErrInvalidRequest
		}
		providerQuestion := IELTSSpeakingProviderQuestion{
			QuestionID:   opportunity.QuestionID,
			PartID:       ieltsPartForQuestionIndex(sequence),
			Index:        sequence,
			QuestionText: opportunity.QuestionText,
		}
		if opportunity.ResponseTurnID != "" {
			turn, turnExists := turnsByID[opportunity.ResponseTurnID]
			ref, refExists := refsByTurnID[opportunity.ResponseTurnID]
			if !turnExists || !refExists ||
				turn.Sequence != sequence ||
				turn.QuestionID != opportunity.QuestionID {
				return preparedIELTSSpeakingShadow{},
					ErrInvalidRequest
			}
			answered++
			responseRefs[ref.EvidenceRefID] = struct{}{}
			providerQuestion.Response =
				&IELTSSpeakingProviderResponse{
					TurnID:        turn.TurnID,
					EvidenceRefID: ref.EvidenceRefID,
					Transcript:    turn.Transcript.Text,
				}
		}
		questions = append(questions, providerQuestion)
	}

	prepared := preparedIELTSSpeakingShadow{
		evidence:     evidence,
		result:       ieltsSpeakingResultSkeleton(snapshot),
		turnsByID:    turnsByID,
		refsByID:     refsByID,
		refsByTurnID: refsByTurnID,
		responseRefs: responseRefs,
	}
	if answered != ieltsQuestionCount {
		prepared.result.Scoreability =
			IELTSSpeakingScoreabilityInsufficient
		prepared.result.Gate = IELTSSpeakingGateBlocked
		prepared.result.ReasonCodes = []IELTSSpeakingReasonCode{
			IELTSReasonOpportunityNotProvided,
		}
		prepared.result.Criteria = blockedIELTSCriteria(
			ratio(answered, ieltsQuestionCount),
			IELTSReasonOpportunityNotProvided,
		)
		prepared.result.QuestionResults =
			ieltsSpeakingQuestionResults(
				prepared,
				prepared.result.Criteria,
			)
		return prepared, nil
	}

	prepared.result.Scoreability =
		IELTSSpeakingScoreabilityProvisional
	prepared.result.Gate = IELTSSpeakingGateFeedbackOnly
	prepared.result.ReasonCodes = []IELTSSpeakingReasonCode{
		IELTSReasonASRConfidenceUnavailable,
		IELTSReasonFluencyTimingUnavailable,
		IELTSReasonPronunciationArtifactUnavailable,
	}
	prepared.input = IELTSSpeakingShadowProviderInput{
		SchemaVersion:     IELTSSpeakingShadowProviderSchemaVersion,
		PromptVersion:     IELTSSpeakingShadowPromptVersion,
		RubricVersion:     IELTSSpeakingShadowRubricVersion,
		SceneType:         SceneIELTSSpeaking,
		ScenarioModel:     string(practice.ScenarioModelIELTSSpeakingFullMock),
		RubricDescriptors: ieltsRubricDescriptorSets(),
		AssessableCriteria: []IELTSCriterion{
			IELTSCriterionFC,
			IELTSCriterionLR,
			IELTSCriterionGRA,
		},
		Questions: questions,
	}
	if !validIELTSSpeakingProviderInput(prepared.input) {
		return preparedIELTSSpeakingShadow{}, ErrInvalidRequest
	}
	return prepared, nil
}

func isFrozenIELTSFullMock(context evidencePracticeContext) bool {
	return context.SceneFamily ==
		string(practice.ScenarioFamilyExam) &&
		context.ScenarioModel ==
			string(practice.ScenarioModelIELTSSpeakingFullMock) &&
		context.ScenarioDefinition.ID == ieltsFullMockDefinitionID &&
		context.ScenarioDefinition.Version ==
			ieltsFullMockDefinitionVersion &&
		context.ScenarioConfig.ID == ieltsFullMockConfigID &&
		context.ScenarioConfig.Version == ieltsFullMockConfigVersion &&
		len(context.TaskBlueprints) == ieltsQuestionCount
}

func ieltsPartForQuestionIndex(index int) IELTSPart {
	switch {
	case index >= 1 && index <= 8:
		return IELTSPart1
	case index == 9:
		return IELTSPart2
	case index >= 10 && index <= 14:
		return IELTSPart3
	default:
		return ""
	}
}

func ieltsSpeakingResultSkeleton(
	snapshot EvidenceSnapshot,
) IELTSSpeakingShadowResult {
	return IELTSSpeakingShadowResult{
		SchemaVersion:   IELTSSpeakingShadowSchemaVersion,
		SnapshotID:      snapshot.ID,
		SceneType:       SceneIELTSSpeaking,
		Scope:           ScopeSession,
		Channel:         ChannelScene,
		ReasonCodes:     []IELTSSpeakingReasonCode{},
		Criteria:        []IELTSSpeakingShadowCriterionResult{},
		QuestionResults: []IELTSSpeakingShadowQuestionResult{},
	}
}

func blockedIELTSCriteria(
	coverage float64,
	reason IELTSSpeakingReasonCode,
) []IELTSSpeakingShadowCriterionResult {
	result := make(
		[]IELTSSpeakingShadowCriterionResult,
		0,
		len(ieltsCriterionOrder),
	)
	for _, criterion := range ieltsCriterionOrder {
		criterionReason := reason
		switch criterion {
		case IELTSCriterionFC:
			if reason != IELTSReasonOpportunityNotProvided {
				criterionReason = IELTSReasonFluencyTimingUnavailable
			}
		case IELTSCriterionPR:
			criterionReason =
				IELTSReasonPronunciationArtifactUnavailable
		}
		result = append(
			result,
			blockedIELTSCriterion(
				criterion,
				coverage,
				criterionReason,
			),
		)
	}
	return result
}

func blockedIELTSCriterion(
	criterion IELTSCriterion,
	coverage float64,
	reason IELTSSpeakingReasonCode,
) IELTSSpeakingShadowCriterionResult {
	return IELTSSpeakingShadowCriterionResult{
		CriterionID:     criterion,
		Scoreability:    IELTSSpeakingScoreabilityInsufficient,
		Gate:            IELTSSpeakingGateBlocked,
		Coverage:        coverage,
		Confidence:      0,
		ReasonCodes:     []IELTSSpeakingReasonCode{reason},
		EvidenceRefIDs:  []string{},
		Strengths:       []IELTSSpeakingShadowFinding{},
		Improvements:    []IELTSSpeakingShadowFinding{},
		UpgradeExamples: []IELTSSpeakingShadowFinding{},
	}
}

type ieltsProviderPayload struct {
	SchemaVersion string                   `json:"schema_version"`
	Criteria      []ieltsProviderCriterion `json:"criteria"`
}

type ieltsProviderCriterion struct {
	CriterionID      IELTSCriterion         `json:"criterion_id"`
	RubricDescriptor IELTSRubricDescriptor  `json:"rubric_descriptor,omitempty"`
	Strengths        []ieltsProviderFinding `json:"strengths"`
	Improvements     []ieltsProviderFinding `json:"improvements"`
	UpgradeExamples  []ieltsProviderFinding `json:"upgrade_examples"`
}

type ieltsProviderFinding struct {
	TemplateID string                `json:"template_id"`
	Suggestion string                `json:"suggestion,omitempty"`
	Evidence   []ieltsProviderAnchor `json:"evidence"`
}

type ieltsProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

type ieltsFindingKind string

const (
	ieltsFindingStrength    ieltsFindingKind = "STRENGTH"
	ieltsFindingImprovement ieltsFindingKind = "IMPROVEMENT"
	ieltsFindingUpgrade     ieltsFindingKind = "UPGRADE_EXAMPLE"
)

type ieltsFeedbackTemplate struct {
	ID      string
	Message string
}

func lookupIELTSFeedbackTemplate(
	criterion IELTSCriterion,
	kind ieltsFindingKind,
) (ieltsFeedbackTemplate, bool) {
	criterionName := map[IELTSCriterion]string{
		IELTSCriterionFC:  "coherence",
		IELTSCriterionLR:  "lexical resource",
		IELTSCriterionGRA: "grammar",
	}[criterion]
	if criterionName == "" || criterion == IELTSCriterionPR {
		return ieltsFeedbackTemplate{}, false
	}
	switch kind {
	case ieltsFindingStrength:
		return ieltsFeedbackTemplate{
			ID: "ielts." + strings.ToLower(
				strings.TrimPrefix(string(criterion), "IELTS_"),
			) + ".strength.v1",
			Message: "This excerpt provides supported " +
				criterionName + " evidence.",
		}, true
	case ieltsFindingImprovement:
		return ieltsFeedbackTemplate{
			ID: "ielts." + strings.ToLower(
				strings.TrimPrefix(string(criterion), "IELTS_"),
			) + ".improvement.v1",
			Message: "This excerpt shows a supported " +
				criterionName + " improvement opportunity.",
		}, true
	case ieltsFindingUpgrade:
		return ieltsFeedbackTemplate{
			ID: "ielts." + strings.ToLower(
				strings.TrimPrefix(string(criterion), "IELTS_"),
			) + ".upgrade.v1",
			Message: "A clearer practice expression can be tried " +
				"for this excerpt.",
		}, true
	default:
		return ieltsFeedbackTemplate{}, false
	}
}

func normalizeIELTSSpeakingProviderResult(
	prepared preparedIELTSSpeakingShadow,
	generated IELTSSpeakingShadowProviderResult,
) (IELTSSpeakingShadowResult, error) {
	if len(generated.Payload) == 0 ||
		len(generated.Payload) > ieltsMaximumProviderPayload ||
		!validProviderIdentifier(generated.Provider) ||
		!validProviderIdentifier(generated.Model) ||
		!validProviderIdentifier(generated.RequestID) {
		return IELTSSpeakingShadowResult{},
			ErrInvalidIELTSSpeakingShadow
	}
	var payload ieltsProviderPayload
	decoder := json.NewDecoder(bytes.NewReader(generated.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil ||
		ensureJSONEOF(decoder) != nil ||
		payload.SchemaVersion !=
			IELTSSpeakingShadowProviderSchemaVersion ||
		len(payload.Criteria) != 3 {
		return IELTSSpeakingShadowResult{},
			ErrInvalidIELTSSpeakingShadow
	}
	byCriterion := make(
		map[IELTSCriterion]ieltsProviderCriterion,
		len(payload.Criteria),
	)
	for index, criterion := range payload.Criteria {
		expected := ieltsCriterionOrder[index]
		if expected == IELTSCriterionPR ||
			criterion.CriterionID != expected {
			return IELTSSpeakingShadowResult{},
				ErrInvalidIELTSSpeakingShadow
		}
		if _, duplicate := byCriterion[criterion.CriterionID]; duplicate {
			return IELTSSpeakingShadowResult{},
				ErrInvalidIELTSSpeakingShadow
		}
		byCriterion[criterion.CriterionID] = criterion
	}

	result := prepared.result
	result.Criteria = make(
		[]IELTSSpeakingShadowCriterionResult,
		0,
		len(ieltsCriterionOrder),
	)
	result.Provider = &IELTSSpeakingShadowProviderLineage{
		Provider:       generated.Provider,
		Model:          generated.Model,
		RequestID:      generated.RequestID,
		PromptVersion:  IELTSSpeakingShadowPromptVersion,
		ResponseSchema: IELTSSpeakingShadowProviderSchemaVersion,
		RubricVersion:  IELTSSpeakingShadowRubricVersion,
	}
	for _, criterionID := range ieltsCriterionOrder[:3] {
		criterion, err := normalizeIELTSProviderCriterion(
			prepared,
			byCriterion[criterionID],
		)
		if err != nil {
			return IELTSSpeakingShadowResult{}, err
		}
		result.Criteria = append(result.Criteria, criterion)
	}
	result.Criteria = append(
		result.Criteria,
		blockedIELTSCriterion(
			IELTSCriterionPR,
			1,
			IELTSReasonPronunciationArtifactUnavailable,
		),
	)
	result.QuestionResults = ieltsSpeakingQuestionResults(
		prepared,
		result.Criteria,
	)
	return result, nil
}

func normalizeIELTSProviderCriterion(
	prepared preparedIELTSSpeakingShadow,
	source ieltsProviderCriterion,
) (IELTSSpeakingShadowCriterionResult, error) {
	if source.CriterionID == IELTSCriterionPR ||
		source.Strengths == nil ||
		source.Improvements == nil ||
		source.UpgradeExamples == nil ||
		len(source.Strengths) > ieltsMaximumFindings ||
		len(source.Improvements) > ieltsMaximumFindings ||
		len(source.UpgradeExamples) > ieltsMaximumFindings ||
		len(source.Strengths)+len(source.Improvements) == 0 {
		return IELTSSpeakingShadowCriterionResult{},
			ErrInvalidIELTSSpeakingShadow
	}
	result := IELTSSpeakingShadowCriterionResult{
		CriterionID:  source.CriterionID,
		Scoreability: IELTSSpeakingScoreabilityProvisional,
		Gate:         IELTSSpeakingGateFeedbackOnly,
		Coverage:     1,
		Confidence:   0,
		ReasonCodes: []IELTSSpeakingReasonCode{
			IELTSReasonASRConfidenceUnavailable,
		},
		EvidenceRefIDs:  []string{},
		Strengths:       []IELTSSpeakingShadowFinding{},
		Improvements:    []IELTSSpeakingShadowFinding{},
		UpgradeExamples: []IELTSSpeakingShadowFinding{},
	}
	if source.CriterionID == IELTSCriterionFC {
		if source.RubricDescriptor != "" {
			return IELTSSpeakingShadowCriterionResult{},
				ErrInvalidIELTSSpeakingShadow
		}
		result.ReasonCodes = append(
			result.ReasonCodes,
			IELTSReasonFluencyTimingUnavailable,
		)
	} else {
		band, descriptor, ok := mapIELTSRubricDescriptor(
			source.CriterionID,
			source.RubricDescriptor,
		)
		if !ok {
			return IELTSSpeakingShadowCriterionResult{},
				ErrInvalidIELTSSpeakingShadow
		}
		result.EstimatedBand = &band
		result.BandDescriptor = descriptor
	}
	var err error
	result.Strengths, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingStrength,
		source.Strengths,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, err
	}
	result.Improvements, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingImprovement,
		source.Improvements,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, err
	}
	result.UpgradeExamples, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingUpgrade,
		source.UpgradeExamples,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, err
	}
	refSet := make(map[string]struct{})
	for _, findings := range [][]IELTSSpeakingShadowFinding{
		result.Strengths,
		result.Improvements,
		result.UpgradeExamples,
	} {
		for _, finding := range findings {
			for _, evidence := range finding.Evidence {
				refSet[evidence.EvidenceRefID] = struct{}{}
			}
		}
	}
	for refID := range refSet {
		result.EvidenceRefIDs = append(
			result.EvidenceRefIDs,
			refID,
		)
	}
	slices.Sort(result.EvidenceRefIDs)
	if len(result.EvidenceRefIDs) == 0 {
		return IELTSSpeakingShadowCriterionResult{},
			ErrInvalidIELTSSpeakingShadow
	}
	result.Coverage = ratio(
		len(result.EvidenceRefIDs),
		ieltsQuestionCount,
	)
	return result, nil
}

func normalizeIELTSFindings(
	prepared preparedIELTSSpeakingShadow,
	criterion IELTSCriterion,
	kind ieltsFindingKind,
	source []ieltsProviderFinding,
) ([]IELTSSpeakingShadowFinding, error) {
	result := make([]IELTSSpeakingShadowFinding, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	template, exists := lookupIELTSFeedbackTemplate(criterion, kind)
	if !exists {
		return nil, ErrInvalidIELTSSpeakingShadow
	}
	for _, item := range source {
		if item.TemplateID != template.ID ||
			len(item.Evidence) == 0 ||
			len(item.Evidence) > ieltsMaximumAnchors ||
			(kind == ieltsFindingStrength && item.Suggestion != "") ||
			(item.Suggestion != "" &&
				!validInterviewText(
					item.Suggestion,
					ieltsMaximumFindingText,
				)) {
			return nil, ErrInvalidIELTSSpeakingShadow
		}
		evidence := make(
			[]IELTSSpeakingShadowEvidence,
			0,
			len(item.Evidence),
		)
		seenAnchors := make(map[string]struct{}, len(item.Evidence))
		for _, anchor := range item.Evidence {
			resolved, err := resolveIELTSProviderAnchor(
				prepared,
				anchor,
			)
			if err != nil {
				return nil, err
			}
			key := resolved.EvidenceRefID + "\x00" +
				strconv.Itoa(resolved.StartUTF8Byte) + "\x00" +
				strconv.Itoa(resolved.EndUTF8Byte)
			if _, duplicate := seenAnchors[key]; duplicate {
				return nil, ErrInvalidIELTSSpeakingShadow
			}
			seenAnchors[key] = struct{}{}
			evidence = append(evidence, resolved)
		}
		finding := IELTSSpeakingShadowFinding{
			Message:    template.Message,
			Suggestion: item.Suggestion,
			Evidence:   evidence,
		}
		finding.ID = stableIELTSFindingID(
			prepared.result.SnapshotID,
			criterion,
			kind,
			finding,
		)
		if _, duplicate := seen[finding.ID]; duplicate {
			return nil, ErrInvalidIELTSSpeakingShadow
		}
		seen[finding.ID] = struct{}{}
		result = append(result, finding)
	}
	return result, nil
}

func resolveIELTSProviderAnchor(
	prepared preparedIELTSSpeakingShadow,
	anchor ieltsProviderAnchor,
) (IELTSSpeakingShadowEvidence, error) {
	ref, exists := prepared.refsByID[anchor.EvidenceRefID]
	_, allowed := prepared.responseRefs[anchor.EvidenceRefID]
	turn, turnExists := prepared.turnsByID[ref.TurnID]
	if !exists || !allowed || !turnExists ||
		anchor.Occurrence < 1 ||
		anchor.Occurrence > ieltsMaximumOccurrence ||
		!validInterviewText(anchor.Quote, ieltsMaximumFindingText) {
		return IELTSSpeakingShadowEvidence{},
			ErrInvalidIELTSSpeakingShadow
	}
	start := nthInterviewOccurrence(
		turn.Transcript.Text,
		anchor.Quote,
		anchor.Occurrence,
	)
	end := start + len(anchor.Quote)
	if start < 0 || end <= start ||
		start < ref.TranscriptSpan.StartUTF8Byte ||
		end > ref.TranscriptSpan.EndUTF8Byte ||
		!utf8.ValidString(turn.Transcript.Text[start:end]) {
		return IELTSSpeakingShadowEvidence{},
			ErrInvalidIELTSSpeakingShadow
	}
	return IELTSSpeakingShadowEvidence{
		EvidenceRefID:   ref.EvidenceRefID,
		TurnID:          ref.TurnID,
		StartUTF8Byte:   start,
		EndUTF8Byte:     end,
		OriginalExcerpt: turn.Transcript.Text[start:end],
	}, nil
}

func ieltsSpeakingQuestionResults(
	prepared preparedIELTSSpeakingShadow,
	criteria []IELTSSpeakingShadowCriterionResult,
) []IELTSSpeakingShadowQuestionResult {
	results := make(
		[]IELTSSpeakingShadowQuestionResult,
		0,
		ieltsQuestionCount,
	)
	for index, opportunity := range prepared.evidence.OpportunityManifest {
		question := IELTSSpeakingShadowQuestionResult{
			QuestionID:        opportunity.QuestionID,
			PartID:            ieltsPartForQuestionIndex(index + 1),
			Index:             index + 1,
			OpportunityStatus: IELTSOpportunityNotProvided,
			AssessmentStatus:  IELTSAssessmentNotAssessed,
			EvidenceRefIDs:    []string{},
			CriterionFindings: make(
				[]IELTSSpeakingQuestionCriterionFindingRefs,
				0,
				len(criteria),
			),
		}
		var responseRef evidenceRef
		if opportunity.ResponseTurnID != "" {
			question.OpportunityStatus = IELTSOpportunityProvided
			question.AssessmentStatus = IELTSAssessmentAssessed
			question.ResponseTurnID = opportunity.ResponseTurnID
			responseRef =
				prepared.refsByTurnID[opportunity.ResponseTurnID]
			question.EvidenceRefIDs =
				[]string{responseRef.EvidenceRefID}
		}
		for _, criterion := range criteria {
			question.CriterionFindings = append(
				question.CriterionFindings,
				IELTSSpeakingQuestionCriterionFindingRefs{
					CriterionID: criterion.CriterionID,
					StrengthFindingIDs: ieltsFindingIDsForEvidenceRef(
						criterion.Strengths,
						responseRef.EvidenceRefID,
					),
					ImprovementFindingIDs: ieltsFindingIDsForEvidenceRef(
						criterion.Improvements,
						responseRef.EvidenceRefID,
					),
					UpgradeExampleFindingIDs: ieltsFindingIDsForEvidenceRef(
						criterion.UpgradeExamples,
						responseRef.EvidenceRefID,
					),
				},
			)
		}
		results = append(results, question)
	}
	return results
}

func ieltsFindingIDsForEvidenceRef(
	findings []IELTSSpeakingShadowFinding,
	evidenceRefID string,
) []string {
	result := make([]string, 0, len(findings))
	if evidenceRefID == "" {
		return result
	}
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

func ieltsRubricDescriptorSets() []IELTSRubricDescriptorSet {
	return []IELTSRubricDescriptorSet{
		{CriterionID: IELTSCriterionLR, Descriptors: ieltsDescriptorsFor(
			IELTSCriterionLR,
		)},
		{CriterionID: IELTSCriterionGRA, Descriptors: ieltsDescriptorsFor(
			IELTSCriterionGRA,
		)},
	}
}

func ieltsDescriptorsFor(
	criterion IELTSCriterion,
) []IELTSRubricDescriptor {
	if criterion != IELTSCriterionLR &&
		criterion != IELTSCriterionGRA {
		return []IELTSRubricDescriptor{}
	}
	prefix := strings.TrimPrefix(string(criterion), "IELTS_")
	result := make([]IELTSRubricDescriptor, 0, 9)
	for band := 1; band <= 9; band++ {
		result = append(
			result,
			IELTSRubricDescriptor(
				prefix+"_PRACTICE_BAND_"+strconv.Itoa(band),
			),
		)
	}
	return result
}

func mapIELTSRubricDescriptor(
	criterion IELTSCriterion,
	descriptor IELTSRubricDescriptor,
) (int, string, bool) {
	allowed := ieltsDescriptorsFor(criterion)
	index := slices.Index(allowed, descriptor)
	if index < 0 {
		return 0, "", false
	}
	band := index + 1
	label := "Vocabulary"
	if criterion == IELTSCriterionGRA {
		label = "Grammar"
	}
	return band, fmt.Sprintf(
		"%s transcript evidence aligns with the practice Band %d descriptor.",
		label,
		band,
	), true
}

func validIELTSSpeakingProviderInput(
	input IELTSSpeakingShadowProviderInput,
) bool {
	if input.SchemaVersion !=
		IELTSSpeakingShadowProviderSchemaVersion ||
		input.PromptVersion != IELTSSpeakingShadowPromptVersion ||
		input.RubricVersion != IELTSSpeakingShadowRubricVersion ||
		input.SceneType != SceneIELTSSpeaking ||
		input.ScenarioModel !=
			string(practice.ScenarioModelIELTSSpeakingFullMock) ||
		!slices.Equal(
			input.AssessableCriteria,
			[]IELTSCriterion{
				IELTSCriterionFC,
				IELTSCriterionLR,
				IELTSCriterionGRA,
			},
		) ||
		len(input.RubricDescriptors) != 2 ||
		len(input.Questions) != ieltsQuestionCount {
		return false
	}
	for index, set := range input.RubricDescriptors {
		expected := []IELTSCriterion{
			IELTSCriterionLR,
			IELTSCriterionGRA,
		}[index]
		if set.CriterionID != expected ||
			!slices.Equal(
				set.Descriptors,
				ieltsDescriptorsFor(expected),
			) {
			return false
		}
	}
	seenQuestions := make(map[string]struct{}, ieltsQuestionCount)
	seenTurns := make(map[string]struct{}, ieltsQuestionCount)
	seenRefs := make(map[string]struct{}, ieltsQuestionCount)
	for index, question := range input.Questions {
		expected := index + 1
		if question.Index != expected ||
			question.PartID != ieltsPartForQuestionIndex(expected) ||
			!validIdentifier(question.QuestionID) ||
			!validInterviewText(
				question.QuestionText,
				interviewShadowMaximumInputString,
			) ||
			question.Response == nil ||
			!validIdentifier(question.Response.TurnID) ||
			!validIdentifier(question.Response.EvidenceRefID) ||
			!validInterviewText(
				question.Response.Transcript,
				interviewShadowMaximumInputString,
			) {
			return false
		}
		if _, duplicate := seenQuestions[question.QuestionID]; duplicate {
			return false
		}
		if _, duplicate := seenTurns[question.Response.TurnID]; duplicate {
			return false
		}
		if _, duplicate := seenRefs[question.Response.EvidenceRefID]; duplicate {
			return false
		}
		seenQuestions[question.QuestionID] = struct{}{}
		seenTurns[question.Response.TurnID] = struct{}{}
		seenRefs[question.Response.EvidenceRefID] = struct{}{}
	}
	return true
}

func stableIELTSFindingID(
	snapshotID string,
	criterion IELTSCriterion,
	kind ieltsFindingKind,
	finding IELTSSpeakingShadowFinding,
) string {
	hash := sha256.New()
	for _, value := range []string{
		snapshotID,
		string(criterion),
		string(kind),
		finding.Message,
		finding.Suggestion,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, evidence := range finding.Evidence {
		for _, value := range []string{
			evidence.EvidenceRefID,
			evidence.TurnID,
			strconv.Itoa(evidence.StartUTF8Byte),
			strconv.Itoa(evidence.EndUTF8Byte),
			evidence.OriginalExcerpt,
		} {
			_, _ = hash.Write([]byte(value))
			_, _ = hash.Write([]byte{0})
		}
	}
	return "ielts_fnd_" + hex.EncodeToString(hash.Sum(nil)[:12])
}

func ValidateIELTSSpeakingShadowResult(
	snapshot EvidenceSnapshot,
	result IELTSSpeakingShadowResult,
) error {
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		return err
	}
	if result.SchemaVersion != IELTSSpeakingShadowSchemaVersion ||
		result.SnapshotID != snapshot.ID ||
		result.SceneType != SceneIELTSSpeaking ||
		result.Scope != ScopeSession ||
		result.Channel != ChannelScene ||
		result.Scoreability != prepared.result.Scoreability ||
		result.Gate != prepared.result.Gate ||
		len(result.Criteria) != len(ieltsCriterionOrder) ||
		len(result.QuestionResults) != ieltsQuestionCount {
		return ErrInvalidIELTSSpeakingShadow
	}
	switch result.Scoreability {
	case IELTSSpeakingScoreabilityProvisional:
		if result.Gate != IELTSSpeakingGateFeedbackOnly ||
			!slices.Equal(
				result.ReasonCodes,
				[]IELTSSpeakingReasonCode{
					IELTSReasonASRConfidenceUnavailable,
					IELTSReasonFluencyTimingUnavailable,
					IELTSReasonPronunciationArtifactUnavailable,
				},
			) ||
			result.Provider == nil ||
			!validIELTSProviderLineage(*result.Provider) {
			return ErrInvalidIELTSSpeakingShadow
		}
	case IELTSSpeakingScoreabilityInsufficient:
		if result.Gate != IELTSSpeakingGateBlocked ||
			!slices.Equal(
				result.ReasonCodes,
				[]IELTSSpeakingReasonCode{
					IELTSReasonOpportunityNotProvided,
				},
			) ||
			result.Provider != nil {
			return ErrInvalidIELTSSpeakingShadow
		}
	default:
		return ErrInvalidIELTSSpeakingShadow
	}

	allFindingIDs := make(map[string]struct{})
	findingRefs := make(
		map[IELTSCriterion]map[string]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	answered := 0
	for _, opportunity := range prepared.evidence.OpportunityManifest {
		if opportunity.ResponseTurnID != "" {
			answered++
		}
	}
	for index, criterion := range result.Criteria {
		expectedScoreability := result.Scoreability
		if criterion.CriterionID == IELTSCriterionPR {
			expectedScoreability =
				IELTSSpeakingScoreabilityInsufficient
		}
		if criterion.CriterionID != ieltsCriterionOrder[index] ||
			criterion.Scoreability != expectedScoreability ||
			criterion.Confidence != 0 ||
			criterion.ReasonCodes == nil ||
			criterion.EvidenceRefIDs == nil ||
			criterion.Strengths == nil ||
			criterion.Improvements == nil ||
			criterion.UpgradeExamples == nil ||
			len(criterion.Strengths) > ieltsMaximumFindings ||
			len(criterion.Improvements) > ieltsMaximumFindings ||
			len(criterion.UpgradeExamples) > ieltsMaximumFindings {
			return ErrInvalidIELTSSpeakingShadow
		}
		findingRefs[criterion.CriterionID] =
			make(map[string]map[string]struct{})
		expectedRefSet := make(map[string]struct{})
		for _, collection := range []struct {
			kind     ieltsFindingKind
			findings []IELTSSpeakingShadowFinding
		}{
			{
				kind:     ieltsFindingStrength,
				findings: criterion.Strengths,
			},
			{
				kind:     ieltsFindingImprovement,
				findings: criterion.Improvements,
			},
			{
				kind:     ieltsFindingUpgrade,
				findings: criterion.UpgradeExamples,
			},
		} {
			for _, finding := range collection.findings {
				if !validIELTSFinding(
					prepared,
					criterion.CriterionID,
					collection.kind,
					finding,
				) {
					return ErrInvalidIELTSSpeakingShadow
				}
				if _, duplicate := allFindingIDs[finding.ID]; duplicate {
					return ErrInvalidIELTSSpeakingShadow
				}
				allFindingIDs[finding.ID] = struct{}{}
				refs := make(
					map[string]struct{},
					len(finding.Evidence),
				)
				for _, evidence := range finding.Evidence {
					expectedRefSet[evidence.EvidenceRefID] =
						struct{}{}
					refs[evidence.EvidenceRefID] = struct{}{}
				}
				findingRefs[criterion.CriterionID][finding.ID] = refs
			}
		}
		expectedRefs := make([]string, 0, len(expectedRefSet))
		for refID := range expectedRefSet {
			expectedRefs = append(expectedRefs, refID)
		}
		slices.Sort(expectedRefs)
		if !slices.Equal(criterion.EvidenceRefIDs, expectedRefs) {
			return ErrInvalidIELTSSpeakingShadow
		}
		switch criterion.Scoreability {
		case IELTSSpeakingScoreabilityProvisional:
			if criterion.Gate !=
				IELTSSpeakingGateFeedbackOnly ||
				len(criterion.EvidenceRefIDs) == 0 ||
				len(criterion.Strengths)+
					len(criterion.Improvements) == 0 ||
				!sameRatio(
					criterion.Coverage,
					ratio(
						len(criterion.EvidenceRefIDs),
						ieltsQuestionCount,
					),
				) ||
				result.Scoreability !=
					IELTSSpeakingScoreabilityProvisional {
				return ErrInvalidIELTSSpeakingShadow
			}
			switch criterion.CriterionID {
			case IELTSCriterionFC:
				if criterion.EstimatedBand != nil ||
					criterion.BandDescriptor != "" ||
					!slices.Equal(
						criterion.ReasonCodes,
						[]IELTSSpeakingReasonCode{
							IELTSReasonASRConfidenceUnavailable,
							IELTSReasonFluencyTimingUnavailable,
						},
					) {
					return ErrInvalidIELTSSpeakingShadow
				}
			case IELTSCriterionLR, IELTSCriterionGRA:
				if criterion.EstimatedBand == nil ||
					!slices.Equal(
						criterion.ReasonCodes,
						[]IELTSSpeakingReasonCode{
							IELTSReasonASRConfidenceUnavailable,
						},
					) {
					return ErrInvalidIELTSSpeakingShadow
				}
				_, descriptor, ok := mapIELTSBand(
					criterion.CriterionID,
					*criterion.EstimatedBand,
				)
				if !ok ||
					criterion.BandDescriptor != descriptor {
					return ErrInvalidIELTSSpeakingShadow
				}
			default:
				return ErrInvalidIELTSSpeakingShadow
			}
		case IELTSSpeakingScoreabilityInsufficient:
			if criterion.Gate != IELTSSpeakingGateBlocked ||
				criterion.EstimatedBand != nil ||
				criterion.BandDescriptor != "" ||
				len(criterion.EvidenceRefIDs) != 0 ||
				len(criterion.Strengths) != 0 ||
				len(criterion.Improvements) != 0 ||
				len(criterion.UpgradeExamples) != 0 ||
				!sameRatio(
					criterion.Coverage,
					ratio(answered, ieltsQuestionCount),
				) ||
				len(criterion.ReasonCodes) != 1 ||
				!validIELTSBlockedReason(
					criterion.CriterionID,
					criterion.ReasonCodes[0],
					result.Scoreability,
				) {
				return ErrInvalidIELTSSpeakingShadow
			}
		default:
			return ErrInvalidIELTSSpeakingShadow
		}
	}

	for index, question := range result.QuestionResults {
		opportunity := prepared.evidence.OpportunityManifest[index]
		if question.QuestionID != opportunity.QuestionID ||
			question.Index != index+1 ||
			question.PartID != ieltsPartForQuestionIndex(index+1) ||
			question.EvidenceRefIDs == nil ||
			len(question.CriterionFindings) !=
				len(ieltsCriterionOrder) {
			return ErrInvalidIELTSSpeakingShadow
		}
		var expectedRefID string
		if opportunity.ResponseTurnID == "" {
			if question.OpportunityStatus !=
				IELTSOpportunityNotProvided ||
				question.AssessmentStatus !=
					IELTSAssessmentNotAssessed ||
				question.ResponseTurnID != "" ||
				len(question.EvidenceRefIDs) != 0 {
				return ErrInvalidIELTSSpeakingShadow
			}
		} else {
			ref, exists := prepared.refsByTurnID[opportunity.ResponseTurnID]
			if !exists ||
				question.OpportunityStatus !=
					IELTSOpportunityProvided ||
				question.AssessmentStatus !=
					IELTSAssessmentAssessed ||
				question.ResponseTurnID !=
					opportunity.ResponseTurnID ||
				!slices.Equal(
					question.EvidenceRefIDs,
					[]string{ref.EvidenceRefID},
				) {
				return ErrInvalidIELTSSpeakingShadow
			}
			expectedRefID = ref.EvidenceRefID
		}
		for criterionIndex, refs := range question.CriterionFindings {
			criterion := result.Criteria[criterionIndex]
			if refs.CriterionID != criterion.CriterionID ||
				!validIELTSQuestionFindingIDs(
					refs.StrengthFindingIDs,
					criterion.Strengths,
					expectedRefID,
				) ||
				!validIELTSQuestionFindingIDs(
					refs.ImprovementFindingIDs,
					criterion.Improvements,
					expectedRefID,
				) ||
				!validIELTSQuestionFindingIDs(
					refs.UpgradeExampleFindingIDs,
					criterion.UpgradeExamples,
					expectedRefID,
				) {
				return ErrInvalidIELTSSpeakingShadow
			}
		}
	}
	return nil
}

func validIELTSProviderLineage(
	lineage IELTSSpeakingShadowProviderLineage,
) bool {
	return validProviderIdentifier(lineage.Provider) &&
		validProviderIdentifier(lineage.Model) &&
		validProviderIdentifier(lineage.RequestID) &&
		lineage.PromptVersion == IELTSSpeakingShadowPromptVersion &&
		lineage.ResponseSchema ==
			IELTSSpeakingShadowProviderSchemaVersion &&
		lineage.RubricVersion == IELTSSpeakingShadowRubricVersion
}

func validIELTSBlockedReason(
	criterion IELTSCriterion,
	reason IELTSSpeakingReasonCode,
	rootStatus IELTSSpeakingScoreabilityStatus,
) bool {
	if criterion == IELTSCriterionPR {
		return reason == IELTSReasonPronunciationArtifactUnavailable
	}
	if rootStatus == IELTSSpeakingScoreabilityInsufficient {
		return reason == IELTSReasonOpportunityNotProvided ||
			reason == IELTSReasonInsufficientEvidence
	}
	switch criterion {
	case IELTSCriterionFC:
		return reason == IELTSReasonFluencyTimingUnavailable ||
			reason == IELTSReasonInsufficientEvidence
	default:
		return reason == IELTSReasonInsufficientEvidence
	}
}

func validIELTSFinding(
	prepared preparedIELTSSpeakingShadow,
	criterion IELTSCriterion,
	kind ieltsFindingKind,
	finding IELTSSpeakingShadowFinding,
) bool {
	if !validIdentifier(finding.ID) ||
		!validInterviewText(
			finding.Message,
			ieltsMaximumFindingText,
		) ||
		(kind == ieltsFindingStrength && finding.Suggestion != "") ||
		(finding.Suggestion != "" &&
			!validInterviewText(
				finding.Suggestion,
				ieltsMaximumFindingText,
			)) ||
		len(finding.Evidence) == 0 ||
		len(finding.Evidence) > ieltsMaximumAnchors {
		return false
	}
	seen := make(map[string]struct{}, len(finding.Evidence))
	for _, evidence := range finding.Evidence {
		ref, refExists := prepared.refsByID[evidence.EvidenceRefID]
		_, allowed := prepared.responseRefs[evidence.EvidenceRefID]
		turn, turnExists := prepared.turnsByID[evidence.TurnID]
		var excerpt string
		if turnExists && evidence.StartUTF8Byte >= 0 &&
			evidence.EndUTF8Byte > evidence.StartUTF8Byte &&
			evidence.EndUTF8Byte <= len([]byte(turn.Transcript.Text)) {
			excerpt = turn.Transcript.Text[evidence.StartUTF8Byte:evidence.EndUTF8Byte]
		}
		if !refExists || !allowed || !turnExists ||
			ref.TurnID != evidence.TurnID ||
			evidence.StartUTF8Byte < 0 ||
			evidence.EndUTF8Byte <= evidence.StartUTF8Byte ||
			evidence.StartUTF8Byte <
				ref.TranscriptSpan.StartUTF8Byte ||
			evidence.EndUTF8Byte >
				ref.TranscriptSpan.EndUTF8Byte ||
			evidence.EndUTF8Byte >
				len([]byte(turn.Transcript.Text)) ||
			!utf8.ValidString(excerpt) ||
			evidence.OriginalExcerpt != excerpt {
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
	expected := stableIELTSFindingID(
		prepared.result.SnapshotID,
		criterion,
		kind,
		IELTSSpeakingShadowFinding{
			Message:    finding.Message,
			Suggestion: finding.Suggestion,
			Evidence:   finding.Evidence,
		},
	)
	return expected == finding.ID
}

func validIELTSQuestionFindingIDs(
	ids []string,
	findings []IELTSSpeakingShadowFinding,
	evidenceRefID string,
) bool {
	if ids == nil {
		return false
	}
	expected := ieltsFindingIDsForEvidenceRef(
		findings,
		evidenceRefID,
	)
	return slices.Equal(ids, expected)
}

func mapIELTSBand(
	criterion IELTSCriterion,
	band int,
) (IELTSRubricDescriptor, string, bool) {
	descriptors := ieltsDescriptorsFor(criterion)
	if band < 1 || band > len(descriptors) {
		return "", "", false
	}
	descriptor := descriptors[band-1]
	_, label, ok := mapIELTSRubricDescriptor(
		criterion,
		descriptor,
	)
	return descriptor, label, ok
}
