package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

const ieltsSpeakingCriterionToolName = "ielts.speaking.criterion.v3"

// discipline: Qiniu's basic schema cannot express the cross-array primary rule
// or dynamic evidence identity; the local validator owns those rules.
func ieltsSpeakingCriterionToolSchema(
	criterion scoring.IELTSCriterion,
	rubricRequired bool,
) map[string]any {
	required := []string{
		"criterion_id",
		"strengths",
		"improvements",
		"upgrade_examples",
	}
	if rubricRequired {
		required = append(required, "rubric_descriptor")
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"schema_version", "criteria"},
		"properties": map[string]any{
			"schema_version": map[string]any{
				"type": "string",
				"enum": []string{
					scoring.IELTSSpeakingShadowProviderSchemaVersion,
				},
			},
			"criteria": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             required,
					"properties": map[string]any{
						"criterion_id": map[string]any{
							"type": "string",
							"enum": []string{string(criterion)},
						},
						"rubric_descriptor": map[string]any{
							"type": "string",
							"enum": ieltsSpeakingRubricDescriptorIDs(
								criterion,
							),
						},
						"strengths": ieltsSpeakingFindingArraySchema(
							criterion,
							"strength",
							false,
							true,
						),
						"improvements": ieltsSpeakingFindingArraySchema(
							criterion,
							"improvement",
							true,
							false,
						),
						"upgrade_examples": ieltsSpeakingFindingArraySchema(
							criterion,
							"upgrade",
							true,
							false,
						),
					},
				},
			},
		},
	}
}

func ieltsSpeakingFindingArraySchema(
	criterion scoring.IELTSCriterion,
	kind string,
	allowSuggestion bool,
	requireFinding bool,
) map[string]any {
	properties := map[string]any{
		"template_id": map[string]any{
			"type": "string",
			"enum": []string{
				"ielts." + ieltsSpeakingCriterionToken(criterion) +
					"." + kind + ".v1",
			},
		},
		"evidence": map[string]any{
			"type":     "array",
			"minItems": 1,
			"maxItems": 4,
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required": []string{
					"evidence_ref_id",
					"quote",
					"occurrence",
				},
				"properties": map[string]any{
					"evidence_ref_id": map[string]any{
						"type": "string", "minLength": 1, "maxLength": 128,
					},
					"quote": map[string]any{
						"type": "string", "minLength": 1, "maxLength": 512,
					},
					"occurrence": map[string]any{
						"type": "integer", "minimum": 1, "maximum": 16,
					},
				},
			},
		},
	}
	if allowSuggestion {
		properties["suggestion"] = map[string]any{
			"type": "string", "minLength": 1, "maxLength": 512,
		}
	}
	result := map[string]any{
		"type":     "array",
		"maxItems": 3,
		"items": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"template_id", "evidence"},
			"properties":           properties,
		},
	}
	if requireFinding {
		result["minItems"] = 1
	}
	return result
}

func ieltsSpeakingRubricDescriptorIDs(
	criterion scoring.IELTSCriterion,
) []string {
	descriptors := scoring.IELTSRubricDescriptors(criterion)
	result := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		result = append(result, descriptor.ID)
	}
	return result
}

func ieltsSpeakingCriterionToken(criterion scoring.IELTSCriterion) string {
	return strings.ToLower(strings.TrimPrefix(string(criterion), "IELTS_"))
}

func validIELTSSpeakingCriterion(criterion scoring.IELTSCriterion) bool {
	switch criterion {
	case scoring.IELTSCriterionFC,
		scoring.IELTSCriterionLR,
		scoring.IELTSCriterionGRA,
		scoring.IELTSCriterionPR:
		return true
	default:
		return false
	}
}

type EvaluationScoringGenerator struct {
	generator *textClient
}

func NewEvaluationScoringGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationScoringGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &EvaluationScoringGenerator{generator: generator}, nil
}

