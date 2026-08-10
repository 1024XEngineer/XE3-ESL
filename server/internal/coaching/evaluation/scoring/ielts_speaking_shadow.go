package scoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

const (
	IELTSSpeakingShadowSchemaVersion         = "ielts-speaking-full-mock-shadow/v1"
	IELTSSpeakingShadowProviderSchemaVersion = "ielts-speaking-full-mock-shadow-provider/v2"
	IELTSSpeakingShadowPromptVersion         = "ielts-speaking-full-mock-shadow-prompt/v5"
	IELTSSpeakingShadowRubricVersion         = "ielts-speaking-public-band-rubric/v2"

	ieltsMaximumProviderPayload = 64 * 1024
	ieltsMaximumFindingText     = 2048
	ieltsMaximumFindings        = 3
	ieltsMaximumAnchors         = 4
	ieltsMaximumOccurrence      = 16
	ieltsMaximumQuestions       = 64
	ieltsMinimumEnglishWords    = 10
	ieltsMinimumEnglishTurns    = 3
	ieltsMinimumAcousticMS      = 3000
)

var ErrInvalidIELTSSpeakingShadow = errors.New(
	"evaluation: invalid IELTS Speaking shadow",
)

var ErrIELTSSpeakingAcousticsPending = errors.New(
	"evaluation: IELTS Speaking acoustics pending",
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
	IELTSReasonPracticeEstimateUncalibrated     IELTSSpeakingReasonCode = "PRACTICE_ESTIMATE_UNCALIBRATED"
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

type IELTSRubricDescriptor struct {
	ID          string `json:"descriptor_id"`
	Band        int    `json:"band"`
	Description string `json:"description"`
}

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
	SceneType          evaluation.SceneType            `json:"scene_type"`
	PracticeMode       string                          `json:"practice_mode"`
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
	TurnID               string   `json:"turn_id"`
	EvidenceRefID        string   `json:"evidence_ref_id"`
	Transcript           string   `json:"confirmed_transcript"`
	RecordingDurationMS  int64    `json:"recording_duration_ms"`
	EnglishWordCount     int      `json:"english_word_count"`
	CJKCharacterCount    int      `json:"cjk_character_count"`
	LanguageEvidence     string   `json:"language_evidence"`
	PronunciationScore   *float64 `json:"pronunciation_score,omitempty"`
	AcousticFluencyScore *float64 `json:"acoustic_fluency_score,omitempty"`
	SpeakingSpeedWPM     *float64 `json:"speaking_speed_wpm,omitempty"`
	AcousticProvider     string   `json:"acoustic_provider,omitempty"`
	AcousticProviderRun  string   `json:"acoustic_provider_run,omitempty"`
}

type IELTSSpeakingAcousticRequest struct {
	TurnID              string
	EvidenceRefID       string
	RecordingDurationMS int64
}

type IELTSSpeakingTurnAcoustics struct {
	TurnID               string
	EvidenceRefID        string
	PronunciationScore   float64
	AcousticFluencyScore *float64
	SpeakingSpeedWPM     *float64
	Provider             string
	ProviderRun          string
}

type IELTSSpeakingAcousticSource interface {
	GetIELTSSpeakingAcoustics(
		context.Context,
		string,
		[]IELTSSpeakingAcousticRequest,
	) ([]IELTSSpeakingTurnAcoustics, error)
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
	SceneType       evaluation.SceneType                 `json:"scene_type"`
	Scope           evaluation.Scope                     `json:"scope"`
	Channel         evaluation.Channel                   `json:"channel"`
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
	provider  IELTSSpeakingShadowProvider
	acoustics IELTSSpeakingAcousticSource
}

func NewIELTSSpeakingShadowEngine(
	provider IELTSSpeakingShadowProvider,
) *IELTSSpeakingShadowEngine {
	return &IELTSSpeakingShadowEngine{provider: provider}
}

func NewIELTSSpeakingShadowEngineWithAcoustics(
	provider IELTSSpeakingShadowProvider,
	acoustics IELTSSpeakingAcousticSource,
) *IELTSSpeakingShadowEngine {
	return &IELTSSpeakingShadowEngine{
		provider:  provider,
		acoustics: acoustics,
	}
}

func (engine *IELTSSpeakingShadowEngine) Evaluate(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
) (IELTSSpeakingShadowResult, error) {
	return engine.evaluate(ctx, snapshot, true)
}

func (engine *IELTSSpeakingShadowEngine) evaluateWithoutAcoustics(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
) (IELTSSpeakingShadowResult, error) {
	return engine.evaluate(ctx, snapshot, false)
}

