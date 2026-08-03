package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInterviewDimensionsReturnsCanonicalClone(t *testing.T) {
	t.Parallel()
	first := InterviewDimensions()
	if !reflect.DeepEqual(first, interviewDimensionOrder[:]) {
		t.Fatalf("dimensions = %v", first)
	}
	first[0] = "MUTATED"
	if second := InterviewDimensions(); second[0] !=
		InterviewDimensionRelevance {
		t.Fatalf("caller mutated canonical dimensions: %v", second)
	}
}

func TestInterviewShadowEngineBuildsEvidenceBoundProvisionalResult(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"我 led the migration well.",
		interviewFollowUpNone,
	)
	provider := &stubInterviewShadowProvider{}
	engine := NewInterviewShadowEngine(provider)

	result, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if result.Scoreability != InterviewScoreabilityProvisional ||
		result.Gate != InterviewGateFeedbackOnly ||
		result.Readiness != InterviewReadinessNotAssessed ||
		result.ReadinessNotice != InterviewShadowReadinessNotice ||
		result.Provider == nil ||
		len(result.Dimensions) != 5 {
		t.Fatalf("result = %#v", result)
	}
	for index, dimension := range result.Dimensions {
		if dimension.DimensionID != interviewDimensionOrder[index] ||
			dimension.Confidence != 0 {
			t.Errorf("dimension %d = %#v", index, dimension)
		}
		if dimension.DimensionID == InterviewDimensionInteraction {
			if dimension.Scoreability != InterviewScoreabilityInsufficient ||
				dimension.Gate != InterviewGateBlocked ||
				!reflect.DeepEqual(
					dimension.ReasonCodes,
					[]InterviewReasonCode{
						InterviewReasonOpportunityNotProvided,
					},
				) {
				t.Errorf("interaction dimension = %#v", dimension)
			}
			continue
		}
		if dimension.Scoreability != InterviewScoreabilityProvisional ||
			dimension.Gate != InterviewGateFeedbackOnly ||
			len(dimension.Strengths) != 1 ||
			len(dimension.EvidenceRefIDs) != 1 {
			t.Errorf("assessable dimension = %#v", dimension)
			continue
		}
		evidence := dimension.Strengths[0].Evidence[0]
		if evidence.OriginalExcerpt != "我 led the migration well." ||
			evidence.StartUTF8Byte != 0 ||
			evidence.EndUTF8Byte !=
				len("我 led the migration well.") {
			t.Errorf("server evidence = %#v", evidence)
		}
	}
	if err := ValidateInterviewShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateInterviewShadowResult() error = %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var wire any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	for _, forbidden := range []string{
		"raw",
		"display",
		"interval",
		"overall",
		"overall_score",
		"total",
		"weights",
	} {
		if jsonContainsExactKey(wire, forbidden) {
			t.Errorf("result contains forbidden key %q: %s", forbidden, encoded)
		}
	}

	inputJSON, err := json.Marshal(provider.lastInput)
	if err != nil {
		t.Fatalf("marshal Provider input: %v", err)
	}
	for _, forbidden := range []string{
		"owner_user_id",
		"resume",
		"background_snapshot",
		"job_description",
		"audio",
		"object_key",
		"signed_url",
	} {
		if strings.Contains(strings.ToLower(string(inputJSON)), forbidden) {
			t.Errorf("Provider input leaks %q: %s", forbidden, inputJSON)
		}
	}
}

func TestInterviewShadowInsufficientEvidenceSkipsProvider(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"Yes.",
		interviewFollowUpNone,
	)
	provider := &stubInterviewShadowProvider{
		err: errors.New("must not be called"),
	}
	result, err := NewInterviewShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
	if result.Scoreability != InterviewScoreabilityInsufficient ||
		result.Gate != InterviewGateBlocked ||
		result.Provider != nil ||
		len(result.Dimensions) != 5 {
		t.Fatalf("result = %#v", result)
	}
	for _, dimension := range result.Dimensions {
		if !reflect.DeepEqual(
			dimension.ReasonCodes,
			[]InterviewReasonCode{InterviewReasonInsufficientEvidence},
		) {
			t.Errorf("dimension = %#v", dimension)
		}
	}
	if err := ValidateInterviewShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateInterviewShadowResult() error = %v", err)
	}
}

