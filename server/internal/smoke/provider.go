package smoke

import (
	"fmt"
	"strings"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

// MockProvider is the explicit external-capability boundary exercised by the
// offline smoke flow. A production adapter can replace question generation and
// review without taking ownership of Session, Turn, retry, or history state.
type MockProvider interface {
	conversation.QuestionProvider
	review.Provider
	FailureController
	FailureGate
}

// FailureController is a smoke-transport test hook. It intentionally remains
// outside the formal Conversation service contract.
type FailureController interface {
	ArmFailure(questionID string, answerText string)
}

type FailureGate interface {
	CheckFailure(questionID string, answerText string) error
}

type FailureControl interface {
	FailureController
	FailureGate
}

// DeterministicProvider is an offline, clock-free implementation. All
// timestamps and resource identities are supplied by the application runtime,
// so equal inputs produce equal outputs.
type DeterministicProvider struct {
	mu            sync.Mutex
	armedIntents  map[string]struct{}
	failedIntents map[string]struct{}
}

func NewDeterministicProvider() *DeterministicProvider {
	return &DeterministicProvider{
		armedIntents:  make(map[string]struct{}),
		failedIntents: make(map[string]struct{}),
	}
}

func (p *DeterministicProvider) ArmFailure(questionID string, answerText string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.armedIntents[questionID+"\x00"+strings.TrimSpace(answerText)] = struct{}{}
}

func (p *DeterministicProvider) CheckFailure(questionID string, answerText string) error {
	intent := questionID + "\x00" + strings.TrimSpace(answerText)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, armed := p.armedIntents[intent]
	_, failed := p.failedIntents[intent]
	if armed && !failed {
		p.failedIntents[intent] = struct{}{}
		return ErrRecoverableFailure
	}
	return nil
}

func (p *DeterministicProvider) BuildQuestion(
	sequence int,
) (conversation.QuestionDraft, error) {
	objectives := []string{"introduction", "system_design", "project_depth", "collaboration"}
	contents := []string{
		"Please introduce yourself and the backend project you are most proud of.",
		"How did you design the reliability controls for that API?",
		"Which design decision was yours, and what trade-off did you make?",
		"How did you align the rollout with the teams that consumed the API?",
	}
	if sequence < 1 || sequence > len(contents) {
		return conversation.QuestionDraft{},
			fmt.Errorf("question sequence %d is outside the deterministic scenario", sequence)
	}
	questionType := "PRIMARY"
	parentID := ""
	if sequence == 3 {
		questionType = "FOLLOW_UP"
		parentID = "question_demo_002"
	}
	return conversation.QuestionDraft{
		ObjectiveID:      objectives[sequence-1],
		Type:             questionType,
		ParentQuestionID: parentID,
		Content:          contents[sequence-1],
	}, nil
}

func (p *DeterministicProvider) Evaluate(
	turn review.TurnInput,
) (review.Evaluation, error) {
	return review.Evaluation{
		Score:      80 + turn.EffectiveSequence,
		Summary:    "Deterministic review completed for the submitted answer.",
		Transcript: turn.AnswerText,
		Category:   "STRUCTURE",
		Message:    "The answer is clear and grounded in an example.",
		Suggestion: "State the trade-off and measurable outcome explicitly.",
		Evidence:   []map[string]any{{"transcript_text": turn.AnswerText}},
	}, nil
}
