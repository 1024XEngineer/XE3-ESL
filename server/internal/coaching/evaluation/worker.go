package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const MinimumIELTSTwoRoundDeadline = 2*45*time.Second + 10*time.Second

var ErrAcousticDependencyFailed error = acousticDependencyFailure{}

type acousticDependencyFailure struct{}

func (acousticDependencyFailure) Error() string {
	return "evaluation: IELTS acoustic dependency failed"
}
func (acousticDependencyFailure) StableCategory() string {
	return "ACOUSTIC_DEPENDENCY_FAILED"
}
func (acousticDependencyFailure) Retryable() bool { return false }

type SessionEvaluators interface {
	EvaluateInterview(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
	EvaluateIELTS(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
	EvaluateGeneral(context.Context, Record, SessionInputSnapshot, ConfigLineage) (json.RawMessage, error)
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
	SessionLane       ClaimLane
	SpeechLane        ClaimLane
	AcousticsEnabled  bool
	InterviewDeadline time.Duration
	IELTSDeadline     time.Duration
	GeneralDeadline   time.Duration
	SpeechDeadline    time.Duration
	RetryDelay        time.Duration
	DependencyDelay   time.Duration
	FinalizeTimeout   time.Duration
}

func (configuration WorkerConfiguration) Valid() bool {
	return configuration.SessionLane.Valid() &&
		len(configuration.SessionLane.Kinds) == 1 &&
		configuration.SessionLane.Kinds[0] == KindSessionReport &&
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
		configuration.RetryDelay >= 0 && configuration.RetryDelay <= time.Hour &&
		configuration.DependencyDelay >= time.Second &&
		configuration.DependencyDelay <= time.Minute &&
		configuration.FinalizeTimeout >= time.Second &&
		configuration.FinalizeTimeout <= 30*time.Second
}

type Worker struct {
	store         Store
	sessions      SessionEvaluators
	speech        SpeechEvaluators
	acoustics     AcousticEvaluator
	sessionAudio  SessionAcousticSource
	configuration WorkerConfiguration
}

func NewWorker(
	store Store,
	sessions SessionEvaluators,
	speech SpeechEvaluators,
	acoustics AcousticEvaluator,
	sessionAudio SessionAcousticSource,
	configuration WorkerConfiguration,
) (*Worker, error) {
	if store == nil || sessions == nil || speech == nil ||
		(configuration.AcousticsEnabled &&
			(acoustics == nil || sessionAudio == nil)) ||
		!configuration.Valid() {
		return nil, ErrInvalidRequest
	}
	return &Worker{
		store:         store,
		sessions:      sessions,
		speech:        speech,
		acoustics:     acoustics,
		sessionAudio:  sessionAudio,
		configuration: configuration,
	}, nil
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
	if read.Pending {
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
	if len(read.Checkpoints) != len(turnIDs) {
		return false, ErrInvalidRequest
	}
	for index := range snapshot.Turns {
		checkpoint, exists := read.Checkpoints[snapshot.Turns[index].ID]
		if !exists {
			continue
		}
		if !checkpoint.Valid() {
			return false, ErrInvalidRequest
		}
		snapshot.Turns[index].Acoustic = &checkpoint
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
				UserID:        claim.UserID,
				ID:            claim.ID,
				LeaseToken:    claim.LeaseToken,
				InputSnapshot: encoded,
				InputHash:     digest,
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
	finalizeContext, cancel := worker.finalizeContext(ctx)
	defer cancel()
	err := worker.store.FailClaim(finalizeContext, Failure{
		UserID:      claim.UserID,
		ID:          claim.ID,
		LeaseToken:  claim.LeaseToken,
		Error:       failure,
		RetryAt:     time.Now().UTC().Add(worker.configuration.RetryDelay),
		MaxAttempts: maxAttemptsFor(claim.Kind, worker.configuration),
	})
	if err != nil {
		return errors.Join(cause, err)
	}
	return cause
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
	if kind == KindSessionReport {
		return configuration.SessionLane.MaxAttempts
	}
	return configuration.SpeechLane.MaxAttempts
}

func containsKind(kinds []Kind, expected Kind) bool {
	for _, kind := range kinds {
		if kind == expected {
			return true
		}
	}
	return false
}
