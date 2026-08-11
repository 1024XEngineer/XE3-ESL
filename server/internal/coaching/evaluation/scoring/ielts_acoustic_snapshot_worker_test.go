package scoring

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestIELTSAcousticSnapshotFreezerKeepsRevisionValidatingBeforeDeadline(
	t *testing.T,
) {
	t.Parallel()
	claim := ieltsAcousticSnapshotClaimForTest(t)
	repository := &ieltsAcousticSnapshotRepositoryStub{claim: claim}
	source := &ieltsAcousticSnapshotSourceStub{
		read: IELTSSpeakingAcousticRead{
			PendingTurnIDs: []string{"turn-1"},
		},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return claim.RevisionCreatedAt.Add(
			IELTSAcousticSnapshotWaitDurationV1 - time.Millisecond,
		)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (IELTSAcousticSnapshotSweepResult{Inspected: 1}) ||
		repository.ensureCalls != 0 || source.calls != 1 {
		t.Fatalf("sweep=%#v repository=%#v source=%#v", sweep, repository, source)
	}
}

func TestIELTSAcousticSnapshotFreezerDeadlineIsImmutableAgainstLateResult(
	t *testing.T,
) {
	t.Parallel()
	claim := ieltsAcousticSnapshotClaimForTest(t)
	repository := &ieltsAcousticSnapshotRepositoryStub{claim: claim}
	source := &ieltsAcousticSnapshotSourceStub{
		read: IELTSSpeakingAcousticRead{
			PendingTurnIDs: []string{"turn-1"},
		},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return claim.RevisionCreatedAt.Add(
			IELTSAcousticSnapshotWaitDurationV1,
		)
	}
	firstSweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if firstSweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 1,
		Frozen:    1,
	}) || repository.ensureCalls != 1 ||
		repository.stored.Resolution != IELTSAcousticSnapshotTextOnly {
		t.Fatalf("sweep=%#v repository=%#v", firstSweep, repository)
	}
	frozenPayload := bytes.Clone(repository.stored.Payload)
	request, err := ieltsSpeakingAcousticRequests(claim.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	fluency := 80.0
	source.read = IELTSSpeakingAcousticRead{
		Values: []IELTSSpeakingTurnAcoustics{{
			TurnID:               request[0].TurnID,
			EvidenceRefID:        request[0].EvidenceRefID,
			PronunciationScore:   80,
			AcousticFluencyScore: &fluency,
			Provider:             "xfyun_ise",
			ProviderRun:          "run_0123456789abcdef01234567",
		}},
	}
	secondSweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if secondSweep != (IELTSAcousticSnapshotSweepResult{}) ||
		repository.ensureCalls != 1 || source.calls != 1 ||
		!bytes.Equal(repository.stored.Payload, frozenPayload) {
		t.Fatalf("sweep=%#v repository=%#v source=%#v", secondSweep, repository, source)
	}
}

func TestIELTSAcousticSnapshotFreezerUsesRevisionCutoffAfterLateFirstSweep(
	t *testing.T,
) {
	t.Parallel()
	claim := ieltsAcousticSnapshotClaimForTest(t)
	repository := &ieltsAcousticSnapshotRepositoryStub{claim: claim}
	source := &ieltsAcousticSnapshotSourceStub{}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	expectedCutoff := claim.RevisionCreatedAt.Add(
		IELTSAcousticSnapshotWaitDurationV1,
	).UTC()
	freezer.now = func() time.Time {
		return expectedCutoff.Add(5 * time.Second)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 1,
		Frozen:    1,
	}) || !source.completedBy.Equal(expectedCutoff) ||
		repository.stored.Resolution != IELTSAcousticSnapshotTextOnly {
		t.Fatalf(
			"sweep=%#v completedBy=%v repository=%#v",
			sweep,
			source.completedBy,
			repository,
		)
	}
}

func TestIELTSAcousticSnapshotFreezerFreezesTerminalBeforeDeadline(
	t *testing.T,
) {
	t.Parallel()
	claim := ieltsAcousticSnapshotClaimForTest(t)
	repository := &ieltsAcousticSnapshotRepositoryStub{claim: claim}
	source := &ieltsAcousticSnapshotSourceStub{}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return claim.RevisionCreatedAt.Add(time.Second)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 1,
		Frozen:    1,
	}) || repository.ensureCalls != 1 {
		t.Fatalf("sweep=%#v repository=%#v", sweep, repository)
	}
}

