package practice

import (
	"context"
	"slices"
)

type CreatePracticePlanCommand struct {
	UserID, ScenarioDefinitionID, ScenarioConfigID, PreparationProfileID string
	ScenarioType                                                         ScenarioType
	SelectedRoleIDs                                                      []string
	IdempotencyKey                                                       string
}

type CreatePracticeSessionCommand struct {
	UserID, PracticePlanID, ParticipantRoleID, PracticeOptionID, IdempotencyKey string
	PlanRevision                                                                int
}

type UpdatePracticePlanCommand struct {
	UserID, PracticePlanID, ScenarioConfigID, PreparationProfileID string
	SelectedRoleIDs                                                []string
	ExpectedRevision                                               int
}

type ApplyTurnOutcomeCommand struct {
	UserID  string
	Outcome TurnOutcome
}

// 在事务边界内编排领域对象和出站端口，不持有请求级状态
type Service struct {
	preparation  PreparationReader
	repository   Repository
	transactions TransactionManager
	ids          IDGenerator
	clock        Clock
}

func newService(dependencies Dependencies) *Service {
	return &Service{
		preparation:  dependencies.PreparationReader,
		repository:   dependencies.Repository,
		transactions: dependencies.TransactionManager,
		ids:          dependencies.IDGenerator,
		clock:        dependencies.Clock,
	}
}

// 相同用户和幂等键会返回首次创建的计划；参数变化则返回幂等冲突
func (s *Service) CreatePracticePlan(ctx context.Context, command CreatePracticePlanCommand) (*PracticePlan, error) {
	if command.UserID == "" || command.IdempotencyKey == "" {
		return nil, ErrPracticeCommandInvalid
	}
	var result *PracticePlan
	err := s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		if command.ScenarioType == "" {
			command.ScenarioType = ScenarioTypeInterview
		}
		key := command.UserID + ":" + command.IdempotencyKey
		stored, original, found, err := s.repository.FindPlanCreation(ctx, key)
		if err != nil {
			return err
		}
		if found {
			if !sameCreatePlanCommand(original, command) {
				return ErrPracticeIdempotencyConflict
			}
			result = stored
			return nil
		}
		if err := s.preparation.ValidatePlan(ctx, command); err != nil {
			return err
		}
		plan, err := NewPracticePlan(s.ids.NewID(), command.UserID, command.ScenarioDefinitionID, command.ScenarioType, command.ScenarioConfigID, command.PreparationProfileID, command.SelectedRoleIDs)
		if err != nil {
			return err
		}
		if err := plan.MarkReady(); err != nil {
			return err
		}
		stored, original, created, err := s.repository.CreatePlan(ctx, key, command, plan)
		if err != nil {
			return err
		}
		if !created && !sameCreatePlanCommand(original, command) {
			return ErrPracticeIdempotencyConflict
		}
		result = stored
		return nil
	})
	return result, err
}

// 只重试配置失败的计划，验证通过后恢复为可用状态
func (s *Service) RetryPlanConfiguration(ctx context.Context, userID, planID string, expectedRevision int) (*PracticePlan, error) {
	return s.changePlan(ctx, userID, planID, expectedRevision, func(ctx context.Context, plan *PracticePlan, _ bool) error {
		if plan.Status() != PracticePlanConfigurationFailed {
			return ErrPracticePlanTransitionNotAllowed
		}
		if err := s.preparation.ValidatePlan(ctx, createCommandFromPlan(plan)); err != nil {
			return err
		}
		return plan.MarkReady()
	})
}

// 更新前同时校验预期修订号和活动场次，成功后修订号递增
func (s *Service) UpdatePracticePlan(ctx context.Context, command UpdatePracticePlanCommand) (*PracticePlan, error) {
	return s.changePlan(ctx, command.UserID, command.PracticePlanID, command.ExpectedRevision, func(ctx context.Context, plan *PracticePlan, hasActiveSession bool) error {
		validation := createCommandFromPlan(plan)
		validation.ScenarioConfigID = command.ScenarioConfigID
		validation.PreparationProfileID = command.PreparationProfileID
		validation.SelectedRoleIDs = command.SelectedRoleIDs
		if err := s.preparation.ValidatePlan(ctx, validation); err != nil {
			return err
		}
		return plan.Update(command.ScenarioConfigID, command.PreparationProfileID, command.SelectedRoleIDs, hasActiveSession)
	})
}

