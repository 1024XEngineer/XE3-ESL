package scoring

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

const (
	ieltsTestPart1QuestionCount = 8
	ieltsTestPart2QuestionCount = 1
	ieltsTestQuestionCount      = 15
)

func TestIELTSSpeakingShadowProducesHonestPartialResult(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	provider := &ieltsProviderStub{}
	engine := NewIELTSSpeakingShadowEngine(provider)

	result, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 3 {
		t.Fatalf("Provider calls = %d, want 3", provider.calls)
	}
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		t.Fatalf("ValidateIELTSSpeakingShadowResult: %v", err)
	}
	if result.Scoreability !=
		IELTSSpeakingScoreabilityProvisional ||
		result.Gate != IELTSSpeakingGateFeedbackOnly ||
		len(result.Criteria) != 4 ||
		result.Criteria[0].EstimatedBand != nil ||
		result.Criteria[1].EstimatedBand == nil ||
		*result.Criteria[1].EstimatedBand != 6 ||
		result.Criteria[2].EstimatedBand == nil ||
		*result.Criteria[2].EstimatedBand != 6 ||
		result.Criteria[3].Scoreability !=
			IELTSSpeakingScoreabilityInsufficient ||
		result.Criteria[3].EstimatedBand != nil {
		t.Fatalf("partial result = %#v", result)
	}
}

func TestIELTSSpeakingShadowProducesFourBandsAndOverallWithAcoustics(
	t *testing.T,
) {
	snapshot := ieltsSpeakingAcousticTestSnapshot(t)
	provider := &ieltsProviderStub{}
	acoustics := ieltsAcousticSnapshotForTest(t, snapshot, ieltsTestQuestionCount)
	result, err := NewIELTSSpeakingShadowEngine(provider).
		EvaluateWithAcousticSnapshot(context.Background(), snapshot, acoustics)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 4 || len(result.Criteria) != 4 {
		t.Fatalf("provider calls = %d; result = %#v", provider.calls, result)
	}
	for _, criterion := range result.Criteria {
		if criterion.Scoreability != IELTSSpeakingScoreabilityProvisional ||
			criterion.EstimatedBand == nil ||
			*criterion.EstimatedBand != 6 {
			t.Fatalf("criterion = %#v", criterion)
		}
	}
}

func TestIELTSSpeakingShadowUsesVerifiedPartialAcousticCoverage(t *testing.T) {
	snapshot := ieltsSpeakingAcousticTestSnapshot(t)
	provider := &ieltsProviderStub{}
	acoustics := ieltsAcousticSnapshotForTest(t, snapshot, 4)
	result, err := NewIELTSSpeakingShadowEngine(provider).
		EvaluateWithAcousticSnapshot(context.Background(), snapshot, acoustics)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	pronunciation := result.Criteria[3]
	if pronunciation.EstimatedBand == nil ||
		!sameRatio(pronunciation.Coverage, ratio(4, ieltsTestQuestionCount)) {
		t.Fatalf("pronunciation = %#v", pronunciation)
	}
}

func TestIELTSSpeakingShadowScoresFrozenPart1WithIELTSRubric(t *testing.T) {
	snapshot := ieltsSpeakingPart1TestSnapshot(t)
	provider := &ieltsProviderStub{}
	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Scoreability != IELTSSpeakingScoreabilityProvisional ||
		len(provider.inputs) != 3 {
		t.Fatalf("result = %#v; inputs = %#v", result, provider.inputs)
	}
	for _, criterion := range []IELTSCriterion{
		IELTSCriterionFC,
		IELTSCriterionLR,
		IELTSCriterionGRA,
	} {
		input, ok := provider.inputs[criterion]
		if !ok || input.PracticeMode != "PART_1" ||
			len(input.Questions) != ieltsTestPart1QuestionCount ||
			!reflect.DeepEqual(
				input.AssessableCriteria,
				[]IELTSCriterion{criterion},
			) {
			t.Fatalf("%s input = %#v", criterion, input)
		}
		if criterion == IELTSCriterionFC {
			if len(input.RubricDescriptors) != 0 {
				t.Fatalf("FC descriptors = %#v", input.RubricDescriptors)
			}
			continue
		}
		if len(input.RubricDescriptors) != 1 ||
			input.RubricDescriptors[0].CriterionID != criterion ||
			!reflect.DeepEqual(
				input.RubricDescriptors[0].Descriptors,
				ieltsDescriptorsFor(criterion),
			) {
			t.Fatalf("%s descriptors = %#v", criterion, input.RubricDescriptors)
		}
	}
	if err := ValidateIELTSSpeakingShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateIELTSSpeakingShadowResult: %v", err)
	}
}

