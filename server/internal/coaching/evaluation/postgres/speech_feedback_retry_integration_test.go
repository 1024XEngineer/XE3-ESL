package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	speechRetryUserID    = "91000000-0000-4000-8000-000000000001"
	speechRetryTurnID    = "92000000-0000-4000-8000-000000000001"
	speechRetrySessionID = "93000000-0000-4000-8000-000000000001"
	speechRetryQuestion  = "94000000-0000-4000-8000-000000000001"
)

func TestSpeechFeedbackPart1InvalidThenValidReusesAcoustics(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, speechRetryUserID, "speech-retry-part1@example.com")
	generator := &speechRetryGenerator{contents: []string{
		`{"items":[{"kind":"CORRECTION","explanation":"动词形式需要修改。","source_text":"missing excerpt","source_occurrence":1,"suggested_text":"read"}]}`,
		`{"items":[{"kind":"RECOMMENDED_EXPRESSION","explanation":"这是更自然的表达。","source_text":"read","source_occurrence":1,"suggested_text":"do some reading"}]}`,
	}}
	acoustics := &speechRetryAcoustics{}
	store, worker := speechRetryWorker(t, pool, generator, acoustics)
	queueSpeechRetry(t, store, "I usually read before work.")

	processed, err := worker.ProcessSpeech(context.Background())
	if !processed || err == nil {
		t.Fatalf("first ProcessSpeech = (%t, %v)", processed, err)
	}
	queued, err := store.GetRecordBySource(
		context.Background(), speechRetryUserID,
		evaluation.KindPracticeTurnFeedback, speechRetryTurnID,
	)
	if err != nil || queued.Status != evaluation.JobQueued ||
		queued.AttemptCount != 1 || queued.Error != nil {
		t.Fatalf("queued retry = %#v, %v", queued, err)
	}
	var checkpointed evaluation.SpeechInputSnapshot
	if evaluation.DecodeStrict(queued.InputSnapshot, &checkpointed) != nil ||
		checkpointed.Acoustic == nil ||
		checkpointed.Acoustic.Status != evaluation.AcousticAssessed {
		t.Fatalf("checkpointed input = %#v", checkpointed)
	}

	processed, err = worker.ProcessSpeech(context.Background())
	if err != nil || !processed {
		t.Fatalf("second ProcessSpeech = (%t, %v)", processed, err)
	}
	ready, err := store.GetRecordBySource(
		context.Background(), speechRetryUserID,
		evaluation.KindPracticeTurnFeedback, speechRetryTurnID,
	)
	if err != nil || ready.Status != evaluation.JobReady ||
		ready.AttemptCount != 2 || ready.Error != nil ||
		acoustics.calls != 1 || generator.calls != 2 {
		t.Fatalf(
			"ready=%#v err=%v acoustic=%d text=%d",
			ready, err, acoustics.calls, generator.calls,
		)
	}
	items, err := store.ListFeedbackItems(
		context.Background(), speechRetryUserID, ready.ID,
	)
	if err != nil || len(items) != 1 ||
		items[0].Category != "RECOMMENDED_EXPRESSION" ||
		items[0].Evidence.OriginalExcerpt != "read" {
		t.Fatalf("feedback items = %#v, %v", items, err)
	}
}

func TestSpeechFeedbackPart3ThreeInvalidResponsesFailPubliclyNonRetryable(t *testing.T) {
	pool := evaluationTestDatabase(t)
	insertEvaluationTestUser(t, pool, speechRetryUserID, "speech-retry-part3@example.com")
	const invalid = `{"items":[{"kind":"STRENGTH","explanation":"无需修改。","source_text":"technology","source_occurrence":1,"suggested_text":null}]}`
	generator := &speechRetryGenerator{contents: []string{invalid, invalid, invalid}}
	acoustics := &speechRetryAcoustics{}
	store, worker := speechRetryWorker(t, pool, generator, acoustics)
	const transcript = "I think technology can improve education when teachers guide its use."
	queueSpeechRetry(t, store, transcript)

	for attempt := 1; attempt <= 3; attempt++ {
		processed, err := worker.ProcessSpeech(context.Background())
		if !processed || err == nil {
			t.Fatalf("ProcessSpeech attempt %d = (%t, %v)", attempt, processed, err)
		}
		record, getErr := store.GetRecordBySource(
			context.Background(), speechRetryUserID,
			evaluation.KindPracticeTurnFeedback, speechRetryTurnID,
		)
		if getErr != nil || record.AttemptCount != attempt {
			t.Fatalf("record attempt %d = %#v, %v", attempt, record, getErr)
		}
		if attempt < 3 && (record.Status != evaluation.JobQueued || record.Error != nil) {
			t.Fatalf("intermediate record %d = %#v", attempt, record)
		}
	}
	failed, err := store.GetRecordBySource(
		context.Background(), speechRetryUserID,
		evaluation.KindPracticeTurnFeedback, speechRetryTurnID,
	)
	if err != nil || failed.Status != evaluation.JobFailed ||
		failed.Error == nil || failed.Error.Code != "PROVIDER_RESPONSE_INVALID" ||
		failed.Error.Retryable || acoustics.calls != 1 || generator.calls != 3 {
		t.Fatalf(
			"failed=%#v err=%v acoustic=%d text=%d",
			failed, err, acoustics.calls, generator.calls,
		)
	}
	var storedError string
	if err := pool.QueryRow(context.Background(),
		`SELECT error::text FROM evaluations WHERE id=$1`, failed.ID,
	).Scan(&storedError); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedError, transcript) || strings.Contains(storedError, invalid) {
		t.Fatalf("terminal error leaked speech or provider output: %s", storedError)
	}
}

