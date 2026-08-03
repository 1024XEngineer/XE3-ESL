package conversation

import "time"

type MessageAudioStatus string

const (
	MessageAudioReadable MessageAudioStatus = "readable"
	MessageAudioDeleting MessageAudioStatus = "deleting"
	MessageAudioDeleted  MessageAudioStatus = "deleted"
)

// MessageAudio is the durable one-to-one recording projection for a user
// Message. ObjectKey remains server-only.
type MessageAudio struct {
	ID             string
	OwnerID        string
	ThreadID       string
	MessageID      string
	CandidateID    string
	ObjectKey      string
	ContentType    string
	Size           int64
	ChecksumSHA256 string
	Duration       time.Duration
	SampleRate     int
	ETag           string
	Status         MessageAudioStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      time.Time
}
