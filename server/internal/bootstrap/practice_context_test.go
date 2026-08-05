package bootstrap

import (
	"context"
	"errors"
	"testing"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationagentthread "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentthread"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testProgrammerInterviewSceneID = "scn_programmer_interview"
	testTechnicalInterviewerRoleID = "role_technical_interviewer"
	testHRInterviewerRoleID        = "role_hr_interviewer"
	testFullSimulationOptionID     = "option_full_simulation"
	testTechnicalFocusOptionID     = "option_technical_focus"
	testIELTSFullMockSceneID       = "scn_ielts_speaking_full"
	testIELTSFullSimulationID      = "option_ielts_full_simulation"
	testWorkplaceProgressSceneID   = "scn_workplace_progress_risk_update"
	testDirectManagerRoleID        = "role_direct_manager"
	testDirectManagerFocusOptionID = "option_direct_manager_focus"
)

func TestPreparationThreadReaderValidatesOwnedThread(
	t *testing.T,
) {
	t.Parallel()

	actor := contextCompositionActor()
	tests := []struct {
		name        string
		thread      agentconversation.Thread
		threadError error
		wantError   error
	}{
		{
			name: "owned Thread",
			thread: agentconversation.Thread{
				ID:      "thread-1",
				OwnerID: actor.UserID,
			},
		},
		{
			name: "different owner",
			thread: agentconversation.Thread{
				ID:      "thread-1",
				OwnerID: "user-other",
			},
			wantError: preparation.ErrPlanNotFound,
		},
		{
			name:        "hidden Thread",
			threadError: agentconversation.ErrNotFound,
			wantError:   preparation.ErrPlanNotFound,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader, err := preparationagentthread.New(agentThreadReaderStub{
				thread: test.thread,
				err:    test.threadError,
			})
			if err != nil {
				t.Fatalf("newPreparationThreadReader: %v", err)
			}
			thread, err := reader.ReadOwnedThread(
				context.Background(),
				actor,
				"thread-1",
			)
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) {
					t.Fatalf("ReadOwnedThread error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadOwnedThread: %v", err)
			}
			if thread.ID != "thread-1" {
				t.Fatalf("thread = %+v", thread)
			}
		})
	}
}

type agentThreadReaderStub struct {
	thread agentconversation.Thread
	err    error
}

func (s agentThreadReaderStub) GetThread(
	context.Context,
	requestcontext.Actor,
	string,
) (agentconversation.Thread, error) {
	return s.thread, s.err
}

func newBootstrapTestCatalog(t *testing.T) *scene.Catalog {
	t.Helper()
	programmerRole := scene.RoleDefinition{
		ID:               testTechnicalInterviewerRoleID,
		SceneID:          testProgrammerInterviewSceneID,
		Type:             "TECHNICAL_INTERVIEWER",
		DisplayName:      "Technical interviewer",
		Responsibilities: "Probe technical evidence and trade-offs.",
		Style:            "Precise",
		PracticeObjectives: []scene.PracticeObjectiveDefinition{{
			ID: "system_design", Description: "Explain a system design clearly.",
		}},
	}
	ieltsRole := scene.RoleDefinition{
		ID:               "role_ielts_examiner",
		SceneID:          testIELTSFullMockSceneID,
		Type:             "IELTS_EXAMINER",
		DisplayName:      "IELTS examiner",
		Responsibilities: "Run all speaking test parts.",
		Style:            "Neutral",
		PracticeObjectives: []scene.PracticeObjectiveDefinition{{
			ID: "fluency", Description: "Speak fluently throughout the response.",
		}},
	}
	catalog, err := scene.NewCatalog([]scene.SceneDefinition{
		{
			ID:                  testProgrammerInterviewSceneID,
			Family:              scene.SceneFamilyInterview,
			Model:               scene.SceneModelProjectExperienceDeepDive,
			Name:                "Technical interview",
			Version:             1,
			Status:              scene.SceneStatusActive,
			TurnPolicyRef:       "interview.project_deep_dive.turn.v1",
			SessionPolicyRef:    "interview.project_deep_dive.session.v1",
			EvaluationPolicyRef: "interview.shadow.evaluation.v1",
			Prompt:              bootstrapTestScenePrompt(),
			Roles:               []scene.RoleDefinition{programmerRole},
			PracticeOptions: []scene.PracticeOption{
				{
					ID:          testFullSimulationOptionID,
					SceneID:     testProgrammerInterviewSceneID,
					Type:        scene.PracticeOptionFullSimulation,
					DisplayName: "Full simulation",
				},
				{
					ID:               testTechnicalFocusOptionID,
					SceneID:          testProgrammerInterviewSceneID,
					RoleDefinitionID: testTechnicalInterviewerRoleID,
					Type:             scene.PracticeOptionFocus,
					DisplayName:      "Technical focus",
				},
			},
		},
		{
			ID:                  testIELTSFullMockSceneID,
			Family:              scene.SceneFamilyExam,
			Model:               scene.SceneModelIELTSSpeakingFullMock,
			Name:                "IELTS Speaking full mock",
			Version:             1,
			Status:              scene.SceneStatusActive,
			TurnPolicyRef:       "ielts.speaking_full_mock.turn.v1",
			SessionPolicyRef:    "ielts.speaking_full_mock.session.v1",
			EvaluationPolicyRef: "ielts.speaking_full_mock.evaluation.v1",
			Prompt:              bootstrapTestScenePrompt(),
			Roles:               []scene.RoleDefinition{ieltsRole},
			PracticeOptions: []scene.PracticeOption{
				{
					ID:          testIELTSFullSimulationID,
					SceneID:     testIELTSFullMockSceneID,
					Type:        scene.PracticeOptionFullSimulation,
					DisplayName: "Full mock",
				},
				{
					ID:               "option_ielts_examiner_focus",
					SceneID:          testIELTSFullMockSceneID,
					RoleDefinitionID: ieltsRole.ID,
					Type:             scene.PracticeOptionFocus,
					DisplayName:      "Examiner focus",
				},
			},
		},
	}, scoring.NewEvaluationPolicyRegistry())
	if err != nil {
		t.Fatalf("scene.NewCatalog: %v", err)
	}
	return catalog
}

func bootstrapTestScenePrompt() scene.ScenePrompt {
	return scene.ScenePrompt{
		PublicSceneBrief:         "Practice one realistic spoken English exchange.",
		PracticeGoal:             "Respond clearly with relevant evidence.",
		UserRole:                 "Learner",
		AIRole:                   "Facilitator",
		PersonaSummary:           "A precise language coach.",
		FocusAreas:               []string{"clarity"},
		TurnBlueprints:           []string{"Ask one primary question."},
		SuggestedDurationSeconds: 600,
	}
}

func contextCompositionActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000112",
		SessionID: "20000000-0000-4000-8000-000000000112",
	}
}
