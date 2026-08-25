package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/sessionevaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/preparationsource"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/repository/postgres"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	customLifecycleUserID        = "71000000-0000-4000-8000-000000000001"
	customLifecycleActorSession  = "72000000-0000-4000-8000-000000000001"
	customLifecycleThreadID      = "73000000-0000-4000-8000-000000000001"
	customLifecyclePlanID        = "74000000-0000-4000-8000-000000000001"
	customLifecycleSessionID     = "75000000-0000-4000-8000-000000000001"
	customLifecycleFacilitatorID = "76000000-0000-4000-8000-000000000001"
	customLifecycleLearnerID     = "77000000-0000-4000-8000-000000000001"
	customLifecycleQuestionID    = "78000000-0000-4000-8000-000000000001"
	customLifecycleTurnID        = "79000000-0000-4000-8000-000000000001"
	customLifecycleCandidateID   = "7a000000-0000-4000-8000-000000000001"
)

func TestCustomPlanLifecycleReachesFormalReportAndRetry(t *testing.T) {
	pool := reviewTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO users (id,canonical_email) VALUES ($1,'custom-lifecycle@example.com')
	`, customLifecycleUserID); err != nil {
		t.Fatalf("seed Custom lifecycle owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO agent_threads (id,user_id) VALUES ($2,$1)
	`, customLifecycleUserID, customLifecycleThreadID); err != nil {
		t.Fatalf("seed Custom lifecycle Agent thread: %v", err)
	}
	actor := requestcontext.Actor{
		UserID: customLifecycleUserID, SessionID: customLifecycleActorSession,
	}

	planRepository := preparationpostgres.NewPostgresPlanRepository(pool)
	planService, err := preparationservice.NewPlanService(
		planRepository,
		&customLifecycleIDs{values: []string{customLifecyclePlanID}},
		customLifecycleUnusedDependencies{},
		customLifecycleThreadReader{},
		customLifecycleUnusedDependencies{},
		customLifecycleUnusedDependencies{},
		planpolicy.NewResolver(),
	)
	if err != nil {
		t.Fatalf("compose Custom Plan service: %v", err)
	}
	plan, replayed, err := planService.PreviewCustomPlan(
		ctx,
		actor,
		"custom-plan-create-0001",
		preparation.CreateCustomPlanRequest{
			SourceThreadID:    customLifecycleThreadID,
			BackgroundSummary: "The user wants to practice proposing a team learning budget.",
			SceneSpec: scene.CustomSceneSpec{
				Scenario:       "向团队负责人申请英语培训预算",
				UserRole:       "项目成员",
				AIRole:         "团队负责人",
				PracticeGoal:   "说明培训价值、预算依据并协商下一步",
				ExperienceHint: scene.PracticeExperienceWorkplace,
			},
		},
	)
	if err != nil || replayed || plan.Status != preparation.PlanStatusDraft ||
		plan.SceneSelection.Source.Type != scene.SceneSourceCustom ||
		plan.SceneSelection.Scene.Key != "custom:"+customLifecyclePlanID {
		t.Fatalf("PreviewCustomPlan = %#v, replayed=%t, err=%v", plan, replayed, err)
	}
	confirmed, replayed, err := planService.ConfirmPlan(
		ctx,
		actor,
		plan.ID,
		"custom-plan-confirm-0001",
		preparation.ConfirmPlanRequest{ExpectedVersion: plan.Version},
	)
	if err != nil || replayed || confirmed.Status != preparation.PlanStatusReady ||
		confirmed.Version != plan.Version+1 {
		t.Fatalf("ConfirmPlan = %#v, replayed=%t, err=%v", confirmed, replayed, err)
	}

	planSource, err := preparationsource.New(planService)
	if err != nil {
		t.Fatalf("compose Preparation Plan projection: %v", err)
	}
	evaluationStore, completionScheduler, evaluationWorker :=
		customLifecycleEvaluationRuntime(t, pool)
	practiceRepository, err := practicepostgres.New(
		pool,
		completionScheduler,
		noopTurnFeedbackScheduler{},
		noopIELTSProfileScheduler{},
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("compose Practice repository: %v", err)
	}
	sessionService, err := practice.NewSessionApplication(
		practiceRepository,
		&customLifecycleIDs{values: []string{
			customLifecycleSessionID,
			customLifecycleFacilitatorID,
			customLifecycleLearnerID,
		}},
		planSource,
	)
	if err != nil {
		t.Fatalf("compose Practice Session service: %v", err)
	}
	bootstrap, replayed, err := sessionService.CreateSession(
		ctx,
		actor,
		confirmed.ID,
		"custom-session-create-0001",
		practice.CreateSessionRequest{ExpectedPlanVersion: confirmed.Version},
	)
	if err != nil || replayed || bootstrap.Session.ID != customLifecycleSessionID ||
		bootstrap.Snapshot.SceneSelection.Scene.ID != "custom:"+customLifecyclePlanID ||
		bootstrap.Snapshot.SceneSelection.Scene.Prompt.UserRole != "项目成员" ||
		bootstrap.Snapshot.SceneSelection.Scene.Prompt.AIRole != "团队负责人" {
		t.Fatalf("CreateSession = %#v, replayed=%t, err=%v", bootstrap, replayed, err)
	}
	active, err := practiceRepository.ActivateSession(
		ctx,
		practice.Actor{UserID: actor.UserID, SessionID: actor.SessionID},
		bootstrap.Session.ID,
		"custom-session-start-0001",
		sha256.Sum256([]byte("custom-session-start-0001")),
	)
	if err != nil || active.Session.Status != practice.SessionInProgress {
		t.Fatalf("ActivateSession = %#v, err=%v", active, err)
	}

	versionAfterTurn := active.Session.Version + 1
	seedCustomLifecycleConfirmedTurn(t, pool, versionAfterTurn)
	completed, replayed, err := sessionService.TransitionSession(
		ctx,
		actor,
		customLifecycleSessionID,
		"custom-session-complete-0001",
		versionAfterTurn,
		practice.SessionComplete,
	)
	if err != nil || replayed || completed.Status != practice.SessionCompleted {
		t.Fatalf("complete Custom Session = %#v, replayed=%t, err=%v", completed, replayed, err)
	}

	record, err := evaluationStore.GetRecordBySource(
		ctx,
		customLifecycleUserID,
		evaluation.KindSessionReport,
		customLifecycleSessionID,
	)
	if err != nil || record.Status != evaluation.JobQueued {
		t.Fatalf("queued Custom Session Report = %#v, %v", record, err)
	}
	processed, err := evaluationWorker.ProcessSession(ctx)
	if err != nil || !processed {
		t.Fatalf("process Custom Session Report = %t, %v", processed, err)
	}
	record, err = evaluationStore.GetRecordBySource(
		ctx,
		customLifecycleUserID,
		evaluation.KindSessionReport,
		customLifecycleSessionID,
	)
	if err != nil || record.Status != evaluation.JobReady {
		t.Fatalf("ready Custom Session Report = %#v, %v", record, err)
	}
	formal, err := evaluationStore.GetFormalReport(
		ctx,
		customLifecycleUserID,
		record.ID,
	)
	if err != nil || !formal.Valid() ||
		formal.PracticeSessionID != customLifecycleSessionID ||
		formal.Report.SceneCategory != string(scene.SceneCategoryWorkplaceGeneral) {
		t.Fatalf("read Custom formal Report = %#v, %v", formal, err)
	}
	feedback := createCustomLifecycleFeedback(t, evaluationStore)
	retryService, err := New(pool, evaluationStore, practiceRepository)
	if err != nil {
		t.Fatalf("compose Review retry service: %v", err)
	}
	retryTurn, replayed, err := retryService.CreateTurn(
		ctx,
		customLifecycleUserID,
		feedback.ID,
		"custom-retry-create-0001",
	)
	if err != nil || replayed || retryTurn.Kind != practice.TurnKindRetry ||
		retryTurn.OriginalTurnID != customLifecycleTurnID ||
		retryTurn.SessionID != customLifecycleSessionID {
		t.Fatalf("CreateTurn = %#v, replayed=%t, err=%v", retryTurn, replayed, err)
	}
	replayedTurn, replayed, err := retryService.CreateTurn(
		ctx,
		customLifecycleUserID,
		feedback.ID,
		"custom-retry-create-0001",
	)
	if err != nil || !replayed || replayedTurn.ID != retryTurn.ID {
		t.Fatalf("replay retry Turn = %#v, replayed=%t, err=%v", replayedTurn, replayed, err)
	}
}

