package ielts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrAnswerPreparationInvalid             = errors.New("ielts: invalid answer preparation request")
	ErrAnswerPreparationNotFound            = errors.New("ielts: answer preparation not found")
	ErrAnswerPreparationConflict            = errors.New("ielts: answer preparation version conflict")
	ErrAnswerPreparationIdempotencyConflict = errors.New("ielts: answer preparation idempotency conflict")
	ErrAnswerPreparationRepository          = errors.New("ielts: answer preparation repository failure")
	ErrAnswerPreparationGeneration          = errors.New("ielts: answer preparation generation failed")
)

type AnswerPreparationStatus string

const (
	AnswerPreparationDraft      AnswerPreparationStatus = "draft"
	AnswerPreparationGenerating AnswerPreparationStatus = "generating"
	AnswerPreparationReady      AnswerPreparationStatus = "ready"
	AnswerPreparationFailed     AnswerPreparationStatus = "failed"
)

type QuestionReference struct {
	BankID           string       `json:"bank_id"`
	Part             PracticeMode `json:"part"`
	SourceID         string       `json:"source_id"`
	QuestionPosition int          `json:"question_position"`
}

type ResolvedQuestion struct {
	Reference QuestionReference `json:"reference"`
	Prompt    string            `json:"prompt"`
}

