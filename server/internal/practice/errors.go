package practice

import "errors"

var (
	ErrPracticePlanInvalid                 = errors.New("practice_plan_invalid")
	ErrPracticePlanTransitionNotAllowed    = errors.New("practice_plan_transition_not_allowed")
	ErrPracticePlanHasActiveSession        = errors.New("practice_plan_has_active_session")
	ErrPracticeSessionInvalid              = errors.New("practice_session_invalid")
	ErrPracticeSessionInvalidTime          = errors.New("practice_session_invalid_time")
	ErrPracticeSessionTransitionNotAllowed = errors.New("practice_session_transition_not_allowed")
	ErrPracticeSessionSnapshotInvalid      = errors.New("practice_session_snapshot_invalid")
)
