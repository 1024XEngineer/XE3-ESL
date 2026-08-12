package qiniu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
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
	provider, err := scoring.NewIELTSSpeakingShadowProvider(
		generator,
		scoring.MaxGenerationTimeout,
	)
	if err != nil {
		t.Fatalf("create IELTS criterion provider: %v", err)
	}
	criteria := [...]scoring.IELTSCriterion{
		scoring.IELTSCriterionFC,
		scoring.IELTSCriterionLR,
		scoring.IELTSCriterionGRA,
	}
	for index, criterion := range criteria {
		if index > 0 {
			time.Sleep(liveQiniuContractRequestInterval)
		}
		request, transcripts := liveIELTSEvaluationEvidence(
			t,
			criterion,
		)
		result, err := provider.AnalyzeIELTSCriterion(
			context.Background(),
			request,
		)
		if err != nil {
			t.Fatalf("live Qiniu IELTS %s failed: %v", criterion, err)
		}
		if result.Provider != TextProviderName ||
			result.Model != configuration.EvaluationModel ||
			strings.TrimSpace(result.RequestID) == "" {
			t.Fatalf(
				"invalid IELTS %s lineage: provider=%q model=%q request_id_present=%t",
				criterion,
				result.Provider,
				result.Model,
				strings.TrimSpace(result.RequestID) != "",
			)
		}
		if err := validateLiveIELTSEvaluationPayload(
			result.Payload,
			criterion,
			transcripts,
		); err != nil {
			t.Fatalf("live Qiniu IELTS %s contract rejected: %v", criterion, err)
		}
		t.Logf(
			"Qiniu IELTS criterion=%s model=%s passed strict sanitized contract",
			criterion,
			configuration.EvaluationModel,
		)
	}
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

type liveIELTSQuestion struct {
	part       scoring.IELTSPart
	question   string
	transcript string
}

type liveIELTSFixtureGenerator struct{}

func (liveIELTSFixtureGenerator) Generate(
	_ context.Context,
	request scoring.TextGenerationRequest,
) (scoring.TextGenerationResult, error) {
	if request.OutputContract !=
		scoring.TextGenerationOutputIELTSSpeakingCriterionV3 ||
		request.OutputCriterion == "" {
		return scoring.TextGenerationResult{},
			errors.New("invalid live IELTS output contract")
	}
	return scoring.TextGenerationResult{
		RequestID: "fixture-request",
		Content:   `{}`,
		Provider:  TextProviderName,
		Model:     "fixture-model",
	}, nil
}

var liveIELTSQuestions = [...]liveIELTSQuestion{
	{
		part:       scoring.IELTSPart1,
		question:   "What kind of music do you enjoy?",
		transcript: "I think calm jazz helps me focus, and I think it also makes busy evenings feel less stressful.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "What do you like about your hometown?",
		transcript: "I think my hometown is welcoming because the parks are lively and neighbors often greet each other.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "What is the weather usually like there?",
		transcript: "The summers are warm and humid, while the winters are short, dry, and generally comfortable.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "Do you work or study?",
		transcript: "I work as a designer, so I regularly discuss ideas with colleagues and explain choices to clients.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "What is your usual morning routine?",
		transcript: "I prepare breakfast, review my schedule, and walk for twenty minutes before I begin work.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "How often do you meet your friends?",
		transcript: "We usually meet on weekends, and we either try a new restaurant or take a long walk together.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "Do you enjoy reading?",
		transcript: "Yes, I enjoy biographies because they show how people respond to difficult choices and unexpected change.",
	},
	{
		part:       scoring.IELTSPart1,
		question:   "Would you like to live somewhere else in the future?",
		transcript: "I may live near the coast for a few years because I would like a slower daily rhythm.",
	},
	{
		part:       scoring.IELTSPart2,
		question:   "Describe a useful skill you learned.",
		transcript: "I learned to cook at university. At first I followed simple recipes, but regular practice made me confident enough to plan healthy meals for friends.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "Why do adults continue learning?",
		transcript: "Adults continue learning because industries change quickly, and gaining a new skill can protect both confidence and employment.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "Should schools teach more practical skills?",
		transcript: "Schools should teach practical skills alongside academic subjects so students can connect theory with everyday decisions.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "How has technology changed learning?",
		transcript: "Technology gives learners immediate access to demonstrations and feedback, although they still need discipline to practice consistently.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "Is it ever too late to learn a skill?",
		transcript: "It is rarely too late because adults can use experience to set realistic goals and recognize useful progress.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "Should employers support employee learning?",
		transcript: "Employers benefit when they provide focused training because capable workers adapt faster and share knowledge with their teams.",
	},
	{
		part:       scoring.IELTSPart3,
		question:   "Why do some people stop learning after school?",
		transcript: "Some people lack time or clear goals, so learning feels optional even when it could improve their opportunities.",
	},
}

