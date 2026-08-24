package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestWorkerIELTSTwoRoundDeadlineExceedsBothProviderRounds(t *testing.T) {
	now := time.Now().UTC()
	claim := sessionClaimFixture(t, now, IELTSStrategyRef)
	store := &workerStoreFake{claims: []Claim{claim}}
	sessions := &sessionEvaluatorsFake{}
	worker := workerFixture(t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{})

	processed, err := worker.ProcessSession(context.Background())
	if err != nil {
		t.Fatalf("ProcessSession() error = %v", err)
	}
	if !processed || sessions.ieltsCalls != 1 || len(store.completions) != 1 {
		t.Fatalf(
			"processed=%v IELTS calls=%d completions=%d",
			processed,
			sessions.ieltsCalls,
			len(store.completions),
		)
	}
	if sessions.ieltsBudget < MinimumIELTSTwoRoundDeadline-time.Second {
		t.Fatalf("IELTS budget = %s", sessions.ieltsBudget)
	}
}

func TestWorkerConfigurationRejectsIELTSDeadlineThatCutsRepairRound(t *testing.T) {
	configuration := workerConfigurationFixture()
	configuration.IELTSDeadline = 90 * time.Second
	if configuration.Valid() {
		t.Fatal("configuration accepted a deadline with no repair margin")
	}
}

func TestWorkerConfigurationDerivesProfileWaitFromLaneLifecycle(t *testing.T) {
	configuration := workerConfigurationFixture()
	perStage := 2*configuration.ProfileDeadline + configuration.RetryDelay +
		configuration.DependencyDelay
	if got := configuration.profileLifecycleWaitBudget(previousProfileLifecycleStages); got != perStage {
		t.Fatalf("single-stage profile wait = %s, want %s", got, perStage)
	}
	if got := configuration.profileLifecycleWaitBudget(finalProfileLifecycleStages); got != 2*perStage {
		t.Fatalf("two-stage profile wait = %s, want %s", got, 2*perStage)
	}
}

func TestWorkerPart2ProfileReusesReadyPart1Profile(t *testing.T) {
	now := time.Now().UTC()
	claim := profileClaimFixture(t, now, IELTSProfileStagePart2)
	part1 := cumulativeProfileFixture(claim.SourceID, []int{1})
	part1Result, _, err := EncodeStrict(part1)
	if err != nil {
		t.Fatal(err)
	}
	store := &workerStoreFake{
		claims: []Claim{claim},
		records: map[Kind]Record{
			KindIELTSPart1Profile: {Status: JobReady, Result: part1Result},
		},
	}
	profiles := &profileEvaluatorsFake{}
	worker, err := NewWorker(
		store, &sessionEvaluatorsFake{}, profiles, &speechEvaluatorsFake{},
		&acousticEvaluatorFake{}, store, workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}

	processed, err := worker.ProcessProfile(context.Background())
	if err != nil || !processed || profiles.calls != 1 ||
		profiles.last.DependencyResolution != IELTSProfileDependencyResolved ||
		profiles.last.PreviousProfile == nil || len(store.checkpoints) != 1 ||
		len(store.completions) != 1 {
		t.Fatalf("ProcessProfile=(%t,%v), calls=%d snapshot=%#v checkpoints=%d completions=%d",
			processed, err, profiles.calls, profiles.last,
			len(store.checkpoints), len(store.completions))
	}
}

func TestWorkerPart2ProfileDefersWhilePart1IsRunning(t *testing.T) {
	now := time.Now().UTC()
	claim := profileClaimFixture(t, now, IELTSProfileStagePart2)
	claim.CreatedAt = now
	store := &workerStoreFake{
		claims: []Claim{claim},
		records: map[Kind]Record{
			KindIELTSPart1Profile: {Status: JobRunning, CreatedAt: now},
		},
	}
	profiles := &profileEvaluatorsFake{}
	worker, err := NewWorker(
		store, &sessionEvaluatorsFake{}, profiles, &speechEvaluatorsFake{},
		&acousticEvaluatorFake{}, store, workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}

	processed, err := worker.ProcessProfile(context.Background())
	if err != nil || !processed || profiles.calls != 0 ||
		len(store.deferrals) != 1 || len(store.checkpoints) != 0 ||
		len(store.completions) != 0 {
		t.Fatalf("ProcessProfile=(%t,%v), calls=%d deferrals=%d checkpoints=%d completions=%d",
			processed, err, profiles.calls, len(store.deferrals),
			len(store.checkpoints), len(store.completions))
	}
}

func TestWorkerPart2ProfileFallsBackWhenPart1IsMissing(t *testing.T) {
	now := time.Now().UTC()
	claim := profileClaimFixture(t, now, IELTSProfileStagePart2)
	store := &workerStoreFake{claims: []Claim{claim}}
	profiles := &profileEvaluatorsFake{}
	worker, err := NewWorker(
		store, &sessionEvaluatorsFake{}, profiles, &speechEvaluatorsFake{},
		&acousticEvaluatorFake{}, store, workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}

	processed, err := worker.ProcessProfile(context.Background())
	if err != nil || !processed || profiles.calls != 1 ||
		profiles.last.DependencyResolution != IELTSProfileDependencyFallback ||
		profiles.last.PreviousProfile != nil || len(store.checkpoints) != 1 ||
		len(store.completions) != 1 {
		t.Fatalf("ProcessProfile=(%t,%v), calls=%d snapshot=%#v checkpoints=%d completions=%d",
			processed, err, profiles.calls, profiles.last,
			len(store.checkpoints), len(store.completions))
	}
}

func TestWorkerFinalReportResolvesPart2ProfileReadyAfter45Seconds(t *testing.T) {
	now := time.Now().UTC()
	claim := sessionClaimFixture(t, now, IELTSStrategyRef)
	part2 := cumulativeProfileFixture(claim.SourceID, []int{1, 2})
	part2Result, _, err := EncodeStrict(part2)
	if err != nil {
		t.Fatal(err)
	}
	store := &workerStoreFake{
		claims: []Claim{claim},
		records: map[Kind]Record{
			KindIELTSPart2Profile: {
				Status: JobReady, Result: part2Result,
				CreatedAt: now.Add(-45 * time.Second),
			},
		},
	}
	sessions := &sessionEvaluatorsFake{}
	worker := workerFixture(
		t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{},
	)

	processed, err := worker.ProcessSession(context.Background())
	if err != nil || !processed || sessions.ieltsCalls != 1 ||
		sessions.lastIELTSSnapshot.ProfileResolution != IELTSFinalProfileResolved ||
		sessions.lastIELTSSnapshot.CumulativeProfile == nil ||
		len(store.checkpoints) != 1 || len(store.completions) != 1 {
		t.Fatalf("ProcessSession=(%t,%v), calls=%d snapshot=%#v checkpoints=%d completions=%d",
			processed, err, sessions.ieltsCalls, sessions.lastIELTSSnapshot,
			len(store.checkpoints), len(store.completions))
	}
}

