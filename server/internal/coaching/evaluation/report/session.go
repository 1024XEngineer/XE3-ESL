package report

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

var ErrSessionReportConfigurationConflict = errors.New(
	"evaluation report: session report configuration conflict",
)

type SessionReportFailure struct {
	Code      string
	Retryable bool
}

// SessionReportReadState is the owner-scoped persistence projection for one
// completed IELTS Practice Session. A QUEUED state may precede creation of the
// Evaluation ledger while the durable completion handoff is still pending.
type SessionReportReadState struct {
	PracticeMode      string
	AvailableSections []string
	Status            evaluation.Status
	Evaluation        *evaluation.Evaluation
	FormalReport      *StoredFormalReport
	Failure           *SessionReportFailure
}
