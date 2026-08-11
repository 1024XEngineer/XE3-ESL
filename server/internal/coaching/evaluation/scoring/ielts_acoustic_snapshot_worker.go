package scoring

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

const (
	IELTSAcousticSnapshotWaitDurationV1 = 120 * time.Second
	IELTSAcousticEvidenceFailureCode    = "acoustic_evidence_invalid"
)

type IELTSAcousticSnapshotClaim struct {
	EvaluationID         string
	EvaluationRevisionID string
	OwnerUserID          string
	RevisionCreatedAt    time.Time
	Snapshot             evidence.EvidenceSnapshot
}

func (claim IELTSAcousticSnapshotClaim) Valid() bool {
	return validUUID(claim.EvaluationID) &&
		validUUID(claim.EvaluationRevisionID) &&
		validUUID(claim.OwnerUserID) &&
		!claim.RevisionCreatedAt.IsZero() &&
		claim.Snapshot.Valid() &&
		claim.Snapshot.OwnerUserID == claim.OwnerUserID &&
		claim.Snapshot.Scope == evaluation.ScopeSession &&
		claim.Snapshot.SceneType == evaluation.SceneIELTSSpeaking
}

type IELTSAcousticSnapshotRepository interface {
	FindPendingIELTSAcousticSnapshot(
		context.Context,
	) (IELTSAcousticSnapshotClaim, bool, error)
	EnsureIELTSAcousticSnapshot(
		context.Context,
		IELTSAcousticSnapshotClaim,
		IELTSAcousticSnapshot,
	) (IELTSAcousticSnapshot, bool, error)
	FailIELTSAcousticSnapshot(
		context.Context,
		IELTSAcousticSnapshotClaim,
	) error
}

type IELTSSpeakingAcousticSource interface {
	ReadIELTSSpeakingAcoustics(
		context.Context,
		string,
		[]IELTSSpeakingAcousticRequest,
	) (IELTSSpeakingAcousticRead, error)
}

type IELTSAcousticSnapshotSweepResult struct {
	Inspected int
	Frozen    int
	Failed    int
}

type IELTSAcousticSnapshotFreezer struct {
	repository   IELTSAcousticSnapshotRepository
	source       IELTSSpeakingAcousticSource
	waitDuration time.Duration
	now          func() time.Time
}

func NewIELTSAcousticSnapshotFreezer(
	repository IELTSAcousticSnapshotRepository,
	source IELTSSpeakingAcousticSource,
	waitDuration time.Duration,
) (*IELTSAcousticSnapshotFreezer, error) {
	if repository == nil || source == nil || waitDuration < time.Second ||
		waitDuration > 10*time.Minute {
		return nil, evaluation.ErrInvalidRequest
	}
	return &IELTSAcousticSnapshotFreezer{
		repository:   repository,
		source:       source,
		waitDuration: waitDuration,
		now:          time.Now,
	}, nil
}

func (freezer *IELTSAcousticSnapshotFreezer) ProcessPending(
	ctx context.Context,
	limit int,
) (IELTSAcousticSnapshotSweepResult, error) {
	if freezer == nil || freezer.repository == nil || freezer.source == nil ||
		freezer.now == nil || ctx == nil || limit < 1 || limit > 20 {
		return IELTSAcousticSnapshotSweepResult{}, evaluation.ErrInvalidRequest
	}
	var sweep IELTSAcousticSnapshotSweepResult
	for sweep.Inspected < limit {
		claim, found, err := freezer.repository.
			FindPendingIELTSAcousticSnapshot(ctx)
		if err != nil {
			return sweep, err
		}
		if !found {
			return sweep, nil
		}
		if !claim.Valid() {
			return sweep, evaluation.ErrInvalidRequest
		}
		sweep.Inspected++
		requests, err := ieltsSpeakingAcousticRequests(claim.Snapshot)
		if err != nil {
			if errors.Is(err, evaluation.ErrInvalidRequest) {
				if err := freezer.repository.FailIELTSAcousticSnapshot(
					ctx,
					claim,
				); err != nil {
					return sweep, err
				}
				sweep.Failed++
				continue
			}
			return sweep, err
		}
		read, err := freezer.source.ReadIELTSSpeakingAcoustics(
			ctx,
			claim.OwnerUserID,
			requests,
		)
		if err != nil {
			if errors.Is(err, ErrIELTSAcousticEvidenceInvalid) {
				if err := freezer.repository.FailIELTSAcousticSnapshot(
					ctx,
					claim,
				); err != nil {
					return sweep, err
				}
				sweep.Failed++
				continue
			}
			return sweep, err
		}
		deadlineReached := !freezer.now().UTC().Before(
			claim.RevisionCreatedAt.Add(freezer.waitDuration),
		)
		draft, err := BuildIELTSAcousticSnapshot(
			claim.EvaluationID,
			claim.Snapshot,
			read,
			deadlineReached,
		)
		if err == ErrIELTSAcousticSnapshotPending {
			return sweep, nil
		}
		if err != nil {
			if errors.Is(err, evaluation.ErrInvalidRequest) {
				if err := freezer.repository.FailIELTSAcousticSnapshot(
					ctx,
					claim,
				); err != nil {
					return sweep, err
				}
				sweep.Failed++
				continue
			}
			return sweep, err
		}
		_, _, err = freezer.repository.EnsureIELTSAcousticSnapshot(
			ctx,
			claim,
			draft,
		)
		if err != nil {
			return sweep, err
		}
		sweep.Frozen++
	}
	return sweep, nil
}