func TestIELTSSpeakingShadowClassifiesMixedLanguageWithoutScoringChinese(
	t *testing.T,
) {
	snapshot := ieltsSpeakingSnapshotWithTranscript(
		t,
		"I explain my English answer clearly. 这是中文补充。",
	)
	provider := &ieltsProviderStub{}
	_, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	response := provider.input.Questions[0].Response
	if response == nil || response.LanguageEvidence != ieltsLanguageMixed ||
		response.EnglishWordCount != 6 || response.CJKCharacterCount == 0 {
		t.Fatalf("mixed response = %#v", response)
	}
}

func TestIELTSSpeakingShadowRejectsChineseOnlySessionAsUnscoreable(
	t *testing.T,
) {
	snapshot := ieltsSpeakingSnapshotWithTranscript(t, "这是中文回答。")
	provider := &ieltsProviderStub{}
	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 0 ||
		result.Scoreability != IELTSSpeakingScoreabilityInsufficient ||
		!slices.Equal(result.ReasonCodes, []IELTSSpeakingReasonCode{
			IELTSReasonInsufficientEvidence,
		}) {
		t.Fatalf("result = %#v; provider calls = %d", result, provider.calls)
	}
	if err := ValidateIELTSSpeakingShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateIELTSSpeakingShadowResult: %v", err)
	}
}

func TestIELTSSpeakingShadowRejectsRepeatedShortAnswersAsUnscoreable(
	t *testing.T,
) {
	snapshot := ieltsSpeakingSnapshotWithTranscript(
		t,
		"Yes, yes. 666 这是中文。",
	)
	provider := &ieltsProviderStub{}
	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 0 ||
		result.Scoreability != IELTSSpeakingScoreabilityInsufficient ||
		result.Gate != IELTSSpeakingGateBlocked ||
		result.Provider != nil ||
		!slices.Equal(result.ReasonCodes, []IELTSSpeakingReasonCode{
			IELTSReasonInsufficientEvidence,
		}) {
		t.Fatalf("result = %#v; provider calls = %d", result, provider.calls)
	}
	if err := ValidateIELTSSpeakingShadowResult(snapshot, result); err != nil {
		t.Fatalf("ValidateIELTSSpeakingShadowResult: %v", err)
	}
}

func TestIELTSSpeakingShadowDoesNotCallProviderWithoutEveryFrozenAnswer(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount-1)
	provider := &ieltsProviderStub{}
	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("Provider calls = %d, want 0", provider.calls)
	}
	if result.Scoreability !=
		IELTSSpeakingScoreabilityInsufficient ||
		result.Gate != IELTSSpeakingGateBlocked ||
		result.Provider != nil ||
		len(result.QuestionResults) != ieltsTestQuestionCount ||
		result.QuestionResults[ieltsTestQuestionCount-1].OpportunityStatus !=
			IELTSOpportunityNotProvided ||
		!slices.Equal(
			result.Criteria[3].ReasonCodes,
			[]IELTSSpeakingReasonCode{
				IELTSReasonPronunciationArtifactUnavailable,
			},
		) {
		t.Fatalf("insufficient result = %#v", result)
	}
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		t.Fatalf("Validate insufficient result: %v", err)
	}
}

func TestIELTSSpeakingShadowRejectsProviderGateAndNumericScore(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["gate_status"] = "PASS"
	value["overall"] = 7
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	_, err = normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		IELTSSpeakingShadowProviderResult{
			Payload:   raw,
			Provider:  "provider",
			Model:     "model",
			RequestID: "request-1",
		},
	)
	if !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("invalid Provider payload error = %v", err)
	}
}