func customLifecycleEvaluationRuntime(
	t *testing.T,
	pool *pgxpool.Pool,
) (*evaluationpostgres.Store, *evaluationpostgres.SessionScheduler, *evaluation.Worker) {
	t.Helper()
	store, err := evaluationpostgres.NewStore(pool)
	if err != nil {
		t.Fatalf("compose Custom Evaluation store: %v", err)
	}
	lineages, err := sessionevaluation.Lineages("qianwen", "qwen-plus")
	if err != nil {
		t.Fatalf("compose Custom Evaluation lineages: %v", err)
	}
	builder, err := evaluation.NewSessionCommandBuilder(lineages, false)
	if err != nil {
		t.Fatalf("compose Custom Session Report builder: %v", err)
	}
	scheduler, err := evaluationpostgres.NewSessionScheduler(store, builder)
	if err != nil {
		t.Fatalf("compose Custom Session Report scheduler: %v", err)
	}
	sessionEvaluator, err := sessionevaluation.New(customLifecycleReportGenerator{})
	if err != nil {
		t.Fatalf("compose Custom Session evaluator: %v", err)
	}
	speechEvaluator, err := speechfeedback.NewCompactEvaluator(
		customLifecycleSpeechFeedbackGenerator{},
	)
	if err != nil {
		t.Fatalf("compose Custom speech evaluator: %v", err)
	}
	worker, err := evaluation.NewWorker(
		store,
		sessionEvaluator,
		customLifecycleProfileEvaluator{},
		speechEvaluator,
		nil,
		nil,
		customLifecycleWorkerConfiguration(),
	)
	if err != nil {
		t.Fatalf("compose Custom Evaluation worker: %v", err)
	}
	return store, scheduler, worker
}

