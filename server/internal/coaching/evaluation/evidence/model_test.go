package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

const evidenceTestPreparationBackground = "Evaluation evidence fixture background."

func TestCanonicalEvidencePayloadIsStableAndRejectsStorageLocators(
	t *testing.T,
) {
	first := json.RawMessage(`{
		"version_manifest":{"schema":"evidence/v2","policy":"policy/v1"},
		"confirmed_turns":[{"turn_id":"turn-1","transcript":"hello"}],
		"practice_context":{"mode":"interview"},
		"provider_lineage":{"transcript_provider":"practice"},
		"evidence_refs":[{"evidence_ref_id":"ref-1","turn_id":"turn-1"}],
		"opportunity_manifest":{"question_count":1}
	}`)
	second := json.RawMessage(`{
		"practice_context": {"mode":"interview"},
		"opportunity_manifest": {"question_count":1},
		"confirmed_turns": [{"transcript":"hello","turn_id":"turn-1"}],
		"evidence_refs": [{"turn_id":"turn-1","evidence_ref_id":"ref-1"}],
		"provider_lineage": {"transcript_provider":"practice"},
		"version_manifest": {"policy":"policy/v1","schema":"evidence/v2"}
	}`)
	firstCanonical, err := CanonicalPayload(first)
	if err != nil {
		t.Fatalf("canonicalize first payload: %v", err)
	}
	secondCanonical, err := CanonicalPayload(second)
	if err != nil {
		t.Fatalf("canonicalize second payload: %v", err)
	}
	if !bytes.Equal(firstCanonical, secondCanonical) {
		t.Fatalf(
			"canonical payloads differ:\n%s\n%s",
			firstCanonical,
			secondCanonical,
		)
	}

	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"practice_context":{}}`),
		json.RawMessage(`{
			"practice_context":{},
			"opportunity_manifest":{},
			"confirmed_turns":{},
			"evidence_refs":[],
			"provider_lineage":{},
			"version_manifest":{}
		}`),
		json.RawMessage(`{
			"practice_context":{},
			"opportunity_manifest":{},
			"confirmed_turns":[],
			"evidence_refs":[],
			"provider_lineage":{},
			"version_manifest":{},
			"unreviewed_extension":true
		}`),
		json.RawMessage(`{
			"practice_context":{},
			"opportunity_manifest":{},
			"confirmed_turns":[{
				"audio":{"object_key":"private/audio.wav"}
			}],
			"evidence_refs":[],
			"provider_lineage":{},
			"version_manifest":{}
		}`),
		json.RawMessage(`{
			"practice_context":{},
			"opportunity_manifest":{},
			"confirmed_turns":[],
			"evidence_refs":[{
				"audio":{"signed_url":"https://private.invalid"}
			}],
			"provider_lineage":{},
			"version_manifest":{}
		}`),
	} {
		if _, err := CanonicalPayload(invalid); !errors.Is(
			err,
			evaluation.ErrInvalidRequest,
		) {
			t.Errorf("invalid payload error = %v", err)
		}
	}
}

func TestEvidenceSnapshotValidVerifiesCanonicalPayloadHash(t *testing.T) {
	sourceManifestHash, err := SourceManifestHash(
		evidenceSnapshotPayloadForID("snapshot_provisional"),
	)
	if err != nil {
		t.Fatalf("derive source manifest: %v", err)
	}
	snapshotID := DeriveSnapshotID(
		testOwnerA,
		"practice-session-1",
		evaluation.ScopeSession,
		sourceManifestHash,
	)
	payload, err := CanonicalPayload(
		evidenceSnapshotPayloadForID(snapshotID),
	)
	if err != nil {
		t.Fatalf("canonicalize fixture: %v", err)
	}
	snapshot := EvidenceSnapshot{
		ID:                 snapshotID,
		OwnerUserID:        testOwnerA,
		PracticeSessionID:  "practice-session-1",
		InputRevision:      1,
		Scope:              evaluation.ScopeSession,
		SceneType:          evaluation.SceneInterview,
		SourceManifestHash: sourceManifestHash,
		SnapshotHash:       sha256.Sum256(payload),
		Payload:            payload,
		CreatedAt:          time.Now().UTC(),
	}
	if !snapshot.Valid() {
		t.Fatalf("valid snapshot rejected: %#v", snapshot)
	}
	snapshot.SnapshotHash[0] ^= 0xff
	if snapshot.Valid() {
		t.Fatal("snapshot with mismatched canonical hash is valid")
	}
}

func TestNormalizeEvidenceSnapshotCommandRequiresSourceManifestHash(
	t *testing.T,
) {
	command := validEvidenceSnapshotCommand()
	command.SourceManifestHash = [sha256.Size]byte{}
	if _, err := normalizeEvidenceSnapshotCommand(command); !errors.Is(
		err,
		evaluation.ErrInvalidRequest,
	) {
		t.Fatalf("zero source manifest hash error = %v", err)
	}
}

func TestEvidenceSourceManifestBindsCompleteConfirmedTurn(t *testing.T) {
	payload := evidenceSnapshotPayloadForID("snapshot_provisional")
	original, err := SourceManifestHash(payload)
	if err != nil {
		t.Fatalf("derive original source manifest: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode EvidenceSnapshot payload: %v", err)
	}
	turn := decoded["confirmed_turns"].([]any)[0].(map[string]any)
	turn["interaction_mode"] = "TEXT"
	changedPayload, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode changed EvidenceSnapshot payload: %v", err)
	}
	changed, err := SourceManifestHash(changedPayload)
	if err != nil {
		t.Fatalf("derive changed source manifest: %v", err)
	}
	if changed == original {
		t.Fatal("confirmed Turn source change did not change manifest hash")
	}
}

func TestNormalizeEvidenceSnapshotCommandRejectsTamperedEvidenceBindings(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "reference points at another turn",
			mutate: func(payload map[string]any) {
				payload["evidence_refs"].([]any)[0].(map[string]any)["turn_id"] = "turn-other"
			},
		},
		{
			name: "transcript span does not bind to text",
			mutate: func(payload map[string]any) {
				payload["evidence_refs"].([]any)[0].(map[string]any)["transcript_span"].(map[string]any)["end_utf8_byte"] = 19
			},
		},
		{
			name: "invented ASR confidence",
			mutate: func(payload map[string]any) {
				payload["confirmed_turns"].([]any)[0].(map[string]any)["transcript"].(map[string]any)["asr_confidence"] = "0.99"
			},
		},
		{
			name: "unknown nested provider field",
			mutate: func(payload map[string]any) {
				payload["provider_lineage"].(map[string]any)["provider_key"] = "unreviewed"
			},
		},
		{
			name: "payload belongs to another session",
			mutate: func(payload map[string]any) {
				payload["practice_context"].(map[string]any)["practice_session_id"] = "practice-session-other"
			},
		},
		{
			name: "version manifest differs from context",
			mutate: func(payload map[string]any) {
				payload["version_manifest"].(map[string]any)["practice_session"] = 99
			},
		},
		{
			name: "opportunity addressee is outside participant context",
			mutate: func(payload map[string]any) {
				payload["opportunity_manifest"].([]any)[0].(map[string]any)["addressee_participant_ids"] = []any{"participant-other"}
			},
		},
		{
			name: "legacy candidate participant role",
			mutate: func(payload map[string]any) {
				payload["practice_context"].(map[string]any)["participants"].([]any)[1].(map[string]any)["role"] = "CANDIDATE"
			},
		},
		{
			name: "legacy interviewer participant role",
			mutate: func(payload map[string]any) {
				payload["practice_context"].(map[string]any)["participants"].([]any)[0].(map[string]any)["role"] = "INTERVIEWER"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := validEvidenceSnapshotCommand()
			var decoded map[string]any
			if err := json.Unmarshal(
				command.CanonicalPayload,
				&decoded,
			); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			test.mutate(decoded)
			encoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("encode fixture: %v", err)
			}
			command.CanonicalPayload = encoded
			if _, err := normalizeEvidenceSnapshotCommand(command); !errors.Is(
				err,
				evaluation.ErrInvalidRequest,
			) {
				t.Fatalf("normalize error = %v", err)
			}
		})
	}
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
			"practice_option":{
				"id":"practice-option-1",
				"type":"FULL_SIMULATION"
			},
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
				{
					"participant_id":"participant-interviewer",
					"role":"FACILITATOR",
					"order":1
				},
				{
					"participant_id":"participant-candidate",
					"role":"LEARNER",
					"order":2
				}
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
			"audio":{
				"availability":"UNAVAILABLE",
				"quality":"NOT_ASSESSED",
				"ise":"NOT_ASSESSED"
			}
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
			"turn_evidence":[{
				"turn_id":"turn-1",
				"evidence_version":1
			}],
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

func evidenceSnapshotPayloadForID(snapshotID string) json.RawMessage {
	return evidenceSnapshotPayloadForMetadata(
		snapshotID,
		"practice-session-1",
	)
}

func evidenceSnapshotPayloadForMetadata(
	snapshotID string,
	practiceSessionID string,
) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(validEvidenceSnapshotPayload(), &decoded); err != nil {
		panic(err)
	}
	decoded["practice_context"].(map[string]any)["practice_session_id"] = practiceSessionID
	decoded["practice_context"].(map[string]any)["preparation"].(map[string]any)["background_snapshot_hash"] = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	refs, ok := decoded["evidence_refs"].([]any)
	if !ok {
		panic("invalid EvidenceSnapshot test fixture")
	}
	for _, value := range refs {
		ref, ok := value.(map[string]any)
		if !ok {
			panic("invalid EvidenceRef test fixture")
		}
		ref["snapshot_id"] = snapshotID
		ref["evidence_ref_id"] = StableRefID(
			snapshotID,
			"turn-1",
			1,
			"",
		)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		panic(err)
	}
	return encoded
}
