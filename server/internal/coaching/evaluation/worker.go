package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const MinimumIELTSTwoRoundDeadline = 2*45*time.Second + 10*time.Second

const (
	previousProfileLifecycleStages = 1
	finalProfileLifecycleStages    = 2
	maximumProfileLifecycleWait    = 30 * time.Minute
)

const acousticDependencyTimeoutReason = "ACOUSTIC_DEPENDENCY_TIMEOUT"

var ErrAcousticDependencyFailed error = acousticDependencyFailure{}

type acousticDependencyFailure struct{}

func (acousticDependencyFailure) Error() string {
	return "evaluation: IELTS acoustic dependency failed"
}
func (acousticDependencyFailure) StableCategory() string {
	return "ACOUSTIC_DEPENDENCY_FAILED"
}
func (acousticDependencyFailure) Retryable() bool { return false }

type processingFailure struct {
	record  Record
	failure JobError
	cause   error
}

func (failure *processingFailure) Error() string { return failure.cause.Error() }
func (failure *processingFailure) Unwrap() error { return failure.cause }
func (failure *processingFailure) EvaluationID() string {
	return failure.record.ID
}
func (failure *processingFailure) EvaluationKind() Kind {
	return failure.record.Kind
}
func (failure *processingFailure) EvaluationAttemptCount() int {
	return failure.record.AttemptCount
}
func (failure *processingFailure) EvaluationJobError() JobError {
	return failure.failure
}

