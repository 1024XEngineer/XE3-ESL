package report

import "time"

type HistoryBoundary struct {
	CreatedAt time.Time
	ReportID  string
}

func (boundary HistoryBoundary) Valid() bool {
	return !boundary.CreatedAt.IsZero() && validUUID(boundary.ReportID)
}

type HistoryQuery struct {
	Limit             int
	Before            *HistoryBoundary
	Search            string
	PracticeSessionID string
}

type HistoryPage struct {
	Items   []StoredFormalReport
	HasMore bool
}
