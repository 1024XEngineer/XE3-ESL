package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const evidenceTestOwner = "11111111-1111-4111-8111-111111111111"

func TestEvidenceSourceComposeFreezesCanonicalTrustedEvidence(t *testing.T) {
	fixture := newEvidenceSourceFixture(t)

	command, err := fixture.reader.Compose(
		fixture.ctx,
		fixture.actor,
		"session-1",
		ScopeSession,
		SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	if command.OwnerUserID != evidenceTestOwner ||
		command.PracticeSessionID != "session-1" ||
		command.Scope != ScopeSession ||
		command.SceneType != SceneOverseasDaily {
		t.Fatalf("Compose() command = %#v", command)
	}
	if canonical, canonicalErr := canonicalEvidencePayload(
		command.CanonicalPayload,
	); canonicalErr != nil ||
		string(canonical) != string(command.CanonicalPayload) {
		t.Fatalf(
			"CanonicalPayload canonical = %s, %v",
			canonical,
			canonicalErr,
		)
	}

	var payload evidencePayload
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
		ScopeSession,
		SceneOverseasDaily,
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
	second.conversation.turns[0], second.conversation.turns[1] =
		second.conversation.turns[1], second.conversation.turns[0]
	secondCommand, err := second.reader.Compose(
		second.ctx,
		second.actor,
		"session-1",
		ScopeSession,
		SceneOverseasDaily,
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
		ScopeSession,
		SceneOverseasDaily,
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
		ScopeSession,
		SceneOverseasDaily,
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
	fixture.conversation.questions["question-3"] =
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
		ScopeSession,
		SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	var payload evidencePayload
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
	asset.Status = practiceinput.AudioAssetDeleted
	fixture.audio.assets["turn-1"] = asset

	command, err := fixture.reader.Compose(
		fixture.ctx,
		fixture.actor,
		"session-1",
		ScopeSession,
		SceneOverseasDaily,
	)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	var payload evidencePayload
	if err := json.Unmarshal(command.CanonicalPayload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	audio := payload.ConfirmedTurns[0].Audio
	if audio.Availability != evidenceUnavailable ||
		audio.Status != string(practiceinput.AudioAssetDeleted) ||
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
		scope  Scope
		scene  SceneType
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
			scope: ScopeSession,
			scene: SceneOverseasDaily,
		},
		{
			name:   "turn scope has no trusted turn anchor",
			mutate: func(*evidenceSourceFixture) {},
			scope:  ScopeTurn,
			scene:  SceneOverseasDaily,
		},
		{
			name: "unfinished session",
			mutate: func(f *evidenceSourceFixture) {
				f.practice.session.Status = practice.SessionInProgress
				f.practice.session.EndedAt = nil
				f.practice.session.EndReason = ""
			},
			scope: ScopeSession,
			scene: SceneOverseasDaily,
		},
		{
			name:   "scene mismatch",
			mutate: func(*evidenceSourceFixture) {},
			scope:  ScopeSession,
			scene:  SceneInterview,
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
			if !errors.Is(err, ErrInvalidRequest) {
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
				f.conversation.turns[0].SessionID = "session-other"
			},
		},
		{
			name: "candidate transcript differs from confirmed turn",
			mutate: func(f *evidenceSourceFixture) {
				candidate := f.conversation.candidates["candidate-1"]
				candidate.Text = "altered"
				f.conversation.candidates["candidate-1"] = candidate
			},
		},
		{
			name: "candidate is not confirmed",
			mutate: func(f *evidenceSourceFixture) {
				candidate := f.conversation.candidates["candidate-1"]
				candidate.Status = practiceinput.CandidateReady
				f.conversation.candidates["candidate-1"] = candidate
			},
		},
		{
			name: "question sequence differs from turn",
			mutate: func(f *evidenceSourceFixture) {
				question := f.conversation.questions["question-1"]
				question.Sequence = 9
				f.conversation.questions["question-1"] = question
			},
		},
		{
			name: "follow-up parent is not an earlier opportunity",
			mutate: func(f *evidenceSourceFixture) {
				question := f.conversation.questions["question-2"]
				question.ParentQuestionID = "question-missing"
				f.conversation.questions["question-2"] = question
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
				asset.Status = practiceinput.AudioAssetMetadataCommitted
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
				ScopeSession,
				SceneOverseasDaily,
			)
			if !errors.Is(err, ErrInvalidRequest) {
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
			name: "conversation is deletion fenced",
			mutate: func(f *evidenceSourceFixture) {
				f.conversation.turnsErr = practiceinput.ErrActorDeleted
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
				ScopeSession,
				SceneOverseasDaily,
			)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("Compose() error = %v", err)
			}
		})
	}
}