func speechRetryWorker(
	t *testing.T,
	pool *pgxpool.Pool,
	generator speechfeedback.TextGenerator,
	acoustics evaluation.AcousticEvaluator,
) (*Store, *evaluation.Worker) {
	t.Helper()
	store := mustStore(t, pool)
	speech, err := speechfeedback.NewCompactEvaluator(generator)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := evaluation.NewWorker(
		store,
		speechRetrySessions{},
		speechRetryProfiles{},
		speech,
		acoustics,
		speechRetrySessionAudio{},
		evaluation.WorkerConfiguration{
			SessionLane: evaluation.ClaimLane{
				Kinds:         []evaluation.Kind{evaluation.KindSessionReport},
				LeaseDuration: 3 * time.Minute, MaxAttempts: 3,
			},
			ProfileLane: evaluation.ClaimLane{
				Kinds: []evaluation.Kind{
					evaluation.KindIELTSPart1Profile,
					evaluation.KindIELTSPart2Profile,
				},
				LeaseDuration: time.Minute, MaxAttempts: 2,
			},
			SpeechLane: evaluation.ClaimLane{
				Kinds: []evaluation.Kind{
					evaluation.KindPracticeTurnFeedback,
					evaluation.KindAgentMessageFeedback,
				},
				LeaseDuration: time.Minute, MaxAttempts: 3,
			},
			AcousticsEnabled:          true,
			InterviewDeadline:         30 * time.Second,
			IELTSDeadline:             110 * time.Second,
			GeneralDeadline:           30 * time.Second,
			SpeechDeadline:            30 * time.Second,
			ProfileDeadline:           30 * time.Second,
			RetryDelay:                0,
			DependencyDelay:           time.Second,
			AcousticDependencyMaxWait: time.Minute,
			FinalizeTimeout:           5 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, worker
}

func queueSpeechRetry(t *testing.T, store *Store, transcript string) {
	t.Helper()
	input, inputHash, err := evaluation.EncodeStrict(evaluation.SpeechInputSnapshot{
		SchemaVersion: evaluation.SpeechInputSchemaVersion,
		Transcript:    transcript,
		EvidenceRefID: speechRetryTurnID,
		QuestionID:    speechRetryQuestion,
		AudioAssetID:  "speech-retry-audio",
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := speechfeedback.Lineage("qianwen", "qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	config, configHash, err := evaluation.EncodeStrict(lineage)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Queue(context.Background(), evaluation.QueueCommand{
		UserID: speechRetryUserID, Kind: evaluation.KindPracticeTurnFeedback,
		SourceID: speechRetryTurnID, ContextID: speechRetrySessionID,
		InputSnapshot: input, InputHash: inputHash,
		ConfigLineage: config, ConfigHash: configHash,
		AvailableAt: time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
}

type speechRetryGenerator struct {
	contents []string
	calls    int
}

func (generator *speechRetryGenerator) Generate(
	context.Context,
	speechfeedback.TextGenerationRequest,
) (speechfeedback.TextGenerationResult, error) {
	generator.calls++
	if generator.calls > len(generator.contents) {
		return speechfeedback.TextGenerationResult{}, errors.New("unexpected generation")
	}
	return speechfeedback.TextGenerationResult{
		RequestID: fmt.Sprintf("speech-retry-request-%d", generator.calls),
		Content:   generator.contents[generator.calls-1],
		Provider:  "qianwen",
		Model:     "qwen-plus",
	}, nil
}

type speechRetryAcoustics struct{ calls int }

func (acoustics *speechRetryAcoustics) EvaluateAcoustic(
	context.Context,
	evaluation.Record,
	evaluation.SpeechInputSnapshot,
) (evaluation.AcousticCheckpoint, error) {
	acoustics.calls++
	pronunciation := 86.0
	return evaluation.AcousticCheckpoint{
		Status:          evaluation.AcousticAssessed,
		Pronunciation:   &pronunciation,
		Provider:        "iflytek",
		ProviderSession: "speech-retry-ise-1",
	}, nil
}

type speechRetrySessions struct{}

func (speechRetrySessions) EvaluateInterview(
	context.Context, evaluation.Record, evaluation.SessionInputSnapshot,
	evaluation.ConfigLineage,
) (json.RawMessage, error) {
	return nil, errors.New("unexpected Interview evaluation")
}

func (speechRetrySessions) EvaluateIELTS(
	context.Context, evaluation.Record, evaluation.SessionInputSnapshot,
	evaluation.ConfigLineage,
) (json.RawMessage, error) {
	return nil, errors.New("unexpected IELTS evaluation")
}

func (speechRetrySessions) EvaluateGeneral(
	context.Context, evaluation.Record, evaluation.SessionInputSnapshot,
	evaluation.ConfigLineage,
) (json.RawMessage, error) {
	return nil, errors.New("unexpected General evaluation")
}

type speechRetryProfiles struct{}

func (speechRetryProfiles) EvaluateProfile(
	context.Context, evaluation.Record, evaluation.IELTSProfileInputSnapshot,
	evaluation.ConfigLineage,
) (json.RawMessage, error) {
	return nil, errors.New("unexpected IELTS profile evaluation")
}

type speechRetrySessionAudio struct{}

func (speechRetrySessionAudio) ReadSessionAcoustics(
	context.Context, string, string, []string,
) (evaluation.SessionAcousticRead, error) {
	return evaluation.SessionAcousticRead{}, errors.New("unexpected Session acoustic read")
}
