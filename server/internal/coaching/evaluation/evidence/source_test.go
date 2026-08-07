package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const evidenceTestOwner = "11111111-1111-4111-8111-111111111111"

func TestEvidenceSourceComposeFreezesCanonicalTrustedEvidence(t *testing.T) {
	fixture := newEvidenceSourceFixture(t)

	command, err := fixture.reader.Compose(
		fixture.ctx,
		fixture.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if command.OwnerUserID != evidenceTestOwner ||
		command.PracticeSessionID != "session-1" ||
		command.Scope != evaluation.ScopeSession ||
		command.SceneType != evaluation.SceneOverseasDaily {
		t.Fatalf("Compose() command = %#v", command)
	}
	if canonical, canonicalErr := CanonicalPayload(
		command.CanonicalPayload,
	); canonicalErr != nil ||
		string(canonical) != string(command.CanonicalPayload) {
		t.Fatalf(
			"CanonicalPayload canonical = %s, %v",
			canonical,
			canonicalErr,
		)
	}

	var payload SnapshotPayload
	if err := json.Unmarshal(command.CanonicalPayload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.OpportunityManifest) != 2 ||
		payload.OpportunityManifest[0].QuestionID != "question-1" ||
		payload.OpportunityManifest[1].ParentQuestionID != "question-1" ||
		len(payload.ConfirmedTurns) != 2 ||
		len(payload.EvidenceRefs) != 2 ||
		len(payload.ProviderLineage.ASR) != 2 {
		t.Fatalf("unexpected payload = %#v", payload)
	}
	if payload.PracticeContext.TaskContext.PublicSceneBrief == "" ||
		payload.PracticeContext.TaskContext.SuggestedDurationSeconds != 300 ||
		payload.PracticeContext.Participants[0].DisplayName != "Receptionist" ||
		payload.PracticeContext.Preparation.SnapshotID !=
			"preparation-snapshot-1" ||
		payload.PracticeContext.Preparation.JobTargetInput == nil {
		t.Fatalf(
			"practice context = %#v",
			payload.PracticeContext,
		)
	}
	first := payload.ConfirmedTurns[0]
	if first.Transcript.Text != "I need a quiet room." ||
		first.Transcript.ASRConfidence != evidenceUnavailable ||
		first.Transcript.WordTimestamps != evidenceUnavailable ||
		first.Audio.Availability != "AVAILABLE" ||
		first.Audio.AudioAssetID != "audio-1" ||
		first.Audio.Quality != evidenceNotAssessed ||
		first.Audio.ISE != evidenceNotAssessed {
		t.Fatalf("first confirmed turn = %#v", first)
	}
	second := payload.ConfirmedTurns[1]
	if second.Audio.Availability != evidenceUnavailable ||
		second.Audio.AudioAssetID != "" ||
		second.Audio.Quality != evidenceNotAssessed ||
		second.Audio.ISE != evidenceNotAssessed {
		t.Fatalf("second confirmed turn = %#v", second)
	}
	if payload.EvidenceRefs[0].Speaker != "USER" ||
		payload.EvidenceRefs[0].TranscriptSpan.EndUTF8Byte !=
			len([]byte(first.Transcript.Text)) ||
		payload.EvidenceRefs[0].AudioSpan == nil ||
		payload.EvidenceRefs[1].AudioSpan != nil ||
		payload.EvidenceRefs[0].Quality.ASRConfidence !=
			evidenceUnavailable {
		t.Fatalf("evidence refs = %#v", payload.EvidenceRefs)
	}
	if payload.ProviderLineage.UnavailableArtifacts.AudioQuality !=
		evidenceNotAssessed ||
		payload.ProviderLineage.UnavailableArtifacts.ISE !=
			evidenceNotAssessed ||
		payload.VersionManifest.SchemaVersion !=
			evidenceSnapshotSchemaVersion {
		t.Fatalf(
			"lineage/version manifest = %#v / %#v",
			payload.ProviderLineage,
			payload.VersionManifest,
		)
	}

	encoded := string(command.CanonicalPayload)
	for _, privateValue := range []string{
		"audio/v1/private/object.wav",
		"private-etag",
		"upload-request-1",
		"sensitive resume snapshot",
		"sensitive background snapshot",
		"confirmed job description snapshot",
	} {
		if strings.Contains(encoded, privateValue) {
			t.Fatalf("canonical payload leaked %q: %s", privateValue, encoded)
		}
	}
}

func TestEvidenceSourceComposeIsStableAcrossRepositoryOrdering(t *testing.T) {
	first := newEvidenceSourceFixture(t)
	firstCommand, err := first.reader.Compose(
		first.ctx,
		first.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("first Compose() error = %v", err)
	}

	second := newEvidenceSourceFixture(t)
	second.practice.snapshot.Participants[0],
		second.practice.snapshot.Participants[1] =
		second.practice.snapshot.Participants[1],
		second.practice.snapshot.Participants[0]
	second.practice.snapshot.PracticeObjectives[0],
		second.practice.snapshot.PracticeObjectives[1] =
		second.practice.snapshot.PracticeObjectives[1],
		second.practice.snapshot.PracticeObjectives[0]
	second.voice.turns[0], second.voice.turns[1] =
		second.voice.turns[1], second.voice.turns[0]
	secondCommand, err := second.reader.Compose(
		second.ctx,
		second.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("second Compose() error = %v", err)
	}

	if string(firstCommand.CanonicalPayload) !=
		string(secondCommand.CanonicalPayload) ||
		firstCommand.SourceManifestHash != secondCommand.SourceManifestHash {
		t.Fatalf(
			"repository ordering changed canonical source:\n%s\n%s",
			firstCommand.CanonicalPayload,
			secondCommand.CanonicalPayload,
		)
	}
}

func TestEvidenceSourceComposePreservesSemanticTaskOrder(t *testing.T) {
	first := newEvidenceSourceFixture(t)
	firstCommand, err := first.reader.Compose(
		first.ctx,
		first.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("first Compose() error = %v", err)
	}
	second := newEvidenceSourceFixture(t)
	second.practice.snapshot.SceneSelection.Scene.Prompt.TurnBlueprints =
		[]string{"handle the issue", "open the conversation"}
	secondCommand, err := second.reader.Compose(
		second.ctx,
		second.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("second Compose() error = %v", err)
	}
	if firstCommand.SourceManifestHash == secondCommand.SourceManifestHash ||
		string(firstCommand.CanonicalPayload) ==
			string(secondCommand.CanonicalPayload) {
		t.Fatal("semantic task order did not create a distinct source")
	}
}

func TestEvidenceSourceComposePreservesUnansweredOfferedOpportunity(
	t *testing.T,
) {
	fixture := newEvidenceSourceFixture(t)
	fixture.voice.questions["question-3"] =
		practice.Question{
			ID:                      "question-3",
			SessionID:               "session-1",
			SpeakerParticipantID:    "participant-facilitator",
			AddresseeParticipantIDs: []string{"participant-candidate"},
			ObjectiveID:             "complete_check_in",
			Type:                    "FOLLOW_UP",
			ParentQuestionID:        "question-2",
			Content:                 "Would you like me to check availability?",
			Sequence:                3,
			CreatedAt: time.Date(
				2026,
				7,
				30,
				11,
				59,
				30,
				0,
				time.UTC,
			),
		}

	command, err := fixture.reader.Compose(
		fixture.ctx,
		fixture.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(command.CanonicalPayload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.OpportunityManifest) != 3 ||
		payload.OpportunityManifest[2].QuestionID != "question-3" ||
		payload.OpportunityManifest[2].ResponseTurnID != "" ||
		len(payload.ConfirmedTurns) != 2 ||
		len(payload.EvidenceRefs) != 2 {
		t.Fatalf("unanswered opportunity payload = %#v", payload)
	}
}

func TestEvidenceSourceComposeMarksDeletedAudioUnavailable(t *testing.T) {
	fixture := newEvidenceSourceFixture(t)
	asset := fixture.audio.assets["turn-1"]
	asset.Status = practicevoice.AudioAssetDeleted
	fixture.audio.assets["turn-1"] = asset

	command, err := fixture.reader.Compose(
		fixture.ctx,
		fixture.actor,
		"session-1",
		evaluation.ScopeSession,
		evaluation.SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(command.CanonicalPayload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	audio := payload.ConfirmedTurns[0].Audio
	if audio.Availability != evidenceUnavailable ||
		audio.Status != string(practicevoice.AudioAssetDeleted) ||
		audio.AudioAssetID != asset.ID ||
		payload.EvidenceRefs[0].AudioSpan != nil {
		t.Fatalf("deleted audio evidence = %#v", audio)
	}
}

func TestEvidenceSourceComposeRejectsUntrustedOrUnsupportedRequest(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*evidenceSourceFixture)
		scope  evaluation.Scope
		scene  evaluation.SceneType
	}{
		{
			name: "different authenticated actor",
			mutate: func(f *evidenceSourceFixture) {
				f.ctx = requestcontext.WithActor(
					context.Background(),
					requestcontext.Actor{
						UserID:    "22222222-2222-4222-8222-222222222222",
						SessionID: "auth-session",
					},
				)
			},
			scope: evaluation.ScopeSession,
			scene: evaluation.SceneOverseasDaily,
		},
		{
			name:   "turn scope has no trusted turn anchor",
			mutate: func(*evidenceSourceFixture) {},
			scope:  evaluation.ScopeTurn,
			scene:  evaluation.SceneOverseasDaily,
		},
		{
			name: "unfinished session",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.session.Status = practice.SessionInProgress
				f.practice.session.EndedAt = nil
				f.practice.session.EndReason = ""
			},
			scope: evaluation.ScopeSession,
			scene: evaluation.SceneOverseasDaily,
		},
		{
			name: "evaluation policy mismatch",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.session.EvaluationPolicyRef =
					"interview.shadow.evaluation.v1"
			},
			scope: evaluation.ScopeSession,
			scene: evaluation.SceneOverseasDaily,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEvidenceSourceFixture(t)
			test.mutate(fixture)
			_, err := fixture.reader.Compose(
				fixture.ctx,
				fixture.actor,
				"session-1",
				test.scope,
				test.scene,
			)
			if !errors.Is(err, evaluation.ErrInvalidRequest) {
				t.Fatalf("Compose() error = %v", err)
			}
		})
	}
}