func TestIELTSSpeakingShadowClassifiesProviderJSONContractFailures(
	t *testing.T,
) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "invalid JSON",
			payload: []byte(`{"schema_version":`),
			want:    errIELTSSpeakingProviderInvalidJSON,
		},
		{
			name: "unknown field",
			payload: []byte(`{"schema_version":"` +
				IELTSSpeakingShadowProviderSchemaVersion +
				`","criteria":[],"unexpected":true}`),
			want: errIELTSSpeakingProviderSchemaMismatch,
		},
		{
			name: "wrong schema version",
			payload: []byte(
				`{"schema_version":"wrong","criteria":[]}`,
			),
			want: errIELTSSpeakingProviderSchemaMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, normalizeErr := normalizeIELTSSpeakingCriterionProviderResult(
				prepared,
				IELTSCriterionFC,
				IELTSSpeakingShadowProviderResult{
					Payload:   test.payload,
					Provider:  "provider",
					Model:     "model",
					RequestID: "request-1",
				},
			)
			if !errors.Is(normalizeErr, test.want) ||
				!errors.Is(normalizeErr, ErrInvalidIELTSSpeakingShadow) {
				t.Fatalf("normalize error = %v", normalizeErr)
			}
		})
	}
}

func TestIELTSSpeakingShadowRejectsNonFullMockEvaluationPolicy(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.EvaluationPolicyRef =
		"ielts.speaking_practice.evaluation.v1"
	snapshot = rebuildIELTSSpeakingSnapshot(t, payload)
	if _, err := prepareIELTSSpeakingShadow(snapshot); !errors.Is(
		err,
		evaluation.ErrInvalidRequest,
	) {
		t.Fatalf("non-full-mock evaluation policy error = %v", err)
	}
}

func TestIELTSSpeakingShadowRepairsUniquelyMispairedAnchor(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	payload.Criteria[0].Strengths[0].Evidence[0].EvidenceRefID =
		prepared.input.Questions[1].Response.EvidenceRefID
	result, err := normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		ieltsProviderResult(t, payload),
	)
	if err != nil {
		t.Fatalf("repair unique cross-turn anchor: %v", err)
	}
	if got := result.Strengths[0].Evidence[0].EvidenceRefID; got != prepared.input.Questions[0].Response.EvidenceRefID {
		t.Fatalf("repaired evidence_ref_id = %q", got)
	}
}

func TestIELTSSpeakingShadowRejectsAmbiguousMispairedAnchor(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	anchor := &payload.Criteria[0].Strengths[0].Evidence[0]
	anchor.EvidenceRefID = "missing-evidence-ref"
	anchor.Quote = "I explain"
	_, err = normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		ieltsProviderResult(t, payload),
	)
	if !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("ambiguous cross-turn anchor error = %v", err)
	}
}

func TestIELTSSpeakingShadowKeepsValidFindingWhenPeerIsInvalid(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionLR)
	response := prepared.input.Questions[0].Response
	if response == nil {
		t.Fatal("missing response fixture")
	}
	template, ok := lookupIELTSFeedbackTemplate(
		IELTSCriterionLR,
		ieltsFindingImprovement,
	)
	if !ok {
		t.Fatal("missing LR improvement template")
	}
	payload.Criteria[0].Strengths = []ieltsProviderFinding{}
	payload.Criteria[0].Improvements = []ieltsProviderFinding{
		{
			TemplateID: template.ID,
			Evidence: []ieltsProviderAnchor{{
				EvidenceRefID: response.EvidenceRefID,
				Quote:         "This quote does not exist.",
				Occurrence:    1,
			}},
		},
		{
			TemplateID: template.ID,
			Suggestion: "Use a more precise example.",
			Evidence: []ieltsProviderAnchor{{
				EvidenceRefID: response.EvidenceRefID,
				Quote:         response.Transcript,
				Occurrence:    1,
			}},
		},
	}

	criterion, err := normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionLR,
		ieltsProviderResult(t, payload),
	)
	if err != nil {
		t.Fatalf("normalize valid sibling: %v", err)
	}
	if len(criterion.Strengths) != 0 || len(criterion.Improvements) != 1 ||
		criterion.Improvements[0].Suggestion != "Use a more precise example." {
		t.Fatalf("criterion findings = %#v", criterion)
	}
}

