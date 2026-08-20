package agentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	schemaVersion        = "agent-turn-context/v1"
	feedbackPollInterval = 100 * time.Millisecond
)

var ErrUnavailable = errors.New("coaching agent context: current-turn state unavailable")

type planReader interface {
	ReadLatestThreadPlan(
		context.Context,
		requestcontext.Actor,
		string,
	) (preparation.PracticePlan, error)
}

type feedbackReader interface {
	GetRecordBySource(
		context.Context,
		string,
		evaluation.Kind,
		string,
	) (evaluation.Record, error)
	ListFeedbackItems(
		context.Context,
		string,
		string,
	) ([]evaluation.FeedbackItem, error)
}

type Contributor struct {
	plans    planReader
	feedback feedbackReader
}

func New(plans planReader, feedback feedbackReader) (*Contributor, error) {
	if plans == nil {
		return nil, ErrUnavailable
	}
	return &Contributor{plans: plans, feedback: feedback}, nil
}

func (contributor *Contributor) Contribute(
	ctx context.Context,
	actor requestcontext.Actor,
	request agentcontext.TurnContextRequest,
) (agentcontext.TurnContextContribution, error) {
	if contributor == nil || contributor.plans == nil || ctx == nil || !actor.Valid() ||
		request.ThreadID == "" || request.InputMessage.ThreadID != request.ThreadID ||
		request.InputMessage.OwnerID != actor.UserID {
		return agentcontext.TurnContextContribution{}, ErrUnavailable
	}

	payload := turnPayload{SchemaVersion: schemaVersion}
	plan, err := contributor.plans.ReadLatestThreadPlan(ctx, actor, request.ThreadID)
	if err == nil {
		practice, practiceErr := practicePayloadFrom(plan)
		if practiceErr != nil {
			return agentcontext.TurnContextContribution{}, practiceErr
		}
		payload.Practice = &practice
	} else if !errors.Is(err, preparation.ErrPlanNotFound) {
		return agentcontext.TurnContextContribution{}, err
	}

	if request.InputMessage.Modality == conversation.MessageModalityVoice &&
		contributor.feedback != nil {
		feedback, feedbackErr := contributor.awaitFeedback(
			ctx,
			actor.UserID,
			request.InputMessage.ID,
		)
		if feedbackErr != nil {
			return agentcontext.TurnContextContribution{}, feedbackErr
		}
		payload.SpeechFeedback = &feedback
	}

	if payload.Practice == nil && payload.SpeechFeedback == nil {
		return agentcontext.TurnContextContribution{}, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return agentcontext.TurnContextContribution{}, ErrUnavailable
	}
	return agentcontext.TurnContextContribution{Payload: encoded}, nil
}

func (contributor *Contributor) awaitFeedback(
	ctx context.Context,
	userID string,
	messageID string,
) (speechFeedbackPayload, error) {
	ticker := time.NewTicker(feedbackPollInterval)
	defer ticker.Stop()
	for {
		record, err := contributor.feedback.GetRecordBySource(
			ctx,
			userID,
			evaluation.KindAgentMessageFeedback,
			messageID,
		)
		if err != nil && !errors.Is(err, evaluation.ErrNotFound) {
			return speechFeedbackPayload{}, err
		}
		if err == nil {
			switch record.Status {
			case evaluation.JobReady:
				items, itemErr := contributor.feedback.ListFeedbackItems(
					ctx,
					userID,
					record.ID,
				)
				if itemErr != nil {
					return speechFeedbackPayload{}, itemErr
				}
				return feedbackPayloadFrom(items)
			case evaluation.JobFailed:
				return speechFeedbackPayload{}, ErrUnavailable
			}
		}
		select {
		case <-ctx.Done():
			return speechFeedbackPayload{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

type turnPayload struct {
	SchemaVersion  string                 `json:"schema_version"`
	SpeechFeedback *speechFeedbackPayload `json:"speech_feedback,omitempty"`
	Practice       *practicePayload       `json:"practice,omitempty"`
}

type speechFeedbackPayload struct {
	Kinds      []string             `json:"kinds"`
	Conclusion string               `json:"conclusion"`
	Items      []speechFeedbackItem `json:"items"`
}

type speechFeedbackItem struct {
	Kind           string `json:"kind"`
	Recommendation string `json:"recommendation"`
	Correction     string `json:"correction,omitempty"`
}

func feedbackPayloadFrom(items []evaluation.FeedbackItem) (speechFeedbackPayload, error) {
	if len(items) == 0 {
		return speechFeedbackPayload{}, ErrUnavailable
	}
	payload := speechFeedbackPayload{
		Kinds: make([]string, 0, len(items)),
		Items: make([]speechFeedbackItem, 0, len(items)),
	}
	hasStrength := false
	hasCorrection := false
	hasRecommendation := false
	for _, item := range items {
		switch speechfeedback.SpeechFeedbackItemKind(item.Category) {
		case speechfeedback.SpeechFeedbackItemStrength:
			hasStrength = true
		case speechfeedback.SpeechFeedbackItemCorrection:
			hasCorrection = true
		case speechfeedback.SpeechFeedbackItemRecommendedExpression:
			hasRecommendation = true
		default:
			return speechFeedbackPayload{}, ErrUnavailable
		}
		payload.Kinds = append(payload.Kinds, item.Category)
		payload.Items = append(payload.Items, speechFeedbackItem{
			Kind: item.Category, Recommendation: item.Recommendation,
			Correction: item.Correction,
		})
	}
	switch {
	case hasStrength && len(items) == 1:
		payload.Conclusion = "NO_CHANGE"
	case hasStrength:
		return speechFeedbackPayload{}, ErrUnavailable
	case hasCorrection && hasRecommendation:
		payload.Conclusion = "CORRECTION_WITH_OPTIONAL_EXPRESSION"
	case hasCorrection:
		payload.Conclusion = "CORRECTION"
	case hasRecommendation:
		payload.Conclusion = "OPTIONAL_EXPRESSION"
	default:
		return speechFeedbackPayload{}, ErrUnavailable
	}
	return payload, nil
}

type practicePayload struct {
	SceneName          string   `json:"scene_name"`
	Goal               string   `json:"goal"`
	UserRole           string   `json:"user_role"`
	AIRole             string   `json:"ai_role"`
	CounterpartRoles   []string `json:"selected_counterpart_roles"`
	PracticeObjectives []string `json:"practice_objectives"`
}

func practicePayloadFrom(plan preparation.PracticePlan) (practicePayload, error) {
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil {
		return practicePayload{}, ErrUnavailable
	}
	payload := practicePayload{
		SceneName:          plan.SceneSelection.Scene.Name,
		Goal:               plan.SceneSelection.Scene.Prompt.PracticeGoal,
		UserRole:           plan.SceneSelection.Scene.Prompt.UserRole,
		AIRole:             plan.SceneSelection.Scene.Prompt.AIRole,
		CounterpartRoles:   make([]string, 0, len(roles)),
		PracticeObjectives: make([]string, 0, len(plan.PracticeObjectives)),
	}
	for _, role := range roles {
		payload.CounterpartRoles = append(payload.CounterpartRoles, role.DisplayName)
	}
	for _, objective := range plan.PracticeObjectives {
		payload.PracticeObjectives = append(
			payload.PracticeObjectives,
			objective.Description,
		)
	}
	return payload, nil
}

var _ agentcontext.TurnContextContributor = (*Contributor)(nil)
