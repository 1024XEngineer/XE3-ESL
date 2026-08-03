package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestIELTSSpeakingShadowProducesHonestPartialReport(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	provider := &ieltsProviderStub{}
	engine := NewIELTSSpeakingShadowEngine(provider)

	result, err := engine.Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("Provider calls = %d, want 1", provider.calls)
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
	report, err := ProjectIELTSSpeakingReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSSpeakingReport: %v", err)
	}
	if report.SpeakingOverall.Status !=
		IELTSSpeakingOverallNotAvailable ||
		report.TargetPlan.Status != IELTSTargetPlanNotConfigured ||
		!slicesEqualInts(
			report.PartReviews[0].QuestionIndexes,
			[]int{1, 2, 3, 4, 5, 6, 7, 8},
		) ||
		!slicesEqualInts(
			report.PartReviews[1].QuestionIndexes,
			[]int{9},
		) ||
		!slicesEqualInts(
			report.PartReviews[2].QuestionIndexes,
			[]int{10, 11, 12, 13, 14},
		) {
		t.Fatalf("projected report = %#v", report)
	}
}

func TestIELTSSpeakingShadowProducesFourBandsAndOverallWithAcoustics(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	provider := &ieltsProviderStub{}
	acoustics := &ieltsAcousticSourceStub{}
	result, err := NewIELTSSpeakingShadowEngineWithAcoustics(
		provider,
		acoustics,
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if acoustics.calls != 1 || len(result.Criteria) != 4 {
		t.Fatalf("acoustic calls = %d; result = %#v", acoustics.calls, result)
	}
	for _, criterion := range result.Criteria {
		if criterion.Scoreability != IELTSSpeakingScoreabilityProvisional ||
			criterion.EstimatedBand == nil ||
			*criterion.EstimatedBand != 6 {
			t.Fatalf("criterion = %#v", criterion)
		}
	}
	report, err := ProjectIELTSSpeakingReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSSpeakingReport: %v", err)
	}
	if report.SpeakingOverall.Status != IELTSSpeakingOverallAvailable ||
		report.SpeakingOverall.EstimatedBand == nil ||
		*report.SpeakingOverall.EstimatedBand != 6 {
		t.Fatalf("speaking overall = %#v", report.SpeakingOverall)
	}
}

func TestIELTSSpeakingShadowDoesNotCallProviderWithoutFourteenAnswers(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, 13)
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
		len(result.QuestionResults) != ieltsQuestionCount ||
		result.QuestionResults[13].OpportunityStatus !=
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
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := validIELTSProviderPayload(prepared.input)
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
	_, err = normalizeIELTSSpeakingProviderResult(
		prepared,
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

func TestIELTSSpeakingShadowRejectsNonFrozenModelVersion(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	var payload evidencePayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.Scene.Version = 1
	snapshot = rebuildIELTSSpeakingSnapshot(t, payload)
	if _, err := prepareIELTSSpeakingShadow(snapshot); !errors.Is(
		err,
		ErrInvalidRequest,
	) {
		t.Fatalf("non-frozen model error = %v", err)
	}
}

func TestIELTSSpeakingShadowRejectsCrossTurnAnchor(t *testing.T) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
	prepared, err := prepareIELTSSpeakingShadow(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	payload := validIELTSProviderPayload(prepared.input)
	payload.Criteria[0].Strengths[0].Evidence[0].EvidenceRefID =
		prepared.input.Questions[1].Response.EvidenceRefID
	_, err = normalizeIELTSSpeakingProviderResult(
		prepared,
		ieltsProviderResult(t, payload),
	)
	if !errors.Is(err, ErrInvalidIELTSSpeakingShadow) {
		t.Fatalf("cross-turn anchor error = %v", err)
	}
}

func TestIELTSSpeakingShadowRejectsCompleteResultDowngrades(
	t *testing.T,
) {
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
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
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsQuestionCount)
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
	payload []byte
	err     error
	calls   int
}

type ieltsAcousticSourceStub struct {
	calls int
}

func (source *ieltsAcousticSourceStub) GetIELTSSpeakingAcoustics(
	_ context.Context,
	_ string,
	requests []IELTSSpeakingAcousticRequest,
) ([]IELTSSpeakingTurnAcoustics, error) {
	source.calls++
	result := make([]IELTSSpeakingTurnAcoustics, 0, len(requests))
	for _, request := range requests {
		fluency := 76.0
		result = append(result, IELTSSpeakingTurnAcoustics{
			TurnID:               request.TurnID,
			EvidenceRefID:        request.EvidenceRefID,
			PronunciationScore:   72,
			AcousticFluencyScore: &fluency,
			Provider:             "xfyun_ise",
			ProviderRun:          "run_fixture",
		})
	}
	return result, nil
}

