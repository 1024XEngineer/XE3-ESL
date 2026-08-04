package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testOwnerA = "10000000-0000-4000-8000-000000000001"
	testEvalID = "30000000-0000-4000-8000-000000000003"
	testRevID  = "40000000-0000-4000-8000-000000000004"

	evidenceTestPreparationBackground = "Evaluation evidence fixture background."
	evidenceUnavailable               = "UNAVAILABLE"
	evidenceNotAssessed               = "NOT_ASSESSED"
)

func testActor(owner string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    owner,
		SessionID: "50000000-0000-4000-8000-000000000005",
	}
}

func validEvaluation() evaluation.Evaluation {
	now := time.Now().UTC()
	return evaluation.Evaluation{
		ID:                testEvalID,
		OwnerUserID:       testOwnerA,
		PracticeSessionID: "session-1",
		InputSnapshotID:   "snapshot-1",
		InputRevision:     1,
		Scope:             evaluation.ScopeSession,
		SceneType:         evaluation.SceneInterview,
		CreatedAt:         now,
		Revision: evaluation.Revision{
			ID:               testRevID,
			EvaluationID:     testEvalID,
			OwnerUserID:      testOwnerA,
			Number:           1,
			Channels:         []evaluation.Channel{evaluation.ChannelScene},
			SceneStrategyRef: "interview/v1",
			PipelineVersion:  "pipeline/v1",
			SchemaVersion:    evaluation.SchemaVersion,
			Status:           evaluation.StatusQueued,
			ClientRequestID:  "trace-1",
			CreatedAt:        now,
			UpdatedAt:        now,
		},
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
			"scene_family":"INTERVIEW",
			"scene_model":"INTERVIEW_BASIC_DIALOGUE",
			"scene":{"id":"scene-1","version":1},
			"practice_option":{"id":"practice-option-1","type":"FULL_SIMULATION"},
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