func TestWorkerFinalReportDefersThroughNormalPart2ProfileRuntime(t *testing.T) {
	configuration := workerConfigurationFixture()
	for _, test := range []struct {
		name     string
		age      time.Duration
		attempts int
	}{
		{name: "ready-window-28s", age: 28 * time.Second, attempts: 1},
		{name: "ready-window-45s", age: 45 * time.Second, attempts: 1},
		{
			name:     "second-attempt-after-upstream-stage",
			age:      configuration.profileLifecycleWaitBudget(previousProfileLifecycleStages) + time.Second,
			attempts: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			claim := sessionClaimFixture(t, now, IELTSStrategyRef)
			store := &workerStoreFake{
				claims: []Claim{claim},
				records: map[Kind]Record{
					KindIELTSPart2Profile: {
						Status: JobRunning, AttemptCount: test.attempts,
						CreatedAt: now.Add(-test.age),
					},
				},
			}
			sessions := &sessionEvaluatorsFake{}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || sessions.ieltsCalls != 0 ||
				len(store.deferrals) != 1 || len(store.checkpoints) != 0 ||
				len(store.completions) != 0 || len(store.failures) != 0 {
				t.Fatalf(
					"ProcessSession=(%t,%v), calls=%d deferrals=%d checkpoints=%d completions=%d failures=%d",
					processed, err, sessions.ieltsCalls, len(store.deferrals),
					len(store.checkpoints), len(store.completions), len(store.failures),
				)
			}
		})
	}
}

func TestWorkerFinalReportFallsBackWhenPart2ProfileIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		record   Record
		timedOut bool
	}{
		{
			name:     "dependency timeout",
			record:   Record{Status: JobRunning, AttemptCount: 2},
			timedOut: true,
		},
		{
			name:   "failed dependency",
			record: Record{Status: JobFailed},
		},
		{
			name: "malformed ready dependency",
			record: Record{
				Status: JobReady,
				Result: json.RawMessage(`{"schema_version":"ielts-cumulative-profile/v1"}`),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			claim := sessionClaimFixture(t, now, IELTSStrategyRef)
			if test.timedOut {
				test.record.CreatedAt = now.Add(
					-workerConfigurationFixture().profileLifecycleWaitBudget(finalProfileLifecycleStages) - time.Second,
				)
			}
			store := &workerStoreFake{
				claims: []Claim{claim},
				records: map[Kind]Record{
					KindIELTSPart2Profile: test.record,
				},
			}
			sessions := &sessionEvaluatorsFake{}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || sessions.ieltsCalls != 1 ||
				sessions.lastIELTSSnapshot.ProfileResolution != IELTSFinalProfileFallback ||
				sessions.lastIELTSSnapshot.CumulativeProfile != nil ||
				len(store.deferrals) != 0 || len(store.checkpoints) != 1 ||
				len(store.completions) != 1 || len(store.failures) != 0 {
				t.Fatalf(
					"ProcessSession=(%t,%v), calls=%d snapshot=%#v deferrals=%d checkpoints=%d completions=%d failures=%d",
					processed, err, sessions.ieltsCalls, sessions.lastIELTSSnapshot,
					len(store.deferrals), len(store.checkpoints), len(store.completions),
					len(store.failures),
				)
			}
		})
	}
}

func TestWorkerReusesAcousticCheckpointAfterTextEvaluationRetry(t *testing.T) {
	now := time.Now().UTC()
	first := speechClaimFixture(t, now, KindPracticeTurnFeedback, nil)
	second := first
	second.AttemptCount = 2
	second.LeaseToken = "22222222-2222-4222-8222-222222222222"
	second.UpdatedAt = now.Add(time.Second)
	second.LeaseExpiresAt = timePointer(now.Add(13 * time.Minute))
	store := &workerStoreFake{claims: []Claim{first, second}}
	speech := &speechEvaluatorsFake{failFirst: true}
	acoustics := &acousticEvaluatorFake{}
	worker := workerFixture(t, store, &sessionEvaluatorsFake{}, speech, acoustics)

	processed, err := worker.ProcessSpeech(context.Background())
	if !processed || err == nil {
		t.Fatalf("first ProcessSpeech() = (%v, %v)", processed, err)
	}
	if acoustics.calls != 1 || len(store.checkpoints) != 1 || len(store.failures) != 1 {
		t.Fatalf(
			"acoustic calls=%d checkpoints=%d failures=%d",
			acoustics.calls,
			len(store.checkpoints),
			len(store.failures),
		)
	}
	store.claims[1].InputSnapshot = store.checkpoints[0].InputSnapshot
	store.claims[1].InputHash = store.checkpoints[0].InputHash

	processed, err = worker.ProcessSpeech(context.Background())
	if err != nil {
		t.Fatalf("retry ProcessSpeech() error = %v", err)
	}
	if !processed || acoustics.calls != 1 || speech.practiceCalls != 2 ||
		len(store.completions) != 1 {
		t.Fatalf(
			"processed=%v acoustic calls=%d text calls=%d completions=%d",
			processed,
			acoustics.calls,
			speech.practiceCalls,
			len(store.completions),
		)
	}
}

func TestWorkerRetriesTransientAcousticFailureWhileAttemptsRemain(t *testing.T) {
	now := time.Now().UTC()
	claim := speechClaimFixture(t, now, KindPracticeTurnFeedback, nil)
	store := &workerStoreFake{claims: []Claim{claim}}
	speech := &speechEvaluatorsFake{}
	acoustics := &acousticEvaluatorFake{err: errors.New("provider unavailable")}
	worker := workerFixture(t, store, &sessionEvaluatorsFake{}, speech, acoustics)

	processed, err := worker.ProcessSpeech(context.Background())
	if !processed || err == nil || acoustics.calls != 1 || speech.practiceCalls != 0 ||
		len(store.checkpoints) != 0 || len(store.completions) != 0 ||
		len(store.failures) != 1 || !store.failures[0].Error.Retryable {
		t.Fatalf(
			"ProcessSpeech=(%v,%v) acoustic=%d text=%d checkpoints=%d completions=%d failures=%#v",
			processed, err, acoustics.calls, speech.practiceCalls,
			len(store.checkpoints), len(store.completions), store.failures,
		)
	}
}

