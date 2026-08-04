package review

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestValidateGeneratedOmitsInterviewOverallAndKeepsUTF8Anchors(
	t *testing.T,
) {
	t.Parallel()
	source := scenarioReviewSource(t, "我 chose café because it was reliable.")
	generated := interviewGeneratedReview(source)

	result, evidence, err := validateGenerated(source, generated)
	if err != nil {
		t.Fatal(err)
	}
	if result.OverallScorePresent || result.OverallScore != 0 {
		t.Fatalf("interview overall score must be absent: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["overall_score"]; exists {
		t.Fatalf("interview JSON unexpectedly contains overall_score: %s", encoded)
	}
	if len(evidence) != len(result.Conclusions) {
		t.Fatalf("evidence=%d, conclusions=%d", len(evidence), len(result.Conclusions))
	}
	if evidence[0].StartUTF8Byte == nil ||
		*evidence[0].StartUTF8Byte != len("我 ") ||
		evidence[0].EndUTF8Byte == nil ||
		*evidence[0].EndUTF8Byte != len("我 chose café") {
		t.Fatalf(
			"UTF-8 anchor=[%v,%v), want [%d,%d)",
			evidence[0].StartUTF8Byte,
			evidence[0].EndUTF8Byte,
			len("我 "),
			len("我 chose café"),
		)
	}
}

func TestValidateGeneratedPersistsAnExplicitZeroDimensionScore(
	t *testing.T,
) {
	t.Parallel()
	source := scenarioReviewSource(t, "我 chose café because it was reliable.")
	generated := interviewGeneratedReview(source)
	generated.Result.Conclusions[0].Score = 0
	generated.Result.Conclusions[0].ScorePresent = true

	result, _, err := validateGenerated(source, generated)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Conclusions []map[string]json.RawMessage `json:"conclusions"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if score, present := wire.Conclusions[0]["score"]; !present || string(score) != "0" {
		t.Fatalf("explicit zero score was not persisted: %s", encoded)
	}

	generated.Result.Conclusions[0].ScorePresent = false
	if _, _, err := validateGenerated(source, generated); !errors.Is(
		err,
		ErrInvalidReview,
	) {
		t.Fatalf("implicit zero score error=%v, want invalid Review", err)
	}
}

func TestFourScenarioPoliciesProduceIndependentFeedbackWithPreciseEvidence(
	t *testing.T,
) {
	t.Parallel()
	for _, contextType := range []EvaluationContextType{
		ContextInterviewProjectDeepDive,
		ContextIELTSSpeakingPart2,
		ContextWorkplaceProgressRisk,
		ContextDailyHotelCheckin,
	} {
		contextType := contextType
		t.Run(string(contextType), func(t *testing.T) {
			t.Parallel()
			source := scenarioReviewSource(
				t,
				"I chose the reliable option because it reduced incidents.",
			)
			source.EvaluationContext = testEvaluationContext(contextType)
			policy, err := DefaultPolicyRegistry().Resolve(
				source.EvaluationContext.SessionPolicyRef,
				PolicyScopeSession,
				contextType,
			)
			if err != nil {
				t.Fatal(err)
			}
			generated := GeneratedReview{
				Result: ReviewResult{
					SummaryEligibility: SummaryEligible,
					Summary:            "A scenario-bound review.",
					FeedbackItems: []ReviewFeedbackItem{{
						Key:     "feedback-1",
						Kind:    FeedbackImprovement,
						Message: "Quantify the result.",
					}},
					RepracticeSuggestionRefs: []string{"feedback-1"},
				},
			}
			for index, dimension := range policy.Dimensions {
				score := 60 + index*5
				key := "conclusion-" + dimension.Key
				generated.Result.Conclusions = append(
					generated.Result.Conclusions,
					ReviewConclusion{
						Key:      key,
						Category: dimension.Key,
						Score:    score,
						Message:  "Grounded conclusion.",
					},
				)
				generated.EvidenceLinks = append(
					generated.EvidenceLinks,
					scenarioEvidenceLink(key, EvidenceTargetConclusion),
				)
			}
			generated.EvidenceLinks = append(
				generated.EvidenceLinks,
				scenarioEvidenceLink("feedback-1", EvidenceTargetFeedback),
			)

			result, evidence, err := validateGenerated(source, generated)
			if err != nil {
				t.Fatal(err)
			}
			wantEligibility := SummaryEligible
			if contextType == ContextIELTSSpeakingPart2 {
				wantEligibility = SummaryProvisional
			}
			if result.OverallScorePresent ||
				result.OverallScore != 0 ||
				result.SummaryEligibility != wantEligibility ||
				len(result.FeedbackItems) != 1 ||
				len(evidence) != len(policy.Dimensions)+1 {
				t.Fatalf(
					"result=%+v evidence=%d, want eligibility=%s evidence=%d",
					result,
					len(evidence),
					wantEligibility,
					len(policy.Dimensions)+1,
				)
			}
			for _, item := range evidence {
				if item.AnchorKind != EvidenceAnchorExactQuote ||
					item.StartUTF8Byte == nil ||
					item.EndUTF8Byte == nil {
					t.Fatalf("imprecise evidence: %+v", item)
				}
			}
		})
	}
}

func TestReliableIELTSOverallIsComputedByServer(t *testing.T) {
	t.Parallel()
	policy, err := DefaultPolicyRegistry().Resolve(
		"ielts.speaking_part2.session.v1",
		PolicyScopeSession,
		ContextIELTSSpeakingPart2,
	)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{
		SummaryEligibility: SummaryEligible,
		Conclusions: []ReviewConclusion{
			{Key: "task", Category: "task_coverage_development", Score: 70},
			{Key: "coherence", Category: "coherence", Score: 70},
			{Key: "lexical", Category: "lexical_resource", Score: 60},
			{Key: "grammar", Category: "grammar_range_accuracy", Score: 70},
		},
	}
	if err := validatePolicyConclusions(&result, policy); err != nil {
		t.Fatal(err)
	}
	if !result.OverallScorePresent || result.OverallScore != 68 {
		t.Fatalf("overall=%d present=%t, want 68", result.OverallScore, result.OverallScorePresent)
	}
}

func scenarioEvidenceLink(
	targetKey string,
	targetKind EvidenceTargetKind,
) EvidenceLink {
	return EvidenceLink{
		TargetKind:    targetKind,
		TargetKey:     targetKey,
		SourceType:    SourceTypeConversationTurn,
		SourceID:      "turn-1",
		SourceVersion: "conversation-turn:evidence-v1",
		Field:         "answer_text",
		AnchorKind:    EvidenceAnchorExactQuote,
		Quote:         "reliable option",
		Occurrence:    1,
	}
}

func TestValidateGeneratedFailsClosedOnEvidenceAndRubricForgery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*GeneratedReview)
	}{
		{"unknown source", func(value *GeneratedReview) { value.EvidenceLinks[0].SourceID = "unknown" }},
		{"wrong version", func(value *GeneratedReview) { value.EvidenceLinks[0].SourceVersion = "wrong" }},
		{"missing quote", func(value *GeneratedReview) { value.EvidenceLinks[0].Quote = "invented" }},
		{"invalid occurrence", func(value *GeneratedReview) { value.EvidenceLinks[0].Occurrence = 2 }},
		{"missing conclusion evidence", func(value *GeneratedReview) { value.EvidenceLinks = value.EvidenceLinks[1:] }},
		{"unknown dimension", func(value *GeneratedReview) { value.Result.Conclusions[0].Category = "model_selected_dimension" }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := scenarioReviewSource(t, "I chose the design because it was reliable.")
			generated := interviewGeneratedReview(source)
			test.mutate(&generated)
			_, _, err := validateGenerated(source, generated)
			if err == nil {
				t.Fatal("forged generated result was accepted")
			}
		})
	}
}

func TestInsufficientEvidenceCompletesWithoutCallingGenerator(t *testing.T) {
	t.Parallel()
	source := scenarioReviewSource(t, "The")
	repository := &insufficientRepository{}
	reader := staticReviewSourceReader{source: source}
	generator := &countingReviewGenerator{}
	service := NewEnsureService(repository, reader, generator)
	service.finalizeTimeout = time.Second
	command := EnsureReviewCommand{
		Actor: Actor{
			UserID: "10000000-0000-4000-8000-000000000001",
		},
		PracticeSessionID:         source.PracticeSessionID,
		ImplementationVersion:     "qianwen-scenario-review-v2",
		SourceTurnID:              source.SourceTurnID,
		SourceTurnVersion:         source.SourceTurnVersion,
		SourceManifestFingerprint: source.ManifestFingerprint,
		EvaluationContext:         source.EvaluationContext,
	}
	result, err := service.EnsureReview(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 0 {
		t.Fatalf("generator calls=%d, want 0", generator.calls)
	}
	if result.Result == nil ||
		result.Result.SummaryEligibility != SummaryInsufficientEvidence ||
		result.Result.OverallScorePresent ||
		result.Result.OverallScore != 0 ||
		len(result.Result.InsufficientEvidenceReasons) == 0 {
		t.Fatalf("unexpected insufficient result: %#v", result.Result)
	}
}

func scenarioReviewSource(t *testing.T, answer string) ReviewSourceSnapshot {
	t.Helper()
	contextValue := testEvaluationContext(ContextInterviewProjectDeepDive)
	snapshot, err := json.Marshal(map[string]string{
		"question_id":   "question-1",
		"question_text": "What trade-off did you make?",
		"answer_text":   answer,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ReviewSourceSnapshot{
		PracticeSessionID:   "session-1",
		SessionVersion:      "practice-session:v1",
		SourceTurnID:        "turn-1",
		SourceTurnVersion:   "conversation-turn:evidence-v1",
		ManifestFingerprint: "manifest-1",
		EvaluationContext:   contextValue,
		Sources: []SourceObject{{
			SourceType:    SourceTypeConversationTurn,
			SourceID:      "turn-1",
			SourceVersion: "conversation-turn:evidence-v1",
			Checksum:      "checksum",
			Snapshot:      snapshot,
		}},
	}
}

func interviewGeneratedReview(source ReviewSourceSnapshot) GeneratedReview {
	dimensions := []struct {
		key   string
		score int
	}{
		{"relevance_structure", 80},
		{"technical_depth", 70},
		{"ownership_decisions", 90},
		{"evidence_impact", 60},
		{"language_clarity", 80},
	}
	conclusions := make([]ReviewConclusion, len(dimensions))
	links := make([]EvidenceLink, len(dimensions))
	for index, dimension := range dimensions {
		key := "conclusion-" + dimension.key
		conclusions[index] = ReviewConclusion{
			Key:      key,
			Category: dimension.key,
			Score:    dimension.score,
			Message:  "Evidence-based conclusion.",
		}
		links[index] = EvidenceLink{
			TargetKind:    EvidenceTargetConclusion,
			TargetKey:     key,
			SourceType:    SourceTypeConversationTurn,
			SourceID:      source.Sources[0].SourceID,
			SourceVersion: source.Sources[0].SourceVersion,
			Field:         "answer_text",
			AnchorKind:    EvidenceAnchorExactQuote,
			Quote:         "chose café",
			Occurrence:    1,
		}
	}
	return GeneratedReview{
		Result: ReviewResult{
			SummaryEligibility:  SummaryEligible,
			OverallScore:        99,
			OverallScorePresent: true,
			Summary:             "A policy-bound review.",
			Conclusions:         conclusions,
		},
		EvidenceLinks: links,
	}
}

type staticReviewSourceReader struct {
	source ReviewSourceSnapshot
}

func (r staticReviewSourceReader) ReadReviewSource(
	context.Context,
	Actor,
	string,
) (ReviewSourceSnapshot, error) {
	return r.source, nil
}

type countingReviewGenerator struct {
	calls int
}

func (g *countingReviewGenerator) GenerateReview(
	context.Context,
	ReviewGenerationInput,
) (GeneratedReview, error) {
	g.calls++
	return GeneratedReview{}, errors.New("must not be called")
}

type insufficientRepository struct {
	result ReviewResult
}

func (r *insufficientRepository) EnsurePending(
	_ context.Context,
	command EnsureReviewCommand,
) (FormalReview, error) {
	return FormalReview{
		ID:                        "20000000-0000-4000-8000-000000000002",
		OwnerUserID:               command.Actor.UserID,
		PracticeSessionID:         command.PracticeSessionID,
		ImplementationVersion:     command.ImplementationVersion,
		SourceTurnID:              command.SourceTurnID,
		SourceTurnVersion:         command.SourceTurnVersion,
		SourceManifestFingerprint: command.SourceManifestFingerprint,
		EvaluationContext:         command.EvaluationContext,
		Status:                    FormalReviewPending,
	}, nil
}

func (r *insufficientRepository) ClaimGeneration(
	_ context.Context,
	actor Actor,
	reviewID string,
	_ time.Duration,
) (FormalReview, GenerationJobContext, bool, error) {
	return FormalReview{ID: reviewID, Status: FormalReviewGenerating},
		GenerationJobContext{
			AttemptID:   "attempt-1",
			ReviewID:    reviewID,
			OwnerUserID: actor.UserID,
			WorkerToken: "worker-1",
			LeaseUntil:  time.Now().Add(time.Minute),
		},
		true,
		nil
}

func (r *insufficientRepository) CompleteGeneration(
	_ context.Context,
	job GenerationJobContext,
	result ReviewResult,
	evidence []ReviewEvidence,
) (FormalReview, error) {
	if err := validateCompletionPayload(result, evidence); err != nil {
		return FormalReview{}, err
	}
	r.result = result
	return FormalReview{
		ID:          job.ReviewID,
		OwnerUserID: job.OwnerUserID,
		Status:      FormalReviewCompleted,
		Result:      &r.result,
	}, nil
}

func (*insufficientRepository) FailGeneration(
	context.Context,
	GenerationJobContext,
	string,
) error {
	return errors.New("unexpected failure")
}

func (*insufficientRepository) Get(
	context.Context,
	Actor,
	string,
) (FormalReview, error) {
	return FormalReview{}, ErrReviewNotFound
}

func (*insufficientRepository) List(
	context.Context,
	Actor,
) ([]FormalReview, error) {
	return nil, nil
}

func (*insufficientRepository) ListAttempts(
	context.Context,
	Actor,
	string,
) ([]GenerationAttempt, error) {
	return nil, nil
}

func (*insufficientRepository) DeleteUserData(
	context.Context,
	DeleteUserReviewsCommand,
) error {
	return nil
}