// 归档操作幂等，但存在活动场次时仍会被拒绝
func (s *Service) ArchivePracticePlan(ctx context.Context, userID, planID string, expectedRevision int) (*PracticePlan, error) {
	return s.changePlan(ctx, userID, planID, expectedRevision, func(_ context.Context, plan *PracticePlan, hasActiveSession bool) error {
		if plan.Status() == PracticePlanArchived {
			return nil
		}
		return plan.Archive(hasActiveSession)
	})
}

// 已恢复的计划重复调用不会再次改变状态
func (s *Service) RestorePracticePlan(ctx context.Context, userID, planID string, expectedRevision int) (*PracticePlan, error) {
	return s.changePlan(ctx, userID, planID, expectedRevision, func(_ context.Context, plan *PracticePlan, _ bool) error {
		if plan.Status() == PracticePlanReady {
			return nil
		}
		return plan.Restore()
	})
}

// 只删除从未产生过场次的计划，历史场次存在时必须保留计划
func (s *Service) DeleteEmptyPracticePlan(ctx context.Context, userID, planID string, expectedRevision int) error {
	return s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		plan, err := s.repository.LockPlanForUpdate(ctx, planID)
		if err != nil {
			return err
		}
		if err := validatePlanAccess(plan, userID, expectedRevision); err != nil {
			return err
		}
		hasSessions, err := s.repository.HasSessions(ctx, planID)
		if err != nil {
			return err
		}
		if hasSessions {
			return ErrPracticePlanHasSessions
		}
		return s.repository.DeletePlan(ctx, planID)
	})
}

// 场次与输入快照在同一事务中创建；同一计划只能保留一个活动场次
func (s *Service) CreatePracticeSession(ctx context.Context, command CreatePracticeSessionCommand) (*PracticeSession, error) {
	if command.UserID == "" || command.IdempotencyKey == "" {
		return nil, ErrPracticeCommandInvalid
	}
	var result *PracticeSession
	err := s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		key := command.UserID + ":" + command.IdempotencyKey
		stored, storedSnapshot, found, err := s.repository.FindSessionCreation(ctx, key)
		if err != nil {
			return err
		}
		if found {
			if !sameSessionCommand(stored, storedSnapshot, command) {
				return ErrPracticeIdempotencyConflict
			}
			result = stored
			return nil
		}
		plan, err := s.repository.GetPlan(ctx, command.PracticePlanID)
		if err != nil {
			return err
		}
		if plan.UserID() != command.UserID {
			return ErrPracticeResourceForbidden
		}
		if plan.Revision() != command.PlanRevision {
			return ErrPracticePlanRevisionConflict
		}
		if plan.Status() != PracticePlanReady {
			return ErrPracticePlanTransitionNotAllowed
		}
		if !slices.Contains(plan.SelectedRoleIDs(), command.ParticipantRoleID) {
			return ErrPracticeParticipantInvalid
		}
		prepared, err := s.preparation.PrepareSession(ctx, plan, command.ParticipantRoleID, command.PracticeOptionID)
		if err != nil {
			return err
		}
		if prepared.Role.RoleDefinitionID != command.ParticipantRoleID || prepared.PracticeOption.PracticeOptionID != command.PracticeOptionID {
			return ErrPracticeSessionSnapshotInvalid
		}
		policy, err := NewPracticeSessionPolicy(prepared.Mode, prepared.TargetObjectiveIDs)
		if err != nil {
			return err
		}
		sessionID, snapshotID := s.ids.NewID(), s.ids.NewID()
		interviewerParticipantID := s.ids.NewID()
		now := s.clock.Now()
		snapshot, err := NewPracticeSessionSnapshot(PracticeSessionSnapshotInput{
			ID:                 snapshotID,
			SessionID:          sessionID,
			PlanRevision:       plan.Revision(),
			ScenarioType:       plan.ScenarioType(),
			ScenarioDefinition: prepared.ScenarioDefinition,
			ScenarioConfig:     prepared.ScenarioConfig,
			Preparation:        prepared.Preparation,
			Participants: []PracticeParticipantInput{{
				ID: interviewerParticipantID, SessionID: sessionID, ParticipantRole: ParticipantRoleInterviewer, SubjectRef: prepared.InterviewerSubject,
				RoleDefinitionID: prepared.Role.RoleDefinitionID,
				RoleSnapshot:     prepared.Role, ParticipantOrder: 1,
			}},
			PracticeOption:  prepared.PracticeOption,
			PracticeFocuses: prepared.PracticeFocuses,
			SessionPolicy:   policy,
			CreatedAt:       now,
		})
		if err != nil {
			return err
		}
		session, err := NewPracticeSession(sessionID, plan.ID(), plan.ScenarioType(), snapshotID, now)
		if err != nil {
			return err
		}
		stored, storedSnapshot, created, err := s.repository.CreateActiveSession(ctx, key, plan.ID(), session, snapshot)
		if err != nil {
			return err
		}
		if !created {
			if !sameSessionCommand(stored, storedSnapshot, command) {
				return ErrPracticeIdempotencyConflict
			}
		}
		result = stored
		return nil
	})
	return result, err
}

