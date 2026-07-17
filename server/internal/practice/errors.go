package practice

import "errors"

var (
	ErrPracticeCommandInvalid           = errors.New("practice_command_invalid")
	ErrPracticePlanInvalid              = errors.New("practice_plan_invalid")
	ErrPracticePlanTransitionNotAllowed = errors.New("practice_plan_transition_not_allowed")
	ErrPracticePlanHasActiveSession     = errors.New("practice_plan_has_active_session")
	// 已产生过场次的计划必须保留，不能按空计划删除
	ErrPracticePlanHasSessions             = errors.New("practice_plan_has_sessions")
	ErrPracticeSessionInvalid              = errors.New("practice_session_invalid")
	ErrPracticeSessionInvalidTime          = errors.New("practice_session_invalid_time")
	ErrPracticeSessionTransitionNotAllowed = errors.New("practice_session_transition_not_allowed")
	ErrPracticeParticipantInvalid          = errors.New("practice_participant_invalid")
	ErrPracticeSessionSnapshotInvalid      = errors.New("practice_session_snapshot_invalid")
	ErrPracticePlanNotFound                = errors.New("practice_plan_not_found")
	ErrPracticeSessionNotFound             = errors.New("practice_session_not_found")
	ErrPracticePlanRevisionConflict        = errors.New("practice_plan_revision_conflict")
	ErrPracticeResourceForbidden           = errors.New("practice_resource_forbidden")
	// 同一幂等键只能绑定一组请求参数
	ErrPracticeIdempotencyConflict = errors.New("practice_idempotency_conflict")
)