func TestEvidenceSourceComposeRejectsInconsistentAuthorities(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*evidenceSourceFixture)
	}{
		{
			name: "turn belongs to another session",
			mutate: func(f *evidenceSourceFixture) {
				f.voice.turns[0].SessionID = "session-other"
			},
		},
		{
			name: "candidate transcript differs from confirmed turn",
			mutate: func(f *evidenceSourceFixture) {
				candidate := f.voice.candidates["candidate-1"]
				candidate.Text = "altered"
				f.voice.candidates["candidate-1"] = candidate
			},
		},
		{
			name: "candidate is not confirmed",
			mutate: func(f *evidenceSourceFixture) {
				candidate := f.voice.candidates["candidate-1"]
				candidate.Status = practicevoice.CandidateReady
				f.voice.candidates["candidate-1"] = candidate
			},
		},
		{
			name: "question sequence differs from turn",
			mutate: func(f *evidenceSourceFixture) {
				question := f.voice.questions["question-1"]
				question.Sequence = 9
				f.voice.questions["question-1"] = question
			},
		},
		{
			name: "follow-up parent is not an earlier opportunity",
			mutate: func(f *evidenceSourceFixture) {
				question := f.voice.questions["question-2"]
				question.ParentQuestionID = "question-missing"
				f.voice.questions["question-2"] = question
			},
		},
		{
			name: "audio belongs to another owner",
			mutate: func(f *evidenceSourceFixture) {
				asset := f.audio.assets["turn-1"]
				asset.OwnerID = "22222222-2222-4222-8222-222222222222"
				f.audio.assets["turn-1"] = asset
			},
		},
		{
			name: "audio is not bound and readable",
			mutate: func(f *evidenceSourceFixture) {
				asset := f.audio.assets["turn-1"]
				asset.Status = practicevoice.AudioAssetMetadataCommitted
				f.audio.assets["turn-1"] = asset
			},
		},
		{
			name: "legacy candidate participant role",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.snapshot.Participants[1].Role = "CANDIDATE"
			},
		},
		{
			name: "legacy interviewer participant role",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.snapshot.Participants[0].Role = "INTERVIEWER"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEvidenceSourceFixture(t)
			test.mutate(fixture)
			_, err := fixture.reader.Compose(
				fixture.ctx,
				fixture.actor,
				"session-1",
				evaluation.ScopeSession,
				evaluation.SceneOverseasDaily,
			)
			if !errors.Is(err, evaluation.ErrInvalidRequest) {
				t.Fatalf("Compose() error = %v", err)
			}
		})
	}
}