func TestEvidenceSceneMatchesAllSupportedIELTSPracticeModels(t *testing.T) {
	for _, model := range []scene.SceneModel{
		scene.SceneModelExamBasicDialogue,
		scene.SceneModelIELTSSpeakingPart2,
		scene.SceneModelIELTSSpeakingFullMock,
	} {
		if !evidenceSceneMatches(
			scene.SceneFamilyExam,
			model,
			SceneIELTSSpeaking,
		) {
			t.Fatalf("IELTS model %q was rejected", model)
		}
	}
}

type evidenceSourceFixture struct {
	ctx          context.Context
	actor        requestcontext.Actor
	practice     *fakeEvidencePracticeSource
	conversation *fakeEvidenceConversationSource
	audio        *fakeEvidenceAudioSource
	reader       *EvidenceSourceReader
}

func newEvidenceSourceFixture(t *testing.T) *evidenceSourceFixture {
	t.Helper()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	started := now.Add(-5 * time.Minute)
	ended := now
	roleObjectives := []scene.PracticeObjectiveDefinition{
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
			ID:             "session-1",
			PlanID:         "plan-1",
			SceneFamily:    scene.SceneFamilyDaily,
			SceneModel:     scene.SceneModelHotelCheckinAndIssueHandling,
			SnapshotID:     "snapshot-1",
			Status:         practice.SessionCompleted,
			Version:        3,
			EffectiveTurns: 2,
			StartedAt:      &started,
			EndedAt:        &ended,
			EndReason:      "COVERAGE_SATISFIED_AT_CHECKPOINT",
			CreatedAt:      started,
		},
		snapshot: practice.SessionSnapshot{
			ID:           "snapshot-1",
			SessionID:    "session-1",
			PlanRevision: 2,
			SceneFamily:  scene.SceneFamilyDaily,
			SceneModel:   scene.SceneModelHotelCheckinAndIssueHandling,
			SceneSelection: scene.SelectionSnapshot{
				Scene: scene.SceneDefinition{
					ID:               "scene-daily-1",
					Family:           scene.SceneFamilyDaily,
					Model:            scene.SceneModelHotelCheckinAndIssueHandling,
					Name:             "Hotel check-in",
					Version:          4,
					Status:           scene.SceneStatusActive,
					TurnPolicyRef:    "daily.hotel_checkin_issue.turn.v1",
					SessionPolicyRef: "daily.hotel_checkin_issue.session.v1",
					Prompt: scene.ScenePrompt{
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
						SuggestedDurationSeconds: 300,
					},
					Roles: []scene.RoleDefinition{
						{
							ID:               "role-receptionist",
							SceneID:          "scene-daily-1",
							Type:             "HOTEL_RECEPTIONIST",
							DisplayName:      "Receptionist",
							Responsibilities: "Help the guest check in.",
							Style:            "professional",
							PracticeObjectives: append(
								[]scene.PracticeObjectiveDefinition(nil),
								roleObjectives...,
							),
							DisplayOrder: 1,
						},
					},
					PracticeOptions: []scene.PracticeOption{
						{
							ID:           "option-full",
							SceneID:      "scene-daily-1",
							Type:         scene.PracticeOptionFullSimulation,
							DisplayName:  "Full simulation",
							DisplayOrder: 1,
						},
					},
					DisplayOrder: 1,
				},
				SelectedRoleIDs:  []string{"role-receptionist"},
				PracticeOptionID: "option-full",
			},
			Preparation: preparation.Snapshot{
				ID:                                 "preparation-snapshot-1",
				SourceProfileID:                    "profile-1",
				SourceVersion:                      4,
				SourceJobTargetID:                  "job-target-1",
				SourceJobTargetConfirmationVersion: 2,
				JobTargetInputSnapshot: &preparation.JobTargetInput{
					Source:              preparation.JobTargetSourceJobDescription,
					JobTitle:            "Guest",
					JobDescription:      "Resolve a hotel room issue.",
					CandidateBackground: "Needs a quiet room.",
					PracticeFocus:       "Make a clear request.",
				},
				ResumeSnapshot:         "sensitive resume snapshot",
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
					RoleSnapshot: &scene.RoleDefinition{
						ID:               "role-receptionist",
						SceneID:          "scene-daily-1",
						Type:             "HOTEL_RECEPTIONIST",
						DisplayName:      "Receptionist",
						Responsibilities: "Help the guest check in.",
						Style:            "professional",
						PracticeObjectives: append(
							[]scene.PracticeObjectiveDefinition(nil),
							roleObjectives...,
						),
						DisplayOrder: 1,
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
			SessionPolicy: preparation.SessionPolicy{
				SuggestedDurationSeconds: 300,
				MinEffectiveTurns:        1,
				MaxEffectiveTurns:        3,
				CoverageCheckpointTurn:   1,
				MaxFollowUpsPerQuestion:  1,
				EarlyCompletionRule: preparation.
					EarlyCompletionCoverageSatisfiedAfterCheckpoint,
			},
			PracticeObjectives: []preparation.PracticeObjective{
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
	conversationSource := &fakeEvidenceConversationSource{
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
		candidates: map[string]practiceinput.StoredTranscriptCandidate{
			"candidate-1": evidenceCandidate(turns[0], "transcript-1", "asr-1"),
			"candidate-2": evidenceCandidate(turns[1], "transcript-2", "asr-2"),
		},
	}
	audioSource := &fakeEvidenceAudioSource{
		assets: map[string]practiceinput.AudioAsset{
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
				Status:          practiceinput.AudioAssetReadable,
				Version:         3,
			},
		},
	}
	reader, err := NewEvidenceSourceReader(
		practiceSource,
		conversationSource,
		audioSource,
	)
	if err != nil {
		t.Fatalf("NewEvidenceSourceReader() error = %v", err)
	}
	return &evidenceSourceFixture{
		ctx:          requestcontext.WithActor(context.Background(), actor),
		actor:        actor,
		practice:     practiceSource,
		conversation: conversationSource,
		audio:        audioSource,
		reader:       reader,
	}
}

func evidenceCandidate(
	turn practice.Turn,
	transcriptID string,
	providerRequestID string,
) practiceinput.StoredTranscriptCandidate {
	return practiceinput.StoredTranscriptCandidate{
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
		Status:                  practiceinput.CandidateConfirmed,
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

type fakeEvidenceConversationSource struct {
	turns        []practice.Turn
	turnsErr     error
	questions    map[string]practice.Question
	questionErr  error
	candidates   map[string]practiceinput.StoredTranscriptCandidate
	candidateErr error
}

func (s *fakeEvidenceConversationSource) ListSessionQuestions(
	_ context.Context,
	_ practiceinput.Actor,
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

func (s *fakeEvidenceConversationSource) GetCandidate(
	_ context.Context,
	_ practiceinput.Actor,
	id string,
) (practiceinput.StoredTranscriptCandidate, error) {
	if s.candidateErr != nil {
		return practiceinput.StoredTranscriptCandidate{}, s.candidateErr
	}
	item, ok := s.candidates[id]
	if !ok {
		return practiceinput.StoredTranscriptCandidate{},
			practiceinput.ErrPersistenceNotFound
	}
	return item, nil
}

func (s *fakeEvidenceConversationSource) ListSessionTurns(
	context.Context,
	practiceinput.Actor,
	string,
) ([]practice.Turn, error) {
	return slicesClone(s.turns), s.turnsErr
}

func (s *fakeEvidenceConversationSource) ListCompletedSessionQuestions(
	ctx context.Context,
	_ string,
	sessionID string,
) ([]practice.Question, error) {
	return s.ListSessionQuestions(
		ctx,
		practiceinput.Actor{},
		sessionID,
	)
}

func (s *fakeEvidenceConversationSource) GetCompletedCandidate(
	ctx context.Context,
	_ string,
	id string,
) (practiceinput.StoredTranscriptCandidate, error) {
	return s.GetCandidate(ctx, practiceinput.Actor{}, id)
}

func (s *fakeEvidenceConversationSource) ListCompletedSessionTurns(
	ctx context.Context,
	_ string,
	sessionID string,
) ([]practice.Turn, error) {
	return s.ListSessionTurns(ctx, practiceinput.Actor{}, sessionID)
}

type fakeEvidenceAudioSource struct {
	assets map[string]practiceinput.AudioAsset
	err    error
}

func (s *fakeEvidenceAudioSource) GetByTurn(
	_ context.Context,
	_ string,
	turnID string,
) (practiceinput.AudioAsset, error) {
	if s.err != nil {
		return practiceinput.AudioAsset{}, s.err
	}
	item, ok := s.assets[turnID]
	if !ok {
		return practiceinput.AudioAsset{},
			practiceinput.ErrAudioAssetNotFound
	}
	return item, nil
}

func slicesClone[T any](source []T) []T {
	return append([]T(nil), source...)
}
