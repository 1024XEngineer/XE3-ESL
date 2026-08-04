package practice

import "time"

// PracticeCompleted is the durable handoff produced with the final Turn.
// Evaluation consumes this reference and reads confirmed evidence separately.
type PracticeCompleted struct {
	SessionID       string
	FinalTurnID     string
	SessionVersion  int
	CompletionToken string
	CreatedAt       time.Time
}