func TestEvidenceSourceComposeMapsCrossOwnerAndDeletionToNotFound(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*evidenceSourceFixture)
	}{
		{
			name: "practice ownership lookup misses",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.sessionErr = practice.ErrNotFound
			},
		},
		{
			name: "voice is deletion fenced",
			mutate: func(f *evidenceSourceFixture) {
				f.voice.turnsErr = practicevoice.ErrActorDeleted
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEvidenceSourceFixture(t)
			test.mutate(fixture)
			_, err := fixture.reader.Compose(
				fixture.ctx,
				fixture.actor,
				"session-1",
				evaluation.ScopeSession,
				evaluation.SceneOverseasDaily,
			)
			if !errors.Is(err, evaluation.ErrNotFound) {
				t.Fatalf("Compose() error = %v", err)
			}
		})
	}
}

func TestEvidenceIELTSAssignmentModeMatchesPracticeMode(t *testing.T) {
	practiceContext := PracticeContext{
		PracticeMode: "PART_2",
		TaskBlueprints: []string{
			"Part 2 cue card: describe a skill",
			"Part 3 question: why do people learn new skills?",
		},
		IELTSAssignment: &IELTSAssignment{
			BankID: "ielts-bank-v1",
			Season: "2026 Q3",
			Mode:   "PART_2",
			Parts: []IELTSAssignmentPart{
				{
					Part:       "PART_2",
					SourceID:   "learning-skill",
					TopicTitle: "Learning a skill",
					CueCard:    "Describe a skill",
					TurnBlueprints: []string{
						"Part 2 cue card: describe a skill",
					},
				},
				{
					Part:       "PART_3",
					SourceID:   "learning-skill",
					TopicTitle: "Learning a skill",
					TurnBlueprints: []string{
						"Part 3 question: why do people learn new skills?",
					},
				},
			},
		},
	}
	if !validEvidenceIELTSAssignment(practiceContext) {
		t.Fatal("matching IELTS assignment mode was rejected")
	}
	practiceContext.IELTSAssignment.Mode = "PART_3"
	if validEvidenceIELTSAssignment(practiceContext) {
		t.Fatal("mismatched IELTS assignment mode was accepted")
	}
	practiceContext.PracticeMode = "FULL_SIMULATION"
	practiceContext.IELTSAssignment.Mode = "FULL_SIMULATION"
	if validEvidenceIELTSAssignment(practiceContext) {
		t.Fatal("IELTS assignment was accepted for a general practice mode")
	}
}

