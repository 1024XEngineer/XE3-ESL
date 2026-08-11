package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testOwnerA                        = "10000000-0000-4000-8000-000000000001"
	testOwnerB                        = "20000000-0000-4000-8000-000000000002"
	evidenceTestPreparationBackground = "Evaluation evidence fixture background."
	evidenceUnavailable               = "UNAVAILABLE"
	evidenceNotAssessed               = "NOT_ASSESSED"
	ieltsPostgresPart1QuestionCount   = 3
	ieltsPostgresPart2QuestionCount   = 1
	ieltsPostgresQuestionCount        = 7
)

func testActor(owner string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    owner,
		SessionID: "50000000-0000-4000-8000-000000000005",
	}
}

func testActorContext(owner string) context.Context {
	return requestcontext.WithActor(context.Background(), testActor(owner))
}

func validCreateRequest() evaluationcore.CreateRequest {
	return evaluationcore.CreateRequest{
		PracticeSessionID: "practice-session-1",
		InputSnapshotID:   "snapshot_provisional",
		InputRevision:     1,
		Scope:             evaluationcore.ScopeSession,
		SceneType:         evaluationcore.SceneInterview,
		Channels:          []evaluationcore.Channel{evaluationcore.ChannelScene},
		SceneStrategyRef:  "interview/v1",
		PipelineVersion:   "pipeline/v1",
		ClientRequestID:   "trace-1",
	}
}

func evidenceTextHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func validEvidenceSnapshotPayload() json.RawMessage {
	return json.RawMessage(`{
		"practice_context":{
			"practice_session_id":"practice-session-1",
			"session_snapshot_id":"practice-snapshot-1",
			"session_version":2,
			"plan_revision":1,
			"practice_experience":"INTERVIEW",
			"scene_category":"INTERVIEW_PROFESSIONAL",
			"practice_mode":"FULL_SIMULATION",
			"evaluation_policy_ref":"interview.shadow.evaluation.v1",
			"scene":{"id":"scene-1","version":1},
			"practice_option":{"id":"practice-option-1","practice_mode":"FULL_SIMULATION"},
			"user_role":"candidate",
			"facilitator_role":"interviewer",
			"practice_goal":"answer an interview question",
			"preparation":{
				"snapshot_id":"preparation-snapshot-1",
				"source_profile_id":"profile-1",
				"source_version":1
			},
			"task_context":{
				"public_scene_brief":"A structured interview.",
				"persona_summary":"A professional interviewer.",
				"focus_areas":["clarity"],
				"suggested_duration_seconds":300
			},
			"task_blueprints":["answer one question"],
			"participants":[
				{"participant_id":"participant-interviewer","role":"FACILITATOR","order":1},
				{"participant_id":"participant-candidate","role":"LEARNER","order":2}
			],
			"practice_objectives":[{
				"id":"clear_answer",
				"description":"Answer the interview question clearly."
			}]
		},
		"opportunity_manifest":[{
			"sequence":1,
			"question_id":"question-1",
			"question_type":"PRIMARY",
			"objective_id":"clear_answer",
			"question_text":"Tell me about a migration you led.",
			"speaker_participant_id":"participant-interviewer",
			"addressee_participant_ids":["participant-candidate"],
			"response_turn_id":"turn-1"
		}],
		"confirmed_turns":[{
			"turn_id":"turn-1",
			"sequence":1,
			"question_id":"question-1",
			"respondent_participant_id":"participant-candidate",
			"interaction_mode":"PUSH_TO_TALK",
			"transcript":{
				"transcript_id":"transcript-1",
				"text":"I led the migration.",
				"evidence_version":1,
				"asr_confidence":"UNAVAILABLE",
				"word_timestamps":"UNAVAILABLE",
				"alternative_hypotheses":"UNAVAILABLE"
			},
			"audio":{"availability":"UNAVAILABLE","quality":"NOT_ASSESSED","ise":"NOT_ASSESSED"}
		}],
		"evidence_refs":[{
			"evidence_ref_id":"evidence_ref_pending",
			"turn_id":"turn-1",
			"speaker":"USER",
			"transcript_span":{"start_utf8_byte":0,"end_utf8_byte":20},
			"quality":{
				"audio":"NOT_ASSESSED",
				"asr_confidence":"UNAVAILABLE",
				"alignment":"UNAVAILABLE",
				"ise":"NOT_ASSESSED"
			},
			"lineage":{
				"transcript_id":"transcript-1",
				"candidate_id":"candidate-1",
				"evidence_version":1,
				"asr_provider":"qianwen",
				"asr_model":"paraformer-v2"
			}
		}],
		"provider_lineage":{
			"asr":[{
				"turn_id":"turn-1",
				"transcript_id":"transcript-1",
				"candidate_id":"candidate-1",
				"evidence_version":1,
				"provider":"qianwen",
				"model":"paraformer-v2",
				"provider_request_id":"provider-request-1"
			}],
			"unavailable_artifacts":{
				"word_timestamps":"UNAVAILABLE",
				"asr_confidence":"UNAVAILABLE",
				"asr_n_best":"UNAVAILABLE",
				"audio_quality":"NOT_ASSESSED",
				"ise":"NOT_ASSESSED",
				"feature_bundle":"UNAVAILABLE"
			}
		},
		"version_manifest":{
			"schema_version":"evidence-snapshot/v3",
			"source_manifest_version":"evidence-source-manifest/v3",
			"practice_session":2,
			"practice_snapshot":"practice-snapshot-1",
			"plan_revision":1,
			"turn_evidence":[{"turn_id":"turn-1","evidence_version":1}],
			"audio_quality":"UNAVAILABLE",
			"ise":"UNAVAILABLE",
			"feature_bundle":"UNAVAILABLE",
			"scoring_prompt":"UNAVAILABLE",
			"rubric":"UNAVAILABLE",
			"gate":"UNAVAILABLE",
			"aggregation":"UNAVAILABLE",
			"calibration":"UNAVAILABLE",
			"pipeline":"UNAVAILABLE"
		}
	}`)
}