type customLifecycleProfileEvaluator struct{}

func (customLifecycleProfileEvaluator) EvaluateProfile(
	context.Context,
	evaluation.Record,
	evaluation.IELTSProfileInputSnapshot,
	evaluation.ConfigLineage,
) (json.RawMessage, error) {
	return nil, errors.New("unexpected IELTS profile evaluation")
}

type customLifecycleReportGenerator struct{}

func (customLifecycleReportGenerator) Generate(
	_ context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	var input struct {
		DimensionKeys []string `json:"dimension_keys"`
	}
	if err := json.Unmarshal([]byte(request.UserPrompt), &input); err != nil {
		return textgeneration.Result{}, err
	}
	dimensions := make([]map[string]any, len(input.DimensionKeys))
	for index, key := range input.DimensionKeys {
		dimensions[index] = map[string]any{
			"key":                  key,
			"score":                78.0,
			"coverage":             1.0,
			"confidence":           0.8,
			"reason_codes":         []string{},
			"strengths":            []any{},
			"improvements":         []any{},
			"recommended_examples": []any{},
		}
	}
	content, err := json.Marshal(map[string]any{
		"scoreability_status": "PROVISIONAL",
		"summary":             "The proposal connects the training budget to project outcomes.",
		"dimensions":          dimensions,
		"priority_actions":    []any{},
	})
	if err != nil {
		return textgeneration.Result{}, err
	}
	return textgeneration.Result{
		RequestID: "custom-lifecycle-report-request-1",
		Content:   string(content),
		Provider:  "qianwen",
		Model:     "qwen-plus",
	}, nil
}

type customLifecycleSpeechFeedbackGenerator struct{}

func (customLifecycleSpeechFeedbackGenerator) Generate(
	context.Context,
	speechfeedback.TextGenerationRequest,
) (speechfeedback.TextGenerationResult, error) {
	return speechfeedback.TextGenerationResult{
		RequestID: "custom-lifecycle-speech-request-1",
		Content: `{"items":[{"kind":"STRENGTH","explanation":"The answer is clear.",` +
			`"suggested_text":null}]}`,
		Provider: "qianwen",
		Model:    "qwen-plus",
	}, nil
}