func TestInterviewShadowInteractionUsesActualFollowUpOpportunity(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name          string
		followUp      interviewFollowUpMode
		wantInputDims int
		wantStatus    InterviewScoreabilityStatus
		wantReason    InterviewReasonCode
	}{
		{
			name:          "not provided",
			followUp:      interviewFollowUpNone,
			wantInputDims: 4,
			wantStatus:    InterviewScoreabilityInsufficient,
			wantReason:    InterviewReasonOpportunityNotProvided,
		},
		{
			name:          "provided but unanswered",
			followUp:      interviewFollowUpUnanswered,
			wantInputDims: 4,
			wantStatus:    InterviewScoreabilityInsufficient,
			wantReason:    InterviewReasonInsufficientEvidence,
		},
		{
			name:          "answered",
			followUp:      interviewFollowUpAnswered,
			wantInputDims: 5,
			wantStatus:    InterviewScoreabilityProvisional,
			wantReason:    InterviewReasonASRConfidenceUnavailable,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := interviewShadowTestSnapshot(
				t,
				"I led a careful migration.",
				test.followUp,
			)
			provider := &stubInterviewShadowProvider{}
			result, err := NewInterviewShadowEngine(provider).Evaluate(
				context.Background(),
				snapshot,
			)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(provider.lastInput.AssessableDimensions) !=
				test.wantInputDims {
				t.Fatalf(
					"assessable dimensions = %v, want %d",
					provider.lastInput.AssessableDimensions,
					test.wantInputDims,
				)
			}
			interaction := result.Dimensions[4]
			if interaction.Scoreability != test.wantStatus ||
				len(interaction.ReasonCodes) != 1 ||
				interaction.ReasonCodes[0] != test.wantReason {
				t.Fatalf("interaction = %#v", interaction)
			}
			if test.followUp == interviewFollowUpAnswered &&
				(interaction.Coverage != 1 ||
					len(interaction.Strengths) != 1) {
				t.Fatalf("answered interaction = %#v", interaction)
			}
			if err := ValidateInterviewShadowResult(
				snapshot,
				result,
			); err != nil {
				t.Fatalf("Validate result: %v", err)
			}
		})
	}
}

func TestInterviewShadowQuestionResultsMarkUnansweredOpportunityBlocked(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpUnanswered,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(result.QuestionResults) != 2 {
		t.Fatalf(
			"question results = %d, want 2",
			len(result.QuestionResults),
		)
	}
	unanswered := result.QuestionResults[1]
	if unanswered.QuestionID != "question-2" ||
		unanswered.QuestionType != "FOLLOW_UP" ||
		unanswered.ParentQuestionID != "question-1" ||
		unanswered.OpportunityStatus !=
			InterviewOpportunityNotProvided ||
		unanswered.ResponseTurnID != "" ||
		len(unanswered.EvidenceRefIDs) != 0 ||
		len(unanswered.DimensionResults) != len(interviewDimensionOrder) {
		t.Fatalf("unanswered question = %#v", unanswered)
	}
	for index, dimension := range unanswered.DimensionResults {
		if dimension.DimensionID != interviewDimensionOrder[index] ||
			dimension.Scoreability !=
				InterviewScoreabilityInsufficient ||
			dimension.Gate != InterviewGateBlocked ||
			dimension.Coverage != 0 ||
			dimension.Confidence != 0 ||
			!reflect.DeepEqual(
				dimension.ReasonCodes,
				[]InterviewReasonCode{
					InterviewReasonOpportunityNotProvided,
				},
			) ||
			len(dimension.EvidenceRefIDs) != 0 ||
			len(dimension.StrengthFindingIDs) != 0 ||
			len(dimension.ImprovementFindingIDs) != 0 ||
			len(dimension.RecommendedExpressionFindingIDs) != 0 {
			t.Errorf("unanswered dimension %d = %#v", index, dimension)
		}
	}
	if err := ValidateInterviewShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateInterviewShadowResult() error = %v", err)
	}
}