func TestEvidenceIELTSAssignmentRejectsBrokenPartComposition(t *testing.T) {
	validContext := func() PracticeContext {
		return PracticeContext{
			PracticeMode: "FULL_MOCK",
			TaskBlueprints: []string{
				"Part 1 question",
				"Part 2 cue card",
				"Part 3 question",
			},
			IELTSAssignment: &IELTSAssignment{
				BankID: "ielts-bank-v1",
				Season: "2026 Q3",
				Mode:   "FULL_MOCK",
				Parts: []IELTSAssignmentPart{
					{Part: "PART_1", SourceID: "part-1-set", TurnBlueprints: []string{"Part 1 question"}},
					{Part: "PART_2", SourceID: "topic-group", TopicTitle: "Learning", CueCard: "Describe a skill", TurnBlueprints: []string{"Part 2 cue card"}},
					{Part: "PART_3", SourceID: "topic-group", TopicTitle: "Learning", TurnBlueprints: []string{"Part 3 question"}},
				},
			},
		}
	}
	if !validEvidenceIELTSAssignment(validContext()) {
		t.Fatal("valid IELTS Part composition was rejected")
	}
	tests := []struct {
		name   string
		mutate func(*PracticeContext)
	}{
		{
			name: "part order",
			mutate: func(context *PracticeContext) {
				context.IELTSAssignment.Parts[0].Part = "PART_2"
			},
		},
		{
			name: "paired source",
			mutate: func(context *PracticeContext) {
				context.IELTSAssignment.Parts[2].SourceID = "other-topic"
			},
		},
		{
			name: "blueprint projection",
			mutate: func(context *PracticeContext) {
				context.IELTSAssignment.Parts[1].TurnBlueprints[0] = "other cue card"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := validContext()
			test.mutate(&context)
			if validEvidenceIELTSAssignment(context) {
				t.Fatal("broken IELTS Part composition was accepted")
			}
		})
	}
}

