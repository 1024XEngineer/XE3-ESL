package qiniu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

const liveQiniuContractRequestInterval = 5 * time.Second

func TestLiveQiniuTextGeneration(t *testing.T) {
	generator, model := liveQiniuAgentGenerator(t)

	request := agentrun.TextRequest{Messages: []agentrun.TextMessage{{
		Role: agentrun.TextRoleUser,
		Content: "You are a friendly English speaking coach. The learner says: " +
			"I want to improve my spoken English for travel. Reply naturally in " +
			"exactly three complete English sentences, 45 to 60 words total. " +
			"Ask one useful follow-up question. Do not use markdown.",
	}}}
	var streamed strings.Builder
	var firstSentenceAt time.Duration
	firstSentenceDelta := 0
	nonEmptyDeltas := 0
	streamStarted := time.Now()
	streamResult, err := generator.GenerateStream(
		context.Background(),
		request,
		agentrun.TextDeltaObserverFunc(func(_ context.Context, delta string) error {
			if strings.TrimSpace(delta) != "" {
				nonEmptyDeltas++
			}
			streamed.WriteString(delta)
			if firstSentenceAt == 0 && strings.ContainsAny(streamed.String(), ".!?") {
				firstSentenceAt = time.Since(streamStarted)
				firstSentenceDelta = nonEmptyDeltas
			}
			return nil
		}),
	)
	streamDuration := time.Since(streamStarted)
	if err != nil {
		t.Fatalf("live Qiniu streaming generation failed: %v", err)
	}
	if streamResult.Provider != TextProviderName ||
		streamResult.Model != model ||
		streamResult.Content == "" || streamResult.Content != streamed.String() ||
		streamResult.Usage.TotalTokens <= 0 {
		t.Fatalf("invalid live Qiniu stream result: %#v", streamResult)
	}
	if nonEmptyDeltas <= 1 {
		t.Fatalf("Qiniu stream emitted %d non-empty visible delta(s), want more than one", nonEmptyDeltas)
	}
	if firstSentenceAt == 0 || firstSentenceDelta >= nonEmptyDeltas {
		t.Fatalf(
			"first complete sentence delta = %d/%d, time = %s, stream duration = %s",
			firstSentenceDelta,
			nonEmptyDeltas,
			firstSentenceAt,
			streamDuration,
		)
	}
	t.Logf(
		"Qiniu model=%s visible_deltas=%d first_sentence=%s total=%s",
		model,
		nonEmptyDeltas,
		firstSentenceAt.Round(time.Millisecond),
		streamDuration.Round(time.Millisecond),
	)
}

func TestLiveQiniuAgentContracts(t *testing.T) {
	generator, model := liveQiniuAgentGenerator(t)
	ctx := context.Background()

	toolResult, err := generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{{
			Role:    agentrun.TextRoleUser,
			Content: "Save that I prefer short daily English practice.",
		}},
		Tools: []agentrun.ToolDefinition{{
			Name:        "preference.save.v1",
			Description: "Save one English practice preference.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preference": map[string]any{"type": "string"},
				},
				"required": []string{"preference"},
			},
		}},
		ToolChoice: agentrun.ToolChoice{Mode: agentrun.ToolChoiceRequired},
	})
	if err != nil {
		t.Fatalf("live Qiniu tool generation failed: %v", err)
	}
	if toolResult.Model != model || len(toolResult.ToolCalls) != 1 ||
		toolResult.ToolCalls[0].Name != "preference.save.v1" {
		t.Fatalf("invalid live Qiniu tool result: %#v", toolResult)
	}
	var toolArguments map[string]any
	if err := json.Unmarshal(toolResult.ToolCalls[0].Arguments, &toolArguments); err != nil ||
		strings.TrimSpace(valueString(toolArguments["preference"])) == "" {
		t.Fatalf("invalid live Qiniu tool arguments: %#v, %v", toolArguments, err)
	}
	time.Sleep(liveQiniuContractRequestInterval)

	imageResult, err := generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{{
			Role: agentrun.TextRoleUser,
			ContentParts: []agentrun.ContentPart{
				{
					Kind: agentrun.ContentPartText,
					Text: "Return only a JSON object with keys company, tip, and " +
						"example. Identify the company in this image, then give one " +
						"short English-learning tip and example.",
				},
				{
					Kind:     agentrun.ContentPartImageURL,
					ImageURL: "https://www.qiniu.com/qiniu_ai_token_snapshot.png",
				},
			},
		}},
		ResponseFormat: agentrun.TextResponseFormatJSON,
	})
	if err != nil {
		t.Fatalf("live Qiniu multimodal JSON generation failed: %v", err)
	}
	var structured map[string]any
	if err := json.Unmarshal([]byte(imageResult.Content), &structured); err != nil ||
		strings.TrimSpace(valueString(structured["tip"])) == "" ||
		strings.TrimSpace(valueString(structured["example"])) == "" {
		t.Fatalf("invalid live Qiniu multimodal JSON result: %#v, %v", structured, err)
	}
	company := valueString(structured["company"])
	if !strings.Contains(strings.ToLower(company), "qiniu") &&
		!strings.Contains(company, "七牛") {
		t.Fatalf("live Qiniu image answer did not identify Qiniu: %q", company)
	}
}