func (engine *IELTSSpeakingShadowEngine) evaluate(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
	includeAcoustics bool,
) (IELTSSpeakingShadowResult, error) {
	if engine == nil || engine.provider == nil || ctx == nil {
		return IELTSSpeakingShadowResult{}, evaluation.ErrInvalidRequest
	}
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		return IELTSSpeakingShadowResult{}, err
	}
	if prepared.result.Scoreability ==
		IELTSSpeakingScoreabilityInsufficient {
		return prepared.result, nil
	}
	if includeAcoustics && engine.acoustics != nil {
		prepared, err = engine.withAcoustics(ctx, snapshot, prepared)
		if err != nil {
			return IELTSSpeakingShadowResult{}, err
		}
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

func (engine *IELTSSpeakingShadowEngine) withAcoustics(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
	prepared preparedIELTSSpeakingShadow,
) (preparedIELTSSpeakingShadow, error) {
	requests := make(
		[]IELTSSpeakingAcousticRequest,
		0,
		len(prepared.input.Questions),
	)
	for _, question := range prepared.input.Questions {
		if question.Response == nil {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		requests = append(requests, IELTSSpeakingAcousticRequest{
			TurnID:              question.Response.TurnID,
			EvidenceRefID:       question.Response.EvidenceRefID,
			RecordingDurationMS: question.Response.RecordingDurationMS,
		})
	}
	values, err := engine.acoustics.GetIELTSSpeakingAcoustics(
		ctx,
		snapshot.OwnerUserID,
		requests,
	)
	if err != nil {
		return preparedIELTSSpeakingShadow{}, err
	}
	if len(values) == 0 {
		return prepared, nil
	}
	byTurn := make(map[string]IELTSSpeakingTurnAcoustics, len(values))
	requestedTurns := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		requestedTurns[request.TurnID] = struct{}{}
	}
	for _, value := range values {
		if !validIELTSSpeakingTurnAcoustics(value) {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		if _, requested := requestedTurns[value.TurnID]; !requested {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		if _, duplicate := byTurn[value.TurnID]; duplicate {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		byTurn[value.TurnID] = value
	}
	for index := range prepared.input.Questions {
		response := prepared.input.Questions[index].Response
		value, ok := byTurn[response.TurnID]
		if !ok {
			continue
		}
		if response.EnglishWordCount == 0 {
			continue
		}
		if value.EvidenceRefID != response.EvidenceRefID {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		pronunciation := value.PronunciationScore
		response.PronunciationScore = &pronunciation
		if value.AcousticFluencyScore != nil {
			fluency := *value.AcousticFluencyScore
			response.AcousticFluencyScore = &fluency
		}
		if value.SpeakingSpeedWPM != nil {
			speed := *value.SpeakingSpeedWPM
			response.SpeakingSpeedWPM = &speed
		}
		response.AcousticProvider = value.Provider
		response.AcousticProviderRun = value.ProviderRun
		prepared.acousticRefs[response.EvidenceRefID] = struct{}{}
		prepared.acousticMS += response.RecordingDurationMS
	}
	legacyCoverageWithoutDuration := prepared.acousticMS == 0 &&
		len(prepared.acousticRefs) >= ieltsMinimumEnglishTurns
	if (prepared.acousticMS < ieltsMinimumAcousticMS &&
		!legacyCoverageWithoutDuration) || len(prepared.acousticRefs) == 0 {
		for _, question := range prepared.input.Questions {
			response := question.Response
			response.PronunciationScore = nil
			response.AcousticFluencyScore = nil
			response.SpeakingSpeedWPM = nil
			response.AcousticProvider = ""
			response.AcousticProviderRun = ""
		}
		prepared.acousticRefs = make(map[string]struct{})
		prepared.acousticMS = 0
		return prepared, nil
	}
	prepared.input.AssessableCriteria = slices.Clone(ieltsCriterionOrder[:])
	prepared.input.RubricDescriptors = ieltsRubricDescriptorSetsFor(
		prepared.input.AssessableCriteria,
	)
	prepared.result.ReasonCodes = []IELTSSpeakingReasonCode{
		IELTSReasonPracticeEstimateUncalibrated,
	}
	return prepared, nil
}

func validIELTSSpeakingTurnAcoustics(
	value IELTSSpeakingTurnAcoustics,
) bool {
	return validIdentifier(value.TurnID) &&
		validIdentifier(value.EvidenceRefID) &&
		value.PronunciationScore >= 0 &&
		value.PronunciationScore <= 100 &&
		validOptionalIELTSAcousticScore(value.AcousticFluencyScore) &&
		validOptionalIELTSSpeakingSpeed(value.SpeakingSpeedWPM) &&
		(value.AcousticFluencyScore != nil || value.SpeakingSpeedWPM != nil) &&
		validProviderIdentifier(value.Provider) &&
		validProviderIdentifier(value.ProviderRun)
}

func validOptionalIELTSAcousticScore(value *float64) bool {
	return value == nil || (*value >= 0 && *value <= 100)
}

func validOptionalIELTSSpeakingSpeed(value *float64) bool {
	return value == nil || (*value > 0 && *value <= 1000)
}

type preparedIELTSSpeakingShadow struct {
	evidence      evidence.SnapshotPayload
	input         IELTSSpeakingShadowProviderInput
	result        IELTSSpeakingShadowResult
	turnsByID     map[string]evidence.ConfirmedTurn
	refsByID      map[string]evidence.Ref
	refsByTurnID  map[string]evidence.Ref
	responseRefs  map[string]struct{}
	questionParts []IELTSPart
	englishTurns  int
	englishWords  int
	acousticRefs  map[string]struct{}
	acousticMS    int64
}

const (
	ieltsLanguageEnglish = "ENGLISH"
	ieltsLanguageMixed   = "MIXED_ENGLISH_ASSESSABLE"
	ieltsLanguageOther   = "NON_ENGLISH_INSUFFICIENT"
)

func prepareIELTSSpeakingShadow(
	snapshot evidence.EvidenceSnapshot,
) (preparedIELTSSpeakingShadow, error) {
	if !snapshot.Valid() ||
		snapshot.Scope != evaluation.ScopeSession ||
		snapshot.SceneType != evaluation.SceneIELTSSpeaking {
		return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
	}
	questionParts, validAssignment := frozenIELTSFullMockQuestionParts(
		payload.PracticeContext,
	)
	questionCount := len(questionParts)
	if !validAssignment || len(payload.OpportunityManifest) != questionCount {
		return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
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

	responseRefs := make(map[string]struct{}, questionCount)
	questions := make(
		[]IELTSSpeakingProviderQuestion,
		0,
		questionCount,
	)
	answered := 0
	englishTurns := 0
	englishWords := 0
	for index, opportunity := range payload.OpportunityManifest {
		sequence := index + 1
		if opportunity.Sequence != sequence {
			return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
		}
		providerQuestion := IELTSSpeakingProviderQuestion{
			QuestionID:   opportunity.QuestionID,
			PartID:       questionParts[index],
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
					evaluation.ErrInvalidRequest
			}
			answered++
			responseRefs[ref.EvidenceRefID] = struct{}{}
			wordCount, cjkCount := ieltsLanguageEvidence(turn.Transcript.Text)
			language := ieltsLanguageOther
			if wordCount > 0 {
				language = ieltsLanguageEnglish
				if cjkCount > 0 {
					language = ieltsLanguageMixed
				}
				englishTurns++
				englishWords += wordCount
			}
			providerQuestion.Response =
				&IELTSSpeakingProviderResponse{
					TurnID:              turn.TurnID,
					EvidenceRefID:       ref.EvidenceRefID,
					Transcript:          turn.Transcript.Text,
					RecordingDurationMS: turn.Audio.DurationMS,
					EnglishWordCount:    wordCount,
					CJKCharacterCount:   cjkCount,
					LanguageEvidence:    language,
				}
		}
		questions = append(questions, providerQuestion)
	}

	prepared := preparedIELTSSpeakingShadow{
		evidence:      payload,
		result:        ieltsSpeakingResultSkeleton(snapshot),
		turnsByID:     turnsByID,
		refsByID:      refsByID,
		refsByTurnID:  refsByTurnID,
		responseRefs:  responseRefs,
		questionParts: questionParts,
		englishTurns:  englishTurns,
		englishWords:  englishWords,
		acousticRefs:  make(map[string]struct{}),
	}
	if answered != questionCount {
		prepared.result.Scoreability =
			IELTSSpeakingScoreabilityInsufficient
		prepared.result.Gate = IELTSSpeakingGateBlocked
		prepared.result.ReasonCodes = []IELTSSpeakingReasonCode{
			IELTSReasonOpportunityNotProvided,
		}
		prepared.result.Criteria = blockedIELTSCriteria(
			ratio(answered, questionCount),
			IELTSReasonOpportunityNotProvided,
		)
		prepared.result.QuestionResults =
			ieltsSpeakingQuestionResults(
				prepared,
				prepared.result.Criteria,
			)
		return prepared, nil
	}
	if englishWords < ieltsMinimumEnglishWords ||
		englishTurns < ieltsMinimumEnglishTurns {
		prepared.result.Scoreability = IELTSSpeakingScoreabilityInsufficient
		prepared.result.Gate = IELTSSpeakingGateBlocked
		prepared.result.ReasonCodes = []IELTSSpeakingReasonCode{
			IELTSReasonInsufficientEvidence,
		}
		prepared.result.Criteria = blockedIELTSCriteria(
			ratio(englishTurns, questionCount),
			IELTSReasonInsufficientEvidence,
		)
		prepared.result.QuestionResults = ieltsSpeakingQuestionResults(
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
		SceneType:         evaluation.SceneIELTSSpeaking,
		PracticeMode:      payload.PracticeContext.PracticeMode,
		RubricDescriptors: ieltsRubricDescriptorSets(),
		AssessableCriteria: []IELTSCriterion{
			IELTSCriterionFC,
			IELTSCriterionLR,
			IELTSCriterionGRA,
		},
		Questions: questions,
	}
	if !validIELTSSpeakingProviderInput(prepared.input) {
		return preparedIELTSSpeakingShadow{}, evaluation.ErrInvalidRequest
	}
	return prepared, nil
}

func ieltsLanguageEvidence(text string) (englishWords int, cjkCharacters int) {
	inEnglishWord := false
	for _, character := range text {
		isEnglishLetter := unicode.In(character, unicode.Latin) &&
			unicode.IsLetter(character)
		if isEnglishLetter {
			if !inEnglishWord {
				englishWords++
			}
			inEnglishWord = true
		} else {
			inEnglishWord = false
		}
		if unicode.In(character, unicode.Han) {
			cjkCharacters++
		}
	}
	return englishWords, cjkCharacters
}

func frozenIELTSFullMockQuestionParts(
	context evidence.PracticeContext,
) ([]IELTSPart, bool) {
	assignment := context.IELTSAssignment
	if context.EvaluationPolicyRef !=
		IELTSSpeakingFullMockEvaluationPolicyRef ||
		context.PracticeMode != "FULL_MOCK" || assignment == nil ||
		assignment.Mode != "FULL_MOCK" ||
		!validIdentifier(assignment.BankID) ||
		strings.TrimSpace(assignment.Season) == "" ||
		strings.TrimSpace(assignment.Season) != assignment.Season ||
		len(assignment.Parts) != 3 {
		return nil, false
	}
	expectedParts := [...]IELTSPart{IELTSPart1, IELTSPart2, IELTSPart3}
	questionParts := make([]IELTSPart, 0, len(context.TaskBlueprints))
	blueprints := make([]string, 0, len(context.TaskBlueprints))
	for index, part := range assignment.Parts {
		partID := IELTSPart(part.Part)
		if partID != expectedParts[index] || !validIdentifier(part.SourceID) ||
			len(part.TurnBlueprints) == 0 {
			return nil, false
		}
		for _, blueprint := range part.TurnBlueprints {
			if strings.TrimSpace(blueprint) == "" ||
				strings.TrimSpace(blueprint) != blueprint {
				return nil, false
			}
			questionParts = append(questionParts, partID)
			blueprints = append(blueprints, blueprint)
		}
		switch partID {
		case IELTSPart1:
			if part.TopicTitle != "" || part.CueCard != "" {
				return nil, false
			}
		case IELTSPart2:
			if strings.TrimSpace(part.TopicTitle) == "" ||
				strings.TrimSpace(part.TopicTitle) != part.TopicTitle ||
				strings.TrimSpace(part.CueCard) == "" ||
				strings.TrimSpace(part.CueCard) != part.CueCard ||
				len(part.TurnBlueprints) != 1 {
				return nil, false
			}
		case IELTSPart3:
			if strings.TrimSpace(part.TopicTitle) == "" ||
				strings.TrimSpace(part.TopicTitle) != part.TopicTitle ||
				part.CueCard != "" {
				return nil, false
			}
		}
	}
	if assignment.Parts[1].SourceID != assignment.Parts[2].SourceID ||
		assignment.Parts[1].TopicTitle != assignment.Parts[2].TopicTitle ||
		len(questionParts) == 0 || len(questionParts) > ieltsMaximumQuestions ||
		!slices.Equal(blueprints, context.TaskBlueprints) {
		return nil, false
	}
	return questionParts, true
}

func ieltsSpeakingResultSkeleton(
	snapshot evidence.EvidenceSnapshot,
) IELTSSpeakingShadowResult {
	return IELTSSpeakingShadowResult{
		SchemaVersion:   IELTSSpeakingShadowSchemaVersion,
		SnapshotID:      snapshot.ID,
		SceneType:       evaluation.SceneIELTSSpeaking,
		Scope:           evaluation.ScopeSession,
		Channel:         evaluation.ChannelScene,
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
			if reason != IELTSReasonOpportunityNotProvided &&
				reason != IELTSReasonInsufficientEvidence {
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
	RubricDescriptor string                 `json:"rubric_descriptor,omitempty"`
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
		IELTSCriterionPR:  "pronunciation",
	}[criterion]
	if criterionName == "" {
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
		return IELTSSpeakingShadowResult{}, fmt.Errorf(
			"provider envelope: %w",
			ErrInvalidIELTSSpeakingShadow,
		)
	}
	var payload ieltsProviderPayload
	decoder := json.NewDecoder(bytes.NewReader(generated.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil ||
		ensureJSONEOF(decoder) != nil ||
		payload.SchemaVersion !=
			IELTSSpeakingShadowProviderSchemaVersion ||
		len(payload.Criteria) != len(prepared.input.AssessableCriteria) {
		return IELTSSpeakingShadowResult{}, fmt.Errorf(
			"provider JSON shape: %w",
			ErrInvalidIELTSSpeakingShadow,
		)
	}
	byCriterion := make(
		map[IELTSCriterion]ieltsProviderCriterion,
		len(payload.Criteria),
	)
	for index, criterion := range payload.Criteria {
		expected := prepared.input.AssessableCriteria[index]
		if criterion.CriterionID != expected {
			return IELTSSpeakingShadowResult{}, fmt.Errorf(
				"provider criterion order: %w",
				ErrInvalidIELTSSpeakingShadow,
			)
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
	for _, criterionID := range prepared.input.AssessableCriteria {
		criterion, err := normalizeIELTSProviderCriterion(
			prepared,
			byCriterion[criterionID],
		)
		if err != nil {
			return IELTSSpeakingShadowResult{}, fmt.Errorf(
				"provider criterion %s: %w",
				criterionID,
				err,
			)
		}
		result.Criteria = append(result.Criteria, criterion)
	}
	if !slices.Contains(prepared.input.AssessableCriteria, IELTSCriterionPR) {
		result.Criteria = append(
			result.Criteria,
			blockedIELTSCriterion(
				IELTSCriterionPR,
				1,
				IELTSReasonPronunciationArtifactUnavailable,
			),
		)
	}
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
	if source.Strengths == nil ||
		source.Improvements == nil ||
		source.UpgradeExamples == nil ||
		len(source.Strengths) > ieltsMaximumFindings ||
		len(source.Improvements) > ieltsMaximumFindings ||
		len(source.UpgradeExamples) > ieltsMaximumFindings ||
		len(source.Strengths)+len(source.Improvements) == 0 {
		return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
			"finding collections: %w",
			ErrInvalidIELTSSpeakingShadow,
		)
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
	fullAcoustics := slices.Contains(
		prepared.input.AssessableCriteria,
		IELTSCriterionPR,
	)
	if source.CriterionID == IELTSCriterionFC && !fullAcoustics {
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
			return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
				"rubric descriptor: %w",
				ErrInvalidIELTSSpeakingShadow,
			)
		}
		result.EstimatedBand = &band
		result.BandDescriptor = descriptor
		if fullAcoustics &&
			(source.CriterionID == IELTSCriterionFC ||
				source.CriterionID == IELTSCriterionPR) {
			result.ReasonCodes = []IELTSSpeakingReasonCode{
				IELTSReasonPracticeEstimateUncalibrated,
			}
		}
	}
	var err error
	result.Strengths, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingStrength,
		source.Strengths,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
			"strengths: %w",
			err,
		)
	}
	result.Improvements, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingImprovement,
		source.Improvements,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
			"improvements: %w",
			err,
		)
	}
	result.UpgradeExamples, err = normalizeIELTSFindings(
		prepared,
		source.CriterionID,
		ieltsFindingUpgrade,
		source.UpgradeExamples,
	)
	if err != nil {
		return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
			"upgrade examples: %w",
			err,
		)
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
		return IELTSSpeakingShadowCriterionResult{}, fmt.Errorf(
			"missing evidence: %w",
			ErrInvalidIELTSSpeakingShadow,
		)
	}
	result.Coverage = ratio(
		len(result.EvidenceRefIDs),
		len(prepared.questionParts),
	)
	if source.CriterionID == IELTSCriterionPR && fullAcoustics {
		result.Coverage = ratio(
			len(prepared.acousticRefs),
			len(prepared.questionParts),
		)
	}
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
			if criterion == IELTSCriterionPR &&
				len(prepared.acousticRefs) > 0 {
				if _, acousticallyAssessed := prepared.acousticRefs[resolved.EvidenceRefID]; !acousticallyAssessed {
					return nil, ErrInvalidIELTSSpeakingShadow
				}
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
	resolved, err := resolveStrictIELTSProviderAnchor(prepared, anchor)
	if err == nil {
		return resolved, nil
	}
	// Providers occasionally copy an exact quote but pair it with the adjacent
	// response's evidence_ref_id (or an occurrence number that belongs to that
	// adjacent response). The quote is still trustworthy when it has exactly
	// one eligible location in the frozen snapshot. Canonicalize that unique
	// location instead of discarding the entire report. Ambiguous or inexact
	// quotes remain invalid and never become evidence.
	return resolveUniqueIELTSProviderQuote(prepared, anchor.Quote)
}

func resolveStrictIELTSProviderAnchor(
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

func resolveUniqueIELTSProviderQuote(
	prepared preparedIELTSSpeakingShadow,
	quote string,
) (IELTSSpeakingShadowEvidence, error) {
	if !validInterviewText(quote, ieltsMaximumFindingText) {
		return IELTSSpeakingShadowEvidence{},
			ErrInvalidIELTSSpeakingShadow
	}
	var unique IELTSSpeakingShadowEvidence
	matches := 0
	for _, question := range prepared.input.Questions {
		response := question.Response
		if response == nil {
			continue
		}
		ref, exists := prepared.refsByID[response.EvidenceRefID]
		turn, turnExists := prepared.turnsByID[response.TurnID]
		_, allowed := prepared.responseRefs[response.EvidenceRefID]
		if !exists || !turnExists || !allowed {
			continue
		}
		searchStart := ref.TranscriptSpan.StartUTF8Byte
		searchEnd := ref.TranscriptSpan.EndUTF8Byte
		for searchStart < searchEnd {
			relative := strings.Index(
				turn.Transcript.Text[searchStart:searchEnd],
				quote,
			)
			if relative < 0 {
				break
			}
			start := searchStart + relative
			end := start + len(quote)
			if end > searchEnd ||
				!utf8.ValidString(turn.Transcript.Text[start:end]) {
				break
			}
			matches++
			if matches > 1 {
				return IELTSSpeakingShadowEvidence{},
					ErrInvalidIELTSSpeakingShadow
			}
			unique = IELTSSpeakingShadowEvidence{
				EvidenceRefID:   ref.EvidenceRefID,
				TurnID:          ref.TurnID,
				StartUTF8Byte:   start,
				EndUTF8Byte:     end,
				OriginalExcerpt: turn.Transcript.Text[start:end],
			}
			searchStart = end
		}
	}
	if matches != 1 {
		return IELTSSpeakingShadowEvidence{},
			ErrInvalidIELTSSpeakingShadow
	}
	return unique, nil
}

func ieltsSpeakingQuestionResults(
	prepared preparedIELTSSpeakingShadow,
	criteria []IELTSSpeakingShadowCriterionResult,
) []IELTSSpeakingShadowQuestionResult {
	results := make(
		[]IELTSSpeakingShadowQuestionResult,
		0,
		len(prepared.questionParts),
	)
	for index, opportunity := range prepared.evidence.OpportunityManifest {
		question := IELTSSpeakingShadowQuestionResult{
			QuestionID:        opportunity.QuestionID,
			PartID:            prepared.questionParts[index],
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
		var responseRef evidence.Ref
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
	return ieltsRubricDescriptorSetsFor([]IELTSCriterion{
		IELTSCriterionLR,
		IELTSCriterionGRA,
	})
}

func ieltsRubricDescriptorSetsFor(
	criteria []IELTSCriterion,
) []IELTSRubricDescriptorSet {
	result := make([]IELTSRubricDescriptorSet, 0, len(criteria))
	for _, criterion := range criteria {
		result = append(result, IELTSRubricDescriptorSet{
			CriterionID: criterion,
			Descriptors: ieltsDescriptorsFor(criterion),
		})
	}
	return result
}

func ieltsDescriptorsFor(
	criterion IELTSCriterion,
) []IELTSRubricDescriptor {
	if !slices.Contains(ieltsCriterionOrder[:], criterion) {
		return []IELTSRubricDescriptor{}
	}
	prefix := strings.TrimPrefix(string(criterion), "IELTS_")
	descriptions := ieltsPublicBandDescriptions(criterion)
	result := make([]IELTSRubricDescriptor, 0, 9)
	for band := 1; band <= 9; band++ {
		result = append(
			result,
			IELTSRubricDescriptor{
				ID:          prefix + "_PRACTICE_BAND_" + strconv.Itoa(band),
				Band:        band,
				Description: descriptions[band-1],
			},
		)
	}
	return result
}

func ieltsPublicBandDescriptions(criterion IELTSCriterion) []string {
	switch criterion {
	case IELTSCriterionFC:
		return []string{
			"No rateable language is available.",
			"Communication is extremely limited and pauses occur before most words.",
			"Long pauses are common, simple sentences are difficult to link, and responses often fail to convey a basic message.",
			"Noticeable pauses, slow delivery, frequent repetition, and basic repetitive links cause coherence breakdowns.",
			"Usually maintains speech flow using repetition, self-correction, or slow speech; complex communication causes fluency problems.",
			"Can speak at length but sometimes loses coherence through hesitation, repetition, or self-correction and does not always use discourse markers appropriately.",
			"Speaks at length without noticeable effort, uses connectives flexibly, and remains coherent despite occasional language-related hesitation or correction.",
			"Speaks fluently with only occasional repetition or correction, with hesitation mainly related to content, and develops topics coherently.",
			"Speaks fluently and coherently with only rare repetition or correction and develops topics fully and appropriately.",
		}
	case IELTSCriterionLR:
		return []string{
			"No rateable lexical resource is available.",
			"Produces only isolated words or memorised utterances.",
			"Uses simple vocabulary for personal information but has insufficient vocabulary for less familiar topics.",
			"Conveys basic meaning on familiar topics, makes frequent word-choice errors, and rarely attempts paraphrase.",
			"Discusses familiar and unfamiliar topics with limited flexibility and attempts paraphrase with mixed success.",
			"Has enough vocabulary to discuss topics at length despite some inappropriate choices and generally paraphrases successfully.",
			"Uses vocabulary flexibly across topics, includes some less common or idiomatic language, and paraphrases effectively despite occasional inappropriate choices.",
			"Uses a wide vocabulary readily and flexibly, handles less common and idiomatic language skilfully, and paraphrases effectively with only occasional inaccuracies.",
			"Uses vocabulary with full flexibility and precision across topics and uses idiomatic language naturally and accurately.",
		}
	case IELTSCriterionGRA:
		return []string{
			"No rateable grammatical language is available.",
			"Cannot produce basic sentence forms.",
			"Attempts basic sentence forms with limited success or relies on memorised utterances, with numerous errors outside memorised expressions.",
			"Produces basic sentence forms and some correct simple sentences, but subordinate structures are rare and frequent errors may cause misunderstanding.",
			"Produces basic sentence forms with reasonable accuracy and attempts a limited range of complex structures that often contain errors.",
			"Uses a mix of simple and complex structures with limited flexibility; frequent errors in complex structures rarely impede understanding.",
			"Uses a range of complex structures with some flexibility and frequently produces error-free sentences, though some mistakes persist.",
			"Uses a wide range of structures flexibly and produces a majority of error-free sentences with only occasional non-systematic errors.",
			"Uses a full range of structures naturally and appropriately with consistently accurate grammar apart from native-like slips.",
		}
	case IELTSCriterionPR:
		return []string{
			"No rateable pronunciation is available.",
			"Speech is often unintelligible.",
			"Shows some features of Band 2 and some positive features of Band 4.",
			"Uses a limited range of pronunciation features; frequent lapses and mispronunciations cause listener difficulty.",
			"Shows all positive features of Band 4 and some positive features of Band 6.",
			"Uses a range of pronunciation features with mixed control and is generally understandable, though individual sounds or words sometimes reduce clarity.",
			"Shows all positive features of Band 6 and some positive features of Band 8.",
			"Uses a wide range of pronunciation features flexibly, is easy to understand throughout, and the first-language accent has minimal effect.",
			"Uses a full range of pronunciation features precisely and subtly and is effortless to understand.",
		}
	default:
		return []string{}
	}
}

func mapIELTSRubricDescriptor(
	criterion IELTSCriterion,
	descriptor string,
) (int, string, bool) {
	allowed := ieltsDescriptorsFor(criterion)
	for _, candidate := range allowed {
		if candidate.ID == descriptor {
			return candidate.Band, candidate.Description, true
		}
	}
	return 0, "", false
}

func validIELTSSpeakingProviderInput(
	input IELTSSpeakingShadowProviderInput,
) bool {
	if input.SchemaVersion !=
		IELTSSpeakingShadowProviderSchemaVersion ||
		input.PromptVersion != IELTSSpeakingShadowPromptVersion ||
		input.RubricVersion != IELTSSpeakingShadowRubricVersion ||
		input.SceneType != evaluation.SceneIELTSSpeaking ||
		input.PracticeMode != "FULL_MOCK" ||
		len(input.Questions) == 0 ||
		len(input.Questions) > ieltsMaximumQuestions ||
		!validIELTSFullMockQuestionSequence(input.Questions) {
		return false
	}
	fullAcoustics := slices.Equal(
		input.AssessableCriteria,
		ieltsCriterionOrder[:],
	)
	textOnly := slices.Equal(input.AssessableCriteria, []IELTSCriterion{
		IELTSCriterionFC,
		IELTSCriterionLR,
		IELTSCriterionGRA,
	})
	if !fullAcoustics && !textOnly {
		return false
	}
	expectedRubricCriteria := input.AssessableCriteria
	if textOnly {
		expectedRubricCriteria = []IELTSCriterion{
			IELTSCriterionLR,
			IELTSCriterionGRA,
		}
	}
	if len(input.RubricDescriptors) != len(expectedRubricCriteria) {
		return false
	}
	for index, set := range input.RubricDescriptors {
		expected := expectedRubricCriteria[index]
		if set.CriterionID != expected ||
			!slices.Equal(
				set.Descriptors,
				ieltsDescriptorsFor(expected),
			) {
			return false
		}
	}
	seenQuestions := make(map[string]struct{}, len(input.Questions))
	seenTurns := make(map[string]struct{}, len(input.Questions))
	seenRefs := make(map[string]struct{}, len(input.Questions))
	acousticResponses := 0
	for index, question := range input.Questions {
		expected := index + 1
		if question.Index != expected ||
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
			) || question.Response.RecordingDurationMS < 0 ||
			question.Response.EnglishWordCount < 0 ||
			question.Response.CJKCharacterCount < 0 {
			return false
		}
		words, cjk := ieltsLanguageEvidence(question.Response.Transcript)
		expectedLanguage := ieltsLanguageOther
		if words > 0 {
			expectedLanguage = ieltsLanguageEnglish
			if cjk > 0 {
				expectedLanguage = ieltsLanguageMixed
			}
		}
		if question.Response.EnglishWordCount != words ||
			question.Response.CJKCharacterCount != cjk ||
			question.Response.LanguageEvidence != expectedLanguage {
			return false
		}
		hasAcoustics := question.Response.PronunciationScore != nil ||
			question.Response.AcousticFluencyScore != nil ||
			question.Response.SpeakingSpeedWPM != nil ||
			question.Response.AcousticProvider != "" ||
			question.Response.AcousticProviderRun != ""
		if fullAcoustics {
			if !hasAcoustics {
				continue
			}
			if question.Response.PronunciationScore == nil ||
				(question.Response.AcousticFluencyScore == nil &&
					question.Response.SpeakingSpeedWPM == nil) ||
				!validIELTSSpeakingTurnAcoustics(
					IELTSSpeakingTurnAcoustics{
						TurnID:               question.Response.TurnID,
						EvidenceRefID:        question.Response.EvidenceRefID,
						PronunciationScore:   *question.Response.PronunciationScore,
						AcousticFluencyScore: question.Response.AcousticFluencyScore,
						SpeakingSpeedWPM:     question.Response.SpeakingSpeedWPM,
						Provider:             question.Response.AcousticProvider,
						ProviderRun:          question.Response.AcousticProviderRun,
					},
				) {
				return false
			}
			acousticResponses++
		} else if hasAcoustics {
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
	return !fullAcoustics || acousticResponses > 0
}

func validIELTSFullMockQuestionSequence(
	questions []IELTSSpeakingProviderQuestion,
) bool {
	if len(questions) == 0 || questions[0].PartID != IELTSPart1 {
		return false
	}
	partIndex := 0
	for _, question := range questions[1:] {
		if question.PartID == ieltsPartOrder[partIndex] {
			continue
		}
		if partIndex+1 >= len(ieltsPartOrder) ||
			question.PartID != ieltsPartOrder[partIndex+1] {
			return false
		}
		partIndex++
	}
	return partIndex == len(ieltsPartOrder)-1
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
	snapshot evidence.EvidenceSnapshot,
	result IELTSSpeakingShadowResult,
) error {
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		return err
	}
	if result.SchemaVersion != IELTSSpeakingShadowSchemaVersion ||
		result.SnapshotID != snapshot.ID ||
		result.SceneType != evaluation.SceneIELTSSpeaking ||
		result.Scope != evaluation.ScopeSession ||
		result.Channel != evaluation.ChannelScene ||
		result.Scoreability != prepared.result.Scoreability ||
		result.Gate != prepared.result.Gate ||
		len(result.Criteria) != len(ieltsCriterionOrder) ||
		len(result.QuestionResults) != len(prepared.questionParts) {
		return ErrInvalidIELTSSpeakingShadow
	}
	switch result.Scoreability {
	case IELTSSpeakingScoreabilityProvisional:
		fullAcoustics := len(result.Criteria) == len(ieltsCriterionOrder) &&
			result.Criteria[len(result.Criteria)-1].CriterionID == IELTSCriterionPR &&
			result.Criteria[len(result.Criteria)-1].Scoreability ==
				IELTSSpeakingScoreabilityProvisional
		expectedReasons := []IELTSSpeakingReasonCode{
			IELTSReasonASRConfidenceUnavailable,
			IELTSReasonFluencyTimingUnavailable,
			IELTSReasonPronunciationArtifactUnavailable,
		}
		if fullAcoustics {
			expectedReasons = []IELTSSpeakingReasonCode{
				IELTSReasonPracticeEstimateUncalibrated,
			}
		}
		if result.Gate != IELTSSpeakingGateFeedbackOnly ||
			!slices.Equal(result.ReasonCodes, expectedReasons) ||
			result.Provider == nil ||
			!validIELTSProviderLineage(*result.Provider) {
			return ErrInvalidIELTSSpeakingShadow
		}
	case IELTSSpeakingScoreabilityInsufficient:
		if result.Gate != IELTSSpeakingGateBlocked ||
			(len(result.ReasonCodes) != 1 ||
				(result.ReasonCodes[0] != IELTSReasonOpportunityNotProvided &&
					result.ReasonCodes[0] != IELTSReasonInsufficientEvidence)) ||
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
	fullAcoustics := result.Scoreability ==
		IELTSSpeakingScoreabilityProvisional &&
		len(result.Criteria) == len(ieltsCriterionOrder) &&
		result.Criteria[len(result.Criteria)-1].CriterionID == IELTSCriterionPR &&
		result.Criteria[len(result.Criteria)-1].Scoreability ==
			IELTSSpeakingScoreabilityProvisional
	questionCount := len(prepared.questionParts)
	for index, criterion := range result.Criteria {
		expectedScoreability := result.Scoreability
		if criterion.CriterionID == IELTSCriterionPR && !fullAcoustics {
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
				result.Scoreability !=
					IELTSSpeakingScoreabilityProvisional {
				return ErrInvalidIELTSSpeakingShadow
			}
			expectedCoverage := ratio(
				len(criterion.EvidenceRefIDs),
				questionCount,
			)
			if criterion.CriterionID == IELTSCriterionPR && fullAcoustics {
				if criterion.Coverage <= 0 || criterion.Coverage > 1 ||
					!sameRatio(
						criterion.Coverage*float64(questionCount),
						math.Round(criterion.Coverage*float64(questionCount)),
					) {
					return ErrInvalidIELTSSpeakingShadow
				}
			} else if !sameRatio(criterion.Coverage, expectedCoverage) {
				return ErrInvalidIELTSSpeakingShadow
			}
			switch criterion.CriterionID {
			case IELTSCriterionFC:
				if fullAcoustics {
					if !validIELTSProvisionalBandCriterion(
						criterion,
						[]IELTSSpeakingReasonCode{
							IELTSReasonPracticeEstimateUncalibrated,
						},
					) {
						return ErrInvalidIELTSSpeakingShadow
					}
				} else if criterion.EstimatedBand != nil ||
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
				if !validIELTSProvisionalBandCriterion(
					criterion,
					[]IELTSSpeakingReasonCode{
						IELTSReasonASRConfidenceUnavailable,
					},
				) {
					return ErrInvalidIELTSSpeakingShadow
				}
			case IELTSCriterionPR:
				if !fullAcoustics ||
					!validIELTSProvisionalBandCriterion(
						criterion,
						[]IELTSSpeakingReasonCode{
							IELTSReasonPracticeEstimateUncalibrated,
						},
					) {
					return ErrInvalidIELTSSpeakingShadow
				}
			default:
				return ErrInvalidIELTSSpeakingShadow
			}
		case IELTSSpeakingScoreabilityInsufficient:
			expectedBlockedCoverage := ratio(answered, questionCount)
			if len(result.ReasonCodes) == 1 &&
				result.ReasonCodes[0] == IELTSReasonInsufficientEvidence {
				expectedBlockedCoverage = ratio(
					prepared.englishTurns,
					questionCount,
				)
			}
			if criterion.Gate != IELTSSpeakingGateBlocked ||
				criterion.EstimatedBand != nil ||
				criterion.BandDescriptor != "" ||
				len(criterion.EvidenceRefIDs) != 0 ||
				len(criterion.Strengths) != 0 ||
				len(criterion.Improvements) != 0 ||
				len(criterion.UpgradeExamples) != 0 ||
				!sameRatio(criterion.Coverage, expectedBlockedCoverage) ||
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
			question.PartID != prepared.questionParts[index] ||
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

func validIELTSProvisionalBandCriterion(
	criterion IELTSSpeakingShadowCriterionResult,
	reasons []IELTSSpeakingReasonCode,
) bool {
	if criterion.EstimatedBand == nil ||
		!slices.Equal(criterion.ReasonCodes, reasons) {
		return false
	}
	_, descriptor, ok := MapIELTSBand(
		criterion.CriterionID,
		*criterion.EstimatedBand,
	)
	return ok && criterion.BandDescriptor == descriptor
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

func MapIELTSBand(
	criterion IELTSCriterion,
	band int,
) (string, string, bool) {
	descriptors := ieltsDescriptorsFor(criterion)
	if band < 1 || band > len(descriptors) {
		return "", "", false
	}
	descriptor := descriptors[band-1]
	_, label, ok := mapIELTSRubricDescriptor(
		criterion,
		descriptor.ID,
	)
	return descriptor.ID, label, ok
}