func TestIELTSSpeakingShadowRejectsCriterionWhenAllPrimaryFindingsAreDropped(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionLR)
	payload.Criteria[0].Strengths[0].Evidence[0].Quote =
		"This quote does not exist in the frozen snapshot."

	_, err = normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionLR,
		ieltsProviderResult(t, payload),
	)
	var rejection *ieltsCriterionProviderRejection
	if !errors.As(err, &rejection) ||
		rejection.stage != "semantic_validation" ||
		rejection.code != "no_primary_findings" {
		t.Fatalf("rejection = %#v; error = %v", rejection, err)
	}
}

func TestIELTSSpeakingShadowCorrectsUniqueOccurrenceWithinReference(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	anchor := &payload.Criteria[0].Strengths[0].Evidence[0]
	anchor.Quote = "I explain"
	anchor.Occurrence = 2

	criterion, err := normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		ieltsProviderResult(t, payload),
	)
	if err != nil {
		t.Fatalf("normalize unique occurrence: %v", err)
	}
	evidence := criterion.Strengths[0].Evidence[0]
	if evidence.EvidenceRefID != anchor.EvidenceRefID ||
		evidence.OriginalExcerpt != anchor.Quote ||
		evidence.StartUTF8Byte != 0 ||
		evidence.EndUTF8Byte != len(anchor.Quote) {
		t.Fatalf("corrected evidence = %#v", evidence)
	}
}

func TestIELTSSpeakingShadowDeduplicatesCanonicalFindings(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	duplicate := payload.Criteria[0].Strengths[0]
	duplicate.Evidence = slices.Clone(duplicate.Evidence)
	duplicate.Evidence[0].EvidenceRefID =
		prepared.input.Questions[1].Response.EvidenceRefID
	payload.Criteria[0].Strengths = append(
		payload.Criteria[0].Strengths,
		duplicate,
	)

	criterion, err := normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		ieltsProviderResult(t, payload),
	)
	if err != nil {
		t.Fatalf("normalize duplicate findings: %v", err)
	}
	if len(criterion.Strengths) != 1 {
		t.Fatalf("strengths = %#v", criterion.Strengths)
	}
}

func TestIELTSSpeakingShadowIgnoresFCDescriptorWithoutAcoustics(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := singleIELTSProviderPayload(t, prepared.input, IELTSCriterionFC)
	payload.Criteria[0].RubricDescriptor = "FC_PRACTICE_BAND_7"
	result, err := normalizeIELTSSpeakingCriterionProviderResult(
		prepared,
		IELTSCriterionFC,
		ieltsProviderResult(t, payload),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.EstimatedBand != nil || result.BandDescriptor != "" {
		t.Fatalf("FC criterion = %#v", result)
	}
}

func TestIELTSSpeakingShadowRejectsCompleteResultDowngrades(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := NewIELTSSpeakingShadowEngine(
		&ieltsProviderStub{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}

	rootDowngrade := valid
	rootDowngrade.Scoreability =
		IELTSSpeakingScoreabilityInsufficient
	rootDowngrade.Gate = IELTSSpeakingGateBlocked
	rootDowngrade.ReasonCodes = []IELTSSpeakingReasonCode{
		IELTSReasonOpportunityNotProvided,
	}
	rootDowngrade.Provider = nil
	rootDowngrade.Criteria = blockedIELTSCriteria(
		1,
		IELTSReasonOpportunityNotProvided,
	)
	rootDowngrade.QuestionResults = ieltsSpeakingQuestionResults(
		prepared,
		rootDowngrade.Criteria,
	)
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		rootDowngrade,
	); !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("root downgrade error = %v", err)
	}

	criterionDowngrade := valid
	criterionDowngrade.Criteria = slices.Clone(valid.Criteria)
	criterionDowngrade.Criteria[1] = blockedIELTSCriterion(
		IELTSCriterionLR,
		1,
		IELTSReasonInsufficientEvidence,
	)
	criterionDowngrade.QuestionResults = ieltsSpeakingQuestionResults(
		prepared,
		criterionDowngrade.Criteria,
	)
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		criterionDowngrade,
	); !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("criterion downgrade error = %v", err)
	}
}