func customLifecycleWorkerConfiguration() evaluation.WorkerConfiguration {
	return evaluation.WorkerConfiguration{
		SessionLane: evaluation.ClaimLane{
			Kinds:         []evaluation.Kind{evaluation.KindSessionReport},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		ProfileLane: evaluation.ClaimLane{
			Kinds: []evaluation.Kind{
				evaluation.KindIELTSPart1Profile,
				evaluation.KindIELTSPart2Profile,
			},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		SpeechLane: evaluation.ClaimLane{
			Kinds: []evaluation.Kind{
				evaluation.KindPracticeTurnFeedback,
				evaluation.KindAgentMessageFeedback,
			},
			LeaseDuration: 3 * time.Minute,
			MaxAttempts:   3,
		},
		InterviewDeadline:         30 * time.Second,
		IELTSDeadline:             110 * time.Second,
		GeneralDeadline:           30 * time.Second,
		SpeechDeadline:            30 * time.Second,
		ProfileDeadline:           30 * time.Second,
		RetryDelay:                time.Second,
		DependencyDelay:           time.Second,
		AcousticDependencyMaxWait: 150 * time.Second,
		FinalizeTimeout:           5 * time.Second,
	}
}

func seedCustomLifecycleConfirmedTurn(
	t *testing.T,
	pool *pgxpool.Pool,
	sessionVersion int,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO practice_questions (
    question_id,session_id,objective_id,question_type,content,
    speaker_participant_id,addressee_participant_ids,sequence
) VALUES (
    $1,$2,'custom_practice_goal','PRIMARY',
    'Why should the team fund this training?', $3, ARRAY[$4]::text[], 1
)
`, customLifecycleQuestionID, customLifecycleSessionID,
		customLifecycleFacilitatorID, customLifecycleLearnerID); err != nil {
		t.Fatalf("seed Custom practice question: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id,session_id,question_id,respondent_participant_id,sequence,
    turn_kind,status,counts_toward_turn_limit,candidate_id,transcript_id,
    evidence_version,transcript,interaction_mode,effective_turns_after,
    session_version_after,progressed_at,confirmed_at
) VALUES (
    $4,$2,$1,$3,1,'EFFECTIVE','confirmed',true,$5,
    'custom-lifecycle-transcript',1,
    'I proposed a focused course and connected the cost to project outcomes.',
    'text',1,$6,transaction_timestamp(),transaction_timestamp()
)
`, customLifecycleQuestionID, customLifecycleSessionID,
		customLifecycleLearnerID, customLifecycleTurnID,
		customLifecycleCandidateID, sessionVersion); err != nil {
		t.Fatalf("seed confirmed Custom practice turn: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
UPDATE practice_sessions
SET effective_turns=1, version=$2, updated_at=transaction_timestamp()
WHERE session_id=$1
	`, customLifecycleSessionID, sessionVersion); err != nil {
		t.Fatalf("advance Custom Session after confirmed turn: %v", err)
	}
}

func createCustomLifecycleFeedback(
	t *testing.T,
	store *evaluationpostgres.Store,
) evaluation.FeedbackItem {
	t.Helper()
	items := seedRetryFeedbackFor(
		t,
		store,
		retryFeedbackFixture{
			UserID:     customLifecycleUserID,
			SessionID:  customLifecycleSessionID,
			QuestionID: customLifecycleQuestionID,
			TurnID:     customLifecycleTurnID,
			Transcript: "I proposed a focused course and connected the cost to project outcomes.",
		},
		[]evaluation.FeedbackItemDraft{{
			Category: "CORRECTION",
			Severity: "MEDIUM",
			Evidence: evaluation.FeedbackEvidence{
				EvidenceRefID:   customLifecycleTurnID,
				StartUTF8Byte:   0,
				EndUTF8Byte:     1,
				OriginalExcerpt: "I",
			},
			Recommendation: "State one measurable project outcome.",
			Correction:     "This course should reduce avoidable rework on the next release.",
			RepracticeMode: "SAME_QUESTION",
		}},
	)
	return items[0]
}

type customLifecycleIDs struct {
	values []string
	next   int
}

func (ids *customLifecycleIDs) NewID() (string, error) {
	if ids == nil || ids.next >= len(ids.values) {
		return "", errors.New("custom lifecycle ID fixture exhausted")
	}
	value := ids.values[ids.next]
	ids.next++
	return value, nil
}

type customLifecycleThreadReader struct{}

func (customLifecycleThreadReader) ReadOwnedThread(
	_ context.Context,
	_ requestcontext.Actor,
	id string,
) (preparation.SourceThread, error) {
	return preparation.SourceThread{ID: id}, nil
}

type customLifecycleUnusedDependencies struct{}

func (customLifecycleUnusedDependencies) ReadConfirmed(
	context.Context,
	requestcontext.Actor,
	string,
	int,
) (preparation.InterviewPreparationSnapshot, error) {
	return preparation.InterviewPreparationSnapshot{}, errors.New("unused")
}

func (customLifecycleUnusedDependencies) ResolveAccessibleSelection(
	context.Context,
	string,
	string,
	int,
	[]string,
	string,
) (scene.SelectionSnapshot, error) {
	return scene.SelectionSnapshot{}, errors.New("unused")
}

func (customLifecycleUnusedDependencies) ResolveQuestionSet(
	context.Context,
	ielts.QuestionSetSelection,
) (ielts.ResolvedQuestionSet, error) {
	return ielts.ResolvedQuestionSet{}, errors.New("unused")
}

func (customLifecycleUnusedDependencies) AssignQuestionSet(
	context.Context,
	ielts.PracticeMode,
	string,
) (ielts.ResolvedQuestionSet, error) {
	return ielts.ResolvedQuestionSet{}, errors.New("unused")
}
