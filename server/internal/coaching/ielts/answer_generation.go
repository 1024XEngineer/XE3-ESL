package ielts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrAnswerGenerationInvalid     = errors.New("ielts: invalid answer generation request")
	ErrAnswerGenerationNotFound    = errors.New("ielts: answer question not found")
	ErrAnswerGenerationUnavailable = errors.New("ielts: answer generation unavailable")
)

type AnswerGenerationRequest struct {
	Question       QuestionReference `json:"question"`
	PersonalPoints []string          `json:"personal_points"`
	TargetBand     float64           `json:"target_band"`
}

type AnswerGenerationInput struct {
	Part           PracticeMode
	Question       string
	PersonalPoints []string
	TargetBand     float64
}

type AnswerGenerationResult struct {
	RequestID         string   `json:"-"`
	Provider          string   `json:"-"`
	Model             string   `json:"-"`
	Answer            string   `json:"answer"`
	Outline           []string `json:"outline"`
	UsefulExpressions []string `json:"useful_expressions"`
	SpeechText        string   `json:"speech_text"`
}

type GeneratedAnswer struct {
	Question          ResolvedQuestion `json:"question"`
	Answer            string           `json:"answer"`
	Outline           []string         `json:"outline"`
	UsefulExpressions []string         `json:"useful_expressions"`
	SpeechText        string           `json:"speech_text"`
}

type AnswerGenerator interface {
	GenerateIELTSAnswer(context.Context, AnswerGenerationInput) (AnswerGenerationResult, error)
}

type AnswerGenerationService struct {
	questions QuestionResolver
	generator AnswerGenerator
}

func NewAnswerGenerationService(questions QuestionResolver, generator AnswerGenerator) (*AnswerGenerationService, error) {
	if questions == nil || generator == nil {
		return nil, ErrAnswerGenerationInvalid
	}
	return &AnswerGenerationService{questions: questions, generator: generator}, nil
}

func (service *AnswerGenerationService) Generate(ctx context.Context, actor requestcontext.Actor, request AnswerGenerationRequest) (GeneratedAnswer, error) {
	if ctx == nil || !actor.Valid() || !validAnswerGenerationRequest(request) {
		return GeneratedAnswer{}, ErrAnswerGenerationInvalid
	}
	question, err := service.questions.ResolveQuestion(ctx, request.Question)
	if err != nil {
		if errors.Is(err, ErrQuestionSetNotFound) {
			return GeneratedAnswer{}, ErrAnswerGenerationNotFound
		}
		return GeneratedAnswer{}, err
	}
	points := normalizeAnswerStrings(request.PersonalPoints)
	result, err := service.generator.GenerateIELTSAnswer(ctx, AnswerGenerationInput{
		Part: request.Question.Part, Question: question.Prompt,
		PersonalPoints: points, TargetBand: request.TargetBand,
	})
	if err != nil || !validAnswerGenerationResult(request.Question.Part, result) {
		return GeneratedAnswer{}, ErrAnswerGenerationUnavailable
	}
	result.Answer = strings.TrimSpace(result.Answer)
	result.SpeechText = strings.TrimSpace(result.SpeechText)
	result.Outline = normalizeAnswerStrings(result.Outline)
	result.UsefulExpressions = normalizeAnswerStrings(result.UsefulExpressions)
	return GeneratedAnswer{
		Question: question, Answer: result.Answer, Outline: result.Outline,
		UsefulExpressions: result.UsefulExpressions, SpeechText: result.SpeechText,
	}, nil
}

func validAnswerGenerationRequest(request AnswerGenerationRequest) bool {
	if !validQuestionReference(request.Question) || request.TargetBand < 4 ||
		request.TargetBand > 9 || request.TargetBand*2 != float64(int(request.TargetBand*2)) ||
		len(request.PersonalPoints) > 12 {
		return false
	}
	for _, point := range request.PersonalPoints {
		trimmed := strings.TrimSpace(point)
		if trimmed == "" || !utf8.ValidString(trimmed) || utf8.RuneCountInString(trimmed) > 500 {
			return false
		}
	}
	return true
}

func validAnswerGenerationResult(part PracticeMode, result AnswerGenerationResult) bool {
	maximumWords := map[PracticeMode]int{
		PracticeModePart1: 75, PracticeModePart2: 240, PracticeModePart3: 95,
	}[part]
	return maximumWords > 0 && catalogResourceIDPattern.MatchString(result.RequestID) &&
		catalogResourceIDPattern.MatchString(result.Provider) && modelid.Valid(result.Model) &&
		validAnswerText(result.Answer, 6000) && validAnswerText(result.SpeechText, 6000) &&
		len(strings.Fields(result.Answer)) <= maximumWords &&
		len(strings.Fields(result.SpeechText)) <= maximumWords &&
		validAnswerList(result.Outline, 12, 500) &&
		validAnswerList(result.UsefulExpressions, 16, 300)
}

func validAnswerText(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func validAnswerList(values []string, maximumItems, maximumRunes int) bool {
	if len(values) == 0 || len(values) > maximumItems {
		return false
	}
	for _, value := range values {
		if !validAnswerText(value, maximumRunes) {
			return false
		}
	}
	return true
}

func normalizeAnswerStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimSpace(value)
	}
	return result
}

func AnswerGenerationSystemPrompt() string {
	return `Return one JSON object with exactly answer, outline, useful_expressions, and speech_text. When personal_points is non-empty, personalize only from those learner-supplied facts and never invent biography, names, places, achievements, or opinions. When personal_points is empty, produce a clearly generic sample answer without specific biographical claims. Produce natural conversational English suitable for the requested IELTS band, not a written essay. Use target_band to control linguistic complexity, not answer length. Do not restate the question and do not return markdown.`
}

func AnswerGenerationUserPrompt(request AnswerGenerationInput) string {
	timing := answerTiming(request.Part)
	payload, _ := json.Marshal(struct {
		Part           PracticeMode `json:"part"`
		Question       string       `json:"question"`
		PersonalPoints []string     `json:"personal_points"`
		TargetBand     float64      `json:"target_band"`
		TargetSeconds  string       `json:"target_seconds"`
		TargetWords    string       `json:"target_words"`
		Structure      string       `json:"structure"`
	}{request.Part, request.Question, request.PersonalPoints, request.TargetBand, timing.seconds, timing.words, timing.structure})
	return fmt.Sprintf("Create one IELTS Speaking practice answer that fits the timing. Keep answer and speech_text within target_words.\n%s", payload)
}

func answerTiming(part PracticeMode) struct{ seconds, words, structure string } {
	switch part {
	case PracticeModePart1:
		return struct{ seconds, words, structure string }{"20-30", "35-55", "direct answer, reason, brief example"}
	case PracticeModePart2:
		return struct{ seconds, words, structure string }{"80-110", "160-220", "one coherent long turn covering every cue-card point"}
	case PracticeModePart3:
		return struct{ seconds, words, structure string }{"25-40", "50-80", "direct position, connected reasons, relevant example or comparison"}
	default:
		return struct{ seconds, words, structure string }{}
	}
}