func TestIELTSAcousticSnapshotFreezerFailsCorruptHeadAndContinues(
	t *testing.T,
) {
	t.Parallel()
	first := ieltsAcousticSnapshotClaimForTest(t)
	second := first
	second.EvaluationID = "72000000-0000-4000-8000-000000000007"
	second.EvaluationRevisionID = "62000000-0000-4000-8000-000000000006"
	repository := &ieltsAcousticSnapshotQueueRepositoryStub{
		claims: []IELTSAcousticSnapshotClaim{first, second},
	}
	source := &ieltsAcousticSnapshotQueueSourceStub{
		errors: []error{ErrIELTSAcousticEvidenceInvalid, nil},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return first.RevisionCreatedAt.Add(
			IELTSAcousticSnapshotWaitDurationV1,
		)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if sweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 2,
		Frozen:    1,
		Failed:    1,
	}) || repository.failed != 1 || repository.frozen != 1 ||
		len(repository.claims) != 0 || source.calls != 2 {
		t.Fatalf("sweep=%#v repository=%#v source=%#v", sweep, repository, source)
	}
}

func TestIELTSAcousticSnapshotFreezerSkipsPendingHeadWithinSweep(
	t *testing.T,
) {
	t.Parallel()
	first := ieltsAcousticSnapshotClaimForTest(t)
	second := first
	second.EvaluationID = "72000000-0000-4000-8000-000000000007"
	second.EvaluationRevisionID = "62000000-0000-4000-8000-000000000006"
	repository := &ieltsAcousticSnapshotQueueRepositoryStub{
		claims: []IELTSAcousticSnapshotClaim{first, second},
	}
	source := &ieltsAcousticSnapshotQueueSourceStub{
		reads: []IELTSSpeakingAcousticRead{{
			PendingTurnIDs: []string{"turn-1"},
		}, {}},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return first.RevisionCreatedAt.Add(time.Second)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 2)
	if err != nil || sweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 2,
		Frozen:    1,
	}) || repository.frozen != 1 || len(repository.claims) != 1 {
		t.Fatalf("sweep=%#v err=%v repository=%#v", sweep, err, repository)
	}
}

func TestIELTSAcousticSnapshotFreezerCursorReachesPastSweepLimit(
	t *testing.T,
) {
	t.Parallel()
	first := ieltsAcousticSnapshotClaimForTest(t)
	second := first
	second.EvaluationID = "72000000-0000-4000-8000-000000000007"
	second.EvaluationRevisionID = "62000000-0000-4000-8000-000000000006"
	repository := &ieltsAcousticSnapshotQueueRepositoryStub{
		claims: []IELTSAcousticSnapshotClaim{first, second},
	}
	source := &ieltsAcousticSnapshotQueueSourceStub{
		reads: []IELTSSpeakingAcousticRead{{
			PendingTurnIDs: []string{"turn-1"},
		}, {}},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return first.RevisionCreatedAt.Add(time.Second)
	}
	firstSweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil || firstSweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 1,
	}) {
		t.Fatalf("first sweep=%#v err=%v", firstSweep, err)
	}
	secondSweep, err := freezer.ProcessPending(context.Background(), 1)
	if err != nil || secondSweep != (IELTSAcousticSnapshotSweepResult{
		Inspected: 1,
		Frozen:    1,
	}) || repository.frozen != 1 || len(repository.claims) != 1 {
		t.Fatalf(
			"second sweep=%#v err=%v repository=%#v",
			secondSweep,
			err,
			repository,
		)
	}
}

