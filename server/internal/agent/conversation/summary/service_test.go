package summary

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const validSummaryJSON = `{"goals":["Prepare for an English interview"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":["Practice a STAR answer"]}`

func TestServiceGeneratesFirstCheckpointFromDeterministicPayload(t *testing.T) {
	repository := &repositoryStub{
		latestErr: conversation.ErrNotFound,
		messages: []conversation.Message{
			sourceMessageFixture(1, conversation.MessageRoleUser, "Help me prepare."),
			sourceMessageFixture(2, conversation.MessageRoleAssistant, "Which interview?"),
		},
	}
	generator := &recordingGenerator{result: summaryResult(validSummaryJSON)}
	service := newTestService(t, repository, generator)

	checkpoint, err := service.GenerateCheckpoint(
		context.Background(),
		testGenerateCommand(2),
	)
	if err != nil {
		t.Fatalf("generate first checkpoint: %v", err)
	}
	if !checkpoint.Valid() {
		t.Fatalf("invalid checkpoint: %#v", checkpoint)
	}
	if repository.findMaxSequence != int64(^uint64(0)>>1) {
		t.Fatalf("latest lookup max sequence = %d", repository.findMaxSequence)
	}
	if repository.listFrom != 1 || repository.listThrough != 2 {
		t.Fatalf(
			"source range = %d..%d, want 1..2",
			repository.listFrom,
			repository.listThrough,
		)
	}
	if repository.created.PreviousCheckpointID != "" ||
		repository.created.SourceFromSequence != 1 ||
		repository.created.CoveredThroughSequence != 2 {
		t.Fatalf("unexpected create command: %#v", repository.created)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("generator requests = %d, want 1", len(generator.requests))
	}
	request := generator.requests[0]
	if request.SystemPrompt == "" || request.UserPrompt == "" {
		t.Fatalf("unexpected generation request: %#v", request)
	}
	if strings.Contains(request.SystemPrompt, "Help me prepare.") {
		t.Fatal("source content leaked into trusted system prompt")
	}
	expectedChecksum := sha256.Sum256(
		[]byte(request.UserPrompt),
	)
	if repository.created.SourceChecksum != expectedChecksum {
		t.Fatal("source checksum does not cover the exact user payload")
	}
	expectedPayload := `{"previous_summary":null,"messages":[{"sequence":1,"role":"user","modality":"text","content":"Help me prepare."},{"sequence":2,"role":"assistant","modality":"text","content":"Which interview?"}]}`
	if request.UserPrompt != expectedPayload {
		t.Fatalf(
			"payload = %s, want %s",
			request.UserPrompt,
			expectedPayload,
		)
	}
}

func TestServiceRollsForwardFromLatestCheckpoint(t *testing.T) {
	previous := checkpointFixture(2)
	repository := &repositoryStub{
		latest: previous,
		messages: []conversation.Message{
			sourceMessageFixture(3, conversation.MessageRoleUser, "A product interview."),
			sourceMessageFixture(4, conversation.MessageRoleAssistant, "Let's use STAR."),
		},
	}
	generator := &recordingGenerator{result: summaryResult(validSummaryJSON)}
	service := newTestService(t, repository, generator)

	checkpoint, err := service.GenerateCheckpoint(
		context.Background(),
		testGenerateCommand(4),
	)
	if err != nil {
		t.Fatalf("generate continuation checkpoint: %v", err)
	}
	if checkpoint.PreviousCheckpointID != previous.ID ||
		checkpoint.SourceFromSequence != 3 ||
		checkpoint.CoveredThroughSequence != 4 {
		t.Fatalf("unexpected continuation: %#v", checkpoint)
	}
	if repository.created.PreviousCheckpointID != previous.ID {
		t.Fatalf(
			"previous checkpoint = %q, want %q",
			repository.created.PreviousCheckpointID,
			previous.ID,
		)
	}
	payload := generator.requests[0].UserPrompt
	if !strings.Contains(
		payload,
		`"covered_through_sequence":2`,
	) || !strings.Contains(
		payload,
		`"sequence":3`,
	) || strings.Contains(payload, previous.ID) {
		t.Fatalf("unexpected rolling payload: %s", payload)
	}
}

func TestServiceRejectsCompletedOrOversizedTargetBeforeGeneration(
	t *testing.T,
) {
	previous := checkpointFixture(2)
	tests := map[string]GenerateCheckpointCommand{
		"already covered": testGenerateCommand(2),
		"too many source messages": testGenerateCommand(
			2 + MaxSourceMessages + 1,
		),
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &repositoryStub{latest: previous}
			generator := &recordingGenerator{
				result: summaryResult(validSummaryJSON),
			}
			service := newTestService(t, repository, generator)
			_, err := service.GenerateCheckpoint(
				context.Background(),
				command,
			)
			if err == nil {
				t.Fatal("invalid target error = nil")
			}
			if len(generator.requests) != 0 {
				t.Fatal("provider called for invalid target")
			}
			if repository.createCalls != 0 {
				t.Fatal("checkpoint created for invalid target")
			}
		})
	}
}