type AnswerPreparation struct {
	ID                 string                  `json:"answer_preparation_id"`
	Question           ResolvedQuestion        `json:"question"`
	PersonalPoints     []string                `json:"personal_points"`
	TargetBand         float64                 `json:"target_band"`
	Status             AnswerPreparationStatus `json:"status"`
	Answer             string                  `json:"answer,omitempty"`
	Outline            []string                `json:"outline,omitempty"`
	UsefulExpressions  []string                `json:"useful_expressions,omitempty"`
	SpeechText         string                  `json:"speech_text,omitempty"`
	FailureCode        string                  `json:"failure_code,omitempty"`
	Version            int                     `json:"version"`
	GenerationRevision int                     `json:"generation_revision"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

type CreateAnswerPreparationRequest struct {
	Question       QuestionReference `json:"question"`
	PersonalPoints []string          `json:"personal_points"`
	TargetBand     float64           `json:"target_band"`
}

type UpdateAnswerPreparationRequest struct {
	ExpectedVersion int      `json:"expected_version"`
	PersonalPoints  []string `json:"personal_points"`
	TargetBand      float64  `json:"target_band"`
}

type GenerateAnswerPreparationRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

type MutationIntent struct {
	Method      string
	Path        string
	Key         string
	Fingerprint [sha256.Size]byte
}

type CreateAnswerPreparationCommand struct {
	ID       string
	Question ResolvedQuestion
	Request  CreateAnswerPreparationRequest
	Intent   MutationIntent
}

type UpdateAnswerPreparationCommand struct {
	ID      string
	Request UpdateAnswerPreparationRequest
	Intent  MutationIntent
}

type BeginAnswerGenerationCommand struct {
	ID      string
	Request GenerateAnswerPreparationRequest
	Intent  MutationIntent
}

type CompleteAnswerGenerationCommand struct {
	ID                 string
	GeneratingVersion  int
	GenerationRevision int
	Result             AnswerGenerationResult
}

type FailAnswerGenerationCommand struct {
	ID                 string
	GeneratingVersion  int
	GenerationRevision int
	FailureCode        string
}

type DeleteAnswerPreparationCommand struct {
	ID              string
	ExpectedVersion int
	Intent          MutationIntent
}

type AnswerPreparationRepository interface {
	Create(context.Context, requestcontext.Actor, CreateAnswerPreparationCommand) (AnswerPreparation, bool, error)
	Get(context.Context, requestcontext.Actor, string) (AnswerPreparation, error)
	Update(context.Context, requestcontext.Actor, UpdateAnswerPreparationCommand) (AnswerPreparation, bool, error)
	BeginGeneration(context.Context, requestcontext.Actor, BeginAnswerGenerationCommand) (AnswerPreparation, bool, error)
	CompleteGeneration(context.Context, requestcontext.Actor, CompleteAnswerGenerationCommand) (AnswerPreparation, error)
	FailGeneration(context.Context, requestcontext.Actor, FailAnswerGenerationCommand) error
	Delete(context.Context, requestcontext.Actor, DeleteAnswerPreparationCommand) (bool, error)
}

type AnswerQuestionResolver interface {
	ResolveAnswerQuestion(context.Context, QuestionReference) (ResolvedQuestion, error)
}

type AnswerGenerationRequest struct {
	Part           PracticeMode
	Question       string
	PersonalPoints []string
	TargetBand     float64
}

type AnswerGenerationResult struct {
	RequestID         string
	Provider          string
	Model             string
	Answer            string
	Outline           []string
	UsefulExpressions []string
	SpeechText        string
}

type AnswerPreparationGenerator interface {
	GenerateAnswerPreparation(context.Context, AnswerGenerationRequest) (AnswerGenerationResult, error)
}

type AnswerPreparationIDGenerator interface {
	NewAnswerPreparationID() (string, error)
}

type AnswerPreparationService struct {
	repository AnswerPreparationRepository
	questions  AnswerQuestionResolver
	generator  AnswerPreparationGenerator
	ids        AnswerPreparationIDGenerator
}

func NewAnswerPreparationService(repository AnswerPreparationRepository, questions AnswerQuestionResolver, generator AnswerPreparationGenerator, ids AnswerPreparationIDGenerator) (*AnswerPreparationService, error) {
	if repository == nil || questions == nil || generator == nil || ids == nil {
		return nil, ErrAnswerPreparationInvalid
	}
	return &AnswerPreparationService{repository: repository, questions: questions, generator: generator, ids: ids}, nil
}

func (service *AnswerPreparationService) Create(ctx context.Context, actor requestcontext.Actor, key string, request CreateAnswerPreparationRequest) (AnswerPreparation, bool, error) {
	if ctx == nil || !actor.Valid() || !validCreateAnswerPreparation(request) {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	intent, err := answerMutationIntent("POST", "/v1/ielts-speaking/answer-preparations", key, request)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	question, err := service.questions.ResolveAnswerQuestion(ctx, request.Question)
	if err != nil {
		if errors.Is(err, ErrQuestionSetNotFound) {
			return AnswerPreparation{}, false, ErrAnswerPreparationNotFound
		}
		return AnswerPreparation{}, false, err
	}
	id, err := service.ids.NewAnswerPreparationID()
	if err != nil {
		return AnswerPreparation{}, false, ErrAnswerPreparationRepository
	}
	request.PersonalPoints = normalizeStrings(request.PersonalPoints)
	return service.repository.Create(ctx, actor, CreateAnswerPreparationCommand{ID: id, Question: question, Request: request, Intent: intent})
}

func (service *AnswerPreparationService) Get(ctx context.Context, actor requestcontext.Actor, id string) (AnswerPreparation, error) {
	if ctx == nil || !actor.Valid() || !validAnswerPreparationID(id) {
		return AnswerPreparation{}, ErrAnswerPreparationInvalid
	}
	return service.repository.Get(ctx, actor, id)
}

func (service *AnswerPreparationService) Update(ctx context.Context, actor requestcontext.Actor, id, key string, request UpdateAnswerPreparationRequest) (AnswerPreparation, bool, error) {
	if ctx == nil || !actor.Valid() || !validAnswerPreparationID(id) || !validUpdateAnswerPreparation(request) {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	request.PersonalPoints = normalizeStrings(request.PersonalPoints)
	intent, err := answerMutationIntent("PATCH", "/v1/ielts-speaking/answer-preparations/"+id, key, request)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	return service.repository.Update(ctx, actor, UpdateAnswerPreparationCommand{ID: id, Request: request, Intent: intent})
}

func (service *AnswerPreparationService) Generate(ctx context.Context, actor requestcontext.Actor, id, key string, request GenerateAnswerPreparationRequest) (AnswerPreparation, bool, error) {
	if ctx == nil || !actor.Valid() || !validAnswerPreparationID(id) || request.ExpectedVersion < 1 {
		return AnswerPreparation{}, false, ErrAnswerPreparationInvalid
	}
	intent, err := answerMutationIntent("POST", "/v1/ielts-speaking/answer-preparations/"+id+"/generations", key, request)
	if err != nil {
		return AnswerPreparation{}, false, err
	}
	preparation, replayed, err := service.repository.BeginGeneration(ctx, actor, BeginAnswerGenerationCommand{ID: id, Request: request, Intent: intent})
	if err != nil {
		return preparation, replayed, err
	}
	if replayed {
		if preparation.Status == AnswerPreparationFailed {
			return preparation, true, ErrAnswerPreparationGeneration
		}
		return preparation, true, nil
	}
	if preparation.Status != AnswerPreparationGenerating {
		return AnswerPreparation{}, false, ErrAnswerPreparationConflict
	}
	result, generationErr := service.generator.GenerateAnswerPreparation(ctx, AnswerGenerationRequest{Part: preparation.Question.Reference.Part, Question: preparation.Question.Prompt, PersonalPoints: preparation.PersonalPoints, TargetBand: preparation.TargetBand})
	if generationErr != nil || !validGenerationResult(result) || !validGenerationLength(preparation.Question.Reference.Part, result) {
		failureCode := "provider_error"
		if generationErr == nil {
			failureCode = "invalid_provider_response"
		}
		failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = service.repository.FailGeneration(failureCtx, actor, FailAnswerGenerationCommand{ID: id, GeneratingVersion: preparation.Version, GenerationRevision: preparation.GenerationRevision, FailureCode: failureCode})
		return AnswerPreparation{}, false, ErrAnswerPreparationGeneration
	}
	ready, err := service.repository.CompleteGeneration(ctx, actor, CompleteAnswerGenerationCommand{ID: id, GeneratingVersion: preparation.Version, GenerationRevision: preparation.GenerationRevision, Result: normalizeGenerationResult(result)})
	return ready, false, err
}

func (service *AnswerPreparationService) Delete(ctx context.Context, actor requestcontext.Actor, id, key string, expectedVersion int) (bool, error) {
	if ctx == nil || !actor.Valid() || !validAnswerPreparationID(id) || expectedVersion < 1 {
		return false, ErrAnswerPreparationInvalid
	}
	payload := struct {
		ExpectedVersion int `json:"expected_version"`
	}{expectedVersion}
	intent, err := answerMutationIntent("DELETE", "/v1/ielts-speaking/answer-preparations/"+id, key, payload)
	if err != nil {
		return false, err
	}
	return service.repository.Delete(ctx, actor, DeleteAnswerPreparationCommand{ID: id, ExpectedVersion: expectedVersion, Intent: intent})
}

func answerMutationIntent(method, path, key string, payload any) (MutationIntent, error) {
	if !validIdempotencyKey(key) {
		return MutationIntent{}, ErrAnswerPreparationInvalid
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return MutationIntent{}, ErrAnswerPreparationInvalid
	}
	return MutationIntent{Method: method, Path: path, Key: key, Fingerprint: sha256.Sum256(encoded)}, nil
}

func validCreateAnswerPreparation(request CreateAnswerPreparationRequest) bool {
	return validQuestionReference(request.Question) && validPersonalPoints(request.PersonalPoints) && validTargetBand(request.TargetBand)
}

func validUpdateAnswerPreparation(request UpdateAnswerPreparationRequest) bool {
	return request.ExpectedVersion >= 1 && validPersonalPoints(request.PersonalPoints) && validTargetBand(request.TargetBand)
}

func validQuestionReference(reference QuestionReference) bool {
	if !validStableID(reference.BankID) || !validStableID(reference.SourceID) || reference.QuestionPosition < 1 {
		return false
	}
	switch reference.Part {
	case PracticeModePart1, PracticeModePart3:
		return true
	case PracticeModePart2:
		return reference.QuestionPosition == 1
	default:
		return false
	}
}

func validPersonalPoints(points []string) bool {
	if len(points) > 12 {
		return false
	}
	for _, point := range points {
		trimmed := strings.TrimSpace(point)
		if trimmed == "" || !utf8.ValidString(trimmed) || utf8.RuneCountInString(trimmed) > 500 {
			return false
		}
	}
	return true
}

func validTargetBand(band float64) bool {
	return band >= 4 && band <= 9 && band*2 == float64(int(band*2))
}

func validGenerationResult(result AnswerGenerationResult) bool {
	return validGeneratedText(result.Answer, 6000) && validGeneratedText(result.SpeechText, 6000) && validGeneratedList(result.Outline, 12, 500) && validGeneratedList(result.UsefulExpressions, 16, 300)
}

func validGenerationLength(part PracticeMode, result AnswerGenerationResult) bool {
	var maximumWords int
	switch part {
	case PracticeModePart1:
		maximumWords = 75
	case PracticeModePart2:
		maximumWords = 280
	case PracticeModePart3:
		maximumWords = 130
	default:
		return false
	}
	return len(strings.Fields(result.Answer)) <= maximumWords &&
		len(strings.Fields(result.SpeechText)) <= maximumWords
}

func validGeneratedText(value string, max int) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}

func validGeneratedList(values []string, maxItems, maxRunes int) bool {
	if len(values) == 0 || len(values) > maxItems {
		return false
	}
	for _, value := range values {
		if !validGeneratedText(value, maxRunes) {
			return false
		}
	}
	return true
}

func normalizeStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimSpace(value)
	}
	return result
}

func normalizeGenerationResult(result AnswerGenerationResult) AnswerGenerationResult {
	result.Answer = strings.TrimSpace(result.Answer)
	result.SpeechText = strings.TrimSpace(result.SpeechText)
	result.Outline = normalizeStrings(result.Outline)
	result.UsefulExpressions = normalizeStrings(result.UsefulExpressions)
	return result
}

func validStableID(value string) bool {
	return value == strings.TrimSpace(value) && value != "" && utf8.ValidString(value) && len(value) <= 128
}
func validAnswerPreparationID(value string) bool {
	return strings.HasPrefix(value, "ielts_answer_") && validStableID(value)
}
func validIdempotencyKey(value string) bool {
	return value == strings.TrimSpace(value) && len(value) >= 8 && len(value) <= 128 && !strings.ContainsAny(value, " \t\r\n")
}

func GenerationSystemPrompt() string {
	return `Return one JSON object with exactly answer, outline, useful_expressions, and speech_text. When personal_points is non-empty, personalize only from those learner-supplied facts and never invent biography, names, places, achievements, or opinions. When personal_points is empty, produce a clearly generic sample answer without specific biographical claims. Produce natural spoken English suitable for the requested IELTS band. answer and speech_text are strings; outline and useful_expressions are non-empty arrays of strings. Do not return markdown.`
}

func GenerationUserPrompt(request AnswerGenerationRequest) string {
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
	return fmt.Sprintf("Create an IELTS Speaking answer preparation that fits the stated timing. Keep answer and speech_text within target_words; do not pad with repetition.\n%s", payload)
}

func answerTiming(part PracticeMode) struct {
	seconds   string
	words     string
	structure string
} {
	switch part {
	case PracticeModePart1:
		return struct{ seconds, words, structure string }{"20-30", "35-55", "2-4 sentences: direct answer, reason, brief example"}
	case PracticeModePart2:
		return struct{ seconds, words, structure string }{"90-120", "180-240", "structured long turn covering the cue-card points with a clear opening and closing"}
	case PracticeModePart3:
		return struct{ seconds, words, structure string }{"35-50", "75-110", "4-6 sentences: position, explanation, example or comparison, conclusion"}
	default:
		return struct{ seconds, words, structure string }{}
	}
}
