package practice

import "errors"

// 错误值只表达稳定的领域语义，协议状态码由 Delivery 层映射
var (
	ErrPracticeCommandInvalid              = errors.New("practice_command_invalid")
	ErrPracticePlanNotFound                = errors.New("practice_plan_not_found")
	ErrPracticePlanNotReady                = errors.New("practice_plan_not_ready")
	ErrPracticePlanArchived                = errors.New("practice_plan_archived")
	ErrPracticePlanHasActiveSession        = errors.New("practice_plan_has_active_session")
	ErrPracticePlanRevisionConflict        = errors.New("practice_plan_revision_conflict")
	ErrPracticeSessionNotFound             = errors.New("practice_session_not_found")
	ErrPracticeSessionTransitionNotAllowed = errors.New("practice_session_transition_not_allowed")
	ErrPracticeSessionAlreadyTerminal      = errors.New("practice_session_already_terminal")
	ErrPracticeParticipantInvalid          = errors.New("practice_participant_invalid")
	ErrPracticeOptionInvalid               = errors.New("practice_option_invalid")
	ErrTurnOutcomeSessionMismatch          = errors.New("turn_outcome_session_mismatch")
	ErrPracticeIdempotencyConflict         = errors.New("practice_idempotency_conflict")
	ErrPracticeResourceForbidden           = errors.New("practice_resource_forbidden")
)