func TestServiceDoesNotPersistFailedGeneration(t *testing.T) {
	providerFailure := errors.New("provider unavailable")
	tests := map[string]*recordingGenerator{
		"provider error": {
			err: providerFailure,
		},
		"provider mismatch": {
			result: GenerationResult{
				Provider: "other",
				Model:    "qwen-plus",
				Content:  validSummaryJSON,
			},
		},
		"model mismatch": {
			result: GenerationResult{
				Provider: "qianwen",
				Model:    "other-model",
				Content:  validSummaryJSON,
			},
		},
		"invalid response": {
			result: summaryResult("```json\n" + validSummaryJSON + "\n```"),
		},
	}
	for name, generator := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &repositoryStub{
				latestErr: conversation.ErrNotFound,
				messages: []conversation.Message{
					sourceMessageFixture(
						1,
						conversation.MessageRoleUser,
						"Hello",
					),
				},
			}
			service := newTestService(t, repository, generator)
			_, err := service.GenerateCheckpoint(
				context.Background(),
				testGenerateCommand(1),
			)
			if err == nil {
				t.Fatal("generation error = nil")
			}
			if repository.createCalls != 0 {
				t.Fatal("checkpoint persisted after generation failure")
			}
		})
	}
}

func TestServicePropagatesRepositoryFailures(t *testing.T) {
	tests := []struct {
		name              string
		repository        *repositoryStub
		wantGeneratorCall bool
	}{
		{
			name: "latest lookup",
			repository: &repositoryStub{
				latestErr: conversation.ErrRepository,
			},
		},
		{
			name: "source read",
			repository: &repositoryStub{
				latestErr: conversation.ErrNotFound,
				listErr:   conversation.ErrRepository,
			},
		},
		{
			name: "checkpoint create",
			repository: &repositoryStub{
				latestErr: conversation.ErrNotFound,
				messages: []conversation.Message{
					sourceMessageFixture(
						1,
						conversation.MessageRoleUser,
						"Hello",
					),
				},
				createErr: conversation.ErrConflict,
			},
			wantGeneratorCall: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := &recordingGenerator{
				result: summaryResult(validSummaryJSON),
			}
			service := newTestService(t, test.repository, generator)
			_, err := service.GenerateCheckpoint(
				context.Background(),
				testGenerateCommand(1),
			)
			if err == nil {
				t.Fatal("repository error = nil")
			}
			if (len(generator.requests) == 1) !=
				test.wantGeneratorCall {
				t.Fatalf(
					"generator calls = %d, want called=%t",
					len(generator.requests),
					test.wantGeneratorCall,
				)
			}
		})
	}
}

