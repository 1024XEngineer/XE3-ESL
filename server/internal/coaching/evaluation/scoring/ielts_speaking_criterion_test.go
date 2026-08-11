package scoring

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestIELTSSpeakingShadowCallsEachCriterionWithFullSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	provider := &ieltsCriterionProviderStub{}

	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	requests := provider.Requests()
	if len(requests) != 3 {
		t.Fatalf("provider requests = %d, want 3", len(requests))
	}
	seen := make(map[IELTSCriterion]struct{}, len(requests))
	for _, request := range requests {
		if request.Repair != nil || len(request.Input.Questions) != 15 ||
			len(request.Input.AssessableCriteria) != 1 {
			t.Fatalf("criterion request = %#v", request)
		}
		seen[request.Input.AssessableCriteria[0]] = struct{}{}
	}
	for _, criterion := range []IELTSCriterion{
		IELTSCriterionFC,
		IELTSCriterionLR,
		IELTSCriterionGRA,
	} {
		if _, ok := seen[criterion]; !ok {
			t.Fatalf("criterion %s was not requested", criterion)
		}
	}
	if result.Provider == nil || result.Provider.RequestID != "" ||
		len(result.Provider.CriterionRuns) != 3 {
		t.Fatalf("provider lineage = %#v", result.Provider)
	}
	for index, run := range result.Provider.CriterionRuns {
		if run.CriterionID != ieltsCriterionOrder[index] ||
			len(run.Attempts) != 1 ||
			run.Attempts[0].Kind != IELTSSpeakingProviderAttemptInitial ||
			run.Attempts[0].Outcome != IELTSSpeakingProviderAttemptAccepted {
			t.Fatalf("criterion run %d = %#v", index, run)
		}
	}
}

func TestIELTSSpeakingShadowRequiresPerCriterionLineage(t *testing.T) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	result, err := NewIELTSSpeakingShadowEngine(
		&ieltsCriterionProviderStub{},
	).Evaluate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	result.Provider.RequestID = "legacy-request"
	result.Provider.CriterionRuns = nil
	if err := ValidateIELTSSpeakingShadowResult(snapshot, result); err == nil {
		t.Fatal("result without per-criterion lineage was accepted")
	}
}

func TestIELTSSpeakingShadowRepairsOnlyRejectedCriterion(t *testing.T) {
	t.Parallel()
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	provider := &ieltsCriterionProviderStub{
		rejectInitial: IELTSCriterionLR,
	}

	result, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		snapshot,
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	requests := provider.Requests()
	if len(requests) != 4 {
		t.Fatalf("provider requests = %d, want 4", len(requests))
	}
	counts := make(map[IELTSCriterion]int)
	var repair *IELTSSpeakingCriterionRepair
	for _, request := range requests {
		criterion := request.Input.AssessableCriteria[0]
		counts[criterion]++
		if request.Repair != nil {
			if criterion != IELTSCriterionLR || repair != nil {
				t.Fatalf("unexpected repair request = %#v", request)
			}
			repair = request.Repair
		}
	}
	if counts[IELTSCriterionFC] != 1 || counts[IELTSCriterionLR] != 2 ||
		counts[IELTSCriterionGRA] != 1 || repair == nil ||
		repair.Stage != "semantic_validation" ||
		repair.Code != "no_primary_findings" {
		t.Fatalf("counts = %#v; repair = %#v", counts, repair)
	}
	lexicalRun := result.Provider.CriterionRuns[1]
	if lexicalRun.CriterionID != IELTSCriterionLR ||
		len(lexicalRun.Attempts) != 2 ||
		lexicalRun.Attempts[0].Outcome !=
			IELTSSpeakingProviderAttemptRejected ||
		lexicalRun.Attempts[0].RejectionCode != "no_primary_findings" ||
		lexicalRun.Attempts[1].Kind != IELTSSpeakingProviderAttemptRepair ||
		lexicalRun.Attempts[1].Outcome !=
			IELTSSpeakingProviderAttemptAccepted {
		t.Fatalf("lexical run = %#v", lexicalRun)
	}
}

func TestIELTSSpeakingShadowStartsInitialCriteriaConcurrently(t *testing.T) {
	t.Parallel()
	started := make(chan IELTSCriterion, 3)
	release := make(chan struct{})
	provider := &ieltsCriterionProviderStub{
		started: started,
		release: release,
	}
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	finished := make(chan error, 1)
	go func() {
		_, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
			context.Background(),
			snapshot,
		)
		finished <- err
	}()

	seen := make(map[IELTSCriterion]struct{}, 3)
	for range 3 {
		select {
		case criterion := <-started:
			seen[criterion] = struct{}{}
		case <-time.After(time.Second):
			close(release)
			t.Fatal("initial criterion calls did not start concurrently")
		}
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("started criteria = %#v", seen)
	}
}

func TestIELTSSpeakingShadowStopsAfterOneCriterionRepair(t *testing.T) {
	t.Parallel()
	provider := &ieltsCriterionProviderStub{
		rejectAlways: IELTSCriterionLR,
	}
	_, err := NewIELTSSpeakingShadowEngine(provider).Evaluate(
		context.Background(),
		ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount),
	)
	if err == nil {
		t.Fatal("Evaluate succeeded after the repair was rejected")
	}
	requests := provider.Requests()
	counts := make(map[IELTSCriterion]int)
	for _, request := range requests {
		counts[request.Input.AssessableCriteria[0]]++
	}
	if len(requests) != 4 || counts[IELTSCriterionFC] != 1 ||
		counts[IELTSCriterionLR] != 2 || counts[IELTSCriterionGRA] != 1 {
		t.Fatalf("criterion calls after exhausted repair = %#v", counts)
	}
}

type ieltsCriterionProviderStub struct {
	mu            sync.Mutex
	requests      []IELTSSpeakingCriterionProviderRequest
	rejectInitial IELTSCriterion
	rejectAlways  IELTSCriterion
	started       chan<- IELTSCriterion
	release       <-chan struct{}
}

func (provider *ieltsCriterionProviderStub) AnalyzeIELTSCriterion(
	_ context.Context,
	request IELTSSpeakingCriterionProviderRequest,
) (IELTSSpeakingShadowProviderResult, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	sequence := len(provider.requests)
	provider.mu.Unlock()
	criterion := request.Input.AssessableCriteria[0]
	if provider.started != nil {
		provider.started <- criterion
		<-provider.release
	}

	payload := validIELTSProviderPayload(request.Input)
	if criterion == provider.rejectAlways ||
		(criterion == provider.rejectInitial && request.Repair == nil) {
		payload.Criteria[0].Strengths = []ieltsProviderFinding{}
		payload.Criteria[0].Improvements = []ieltsProviderFinding{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return IELTSSpeakingShadowProviderResult{}, err
	}
	return IELTSSpeakingShadowProviderResult{
		Payload:   raw,
		Provider:  "provider",
		Model:     "model",
		RequestID: criterionRequestID(criterion, sequence),
	}, nil
}

func (provider *ieltsCriterionProviderStub) Requests() []IELTSSpeakingCriterionProviderRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return slices.Clone(provider.requests)
}

func criterionRequestID(criterion IELTSCriterion, sequence int) string {
	return "request-" + string(criterion) + "-" + string(rune('a'+sequence-1))
}