func postgresTestSnapshot(
	t *testing.T,
	payload evidence.SnapshotPayload,
	sceneType evaluationcore.SceneType,
) evidence.EvidenceSnapshot {
	t.Helper()
	return postgresTestSnapshotForOwner(t, payload, sceneType, testOwnerA)
}

func postgresTestSnapshotForOwner(
	t *testing.T,
	payload evidence.SnapshotPayload,
	sceneType evaluationcore.SceneType,
	ownerUserID string,
) evidence.EvidenceSnapshot {
	t.Helper()
	return postgresTestSnapshotForOwnerAndSession(
		t,
		payload,
		sceneType,
		ownerUserID,
		"practice-session-1",
	)
}

func postgresTestSnapshotForOwnerAndSession(
	t *testing.T,
	payload evidence.SnapshotPayload,
	sceneType evaluationcore.SceneType,
	ownerUserID string,
	practiceSessionID string,
) evidence.EvidenceSnapshot {
	t.Helper()
	payload.PracticeContext.PracticeSessionID = practiceSessionID
	for index := range payload.EvidenceRefs {
		payload.EvidenceRefs[index].SnapshotID = ""
		payload.EvidenceRefs[index].EvidenceRefID = ""
	}
	provisional, err := evidence.CanonicalJSON(payload)
	if err != nil {
		t.Fatalf("canonicalize provisional EvidenceSnapshot: %v", err)
	}
	sourceManifestHash, err := evidence.SourceManifestHash(provisional)
	if err != nil {
		t.Fatalf("derive EvidenceSnapshot source manifest: %v", err)
	}
	snapshotID := evidence.DeriveSnapshotID(
		ownerUserID,
		practiceSessionID,
		evaluationcore.ScopeSession,
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
		t.Fatalf("canonicalize EvidenceSnapshot: %v", err)
	}
	snapshot := evidence.EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        ownerUserID,
		PracticeSessionID:  practiceSessionID,
		InputRevision:      1,
		Scope:              evaluationcore.ScopeSession,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(canonical),
		Payload:            canonical,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatal("PostgreSQL EvidenceSnapshot fixture is invalid")
	}
	return snapshot
}