func TestIELTSSpeakingShadowRejectsFindingKindConfusion(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := NewIELTSSpeakingShadowEngine(
		&ieltsProviderStub{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}

	wrongKind := valid
	wrongKind.Criteria = slices.Clone(valid.Criteria)
	wrongKind.Criteria[0].Strengths = slices.Clone(
		valid.Criteria[0].Strengths,
	)
	finding := wrongKind.Criteria[0].Strengths[0]
	finding.ID = stableIELTSFindingID(
		wrongKind.SnapshotID,
		wrongKind.Criteria[0].CriterionID,
		ieltsFindingImprovement,
		finding,
	)
	wrongKind.Criteria[0].Strengths[0] = finding
	wrongKind.QuestionResults = ieltsSpeakingQuestionResults(
		prepared,
		wrongKind.Criteria,
	)
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		wrongKind,
	); !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("wrong finding kind error = %v", err)
	}

	suggestedStrength := valid
	suggestedStrength.Criteria = slices.Clone(valid.Criteria)
	suggestedStrength.Criteria[0].Strengths = slices.Clone(
		valid.Criteria[0].Strengths,
	)
	finding = suggestedStrength.Criteria[0].Strengths[0]
	finding.Suggestion = "This must not be attached to a strength."
	finding.ID = stableIELTSFindingID(
		suggestedStrength.SnapshotID,
		suggestedStrength.Criteria[0].CriterionID,
		ieltsFindingStrength,
		finding,
	)
	suggestedStrength.Criteria[0].Strengths[0] = finding
	suggestedStrength.QuestionResults = ieltsSpeakingQuestionResults(
		prepared,
		suggestedStrength.Criteria,
	)
	if err := ValidateIELTSSpeakingShadowResult(
		snapshot,
		suggestedStrength,
	); !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("strength suggestion error = %v", err)
	}
}

type ieltsProviderStub struct {
	mu      sync.Mutex
	payload []byte
	mutate  func(
		IELTSSpeakingCriterionProviderRequest,
		ieltsProviderPayload,
	) ieltsProviderPayload
	observeContext func(context.Context)
	err            error
	calls          int
	input          IELTSSpeakingShadowProviderInput
	inputs         map[IELTSCriterion]IELTSSpeakingShadowProviderInput
}

func (provider *ieltsProviderStub) AnalyzeIELTSCriterion(
	ctx context.Context,
	request IELTSSpeakingCriterionProviderRequest,
) (IELTSSpeakingShadowProviderResult, error) {
	if provider.observeContext != nil {
		provider.observeContext(ctx)
	}
	provider.mu.Lock()
	provider.calls++
	provider.input = request.Input
	if provider.inputs == nil {
		provider.inputs = make(
			map[IELTSCriterion]IELTSSpeakingShadowProviderInput,
		)
	}
	provider.inputs[request.Input.AssessableCriteria[0]] = request.Input
	call := provider.calls
	provider.mu.Unlock()
	if provider.err != nil {
		return IELTSSpeakingShadowProviderResult{}, provider.err
	}
	payload := validIELTSProviderPayload(request.Input)
	if provider.mutate != nil {
		payload = provider.mutate(request, payload)
	}
	result := ieltsProviderResult(nil, payload)
	result.RequestID = criterionRequestID(
		request.Input.AssessableCriteria[0],
		call,
	)
	if provider.payload != nil {
		result.Payload = provider.payload
	}
	return result, nil
}

func singleIELTSProviderPayload(
	t *testing.T,
	input IELTSSpeakingShadowProviderInput,
	target IELTSCriterion,
) ieltsProviderPayload {
	t.Helper()
	criterionInput, err := ieltsCriterionProviderInput(input, target)
	if err != nil {
		t.Fatalf("ieltsCriterionProviderInput: %v", err)
	}
	return validIELTSProviderPayload(criterionInput)
}

func validIELTSProviderPayload(
	input IELTSSpeakingShadowProviderInput,
) ieltsProviderPayload {
	first := input.Questions[0].Response
	if first == nil {
		panic("IELTS Provider fixture needs a response")
	}
	criteria := make([]ieltsProviderCriterion, 0, 3)
	for _, criterion := range input.AssessableCriteria {
		template, ok := lookupIELTSFeedbackTemplate(
			criterion,
			ieltsFindingStrength,
		)
		if !ok {
			panic("missing IELTS feedback template")
		}
		value := ieltsProviderCriterion{
			CriterionID: criterion,
			Strengths: []ieltsProviderFinding{{
				TemplateID: template.ID,
				Evidence: []ieltsProviderAnchor{{
					EvidenceRefID: first.EvidenceRefID,
					Quote:         first.Transcript,
					Occurrence:    1,
				}},
			}},
			Improvements:    []ieltsProviderFinding{},
			UpgradeExamples: []ieltsProviderFinding{},
		}
		if descriptors := ieltsDescriptorsFor(criterion); len(descriptors) > 0 &&
			len(input.RubricDescriptors) > 0 {
			value.RubricDescriptor =
				descriptors[5].ID
		}
		criteria = append(criteria, value)
	}
	return ieltsProviderPayload{
		SchemaVersion: IELTSSpeakingShadowProviderSchemaVersion,
		Criteria:      criteria,
	}
}