func TestWorkerFallsBackToTextAfterTerminalAcousticFailure(t *testing.T) {
	tests := []struct {
		name         string
		attemptCount int
		err          error
	}{
		{
			name:         "non-retryable failure",
			attemptCount: 1,
			err:          terminalAcousticFailure{},
		},
		{
			name:         "retryable failure on final attempt",
			attemptCount: workerConfigurationFixture().SpeechLane.MaxAttempts,
			err:          errors.New("provider unavailable"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			claim := speechClaimFixture(t, now, KindPracticeTurnFeedback, nil)
			claim.AttemptCount = test.attemptCount
			store := &workerStoreFake{claims: []Claim{claim}}
			speech := &speechEvaluatorsFake{}
			acoustics := &acousticEvaluatorFake{err: test.err}
			worker := workerFixture(
				t, store, &sessionEvaluatorsFake{}, speech, acoustics,
			)

			processed, err := worker.ProcessSpeech(context.Background())
			if err != nil || !processed || acoustics.calls != 1 ||
				speech.practiceCalls != 1 || len(store.checkpoints) != 1 ||
				len(store.completions) != 1 || len(store.failures) != 0 {
				t.Fatalf(
					"ProcessSpeech=(%v,%v) acoustic=%d text=%d checkpoints=%d completions=%d failures=%d",
					processed, err, acoustics.calls, speech.practiceCalls,
					len(store.checkpoints), len(store.completions), len(store.failures),
				)
			}
			var result SpeechResult
			if DecodeStrict(store.completions[0].Result, &result) != nil ||
				result.Acoustic.Status != AcousticNotAssessed ||
				result.Acoustic.Reason != "ACOUSTIC_ASSESSMENT_FAILED" {
				t.Fatalf("completion result = %#v", result)
			}
		})
	}
}

func TestWorkerIELTSReportReusesTurnAcousticsAfterTextRetry(t *testing.T) {
	for _, mode := range []string{"PART_1", "PART_2", "PART_3", "FULL_MOCK"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			first := ieltsSessionClaimWithAudio(t, now, mode, 1)
			second := first
			second.AttemptCount = 2
			second.LeaseToken = "44444444-4444-4444-8444-444444444444"
			second.UpdatedAt = now.Add(time.Second)
			second.LeaseExpiresAt = timePointer(now.Add(3 * time.Minute))
			pronunciation := 84.0
			store := &workerStoreFake{
				claims: []Claim{first, second},
				sessionAcoustics: SessionAcousticRead{Checkpoints: map[string]AcousticCheckpoint{
					"30000000-0000-4000-8000-000000000001": {
						Status:          AcousticAssessed,
						Pronunciation:   &pronunciation,
						Provider:        "xfyun-ise",
						ProviderSession: "ise-session-1",
					},
				}},
			}
			sessions := &sessionEvaluatorsFake{failFirstIELTS: true}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if !processed || err == nil || len(store.checkpoints) == 0 ||
				store.sessionAcousticCalls != 1 {
				t.Fatalf(
					"first ProcessSession = (%v, %v), checkpoints=%d reads=%d",
					processed, err, len(store.checkpoints), store.sessionAcousticCalls,
				)
			}
			latestCheckpoint := store.checkpoints[len(store.checkpoints)-1]
			store.claims[1].InputSnapshot = latestCheckpoint.InputSnapshot
			store.claims[1].InputHash = latestCheckpoint.InputHash

			processed, err = worker.ProcessSession(context.Background())
			if err != nil {
				t.Fatalf("retry ProcessSession() error = %v", err)
			}
			if !processed || store.sessionAcousticCalls != 1 ||
				sessions.ieltsCalls != 2 || len(store.completions) != 1 ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic == nil ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic.Status != AcousticAssessed {
				t.Fatalf(
					"processed=%v reads=%d IELTS calls=%d completions=%d snapshot=%#v",
					processed, store.sessionAcousticCalls, sessions.ieltsCalls,
					len(store.completions), sessions.lastIELTSSnapshot,
				)
			}
		})
	}
}

func TestWorkerUsesReadyIELTSAcousticsForEveryPracticeMode(t *testing.T) {
	for _, mode := range []string{"PART_1", "PART_2", "PART_3", "FULL_MOCK"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			claim := ieltsSessionClaimWithAudio(t, now, mode, 1)
			pronunciation := 84.0
			store := &workerStoreFake{
				claims: []Claim{claim},
				sessionAcoustics: SessionAcousticRead{Checkpoints: map[string]AcousticCheckpoint{
					"30000000-0000-4000-8000-000000000001": {
						Status:          AcousticAssessed,
						Pronunciation:   &pronunciation,
						Provider:        "xfyun-ise",
						ProviderSession: "ise-session-ready",
					},
				}},
			}
			sessions := &sessionEvaluatorsFake{}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{},
				&acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || sessions.ieltsCalls != 1 ||
				len(store.completions) != 1 || len(store.deferrals) != 0 ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic == nil ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic.Status != AcousticAssessed {
				t.Fatalf(
					"ProcessSession=(%v,%v) calls=%d deferrals=%d completions=%d snapshot=%#v",
					processed, err, sessions.ieltsCalls, len(store.deferrals),
					len(store.completions), sessions.lastIELTSSnapshot,
				)
			}
		})
	}
}

func TestWorkerDefersIELTSAcousticsForEveryPracticeMode(t *testing.T) {
	for _, mode := range []string{"PART_1", "PART_2", "PART_3", "FULL_MOCK"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			claim := ieltsSessionClaimWithAudio(t, now, mode, 1)
			claim.CreatedAt = now
			store := &workerStoreFake{
				claims:           []Claim{claim},
				sessionAcoustics: SessionAcousticRead{Pending: true},
			}
			worker := workerFixture(
				t, store, &sessionEvaluatorsFake{}, &speechEvaluatorsFake{},
				&acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || len(store.deferrals) != 1 ||
				len(store.failures) != 0 || len(store.completions) != 0 {
				t.Fatalf(
					"ProcessSession=(%v,%v) deferrals=%d failures=%d completions=%d",
					processed, err, len(store.deferrals), len(store.failures),
					len(store.completions),
				)
			}
		})
	}
}

