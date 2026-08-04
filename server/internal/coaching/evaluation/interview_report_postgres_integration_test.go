package evaluation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresInterviewReportLookupIsOwnerScopedAndFailClosed(
	t *testing.T,
) {
	pool, repository, _, value := prepareInterviewShadowRuntime(t)

	state, err := repository.GetCurrentInterviewReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	)
	if err != nil {
		t.Fatalf("GetCurrentInterviewReportState: %v", err)
	}
	if state.Evaluation.ID != value.ID ||
		state.Evaluation.Revision.ID != value.Revision.ID {
		t.Fatalf("report state = %#v, want Evaluation %#v", state, value)
	}
	if _, err := repository.GetCurrentInterviewReportState(
		context.Background(),
		testOwnerB,
		value.PracticeSessionID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner report error = %v", err)
	}
	if _, err := repository.GetCurrentInterviewReportState(
		context.Background(),
		testOwnerA,
		"session-without-evaluation",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing report error = %v", err)
	}

	insertDuplicateInterviewReportEvaluation(
		t,
		pool,
		value,
	)
	if _, err := repository.GetCurrentInterviewReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	); !errors.Is(err, ErrInterviewShadowConfigurationConflict) {
		t.Fatalf("ambiguous report error = %v", err)
	}
}

func TestPostgresLatestInterviewReportLookupUsesTrustedOwner(t *testing.T) {
	_, repository, configuration, value := prepareInterviewShadowRuntime(t)
	claim := claimInterviewShadow(t, repository, configuration)
	result := evaluateInterviewShadowClaim(t, claim)
	if err := repository.CompleteInterviewShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("CompleteInterviewShadow: %v", err)
	}
	state, err := repository.GetLatestInterviewReportState(
		context.Background(),
		testOwnerA,
	)
	if err != nil {
		t.Fatalf("GetLatestInterviewReportState: %v", err)
	}
	if state.Evaluation.ID != value.ID ||
		state.Evaluation.PracticeSessionID != value.PracticeSessionID ||
		state.Runtime.Result == nil || state.Snapshot == nil {
		t.Fatalf("latest report state = %#v", state)
	}
	if _, err := repository.GetLatestInterviewReportState(
		context.Background(),
		testOwnerB,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner latest report error = %v", err)
	}
}

func TestPostgresInterviewReportReadsEveryPublishedState(t *testing.T) {
	t.Run("queued", func(t *testing.T) {
		_, repository, _, value := prepareInterviewShadowRuntime(t)
		state, err := repository.GetCurrentInterviewReportState(
			context.Background(),
			testOwnerA,
			value.PracticeSessionID,
		)
		if err != nil {
			t.Fatalf("queued report: %v", err)
		}
		if state.Runtime.ModuleStatus !=
			InterviewShadowRuntimePending ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			t.Fatalf("queued state = %#v", state)
		}
	})
	t.Run("running", func(t *testing.T) {
		_, repository, configuration, value :=
			prepareInterviewShadowRuntime(t)
		claimInterviewShadow(t, repository, configuration)
		state, err := repository.GetCurrentInterviewReportState(
			context.Background(),
			testOwnerA,
			value.PracticeSessionID,
		)
		if err != nil {
			t.Fatalf("running report: %v", err)
		}
		if state.Runtime.ModuleStatus !=
			InterviewShadowRuntimeRunning ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			t.Fatalf("running state = %#v", state)
		}
	})
	t.Run("ready", func(t *testing.T) {
		_, repository, configuration, value :=
			prepareInterviewShadowRuntime(t)
		claim := claimInterviewShadow(t, repository, configuration)
		result := evaluateInterviewShadowClaim(t, claim)
		if err := repository.CompleteInterviewShadow(
			context.Background(),
			claim,
			result,
		); err != nil {
			t.Fatalf("CompleteInterviewShadow: %v", err)
		}
		state, err := repository.GetCurrentInterviewReportState(
			context.Background(),
			testOwnerA,
			value.PracticeSessionID,
		)
		if err != nil {
			t.Fatalf("ready report: %v", err)
		}
		if state.Runtime.ModuleStatus !=
			InterviewShadowRuntimeReady ||
			state.Runtime.Result == nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot == nil {
			t.Fatalf("ready state = %#v", state)
		}
		report, err := ProjectInterviewReport(
			*state.Snapshot,
			*state.Runtime.Result,
		)
		if err != nil || !report.Valid() ||
			len(report.Dimensions) != 5 ||
			len(report.Questions) == 0 {
			t.Fatalf("persisted report = %#v, error = %v", report, err)
		}
	})
	t.Run("failed", func(t *testing.T) {
		_, repository, configuration, value :=
			prepareInterviewShadowRuntime(t)
		configuration.MaxAttempts = 1
		claim := claimInterviewShadow(t, repository, configuration)
		status, err := repository.FailInterviewShadow(
			context.Background(),
			claim,
			InterviewShadowFailure{
				Code:      "provider_timeout",
				Retryable: true,
			},
			configuration,
		)
		if err != nil || status != InterviewShadowRuntimeFailed {
			t.Fatalf("FailInterviewShadow = %q, %v", status, err)
		}
		state, err := repository.GetCurrentInterviewReportState(
			context.Background(),
			testOwnerA,
			value.PracticeSessionID,
		)
		if err != nil {
			t.Fatalf("failed report: %v", err)
		}
		if state.Runtime.ModuleStatus !=
			InterviewShadowRuntimeFailed ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure == nil ||
			state.Runtime.Failure.Code != "provider_timeout" ||
			state.Runtime.Failure.Retryable ||
			state.Snapshot != nil {
			t.Fatalf("failed state = %#v", state)
		}
	})
}