func TestInterviewShadowQuestionResultsBindFollowUpFindingsServerSide(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpAnswered,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(result.QuestionResults) != 2 {
		t.Fatalf(
			"question results = %d, want 2",
			len(result.QuestionResults),
		)
	}
	followUp := result.QuestionResults[1]
	interaction := followUp.DimensionResults[4]
	rootInteraction := result.Dimensions[4]
	if followUp.OpportunityStatus != InterviewOpportunityProvided ||
		followUp.ResponseTurnID != "turn-2" ||
		len(followUp.EvidenceRefIDs) != 1 ||
		interaction.DimensionID != InterviewDimensionInteraction ||
		interaction.Scoreability != InterviewScoreabilityProvisional ||
		interaction.Gate != InterviewGateFeedbackOnly ||
		interaction.Coverage != 1 ||
		!reflect.DeepEqual(
			interaction.EvidenceRefIDs,
			followUp.EvidenceRefIDs,
		) ||
		!reflect.DeepEqual(
			interaction.StrengthFindingIDs,
			[]string{rootInteraction.Strengths[0].ID},
		) {
		t.Fatalf(
			"follow-up = %#v, interaction = %#v",
			followUp,
			interaction,
		)
	}
	primaryInteraction := result.QuestionResults[0].DimensionResults[4]
	if primaryInteraction.Scoreability !=
		InterviewScoreabilityInsufficient ||
		primaryInteraction.Gate != InterviewGateBlocked ||
		primaryInteraction.Coverage != 0 ||
		!reflect.DeepEqual(
			primaryInteraction.ReasonCodes,
			[]InterviewReasonCode{
				InterviewReasonOpportunityNotProvided,
			},
		) {
		t.Fatalf("primary interaction = %#v", primaryInteraction)
	}

	tampered := cloneInterviewShadowResult(t, result)
	tampered.QuestionResults[1].DimensionResults[4].
		StrengthFindingIDs = []string{
		result.Dimensions[0].Strengths[0].ID,
	}
	if err := ValidateInterviewShadowResult(
		snapshot,
		tampered,
	); !errors.Is(err, ErrInvalidInterviewShadow) {
		t.Fatalf("forged question mapping error = %v", err)
	}
}

func TestInterviewShadowQuestionResultsPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpAnswered,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded InterviewShadowResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Fatalf("round-trip changed result:\n%#v\n%#v", result, decoded)
	}
	if err := ValidateInterviewShadowResult(snapshot, decoded); err != nil {
		t.Fatalf("Validate round-trip result: %v", err)
	}
}