type SessionEvaluators interface {
	EvaluateInterview(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
	EvaluateIELTS(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
	EvaluateGeneral(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
}

type IELTSProfileEvaluators interface {
	EvaluateProfile(
		context.Context,
		Record,
		IELTSProfileInputSnapshot,
		ConfigLineage,
	) (json.RawMessage, error)
}

type SpeechEvaluators interface {
	EvaluatePracticeTurn(
		context.Context,
		SpeechInputSnapshot,
		ConfigLineage,
	) (json.RawMessage, []FeedbackItemDraft, error)
	EvaluateAgentMessage(
		context.Context,
		SpeechInputSnapshot,
		ConfigLineage,
	) (json.RawMessage, []FeedbackItemDraft, error)
}

type AcousticEvaluator interface {
	EvaluateAcoustic(context.Context, Record, SpeechInputSnapshot) (AcousticCheckpoint, error)
}

type SessionAcousticRead struct {
	Checkpoints map[string]AcousticCheckpoint
	Pending     bool
}

type SessionAcousticSource interface {
	ReadSessionAcoustics(
		context.Context,
		string,
		string,
		[]string,
	) (SessionAcousticRead, error)
}

type WorkerConfiguration struct {
	SessionLane               ClaimLane
	ProfileLane               ClaimLane
	SpeechLane                ClaimLane
	AcousticsEnabled          bool
	InterviewDeadline         time.Duration
	IELTSDeadline             time.Duration
	GeneralDeadline           time.Duration
	SpeechDeadline            time.Duration
	ProfileDeadline           time.Duration
	RetryDelay                time.Duration
	DependencyDelay           time.Duration
	AcousticDependencyMaxWait time.Duration
	FinalizeTimeout           time.Duration
}

func (configuration WorkerConfiguration) Valid() bool {
	return configuration.SessionLane.Valid() &&
		len(configuration.SessionLane.Kinds) == 1 &&
		configuration.SessionLane.Kinds[0] == KindSessionReport &&
		configuration.ProfileLane.Valid() &&
		len(configuration.ProfileLane.Kinds) == 2 &&
		containsKind(configuration.ProfileLane.Kinds, KindIELTSPart1Profile) &&
		containsKind(configuration.ProfileLane.Kinds, KindIELTSPart2Profile) &&
		configuration.SpeechLane.Valid() &&
		len(configuration.SpeechLane.Kinds) == 2 &&
		containsKind(configuration.SpeechLane.Kinds, KindPracticeTurnFeedback) &&
		containsKind(configuration.SpeechLane.Kinds, KindAgentMessageFeedback) &&
		configuration.InterviewDeadline > 0 &&
		configuration.InterviewDeadline < configuration.SessionLane.LeaseDuration &&
		configuration.IELTSDeadline >= MinimumIELTSTwoRoundDeadline &&
		configuration.IELTSDeadline < configuration.SessionLane.LeaseDuration &&
		configuration.GeneralDeadline > 0 &&
		configuration.GeneralDeadline < configuration.SessionLane.LeaseDuration &&
		configuration.SpeechDeadline > 0 &&
		configuration.SpeechDeadline < configuration.SpeechLane.LeaseDuration &&
		configuration.ProfileDeadline > 0 &&
		configuration.ProfileDeadline < configuration.ProfileLane.LeaseDuration &&
		configuration.RetryDelay >= 0 && configuration.RetryDelay <= time.Hour &&
		configuration.DependencyDelay >= time.Second &&
		configuration.DependencyDelay <= time.Minute &&
		configuration.AcousticDependencyMaxWait >= configuration.DependencyDelay &&
		configuration.AcousticDependencyMaxWait <= 5*time.Minute &&
		configuration.profileLifecycleWaitBudget(finalProfileLifecycleStages) <=
			maximumProfileLifecycleWait &&
		configuration.FinalizeTimeout >= time.Second &&
		configuration.FinalizeTimeout <= 30*time.Second
}

func (configuration WorkerConfiguration) profileLifecycleWaitBudget(stages int) time.Duration {
	attempts := time.Duration(configuration.ProfileLane.MaxAttempts)
	processing := attempts*configuration.ProfileDeadline +
		(attempts-1)*configuration.RetryDelay + configuration.DependencyDelay
	// A crashed worker can retain each attempt until its lease expires. The
	// lease margin extends, rather than duplicates, the provider deadline.
	leaseRecoveryMargin := attempts *
		(configuration.ProfileLane.LeaseDuration - configuration.ProfileDeadline)
	perStage := configuration.AcousticDependencyMaxWait + processing + leaseRecoveryMargin
	return time.Duration(stages) * perStage
}

type Worker struct {
	store         Store
	sessions      SessionEvaluators
	profiles      IELTSProfileEvaluators
	speech        SpeechEvaluators
	acoustics     AcousticEvaluator
	sessionAudio  SessionAcousticSource
	configuration WorkerConfiguration
}

func NewWorker(
	store Store,
	sessions SessionEvaluators,
	profiles IELTSProfileEvaluators,
	speech SpeechEvaluators,
	acoustics AcousticEvaluator,
	sessionAudio SessionAcousticSource,
	configuration WorkerConfiguration,
) (*Worker, error) {
	if store == nil || sessions == nil || profiles == nil || speech == nil ||
		(configuration.AcousticsEnabled &&
			(acoustics == nil || sessionAudio == nil)) ||
		!configuration.Valid() {
		return nil, ErrInvalidRequest
	}
	return &Worker{
		store:         store,
		sessions:      sessions,
		profiles:      profiles,
		speech:        speech,
		acoustics:     acoustics,
		sessionAudio:  sessionAudio,
		configuration: configuration,
	}, nil
}

func (worker *Worker) ProcessProfile(ctx context.Context) (bool, error) {
	if worker == nil || worker.store == nil || ctx == nil {
		return false, ErrInvalidRequest
	}
	claim, err := worker.store.ClaimNext(ctx, worker.configuration.ProfileLane)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, deferred, err := worker.evaluateProfile(ctx, &claim)
	if deferred {
		return true, err
	}
	if err != nil {
		return true, worker.fail(ctx, claim, err)
	}
	if err := worker.complete(ctx, claim, result, nil); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *Worker) evaluateProfile(
	ctx context.Context,
	claim *Claim,
) (json.RawMessage, bool, error) {
	if claim == nil || (claim.Kind != KindIELTSPart1Profile &&
		claim.Kind != KindIELTSPart2Profile) {
		return nil, false, ErrInvalidRequest
	}
	var snapshot IELTSProfileInputSnapshot
	var lineage ConfigLineage
	if DecodeStrict(claim.InputSnapshot, &snapshot) != nil || !snapshot.Valid() ||
		DecodeStrict(claim.ConfigLineage, &lineage) != nil || !lineage.Valid() {
		return nil, false, ErrInvalidRequest
	}
	if snapshot.Stage == IELTSProfileStagePart2 &&
		snapshot.DependencyResolution == IELTSProfileDependencyPending {
		deferred, err := worker.resolvePreviousProfile(ctx, claim, &snapshot)
		if err != nil || deferred {
			return nil, deferred, err
		}
	}
	if worker.configuration.AcousticsEnabled {
		deferred, err := worker.resolveProfileAcoustics(ctx, claim, &snapshot)
		if err != nil || deferred {
			return nil, deferred, err
		}
	}
	evaluationContext, cancel := context.WithTimeout(
		ctx, worker.configuration.ProfileDeadline,
	)
	defer cancel()
	result, err := worker.profiles.EvaluateProfile(
		evaluationContext, claim.Record, snapshot, lineage,
	)
	return result, false, err
}

func (worker *Worker) resolvePreviousProfile(
	ctx context.Context,
	claim *Claim,
	snapshot *IELTSProfileInputSnapshot,
) (bool, error) {
	record, err := worker.store.GetRecordBySource(
		ctx, claim.UserID, KindIELTSPart1Profile, snapshot.SessionID,
	)
	if err == nil && (record.Status == JobQueued || record.Status == JobRunning) &&
		time.Now().UTC().Before(record.CreatedAt.Add(
			worker.configuration.profileLifecycleWaitBudget(previousProfileLifecycleStages),
		)) {
		finalizeContext, cancel := worker.finalizeContext(ctx)
		defer cancel()
		return true, worker.store.DeferClaim(finalizeContext, Deferral{
			UserID: claim.UserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
			AvailableAt: time.Now().UTC().Add(worker.configuration.DependencyDelay),
		})
	}
	if err == nil && record.Status == JobReady {
		var profile IELTSCumulativeProfile
		if DecodeStrict(record.Result, &profile) == nil && profile.Valid() &&
			profile.SessionID == snapshot.SessionID && len(profile.CompletedParts) == 1 {
			snapshot.PreviousProfile = &profile
			snapshot.DependencyResolution = IELTSProfileDependencyResolved
		} else {
			snapshot.DependencyResolution = IELTSProfileDependencyFallback
		}
	} else if errors.Is(err, ErrNotFound) || err == nil {
		snapshot.DependencyResolution = IELTSProfileDependencyFallback
	} else {
		return false, err
	}
	return false, worker.checkpointProfileSnapshot(ctx, claim, *snapshot)
}

func (worker *Worker) resolveProfileAcoustics(
	ctx context.Context,
	claim *Claim,
	snapshot *IELTSProfileInputSnapshot,
) (bool, error) {
	turnIDs := make([]string, 0, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if turn.AudioAssetID != "" && turn.Acoustic == nil {
			turnIDs = append(turnIDs, turn.ID)
		}
	}
	if len(turnIDs) == 0 {
		return false, nil
	}
	read, err := worker.sessionAudio.ReadSessionAcoustics(
		ctx, claim.UserID, snapshot.SessionID, turnIDs,
	)
	if err != nil {
		return false, err
	}
	timedOut := acousticDependencyTimedOut(
		claim.CreatedAt,
		worker.configuration.AcousticDependencyMaxWait,
	)
	if read.Pending && !timedOut {
		finalizeContext, cancel := worker.finalizeContext(ctx)
		defer cancel()
		return true, worker.store.DeferClaim(finalizeContext, Deferral{
			UserID: claim.UserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
			AvailableAt: time.Now().UTC().Add(worker.configuration.DependencyDelay),
		})
	}
	if err := applyAcousticDependencies(
		snapshot.Turns,
		turnIDs,
		read,
		read.Pending && timedOut,
	); err != nil {
		return false, err
	}
	return false, worker.checkpointProfileSnapshot(ctx, claim, *snapshot)
}

func (worker *Worker) checkpointProfileSnapshot(
	ctx context.Context,
	claim *Claim,
	snapshot IELTSProfileInputSnapshot,
) error {
	if !snapshot.Valid() {
		return ErrInvalidRequest
	}
	encoded, digest, err := EncodeStrict(snapshot)
	if err != nil {
		return err
	}
	finalizeContext, cancel := worker.finalizeContext(ctx)
	updated, err := worker.store.CheckpointSnapshot(finalizeContext, SnapshotCheckpoint{
		UserID: claim.UserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
		InputSnapshot: encoded, InputHash: digest,
	})
	cancel()
	if err == nil {
		claim.Record = updated
	}
	return err
}

func (worker *Worker) ProcessSession(ctx context.Context) (bool, error) {
	if worker == nil || worker.store == nil || ctx == nil {
		return false, ErrInvalidRequest
	}
	claim, err := worker.store.ClaimNext(ctx, worker.configuration.SessionLane)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, deferred, err := worker.evaluateSession(ctx, &claim)
	if deferred {
		return true, err
	}
	if err != nil {
		return true, worker.fail(ctx, claim, err)
	}
	if err := worker.complete(ctx, claim, result, nil); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *Worker) ProcessSpeech(ctx context.Context) (bool, error) {
	if worker == nil || worker.store == nil || ctx == nil {
		return false, ErrInvalidRequest
	}
	claim, err := worker.store.ClaimNext(ctx, worker.configuration.SpeechLane)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, items, err := worker.evaluateSpeech(ctx, &claim)
	if err != nil {
		return true, worker.fail(ctx, claim, err)
	}
	if err := worker.complete(ctx, claim, result, items); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *Worker) evaluateSession(
	ctx context.Context,
	claim *Claim,
) (json.RawMessage, bool, error) {
	if claim == nil {
		return nil, false, ErrInvalidRequest
	}
	if claim.Kind != KindSessionReport {
		return nil, false, ErrInvalidRequest
	}
	var snapshot SessionInputSnapshot
	var lineage ConfigLineage
	if err := DecodeStrict(claim.InputSnapshot, &snapshot); err != nil ||
		!snapshot.Valid() ||
		DecodeStrict(claim.ConfigLineage, &lineage) != nil || !lineage.Valid() {
		return nil, false, ErrInvalidRequest
	}
	if lineage.StrategyRef == IELTSStrategyRef && worker.configuration.AcousticsEnabled {
		deferred, err := worker.resolveSessionAcoustics(ctx, claim, &snapshot)
		if err != nil || deferred {
			return nil, deferred, err
		}
	}
	if lineage.StrategyRef == IELTSStrategyRef &&
		snapshot.PracticeMode == "FULL_MOCK" && snapshot.ProfileResolution == "" {
		deferred, err := worker.resolveFinalProfile(ctx, claim, &snapshot)
		if err != nil || deferred {
			return nil, deferred, err
		}
	}
	var deadline time.Duration
	var evaluate func(context.Context) (json.RawMessage, error)
	switch lineage.StrategyRef {
	case InterviewStrategyRef:
		deadline = worker.configuration.InterviewDeadline
		evaluate = func(evaluationContext context.Context) (json.RawMessage, error) {
			return worker.sessions.EvaluateInterview(evaluationContext, claim.Record, snapshot, lineage)
		}
	case IELTSStrategyRef:
		deadline = worker.configuration.IELTSDeadline
		evaluate = func(evaluationContext context.Context) (json.RawMessage, error) {
			return worker.sessions.EvaluateIELTS(evaluationContext, claim.Record, snapshot, lineage)
		}
	case GeneralStrategyRef:
		deadline = worker.configuration.GeneralDeadline
		evaluate = func(evaluationContext context.Context) (json.RawMessage, error) {
			return worker.sessions.EvaluateGeneral(evaluationContext, claim.Record, snapshot, lineage)
		}
	default:
		return nil, false, ErrInvalidRequest
	}
	evaluationContext, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	result, err := evaluate(evaluationContext)
	if err != nil {
		return nil, false, err
	}
	if !json.Valid(result) {
		return nil, false, ErrInvalidRequest
	}
	return result, false, nil
}

func (worker *Worker) resolveFinalProfile(
	ctx context.Context,
	claim *Claim,
	snapshot *SessionInputSnapshot,
) (bool, error) {
	record, err := worker.store.GetRecordBySource(
		ctx, claim.UserID, KindIELTSPart2Profile, snapshot.SessionID,
	)
	if err == nil && (record.Status == JobQueued || record.Status == JobRunning) &&
		time.Now().UTC().Before(record.CreatedAt.Add(
			worker.configuration.profileLifecycleWaitBudget(finalProfileLifecycleStages),
		)) {
		finalizeContext, cancel := worker.finalizeContext(ctx)
		defer cancel()
		return true, worker.store.DeferClaim(finalizeContext, Deferral{
			UserID: claim.UserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
			AvailableAt: time.Now().UTC().Add(worker.configuration.DependencyDelay),
		})
	}
	if err == nil && record.Status == JobReady {
		var profile IELTSCumulativeProfile
		if DecodeStrict(record.Result, &profile) == nil && profile.Valid() &&
			profile.SessionID == snapshot.SessionID && len(profile.CompletedParts) == 2 {
			snapshot.CumulativeProfile = &profile
			snapshot.ProfileResolution = IELTSFinalProfileResolved
		} else {
			snapshot.ProfileResolution = IELTSFinalProfileFallback
		}
	} else if errors.Is(err, ErrNotFound) || err == nil {
		snapshot.ProfileResolution = IELTSFinalProfileFallback
	} else {
		return false, err
	}
	if !snapshot.Valid() {
		return false, ErrInvalidRequest
	}
	encoded, digest, err := EncodeStrict(*snapshot)
	if err != nil {
		return false, err
	}
	finalizeContext, cancel := worker.finalizeContext(ctx)
	updated, err := worker.store.CheckpointSnapshot(finalizeContext, SnapshotCheckpoint{
		UserID: claim.UserID, ID: claim.ID, LeaseToken: claim.LeaseToken,
		InputSnapshot: encoded, InputHash: digest,
	})
	cancel()
	if err == nil {
		claim.Record = updated
	}
	return false, err
}

func (worker *Worker) resolveSessionAcoustics(
	ctx context.Context,
	claim *Claim,
	snapshot *SessionInputSnapshot,
) (bool, error) {
	turnIDs := make([]string, 0, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if turn.Effective && turn.AudioAssetID != "" && turn.Acoustic == nil {
			turnIDs = append(turnIDs, turn.ID)
		}
	}
	if len(turnIDs) == 0 {
		return false, nil
	}
	read, err := worker.sessionAudio.ReadSessionAcoustics(
		ctx, claim.UserID, snapshot.SessionID, turnIDs,
	)
	if err != nil {
		return false, err
	}
	timedOut := acousticDependencyTimedOut(
		claim.CreatedAt,
		worker.configuration.AcousticDependencyMaxWait,
	)
	if read.Pending && !timedOut {
		finalizeContext, cancel := worker.finalizeContext(ctx)
		defer cancel()
		err := worker.store.DeferClaim(finalizeContext, Deferral{
			UserID:      claim.UserID,
			ID:          claim.ID,
			LeaseToken:  claim.LeaseToken,
			AvailableAt: time.Now().UTC().Add(worker.configuration.DependencyDelay),
		})
		return true, err
	}
	if err := applyAcousticDependencies(
		snapshot.Turns,
		turnIDs,
		read,
		read.Pending && timedOut,
	); err != nil {
		return false, err
	}
	encoded, digest, err := EncodeStrict(*snapshot)
	if err != nil {
		return false, err
	}
	finalizeContext, cancel := worker.finalizeContext(ctx)
	updated, err := worker.store.CheckpointSnapshot(finalizeContext, SnapshotCheckpoint{
		UserID:        claim.UserID,
		ID:            claim.ID,
		LeaseToken:    claim.LeaseToken,
		InputSnapshot: encoded,
		InputHash:     digest,
	})
	cancel()
	if err != nil {
		return false, err
	}
	claim.Record = updated
	return false, nil
}

func (worker *Worker) evaluateSpeech(
	ctx context.Context,
	claim *Claim,
) (json.RawMessage, []FeedbackItemDraft, error) {
	if claim == nil || (claim.Kind != KindPracticeTurnFeedback &&
		claim.Kind != KindAgentMessageFeedback) {
		return nil, nil, ErrInvalidRequest
	}
	var snapshot SpeechInputSnapshot
	var lineage ConfigLineage
	if err := DecodeStrict(claim.InputSnapshot, &snapshot); err != nil ||
		!snapshot.Valid(claim.Kind) ||
		DecodeStrict(claim.ConfigLineage, &lineage) != nil || !lineage.Valid() {
		return nil, nil, ErrInvalidRequest
	}
	evaluationContext, cancel := context.WithTimeout(
		ctx,
		worker.configuration.SpeechDeadline,
	)
	defer cancel()
	if claim.Kind == KindPracticeTurnFeedback && snapshot.Acoustic == nil &&
		snapshot.AudioAssetID != "" {
		if !worker.configuration.AcousticsEnabled || worker.acoustics == nil {
			return nil, nil, ErrInvalidRequest
		}
		checkpoint, err := worker.acoustics.EvaluateAcoustic(
			evaluationContext,
			claim.Record,
			snapshot,
		)
		if err != nil {
			failure := stableJobError(err)
			if failure.Retryable &&
				claim.AttemptCount < worker.configuration.SpeechLane.MaxAttempts {
				return nil, nil, err
			}
			checkpoint = AcousticCheckpoint{
				Status: AcousticNotAssessed,
				Reason: "ACOUSTIC_ASSESSMENT_FAILED",
			}
		}
		if !checkpoint.Valid() {
			return nil, nil, ErrInvalidRequest
		}
		snapshot.Acoustic = &checkpoint
		encoded, digest, err := EncodeStrict(snapshot)
		if err != nil {
			return nil, nil, err
		}
		finalizeContext, finalizeCancel := worker.finalizeContext(ctx)
		updated, checkpointErr := worker.store.CheckpointSnapshot(
			finalizeContext,
			SnapshotCheckpoint{
				UserID:               claim.UserID,
				ID:                   claim.ID,
				LeaseToken:           claim.LeaseToken,
				InputSnapshot:        encoded,
				InputHash:            digest,
				RestartAttemptBudget: true,
			},
		)
		finalizeCancel()
		if checkpointErr != nil {
			return nil, nil, checkpointErr
		}
		claim.Record = updated
	}
	var result json.RawMessage
	var items []FeedbackItemDraft
	var err error
	switch claim.Kind {
	case KindPracticeTurnFeedback:
		result, items, err = worker.speech.EvaluatePracticeTurn(
			evaluationContext,
			snapshot,
			lineage,
		)
	case KindAgentMessageFeedback:
		if snapshot.Acoustic == nil ||
			snapshot.Acoustic.Status != AcousticNotAssessed {
			return nil, nil, ErrInvalidRequest
		}
		result, items, err = worker.speech.EvaluateAgentMessage(
			evaluationContext,
			snapshot,
			lineage,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	var speechResult SpeechResult
	if DecodeStrict(result, &speechResult) != nil || !speechResult.Valid() ||
		len(items) > 32 {
		return nil, nil, ErrInvalidRequest
	}
	for _, item := range items {
		if !item.Valid() {
			return nil, nil, ErrInvalidRequest
		}
	}
	return result, items, nil
}

func (worker *Worker) complete(
	ctx context.Context,
	claim Claim,
	result json.RawMessage,
	items []FeedbackItemDraft,
) error {
	finalizeContext, cancel := worker.finalizeContext(ctx)
	defer cancel()
	err := worker.store.CompleteClaim(finalizeContext, Completion{
		UserID:     claim.UserID,
		ID:         claim.ID,
		LeaseToken: claim.LeaseToken,
		Result:     result,
		Items:      items,
	})
	if err == nil || errors.Is(err, ErrClaimLost) {
		return err
	}
	failErr := worker.fail(ctx, claim, err)
	if failErr != nil {
		return errors.Join(err, failErr)
	}
	return err
}

func (worker *Worker) fail(ctx context.Context, claim Claim, cause error) error {
	failure := stableJobError(cause)
	processing := &processingFailure{
		record: claim.Record, failure: failure, cause: cause,
	}
	finalizeContext, cancel := worker.finalizeContext(ctx)
	defer cancel()
	err := worker.store.FailClaim(finalizeContext, Failure{
		UserID:             claim.UserID,
		ID:                 claim.ID,
		LeaseToken:         claim.LeaseToken,
		Error:              failure,
		AutomaticRetryable: automaticRetryable(cause, failure),
		RetryAt:            time.Now().UTC().Add(worker.configuration.RetryDelay),
		MaxAttempts:        maxAttemptsFor(claim.Kind, worker.configuration),
	})
	if err != nil {
		return errors.Join(processing, err)
	}
	return processing
}

func (worker *Worker) finalizeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		worker.configuration.FinalizeTimeout,
	)
}

type stableFailure interface {
	error
	StableCategory() string
	Retryable() bool
}

type automaticRetryableFailure interface {
	AutomaticRetryable() bool
}

func automaticRetryable(err error, publicFailure JobError) bool {
	var automatic automaticRetryableFailure
	if errors.As(err, &automatic) {
		return automatic.AutomaticRetryable()
	}
	return publicFailure.Retryable
}

func stableJobError(err error) JobError {
	failure := JobError{
		Code:      "INTERNAL_PROCESSING_ERROR",
		Retryable: true,
		Message:   "Evaluation processing failed.",
	}
	var stable stableFailure
	if errors.As(err, &stable) && validIdentifier(stable.StableCategory()) {
		failure.Code = stable.StableCategory()
		failure.Retryable = stable.Retryable()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Code = "PROCESSING_TIMEOUT"
		failure.Retryable = true
	}
	if errors.Is(err, ErrInvalidRequest) {
		failure.Code = "INVALID_EVALUATION_INPUT"
		failure.Retryable = false
	}
	return failure
}

func maxAttemptsFor(kind Kind, configuration WorkerConfiguration) int {
	switch kind {
	case KindSessionReport:
		return configuration.SessionLane.MaxAttempts
	case KindIELTSPart1Profile, KindIELTSPart2Profile:
		return configuration.ProfileLane.MaxAttempts
	case KindPracticeTurnFeedback, KindAgentMessageFeedback:
		return configuration.SpeechLane.MaxAttempts
	default:
		return 0
	}
}

func acousticDependencyTimedOut(createdAt time.Time, maxWait time.Duration) bool {
	return !time.Now().UTC().Before(createdAt.Add(maxWait))
}

func applyAcousticDependencies(
	turns []SessionEvidenceTurn,
	turnIDs []string,
	read SessionAcousticRead,
	markPendingNotAssessed bool,
) error {
	pending := make(map[string]struct{}, len(turnIDs))
	for _, turnID := range turnIDs {
		pending[turnID] = struct{}{}
	}
	for turnID := range read.Checkpoints {
		if _, expected := pending[turnID]; !expected {
			return ErrInvalidRequest
		}
	}
	for index := range turns {
		if _, exists := pending[turns[index].ID]; !exists {
			continue
		}
		checkpoint, exists := read.Checkpoints[turns[index].ID]
		if !exists {
			if !markPendingNotAssessed {
				return ErrInvalidRequest
			}
			checkpoint = AcousticCheckpoint{
				Status: AcousticNotAssessed,
				Reason: acousticDependencyTimeoutReason,
			}
		}
		if !checkpoint.Valid() {
			return ErrInvalidRequest
		}
		turns[index].Acoustic = &checkpoint
		delete(pending, turns[index].ID)
	}
	if len(pending) != 0 {
		return ErrInvalidRequest
	}
	return nil
}

func containsKind(kinds []Kind, expected Kind) bool {
	for _, kind := range kinds {
		if kind == expected {
			return true
		}
	}
	return false
}