func (provider *ieltsProviderStub) AnalyzeIELTSSpeaking(
	_ context.Context,
	input IELTSSpeakingShadowProviderInput,
) (IELTSSpeakingShadowProviderResult, error) {
	provider.calls++
	if provider.err != nil {
		return IELTSSpeakingShadowProviderResult{}, provider.err
	}
	result := ieltsProviderResult(
		nil,
		validIELTSProviderPayload(input),
	)
	if provider.payload != nil {
		result.Payload = provider.payload
	}
	return result, nil
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
			(criterion != IELTSCriterionFC ||
				slices.Contains(input.AssessableCriteria, IELTSCriterionPR)) {
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
) EvidenceSnapshot {
	t.Helper()
	var payload evidencePayload
	if err := json.Unmarshal(
		validEvidenceSnapshotPayload(),
		&payload,
	); err != nil {
		t.Fatalf("decode EvidenceSnapshot fixture: %v", err)
	}
	payload.PracticeContext.SceneFamily =
		string(scene.SceneFamilyExam)
	payload.PracticeContext.SceneModel =
		string(scene.SceneModelIELTSSpeakingFullMock)
	payload.PracticeContext.Scene =
		evidenceVersionedRef{
			ID:      "scn_ielts_speaking_full",
			Version: ieltsFullMockSceneVersion,
		}
	payload.PracticeContext.Preparation.BackgroundSnapshotHash = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	payload.PracticeContext.PracticeOption = evidencePracticeOption{
		ID:   "option_ielts_speaking_full_full",
		Type: string(scene.PracticeOptionFullSimulation),
	}
	payload.PracticeContext.UserRole = "考生"
	payload.PracticeContext.FacilitatorRole = "IELTS 口语考官"
	payload.PracticeContext.PracticeGoal =
		"适应真实三段式流程，并在不同题型中保持连贯自然的表达。"
	payload.PracticeContext.PracticeObjectives = []evidenceObjective{
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
	payload.PracticeContext.TaskContext = evidenceTaskContext{
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
		"Part 1 question: Do you use artificial intelligence in your daily life?",
		"Part 1 question: Has technology changed the way you learn things?",
		"Part 1 question: Is there any technology you find difficult to use?",
		"Part 1 question: What do you usually do in your free time?",
		"Part 1 question: Do you prefer spending your free time alone or with other people?",
		"Part 2 cue card: Describe a skill you would like to learn.\n" +
			"You should say:\n• What the skill is\n• Why you want to learn it\n" +
			"• How you would learn it\n• And explain how learning this skill would benefit you",
		"Part 3 question: What kinds of skills are most valuable in today's society?",
		"Part 3 question: Some people say it is never too late to learn a new skill. Do you agree?",
		"Part 3 question: Do you think schools should focus more on practical skills?",
		"Part 3 question: How has technology changed the way people learn skills?",
		"Part 3 question: Do you think some skills will become obsolete in the future?",
	}
	payload.OpportunityManifest =
		make([]evidenceOpportunity, 0, ieltsQuestionCount)
	payload.ConfirmedTurns =
		make([]evidenceConfirmedTurn, 0, answered)
	payload.EvidenceRefs = make([]evidenceRef, 0, answered)
	payload.ProviderLineage.ASR =
		make([]evidenceASRLineage, 0, answered)
	payload.VersionManifest.TurnEvidence =
		make([]evidenceTurnVersion, 0, answered)

	for index := 1; index <= ieltsQuestionCount; index++ {
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
		if index <= 8 {
			objectiveID = "part_1_familiar_topics"
		} else if index == 9 {
			objectiveID = "part_2_long_turn"
		}
		opportunity := evidenceOpportunity{
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
				evidenceConfirmedTurn{
					TurnID:                  turnID,
					Sequence:                index,
					QuestionID:              questionID,
					RespondentParticipantID: "participant-candidate",
					InteractionMode:         "PUSH_TO_TALK",
					Transcript: evidenceTranscript{
						ID:                    transcriptID,
						Text:                  transcript,
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
			payload.EvidenceRefs = append(
				payload.EvidenceRefs,
				evidenceRef{
					TurnID:  turnID,
					Speaker: "USER",
					TranscriptSpan: evidenceTranscriptSpan{
						StartUTF8Byte: 0,
						EndUTF8Byte:   len(transcript),
					},
					Quality: evidenceQuality{
						Audio:         evidenceNotAssessed,
						ASRConfidence: evidenceUnavailable,
						Alignment:     evidenceUnavailable,
						ISE:           evidenceNotAssessed,
					},
					Lineage: evidenceRefLineage{
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
				evidenceASRLineage{
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
				evidenceTurnVersion{
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

func rebuildIELTSSpeakingSnapshot(
	t *testing.T,
	payload evidencePayload,
) EvidenceSnapshot {
	t.Helper()
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize provisional IELTS Snapshot: %v", err)
	}
	sourceManifestHash, err := evidenceSourceManifestHash(provisional)
	if err != nil {
		t.Fatalf("derive IELTS Snapshot source manifest: %v", err)
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
		payload.EvidenceRefs[index].EvidenceRefID =
			stableEvidenceRefID(
				snapshotID,
				turn.TurnID,
				turn.Transcript.EvidenceVersion,
				turn.Audio.ChecksumSHA256,
			)
	}
	canonical, err := canonicalEvidenceJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize IELTS Snapshot: %v", err)
	}
	snapshot := EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              ScopeSession,
		SceneType:          SceneIELTSSpeaking,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("IELTS EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}

func slicesEqualInts(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