func TestInterviewShadowProviderResponseIsStrictAndFailClosed(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepareInterviewShadow() error = %v", err)
	}
	valid := validInterviewProviderPayload(t, prepared.input)

	tests := []struct {
		name   string
		mutate func(map[string]any)
		raw    json.RawMessage
	}{
		{
			name: "root overall",
			mutate: func(value map[string]any) {
				value["overall"] = 80
			},
		},
		{
			name: "dimension score out of range",
			mutate: func(value map[string]any) {
				firstProviderDimension(value)["score"] = 101
			},
		},
		{
			name: "weights",
			mutate: func(value map[string]any) {
				firstProviderDimension(value)["weights"] = []any{1}
			},
		},
		{
			name: "invented dimension",
			mutate: func(value map[string]any) {
				firstProviderDimension(value)["dimension_id"] = "PERSONALITY"
			},
		},
		{
			name: "invented ref",
			mutate: func(value map[string]any) {
				firstProviderAnchor(value)["evidence_ref_id"] = "ref-invented"
			},
		},
		{
			name: "invented quote",
			mutate: func(value map[string]any) {
				firstProviderAnchor(value)["quote"] = "words never spoken"
			},
		},
		{
			name: "provider supplied original excerpt",
			mutate: func(value map[string]any) {
				firstProviderAnchor(value)["original_excerpt"] = "forged"
			},
		},
		{
			name: "duplicate dimension",
			mutate: func(value map[string]any) {
				dimensions := value["dimensions"].([]any)
				value["dimensions"] = append(dimensions, dimensions[0])
			},
		},
		{
			name: "missing dimension",
			mutate: func(value map[string]any) {
				dimensions := value["dimensions"].([]any)
				value["dimensions"] = dimensions[1:]
			},
		},
		{
			name: "missing strengths array",
			mutate: func(value map[string]any) {
				delete(firstProviderDimension(value), "strengths")
			},
		},
		{
			name: "non-string template id",
			mutate: func(value map[string]any) {
				firstProviderFinding(value)["template_id"] = 1
			},
		},
		{
			name: "trailing JSON",
			raw:  append(append(json.RawMessage(nil), valid...), []byte(`{}`)...),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw := test.raw
			if raw == nil {
				var value map[string]any
				if err := json.Unmarshal(valid, &value); err != nil {
					t.Fatalf("decode valid payload: %v", err)
				}
				test.mutate(value)
				var err error
				raw, err = json.Marshal(value)
				if err != nil {
					t.Fatalf("marshal mutation: %v", err)
				}
			}
			provider := &stubInterviewShadowProvider{payload: raw}
			_, err := NewInterviewShadowEngine(provider).Evaluate(
				context.Background(),
				snapshot,
			)
			if !errors.Is(err, ErrInvalidInterviewShadow) {
				t.Fatalf("error = %v, want invalid Interview shadow", err)
			}
		})
	}
}

func TestInterviewShadowProviderFeedbackContentIsFailClosed(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepareInterviewShadow() error = %v", err)
	}
	valid := validInterviewProviderPayload(t, prepared.input)
	tests := []string{
		"Your consonants were crisp and cadence natural.",
		"This answer earns eight points.",
		"You are likely to be hired.",
		"You are ready for the role.",
		"你的口音很标准。",
		"这段回答值八分。",
		"建议录用该候选人。",
		"你的面试准备度很高。",
	}
	for _, forbidden := range tests {
		forbidden := forbidden
		t.Run(forbidden, func(t *testing.T) {
			t.Parallel()
			var value map[string]any
			if err := json.Unmarshal(valid, &value); err != nil {
				t.Fatalf("decode valid payload: %v", err)
			}
			firstProviderFinding(value)["template_id"] = forbidden
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal mutation: %v", err)
			}
			_, err = NewInterviewShadowEngine(
				&stubInterviewShadowProvider{payload: raw},
			).Evaluate(context.Background(), snapshot)
			if !errors.Is(err, ErrInvalidInterviewShadow) {
				t.Fatalf(
					"error = %v, want invalid Interview shadow",
					err,
				)
			}
		})
	}
	t.Run("provider cannot supply free text fields", func(t *testing.T) {
		var value map[string]any
		if err := json.Unmarshal(valid, &value); err != nil {
			t.Fatalf("decode valid payload: %v", err)
		}
		firstProviderFinding(value)["message"] =
			"Your consonants were crisp and cadence natural."
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal mutation: %v", err)
		}
		_, err = NewInterviewShadowEngine(
			&stubInterviewShadowProvider{payload: raw},
		).Evaluate(context.Background(), snapshot)
		if !errors.Is(err, ErrInvalidInterviewShadow) {
			t.Fatalf(
				"error = %v, want invalid Interview shadow",
				err,
			)
		}
	})
}

