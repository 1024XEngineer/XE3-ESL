package agentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	testUserID    = "10000000-0000-4000-8000-000000000001"
	testThreadID  = "20000000-0000-4000-8000-000000000001"
	testMessageID = "30000000-0000-4000-8000-000000000001"
	testEvalID    = "40000000-0000-4000-8000-000000000001"
)

func TestContributorBindsNoChangeFeedbackAndRentalRepairRoles(t *testing.T) {
	contributor, err := New(
		&planReaderFake{plan: rentalRepairPlan()},
		&feedbackReaderFake{
			records: []evaluation.Record{{ID: testEvalID, Status: evaluation.JobReady}},
			items: []evaluation.FeedbackItem{{
				Category: "STRENGTH", Recommendation: "表达已经很自然，无需润色",
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	contribution, err := contributor.Contribute(
		context.Background(),
		testActor(),
		voiceRequest(),
	)
	if err != nil {
		t.Fatalf("Contribute() error = %v", err)
	}
	var payload turnPayload
	if err := json.Unmarshal(contribution.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SpeechFeedback == nil ||
		payload.SpeechFeedback.Conclusion != "NO_CHANGE" ||
		len(payload.SpeechFeedback.Kinds) != 1 ||
		payload.SpeechFeedback.Kinds[0] != "STRENGTH" {
		t.Fatalf("speech feedback = %#v", payload.SpeechFeedback)
	}
	if payload.Practice == nil || payload.Practice.SceneName != "租房报修" ||
		payload.Practice.UserRole != "租客" ||
		payload.Practice.AIRole != "物业工作人员或房东" ||
		len(payload.Practice.CounterpartRoles) != 1 ||
		payload.Practice.CounterpartRoles[0] != "物业工作人员" {
		t.Fatalf("practice = %#v", payload.Practice)
	}
}

func TestContributorUsesNewPlanAfterSceneSwitchWithoutOldRole(t *testing.T) {
	plans := &planReaderFake{plan: rentalRepairPlan()}
	contributor, err := New(plans, &feedbackReaderFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := voiceRequest()
	request.InputMessage.Modality = conversation.MessageModalityText
	if _, err := contributor.Contribute(context.Background(), testActor(), request); err != nil {
		t.Fatal(err)
	}

	plans.plan = restaurantPlan()
	contribution, err := contributor.Contribute(
		context.Background(), testActor(), request,
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload turnPayload
	if err := json.Unmarshal(contribution.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Practice == nil || payload.Practice.SceneName != "餐厅点餐" ||
		payload.Practice.UserRole != "顾客" || payload.Practice.AIRole != "服务员" ||
		len(payload.Practice.CounterpartRoles) != 1 ||
		payload.Practice.CounterpartRoles[0] != "服务员" {
		t.Fatalf("practice after switch = %#v", payload.Practice)
	}
	encoded := string(contribution.Payload)
	if strings.Contains(encoded, "租客") || strings.Contains(encoded, "物业") ||
		strings.Contains(encoded, "房东") {
		t.Fatalf("new contribution retained old role: %s", encoded)
	}
}

func TestContributorWaitsForCurrentVoiceFeedback(t *testing.T) {
	feedback := &feedbackReaderFake{
		records: []evaluation.Record{
			{ID: testEvalID, Status: evaluation.JobQueued},
			{ID: testEvalID, Status: evaluation.JobReady},
		},
		items: []evaluation.FeedbackItem{{
			Category: "RECOMMENDED_EXPRESSION", Recommendation: "可选表达",
			Correction: "I would like to arrange a repair visit.",
		}},
	}
	contributor, err := New(&planReaderFake{err: preparation.ErrPlanNotFound}, feedback)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	contribution, err := contributor.Contribute(ctx, testActor(), voiceRequest())
	if err != nil {
		t.Fatal(err)
	}
	var payload turnPayload
	if err := json.Unmarshal(contribution.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if feedback.calls != 2 || payload.SpeechFeedback == nil ||
		payload.SpeechFeedback.Conclusion != "OPTIONAL_EXPRESSION" {
		t.Fatalf("calls = %d, feedback = %#v", feedback.calls, payload.SpeechFeedback)
	}
}

func TestContributorDoesNotGenerateWithoutFailedVoiceFeedback(t *testing.T) {
	contributor, err := New(
		&planReaderFake{plan: rentalRepairPlan()},
		&feedbackReaderFake{
			records: []evaluation.Record{{ID: testEvalID, Status: evaluation.JobFailed}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = contributor.Contribute(
		context.Background(), testActor(), voiceRequest(),
	)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Contribute() error = %v, want ErrUnavailable", err)
	}
}

func testActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: testUserID, SessionID: "session"}
}

func voiceRequest() agentcontext.TurnContextRequest {
	return agentcontext.TurnContextRequest{
		ThreadID: testThreadID,
		InputMessage: conversation.Message{
			ID: testMessageID, OwnerID: testUserID, ThreadID: testThreadID,
			Role: conversation.MessageRoleUser, Modality: conversation.MessageModalityVoice,
			Content: "The air conditioner is leaking water, and I would like to schedule a repair visit for tomorrow afternoon.",
		},
	}
}

func rentalRepairPlan() preparation.PracticePlan {
	return practicePlan("租房报修", "租客", "物业工作人员或房东", "物业工作人员")
}

func restaurantPlan() preparation.PracticePlan {
	return practicePlan("餐厅点餐", "顾客", "服务员", "服务员")
}

func practicePlan(name, userRole, aiRole, selectedRole string) preparation.PracticePlan {
	return preparation.PracticePlan{
		SceneSelection: scene.SelectionSnapshot{
			Scene: scene.ExecutableSceneSnapshot{
				Name: name,
				Prompt: scene.ScenePrompt{
					PracticeGoal: "完成当前场景沟通", UserRole: userRole, AIRole: aiRole,
				},
				Roles: []scene.RoleSnapshot{{ID: "role", DisplayName: selectedRole}},
			},
			SelectedRoleIDs: []string{"role"},
		},
		PracticeObjectives: []preparation.PracticeObjective{{Description: "清楚表达诉求"}},
	}
}

type planReaderFake struct {
	plan preparation.PracticePlan
	err  error
}

func (reader *planReaderFake) ReadLatestThreadPlan(
	context.Context, requestcontext.Actor, string,
) (preparation.PracticePlan, error) {
	return reader.plan, reader.err
}

type feedbackReaderFake struct {
	records []evaluation.Record
	items   []evaluation.FeedbackItem
	calls   int
}

func (reader *feedbackReaderFake) GetRecordBySource(
	context.Context, string, evaluation.Kind, string,
) (evaluation.Record, error) {
	if len(reader.records) == 0 {
		return evaluation.Record{}, evaluation.ErrNotFound
	}
	index := reader.calls
	if index >= len(reader.records) {
		index = len(reader.records) - 1
	}
	reader.calls++
	return reader.records[index], nil
}

func (reader *feedbackReaderFake) ListFeedbackItems(
	context.Context, string, string,
) ([]evaluation.FeedbackItem, error) {
	if reader.items == nil {
		return nil, errors.New("unexpected ListFeedbackItems")
	}
	return reader.items, nil
}
