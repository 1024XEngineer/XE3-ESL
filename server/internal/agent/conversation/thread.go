package conversation

import "time"

type Thread struct {
	ID             string
	OwnerID        string
	Title          string
	ActiveGoalID   string
	NextMessageSeq int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ThreadGoalLink struct {
	OwnerID   string
	ThreadID  string
	GoalID    string
	Active    bool
	LinkedAt  time.Time
	UpdatedAt time.Time
}