func TestIELTSAcousticSnapshotFreezerSkipsTransientHeadWithinSweep(
	t *testing.T,
) {
	t.Parallel()
	first := ieltsAcousticSnapshotClaimForTest(t)
	second := first
	second.EvaluationID = "72000000-0000-4000-8000-000000000007"
	second.EvaluationRevisionID = "62000000-0000-4000-8000-000000000006"
	repository := &ieltsAcousticSnapshotQueueRepositoryStub{
		claims: []IELTSAcousticSnapshotClaim{first, second},
	}
	sourceErr := errors.New("source temporarily unavailable")
	source := &ieltsAcousticSnapshotQueueSourceStub{
		errors: []error{sourceErr, nil},
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		source,
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return first.RevisionCreatedAt.Add(time.Second)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 2)
	if !errors.Is(err, sourceErr) ||
		sweep != (IELTSAcousticSnapshotSweepResult{
			Inspected: 2,
			Frozen:    1,
		}) || repository.frozen != 1 || len(repository.claims) != 1 {
		t.Fatalf("sweep=%#v err=%v repository=%#v", sweep, err, repository)
	}
}

func TestIELTSAcousticSnapshotFreezerPreservesTransientFailures(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "context deadline", err: context.DeadlineExceeded},
		{name: "source transient", err: errors.New("source unavailable")},
		{name: "generic invalid request", err: evaluation.ErrInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			claim := ieltsAcousticSnapshotClaimForTest(t)
			repository := &ieltsAcousticSnapshotRepositoryStub{claim: claim}
			source := &ieltsAcousticSnapshotQueueSourceStub{
				errors: []error{test.err},
			}
			freezer, err := NewIELTSAcousticSnapshotFreezer(
				repository,
				source,
				IELTSAcousticSnapshotWaitDurationV1,
			)
			if err != nil {
				t.Fatal(err)
			}
			sweep, err := freezer.ProcessPending(context.Background(), 1)
			if !errors.Is(err, test.err) ||
				sweep != (IELTSAcousticSnapshotSweepResult{Inspected: 1}) ||
				repository.failCalls != 0 || repository.ensureCalls != 0 {
				t.Fatalf(
					"sweep=%#v err=%v repository=%#v",
					sweep,
					err,
					repository,
				)
			}
		})
	}
}