func TestPostgresInterviewReportDoesNotSelectHistoricalShadowRevision(
	t *testing.T,
) {
	_, repository, _, value := prepareInterviewShadowRuntime(t)
	service := NewService(repository, repository)
	_, replayed, err := service.Reevaluate(
		testActorContext(testOwnerA),
		testActor(testOwnerA),
		value.ID,
		ReevaluateRequest{
			Channels:         []Channel{ChannelScene},
			SceneStrategyRef: "another-interview-strategy/v1",
			PipelineVersion:  InterviewShadowPipelineVersion,
			ClientRequestID:  "historical-shadow-revision",
		},
	)
	if err != nil || replayed {
		t.Fatalf("Reevaluate replayed=%t error=%v", replayed, err)
	}
	if _, err := repository.GetCurrentInterviewReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("historical Shadow report error = %v", err)
	}
}

func insertDuplicateInterviewReportEvaluation(
	t *testing.T,
	pool *pgxpool.Pool,
	source Evaluation,
) {
	t.Helper()
	ctx := context.Background()
	var evaluationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO evaluation_ledgers (
			owner_user_id,
			root_idempotency_key,
			root_request_fingerprint,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scope,
			scene_type
		)
		VALUES (
			$1,
			decode(repeat('11', 32), 'hex'),
			decode(repeat('22', 32), 'hex'),
			$2,
			$3,
			$4,
			'SESSION',
			'INTERVIEW'
		)
		RETURNING id::text
	`, source.OwnerUserID, source.PracticeSessionID,
		source.InputSnapshotID, source.InputRevision).Scan(
		&evaluationID,
	); err != nil {
		t.Fatalf("insert duplicate report ledger: %v", err)
	}
	var revisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO evaluation_revisions (
			evaluation_id,
			owner_user_id,
			revision,
			channels,
			scene_strategy_ref,
			pipeline_version,
			schema_version,
			request_fingerprint
		)
		VALUES (
			$1,
			$2,
			1,
			ARRAY['SCENE']::text[],
			$3,
			$4,
			$5,
			decode(repeat('33', 32), 'hex')
		)
		RETURNING id::text
	`, evaluationID, source.OwnerUserID,
		InterviewShadowStrategyRef,
		InterviewShadowPipelineVersion,
		SchemaVersion).Scan(&revisionID); err != nil {
		t.Fatalf("insert duplicate report revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO evaluation_revision_states (
			revision_id,
			evaluation_id,
			owner_user_id
		)
		VALUES ($1, $2, $3)
		RETURNING revision_id::text
	`, revisionID, evaluationID, source.OwnerUserID).Scan(
		new(string),
	); err != nil {
		t.Fatalf("insert duplicate report state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO evaluation_outbox (
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			channel,
			channel_key,
			event_type,
			payload
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3::uuid,
			'SCENE',
			decode(repeat('44', 32), 'hex'),
			'evaluation.revision.queued',
			jsonb_build_object(
				'evaluation_id', ($1::uuid)::text,
				'evaluation_revision_id', ($2::uuid)::text,
				'revision', 1,
				'channel', 'SCENE'
			)
		)
		RETURNING id::text
	`, evaluationID, revisionID, source.OwnerUserID).Scan(
		new(string),
	); err != nil {
		t.Fatalf("insert duplicate report outbox: %v", err)
	}
}