func TestInterviewShadowProviderAllowsNumericBusinessEvidence(
	t *testing.T,
) {
	t.Parallel()
	transcript := "I reduced latency by 20% and documented the rollout."
	snapshot := interviewShadowTestSnapshot(
		t,
		transcript,
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	findingResult := result.Dimensions[0].Strengths[0]
	template, ok := interviewShadowFeedbackTemplate(
		InterviewDimensionRelevance,
		interviewFindingStrength,
	)
	if !ok ||
		findingResult.Message != template.Message ||
		findingResult.Suggestion != template.Suggestion ||
		len(findingResult.Evidence) != 1 ||
		findingResult.Evidence[0].OriginalExcerpt != transcript ||
		strings.Contains(findingResult.Message, "20%") {
		t.Fatalf("finding = %#v", findingResult)
	}
}

func TestInterviewShadowRejectsPersistedFreeTextTampering(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I reduced latency by 20% and documented the rollout.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	tampered := cloneInterviewShadowResult(t, result)
	finding := &tampered.Dimensions[0].Strengths[0]
	finding.Message = "This answer earns eight points."
	finding.ID = stableInterviewFindingID(
		tampered.SnapshotID,
		InterviewDimensionRelevance,
		interviewFindingStrength,
		*finding,
	)
	if err := ValidateInterviewShadowResult(
		snapshot,
		tampered,
	); !errors.Is(err, ErrInvalidInterviewShadow) {
		t.Fatalf("tampered result error = %v", err)
	}
}

func TestInterviewShadowResolvesUTF8OccurrenceOnServer(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"我 said go, then go again.",
		interviewFollowUpNone,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw := validInterviewProviderPayload(t, prepared.input)
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	anchor := firstProviderAnchor(value)
	anchor["quote"] = "go"
	anchor["occurrence"] = 2
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{payload: raw},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	evidence := result.Dimensions[0].Strengths[0].Evidence[0]
	wantStart := strings.LastIndex("我 said go, then go again.", "go")
	if evidence.StartUTF8Byte != wantStart ||
		evidence.EndUTF8Byte != wantStart+len("go") ||
		evidence.OriginalExcerpt != "go" {
		t.Fatalf("evidence = %#v, want start %d", evidence, wantStart)
	}
}

func TestInterviewShadowInteractionRejectsPrimaryOnlyEvidence(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpAnswered,
	)
	prepared, err := prepareInterviewShadow(snapshot)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw := validInterviewProviderPayload(t, prepared.input)
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	dimensions := value["dimensions"].([]any)
	interaction := dimensions[len(dimensions)-1].(map[string]any)
	anchor := interaction["strengths"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	primary := prepared.input.Opportunities[0].Response
	anchor["evidence_ref_id"] = primary.EvidenceRefID
	anchor["quote"] = primary.Transcript
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	_, err = NewInterviewShadowEngine(
		&stubInterviewShadowProvider{payload: raw},
	).Evaluate(context.Background(), snapshot)
	if !errors.Is(err, ErrInvalidInterviewShadow) {
		t.Fatalf("error = %v, want invalid Interview shadow", err)
	}
}

func TestValidateInterviewShadowResultRejectsPersistenceTampering(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	result, err := NewInterviewShadowEngine(
		&stubInterviewShadowProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*InterviewShadowResult)
	}{
		{
			name: "readiness",
			mutate: func(value *InterviewShadowResult) {
				value.Readiness = "HIGH"
			},
		},
		{
			name: "root gate",
			mutate: func(value *InterviewShadowResult) {
				value.Gate = InterviewGateBlocked
			},
		},
		{
			name: "unknown dimension",
			mutate: func(value *InterviewShadowResult) {
				value.Dimensions[0].DimensionID = "INVENTED"
			},
		},
		{
			name: "coverage",
			mutate: func(value *InterviewShadowResult) {
				value.Dimensions[0].Coverage = 0.5
			},
		},
		{
			name: "forged excerpt",
			mutate: func(value *InterviewShadowResult) {
				value.Dimensions[0].Strengths[0].
					Evidence[0].OriginalExcerpt = "forged"
			},
		},
		{
			name: "forged ref",
			mutate: func(value *InterviewShadowResult) {
				value.Dimensions[0].Strengths[0].
					Evidence[0].EvidenceRefID = "ref-invented"
			},
		},
		{
			name: "forged finding id",
			mutate: func(value *InterviewShadowResult) {
				value.Dimensions[0].Strengths[0].ID = "finding-invented"
			},
		},
		{
			name: "missing provider lineage",
			mutate: func(value *InterviewShadowResult) {
				value.Provider = nil
			},
		},
		{
			name: "question opportunity status",
			mutate: func(value *InterviewShadowResult) {
				value.QuestionResults[0].OpportunityStatus =
					InterviewOpportunityNotProvided
			},
		},
		{
			name: "question response turn",
			mutate: func(value *InterviewShadowResult) {
				value.QuestionResults[0].ResponseTurnID =
					"turn-invented"
			},
		},
		{
			name: "question evidence ref",
			mutate: func(value *InterviewShadowResult) {
				value.QuestionResults[0].EvidenceRefIDs[0] =
					"ref-invented"
			},
		},
		{
			name: "question dimension coverage",
			mutate: func(value *InterviewShadowResult) {
				value.QuestionResults[0].DimensionResults[0].
					Coverage = 0
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mutated := cloneInterviewShadowResult(t, result)
			test.mutate(&mutated)
			if err := ValidateInterviewShadowResult(
				snapshot,
				mutated,
			); !errors.Is(err, ErrInvalidInterviewShadow) {
				t.Fatalf("error = %v, want invalid Interview shadow", err)
			}
		})
	}
}