func liveIELTSEvaluationEvidence(
	t testing.TB,
	criterion scoring.IELTSCriterion,
) (scoring.IELTSSpeakingCriterionProviderRequest, map[string]string) {
	t.Helper()
	questions := make(
		[]scoring.IELTSSpeakingProviderQuestion,
		0,
		len(liveIELTSQuestions),
	)
	transcripts := make(map[string]string, len(liveIELTSQuestions))
	for index, fixture := range liveIELTSQuestions {
		questionID := fmt.Sprintf("question-%02d", index+1)
		turnID := fmt.Sprintf("turn-%02d", index+1)
		refID := fmt.Sprintf("evidence-%02d", index+1)
		transcripts[refID] = fixture.transcript
		questions = append(questions, scoring.IELTSSpeakingProviderQuestion{
			QuestionID:   questionID,
			PartID:       fixture.part,
			Index:        index + 1,
			QuestionText: fixture.question,
			Response: &scoring.IELTSSpeakingProviderResponse{
				TurnID:              turnID,
				EvidenceRefID:       refID,
				Transcript:          fixture.transcript,
				RecordingDurationMS: int64(8000 + index*500),
				EnglishWordCount:    len(strings.Fields(fixture.transcript)),
				LanguageEvidence:    "ENGLISH",
			},
		})
	}
	input := scoring.IELTSSpeakingShadowProviderInput{
		SchemaVersion:      scoring.IELTSSpeakingShadowProviderSchemaVersion,
		PromptVersion:      scoring.IELTSSpeakingShadowPromptVersion,
		RubricVersion:      scoring.IELTSSpeakingShadowRubricVersion,
		SceneType:          evaluation.SceneIELTSSpeaking,
		PracticeMode:       "FULL_MOCK",
		RubricDescriptors:  liveIELTSRubricDescriptors(criterion),
		AssessableCriteria: []scoring.IELTSCriterion{criterion},
		Questions:          questions,
	}
	return scoring.IELTSSpeakingCriterionProviderRequest{Input: input},
		transcripts
}

func liveIELTSRubricDescriptors(
	criterion scoring.IELTSCriterion,
) []scoring.IELTSRubricDescriptorSet {
	if criterion == scoring.IELTSCriterionFC {
		return []scoring.IELTSRubricDescriptorSet{}
	}
	return []scoring.IELTSRubricDescriptorSet{{
		CriterionID: criterion,
		Descriptors: scoring.IELTSRubricDescriptors(criterion),
	}}
}

func TestLiveQiniuIELTSEvaluationFixtureUsesSingleCriterionV3(
	t *testing.T,
) {
	t.Parallel()
	provider, err := scoring.NewIELTSSpeakingShadowProvider(
		liveIELTSFixtureGenerator{},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	criteria := [...]scoring.IELTSCriterion{
		scoring.IELTSCriterionFC,
		scoring.IELTSCriterionLR,
		scoring.IELTSCriterionGRA,
	}
	for _, criterion := range criteria {
		request, transcripts := liveIELTSEvaluationEvidence(t, criterion)
		if _, err := provider.AnalyzeIELTSCriterion(
			context.Background(),
			request,
		); err != nil {
			t.Fatalf("production criterion fixture %s: %v", criterion, err)
		}
		if request.Input.SchemaVersion !=
			scoring.IELTSSpeakingShadowProviderSchemaVersion ||
			request.Input.PromptVersion !=
				scoring.IELTSSpeakingShadowPromptVersion ||
			len(request.Input.AssessableCriteria) != 1 ||
			request.Input.AssessableCriteria[0] != criterion ||
			len(request.Input.Questions) != 15 || len(transcripts) != 15 {
			t.Fatalf("live IELTS %s fixture = %#v", criterion, request)
		}
		parts := map[scoring.IELTSPart]int{}
		for _, question := range request.Input.Questions {
			parts[question.PartID]++
		}
		if parts[scoring.IELTSPart1] != 8 ||
			parts[scoring.IELTSPart2] != 1 ||
			parts[scoring.IELTSPart3] != 6 ||
			strings.Count(
				request.Input.Questions[0].Response.Transcript,
				"I think",
			) != 2 {
			t.Fatalf("live IELTS %s fixture coverage = %#v", criterion, parts)
		}
	}
}

func validateLiveIELTSEvaluationPayload(
	raw []byte,
	target scoring.IELTSCriterion,
	transcripts map[string]string,
) error {
	var payload liveIELTSEvaluationPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	if payload.SchemaVersion != scoring.IELTSSpeakingShadowProviderSchemaVersion ||
		len(payload.Criteria) != 1 {
		return errLiveIELTSEvaluationSchema
	}
	criterion := payload.Criteria[0]
	if criterion.CriterionID != target ||
		!validLiveIELTSRubricDescriptor(
			target,
			criterion.RubricDescriptor,
		) ||
		!validLiveIELTSFindings(target, "strength", criterion.Strengths, transcripts) ||
		!validLiveIELTSFindings(target, "improvement", criterion.Improvements, transcripts) ||
		!validLiveIELTSFindings(target, "upgrade", criterion.UpgradeExamples, transcripts) ||
		len(criterion.Strengths)+len(criterion.Improvements) == 0 {
		return errLiveIELTSEvaluationSchema
	}
	return nil
}

func validLiveIELTSRubricDescriptor(
	criterion scoring.IELTSCriterion,
	descriptor string,
) bool {
	switch criterion {
	case scoring.IELTSCriterionFC:
		if descriptor == "" {
			return true
		}
	}
	for _, candidate := range scoring.IELTSRubricDescriptors(criterion) {
		if descriptor == candidate.ID {
			return true
		}
	}
	return false
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