// 已经开始的场次重复调用不会重置开始时间
func (s *Service) StartPracticeSession(ctx context.Context, userID, sessionID string) (*PracticeSession, error) {
	return s.changeSession(ctx, userID, sessionID, func(session *PracticeSession) error {
		if session.Status() == PracticeSessionInProgress {
			return nil
		}
		return session.Start(s.clock.Now())
	})
}

// 已暂停的场次重复调用不会再次写入状态
func (s *Service) PausePracticeSession(ctx context.Context, userID, sessionID string) (*PracticeSession, error) {
	return s.changeSession(ctx, userID, sessionID, func(session *PracticeSession) error {
		if session.Status() == PracticeSessionPaused {
			return nil
		}
		return session.Pause()
	})
}

// 已恢复的场次重复调用不会再次写入状态
func (s *Service) ResumePracticeSession(ctx context.Context, userID, sessionID string) (*PracticeSession, error) {
	return s.changeSession(ctx, userID, sessionID, func(session *PracticeSession) error {
		if session.Status() == PracticeSessionInProgress {
			return nil
		}
		return session.Resume()
	})
}

// 提前结束操作幂等，首次结束时必须提供原因
func (s *Service) EndPracticeSessionEarly(ctx context.Context, userID, sessionID, reason string) (*PracticeSession, error) {
	return s.changeSession(ctx, userID, sessionID, func(session *PracticeSession) error {
		if session.Status() == PracticeSessionEndedEarly {
			return nil
		}
		return session.EndEarly(s.clock.Now(), reason)
	})
}

// 读取前校验场次归属，返回创建场次时冻结的输入
func (s *Service) GetPracticeSessionSnapshot(ctx context.Context, userID, sessionID string) (PracticeSessionSnapshot, error) {
	session, err := s.ownedSession(ctx, userID, sessionID)
	if err != nil {
		return PracticeSessionSnapshot{}, err
	}
	return s.repository.GetSnapshot(ctx, session.SnapshotID())
}

// turn_id 已处理时返回首次保存的动作，不重复推进回合计数
func (s *Service) ApplyTurnOutcome(ctx context.Context, command ApplyTurnOutcomeCommand) (NextAction, error) {
	var result NextAction
	err := s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		session, err := s.repository.LockSessionForUpdate(ctx, command.Outcome.SessionID)
		if err != nil {
			return err
		}
		if err := s.ensureSessionOwner(ctx, command.UserID, session); err != nil {
			return err
		}
		if saved, ok, err := s.repository.FindTurnAction(ctx, session.ID(), command.Outcome.TurnID); err != nil {
			return err
		} else if ok {
			result = saved
			return nil
		}
		if session.Status() != PracticeSessionInProgress {
			return ErrPracticeSessionTransitionNotAllowed
		}
		count, err := s.repository.EffectiveTurnCount(ctx, session.ID())
		if err != nil {
			return err
		}
		snapshot, err := s.repository.GetSnapshot(ctx, session.SnapshotID())
		if err != nil {
			return err
		}
		startedAt, ok := session.StartedAt()
		if !ok {
			return ErrPracticeSessionInvalidTime
		}
		now := s.clock.Now()
		policy := snapshot.SessionPolicy()
		remainingTimeSeconds := policy.SuggestedDurationSeconds() - int(now.Sub(startedAt).Seconds())
		result, err = EvaluatePolicy(policy, ProgressInput{EffectiveTurnCount: count + 1, RemainingTimeSeconds: remainingTimeSeconds, Outcome: command.Outcome})
		if err != nil {
			return err
		}
		if result == NextActionCompleteSession {
			if err := session.Complete(now); err != nil {
				return err
			}
			if err := s.repository.SaveSession(ctx, session); err != nil {
				return err
			}
		}
		return s.repository.SaveTurnProgress(ctx, session.ID(), command.Outcome.TurnID, count+1, result)
	})
	return result, err
}