func TestInterviewShadowEngineIsDeterministicAndPropagatesProviderErrors(
	t *testing.T,
) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpAnswered,
	)
	provider := &stubInterviewShadowProvider{}
	engine := NewInterviewShadowEngine(provider)
	first, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("first Evaluate() error = %v", err)
	}
	second, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("second Evaluate() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same evidence/result differs:\n%#v\n%#v", first, second)
	}

	sentinel := errors.New("provider unavailable")
	_, err = NewInterviewShadowEngine(
		&stubInterviewShadowProvider{err: sentinel},
	).Evaluate(context.Background(), snapshot)
	if !errors.Is(err, sentinel) {
		t.Fatalf("provider error = %v", err)
	}
}

func TestInterviewShadowRejectsWrongSnapshotBoundary(t *testing.T) {
	t.Parallel()
	snapshot := interviewShadowTestSnapshot(
		t,
		"I led a careful migration.",
		interviewFollowUpNone,
	)
	tests := []EvidenceSnapshot{
		func() EvidenceSnapshot {
			value := snapshot
			value.SceneType = SceneOverseasDaily
			return value
		}(),
		func() EvidenceSnapshot {
			value := snapshot
			value.Scope = ScopeTurn
			return value
		}(),
		func() EvidenceSnapshot {
			value := snapshot
			value.SnapshotHash[0] ^= 0xff
			return value
		}(),
	}
	for _, invalid := range tests {
		if _, err := NewInterviewShadowEngine(
			&stubInterviewShadowProvider{},
		).Evaluate(
			context.Background(),
			invalid,
		); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("error = %v, want invalid request", err)
		}
	}
}

type stubInterviewShadowProvider struct {
	calls     int
	lastInput InterviewShadowProviderInput
	payload   json.RawMessage
	err       error
}