func interviewShadowTestSnapshot(t *testing.T, transcript string) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(validEvidenceSnapshotPayload(), &payload); err != nil {
		t.Fatalf("decode Interview EvidenceSnapshot fixture: %v", err)
	}
	payload.PracticeContext.Preparation.BackgroundSnapshotHash = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	payload.ConfirmedTurns[0].Transcript.Text = transcript
	payload.EvidenceRefs[0].TranscriptSpan.EndUTF8Byte = len(transcript)
	return postgresTestSnapshot(t, payload, evaluationcore.SceneInterview)
}

func generalSceneTestSnapshot(
	t *testing.T,
	sceneType evaluationcore.SceneType,
	experience scene.PracticeExperience,
	category scene.SceneCategory,
	mode scene.PracticeMode,
	transcript string,
) evidence.EvidenceSnapshot {
	t.Helper()
	snapshot := interviewShadowTestSnapshot(t, transcript)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.PracticeContext.PracticeExperience = string(experience)
	payload.PracticeContext.SceneCategory = string(category)
	payload.PracticeContext.PracticeMode = string(mode)
	payload.PracticeContext.PracticeOption.Mode = string(mode)
	payload.PracticeContext.EvaluationPolicyRef = "general.scene.evaluation.v1"
	if experience == scene.PracticeExperienceIELTSSpeaking {
		payload.PracticeContext.EvaluationPolicyRef =
			scoring.IELTSSpeakingPracticeEvaluationPolicyRef
		payload.PracticeContext.IELTSAssignment = &evidence.IELTSAssignment{
			BankID: "ielts-bank-1",
			Season: "2026-05",
			Mode:   string(mode),
			Parts: []evidence.IELTSAssignmentPart{{
				Part:           string(mode),
				SourceID:       "part-1-set-1",
				TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints),
			}},
		}
	}
	payload.PracticeContext.Scene.ID = "scene-general-1"
	return postgresTestSnapshot(t, payload, sceneType)
}