func ieltsProviderResult(
	t *testing.T,
	payload ieltsProviderPayload,
) IELTSSpeakingShadowProviderResult {
	if t != nil {
		t.Helper()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return IELTSSpeakingShadowProviderResult{
		Payload:   raw,
		Provider:  "provider",
		Model:     "model",
		RequestID: "request-1",
	}
}

func ieltsSpeakingTestSnapshot(
	t *testing.T,
	answered int,
) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(
		validEvidenceSnapshotPayload(),
		&payload,
	); err != nil {
		t.Fatalf("decode evidence.EvidenceSnapshot fixture: %v", err)
	}
	payload.PracticeContext.PracticeExperience =
		string(scene.PracticeExperienceIELTSSpeaking)
	payload.PracticeContext.SceneCategory =
		string(scene.SceneCategoryIELTSSpeaking)
	payload.PracticeContext.PracticeMode =
		string(scene.PracticeModeFullMock)
	payload.PracticeContext.EvaluationPolicyRef =
		IELTSSpeakingFullMockEvaluationPolicyRef
	payload.PracticeContext.Scene =
		evidence.VersionedRef{
			ID:      "scn_ielts_speaking",
			Version: 1,
		}
	payload.PracticeContext.Preparation.BackgroundSnapshotHash = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	payload.PracticeContext.PracticeOption = evidence.PracticeOption{
		ID:   "option_ielts_speaking_full_mock",
		Mode: string(scene.PracticeModeFullMock),
	}
	payload.PracticeContext.UserRole = "考生"
	payload.PracticeContext.FacilitatorRole = "IELTS 口语考官"
	payload.PracticeContext.PracticeGoal =
		"适应真实三段式流程，并在不同题型中保持连贯自然的表达。"
	payload.PracticeContext.PracticeObjectives = []evidence.Objective{
		{
			ID: "part_1_familiar_topics",
			Description: "Answer familiar-topic questions directly with " +
				"relevant detail.",
		},
		{
			ID: "part_2_long_turn",
			Description: "Deliver a coherent long turn that covers every " +
				"cue-card point.",
		},
		{
			ID: "part_3_discussion",
			Description: "Develop abstract ideas with reasons, examples, " +
				"and comparisons.",
		},
	}
	payload.PracticeContext.TaskContext = evidence.TaskContext{
		PublicSceneBrief: "按 Part 1、Part 2、Part 3 连续完成一轮 IELTS 口语完整模考。",
		PersonaSummary: "A neutral IELTS speaking examiner who follows the frozen " +
			"three-part mock-test sequence, asks exactly one item at a time, and " +
			"never teaches or scores during the simulation.",
		FocusAreas: []string{
			"part_1_familiar_topics",
			"part_2_long_turn",
			"part_3_discussion",
			"section_transition",
		},
		SuggestedDurationSeconds: 900,
	}
	payload.PracticeContext.TaskBlueprints = []string{
		"Part 1 question: Where is your hometown?",
		"Part 1 question: Is there anything you do not like about your hometown?",
		"Part 1 question: Would you say it is a good place for young people?",
		"Part 1 question: What do people usually do in your hometown?",
		"Part 1 question: Has your hometown changed in recent years?",
		"Part 1 question: Do you often visit your hometown?",
		"Part 1 question: What is the weather like in your hometown?",
		"Part 1 question: Would you like to live there in the future?",
		"Part 2 cue card: Describe a skill you would like to learn.\n" +
			"You should say:\n• What the skill is\n• Why you want to learn it\n" +
			"• How you would learn it\n• And explain how learning this skill would benefit you",
		"Part 3 question: What kinds of skills are most valuable in today's society?",
		"Part 3 question: Some people say it is never too late to learn a new skill. Do you agree?",
		"Part 3 question: Do you think schools should focus more on practical skills?",
		"Part 3 question: How has technology changed the way people learn skills?",
		"Part 3 question: Should employers help workers learn new skills?",
		"Part 3 question: Why do some people stop learning after leaving school?",
	}
	payload.PracticeContext.IELTSAssignment = &evidence.IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   string(scene.PracticeModeFullMock),
		Parts: []evidence.IELTSAssignmentPart{
			{
				Part:           string(scene.PracticeModePart1),
				SourceID:       "part-1-set-1",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[:ieltsTestPart1QuestionCount]),
			},
			{
				Part:           string(scene.PracticeModePart2),
				SourceID:       "topic-group-1",
				TopicTitle:     "Learning a skill",
				CueCard:        "Describe a skill you would like to learn.",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[ieltsTestPart1QuestionCount : ieltsTestPart1QuestionCount+ieltsTestPart2QuestionCount]),
			},
			{
				Part:           string(scene.PracticeModePart3),
				SourceID:       "topic-group-1",
				TopicTitle:     "Learning a skill",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[ieltsTestPart1QuestionCount+ieltsTestPart2QuestionCount:]),
			},
		},
	}
	payload.OpportunityManifest =
		make([]evidence.Opportunity, 0, ieltsTestQuestionCount)
	payload.ConfirmedTurns =
		make([]evidence.ConfirmedTurn, 0, answered)
	payload.EvidenceRefs = make([]evidence.Ref, 0, answered)
	payload.ProviderLineage.ASR =
		make([]evidence.ASRLineage, 0, answered)
	payload.VersionManifest.TurnEvidence =
		make([]evidence.TurnVersion, 0, answered)

	for index := 1; index <= ieltsTestQuestionCount; index++ {
		questionID := fmt.Sprintf("question-%d", index)
		turnID := fmt.Sprintf("turn-%d", index)
		transcriptID := fmt.Sprintf("transcript-%d", index)
		candidateID := fmt.Sprintf("candidate-%d", index)
		questionText := fmt.Sprintf("IELTS question %d?", index)
		transcript := fmt.Sprintf(
			"I explain answer %d clearly with a concrete example.",
			index,
		)
		objectiveID := "part_3_discussion"
		if index <= ieltsTestPart1QuestionCount {
			objectiveID = "part_1_familiar_topics"
		} else if index ==
			ieltsTestPart1QuestionCount+ieltsTestPart2QuestionCount {
			objectiveID = "part_2_long_turn"
		}
		opportunity := evidence.Opportunity{
			Sequence:                index,
			QuestionID:              questionID,
			QuestionType:            "PRIMARY",
			ObjectiveID:             objectiveID,
			QuestionText:            questionText,
			SpeakerParticipantID:    "participant-interviewer",
			AddresseeParticipantIDs: []string{"participant-candidate"},
		}
		if index <= answered {
			opportunity.ResponseTurnID = turnID
			payload.ConfirmedTurns = append(
				payload.ConfirmedTurns,
				evidence.ConfirmedTurn{
					TurnID:                  turnID,
					Sequence:                index,
					QuestionID:              questionID,
					RespondentParticipantID: "participant-candidate",
					InteractionMode:         "PUSH_TO_TALK",
					Transcript: evidence.Transcript{
						ID:                    transcriptID,
						Text:                  transcript,
						EvidenceVersion:       1,
						ASRConfidence:         evidenceUnavailable,
						WordTimestamps:        evidenceUnavailable,
						AlternativeHypotheses: evidenceUnavailable,
					},
					Audio: evidence.Audio{
						Availability: evidenceUnavailable,
						Quality:      evidenceNotAssessed,
						ISE:          evidenceNotAssessed,
					},
				},
			)
			payload.EvidenceRefs = append(
				payload.EvidenceRefs,
				evidence.Ref{
					TurnID:  turnID,
					Speaker: "USER",
					TranscriptSpan: evidence.TranscriptSpan{
						StartUTF8Byte: 0,
						EndUTF8Byte:   len(transcript),
					},
					Quality: evidence.Quality{
						Audio:         evidenceNotAssessed,
						ASRConfidence: evidenceUnavailable,
						Alignment:     evidenceUnavailable,
						ISE:           evidenceNotAssessed,
					},
					Lineage: evidence.RefLineage{
						TranscriptID:    transcriptID,
						CandidateID:     candidateID,
						EvidenceVersion: 1,
						ASRProvider:     "qianwen",
						ASRModel:        "paraformer-v2",
					},
				},
			)
			payload.ProviderLineage.ASR = append(
				payload.ProviderLineage.ASR,
				evidence.ASRLineage{
					TurnID:          turnID,
					TranscriptID:    transcriptID,
					CandidateID:     candidateID,
					EvidenceVersion: 1,
					Provider:        "qianwen",
					Model:           "paraformer-v2",
					ProviderRequestID: fmt.Sprintf(
						"provider-request-%d",
						index,
					),
				},
			)
			payload.VersionManifest.TurnEvidence = append(
				payload.VersionManifest.TurnEvidence,
				evidence.TurnVersion{
					TurnID:          turnID,
					EvidenceVersion: 1,
				},
			)
		}
		payload.OpportunityManifest = append(
			payload.OpportunityManifest,
			opportunity,
		)
	}
	return rebuildIELTSSpeakingSnapshot(t, payload)
}