func (p *stubInterviewShadowProvider) AnalyzeInterview(
	_ context.Context,
	input InterviewShadowProviderInput,
) (InterviewShadowProviderResult, error) {
	p.calls++
	p.lastInput = input
	if p.err != nil {
		return InterviewShadowProviderResult{}, p.err
	}
	payload := p.payload
	if payload == nil {
		encoded, err := json.Marshal(validInterviewProviderPayloadValue(input))
		if err != nil {
			return InterviewShadowProviderResult{}, err
		}
		payload = encoded
	}
	return InterviewShadowProviderResult{
		Payload:   payload,
		Provider:  "qianwen",
		Model:     "qwen-plus",
		RequestID: "provider-request-1",
	}, nil
}

func validInterviewProviderPayload(
	t *testing.T,
	input InterviewShadowProviderInput,
) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(validInterviewProviderPayloadValue(input))
	if err != nil {
		t.Fatalf("marshal Provider payload: %v", err)
	}
	return encoded
}

func validInterviewProviderPayloadValue(
	input InterviewShadowProviderInput,
) interviewProviderPayload {
	var firstResponse *InterviewProviderResponse
	var followUpResponse *InterviewProviderResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil && firstResponse == nil {
			firstResponse = opportunity.Response
		}
		if opportunity.QuestionType == "FOLLOW_UP" &&
			opportunity.Response != nil {
			followUpResponse = opportunity.Response
		}
	}
	if firstResponse == nil {
		panic("valid Provider fixture requires one response")
	}
	dimensions := make(
		[]interviewProviderDimension,
		0,
		len(input.AssessableDimensions),
	)
	for _, dimension := range input.AssessableDimensions {
		response := firstResponse
		if dimension == InterviewDimensionInteraction {
			response = followUpResponse
		}
		if response == nil {
			panic("Interaction fixture requires one follow-up response")
		}
		template, ok := interviewShadowFeedbackTemplate(
			dimension,
			interviewFindingStrength,
		)
		if !ok {
			panic("valid Provider fixture requires a feedback template")
		}
		dimensions = append(dimensions, interviewProviderDimension{
			DimensionID: dimension,
			Score:       75,
			Strengths: []interviewProviderFinding{{
				TemplateID: template.ID,
				Evidence: []interviewProviderAnchor{{
					EvidenceRefID: response.EvidenceRefID,
					Quote:         response.Transcript,
					Occurrence:    1,
				}},
			}},
			Improvements:           []interviewProviderFinding{},
			RecommendedExpressions: []interviewProviderFinding{},
		})
	}
	return interviewProviderPayload{
		SchemaVersion: InterviewShadowProviderSchemaVersion,
		Dimensions:    dimensions,
	}
}

type interviewFollowUpMode int

const (
	interviewFollowUpNone interviewFollowUpMode = iota
	interviewFollowUpUnanswered
	interviewFollowUpAnswered
)