func TestLiveQiniuIELTSEvaluationContract(t *testing.T) {
	configuration := liveQiniuConfiguration(t)
	generator, err := NewEvaluationScoringGenerator(TextConfig{
		BaseURL: configuration.BaseURL, Model: configuration.EvaluationModel,
		Timeout: configuration.Timeout, MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qiniu Evaluation generator: %v", err)
	}
	result, err := generator.Generate(
		context.Background(),
		scoring.TextGenerationRequest{
			SystemPrompt: scoring.IELTSSpeakingShadowSystemContract,
			UserPrompt:   liveIELTSEvaluationEvidence,
		},
	)
	if err != nil {
		t.Fatalf("live Qiniu IELTS Evaluation failed: %v", err)
	}
	if result.Provider != TextProviderName ||
		result.Model != configuration.EvaluationModel ||
		strings.TrimSpace(result.RequestID) == "" {
		t.Fatalf(
			"invalid IELTS Evaluation lineage: provider=%q model=%q request_id_present=%t",
			result.Provider,
			result.Model,
			strings.TrimSpace(result.RequestID) != "",
		)
	}
	if err := validateLiveIELTSEvaluationPayload([]byte(result.Content)); err != nil {
		t.Fatalf("live Qiniu IELTS contract rejected: %v", err)
	}
	t.Logf(
		"Qiniu IELTS Evaluation model=%s passed strict sanitized contract",
		configuration.EvaluationModel,
	)
}

func liveQiniuAgentGenerator(t *testing.T) (*AgentRunGenerator, string) {
	t.Helper()
	configuration := liveQiniuConfiguration(t)
	generator, err := NewAgentRunGenerator(TextConfig{
		BaseURL: configuration.BaseURL, Model: configuration.Model,
		Timeout: configuration.Timeout, MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qiniu text generator: %v", err)
	}
	return generator, configuration.Model
}

func liveQiniuConfiguration(t *testing.T) platformconfig.TextGenerationConfig {
	t.Helper()
	if os.Getenv("QINIU_LLM_LIVE_TEST") != "1" {
		t.Skip("set QINIU_LLM_LIVE_TEST=1 with the Qiniu AI environment variables; the real request may incur charges")
	}
	configuration, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load Qiniu text generation config: %v", err)
	}
	if configuration.Provider != platformconfig.TextProviderQiniu {
		t.Fatalf("TEXT_GENERATION_PROVIDER = %q, want qiniu", configuration.Provider)
	}
	return configuration
}

func valueString(value any) string {
	text, _ := value.(string)
	return text
}

type liveIELTSEvaluationPayload struct {
	SchemaVersion string                         `json:"schema_version"`
	Criteria      []liveIELTSEvaluationCriterion `json:"criteria"`
}

type liveIELTSEvaluationCriterion struct {
	CriterionID      scoring.IELTSCriterion       `json:"criterion_id"`
	RubricDescriptor string                       `json:"rubric_descriptor,omitempty"`
	Strengths        []liveIELTSEvaluationFinding `json:"strengths"`
	Improvements     []liveIELTSEvaluationFinding `json:"improvements"`
	UpgradeExamples  []liveIELTSEvaluationFinding `json:"upgrade_examples"`
}

type liveIELTSEvaluationFinding struct {
	TemplateID string                      `json:"template_id"`
	Suggestion string                      `json:"suggestion,omitempty"`
	Evidence   []liveIELTSEvaluationAnchor `json:"evidence"`
}

type liveIELTSEvaluationAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

const liveIELTSEvaluationEvidence = `{
  "schema_version":"ielts-speaking-full-mock-shadow-provider/v2",
  "prompt_version":"ielts-speaking-full-mock-shadow-prompt/v5",
  "rubric_version":"ielts-speaking-public-band-rubric/v2",
  "scene_type":"IELTS_SPEAKING",
  "practice_mode":"FULL_MOCK",
  "rubric_descriptors":[
    {"criterion_id":"IELTS_LR","descriptors":[{"descriptor_id":"LR_PRACTICE_BAND_6","band":6,"description":"Uses enough vocabulary to discuss topics at length."}]},
    {"criterion_id":"IELTS_GRA","descriptors":[{"descriptor_id":"GRA_PRACTICE_BAND_6","band":6,"description":"Uses a mix of simple and complex structures."}]}
  ],
  "assessable_criteria":["IELTS_FC","IELTS_LR","IELTS_GRA"],
  "questions":[
    {"question_id":"question-1","part_id":"PART_1","index":1,"question_text":"What kind of music do you enjoy?","response":{"turn_id":"turn-1","evidence_ref_id":"evidence-1","confirmed_transcript":"I enjoy calm jazz because it helps me concentrate after a busy day.","recording_duration_ms":9000,"english_word_count":13,"cjk_character_count":0,"language_evidence":"ENGLISH"}},
    {"question_id":"question-2","part_id":"PART_2","index":2,"question_text":"Describe a useful skill you learned.","response":{"turn_id":"turn-2","evidence_ref_id":"evidence-2","confirmed_transcript":"I learned to cook at university, and regular practice made me more confident.","recording_duration_ms":10000,"english_word_count":13,"cjk_character_count":0,"language_evidence":"ENGLISH"}},
    {"question_id":"question-3","part_id":"PART_3","index":3,"question_text":"Why do adults continue learning?","response":{"turn_id":"turn-3","evidence_ref_id":"evidence-3","confirmed_transcript":"Adults keep learning because work changes quickly and new skills bring satisfaction.","recording_duration_ms":10000,"english_word_count":12,"cjk_character_count":0,"language_evidence":"ENGLISH"}}
  ]
}`

func validateLiveIELTSEvaluationPayload(raw []byte) error {
	var payload liveIELTSEvaluationPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	expected := [...]scoring.IELTSCriterion{
		scoring.IELTSCriterionFC,
		scoring.IELTSCriterionLR,
		scoring.IELTSCriterionGRA,
	}
	if payload.SchemaVersion != scoring.IELTSSpeakingShadowProviderSchemaVersion ||
		len(payload.Criteria) != len(expected) {
		return errLiveIELTSEvaluationSchema
	}
	descriptors := map[scoring.IELTSCriterion]string{
		scoring.IELTSCriterionLR:  "LR_PRACTICE_BAND_6",
		scoring.IELTSCriterionGRA: "GRA_PRACTICE_BAND_6",
	}
	transcripts := map[string]string{
		"evidence-1": "I enjoy calm jazz because it helps me concentrate after a busy day.",
		"evidence-2": "I learned to cook at university, and regular practice made me more confident.",
		"evidence-3": "Adults keep learning because work changes quickly and new skills bring satisfaction.",
	}
	for index, criterion := range payload.Criteria {
		criterionID := expected[index]
		if criterion.CriterionID != criterionID ||
			!validLiveIELTSRubricDescriptor(
				criterionID,
				criterion.RubricDescriptor,
				descriptors[criterionID],
			) ||
			!validLiveIELTSFindings(criterionID, "strength", criterion.Strengths, transcripts) ||
			!validLiveIELTSFindings(criterionID, "improvement", criterion.Improvements, transcripts) ||
			!validLiveIELTSFindings(criterionID, "upgrade", criterion.UpgradeExamples, transcripts) ||
			len(criterion.Strengths)+len(criterion.Improvements) == 0 {
			return errLiveIELTSEvaluationSchema
		}
	}
	return nil
}

func validLiveIELTSRubricDescriptor(
	criterion scoring.IELTSCriterion,
	descriptor string,
	expected string,
) bool {
	if criterion != scoring.IELTSCriterionFC {
		return descriptor == expected
	}
	if descriptor == "" {
		return true
	}
	const prefix = "FC_PRACTICE_BAND_"
	return strings.HasPrefix(descriptor, prefix) &&
		len(descriptor) == len(prefix)+1 &&
		descriptor[len(prefix)] >= '1' && descriptor[len(prefix)] <= '9'
}

func TestValidLiveIELTSRubricDescriptor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		criterion  scoring.IELTSCriterion
		descriptor string
		expected   string
		valid      bool
	}{
		{name: "text-only FC omitted", criterion: scoring.IELTSCriterionFC, valid: true},
		{name: "text-only FC known band", criterion: scoring.IELTSCriterionFC, descriptor: "FC_PRACTICE_BAND_6", valid: true},
		{name: "text-only FC unknown band", criterion: scoring.IELTSCriterionFC, descriptor: "FC_PRACTICE_BAND_10"},
		{name: "text-only FC arbitrary", criterion: scoring.IELTSCriterionFC, descriptor: "FC_UNTRUSTED"},
		{name: "LR selected descriptor", criterion: scoring.IELTSCriterionLR, descriptor: "LR_PRACTICE_BAND_6", expected: "LR_PRACTICE_BAND_6", valid: true},
		{name: "LR descriptor outside input", criterion: scoring.IELTSCriterionLR, descriptor: "LR_PRACTICE_BAND_7", expected: "LR_PRACTICE_BAND_6"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validLiveIELTSRubricDescriptor(
				test.criterion,
				test.descriptor,
				test.expected,
			); got != test.valid {
				t.Fatalf("valid = %t, want %t", got, test.valid)
			}
		})
	}
}

