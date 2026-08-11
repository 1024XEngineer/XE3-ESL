package scoring

import (
	"bytes"
	"context"
	"testing"
	"time"
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
		15*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return claim.RevisionCreatedAt.Add(14 * time.Second)
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
		15*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	freezer.now = func() time.Time {
		return claim.RevisionCreatedAt.Add(15 * time.Second)
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
}

func (stub *ieltsAcousticSnapshotRepositoryStub) FindPendingIELTSAcousticSnapshot(
	context.Context,
) (IELTSAcousticSnapshotClaim, bool, error) {
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

type ieltsAcousticSnapshotSourceStub struct {
	read  IELTSSpeakingAcousticRead
	calls int
}

func (stub *ieltsAcousticSnapshotSourceStub) ReadIELTSSpeakingAcoustics(
	context.Context,
	string,
	[]IELTSSpeakingAcousticRequest,
) (IELTSSpeakingAcousticRead, error) {
	stub.calls++
	return stub.read, nil
}
