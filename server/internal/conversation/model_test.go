package conversation_test

import (
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
)

func TestConversationEnumContracts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"primary question", string(conversation.QuestionTypePrimary), "PRIMARY"},
		{"follow-up question", string(conversation.QuestionTypeFollowUp), "FOLLOW_UP"},
		{"push-to-talk interaction", string(conversation.InteractionModePushToTalk), "PUSH_TO_TALK"},
		{"realtime interaction", string(conversation.InteractionModeRealtime), "REALTIME"},
		{"processing turn", string(conversation.TurnStatusProcessing), "processing"},
		{"completed turn", string(conversation.TurnStatusCompleted), "completed"},
		{"question audio owner", string(conversation.AudioOwnerTypeQuestion), "QUESTION"},
		{"turn audio owner", string(conversation.AudioOwnerTypeTurn), "TURN"},
		{"pending audio", string(conversation.AudioStatusPending), "pending"},
		{"ready audio", string(conversation.AudioStatusReady), "ready"},
		{"failed audio", string(conversation.AudioStatusFailed), "failed"},
		{"deleted audio", string(conversation.AudioStatusDeleted), "deleted"},
		{"valid answer", string(conversation.AnswerValidityValid), "VALID"},
		{"invalid compatibility value", string(conversation.AnswerValidityInvalid), "INVALID"},
		{"covered objective", string(conversation.ObjectiveCoverageCovered), "COVERED"},
		{"partially covered objective", string(conversation.ObjectiveCoveragePartiallyCovered), "PARTIALLY_COVERED"},
		{"not covered objective", string(conversation.ObjectiveCoverageNotCovered), "NOT_COVERED"},
		{"transcription stage", string(conversation.ProcessingStageTranscription), "transcription"},
		{"outcome submission stage", string(conversation.ProcessingStageTurnOutcomeSubmission), "turn_outcome_submission"},
		{"started attempt", string(conversation.ProcessingAttemptStatusStarted), "started"},
		{"succeeded attempt", string(conversation.ProcessingAttemptStatusSucceeded), "succeeded"},
		{"failed attempt", string(conversation.ProcessingAttemptStatusFailed), "failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("got %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestProcessingAttemptSupportsFailureBeforeTurnCreation(t *testing.T) {
	questionID := "question-1"
	audioAssetID := "audio-1"
	audioAsset := conversation.AudioAsset{
		AudioAssetID:            audioAssetID,
		PendingAnswerQuestionID: &questionID,
		AudioStatus:             conversation.AudioStatusReady,
	}
	attempt := conversation.ProcessingAttempt{
		QuestionID:   questionID,
		AudioAssetID: &audioAssetID,
		Stage:        conversation.ProcessingStageTranscription,
		Status:       conversation.ProcessingAttemptStatusFailed,
		Failure: &conversation.ProcessingFailure{
			Code:      "asr_unavailable",
			Retryable: true,
		},
	}

	if audioAsset.OwnerType != nil || audioAsset.OwnerID != nil {
		t.Fatal("pre-turn answer audio must not reference a Turn that does not exist")
	}
	if audioAsset.PendingAnswerQuestionID == nil || *audioAsset.PendingAnswerQuestionID != attempt.QuestionID {
		t.Fatal("pre-turn answer audio must retain its Question association")
	}
	if attempt.TurnID != nil {
		t.Fatal("a failed pre-turn transcription must not reference a Turn")
	}
	if attempt.QuestionID == "" || attempt.AudioAssetID == nil {
		t.Fatal("a failed pre-turn transcription must retain question and audio references")
	}
}

func TestConversationModelsDoNotDuplicateUserOwnership(t *testing.T) {
	models := []any{
		conversation.Question{},
		conversation.Turn{},
		conversation.TurnOutcome{},
	}

	for _, model := range models {
		modelType := reflect.TypeOf(model)
		if _, exists := modelType.FieldByName("UserID"); exists {
			t.Fatalf("%s must resolve user ownership through PracticeSessionID", modelType.Name())
		}
	}
}