func TestEvidencePracticeContextProjectsFrozenIELTSParts(t *testing.T) {
	fixture := newEvidenceSourceFixture(t)
	session := fixture.practice.session
	snapshot := fixture.practice.snapshot
	session.Experience = practice.PracticeExperienceIELTSSpeaking
	session.Category = practice.SceneCategory("IELTS_SPEAKING")
	session.PracticeMode = practice.PracticeModeFullMock
	session.EvaluationPolicyRef = "ielts.speaking.full_mock.evaluation.v1"
	snapshot.Experience = session.Experience
	snapshot.Category = session.Category
	snapshot.PracticeMode = session.PracticeMode
	snapshot.SceneSelection.Scene.Experience = session.Experience
	snapshot.SceneSelection.Scene.Category = session.Category
	snapshot.SceneSelection.Scene.Prompt.TurnBlueprints = []string{
		"Part 1 question",
		"Part 2 cue card",
		"Part 3 question",
	}
	option := &snapshot.SceneSelection.Scene.PracticeOptions[0]
	option.Mode = practice.PracticeModeFullMock
	option.EvaluationPolicyRef = session.EvaluationPolicyRef
	snapshot.IELTSAssignment = &practice.IELTSAssignment{
		BankID: "ielts-bank-v1",
		Season: "2026 Q3",
		Mode:   practice.PracticeModeFullMock,
		Parts: []practice.IELTSPart{
			{Part: practice.PracticeModePart1, SourceID: "part-1-set", TurnBlueprints: []string{"Part 1 question"}},
			{Part: practice.PracticeModePart2, SourceID: "topic-group", TopicTitle: "Learning", CueCard: "Describe a skill", TurnBlueprints: []string{"Part 2 cue card"}},
			{Part: practice.PracticeModePart3, SourceID: "topic-group", TopicTitle: "Learning", TurnBlueprints: []string{"Part 3 question"}},
		},
	}

	context, _, _, ok := evidencePracticeContextFromSnapshot(
		fixture.actor.UserID,
		session,
		snapshot,
	)
	if !ok || context.IELTSAssignment == nil ||
		len(context.IELTSAssignment.Parts) != 3 ||
		context.IELTSAssignment.Parts[1].TopicTitle != "Learning" ||
		context.IELTSAssignment.Parts[1].CueCard != "Describe a skill" ||
		!slices.Equal(
			context.IELTSAssignment.Parts[2].TurnBlueprints,
			[]string{"Part 3 question"},
		) {
		t.Fatalf("projected IELTS assignment = %#v", context.IELTSAssignment)
	}
	snapshot.IELTSAssignment.Parts[2].TurnBlueprints[0] = "mutated"
	if context.IELTSAssignment.Parts[2].TurnBlueprints[0] != "Part 3 question" {
		t.Fatal("projected IELTS Part blueprints alias the Session Snapshot")
	}
}

type evidenceSourceFixture struct {
	ctx      context.Context
	actor    requestcontext.Actor
	practice *fakeEvidencePracticeSource
	voice    *fakeEvidenceVoiceSource
	audio    *fakeEvidenceAudioSource
	reader   *EvidenceSourceReader
}