func interviewShadowTestSnapshot(
	t *testing.T,
	transcript string,
	followUp interviewFollowUpMode,
) EvidenceSnapshot {
	t.Helper()
	var payload evidencePayload
	if err := json.Unmarshal(validEvidenceSnapshotPayload(), &payload); err != nil {
		t.Fatalf("decode EvidenceSnapshot fixture: %v", err)
	}
	payload.ConfirmedTurns[0].Transcript.Text = transcript
	payload.EvidenceRefs[0].TranscriptSpan.EndUTF8Byte = len(transcript)

	if followUp != interviewFollowUpNone {
		payload.OpportunityManifest = append(
			payload.OpportunityManifest,
			evidenceOpportunity{
				Sequence:                2,
				QuestionID:              "question-2",
				QuestionType:            "FOLLOW_UP",
				ParentQuestionID:        "question-1",
				ObjectiveID:             "objective-1",
				QuestionText:            "What changed after the migration?",
				SpeakerParticipantID:    "participant-interviewer",
				AddresseeParticipantIDs: []string{"participant-candidate"},
			},
		)
	}
	if followUp == interviewFollowUpAnswered {
		secondText := "Latency fell and releases became safer."
		payload.OpportunityManifest[1].ResponseTurnID = "turn-2"
		payload.ConfirmedTurns = append(
			payload.ConfirmedTurns,
			evidenceConfirmedTurn{
				TurnID:                  "turn-2",
				Sequence:                2,
				QuestionID:              "question-2",
				RespondentParticipantID: "participant-candidate",
				InteractionMode:         "PUSH_TO_TALK",
				Transcript: evidenceTranscript{
					ID:                    "transcript-2",
					Text:                  secondText,
					EvidenceVersion:       1,
					ASRConfidence:         evidenceUnavailable,
					WordTimestamps:        evidenceUnavailable,
					AlternativeHypotheses: evidenceUnavailable,
				},
				Audio: evidenceAudio{
					Availability: evidenceUnavailable,
					Quality:      evidenceNotAssessed,
					ISE:          evidenceNotAssessed,
				},
			},
		)
		ref := payload.EvidenceRefs[0]
		ref.EvidenceRefID = ""
		ref.SnapshotID = ""
		ref.TurnID = "turn-2"
		ref.TranscriptSpan.EndUTF8Byte = len(secondText)
		ref.Lineage.TranscriptID = "transcript-2"
		ref.Lineage.CandidateID = "candidate-2"
		payload.EvidenceRefs = append(payload.EvidenceRefs, ref)
		payload.ProviderLineage.ASR = append(
			payload.ProviderLineage.ASR,
			evidenceASRLineage{
				TurnID:            "turn-2",
				TranscriptID:      "transcript-2",
				CandidateID:       "candidate-2",
				EvidenceVersion:   1,
				Provider:          "qianwen",
				Model:             "paraformer-v2",
				ProviderRequestID: "provider-request-2",
			},
		)
		payload.VersionManifest.TurnEvidence = append(
			payload.VersionManifest.TurnEvidence,
			evidenceTurnVersion{
				TurnID:          "turn-2",
				EvidenceVersion: 1,
			},
		)
	}

	provisional, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize provisional EvidenceSnapshot: %v", err)
	}
	sourceManifestHash, err := evidenceSourceManifestHash(provisional)
	if err != nil {
		t.Fatalf("derive EvidenceSnapshot source manifest: %v", err)
	}
	snapshotID := deriveEvidenceSnapshotID(
		testOwnerA,
		"practice-session-1",
		ScopeSession,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		turn := payload.ConfirmedTurns[index]
		payload.EvidenceRefs[index].SnapshotID = snapshotID
		payload.EvidenceRefs[index].EvidenceRefID = stableEvidenceRefID(
			snapshotID,
			turn.TurnID,
			turn.Transcript.EvidenceVersion,
			turn.Audio.ChecksumSHA256,
		)
	}
	canonical, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize EvidenceSnapshot: %v", err)
	}
	snapshot := EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              ScopeSession,
		SceneType:          SceneInterview,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("Interview EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}

func firstProviderDimension(value map[string]any) map[string]any {
	return value["dimensions"].([]any)[0].(map[string]any)
}

func firstProviderFinding(value map[string]any) map[string]any {
	return firstProviderDimension(value)["strengths"].([]any)[0].(map[string]any)
}

func firstProviderAnchor(value map[string]any) map[string]any {
	return firstProviderFinding(value)["evidence"].([]any)[0].(map[string]any)
}

func cloneInterviewShadowResult(
	t *testing.T,
	source InterviewShadowResult,
) InterviewShadowResult {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal result clone: %v", err)
	}
	var result InterviewShadowResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("decode result clone: %v", err)
	}
	return result
}

func jsonContainsExactKey(value any, wanted string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == wanted || jsonContainsExactKey(nested, wanted) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if jsonContainsExactKey(nested, wanted) {
				return true
			}
		}
	}
	return false
}