func TestWorkerTimesOutIELTSAcousticsForEveryPracticeMode(t *testing.T) {
	for _, mode := range []string{"PART_1", "PART_2", "PART_3", "FULL_MOCK"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			claim := ieltsSessionClaimWithAudio(t, now, mode, 2)
			claim.CreatedAt = now.Add(
				-workerConfigurationFixture().AcousticDependencyMaxWait - time.Second,
			)
			pronunciation := 84.0
			store := &workerStoreFake{
				claims: []Claim{claim},
				sessionAcoustics: SessionAcousticRead{
					Pending: true,
					Checkpoints: map[string]AcousticCheckpoint{
						"30000000-0000-4000-8000-000000000001": {
							Status:          AcousticAssessed,
							Pronunciation:   &pronunciation,
							Provider:        "xfyun-ise",
							ProviderSession: "ise-session-complete",
						},
					},
				},
			}
			sessions := &sessionEvaluatorsFake{}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{},
				&acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || sessions.ieltsCalls != 1 ||
				len(store.deferrals) != 0 || len(store.completions) != 1 ||
				len(sessions.lastIELTSSnapshot.Turns) != 2 {
				t.Fatalf(
					"ProcessSession=(%v,%v) calls=%d deferrals=%d completions=%d snapshot=%#v",
					processed, err, sessions.ieltsCalls, len(store.deferrals),
					len(store.completions), sessions.lastIELTSSnapshot,
				)
			}
			first := sessions.lastIELTSSnapshot.Turns[0].Acoustic
			second := sessions.lastIELTSSnapshot.Turns[1].Acoustic
			if first == nil || first.Status != AcousticAssessed ||
				second == nil || second.Status != AcousticNotAssessed ||
				second.Reason != acousticDependencyTimeoutReason {
				t.Fatalf("timed out acoustics = %#v, %#v", first, second)
			}
		})
	}
}

func TestWorkerDefersProfileAcousticsForBothStages(t *testing.T) {
	for _, stage := range []IELTSProfileStage{
		IELTSProfileStagePart1,
		IELTSProfileStagePart2,
	} {
		t.Run(string(stage), func(t *testing.T) {
			now := time.Now().UTC()
			claim := profileClaimWithAudio(t, now, stage)
			claim.CreatedAt = now
			store := &workerStoreFake{
				claims:           []Claim{claim},
				sessionAcoustics: SessionAcousticRead{Pending: true},
			}
			profiles := &profileEvaluatorsFake{}
			worker, err := NewWorker(
				store,
				&sessionEvaluatorsFake{},
				profiles,
				&speechEvaluatorsFake{},
				&acousticEvaluatorFake{},
				store,
				workerConfigurationFixture(),
			)
			if err != nil {
				t.Fatal(err)
			}

			processed, err := worker.ProcessProfile(context.Background())
			if err != nil || !processed || profiles.calls != 0 ||
				store.sessionAcousticCalls != 1 || len(store.deferrals) != 1 ||
				len(store.failures) != 0 || len(store.completions) != 0 {
				t.Fatalf(
					"ProcessProfile=(%v,%v) calls=%d reads=%d deferrals=%d failures=%d completions=%d",
					processed, err, profiles.calls, store.sessionAcousticCalls,
					len(store.deferrals), len(store.failures), len(store.completions),
				)
			}
		})
	}
}

func TestWorkerTimesOutProfileAcousticsForBothStages(t *testing.T) {
	for _, stage := range []IELTSProfileStage{
		IELTSProfileStagePart1,
		IELTSProfileStagePart2,
	} {
		t.Run(string(stage), func(t *testing.T) {
			now := time.Now().UTC()
			claim := profileClaimWithAudio(t, now, stage)
			claim.CreatedAt = now.Add(
				-workerConfigurationFixture().AcousticDependencyMaxWait - time.Second,
			)
			checkpoints := map[string]AcousticCheckpoint{}
			if stage == IELTSProfileStagePart2 {
				pronunciation := 84.0
				checkpoints["30000000-0000-4000-8000-000000000001"] =
					AcousticCheckpoint{
						Status:          AcousticAssessed,
						Pronunciation:   &pronunciation,
						Provider:        "xfyun-ise",
						ProviderSession: "ise-session-complete",
					}
			}
			store := &workerStoreFake{
				claims: []Claim{claim},
				sessionAcoustics: SessionAcousticRead{
					Pending:     true,
					Checkpoints: checkpoints,
				},
			}
			profiles := &profileEvaluatorsFake{}
			worker, err := NewWorker(
				store,
				&sessionEvaluatorsFake{},
				profiles,
				&speechEvaluatorsFake{},
				&acousticEvaluatorFake{},
				store,
				workerConfigurationFixture(),
			)
			if err != nil {
				t.Fatal(err)
			}

			processed, err := worker.ProcessProfile(context.Background())
			if err != nil || !processed || profiles.calls != 1 ||
				store.sessionAcousticCalls != 1 || len(store.deferrals) != 0 ||
				len(store.completions) != 1 {
				t.Fatalf(
					"ProcessProfile=(%v,%v) calls=%d reads=%d deferrals=%d completions=%d snapshot=%#v",
					processed, err, profiles.calls, store.sessionAcousticCalls,
					len(store.deferrals), len(store.completions), profiles.last,
				)
			}
			last := profiles.last.Turns[len(profiles.last.Turns)-1].Acoustic
			if last == nil || last.Status != AcousticNotAssessed ||
				last.Reason != acousticDependencyTimeoutReason {
				t.Fatalf("timed out acoustic = %#v", last)
			}
			if stage == IELTSProfileStagePart2 {
				first := profiles.last.Turns[0].Acoustic
				if first == nil || first.Status != AcousticAssessed {
					t.Fatalf("completed acoustic was not preserved: %#v", first)
				}
			}
		})
	}
}

