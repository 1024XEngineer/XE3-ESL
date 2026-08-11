package scoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	GeneralSceneStrategyRef           = "general-scene-evaluation/v1"
	GeneralScenePipelineVersion       = "evaluation-pipeline/v1"
	GeneralSceneSchemaVersion         = "general-scene-evaluation/v1"
	GeneralSceneProviderSchemaVersion = "general-scene-evaluation-provider/v1"
	GeneralScenePromptVersion         = "general-scene-evaluation-prompt/v1"

	generalSceneMinimumWords            = 3
	generalSceneMaximumPayload          = 64 * 1024
	generalSceneMaximumTextBytes        = 2048
	generalSceneMaximumProviderFindings = 3
	// Twelve provider anchors split across three IELTS Parts need at most five
	// four-anchor findings after section-preserving normalization.
	generalSceneMaximumNormalizedFindings = 5
	generalSceneMaximumAnchors            = 4
	generalSceneMaximumOccurrence         = 16
)

var ErrInvalidGeneralSceneResult = errors.New(
	"evaluation: invalid general Scene result",
)

var errGeneralSceneProviderInvalidJSON = fmt.Errorf(
	"evaluation: general Scene provider returned invalid JSON: %w",
	ErrInvalidGeneralSceneResult,
)

var errGeneralSceneProviderSchemaMismatch = fmt.Errorf(
	"evaluation: general Scene provider response schema mismatch: %w",
	ErrInvalidGeneralSceneResult,
)

type GeneralSceneDimension string

const (
	GeneralSceneDimensionTaskAchievement GeneralSceneDimension = "TASK_ACHIEVEMENT"
	GeneralSceneDimensionClarity         GeneralSceneDimension = "CLARITY_COHERENCE"
	GeneralSceneDimensionLanguage        GeneralSceneDimension = "LANGUAGE_CONTROL"
	GeneralSceneDimensionInteraction     GeneralSceneDimension = "INTERACTION"
)

type GeneralSceneScoreability string

const (
	GeneralSceneScoreabilityProvisional  GeneralSceneScoreability = "PROVISIONAL"
	GeneralSceneScoreabilityInsufficient GeneralSceneScoreability = "INSUFFICIENT"
)

type GeneralSceneScoreScale string

const GeneralSceneScalePercentage100 GeneralSceneScoreScale = "PERCENTAGE_100"

type GeneralSceneDimensionResult struct {
	Key          string                 `json:"key"`
	Score        *float64               `json:"score,omitempty"`
	Scale        GeneralSceneScoreScale `json:"scale"`
	Coverage     float64                `json:"coverage"`
	Confidence   float64                `json:"confidence"`
	ReasonCodes  []string               `json:"reason_codes"`
	EvidenceRefs []string               `json:"evidence_ref_ids"`
	Strengths    []GeneralSceneFinding  `json:"strengths"`
	Improvements []GeneralSceneFinding  `json:"improvements"`
	Examples     []GeneralSceneFinding  `json:"recommended_examples"`
}

type GeneralSceneFinding struct {
	ID         string                 `json:"finding_id"`
	Message    string                 `json:"message"`
	Suggestion string                 `json:"suggestion,omitempty"`
	Evidence   []GeneralSceneEvidence `json:"evidence"`
}

type GeneralSceneEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	TurnID          string `json:"turn_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

type GeneralScenePriorityAction struct {
	DimensionKey string `json:"dimension_key"`
	FindingID    string `json:"finding_id"`
}

var generalSceneDimensionOrder = [...]GeneralSceneDimension{
	GeneralSceneDimensionTaskAchievement,
	GeneralSceneDimensionClarity,
	GeneralSceneDimensionLanguage,
	GeneralSceneDimensionInteraction,
}

func GeneralSceneDimensions() []GeneralSceneDimension {
	return slices.Clone(generalSceneDimensionOrder[:])
}

type GeneralSceneProvider interface {
	AnalyzeGeneralScene(
		context.Context,
		GeneralSceneProviderInput,
	) (GeneralSceneProviderResult, error)
}

type GeneralSceneProviderResult struct {
	Payload   json.RawMessage
	Provider  string
	Model     string
	RequestID string
}

type GeneralSceneProviderInput struct {
	SchemaVersion        string                    `json:"schema_version"`
	PromptVersion        string                    `json:"prompt_version"`
	SceneType            evaluation.SceneType      `json:"scene_type"`
	PracticeExperience   string                    `json:"practice_experience"`
	SceneCategory        string                    `json:"scene_category"`
	PracticeMode         string                    `json:"practice_mode"`
	PracticeGoal         string                    `json:"practice_goal"`
	PublicSceneBrief     string                    `json:"public_scene_brief"`
	FocusAreas           []string                  `json:"focus_areas"`
	PracticeObjectives   []GeneralSceneObjective   `json:"practice_objectives"`
	AssessableDimensions []GeneralSceneDimension   `json:"assessable_dimensions"`
	Opportunities        []GeneralSceneOpportunity `json:"opportunities"`
}

type GeneralSceneObjective struct {
	ID          string `json:"objective_id"`
	Description string `json:"description"`
}

type GeneralSceneOpportunity struct {
	QuestionID   string                `json:"question_id"`
	ObjectiveID  string                `json:"objective_id"`
	QuestionType string                `json:"question_type"`
	QuestionText string                `json:"question_text"`
	Response     *GeneralSceneResponse `json:"response,omitempty"`
}

type GeneralSceneResponse struct {
	TurnID        string `json:"turn_id"`
	EvidenceRefID string `json:"evidence_ref_id"`
	Transcript    string `json:"confirmed_transcript"`
}

type GeneralSceneResult struct {
	SchemaVersion      string                        `json:"schema_version"`
	SnapshotID         string                        `json:"snapshot_id"`
	SceneType          evaluation.SceneType          `json:"scene_type"`
	PracticeExperience string                        `json:"practice_experience"`
	SceneCategory      string                        `json:"scene_category"`
	PracticeMode       string                        `json:"practice_mode"`
	Scope              evaluation.Scope              `json:"scope"`
	Channel            evaluation.Channel            `json:"channel"`
	ScoreabilityStatus GeneralSceneScoreability      `json:"scoreability_status"`
	Dimensions         []GeneralSceneDimensionResult `json:"dimensions"`
	PriorityActions    []GeneralScenePriorityAction  `json:"priority_actions"`
	Provider           *GeneralSceneProviderLineage  `json:"provider_lineage,omitempty"`
}

type GeneralSceneProviderLineage struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	RequestID      string `json:"request_id"`
	PromptVersion  string `json:"prompt_version"`
	ResponseSchema string `json:"response_schema"`
}

type GeneralSceneEngine struct {
	provider GeneralSceneProvider
}

func NewGeneralSceneEngine(provider GeneralSceneProvider) *GeneralSceneEngine {
	return &GeneralSceneEngine{provider: provider}
}

func (engine *GeneralSceneEngine) Evaluate(
	ctx context.Context,
	snapshot evidence.EvidenceSnapshot,
) (GeneralSceneResult, error) {
	if engine == nil || engine.provider == nil || ctx == nil {
		return GeneralSceneResult{}, evaluation.ErrInvalidRequest
	}
	prepared, err := prepareGeneralScene(snapshot)
	if err != nil {
		return GeneralSceneResult{}, err
	}
	if prepared.result.ScoreabilityStatus ==
		GeneralSceneScoreabilityInsufficient {
		return prepared.result, nil
	}
	generated, err := engine.provider.AnalyzeGeneralScene(ctx, prepared.input)
	if err != nil {
		return GeneralSceneResult{}, err
	}
	result, err := normalizeGeneralSceneProviderResult(prepared, generated)
	if err != nil {
		return GeneralSceneResult{}, err
	}
	if err := ValidateGeneralSceneResult(snapshot, result); err != nil {
		return GeneralSceneResult{}, err
	}
	return result, nil
}

type preparedGeneralScene struct {
	input                GeneralSceneProviderInput
	result               GeneralSceneResult
	turnsByID            map[string]evidence.ConfirmedTurn
	refsByID             map[string]evidence.Ref
	allowedRefs          map[string]struct{}
	findingSectionsByRef map[string]IELTSPart
	coverage             float64
	confidence           float64
}

func prepareGeneralScene(
	snapshot evidence.EvidenceSnapshot,
) (preparedGeneralScene, error) {
	if !snapshot.Valid() || snapshot.Scope != evaluation.ScopeSession ||
		!generalSceneTypeSupported(snapshot.SceneType) {
		return preparedGeneralScene{}, evaluation.ErrInvalidRequest
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return preparedGeneralScene{}, evaluation.ErrInvalidRequest
	}
	turnsByID := make(map[string]evidence.ConfirmedTurn, len(payload.ConfirmedTurns))
	refsByID := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	refsByTurnID := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	for _, turn := range payload.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}
	for _, ref := range payload.EvidenceRefs {
		refsByID[ref.EvidenceRefID] = ref
		refsByTurnID[ref.TurnID] = ref
	}
	if len(payload.OpportunityManifest) == 0 {
		return preparedGeneralScene{}, evaluation.ErrInvalidRequest
	}
	findingSections, err := generalSceneFindingSections(
		snapshot.SceneType,
		payload.PracticeContext,
		len(payload.OpportunityManifest),
	)
	if err != nil {
		return preparedGeneralScene{}, err
	}
	allowedRefs := make(map[string]struct{}, len(payload.EvidenceRefs))
	findingSectionsByRef := make(map[string]IELTSPart, len(payload.EvidenceRefs))
	opportunities := make(
		[]GeneralSceneOpportunity,
		0,
		len(payload.OpportunityManifest),
	)
	wordCount := 0
	answered := 0
	for index, opportunity := range payload.OpportunityManifest {
		providerOpportunity := GeneralSceneOpportunity{
			QuestionID:   opportunity.QuestionID,
			ObjectiveID:  opportunity.ObjectiveID,
			QuestionType: opportunity.QuestionType,
			QuestionText: opportunity.QuestionText,
		}
		if opportunity.ResponseTurnID != "" {
			turn, turnExists := turnsByID[opportunity.ResponseTurnID]
			ref, refExists := refsByTurnID[opportunity.ResponseTurnID]
			if !turnExists || !refExists {
				return preparedGeneralScene{}, evaluation.ErrInvalidRequest
			}
			answered++
			wordCount += generalSceneWordCount(turn.Transcript.Text)
			allowedRefs[ref.EvidenceRefID] = struct{}{}
			findingSectionsByRef[ref.EvidenceRefID] = findingSections[index]
			providerOpportunity.Response = &GeneralSceneResponse{
				TurnID:        turn.TurnID,
				EvidenceRefID: ref.EvidenceRefID,
				Transcript:    turn.Transcript.Text,
			}
		}
		opportunities = append(opportunities, providerOpportunity)
	}
	coverage := ratio(answered, len(payload.OpportunityManifest))
	confidence := generalSceneConfidence(coverage)
	result := generalSceneResultSkeleton(
		snapshot,
		payload.PracticeContext,
	)
	if answered == 0 || wordCount < generalSceneMinimumWords {
		result.ScoreabilityStatus = GeneralSceneScoreabilityInsufficient
		for _, dimension := range generalSceneDimensionOrder {
			result.Dimensions = append(
				result.Dimensions,
				insufficientGeneralSceneDimension(dimension, coverage),
			)
		}
	}
	objectives := make(
		[]GeneralSceneObjective,
		len(payload.PracticeContext.PracticeObjectives),
	)
	for index, objective := range payload.PracticeContext.PracticeObjectives {
		objectives[index] = GeneralSceneObjective(objective)
	}
	return preparedGeneralScene{
		input: GeneralSceneProviderInput{
			SchemaVersion:        GeneralSceneProviderSchemaVersion,
			PromptVersion:        GeneralScenePromptVersion,
			SceneType:            snapshot.SceneType,
			PracticeExperience:   payload.PracticeContext.PracticeExperience,
			SceneCategory:        payload.PracticeContext.SceneCategory,
			PracticeMode:         payload.PracticeContext.PracticeMode,
			PracticeGoal:         payload.PracticeContext.PracticeGoal,
			PublicSceneBrief:     payload.PracticeContext.TaskContext.PublicSceneBrief,
			FocusAreas:           slices.Clone(payload.PracticeContext.TaskContext.FocusAreas),
			PracticeObjectives:   objectives,
			AssessableDimensions: GeneralSceneDimensions(),
			Opportunities:        opportunities,
		},
		result:               result,
		turnsByID:            turnsByID,
		refsByID:             refsByID,
		allowedRefs:          allowedRefs,
		findingSectionsByRef: findingSectionsByRef,
		coverage:             coverage,
		confidence:           confidence,
	}, nil
}

func generalSceneFindingSections(
	sceneType evaluation.SceneType,
	context evidence.PracticeContext,
	opportunityCount int,
) ([]IELTSPart, error) {
	sections := make([]IELTSPart, opportunityCount)
	if sceneType != evaluation.SceneIELTSSpeaking {
		return sections, nil
	}
	assignment := context.IELTSAssignment
	if assignment == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	index := 0
	for _, part := range assignment.Parts {
		partID := IELTSPart(part.Part)
		if !slices.Contains(ieltsPartOrder[:], partID) {
			return nil, evaluation.ErrInvalidRequest
		}
		for range part.TurnBlueprints {
			if index >= len(sections) {
				return nil, evaluation.ErrInvalidRequest
			}
			sections[index] = partID
			index++
		}
	}
	if index != len(sections) {
		return nil, evaluation.ErrInvalidRequest
	}
	return sections, nil
}

func generalSceneTypeSupported(sceneType evaluation.SceneType) bool {
	return sceneType == evaluation.SceneIELTSSpeaking ||
		sceneType == evaluation.SceneOverseasDaily ||
		sceneType == evaluation.SceneOverseasWorkplace
}

func generalSceneResultSkeleton(
	snapshot evidence.EvidenceSnapshot,
	context evidence.PracticeContext,
) GeneralSceneResult {
	return GeneralSceneResult{
		SchemaVersion:      GeneralSceneSchemaVersion,
		SnapshotID:         snapshot.ID,
		SceneType:          snapshot.SceneType,
		PracticeExperience: context.PracticeExperience,
		SceneCategory:      context.SceneCategory,
		PracticeMode:       context.PracticeMode,
		Scope:              evaluation.ScopeSession,
		Channel:            evaluation.ChannelScene,
		ScoreabilityStatus: GeneralSceneScoreabilityProvisional,
		Dimensions:         []GeneralSceneDimensionResult{},
		PriorityActions:    []GeneralScenePriorityAction{},
	}
}

func insufficientGeneralSceneDimension(
	dimension GeneralSceneDimension,
	coverage float64,
) GeneralSceneDimensionResult {
	return GeneralSceneDimensionResult{
		Key:          string(dimension),
		Scale:        GeneralSceneScalePercentage100,
		Coverage:     coverage,
		Confidence:   0,
		ReasonCodes:  []string{"INSUFFICIENT_EVIDENCE"},
		EvidenceRefs: []string{},
		Strengths:    []GeneralSceneFinding{},
		Improvements: []GeneralSceneFinding{},
		Examples:     []GeneralSceneFinding{},
	}
}

type generalSceneProviderPayload struct {
	SchemaVersion string                          `json:"schema_version"`
	Dimensions    []generalSceneProviderDimension `json:"dimensions"`
}

type generalSceneProviderDimension struct {
	DimensionID  GeneralSceneDimension         `json:"dimension_id"`
	Score        int                           `json:"score"`
	Strengths    []generalSceneProviderFinding `json:"strengths"`
	Improvements []generalSceneProviderFinding `json:"improvements"`
	Examples     []generalSceneProviderFinding `json:"recommended_examples"`
}

type generalSceneProviderFinding struct {
	TemplateID string                       `json:"template_id"`
	Evidence   []generalSceneProviderAnchor `json:"evidence"`
}

type generalSceneProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

type generalSceneFindingKind string

const (
	generalSceneStrength    generalSceneFindingKind = "STRENGTH"
	generalSceneImprovement generalSceneFindingKind = "IMPROVEMENT"
	generalSceneExample     generalSceneFindingKind = "RECOMMENDED_EXAMPLE"
)

type generalSceneFeedbackTemplate struct {
	ID         string
	Message    string
	Suggestion string
}

func generalSceneTemplate(
	dimension GeneralSceneDimension,
	kind generalSceneFindingKind,
) (generalSceneFeedbackTemplate, bool) {
	template := generalSceneFeedbackTemplate{
		ID: string(dimension) + ":" + string(kind) + ":v1",
	}
	switch dimension {
	case GeneralSceneDimensionTaskAchievement:
		switch kind {
		case generalSceneStrength:
			template.Message = "The response advances the communication goal with relevant information."
		case generalSceneImprovement:
			template.Message = "The response needs to make the intended outcome clearer."
			template.Suggestion = "State the intended outcome first, then add the information needed to achieve it."
		case generalSceneExample:
			template.Message = "Use a direct goal-oriented response pattern."
			template.Suggestion = "What I need is ..., and the key detail is ...."
		default:
			return generalSceneFeedbackTemplate{}, false
		}
	case GeneralSceneDimensionClarity:
		switch kind {
		case generalSceneStrength:
			template.Message = "The response presents its ideas in a clear, connected order."
		case generalSceneImprovement:
			template.Message = "The response's main point and supporting details are difficult to follow."
			template.Suggestion = "Keep one main point per sentence and connect the details in a clear sequence."
		case generalSceneExample:
			template.Message = "Use simple signposting to connect the response."
			template.Suggestion = "First, .... Also, .... So, ...."
		default:
			return generalSceneFeedbackTemplate{}, false
		}
	case GeneralSceneDimensionLanguage:
		switch kind {
		case generalSceneStrength:
			template.Message = "The response uses suitable vocabulary and sentence forms for the situation."
		case generalSceneImprovement:
			template.Message = "The response needs more precise wording or better-controlled sentence forms."
			template.Suggestion = "Prefer specific words and complete sentence patterns that fit the situation."
		case generalSceneExample:
			template.Message = "Use a more precise expression for the same meaning."
			template.Suggestion = "Replace the broad phrase with a specific action, request, or reason."
		default:
			return generalSceneFeedbackTemplate{}, false
		}
	case GeneralSceneDimensionInteraction:
		switch kind {
		case generalSceneStrength:
			template.Message = "The response addresses the other speaker and keeps the exchange moving."
		case generalSceneImprovement:
			template.Message = "The response needs to connect more directly to the other speaker's turn."
			template.Suggestion = "Acknowledge the question or request, answer it directly, then add one useful detail."
		case generalSceneExample:
			template.Message = "Use a direct interaction bridge."
			template.Suggestion = "Yes, .... The reason is ..., and I can ...."
		default:
			return generalSceneFeedbackTemplate{}, false
		}
	default:
		return generalSceneFeedbackTemplate{}, false
	}
	return template, true
}

func normalizeGeneralSceneProviderResult(
	prepared preparedGeneralScene,
	generated GeneralSceneProviderResult,
) (GeneralSceneResult, error) {
	if len(generated.Payload) == 0 ||
		len(generated.Payload) > generalSceneMaximumPayload ||
		!validProviderIdentifier(generated.Provider) ||
		!validModelIdentifier(generated.Model) ||
		!validProviderIdentifier(generated.RequestID) {
		return GeneralSceneResult{}, fmt.Errorf(
			"provider envelope: %w",
			ErrInvalidGeneralSceneResult,
		)
	}
	var payload generalSceneProviderPayload
	if !json.Valid(generated.Payload) {
		return GeneralSceneResult{}, fmt.Errorf(
			"provider JSON: %w",
			errGeneralSceneProviderInvalidJSON,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(generated.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil ||
		payload.SchemaVersion != GeneralSceneProviderSchemaVersion ||
		len(payload.Dimensions) != len(generalSceneDimensionOrder) {
		return GeneralSceneResult{}, fmt.Errorf(
			"provider JSON shape: %w",
			errGeneralSceneProviderSchemaMismatch,
		)
	}
	byDimension := make(
		map[GeneralSceneDimension]generalSceneProviderDimension,
		len(payload.Dimensions),
	)
	for _, dimension := range payload.Dimensions {
		if !slices.Contains(generalSceneDimensionOrder[:], dimension.DimensionID) {
			return GeneralSceneResult{}, fmt.Errorf(
				"provider dimension: %w",
				errGeneralSceneProviderSchemaMismatch,
			)
		}
		if _, duplicate := byDimension[dimension.DimensionID]; duplicate {
			return GeneralSceneResult{}, fmt.Errorf(
				"provider duplicate dimension: %w",
				errGeneralSceneProviderSchemaMismatch,
			)
		}
		byDimension[dimension.DimensionID] = dimension
	}
	result := prepared.result
	result.Provider = &GeneralSceneProviderLineage{
		Provider:       generated.Provider,
		Model:          generated.Model,
		RequestID:      generated.RequestID,
		PromptVersion:  GeneralScenePromptVersion,
		ResponseSchema: GeneralSceneProviderSchemaVersion,
	}
	for _, dimensionID := range generalSceneDimensionOrder {
		dimension, err := normalizeGeneralSceneDimension(
			prepared,
			byDimension[dimensionID],
		)
		if err != nil {
			return GeneralSceneResult{}, err
		}
		result.Dimensions = append(result.Dimensions, dimension)
	}
	result.PriorityActions = generalScenePriorityActions(result.Dimensions)
	return result, nil
}

func normalizeGeneralSceneDimension(
	prepared preparedGeneralScene,
	source generalSceneProviderDimension,
) (GeneralSceneDimensionResult, error) {
	if source.Score < 0 || source.Score > 100 ||
		source.Strengths == nil || source.Improvements == nil ||
		source.Examples == nil ||
		len(source.Strengths) > generalSceneMaximumProviderFindings ||
		len(source.Improvements) > generalSceneMaximumProviderFindings ||
		len(source.Examples) > generalSceneMaximumProviderFindings ||
		len(source.Strengths)+len(source.Improvements) == 0 {
		return GeneralSceneDimensionResult{}, ErrInvalidGeneralSceneResult
	}
	score := float64(source.Score)
	dimension := GeneralSceneDimensionResult{
		Key:          string(source.DimensionID),
		Score:        &score,
		Scale:        GeneralSceneScalePercentage100,
		Coverage:     prepared.coverage,
		Confidence:   prepared.confidence,
		ReasonCodes:  []string{"ASR_CONFIDENCE_UNAVAILABLE"},
		EvidenceRefs: []string{},
		Strengths:    []GeneralSceneFinding{},
		Improvements: []GeneralSceneFinding{},
		Examples:     []GeneralSceneFinding{},
	}
	collections := []struct {
		kind   generalSceneFindingKind
		source []generalSceneProviderFinding
		target *[]GeneralSceneFinding
	}{
		{generalSceneStrength, source.Strengths, &dimension.Strengths},
		{generalSceneImprovement, source.Improvements, &dimension.Improvements},
		{generalSceneExample, source.Examples, &dimension.Examples},
	}
	refSet := make(map[string]struct{})
	for _, collection := range collections {
		findings, err := normalizeGeneralSceneFindings(
			prepared,
			source.DimensionID,
			collection.kind,
			collection.source,
		)
		if err != nil {
			return GeneralSceneDimensionResult{}, err
		}
		*collection.target = findings
		for _, finding := range findings {
			for _, evidence := range finding.Evidence {
				refSet[evidence.EvidenceRefID] = struct{}{}
			}
		}
	}
	for refID := range refSet {
		dimension.EvidenceRefs = append(dimension.EvidenceRefs, refID)
	}
	slices.Sort(dimension.EvidenceRefs)
	return dimension, nil
}

func normalizeGeneralSceneFindings(
	prepared preparedGeneralScene,
	dimension GeneralSceneDimension,
	kind generalSceneFindingKind,
	source []generalSceneProviderFinding,
) ([]GeneralSceneFinding, error) {
	template, exists := generalSceneTemplate(dimension, kind)
	if !exists {
		return nil, ErrInvalidGeneralSceneResult
	}
	if len(source) == 0 {
		return []GeneralSceneFinding{}, nil
	}
	type findingGroup struct {
		evidence []GeneralSceneEvidence
	}
	groups := make([]findingGroup, 0, len(source))
	groupIndexes := make(map[IELTSPart]int)
	seenAnchors := make(map[string]struct{})
	for _, item := range source {
		if item.TemplateID != template.ID || len(item.Evidence) == 0 ||
			len(item.Evidence) > generalSceneMaximumAnchors {
			return nil, ErrInvalidGeneralSceneResult
		}
		itemAnchors := make(map[string]struct{}, len(item.Evidence))
		for _, anchor := range item.Evidence {
			resolved, err := resolveGeneralSceneAnchor(prepared, anchor)
			if err != nil {
				return nil, err
			}
			key := resolved.EvidenceRefID + "\x00" +
				strconv.Itoa(resolved.StartUTF8Byte) + "\x00" +
				strconv.Itoa(resolved.EndUTF8Byte)
			if _, duplicate := itemAnchors[key]; duplicate {
				return nil, ErrInvalidGeneralSceneResult
			}
			itemAnchors[key] = struct{}{}
			if _, duplicate := seenAnchors[key]; duplicate {
				continue
			}
			seenAnchors[key] = struct{}{}
			section, sectionExists :=
				prepared.findingSectionsByRef[resolved.EvidenceRefID]
			if !sectionExists {
				return nil, ErrInvalidGeneralSceneResult
			}
			groupIndex, groupExists := groupIndexes[section]
			if !groupExists {
				groupIndex = len(groups)
				groupIndexes[section] = groupIndex
				groups = append(groups, findingGroup{})
			}
			groups[groupIndex].evidence = append(
				groups[groupIndex].evidence,
				resolved,
			)
		}
	}
	result := make([]GeneralSceneFinding, 0, len(groups))
	for _, group := range groups {
		for start := 0; start < len(group.evidence); start += generalSceneMaximumAnchors {
			end := min(start+generalSceneMaximumAnchors, len(group.evidence))
			finding := GeneralSceneFinding{
				Message:    template.Message,
				Suggestion: template.Suggestion,
				Evidence:   slices.Clone(group.evidence[start:end]),
			}
			finding.ID = stableGeneralSceneFindingID(
				prepared.result.SnapshotID,
				dimension,
				kind,
				finding,
			)
			result = append(result, finding)
		}
	}
	if len(result) > generalSceneMaximumNormalizedFindings {
		return nil, ErrInvalidGeneralSceneResult
	}
	return result, nil
}

func resolveGeneralSceneAnchor(
	prepared preparedGeneralScene,
	anchor generalSceneProviderAnchor,
) (GeneralSceneEvidence, error) {
	ref, exists := prepared.refsByID[anchor.EvidenceRefID]
	_, allowed := prepared.allowedRefs[anchor.EvidenceRefID]
	turn, turnExists := prepared.turnsByID[ref.TurnID]
	if !exists || !allowed || !turnExists || anchor.Occurrence < 1 ||
		anchor.Occurrence > generalSceneMaximumOccurrence ||
		!validGeneralSceneText(anchor.Quote) {
		return GeneralSceneEvidence{}, ErrInvalidGeneralSceneResult
	}
	start := nthGeneralSceneOccurrence(
		turn.Transcript.Text,
		anchor.Quote,
		anchor.Occurrence,
	)
	end := start + len(anchor.Quote)
	if start < ref.TranscriptSpan.StartUTF8Byte ||
		end > ref.TranscriptSpan.EndUTF8Byte || start < 0 || end <= start ||
		!utf8.ValidString(turn.Transcript.Text[start:end]) {
		return GeneralSceneEvidence{}, ErrInvalidGeneralSceneResult
	}
	return GeneralSceneEvidence{
		EvidenceRefID:   ref.EvidenceRefID,
		TurnID:          ref.TurnID,
		StartUTF8Byte:   start,
		EndUTF8Byte:     end,
		OriginalExcerpt: turn.Transcript.Text[start:end],
	}, nil
}

func generalScenePriorityActions(
	dimensions []GeneralSceneDimensionResult,
) []GeneralScenePriorityAction {
	candidates := make([]GeneralSceneDimensionResult, 0, len(dimensions))
	for _, dimension := range dimensions {
		if dimension.Score != nil && len(dimension.Improvements) > 0 {
			candidates = append(candidates, dimension)
		}
	}
	slices.SortStableFunc(candidates, func(left, right GeneralSceneDimensionResult) int {
		if *left.Score < *right.Score {
			return -1
		}
		if *left.Score > *right.Score {
			return 1
		}
		return strings.Compare(left.Key, right.Key)
	})
	limit := min(3, len(candidates))
	actions := make([]GeneralScenePriorityAction, 0, limit)
	for _, dimension := range candidates[:limit] {
		actions = append(actions, GeneralScenePriorityAction{
			DimensionKey: dimension.Key,
			FindingID:    dimension.Improvements[0].ID,
		})
	}
	return actions
}

func ValidateGeneralSceneResult(
	snapshot evidence.EvidenceSnapshot,
	result GeneralSceneResult,
) error {
	prepared, err := prepareGeneralScene(snapshot)
	if err != nil || result.SchemaVersion != GeneralSceneSchemaVersion ||
		result.SnapshotID != snapshot.ID ||
		result.SceneType != snapshot.SceneType ||
		result.PracticeExperience != prepared.result.PracticeExperience ||
		result.SceneCategory != prepared.result.SceneCategory ||
		result.PracticeMode != prepared.result.PracticeMode ||
		result.Scope != evaluation.ScopeSession || result.Channel != evaluation.ChannelScene ||
		result.Dimensions == nil ||
		len(result.Dimensions) != len(generalSceneDimensionOrder) ||
		result.PriorityActions == nil {
		return ErrInvalidGeneralSceneResult
	}
	insufficient := prepared.result.ScoreabilityStatus ==
		GeneralSceneScoreabilityInsufficient
	if insufficient {
		if result.ScoreabilityStatus != GeneralSceneScoreabilityInsufficient ||
			result.Provider != nil || len(result.PriorityActions) != 0 {
			return ErrInvalidGeneralSceneResult
		}
	} else if result.ScoreabilityStatus != GeneralSceneScoreabilityProvisional ||
		!validGeneralSceneLineage(result.Provider) {
		return ErrInvalidGeneralSceneResult
	}
	seenFindings := make(map[string]struct{})
	for index, dimension := range result.Dimensions {
		expected := generalSceneDimensionOrder[index]
		if dimension.Key != string(expected) ||
			dimension.Scale != GeneralSceneScalePercentage100 ||
			!sameRatio(dimension.Coverage, prepared.coverage) {
			return ErrInvalidGeneralSceneResult
		}
		if insufficient {
			if dimension.Score != nil || dimension.Confidence != 0 ||
				!slices.Equal(dimension.ReasonCodes, []string{"INSUFFICIENT_EVIDENCE"}) ||
				len(dimension.EvidenceRefs) != 0 ||
				len(dimension.Strengths) != 0 ||
				len(dimension.Improvements) != 0 || len(dimension.Examples) != 0 {
				return ErrInvalidGeneralSceneResult
			}
			continue
		}
		if dimension.Score == nil || *dimension.Score < 0 ||
			*dimension.Score > 100 ||
			math.IsNaN(*dimension.Score) || math.IsInf(*dimension.Score, 0) ||
			!sameRatio(dimension.Confidence, prepared.confidence) ||
			!slices.Equal(dimension.ReasonCodes, []string{"ASR_CONFIDENCE_UNAVAILABLE"}) ||
			len(dimension.Strengths)+len(dimension.Improvements) == 0 ||
			len(dimension.Strengths) > generalSceneMaximumNormalizedFindings ||
			len(dimension.Improvements) > generalSceneMaximumNormalizedFindings ||
			len(dimension.Examples) > generalSceneMaximumNormalizedFindings ||
			!validateGeneralSceneDimensionFindings(
				prepared,
				expected,
				dimension,
				seenFindings,
			) {
			return ErrInvalidGeneralSceneResult
		}
	}
	if !slices.Equal(
		result.PriorityActions,
		generalScenePriorityActions(result.Dimensions),
	) {
		return ErrInvalidGeneralSceneResult
	}
	return nil
}

func validateGeneralSceneDimensionFindings(
	prepared preparedGeneralScene,
	dimension GeneralSceneDimension,
	result GeneralSceneDimensionResult,
	seenFindings map[string]struct{},
) bool {
	refSet := make(map[string]struct{})
	collections := []struct {
		kind     generalSceneFindingKind
		findings []GeneralSceneFinding
	}{
		{generalSceneStrength, result.Strengths},
		{generalSceneImprovement, result.Improvements},
		{generalSceneExample, result.Examples},
	}
	for _, collection := range collections {
		template, exists := generalSceneTemplate(dimension, collection.kind)
		if !exists ||
			len(collection.findings) > generalSceneMaximumNormalizedFindings {
			return false
		}
		seenAnchors := make(map[string]struct{})
		lastChunkSizes := make(map[IELTSPart]int)
		for _, finding := range collection.findings {
			if finding.Message != template.Message ||
				finding.Suggestion != template.Suggestion ||
				len(finding.Evidence) == 0 ||
				len(finding.Evidence) > generalSceneMaximumAnchors ||
				finding.ID != stableGeneralSceneFindingID(
					prepared.result.SnapshotID,
					dimension,
					collection.kind,
					finding,
				) {
				return false
			}
			section, sectionExists :=
				prepared.findingSectionsByRef[finding.Evidence[0].EvidenceRefID]
			if !sectionExists {
				return false
			}
			if previousSize, seen := lastChunkSizes[section]; seen && previousSize < generalSceneMaximumAnchors {
				return false
			}
			if _, duplicate := seenFindings[finding.ID]; duplicate {
				return false
			}
			seenFindings[finding.ID] = struct{}{}
			for _, evidence := range finding.Evidence {
				evidenceSection, exists :=
					prepared.findingSectionsByRef[evidence.EvidenceRefID]
				if !exists || evidenceSection != section ||
					!validResolvedGeneralSceneEvidence(prepared, evidence) {
					return false
				}
				key := evidence.EvidenceRefID + "\x00" +
					strconv.Itoa(evidence.StartUTF8Byte) + "\x00" +
					strconv.Itoa(evidence.EndUTF8Byte)
				if _, duplicate := seenAnchors[key]; duplicate {
					return false
				}
				seenAnchors[key] = struct{}{}
				refSet[evidence.EvidenceRefID] = struct{}{}
			}
			lastChunkSizes[section] = len(finding.Evidence)
		}
	}
	expectedRefs := make([]string, 0, len(refSet))
	for refID := range refSet {
		expectedRefs = append(expectedRefs, refID)
	}
	slices.Sort(expectedRefs)
	return slices.Equal(result.EvidenceRefs, expectedRefs)
}

func validResolvedGeneralSceneEvidence(
	prepared preparedGeneralScene,
	evidence GeneralSceneEvidence,
) bool {
	ref, exists := prepared.refsByID[evidence.EvidenceRefID]
	_, allowed := prepared.allowedRefs[evidence.EvidenceRefID]
	turn, turnExists := prepared.turnsByID[ref.TurnID]
	if !exists || !allowed || !turnExists ||
		evidence.TurnID != ref.TurnID ||
		evidence.StartUTF8Byte < ref.TranscriptSpan.StartUTF8Byte ||
		evidence.EndUTF8Byte > ref.TranscriptSpan.EndUTF8Byte ||
		evidence.EndUTF8Byte <= evidence.StartUTF8Byte {
		return false
	}
	excerpt := turn.Transcript.Text[evidence.StartUTF8Byte:evidence.EndUTF8Byte]
	return utf8.ValidString(excerpt) && evidence.OriginalExcerpt == excerpt
}

func validGeneralSceneLineage(lineage *GeneralSceneProviderLineage) bool {
	return lineage != nil && validProviderIdentifier(lineage.Provider) &&
		validModelIdentifier(lineage.Model) &&
		validProviderIdentifier(lineage.RequestID) &&
		lineage.PromptVersion == GeneralScenePromptVersion &&
		lineage.ResponseSchema == GeneralSceneProviderSchemaVersion
}

func generalSceneConfidence(coverage float64) float64 {
	value := coverage * 0.6
	return math.Round(value*1000) / 1000
}

func generalSceneWordCount(value string) int {
	count := 0
	inWord := false
	for _, item := range value {
		word := unicode.IsLetter(item) || unicode.IsDigit(item) || item == '\''
		if word && !inWord {
			count++
		}
		inWord = word
	}
	return count
}

func validGeneralSceneText(value string) bool {
	return strings.TrimSpace(value) == value && value != "" &&
		len(value) <= generalSceneMaximumTextBytes && utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00')
}

func nthGeneralSceneOccurrence(value string, quote string, occurrence int) int {
	start := 0
	for current := 1; current <= occurrence; current++ {
		index := strings.Index(value[start:], quote)
		if index < 0 {
			return -1
		}
		index += start
		if current == occurrence {
			return index
		}
		start = index + len(quote)
	}
	return -1
}

func stableGeneralSceneFindingID(
	snapshotID string,
	dimension GeneralSceneDimension,
	kind generalSceneFindingKind,
	finding GeneralSceneFinding,
) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("general-scene-finding:v1\x00"))
	_, _ = hasher.Write([]byte(snapshotID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(dimension))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(kind))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(finding.Message))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(finding.Suggestion))
	for _, evidence := range finding.Evidence {
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(evidence.EvidenceRefID))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(strconv.Itoa(evidence.StartUTF8Byte)))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(strconv.Itoa(evidence.EndUTF8Byte)))
	}
	return "general-finding:" + hex.EncodeToString(hasher.Sum(nil)[:12])
}