func TestIELTSAcousticSnapshotFreezerPreservesRepositoryFailure(
	t *testing.T,
) {
	t.Parallel()
	repositoryErr := errors.New("database unavailable")
	repository := &ieltsAcousticSnapshotRepositoryStub{
		claim:   ieltsAcousticSnapshotClaimForTest(t),
		findErr: repositoryErr,
	}
	freezer, err := NewIELTSAcousticSnapshotFreezer(
		repository,
		&ieltsAcousticSnapshotSourceStub{},
		IELTSAcousticSnapshotWaitDurationV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := freezer.ProcessPending(context.Background(), 1)
	if !errors.Is(err, repositoryErr) ||
		sweep != (IELTSAcousticSnapshotSweepResult{}) ||
		repository.failCalls != 0 || repository.ensureCalls != 0 {
		t.Fatalf("sweep=%#v err=%v repository=%#v", sweep, err, repository)
	}
}

func ieltsAcousticSnapshotClaimForTest(
	t *testing.T,
) IELTSAcousticSnapshotClaim {
	t.Helper()
	return IELTSAcousticSnapshotClaim{
		EvaluationID:         testEvalID,
		EvaluationRevisionID: testRevID,
		OwnerUserID:          testOwnerA,
		RevisionCreatedAt:    time.Now().UTC(),
		Snapshot:             ieltsSpeakingAcousticTestSnapshot(t),
	}
}

type ieltsAcousticSnapshotRepositoryStub struct {
	claim       IELTSAcousticSnapshotClaim
	stored      IELTSAcousticSnapshot
	ensureCalls int
	failCalls   int
	findErr     error
}

func (stub *ieltsAcousticSnapshotRepositoryStub) FindPendingIELTSAcousticSnapshot(
	context.Context,
	*IELTSAcousticSnapshotCursor,
) (IELTSAcousticSnapshotClaim, bool, error) {
	if stub.findErr != nil {
		return IELTSAcousticSnapshotClaim{}, false, stub.findErr
	}
	return stub.claim, stub.stored.ID == "", nil
}

func (stub *ieltsAcousticSnapshotRepositoryStub) EnsureIELTSAcousticSnapshot(
	_ context.Context,
	_ IELTSAcousticSnapshotClaim,
	draft IELTSAcousticSnapshot,
) (IELTSAcousticSnapshot, bool, error) {
	stub.ensureCalls++
	stub.stored = draft
	return draft, false, nil
}

func (stub *ieltsAcousticSnapshotRepositoryStub) FailIELTSAcousticSnapshot(
	context.Context,
	IELTSAcousticSnapshotClaim,
) error {
	stub.failCalls++
	return nil
}

type ieltsAcousticSnapshotSourceStub struct {
	read        IELTSSpeakingAcousticRead
	completedBy time.Time
	calls       int
}

func (stub *ieltsAcousticSnapshotSourceStub) ReadIELTSSpeakingAcoustics(
	_ context.Context,
	_ string,
	_ []IELTSSpeakingAcousticRequest,
	completedBy time.Time,
) (IELTSSpeakingAcousticRead, error) {
	stub.calls++
	stub.completedBy = completedBy
	return stub.read, nil
}

type ieltsAcousticSnapshotQueueRepositoryStub struct {
	claims []IELTSAcousticSnapshotClaim
	failed int
	frozen int
}

func (stub *ieltsAcousticSnapshotQueueRepositoryStub) FindPendingIELTSAcousticSnapshot(
	_ context.Context,
	after *IELTSAcousticSnapshotCursor,
) (IELTSAcousticSnapshotClaim, bool, error) {
	for _, claim := range stub.claims {
		if after == nil || claim.RevisionCreatedAt.After(after.RevisionCreatedAt) ||
			(claim.RevisionCreatedAt.Equal(after.RevisionCreatedAt) &&
				claim.EvaluationRevisionID > after.EvaluationRevisionID) {
			return claim, true, nil
		}
	}
	if len(stub.claims) == 0 || after != nil {
		return IELTSAcousticSnapshotClaim{}, false, nil
	}
	return stub.claims[0], true, nil
}

func (stub *ieltsAcousticSnapshotQueueRepositoryStub) EnsureIELTSAcousticSnapshot(
	_ context.Context,
	claim IELTSAcousticSnapshotClaim,
	draft IELTSAcousticSnapshot,
) (IELTSAcousticSnapshot, bool, error) {
	index := stub.claimIndex(claim.EvaluationID)
	if index < 0 {
		return IELTSAcousticSnapshot{}, false, evaluation.ErrInvalidRequest
	}
	stub.claims = append(stub.claims[:index], stub.claims[index+1:]...)
	stub.frozen++
	return draft, false, nil
}

func (stub *ieltsAcousticSnapshotQueueRepositoryStub) FailIELTSAcousticSnapshot(
	_ context.Context,
	claim IELTSAcousticSnapshotClaim,
) error {
	index := stub.claimIndex(claim.EvaluationID)
	if index < 0 {
		return evaluation.ErrInvalidRequest
	}
	stub.claims = append(stub.claims[:index], stub.claims[index+1:]...)
	stub.failed++
	return nil
}

func (stub *ieltsAcousticSnapshotQueueRepositoryStub) claimIndex(
	evaluationID string,
) int {
	for index := range stub.claims {
		if stub.claims[index].EvaluationID == evaluationID {
			return index
		}
	}
	return -1
}

type ieltsAcousticSnapshotQueueSourceStub struct {
	errors []error
	reads  []IELTSSpeakingAcousticRead
	calls  int
}

func (stub *ieltsAcousticSnapshotQueueSourceStub) ReadIELTSSpeakingAcoustics(
	context.Context,
	string,
	[]IELTSSpeakingAcousticRequest,
	time.Time,
) (IELTSSpeakingAcousticRead, error) {
	if stub.calls >= len(stub.errors) && stub.calls >= len(stub.reads) {
		return IELTSSpeakingAcousticRead{}, evaluation.ErrInvalidRequest
	}
	var read IELTSSpeakingAcousticRead
	if stub.calls < len(stub.reads) {
		read = stub.reads[stub.calls]
	}
	var err error
	if stub.calls < len(stub.errors) {
		err = stub.errors[stub.calls]
	}
	stub.calls++
	return read, err
}