func TestWorkerCompletesIELTSReportAfterTerminalAcousticFailure(t *testing.T) {
	for _, mode := range []string{"PART_1", "PART_2", "PART_3", "FULL_MOCK"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			claim := ieltsSessionClaimWithAudio(t, now, mode, 1)
			store := &workerStoreFake{
				claims: []Claim{claim},
				sessionAcoustics: SessionAcousticRead{Checkpoints: map[string]AcousticCheckpoint{
					"30000000-0000-4000-8000-000000000001": {
						Status: AcousticNotAssessed,
						Reason: "ACOUSTIC_ASSESSMENT_FAILED",
					},
				}},
			}
			sessions := &sessionEvaluatorsFake{}
			worker := workerFixture(
				t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{},
			)

			processed, err := worker.ProcessSession(context.Background())
			if err != nil || !processed || sessions.ieltsCalls != 1 ||
				len(store.completions) != 1 || len(store.failures) != 0 ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic == nil ||
				sessions.lastIELTSSnapshot.Turns[0].Acoustic.Reason !=
					"ACOUSTIC_ASSESSMENT_FAILED" {
				t.Fatalf(
					"ProcessSession=(%v,%v) IELTS=%d completions=%d failures=%d snapshot=%#v",
					processed, err, sessions.ieltsCalls, len(store.completions),
					len(store.failures), sessions.lastIELTSSnapshot,
				)
			}
		})
	}
}

func TestAgentMessageSnapshotRequiresExplicitAcousticNotAssessed(t *testing.T) {
	snapshot := speechSnapshotFixture(nil)
	snapshot.AudioAssetID = ""
	snapshot.QuestionID = ""
	if snapshot.Valid(KindAgentMessageFeedback) {
		t.Fatal("Agent message accepted an implicit acoustic state")
	}
	snapshot.Acoustic = &AcousticCheckpoint{
		Status: AcousticNotAssessed,
		Reason: "AGENT_MESSAGE_HAS_NO_USER_AUDIO",
	}
	if !snapshot.Valid(KindAgentMessageFeedback) {
		t.Fatal("Agent message rejected explicit NOT_ASSESSED")
	}
}

func TestInvalidEvaluationInputIsNotRetried(t *testing.T) {
	failure := stableJobError(ErrInvalidRequest)
	if failure.Retryable || failure.Code != "INVALID_EVALUATION_INPUT" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestWorkerSendsFinalAttemptFailureToAtomicStateTransition(t *testing.T) {
	now := time.Now().UTC()
	claim := sessionClaimFixture(t, now, InterviewStrategyRef)
	claim.AttemptCount = 3
	store := &workerStoreFake{claims: []Claim{claim}}
	sessions := &sessionEvaluatorsFake{interviewError: errors.New("provider down")}
	worker := workerFixture(t, store, sessions, &speechEvaluatorsFake{}, &acousticEvaluatorFake{})

	processed, err := worker.ProcessSession(context.Background())
	if !processed || err == nil || len(store.failures) != 1 {
		t.Fatalf("ProcessSession() = (%v, %v), failures=%d", processed, err, len(store.failures))
	}
	if store.failures[0].MaxAttempts != 3 || store.failures[0].ID != claim.ID ||
		store.failures[0].LeaseToken != claim.LeaseToken {
		t.Fatalf("failure = %#v", store.failures[0])
	}
}

func TestWorkerProfileSecondFailureUsesProfileLaneTerminalAttempt(t *testing.T) {
	now := time.Now().UTC()
	claim := profileClaimFixture(t, now, IELTSProfileStagePart1)
	claim.AttemptCount = 2
	store := &workerStoreFake{claims: []Claim{claim}}
	profiles := &profileEvaluatorsFake{err: errors.New("provider down")}
	worker, err := NewWorker(
		store,
		&sessionEvaluatorsFake{},
		profiles,
		&speechEvaluatorsFake{},
		&acousticEvaluatorFake{},
		store,
		workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatal(err)
	}

	processed, err := worker.ProcessProfile(context.Background())
	if !processed || err == nil || profiles.calls != 1 || len(store.failures) != 1 {
		t.Fatalf(
			"ProcessProfile() = (%v, %v), calls=%d failures=%d",
			processed,
			err,
			profiles.calls,
			len(store.failures),
		)
	}
	if store.failures[0].MaxAttempts != 2 ||
		store.failures[0].ID != claim.ID ||
		store.failures[0].LeaseToken != claim.LeaseToken {
		t.Fatalf("failure = %#v", store.failures[0])
	}
}

type workerStoreFake struct {
	claims               []Claim
	claimIndex           int
	checkpoints          []SnapshotCheckpoint
	completions          []Completion
	failures             []Failure
	deferrals            []Deferral
	sessionAcoustics     SessionAcousticRead
	sessionAcousticCalls int
	records              map[Kind]Record
}

func (store *workerStoreFake) DeferClaim(_ context.Context, deferral Deferral) error {
	store.deferrals = append(store.deferrals, deferral)
	return nil
}

func (store *workerStoreFake) ReadSessionAcoustics(
	context.Context,
	string,
	string,
	[]string,
) (SessionAcousticRead, error) {
	store.sessionAcousticCalls++
	return store.sessionAcoustics, nil
}

func (store *workerStoreFake) Queue(context.Context, QueueCommand) (Record, bool, error) {
	return Record{}, false, errors.New("unexpected Queue")
}

func (store *workerStoreFake) GetRecordBySource(
	_ context.Context,
	_ string,
	kind Kind,
	_ string,
) (Record, error) {
	if record, exists := store.records[kind]; exists {
		return record, nil
	}
	if kind == KindIELTSPart1Profile || kind == KindIELTSPart2Profile {
		return Record{}, ErrNotFound
	}
	return Record{}, errors.New("unexpected GetRecordBySource")
}

func (store *workerStoreFake) ClaimNext(_ context.Context, _ ClaimLane) (Claim, error) {
	if store.claimIndex >= len(store.claims) {
		return Claim{}, ErrNotFound
	}
	claim := store.claims[store.claimIndex]
	store.claimIndex++
	return claim, nil
}

func (store *workerStoreFake) CheckpointSnapshot(
	_ context.Context,
	checkpoint SnapshotCheckpoint,
) (Record, error) {
	store.checkpoints = append(store.checkpoints, checkpoint)
	record := store.claims[store.claimIndex-1].Record
	record.InputSnapshot = checkpoint.InputSnapshot
	record.InputHash = checkpoint.InputHash
	return record, nil
}

func (store *workerStoreFake) CompleteClaim(_ context.Context, completion Completion) error {
	store.completions = append(store.completions, completion)
	return nil
}

func (store *workerStoreFake) FailClaim(_ context.Context, failure Failure) error {
	store.failures = append(store.failures, failure)
	return nil
}

func (store *workerStoreFake) ListFeedbackItems(context.Context, string, string) ([]FeedbackItem, error) {
	return nil, errors.New("unexpected ListFeedbackItems")
}

func (store *workerStoreFake) GetFeedbackItem(context.Context, string, string) (FeedbackItem, error) {
	return FeedbackItem{}, errors.New("unexpected GetFeedbackItem")
}

type sessionEvaluatorsFake struct {
	interviewError    error
	interviewCalls    int
	ieltsCalls        int
	ieltsBudget       time.Duration
	failFirstIELTS    bool
	lastIELTSSnapshot SessionInputSnapshot
	generalCalls      int
}

func (evaluator *sessionEvaluatorsFake) EvaluateInterview(
	context.Context,
	Record,
	SessionInputSnapshot,
	ConfigLineage,
) (json.RawMessage, error) {
	evaluator.interviewCalls++
	if evaluator.interviewError != nil {
		return nil, evaluator.interviewError
	}
	return json.RawMessage(`{"schema_version":"report/v1"}`), nil
}

func (evaluator *sessionEvaluatorsFake) EvaluateIELTS(
	ctx context.Context,
	_ Record,
	snapshot SessionInputSnapshot,
	_ ConfigLineage,
) (json.RawMessage, error) {
	evaluator.ieltsCalls++
	evaluator.lastIELTSSnapshot = snapshot
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("IELTS evaluator has no deadline")
	}
	evaluator.ieltsBudget = time.Until(deadline)
	if evaluator.failFirstIELTS && evaluator.ieltsCalls == 1 {
		return nil, errors.New("text provider unavailable")
	}
	return json.RawMessage(`{"schema_version":"report/v1"}`), nil
}