func (s *Service) changeSession(ctx context.Context, userID, id string, change func(*PracticeSession) error) (*PracticeSession, error) {
	var result *PracticeSession
	err := s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		session, err := s.repository.LockSessionForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := s.ensureSessionOwner(ctx, userID, session); err != nil {
			return err
		}
		if err := change(session); err != nil {
			return err
		}
		if err := s.repository.SaveSession(ctx, session); err != nil {
			return err
		}
		result = session
		return nil
	})
	return result, err
}

func (s *Service) changePlan(ctx context.Context, userID, planID string, expectedRevision int, change func(context.Context, *PracticePlan, bool) error) (*PracticePlan, error) {
	var result *PracticePlan
	err := s.transactions.WithinTransaction(ctx, func(ctx context.Context) error {
		plan, err := s.repository.LockPlanForUpdate(ctx, planID)
		if err != nil {
			return err
		}
		if err := validatePlanAccess(plan, userID, expectedRevision); err != nil {
			return err
		}
		hasActiveSession, err := s.repository.HasActiveSession(ctx, planID)
		if err != nil {
			return err
		}
		if err := change(ctx, plan, hasActiveSession); err != nil {
			return err
		}
		if err := s.repository.SavePlan(ctx, plan); err != nil {
			return err
		}
		result = plan
		return nil
	})
	return result, err
}

func validatePlanAccess(plan *PracticePlan, userID string, expectedRevision int) error {
	if userID == "" || expectedRevision < 1 {
		return ErrPracticeCommandInvalid
	}
	if plan.UserID() != userID {
		return ErrPracticeResourceForbidden
	}
	if plan.Revision() != expectedRevision {
		return ErrPracticePlanRevisionConflict
	}
	return nil
}

func createCommandFromPlan(plan *PracticePlan) CreatePracticePlanCommand {
	return CreatePracticePlanCommand{UserID: plan.UserID(), ScenarioDefinitionID: plan.ScenarioDefinitionID(), ScenarioType: plan.ScenarioType(), ScenarioConfigID: plan.ScenarioConfigID(), PreparationProfileID: plan.PreparationProfileID(), SelectedRoleIDs: plan.SelectedRoleIDs()}
}
func (s *Service) ensureSessionOwner(ctx context.Context, userID string, session *PracticeSession) error {
	plan, err := s.repository.GetPlan(ctx, session.PlanID())
	if err != nil {
		return err
	}
	if plan.UserID() != userID {
		return ErrPracticeResourceForbidden
	}
	return nil
}
func (s *Service) ownedSession(ctx context.Context, userID, id string) (*PracticeSession, error) {
	session, err := s.repository.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	plan, err := s.repository.GetPlan(ctx, session.PlanID())
	if err != nil {
		return nil, err
	}
	if plan.UserID() != userID {
		return nil, ErrPracticeResourceForbidden
	}
	return session, nil
}
func sameCreatePlanCommand(left, right CreatePracticePlanCommand) bool {
	return left.UserID == right.UserID && left.ScenarioDefinitionID == right.ScenarioDefinitionID && left.ScenarioType == right.ScenarioType && left.ScenarioConfigID == right.ScenarioConfigID && left.PreparationProfileID == right.PreparationProfileID && slices.Equal(left.SelectedRoleIDs, right.SelectedRoleIDs)
}

func sameSessionCommand(session *PracticeSession, snapshot PracticeSessionSnapshot, command CreatePracticeSessionCommand) bool {
	roles := snapshot.ParticipantRoleIDs()
	return session.PlanID() == command.PracticePlanID && snapshot.PlanRevision() == command.PlanRevision && len(roles) == 1 && roles[0] == command.ParticipantRoleID && snapshot.PracticeOptionID() == command.PracticeOptionID
}