func ieltsSpeakingTestSnapshot(t *testing.T, answered int) evidence.EvidenceSnapshot {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(validEvidenceSnapshotPayload(), &payload); err != nil {
		t.Fatalf("decode IELTS EvidenceSnapshot fixture: %v", err)
	}
	payload.PracticeContext.PracticeExperience =
		string(scene.PracticeExperienceIELTSSpeaking)
	payload.PracticeContext.SceneCategory =
		string(scene.SceneCategoryIELTSSpeaking)
	payload.PracticeContext.PracticeMode = string(scene.PracticeModeFullMock)
	payload.PracticeContext.EvaluationPolicyRef =
		scoring.IELTSSpeakingFullMockEvaluationPolicyRef
	payload.PracticeContext.Scene = evidence.VersionedRef{
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
	payload.PracticeContext.PracticeGoal = "完成一轮 IELTS 口语完整模考。"
	payload.PracticeContext.PracticeObjectives = []evidence.Objective{
		{ID: "part_1_familiar_topics", Description: "Answer familiar-topic questions."},
		{ID: "part_2_long_turn", Description: "Deliver a coherent long turn."},
		{ID: "part_3_discussion", Description: "Develop abstract ideas."},
	}
	payload.PracticeContext.TaskContext = evidence.TaskContext{
		PublicSceneBrief:         "完成 IELTS 口语完整模考。",
		PersonaSummary:           "A neutral IELTS speaking examiner.",
		FocusAreas:               []string{"part_1", "part_2", "part_3"},
		SuggestedDurationSeconds: 900,
	}
	payload.PracticeContext.TaskBlueprints = make([]string, ieltsPostgresQuestionCount)
	payload.PracticeContext.IELTSAssignment = &evidence.IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   string(scene.PracticeModeFullMock),
	}
	for index := range payload.PracticeContext.TaskBlueprints {
		payload.PracticeContext.TaskBlueprints[index] = fmt.Sprintf(
			"IELTS question %d?",
			index+1,
		)
	}
	payload.PracticeContext.IELTSAssignment.Parts = []evidence.IELTSAssignmentPart{
		{
			Part:           string(scene.PracticeModePart1),
			SourceID:       "part-1-set-1",
			TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[:ieltsPostgresPart1QuestionCount]),
		},
		{
			Part:           string(scene.PracticeModePart2),
			SourceID:       "topic-group-1",
			TopicTitle:     "Learning a skill",
			CueCard:        "Describe a skill you would like to learn.",
			TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[ieltsPostgresPart1QuestionCount : ieltsPostgresPart1QuestionCount+ieltsPostgresPart2QuestionCount]),
		},
		{
			Part:           string(scene.PracticeModePart3),
			SourceID:       "topic-group-1",
			TopicTitle:     "Learning a skill",
			TurnBlueprints: slices.Clone(payload.PracticeContext.TaskBlueprints[ieltsPostgresPart1QuestionCount+ieltsPostgresPart2QuestionCount:]),
		},
	}
	payload.OpportunityManifest = make([]evidence.Opportunity, 0, ieltsPostgresQuestionCount)
	payload.ConfirmedTurns = make([]evidence.ConfirmedTurn, 0, answered)
	payload.EvidenceRefs = make([]evidence.Ref, 0, answered)
	payload.ProviderLineage.ASR = make([]evidence.ASRLineage, 0, answered)
	payload.VersionManifest.TurnEvidence = make([]evidence.TurnVersion, 0, answered)
	for index := 1; index <= ieltsPostgresQuestionCount; index++ {
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
		if index <= ieltsPostgresPart1QuestionCount {
			objectiveID = "part_1_familiar_topics"
		} else if index ==
			ieltsPostgresPart1QuestionCount+ieltsPostgresPart2QuestionCount {
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
					ASRConfidence:         evidenceUnavailable,
					WordTimestamps:        evidenceUnavailable,
					AlternativeHypotheses: evidenceUnavailable,
				},
				Audio: evidence.Audio{
					Availability: evidenceUnavailable,
					Quality:      evidenceNotAssessed,
					ISE:          evidenceNotAssessed,
				},
			})
			payload.EvidenceRefs = append(payload.EvidenceRefs, evidence.Ref{
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
			})
			payload.ProviderLineage.ASR = append(payload.ProviderLineage.ASR, evidence.ASRLineage{
				TurnID:            turnID,
				TranscriptID:      transcriptID,
				CandidateID:       candidateID,
				EvidenceVersion:   1,
				Provider:          "qianwen",
				Model:             "paraformer-v2",
				ProviderRequestID: fmt.Sprintf("provider-request-%d", index),
			})
			payload.VersionManifest.TurnEvidence = append(
				payload.VersionManifest.TurnEvidence,
				evidence.TurnVersion{TurnID: turnID, EvidenceVersion: 1},
			)
		}
		payload.OpportunityManifest = append(payload.OpportunityManifest, opportunity)
	}
	return postgresTestSnapshot(t, payload, evaluationcore.SceneIELTSSpeaking)
}

func persistEvidenceSnapshotFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	snapshot evidence.EvidenceSnapshot,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO evaluation_evidence_snapshots (
			id, owner_user_id, practice_session_id, scope, scene_type,
			input_revision, source_manifest_hash, snapshot_hash,
			canonical_payload, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, snapshot.ID, snapshot.OwnerUserID, snapshot.PracticeSessionID,
		snapshot.Scope, snapshot.SceneType, snapshot.InputRevision,
		snapshot.SourceManifestHash[:], snapshot.SnapshotHash[:],
		snapshot.Payload, snapshot.CreatedAt); err != nil {
		t.Fatalf("persist EvidenceSnapshot fixture: %v", err)
	}
}

type generalSceneProviderStub struct {
	calls int
}