func (evaluator *sessionEvaluatorsFake) EvaluateGeneral(
	context.Context,
	Record,
	SessionInputSnapshot,
	ConfigLineage,
) (json.RawMessage, error) {
	evaluator.generalCalls++
	return json.RawMessage(`{"schema_version":"report/v1"}`), nil
}

type speechEvaluatorsFake struct {
	failFirst     bool
	practiceCalls int
	agentCalls    int
}

func (evaluator *speechEvaluatorsFake) EvaluatePracticeTurn(
	_ context.Context,
	snapshot SpeechInputSnapshot,
	_ ConfigLineage,
) (json.RawMessage, []FeedbackItemDraft, error) {
	evaluator.practiceCalls++
	if snapshot.Acoustic == nil {
		return nil, nil, errors.New("missing acoustic checkpoint")
	}
	if evaluator.failFirst && evaluator.practiceCalls == 1 {
		return nil, nil, errors.New("text provider unavailable")
	}
	return speechResultFixture(snapshot), []FeedbackItemDraft{feedbackDraftFixture(snapshot)}, nil
}

func (evaluator *speechEvaluatorsFake) EvaluateAgentMessage(
	_ context.Context,
	snapshot SpeechInputSnapshot,
	_ ConfigLineage,
) (json.RawMessage, []FeedbackItemDraft, error) {
	evaluator.agentCalls++
	return speechResultFixture(snapshot), []FeedbackItemDraft{feedbackDraftFixture(snapshot)}, nil
}

type acousticEvaluatorFake struct {
	calls int
	err   error
}

func (evaluator *acousticEvaluatorFake) EvaluateAcoustic(
	context.Context,
	Record,
	SpeechInputSnapshot,
) (AcousticCheckpoint, error) {
	evaluator.calls++
	if evaluator.err != nil {
		return AcousticCheckpoint{}, evaluator.err
	}
	pronunciation := 82.0
	return AcousticCheckpoint{
		Status:          AcousticAssessed,
		Pronunciation:   &pronunciation,
		Provider:        "iflytek",
		ProviderSession: "ise-session-1",
	}, nil
}

type terminalAcousticFailure struct{}

func (terminalAcousticFailure) Error() string          { return "acoustic unavailable" }
func (terminalAcousticFailure) StableCategory() string { return "ACOUSTIC_ASSESSMENT_FAILED" }
func (terminalAcousticFailure) Retryable() bool        { return false }

