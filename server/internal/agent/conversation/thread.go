package conversation

import "time"

type Thread struct {
	ID             string
	OwnerID        string
	Title          string
	ActiveMatterID string
	NextMessageSeq int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ThreadMatterLink struct {
	OwnerID   string
	ThreadID  string
	MatterID  string
	Active    bool
	LinkedAt  time.Time
	UpdatedAt time.Time
}
