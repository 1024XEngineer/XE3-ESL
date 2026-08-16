package conversation

import "time"

type Thread struct {
	ID             string
	OwnerID        string
	Title          string
	NextMessageSeq int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