func (provider *generalSceneProviderStub) AnalyzeGeneralScene(
	_ context.Context,
	input scoring.GeneralSceneProviderInput,
) (scoring.GeneralSceneProviderResult, error) {
	provider.calls++
	var response *scoring.GeneralSceneResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil {
			response = opportunity.Response
			break
		}
	}
	if response == nil {
		panic("general Scene provider fixture requires a response")
	}
	dimensions := make([]map[string]any, 0, len(input.AssessableDimensions))
	for index, dimension := range input.AssessableDimensions {
		dimensions = append(dimensions, map[string]any{
			"dimension_id": dimension,
			"score":        60 + index*10,
			"strengths":    []any{},
			"improvements": []any{map[string]any{
				"template_id": string(dimension) + ":IMPROVEMENT:v1",
				"evidence": []any{map[string]any{
					"evidence_ref_id": response.EvidenceRefID,
					"quote":           response.Transcript,
					"occurrence":      1,
				}},
			}},
			"recommended_examples": []any{},
		})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": scoring.GeneralSceneProviderSchemaVersion,
		"dimensions":     dimensions,
	})
	return scoring.GeneralSceneProviderResult{
		Payload: payload, Provider: "qianwen", Model: "qwen-plus",
		RequestID: "provider-request-1",
	}, err
}

type stubInterviewShadowProvider struct{}

func (*stubInterviewShadowProvider) AnalyzeInterview(
	_ context.Context,
	input scoring.InterviewShadowProviderInput,
) (scoring.InterviewShadowProviderResult, error) {
	var first *scoring.InterviewProviderResponse
	for _, opportunity := range input.Opportunities {
		if opportunity.Response != nil {
			first = opportunity.Response
			break
		}
	}
	if first == nil {
		panic("Interview provider fixture requires a response")
	}
	dimensions := make([]map[string]any, 0, len(input.AssessableDimensions))
	for _, dimension := range input.AssessableDimensions {
		dimensions = append(dimensions, map[string]any{
			"dimension_id": dimension,
			"score":        75,
			"strengths": []any{map[string]any{
				"template_id": string(dimension) + ":STRENGTH:v1",
				"evidence": []any{map[string]any{
					"evidence_ref_id": first.EvidenceRefID,
					"quote":           first.Transcript,
					"occurrence":      1,
				}},
			}},
			"improvements":            []any{},
			"recommended_expressions": []any{},
		})
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": scoring.InterviewShadowProviderSchemaVersion,
		"dimensions":     dimensions,
	})
	return scoring.InterviewShadowProviderResult{
		Payload: payload, Provider: "qianwen", Model: "qwen-plus",
		RequestID: "provider-request-1",
	}, err
}

type ieltsProviderStub struct{}

func (*ieltsProviderStub) AnalyzeIELTSCriterion(
	_ context.Context,
	request scoring.IELTSSpeakingCriterionProviderRequest,
) (scoring.IELTSSpeakingShadowProviderResult, error) {
	input := request.Input
	first := input.Questions[0].Response
	if first == nil {
		panic("IELTS provider fixture requires a response")
	}
	criteria := make([]map[string]any, 0, len(input.AssessableCriteria))
	for _, criterion := range input.AssessableCriteria {
		value := map[string]any{
			"criterion_id": criterion,
			"strengths": []any{map[string]any{
				"template_id": "ielts." + strings.ToLower(
					strings.TrimPrefix(string(criterion), "IELTS_"),
				) + ".strength.v1",
				"evidence": []any{map[string]any{
					"evidence_ref_id": first.EvidenceRefID,
					"quote":           first.Transcript,
					"occurrence":      1,
				}},
			}},
			"improvements":     []any{},
			"upgrade_examples": []any{},
		}
		if criterion == scoring.IELTSCriterionLR || criterion == scoring.IELTSCriterionGRA {
			descriptor, _, ok := scoring.MapIELTSBand(criterion, 5)
			if !ok {
				panic("missing IELTS rubric descriptor fixture")
			}
			value["rubric_descriptor"] = descriptor
		}
		criteria = append(criteria, value)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": scoring.IELTSSpeakingShadowProviderSchemaVersion,
		"criteria":       criteria,
	})
	return scoring.IELTSSpeakingShadowProviderResult{
		Payload: payload, Provider: "qianwen", Model: "qwen-plus",
		RequestID: "provider-request-" + strings.ToLower(
			strings.TrimPrefix(
				string(input.AssessableCriteria[0]),
				"IELTS_",
			),
		),
	}, err
}
