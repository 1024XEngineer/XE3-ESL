package practice

// CreateRetryTurnCommand is trusted input from Review. The client request ID
// is the retry identity; reusing it with different source fields is rejected.
type CreateRetryTurnCommand struct {
	UserID          string
	SessionID       string
	OriginalTurnID  string
	QuestionID      string
	ClientRequestID string
}
