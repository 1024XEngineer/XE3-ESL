package conversation

import "time"

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

type MessageModality string

const (
	MessageModalityText       MessageModality = "text"
	MessageModalityVoice      MessageModality = "voice"
	MessageModalityMultimodal MessageModality = "multimodal"
)

type Message struct {
	ID                      string
	OwnerID                 string
	ThreadID                string
	Sequence                int64
	Role                    MessageRole
	ClientMessageID         string
	ProducedByRunID         string
	Modality                MessageModality
	Content                 string
	Audio                   *MessageAudio
	SpeechFeedbackStatusURL string
	CreatedAt               time.Time
}