func newEvidenceSourceFixture(t *testing.T) *evidenceSourceFixture {
	t.Helper()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	started := now.Add(-5 * time.Minute)
	ended := now
	roleObjectives := []practice.PracticeObjectiveDefinition{
		{ID: "complete_check_in", Description: "Complete check-in accurately."},
		{ID: "clear_request", Description: "Make a clear request."},
		{
			ID:          "polite_register",
			Description: "Use a polite and context-appropriate register.",
		},
	}
	actor := requestcontext.Actor{
		UserID:    evidenceTestOwner,
		SessionID: "auth-session",
	}
	practiceSource := &fakeEvidencePracticeSource{
		session: practice.Session{
			ID:                  "session-1",
			PlanID:              "plan-1",
			Experience:          practice.PracticeExperienceLifeAndTravel,
			Category:            practice.SceneCategory("LIFE_DAILY"),
			PracticeMode:        practice.PracticeModeFullSimulation,
			EvaluationPolicyRef: "daily.general.evaluation.v1",
			SnapshotID:          "snapshot-1",
			Status:              practice.SessionCompleted,
			Version:             3,
			EffectiveTurns:      2,
			StartedAt:           &started,
			EndedAt:             &ended,
			EndReason:           "USER_COMPLETED",
			CreatedAt:           started,
		},
		snapshot: practice.SessionSnapshot{
			ID:           "snapshot-1",
			SessionID:    "session-1",
			PlanRevision: 2,
			Experience:   practice.PracticeExperienceLifeAndTravel,
			Category:     practice.SceneCategory("LIFE_DAILY"),
			PracticeMode: practice.PracticeModeFullSimulation,
			SceneSelection: practice.SceneSelection{
				Scene: practice.SceneDefinition{
					ID:         "scene-daily-1",
					Experience: practice.PracticeExperienceLifeAndTravel,
					Category:   practice.SceneCategory("LIFE_DAILY"),
					Name:       "Hotel check-in",
					Version:    4,
					Status:     practice.SceneStatusActive,
					Prompt: practice.ScenePrompt{
						PublicSceneBrief: "You are checking in at a hotel.",
						PracticeGoal:     "check in and resolve a room issue",
						UserRole:         "guest",
						AIRole:           "hotel receptionist",
						PersonaSummary: "A professional receptionist who helps " +
							"with room issues.",
						FocusAreas: []string{
							"clear request",
							"polite register",
						},
						TurnBlueprints: []string{
							"open the conversation",
							"handle the issue",
						},
					},
					Roles: []practice.RoleDefinition{
						{
							ID:               "role-receptionist",
							SceneID:          "scene-daily-1",
							Type:             "HOTEL_RECEPTIONIST",
							DisplayName:      "Receptionist",
							Responsibilities: "Help the guest check in.",
							Style:            "professional",
							PracticeObjectives: append(
								[]practice.PracticeObjectiveDefinition(nil),
								roleObjectives...,
							),
						},
					},
					PracticeOptions: []practice.PracticeOption{
						{
							ID:                       "option-full",
							SceneID:                  "scene-daily-1",
							Mode:                     practice.PracticeModeFullSimulation,
							DisplayName:              "Full simulation",
							SuggestedDurationSeconds: 300,
							TurnPolicyRef:            "daily.hotel_checkin_issue.turn.v1",
							SessionPolicyRef:         "daily.hotel_checkin_issue.session.v1",
							EvaluationPolicyRef:      "daily.general.evaluation.v1",
						},
					},
				},
				SelectedRoleIDs:  []string{"role-receptionist"},
				PracticeOptionID: "option-full",
			},
			Preparation: practice.PreparationSnapshot{
				ID:                                 "preparation-snapshot-1",
				SourceProfileID:                    "profile-1",
				SourceVersion:                      4,
				SourceJobTargetID:                  "job-target-1",
				SourceJobTargetConfirmationVersion: 2,
				JobTargetInputSnapshot: &practice.JobTargetInput{
					Source:              "JOB_DESCRIPTION",
					JobTitle:            "Guest",
					JobDescription:      "Resolve a hotel room issue.",
					CandidateBackground: "Needs a quiet room.",
					PracticeFocus:       "Make a clear request.",
				},
				ResumeSnapshot: &practice.ResumeRevisionSnapshot{
					ResumeID: "50000000-0000-4000-8000-000000000001",
					Revision: 1,
					Material: practice.ResumeMaterial{
						WorkExperiences:      []practice.ResumeWorkExperience{},
						ProjectExperiences:   []practice.ResumeProjectExperience{},
						EducationExperiences: []practice.ResumeEducationExperience{},
						Skills:               []string{"Go"},
						Awards:               []string{},
					},
				},
				BackgroundSnapshot:     "sensitive background snapshot",
				JobDescriptionSnapshot: "confirmed job description snapshot",
			},
			Participants: []practice.Participant{
				{
					ID:        "participant-facilitator",
					SessionID: "session-1",
					Role:      "FACILITATOR",
					SubjectRef: practice.SubjectRef{
						Namespace: "speakup.role",
						SubjectID: "receptionist",
					},
					RoleDefinitionID: "role-receptionist",
					RoleSnapshot: &practice.RoleDefinition{
						ID:               "role-receptionist",
						SceneID:          "scene-daily-1",
						Type:             "HOTEL_RECEPTIONIST",
						DisplayName:      "Receptionist",
						Responsibilities: "Help the guest check in.",
						Style:            "professional",
						PracticeObjectives: append(
							[]practice.PracticeObjectiveDefinition(nil),
							roleObjectives...,
						),
					},
					Order: 1,
				},
				{
					ID:        "participant-candidate",
					SessionID: "session-1",
					Role:      "LEARNER",
					SubjectRef: practice.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: evidenceTestOwner,
					},
					Order: 2,
				},
			},
			SessionPolicy: practice.SessionPolicy{
				CompletionMode:           practice.CompletionModeUserControlled,
				SuggestedDurationSeconds: 300,
				MinEffectiveTurns:        1,
				MaxEffectiveTurns:        0,
				CoverageCheckpointTurn:   1,
				MaxFollowUpsPerQuestion:  1,
				EarlyCompletionRule: practice.
					EarlyCompletionCoverageSatisfiedAfterCheckpoint,
				RetryAllowed: true,
			},
			PracticeObjectives: []practice.PracticeObjective{
				{ID: "complete_check_in", Description: "Complete check-in accurately."},
				{ID: "clear_request", Description: "Make a clear request."},
				{
					ID:          "polite_register",
					Description: "Use a polite and context-appropriate register.",
				},
			},
			CreatedAt: started,
		},
	}
	turns := []practice.Turn{
		{
			ID:                      "turn-1",
			SessionID:               "session-1",
			QuestionID:              "question-1",
			SpeakerParticipantID:    "participant-facilitator",
			AddresseeParticipantIDs: []string{"participant-candidate"},
			RespondentParticipantID: "participant-candidate",
			Sequence:                1,
			InteractionMode:         "PUSH_TO_TALK",
			AnswerText:              "I need a quiet room.",
			CandidateID:             "candidate-1",
			EvidenceVersion:         1,
			CountsTowardTurnLimit:   true,
			ConfirmedAt:             now.Add(-2 * time.Minute),
			CreatedAt:               now.Add(-2 * time.Minute),
		},
		{
			ID:                      "turn-2",
			SessionID:               "session-1",
			QuestionID:              "question-2",
			SpeakerParticipantID:    "participant-facilitator",
			AddresseeParticipantIDs: []string{"participant-candidate"},
			RespondentParticipantID: "participant-candidate",
			Sequence:                2,
			InteractionMode:         "TEXT",
			AnswerText:              "Could you check another room?",
			CandidateID:             "candidate-2",
			EvidenceVersion:         2,
			CountsTowardTurnLimit:   true,
			ConfirmedAt:             now.Add(-time.Minute),
			CreatedAt:               now.Add(-time.Minute),
		},
	}
	voiceSource := &fakeEvidenceVoiceSource{
		turns: turns,
		questions: map[string]practice.Question{
			"question-1": {
				ID:                      "question-1",
				SessionID:               "session-1",
				SpeakerParticipantID:    "participant-facilitator",
				AddresseeParticipantIDs: []string{"participant-candidate"},
				ObjectiveID:             "complete_check_in",
				Type:                    "PRIMARY",
				Content:                 "How may I help you?",
				Sequence:                1,
				CreatedAt:               now.Add(-3 * time.Minute),
			},
			"question-2": {
				ID:                      "question-2",
				SessionID:               "session-1",
				SpeakerParticipantID:    "participant-facilitator",
				AddresseeParticipantIDs: []string{"participant-candidate"},
				ObjectiveID:             "complete_check_in",
				Type:                    "FOLLOW_UP",
				ParentQuestionID:        "question-1",
				Content:                 "Would another room work?",
				Sequence:                2,
				CreatedAt:               now.Add(-90 * time.Second),
			},
		},
		candidates: map[string]practicevoice.StoredTranscriptCandidate{
			"candidate-1": evidenceCandidate(turns[0], "transcript-1", "asr-1"),
			"candidate-2": evidenceCandidate(turns[1], "transcript-2", "asr-2"),
		},
	}
	audioSource := &fakeEvidenceAudioSource{
		assets: map[string]practicevoice.AudioAsset{
			"turn-1": {
				ID:              "audio-1",
				OwnerID:         evidenceTestOwner,
				UploadRequestID: "upload-request-1",
				ObjectKey:       "audio/v1/private/object.wav",
				CandidateID:     "candidate-1",
				TurnID:          "turn-1",
				ContentType:     "audio/wav",
				Size:            4096,
				ChecksumSHA256:  strings.Repeat("a", 64),
				Duration:        1501 * time.Millisecond,
				ETag:            "private-etag",
				Status:          practicevoice.AudioAssetReadable,
				Version:         3,
			},
		},
	}
	reader, err := NewEvidenceSourceReader(
		practiceSource,
		voiceSource,
		audioSource,
	)
	if err != nil {
		t.Fatalf("NewEvidenceSourceReader() error = %v", err)
	}
	return &evidenceSourceFixture{
		ctx:      requestcontext.WithActor(context.Background(), actor),
		actor:    actor,
		practice: practiceSource,
		voice:    voiceSource,
		audio:    audioSource,
		reader:   reader,
	}
}