func TestDecodeSummaryContentIsStrict(t *testing.T) {
	if _, err := decodeSummaryContent(validSummaryJSON); err != nil {
		t.Fatalf("decode valid summary: %v", err)
	}
	tests := map[string]string{
		"markdown":         "```json\n" + validSummaryJSON + "\n```",
		"outer whitespace": " " + validSummaryJSON,
		"unknown field":    `{"goals":["g"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[],"extra":[]}`,
		"duplicate field":  `{"goals":["g"],"goals":["other"],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
		"missing field":    `{"goals":["g"],"background":[],"progress":[],"decisions":[],"open_questions":[]}`,
		"nil array":        `{"goals":["g"],"background":null,"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
		"wrong type":       `{"goals":"g","background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
		"empty summary":    `{"goals":[],"background":[],"progress":[],"decisions":[],"open_questions":[],"next_steps":[]}`,
		"trailing json":    validSummaryJSON + `{}`,
		"oversized response": strings.Repeat(
			"x",
			maxSummaryResponseBytes+1,
		),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSummaryContent(content); !errors.Is(
				err,
				ErrInvalidResponse,
			) {
				t.Fatalf("decode error = %v, want invalid response", err)
			}
		})
	}
}

func TestGenerationPayloadIsStableAndOmitsInternalIdentity(t *testing.T) {
	messages := []conversation.Message{
		sourceMessageFixture(1, conversation.MessageRoleUser, "Hello"),
	}
	first, err := encodeGenerationPayload(
		Checkpoint{},
		false,
		messages,
	)
	if err != nil {
		t.Fatalf("encode first payload: %v", err)
	}
	messages[0].ID = "90000000-0000-4000-8000-000000000099"
	messages[0].OwnerID = "90000000-0000-4000-8000-000000000098"
	messages[0].ThreadID = "90000000-0000-4000-8000-000000000097"
	second, err := encodeGenerationPayload(
		Checkpoint{},
		false,
		messages,
	)
	if err != nil {
		t.Fatalf("encode second payload: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("payload changed with internal identity:\n%s\n%s", first, second)
	}
	if strings.Contains(string(first), "90000000") {
		t.Fatal("payload contains internal identity")
	}
}

type repositoryStub struct {
	latest          Checkpoint
	latestErr       error
	messages        []conversation.Message
	listErr         error
	createErr       error
	created         CreateCheckpointCommand
	createCalls     int
	findMaxSequence int64
	listFrom        int64
	listThrough     int64
}

func (repository *repositoryStub) FindLatestCheckpoint(
	_ context.Context,
	_ string,
	_ string,
	maxSequence int64,
) (Checkpoint, error) {
	repository.findMaxSequence = maxSequence
	return repository.latest, repository.latestErr
}

func (repository *repositoryStub) ListMessagesForSummary(
	_ context.Context,
	_ string,
	_ string,
	sourceFromSequence int64,
	coveredThroughSequence int64,
) ([]conversation.Message, error) {
	repository.listFrom = sourceFromSequence
	repository.listThrough = coveredThroughSequence
	return append([]conversation.Message(nil), repository.messages...), repository.listErr
}

func (repository *repositoryStub) CreateCheckpoint(
	_ context.Context,
	command CreateCheckpointCommand,
) (Checkpoint, error) {
	repository.createCalls++
	repository.created = command
	if repository.createErr != nil {
		return Checkpoint{}, repository.createErr
	}
	return Checkpoint{
		ID:                     "40000000-0000-4000-8000-000000000001",
		OwnerID:                command.OwnerID,
		ThreadID:               command.ThreadID,
		PreviousCheckpointID:   command.PreviousCheckpointID,
		SourceFromSequence:     command.SourceFromSequence,
		CoveredThroughSequence: command.CoveredThroughSequence,
		Content:                command.Content,
		PolicyVersion:          command.PolicyVersion,
		PromptVersion:          command.PromptVersion,
		Provider:               command.Provider,
		Model:                  command.Model,
		SourceChecksum:         command.SourceChecksum,
		CreatedAt:              time.Now().UTC(),
	}, nil
}

type recordingGenerator struct {
	result   GenerationResult
	err      error
	requests []GenerationRequest
}

func (generator *recordingGenerator) GenerateJSON(
	_ context.Context,
	request GenerationRequest,
) (GenerationResult, error) {
	generator.requests = append(generator.requests, request)
	return generator.result, generator.err
}

func newTestService(
	t *testing.T,
	repository Repository,
	generator Generator,
) *Service {
	t.Helper()
	service, err := NewService(
		repository,
		generator,
		Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      "qianwen",
			Model:         "qwen-plus",
		},
	)
	if err != nil {
		t.Fatalf("new summary service: %v", err)
	}
	return service
}

func testGenerateCommand(through int64) GenerateCheckpointCommand {
	return GenerateCheckpointCommand{
		OwnerID:                "10000000-0000-4000-8000-000000000001",
		ThreadID:               "20000000-0000-4000-8000-000000000001",
		CoveredThroughSequence: through,
	}
}

func sourceMessageFixture(
	sequence int64,
	role conversation.MessageRole,
	content string,
) conversation.Message {
	return conversation.Message{
		ID:        "30000000-0000-4000-8000-000000000001",
		OwnerID:   "10000000-0000-4000-8000-000000000001",
		ThreadID:  "20000000-0000-4000-8000-000000000001",
		Sequence:  sequence,
		Role:      role,
		Modality:  conversation.MessageModalityText,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}

func checkpointFixture(coveredThrough int64) Checkpoint {
	return Checkpoint{
		ID:                     "50000000-0000-4000-8000-000000000001",
		OwnerID:                "10000000-0000-4000-8000-000000000001",
		ThreadID:               "20000000-0000-4000-8000-000000000001",
		SourceFromSequence:     1,
		CoveredThroughSequence: coveredThrough,
		Content: Content{
			Goals:         []string{"Prepare for an interview"},
			Background:    []string{},
			Progress:      []string{},
			Decisions:     []string{},
			OpenQuestions: []string{},
			NextSteps:     []string{},
		},
		PolicyVersion:  "summary-policy-v1",
		PromptVersion:  "summary-prompt-v1",
		Provider:       "qianwen",
		Model:          "qwen-plus",
		SourceChecksum: sha256.Sum256([]byte("previous source")),
		CreatedAt:      time.Now().UTC(),
	}
}

func summaryResult(content string) GenerationResult {
	return GenerationResult{
		Provider: "qianwen",
		Model:    "qwen-plus",
		Content:  content,
	}
}
