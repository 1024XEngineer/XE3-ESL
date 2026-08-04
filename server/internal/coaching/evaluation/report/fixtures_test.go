package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

const (
	reportTestOwner                 = "10000000-0000-4000-8000-000000000001"
	reportTestPreparationBackground = "Evaluation evidence fixture background."
	reportEvidenceUnavailable       = "UNAVAILABLE"
	reportEvidenceNotAssessed       = "NOT_ASSESSED"
	reportIELTSFullMockSceneVersion = 2
	reportIELTSFullMockSceneID      = "scn_ielts_speaking_full"
)

type interviewReportFollowUpMode int

const (
	interviewReportFollowUpNone interviewReportFollowUpMode = iota
	interviewReportFollowUpUnanswered
	interviewReportFollowUpAnswered
)

type interviewReportProviderOptions struct {
	ImprovementDimensions int
	AddFollowUpEvidence   bool
}

type interviewReportProvider struct {
	options interviewReportProviderOptions
}

type interviewReportProviderPayload struct {
	SchemaVersion string                             `json:"schema_version"`
	Dimensions    []interviewReportProviderDimension `json:"dimensions"`
}

type interviewReportProviderDimension struct {
	DimensionID            scoring.InterviewDimension       `json:"dimension_id"`
	Score                  int                              `json:"score"`
	Strengths              []interviewReportProviderFinding `json:"strengths"`
	Improvements           []interviewReportProviderFinding `json:"improvements"`
	RecommendedExpressions []interviewReportProviderFinding `json:"recommended_expressions"`
}

type interviewReportProviderFinding struct {
	TemplateID string                          `json:"template_id"`
	Evidence   []interviewReportProviderAnchor `json:"evidence"`
}

type interviewReportProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

func (provider *interviewReportProvider) AnalyzeInterview(
	_ context.Context,
	input scoring.InterviewShadowProviderInput,
) (scoring.InterviewShadowProviderResult, error) {
	var first *scoring.InterviewProviderResponse
	var followUp *scoring.InterviewProviderResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil && first == nil {
			first = opportunity.Response
		}
		if opportunity.QuestionType == "FOLLOW_UP" {
			followUp = opportunity.Response
		}
	}
	if first == nil {
		panic("Interview report fixture requires one response")
	}
	payload := interviewReportProviderPayload{
		SchemaVersion: scoring.InterviewShadowProviderSchemaVersion,
		Dimensions: make(
			[]interviewReportProviderDimension,
			0,
			len(input.AssessableDimensions),
		),
	}
	for index, dimension := range input.AssessableDimensions {
		response := first
		if dimension == scoring.InterviewDimensionInteraction {
			response = followUp
		}
		if response == nil {
			panic("Interaction report fixture requires a follow-up response")
		}
		anchors := []interviewReportProviderAnchor{{
			EvidenceRefID: response.EvidenceRefID,
			Quote:         response.Transcript,
			Occurrence:    1,
		}}
		if provider.options.AddFollowUpEvidence &&
			index == 0 && followUp != nil {
			anchors = append(anchors, interviewReportProviderAnchor{
				EvidenceRefID: followUp.EvidenceRefID,
				Quote:         followUp.Transcript,
				Occurrence:    1,
			})
		}
		value := interviewReportProviderDimension{
			DimensionID: dimension,
			Score:       75,
			Strengths: []interviewReportProviderFinding{{
				TemplateID: interviewReportTemplateID(dimension, "STRENGTH"),
				Evidence:   anchors,
			}},
			Improvements:           []interviewReportProviderFinding{},
			RecommendedExpressions: []interviewReportProviderFinding{},
		}
		if index < provider.options.ImprovementDimensions {
			value.Improvements = []interviewReportProviderFinding{{
				TemplateID: interviewReportTemplateID(dimension, "IMPROVEMENT"),
				Evidence:   anchors[:1],
			}}
		}
		payload.Dimensions = append(payload.Dimensions, value)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return scoring.InterviewShadowProviderResult{}, err
	}
	return scoring.InterviewShadowProviderResult{
		Payload:   raw,
		Provider:  "provider",
		Model:     "model",
		RequestID: "request-1",
	}, nil
}

func interviewReportTemplateID(
	dimension scoring.InterviewDimension,
	kind string,
) string {
	return string(dimension) + ":" + kind + ":v1"
}