func evidenceCandidate(
	turn practice.Turn,
	transcriptID string,
	providerRequestID string,
) practicevoice.StoredTranscriptCandidate {
	return practicevoice.StoredTranscriptCandidate{
		ID:                      turn.CandidateID,
		QuestionID:              turn.QuestionID,
		SessionID:               turn.SessionID,
		RespondentParticipantID: turn.RespondentParticipantID,
		TranscriptID:            transcriptID,
		EvidenceVersion:         turn.EvidenceVersion,
		Provider:                "qianwen",
		Model:                   "paraformer-v2",
		ProviderRequestID:       providerRequestID,
		Text:                    turn.AnswerText,
		Status:                  practicevoice.CandidateConfirmed,
		CreatedAt:               turn.CreatedAt,
	}
}

type fakeEvidencePracticeSource struct {
	session     practice.Session
	sessionErr  error
	snapshot    practice.SessionSnapshot
	snapshotErr error
}

func (s *fakeEvidencePracticeSource) GetSession(
	context.Context,
	practice.Actor,
	string,
) (practice.Session, error) {
	return s.session, s.sessionErr
}

func (s *fakeEvidencePracticeSource) GetSessionSnapshot(
	context.Context,
	practice.Actor,
	string,
) (practice.SessionSnapshot, error) {
	return s.snapshot, s.snapshotErr
}