func (generator *EvaluationScoringGenerator) Generate(
	ctx context.Context,
	request scoring.TextGenerationRequest,
) (scoring.TextGenerationResult, error) {
	if generator == nil || generator.generator == nil || ctx == nil ||
		request.SystemPrompt == "" || request.UserPrompt == "" {
		return scoring.TextGenerationResult{}, errors.New(
			"qianwen: invalid Evaluation scoring request",
		)
	}
	providerRequest := protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: request.SystemPrompt},
			{Role: protocol.TextRoleUser, Content: request.UserPrompt},
		},
		ResponseFormat: protocol.TextResponseFormatJSON,
	}
	validOutputContract := false
	switch request.OutputContract {
	case scoring.TextGenerationOutputDefault:
		validOutputContract = request.OutputCriterion == ""
	case scoring.TextGenerationOutputIELTSSpeakingCriterionV3:
		validOutputContract = validIELTSSpeakingCriterion(
			request.OutputCriterion,
		)
	}
	if !validOutputContract {
		return scoring.TextGenerationResult{}, errors.New(
			"qianwen: Evaluation scoring output contract is unsupported",
		)
	}
	ieltsCriterion := request.OutputContract ==
		scoring.TextGenerationOutputIELTSSpeakingCriterionV3
	qiniuStructuredIELTS := generator.generator.provider == qiniuProviderName &&
		ieltsCriterion
	qianwenStructuredIELTS := generator.generator.provider == providerName &&
		ieltsCriterion
	if qiniuStructuredIELTS {
		providerRequest.ResponseFormat = protocol.TextResponseFormatDefault
		providerRequest.Tools = []protocol.ToolDefinition{{
			Name: ieltsSpeakingCriterionToolName,
			Description: "Return one evidence-bound IELTS Speaking " +
				"criterion.",
			InputSchema: ieltsSpeakingCriterionToolSchema(
				request.OutputCriterion,
				request.OutputRubricRequired,
			),
		}}
		providerRequest.ToolChoice = protocol.ToolChoice{
			Mode: protocol.ToolChoiceSpecific,
			Name: ieltsSpeakingCriterionToolName,
		}
	} else if qianwenStructuredIELTS {
		providerRequest.ResponseFormat = protocol.TextResponseFormatJSONSchema
		providerRequest.ResponseSchema = &protocol.JSONSchemaDefinition{
			Name:   "ielts_speaking_criterion_v3",
			Strict: true,
			Schema: ieltsSpeakingCriterionToolSchema(
				request.OutputCriterion,
				request.OutputRubricRequired,
			),
		}
	}
	result, err := generator.generator.Generate(ctx, providerRequest)
	if err != nil {
		return scoring.TextGenerationResult{}, err
	}
	content := result.Content
	if qiniuStructuredIELTS {
		if strings.TrimSpace(result.Content) != "" ||
			result.FinishReason != "tool_calls" ||
			len(result.ToolCalls) != 1 ||
			result.ToolCalls[0].Name != ieltsSpeakingCriterionToolName ||
			!json.Valid(result.ToolCalls[0].Arguments) {
			return scoring.TextGenerationResult{}, errors.New(
				"qianwen: IELTS criterion returned an invalid tool call",
			)
		}
		content = string(result.ToolCalls[0].Arguments)
	} else if qianwenStructuredIELTS &&
		(result.FinishReason != "stop" || !json.Valid([]byte(content))) {
		return scoring.TextGenerationResult{}, protocol.NewGenerationError(
			protocol.ErrorInvalidResponse,
			0,
			"",
			result.ID,
			errors.New("qianwen: IELTS criterion violated JSON Schema output"),
		)
	}
	return scoring.TextGenerationResult{
		RequestID: result.ID,
		Content:   content,
		Provider:  result.Provider,
		Model:     result.Model,
	}, nil
}

var _ scoring.TextGenerator = (*EvaluationScoringGenerator)(nil)
