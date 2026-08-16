package conversation

import "time"

// AudioAttachment is the user recording related to one confirmed voice
// Message. Its ID is the shared media asset ID; storage keys remain private.
type AudioAttachment struct {
	ID          string
	MessageID   string
	ContentType string
	Size        int64
	Duration    time.Duration
	CreatedAt   time.Time
}