func workerFixture(
	t *testing.T,
	store Store,
	sessions SessionEvaluators,
	speech SpeechEvaluators,
	acoustics AcousticEvaluator,
) *Worker {
	t.Helper()
	worker, err := NewWorker(
		store,
		sessions,
		&profileEvaluatorsFake{},
		speech,
		acoustics,
		store.(*workerStoreFake),
		workerConfigurationFixture(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	return worker
}

func workerConfigurationFixture() WorkerConfiguration {
	return WorkerConfiguration{
		SessionLane: ClaimLane{
			Kinds:         []Kind{KindSessionReport},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		ProfileLane: ClaimLane{
			Kinds:         []Kind{KindIELTSPart1Profile, KindIELTSPart2Profile},
			LeaseDuration: 90 * time.Second,
			MaxAttempts:   2,
		},
		SpeechLane: ClaimLane{
			Kinds: []Kind{
				KindPracticeTurnFeedback,
				KindAgentMessageFeedback,
			},
			LeaseDuration: 13 * time.Minute,
			MaxAttempts:   3,
		},
		AcousticsEnabled:          true,
		InterviewDeadline:         45 * time.Second,
		IELTSDeadline:             110 * time.Second,
		GeneralDeadline:           45 * time.Second,
		SpeechDeadline:            11*time.Minute + 30*time.Second,
		ProfileDeadline:           45 * time.Second,
		RetryDelay:                time.Second,
		DependencyDelay:           5 * time.Second,
		AcousticDependencyMaxWait: 150 * time.Second,
		FinalizeTimeout:           5 * time.Second,
	}
}

type profileEvaluatorsFake struct {
	calls int
	last  IELTSProfileInputSnapshot
	err   error
}

func (evaluator *profileEvaluatorsFake) EvaluateProfile(
	_ context.Context,
	_ Record,
	snapshot IELTSProfileInputSnapshot,
	_ ConfigLineage,
) (json.RawMessage, error) {
	evaluator.calls++
	evaluator.last = snapshot
	if evaluator.err != nil {
		return nil, evaluator.err
	}
	return json.RawMessage(`{"schema_version":"ielts-cumulative-profile/v1"}`), nil
}

func profileClaimFixture(
	t *testing.T,
	now time.Time,
	stage IELTSProfileStage,
) Claim {
	t.Helper()
	snapshot := IELTSProfileInputSnapshot{
		SchemaVersion: IELTSProfileInputSchemaVersion,
		SessionID:     "20000000-0000-4000-8000-000000000001", SessionVersion: 3,
		Stage: stage, CompletedAt: now.Add(-time.Minute),
		Part1Boundary: 1, Part2Boundary: 2,
		AcousticCapability:   AcousticCapabilityEnabled,
		DependencyResolution: IELTSProfileDependencyPending,
		Questions: []SessionEvidenceQuestion{
			{ID: "40000000-0000-4000-8000-000000000001", Position: 1,
				Text: "Part 1", SpeakerParticipantID: "assistant",
				AddresseeParticipantIDs: []string{"learner"}},
		},
		Turns: []SessionEvidenceTurn{
			{ID: "30000000-0000-4000-8000-000000000001", Position: 1,
				QuestionID:              "40000000-0000-4000-8000-000000000001",
				RespondentParticipantID: "learner", Transcript: "Part 1 answer",
				Effective: true, ConfirmedAt: now.Add(-time.Minute)},
		},
	}
	if stage == IELTSProfileStagePart2 {
		snapshot.Questions = append(snapshot.Questions, SessionEvidenceQuestion{
			ID: "40000000-0000-4000-8000-000000000002", Position: 2,
			Text: "Part 2", SpeakerParticipantID: "assistant",
			AddresseeParticipantIDs: []string{"learner"},
		})
		snapshot.Turns = append(snapshot.Turns, SessionEvidenceTurn{
			ID: "30000000-0000-4000-8000-000000000002", Position: 2,
			QuestionID:              "40000000-0000-4000-8000-000000000002",
			RespondentParticipantID: "learner", Transcript: "Part 2 answer",
			Effective: true, ConfirmedAt: now.Add(-time.Minute),
		})
	}
	input, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lineage := ConfigLineage{
		SchemaVersion:   ConfigLineageSchemaVersion,
		StrategyRef:     "ielts-cumulative-profile/v1",
		PipelineVersion: "ielts-cumulative-profile/v1",
		PromptVersion:   "ielts-cumulative-profile/v1",
		ResultSchema:    IELTSCumulativeProfileSchemaVersion,
		Provider:        "qianwen", Model: "qwen-plus",
	}
	config, configHash, err := EncodeStrict(lineage)
	if err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(90 * time.Second)
	started := now
	kind := KindIELTSPart1Profile
	if stage == IELTSProfileStagePart2 {
		kind = KindIELTSPart2Profile
	}
	return Claim{Record: Record{
		ID:     "11111111-1111-4111-8111-111111111119",
		UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Kind:   kind, SourceID: snapshot.SessionID, ContextID: snapshot.SessionID,
		Status: JobRunning, InputSnapshot: input, InputHash: inputHash,
		ConfigLineage: config, ConfigHash: configHash, AttemptCount: 1,
		LeaseToken:     "11111111-1111-4111-8111-111111111120",
		LeaseExpiresAt: &leaseExpiry, AvailableAt: now.Add(-time.Minute),
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now, StartedAt: &started,
	}, LeaseDuration: 90 * time.Second}
}

func profileClaimWithAudio(
	t *testing.T,
	now time.Time,
	stage IELTSProfileStage,
) Claim {
	t.Helper()
	claim := profileClaimFixture(t, now, stage)
	var snapshot IELTSProfileInputSnapshot
	if err := DecodeStrict(claim.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	for index := range snapshot.Turns {
		snapshot.Turns[index].AudioAssetID = []string{"audio-1", "audio-2"}[index]
	}
	input, digest, err := EncodeStrict(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	claim.InputSnapshot = input
	claim.InputHash = digest
	return claim
}

func cumulativeProfileFixture(sessionID string, parts []int) IELTSCumulativeProfile {
	dimensions := make([]IELTSProfileDimension, 0, 4)
	for _, key := range []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	} {
		dimensions = append(dimensions, IELTSProfileDimension{
			Key: key, ProvisionalBandLow: 6, ProvisionalBandHigh: 7,
			Coverage: 0.5, Confidence: 0.5,
			Observations: []IELTSProfileObservation{},
		})
	}
	return IELTSCumulativeProfile{
		SchemaVersion: IELTSCumulativeProfileSchemaVersion,
		SessionID:     sessionID, CompletedParts: parts, Dimensions: dimensions,
		Provider: "qianwen", Model: "qwen-plus",
	}
}

func sessionClaimFixture(t *testing.T, now time.Time, strategy string) Claim {
	t.Helper()
	snapshot := SessionInputSnapshot{
		SchemaVersion:       SessionInputSchemaVersion,
		SessionID:           "20000000-0000-4000-8000-000000000001",
		SessionVersion:      1,
		EvaluationPolicyRef: IELTSSpeakingFullMockEvaluationPolicyRef,
		PracticeExperience:  "IELTS_SPEAKING",
		SceneCategory:       "IELTS_SPEAKING",
		PracticeMode:        "FULL_MOCK",
		CompletedAt:         now.Add(-time.Minute),
		AcousticCapability:  AcousticCapabilityEnabled,
		PlanSnapshot:        json.RawMessage(`{}`),
		Participants:        json.RawMessage(`[]`),
		Questions: []SessionEvidenceQuestion{{
			ID:                      "40000000-0000-4000-8000-000000000001",
			Position:                1,
			Text:                    "Tell me about yourself.",
			SpeakerParticipantID:    "assistant-1",
			AddresseeParticipantIDs: []string{"user-1"},
		}},
		Turns: []SessionEvidenceTurn{{
			ID:                      "30000000-0000-4000-8000-000000000001",
			Position:                1,
			QuestionID:              "40000000-0000-4000-8000-000000000001",
			RespondentParticipantID: "user-1",
			Transcript:              "I enjoy learning English.",
			Effective:               true,
			ConfirmedAt:             now.Add(-time.Minute),
		}},
	}
	if strategy == InterviewStrategyRef {
		snapshot.EvaluationPolicyRef = InterviewEvaluationPolicyRef
		snapshot.PracticeExperience = "INTERVIEW"
		snapshot.SceneCategory = "JOB_INTERVIEW"
		snapshot.PracticeMode = "FULL_SIMULATION"
	}
	inputJSON, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	configJSON, configHash, err := EncodeStrict(configLineageFixture(strategy))
	if err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(3 * time.Minute)
	started := now
	return Claim{
		Record: Record{
			ID:             "11111111-1111-4111-8111-111111111111",
			UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			Kind:           KindSessionReport,
			SourceID:       snapshot.SessionID,
			ContextID:      snapshot.SessionID,
			Status:         JobRunning,
			InputSnapshot:  inputJSON,
			InputHash:      inputHash,
			ConfigLineage:  configJSON,
			ConfigHash:     configHash,
			AttemptCount:   1,
			LeaseToken:     "11111111-1111-4111-8111-111111111112",
			LeaseExpiresAt: &leaseExpiry,
			AvailableAt:    now.Add(-time.Minute),
			CreatedAt:      now.Add(-time.Minute),
			UpdatedAt:      now,
			StartedAt:      &started,
		},
		LeaseDuration: 3 * time.Minute,
	}
}

func ieltsSessionClaimWithAudio(
	t *testing.T,
	now time.Time,
	practiceMode string,
	turnCount int,
) Claim {
	t.Helper()
	claim := sessionClaimFixture(t, now, IELTSStrategyRef)
	var snapshot SessionInputSnapshot
	if err := DecodeStrict(claim.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.PracticeMode = practiceMode
	if practiceMode == "FULL_MOCK" {
		snapshot.EvaluationPolicyRef = IELTSSpeakingFullMockEvaluationPolicyRef
	} else {
		snapshot.EvaluationPolicyRef = IELTSSpeakingPracticeEvaluationPolicyRef
	}
	snapshot.Turns[0].AudioAssetID = "audio-1"
	if turnCount == 2 {
		snapshot.Questions = append(snapshot.Questions, SessionEvidenceQuestion{
			ID:                      "40000000-0000-4000-8000-000000000002",
			Position:                2,
			Text:                    "Why is it useful?",
			SpeakerParticipantID:    "assistant-1",
			AddresseeParticipantIDs: []string{"user-1"},
		})
		snapshot.Turns = append(snapshot.Turns, SessionEvidenceTurn{
			ID:                      "30000000-0000-4000-8000-000000000002",
			Position:                2,
			QuestionID:              "40000000-0000-4000-8000-000000000002",
			RespondentParticipantID: "user-1",
			Transcript:              "It helps me communicate clearly.",
			Effective:               true,
			ConfirmedAt:             now.Add(-time.Minute),
			AudioAssetID:            "audio-2",
		})
	}
	if turnCount < 1 || turnCount > 2 {
		t.Fatalf("unsupported turn count %d", turnCount)
	}
	input, digest, err := EncodeStrict(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	claim.InputSnapshot = input
	claim.InputHash = digest
	return claim
}

func speechClaimFixture(
	t *testing.T,
	now time.Time,
	kind Kind,
	acoustic *AcousticCheckpoint,
) Claim {
	t.Helper()
	snapshot := speechSnapshotFixture(acoustic)
	inputJSON, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	configJSON, configHash, err := EncodeStrict(configLineageFixture("speech-feedback/v1"))
	if err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(13 * time.Minute)
	started := now
	return Claim{
		Record: Record{
			ID:             "33333333-3333-4333-8333-333333333333",
			UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			Kind:           kind,
			SourceID:       "30000000-0000-4000-8000-000000000001",
			ContextID:      "20000000-0000-4000-8000-000000000001",
			Status:         JobRunning,
			InputSnapshot:  inputJSON,
			InputHash:      inputHash,
			ConfigLineage:  configJSON,
			ConfigHash:     configHash,
			AttemptCount:   1,
			LeaseToken:     "33333333-3333-4333-8333-333333333334",
			LeaseExpiresAt: &leaseExpiry,
			AvailableAt:    now.Add(-time.Minute),
			CreatedAt:      now.Add(-time.Minute),
			UpdatedAt:      now,
			StartedAt:      &started,
		},
		LeaseDuration: 13 * time.Minute,
	}
}

func speechSnapshotFixture(acoustic *AcousticCheckpoint) SpeechInputSnapshot {
	return SpeechInputSnapshot{
		SchemaVersion: SpeechInputSchemaVersion,
		Transcript:    "I has a plan.",
		EvidenceRefID: "30000000-0000-4000-8000-000000000001",
		QuestionID:    "40000000-0000-4000-8000-000000000001",
		PromptText:    "Describe your plan.",
		AudioAssetID:  "audio-1",
		Acoustic:      acoustic,
	}
}

func configLineageFixture(strategy string) ConfigLineage {
	return ConfigLineage{
		SchemaVersion:   ConfigLineageSchemaVersion,
		StrategyRef:     strategy,
		PipelineVersion: "pipeline/v1",
		PromptVersion:   "prompt/v1",
		ResultSchema:    "result/v1",
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
}

func speechResultFixture(snapshot SpeechInputSnapshot) json.RawMessage {
	encoded, _ := json.Marshal(SpeechResult{
		SchemaVersion:      "speech-feedback/v1",
		ScoreabilityStatus: "PROVISIONAL",
		Summary:            "Feedback is ready.",
		ReasonCodes:        []string{},
		Acoustic:           *snapshot.Acoustic,
	})
	return encoded
}

func feedbackDraftFixture(snapshot SpeechInputSnapshot) FeedbackItemDraft {
	return FeedbackItemDraft{
		Category: "CORRECTION",
		Severity: "MEDIUM",
		Evidence: FeedbackEvidence{
			EvidenceRefID:   snapshot.EvidenceRefID,
			StartUTF8Byte:   0,
			EndUTF8Byte:     len(snapshot.Transcript),
			OriginalExcerpt: snapshot.Transcript,
		},
		Recommendation: "Use the correct verb form.",
		Correction:     "I have a plan.",
		RepracticeMode: "SAME_QUESTION",
	}
}

func timePointer(value time.Time) *time.Time { return &value }

var _ Store = (*workerStoreFake)(nil)