var errLiveIELTSEvaluationSchema = errors.New(
	"IELTS Evaluation response did not match the strict contract",
)

func validLiveIELTSFindings(
	criterion scoring.IELTSCriterion,
	kind string,
	findings []liveIELTSEvaluationFinding,
	transcripts map[string]string,
) bool {
	if findings == nil || len(findings) > 3 {
		return false
	}
	prefix := strings.ToLower(strings.TrimPrefix(string(criterion), "IELTS_"))
	wantTemplate := "ielts." + prefix + "." + kind + ".v1"
	for _, finding := range findings {
		if finding.TemplateID != wantTemplate || len(finding.Evidence) == 0 ||
			len(finding.Evidence) > 4 ||
			(kind == "strength" && finding.Suggestion != "") {
			return false
		}
		for _, anchor := range finding.Evidence {
			transcript, ok := transcripts[anchor.EvidenceRefID]
			if !ok || strings.TrimSpace(anchor.Quote) == "" ||
				anchor.Occurrence < 1 ||
				nthLiveIELTSOccurrence(
					transcript,
					anchor.Quote,
					anchor.Occurrence,
				) < 0 {
				return false
			}
		}
	}
	return true
}

func nthLiveIELTSOccurrence(text string, quote string, occurrence int) int {
	start := 0
	for current := 1; current <= occurrence; current++ {
		relative := strings.Index(text[start:], quote)
		if relative < 0 {
			return -1
		}
		start += relative
		if current == occurrence {
			return start
		}
		start += len(quote)
	}
	return -1
}