func ieltsSpeakingSnapshotWithTranscript(
	t *testing.T,
	transcript string,
) evidence.EvidenceSnapshot {
	t.Helper()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatalf("decode IELTS Snapshot: %v", err)
	}
	for index := range payload.ConfirmedTurns {
		payload.ConfirmedTurns[index].Transcript.Text = transcript
		payload.EvidenceRefs[index].TranscriptSpan.EndUTF8Byte = len(transcript)
	}
	return rebuildIELTSSpeakingSnapshot(t, payload)
}

func ieltsSpeakingPart1TestSnapshot(t *testing.T) evidence.EvidenceSnapshot {
	t.Helper()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatalf("decode IELTS Snapshot: %v", err)
	}
	payload.PracticeContext.PracticeMode = "PART_1"
	payload.PracticeContext.EvaluationPolicyRef = IELTSSpeakingPracticeEvaluationPolicyRef
	payload.PracticeContext.PracticeOption.Mode = "PART_1"
	payload.PracticeContext.IELTSAssignment.Mode = "PART_1"
	payload.PracticeContext.IELTSAssignment.Parts =
		payload.PracticeContext.IELTSAssignment.Parts[:1]
	payload.PracticeContext.TaskBlueprints =
		payload.PracticeContext.TaskBlueprints[:ieltsTestPart1QuestionCount]
	payload.OpportunityManifest =
		payload.OpportunityManifest[:ieltsTestPart1QuestionCount]
	payload.ConfirmedTurns = payload.ConfirmedTurns[:ieltsTestPart1QuestionCount]
	payload.EvidenceRefs = payload.EvidenceRefs[:ieltsTestPart1QuestionCount]
	payload.ProviderLineage.ASR =
		payload.ProviderLineage.ASR[:ieltsTestPart1QuestionCount]
	payload.VersionManifest.TurnEvidence =
		payload.VersionManifest.TurnEvidence[:ieltsTestPart1QuestionCount]
	return rebuildIELTSSpeakingSnapshot(t, payload)
}

func rebuildIELTSSpeakingSnapshot(
	t *testing.T,
	payload evidence.SnapshotPayload,
) evidence.EvidenceSnapshot {
	t.Helper()
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize provisional IELTS Snapshot: %v", err)
	}
	sourceManifestHash, err := evidence.SourceManifestHash(provisional)
	if err != nil {
		t.Fatalf("derive IELTS Snapshot source manifest: %v", err)
	}
	snapshotID := evidence.DeriveSnapshotID(
		testOwnerA,
		"practice-session-1",
		evaluation.ScopeSession,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		turn := payload.ConfirmedTurns[index]
		payload.EvidenceRefs[index].SnapshotID = snapshotID
		payload.EvidenceRefs[index].EvidenceRefID =
			evidence.StableRefID(
				snapshotID,
				turn.TurnID,
				turn.Transcript.EvidenceVersion,
				turn.Audio.ChecksumSHA256,
			)
	}
	canonical, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize IELTS Snapshot: %v", err)
	}
	snapshot := evidence.EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              evaluation.ScopeSession,
		SceneType:          evaluation.SceneIELTSSpeaking,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("IELTS evidence.EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}