func interviewReportTestResult(
	t *testing.T,
	snapshot evidence.EvidenceSnapshot,
	options interviewReportProviderOptions,
) scoring.InterviewShadowResult {
	t.Helper()
	result, err := scoring.NewInterviewShadowEngine(
		&interviewReportProvider{options: options},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("evaluate Interview report fixture: %v", err)
	}
	return result
}

func interviewReportTestSnapshot(
	t *testing.T,
	transcript string,
	followUp interviewReportFollowUpMode,
) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(reportTestEvidencePayload(), &payload); err != nil {
		t.Fatalf("decode Interview report fixture: %v", err)
	}
	payload.ConfirmedTurns[0].Transcript.Text = transcript
	payload.EvidenceRefs[0].TranscriptSpan.EndUTF8Byte = len(transcript)
	if followUp != interviewReportFollowUpNone {
		payload.OpportunityManifest = append(
			payload.OpportunityManifest,
			evidence.Opportunity{
				Sequence:                2,
				QuestionID:              "question-2",
				QuestionType:            "FOLLOW_UP",
				ParentQuestionID:        "question-1",
				ObjectiveID:             "clear_answer",
				QuestionText:            "What changed after the migration?",
				SpeakerParticipantID:    "participant-interviewer",
				AddresseeParticipantIDs: []string{"participant-candidate"},
			},
		)
	}
	if followUp == interviewReportFollowUpAnswered {
		secondText := "Latency fell and releases became safer."
		payload.OpportunityManifest[1].ResponseTurnID = "turn-2"
		payload.ConfirmedTurns = append(
			payload.ConfirmedTurns,
			evidence.ConfirmedTurn{
				TurnID:                  "turn-2",
				Sequence:                2,
				QuestionID:              "question-2",
				RespondentParticipantID: "participant-candidate",
				InteractionMode:         "PUSH_TO_TALK",
				Transcript: evidence.Transcript{
					ID:                    "transcript-2",
					Text:                  secondText,
					EvidenceVersion:       1,
					ASRConfidence:         reportEvidenceUnavailable,
					WordTimestamps:        reportEvidenceUnavailable,
					AlternativeHypotheses: reportEvidenceUnavailable,
				},
				Audio: evidence.Audio{
					Availability: reportEvidenceUnavailable,
					Quality:      reportEvidenceNotAssessed,
					ISE:          reportEvidenceNotAssessed,
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
			evidence.ASRLineage{
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
			evidence.TurnVersion{TurnID: "turn-2", EvidenceVersion: 1},
		)
	}
	return rebuildReportTestSnapshot(t, payload, evaluation.SceneInterview)
}

type ieltsReportProvider struct{}

type ieltsReportProviderPayload struct {
	SchemaVersion string                         `json:"schema_version"`
	Criteria      []ieltsReportProviderCriterion `json:"criteria"`
}

type ieltsReportProviderCriterion struct {
	CriterionID      scoring.IELTSCriterion       `json:"criterion_id"`
	RubricDescriptor string                       `json:"rubric_descriptor,omitempty"`
	Strengths        []ieltsReportProviderFinding `json:"strengths"`
	Improvements     []ieltsReportProviderFinding `json:"improvements"`
	UpgradeExamples  []ieltsReportProviderFinding `json:"upgrade_examples"`
}

type ieltsReportProviderFinding struct {
	TemplateID string                      `json:"template_id"`
	Suggestion string                      `json:"suggestion,omitempty"`
	Evidence   []ieltsReportProviderAnchor `json:"evidence"`
}

type ieltsReportProviderAnchor struct {
	EvidenceRefID string `json:"evidence_ref_id"`
	Quote         string `json:"quote"`
	Occurrence    int    `json:"occurrence"`
}

func (*ieltsReportProvider) AnalyzeIELTSSpeaking(
	_ context.Context,
	input scoring.IELTSSpeakingShadowProviderInput,
) (scoring.IELTSSpeakingShadowProviderResult, error) {
	first := input.Questions[0].Response
	if first == nil {
		panic("IELTS report fixture requires one response")
	}
	payload := ieltsReportProviderPayload{
		SchemaVersion: scoring.IELTSSpeakingShadowProviderSchemaVersion,
		Criteria: make(
			[]ieltsReportProviderCriterion,
			0,
			len(input.AssessableCriteria),
		),
	}
	for _, criterion := range input.AssessableCriteria {
		value := ieltsReportProviderCriterion{
			CriterionID: criterion,
			Strengths: []ieltsReportProviderFinding{{
				TemplateID: "ielts." + strings.ToLower(
					strings.TrimPrefix(string(criterion), "IELTS_"),
				) + ".strength.v1",
				Evidence: []ieltsReportProviderAnchor{{
					EvidenceRefID: first.EvidenceRefID,
					Quote:         first.Transcript,
					Occurrence:    1,
				}},
			}},
			Improvements:    []ieltsReportProviderFinding{},
			UpgradeExamples: []ieltsReportProviderFinding{},
		}
		if criterion == scoring.IELTSCriterionLR ||
			criterion == scoring.IELTSCriterionGRA {
			value.RubricDescriptor = reportIELTSDescriptor(input, criterion)
		}
		payload.Criteria = append(payload.Criteria, value)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return scoring.IELTSSpeakingShadowProviderResult{}, err
	}
	return scoring.IELTSSpeakingShadowProviderResult{
		Payload:   raw,
		Provider:  "provider",
		Model:     "model",
		RequestID: "request-1",
	}, nil
}

func reportIELTSDescriptor(
	input scoring.IELTSSpeakingShadowProviderInput,
	criterion scoring.IELTSCriterion,
) string {
	for _, set := range input.RubricDescriptors {
		if set.CriterionID == criterion {
			return set.Descriptors[5].ID
		}
	}
	panic("IELTS report fixture has no descriptor")
}

func ieltsReportTestResult(
	t *testing.T,
	snapshot evidence.EvidenceSnapshot,
) scoring.IELTSSpeakingShadowResult {
	t.Helper()
	result, err := scoring.NewIELTSSpeakingShadowEngine(
		&ieltsReportProvider{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("evaluate IELTS report fixture: %v", err)
	}
	return result
}

func ieltsReportTestSnapshot(
	t *testing.T,
	answered int,
) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(reportTestEvidencePayload(), &payload); err != nil {
		t.Fatalf("decode IELTS report fixture: %v", err)
	}
	payload.PracticeContext.SceneFamily = string(scene.SceneFamilyExam)
	payload.PracticeContext.SceneModel = string(scene.SceneModelIELTSSpeakingFullMock)
	payload.PracticeContext.Scene = evidence.VersionedRef{
		ID: reportIELTSFullMockSceneID, Version: reportIELTSFullMockSceneVersion,
	}
	payload.PracticeContext.Preparation.BackgroundSnapshotHash =
		reportTestTextHash(reportTestPreparationBackground)
	payload.PracticeContext.PracticeOption = evidence.PracticeOption{
		ID:   "option_ielts_speaking_full_full",
		Type: string(scene.PracticeOptionFullSimulation),
	}
	payload.PracticeContext.UserRole = "考生"
	payload.PracticeContext.FacilitatorRole = "IELTS 口语考官"
	payload.PracticeContext.PracticeGoal =
		"适应真实三段式流程，并在不同题型中保持连贯自然的表达。"
	payload.PracticeContext.PracticeObjectives = []evidence.Objective{
		{ID: "part_1_familiar_topics", Description: "Answer familiar-topic questions directly with relevant detail."},
		{ID: "part_2_long_turn", Description: "Deliver a coherent long turn that covers every cue-card point."},
		{ID: "part_3_discussion", Description: "Develop abstract ideas with reasons, examples, and comparisons."},
	}
	payload.PracticeContext.TaskContext = evidence.TaskContext{
		PublicSceneBrief: "按 Part 1、Part 2、Part 3 连续完成一轮 IELTS 口语完整模考。",
		PersonaSummary: "A neutral IELTS speaking examiner who follows the frozen " +
			"three-part mock-test sequence, asks exactly one item at a time, and " +
			"never teaches or scores during the simulation.",
		FocusAreas: []string{
			"part_1_familiar_topics", "part_2_long_turn", "part_3_discussion", "section_transition",
		},
		SuggestedDurationSeconds: 900,
	}
	payload.PracticeContext.TaskBlueprints = make([]string, scoring.IELTSQuestionCount)
	for index := range payload.PracticeContext.TaskBlueprints {
		payload.PracticeContext.TaskBlueprints[index] = fmt.Sprintf(
			"IELTS question blueprint %d", index+1,
		)
	}
	payload.OpportunityManifest = make(
		[]evidence.Opportunity, 0, scoring.IELTSQuestionCount,
	)
	payload.ConfirmedTurns = make([]evidence.ConfirmedTurn, 0, answered)
	payload.EvidenceRefs = make([]evidence.Ref, 0, answered)
	payload.ProviderLineage.ASR = make([]evidence.ASRLineage, 0, answered)
	payload.VersionManifest.TurnEvidence = make([]evidence.TurnVersion, 0, answered)
	for index := 1; index <= scoring.IELTSQuestionCount; index++ {
		questionID := fmt.Sprintf("question-%d", index)
		turnID := fmt.Sprintf("turn-%d", index)
		transcriptID := fmt.Sprintf("transcript-%d", index)
		candidateID := fmt.Sprintf("candidate-%d", index)
		transcript := fmt.Sprintf(
			"I explain answer %d clearly with a concrete example.", index,
		)
		objectiveID := "part_3_discussion"
		if index <= 8 {
			objectiveID = "part_1_familiar_topics"
		} else if index == 9 {
			objectiveID = "part_2_long_turn"
		}
		opportunity := evidence.Opportunity{
			Sequence:                index,
			QuestionID:              questionID,
			QuestionType:            "PRIMARY",
			ObjectiveID:             objectiveID,
			QuestionText:            fmt.Sprintf("IELTS question %d?", index),
			SpeakerParticipantID:    "participant-interviewer",
			AddresseeParticipantIDs: []string{"participant-candidate"},
		}
		if index <= answered {
			opportunity.ResponseTurnID = turnID
			payload.ConfirmedTurns = append(payload.ConfirmedTurns, evidence.ConfirmedTurn{
				TurnID:                  turnID,
				Sequence:                index,
				QuestionID:              questionID,
				RespondentParticipantID: "participant-candidate",
				InteractionMode:         "PUSH_TO_TALK",
				Transcript: evidence.Transcript{
					ID:                    transcriptID,
					Text:                  transcript,
					EvidenceVersion:       1,
					ASRConfidence:         reportEvidenceUnavailable,
					WordTimestamps:        reportEvidenceUnavailable,
					AlternativeHypotheses: reportEvidenceUnavailable,
				},
				Audio: evidence.Audio{
					Availability: reportEvidenceUnavailable,
					Quality:      reportEvidenceNotAssessed,
					ISE:          reportEvidenceNotAssessed,
				},
			})
			payload.EvidenceRefs = append(payload.EvidenceRefs, evidence.Ref{
				TurnID:  turnID,
				Speaker: "USER",
				TranscriptSpan: evidence.TranscriptSpan{
					StartUTF8Byte: 0, EndUTF8Byte: len(transcript),
				},
				Quality: evidence.Quality{
					Audio: reportEvidenceNotAssessed, ASRConfidence: reportEvidenceUnavailable,
					Alignment: reportEvidenceUnavailable, ISE: reportEvidenceNotAssessed,
				},
				Lineage: evidence.RefLineage{
					TranscriptID: transcriptID, CandidateID: candidateID, EvidenceVersion: 1,
					ASRProvider: "qianwen", ASRModel: "paraformer-v2",
				},
			})
			payload.ProviderLineage.ASR = append(payload.ProviderLineage.ASR, evidence.ASRLineage{
				TurnID: turnID, TranscriptID: transcriptID, CandidateID: candidateID,
				EvidenceVersion: 1, Provider: "qianwen", Model: "paraformer-v2",
				ProviderRequestID: fmt.Sprintf("provider-request-%d", index),
			})
			payload.VersionManifest.TurnEvidence = append(
				payload.VersionManifest.TurnEvidence,
				evidence.TurnVersion{TurnID: turnID, EvidenceVersion: 1},
			)
		}
		payload.OpportunityManifest = append(payload.OpportunityManifest, opportunity)
	}
	return rebuildReportTestSnapshot(t, payload, evaluation.SceneIELTSSpeaking)
}

func rebuildReportTestSnapshot(
	t *testing.T,
	payload evidence.SnapshotPayload,
	sceneType evaluation.SceneType,
) evidence.EvidenceSnapshot {
	t.Helper()
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize provisional report fixture: %v", err)
	}
	sourceManifestHash, err := evidence.SourceManifestHash(provisional)
	if err != nil {
		t.Fatalf("derive report fixture source manifest: %v", err)
	}
	snapshotID := evidence.DeriveSnapshotID(
		reportTestOwner,
		"practice-session-1",
		evaluation.ScopeSession,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		turn := payload.ConfirmedTurns[index]
		payload.EvidenceRefs[index].SnapshotID = snapshotID
		payload.EvidenceRefs[index].EvidenceRefID = evidence.StableRefID(
			snapshotID,
			turn.TurnID,
			turn.Transcript.EvidenceVersion,
			turn.Audio.ChecksumSHA256,
		)
	}
	canonical, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize report fixture: %v", err)
	}
	snapshot := evidence.EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        reportTestOwner,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              evaluation.ScopeSession,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("report evidence fixture is invalid")
	}
	return snapshot
}

func reportTestTextHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func jsonContainsExactKey(value any, wanted string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == wanted || jsonContainsExactKey(child, wanted) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonContainsExactKey(child, wanted) {
				return true
			}
		}
	}
	return false
}

func reportTestEvidencePayload() json.RawMessage {
	return json.RawMessage(`{
		"practice_context":{
			"practice_session_id":"practice-session-1",
			"session_snapshot_id":"practice-snapshot-1",
			"session_version":2,
			"plan_revision":1,
			"scene_family":"INTERVIEW",
			"scene_model":"INTERVIEW_BASIC_DIALOGUE",
			"scene":{"id":"scene-1","version":1},
			"practice_option":{"id":"practice-option-1","type":"FULL_SIMULATION"},
			"user_role":"candidate",
			"facilitator_role":"interviewer",
			"practice_goal":"answer an interview question",
			"preparation":{"snapshot_id":"preparation-snapshot-1","source_profile_id":"profile-1","source_version":1},
			"task_context":{"public_scene_brief":"A structured interview.","persona_summary":"A professional interviewer.","focus_areas":["clarity"],"suggested_duration_seconds":300},
			"task_blueprints":["answer one question"],
			"participants":[
				{"participant_id":"participant-interviewer","role":"FACILITATOR","order":1},
				{"participant_id":"participant-candidate","role":"LEARNER","order":2}
			],
			"practice_objectives":[{"id":"clear_answer","description":"Answer the interview question clearly."}]
		},
		"opportunity_manifest":[{
			"sequence":1,"question_id":"question-1","question_type":"PRIMARY","objective_id":"clear_answer",
			"question_text":"Tell me about a migration you led.","speaker_participant_id":"participant-interviewer",
			"addressee_participant_ids":["participant-candidate"],"response_turn_id":"turn-1"
		}],
		"confirmed_turns":[{
			"turn_id":"turn-1","sequence":1,"question_id":"question-1","respondent_participant_id":"participant-candidate","interaction_mode":"PUSH_TO_TALK",
			"transcript":{"transcript_id":"transcript-1","text":"I led the migration.","evidence_version":1,"asr_confidence":"UNAVAILABLE","word_timestamps":"UNAVAILABLE","alternative_hypotheses":"UNAVAILABLE"},
			"audio":{"availability":"UNAVAILABLE","quality":"NOT_ASSESSED","ise":"NOT_ASSESSED"}
		}],
		"evidence_refs":[{
			"evidence_ref_id":"evidence_ref_pending","turn_id":"turn-1","speaker":"USER","transcript_span":{"start_utf8_byte":0,"end_utf8_byte":20},
			"quality":{"audio":"NOT_ASSESSED","asr_confidence":"UNAVAILABLE","alignment":"UNAVAILABLE","ise":"NOT_ASSESSED"},
			"lineage":{"transcript_id":"transcript-1","candidate_id":"candidate-1","evidence_version":1,"asr_provider":"qianwen","asr_model":"paraformer-v2"}
		}],
		"provider_lineage":{
			"asr":[{"turn_id":"turn-1","transcript_id":"transcript-1","candidate_id":"candidate-1","evidence_version":1,"provider":"qianwen","model":"paraformer-v2","provider_request_id":"provider-request-1"}],
			"unavailable_artifacts":{"word_timestamps":"UNAVAILABLE","asr_confidence":"UNAVAILABLE","asr_n_best":"UNAVAILABLE","audio_quality":"NOT_ASSESSED","ise":"NOT_ASSESSED","feature_bundle":"UNAVAILABLE"}
		},
		"version_manifest":{
			"schema_version":"evidence-snapshot/v3","source_manifest_version":"evidence-source-manifest/v3","practice_session":2,"practice_snapshot":"practice-snapshot-1","plan_revision":1,
			"turn_evidence":[{"turn_id":"turn-1","evidence_version":1}],"audio_quality":"UNAVAILABLE","ise":"UNAVAILABLE","feature_bundle":"UNAVAILABLE",
			"scoring_prompt":"UNAVAILABLE","rubric":"UNAVAILABLE","gate":"UNAVAILABLE","aggregation":"UNAVAILABLE","calibration":"UNAVAILABLE","pipeline":"UNAVAILABLE"
		}
	}`)
}