func (s *fakeEvidencePracticeSource) GetCompletedSession(
	context.Context,
	string,
	string,
) (practice.Session, error) {
	return s.session, s.sessionErr
}

func (s *fakeEvidencePracticeSource) GetCompletedSessionSnapshot(
	context.Context,
	string,
	string,
) (practice.SessionSnapshot, error) {
	return s.snapshot, s.snapshotErr
}

type fakeEvidenceVoiceSource struct {
	turns        []practice.Turn
	turnsErr     error
	questions    map[string]practice.Question
	questionErr  error
	candidates   map[string]practicevoice.StoredTranscriptCandidate
	candidateErr error
}

func (s *fakeEvidenceVoiceSource) ListSessionQuestions(
	_ context.Context,
	_ practicevoice.Actor,
	sessionID string,
) ([]practice.Question, error) {
	if s.questionErr != nil {
		return nil, s.questionErr
	}
	result := make([]practice.Question, 0, len(s.questions))
	for _, item := range s.questions {
		if item.SessionID == sessionID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *fakeEvidenceVoiceSource) GetCandidate(
	_ context.Context,
	_ practicevoice.Actor,
	id string,
) (practicevoice.StoredTranscriptCandidate, error) {
	if s.candidateErr != nil {
		return practicevoice.StoredTranscriptCandidate{}, s.candidateErr
	}
	item, ok := s.candidates[id]
	if !ok {
		return practicevoice.StoredTranscriptCandidate{},
			practicevoice.ErrPersistenceNotFound
	}
	return item, nil
}

func (s *fakeEvidenceVoiceSource) ListSessionTurns(
	context.Context,
	practicevoice.Actor,
	string,
) ([]practice.Turn, error) {
	return slicesClone(s.turns), s.turnsErr
}

func (s *fakeEvidenceVoiceSource) ListCompletedSessionQuestions(
	ctx context.Context,
	_ string,
	sessionID string,
) ([]practice.Question, error) {
	return s.ListSessionQuestions(
		ctx,
		practicevoice.Actor{},
		sessionID,
	)
}

func (s *fakeEvidenceVoiceSource) GetCompletedCandidate(
	ctx context.Context,
	_ string,
	id string,
) (practicevoice.StoredTranscriptCandidate, error) {
	return s.GetCandidate(ctx, practicevoice.Actor{}, id)
}

func (s *fakeEvidenceVoiceSource) ListCompletedSessionTurns(
	ctx context.Context,
	_ string,
	sessionID string,
) ([]practice.Turn, error) {
	return s.ListSessionTurns(ctx, practicevoice.Actor{}, sessionID)
}

type fakeEvidenceAudioSource struct {
	assets map[string]practicevoice.AudioAsset
	err    error
}

func (s *fakeEvidenceAudioSource) GetByTurn(
	_ context.Context,
	_ string,
	turnID string,
) (practicevoice.AudioAsset, error) {
	if s.err != nil {
		return practicevoice.AudioAsset{}, s.err
	}
	item, ok := s.assets[turnID]
	if !ok {
		return practicevoice.AudioAsset{},
			practicevoice.ErrAudioAssetNotFound
	}
	return item, nil
}

func slicesClone[T any](source []T) []T {
	return append([]T(nil), source...)
}
