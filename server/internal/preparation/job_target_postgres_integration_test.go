package preparation_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

func TestPostgresJobTargetLifecycleRecoveryAndFencing(t *testing.T) {
	_, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA, preparationUserB)
	repository := preparation.NewPostgresJobTargetRepository(pool)
	ctx := context.Background()
	actorA := preparationActor(preparationUserA, preparationSessionA)
	actorB := preparationActor(preparationUserB, preparationSessionB)

	createRequest := preparation.CreateJobTargetRequest{
		Source:         preparation.JobTargetSourceJobDescription,
		JobTitle:       "Platform engineer",
		JobDescription: "Own reliable APIs and explain system trade-offs.",
		Company:        "Example",
		Seniority:      "Senior",
	}
	createCommand := preparation.CreateJobTargetCommand{
		TargetID: "target-original",
		Request:  createRequest,
		Intent: jobTargetIntent(
			"POST",
			"/v1/job-targets",
			"target-create-key",
			createRequest,
		),
	}
	target, replayed, err := repository.Create(
		ctx,
		actorA,
		createCommand,
	)
	if err != nil || replayed {
		t.Fatalf("Create replayed=%t error=%v", replayed, err)
	}
	if target.Stage != preparation.JobTargetStageDraft ||
		target.InputVersion != 1 ||
		target.Analysis != nil ||
		target.Confirmation != nil {
		t.Fatalf("created target = %#v", target)
	}

	replayCommand := createCommand
	replayCommand.TargetID = "target-replay-id-is-ignored"
	replayedTarget, replayed, err := repository.Create(
		ctx,
		actorA,
		replayCommand,
	)
	if err != nil || !replayed ||
		!reflect.DeepEqual(replayedTarget, target) {
		t.Fatalf(
			"Create replay target=%#v replayed=%t error=%v",
			replayedTarget,
			replayed,
			err,
		)
	}
	changedCreate := createCommand
	changedCreate.Request.JobTitle = "Changed"
	changedCreate.Intent = jobTargetIntent(
		"POST",
		"/v1/job-targets",
		createCommand.Intent.Key,
		changedCreate.Request,
	)
	if _, _, err := repository.Create(
		ctx,
		actorA,
		changedCreate,
	); !errors.Is(err, preparation.ErrJobTargetIdempotencyConflict) {
		t.Fatalf("changed create replay error = %v", err)
	}
	if _, err := repository.Get(
		ctx,
		actorB,
		target.ID,
	); !errors.Is(err, preparation.ErrJobTargetNotFound) {
		t.Fatalf("cross-user Get error = %v", err)
	}

	analysisRequest := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 1,
	}
	analysisCommand := preparation.AnalyzeJobTargetCommand{
		TargetID: target.ID,
		Request:  analysisRequest,
		Intent: jobTargetIntent(
			"POST",
			"/v1/job-targets/"+target.ID+"/analyses",
			"target-analysis-key",
			analysisRequest,
		),
		Lease: time.Minute,
	}
	parsing, firstClaim, claimed, replayed, err :=
		repository.ClaimAnalysis(ctx, actorA, analysisCommand)
	if err != nil || !claimed || replayed ||
		parsing.Stage != preparation.JobTargetStageParsing ||
		parsing.Analysis == nil ||
		parsing.Analysis.Status != preparation.JobTargetAnalysisRunning {
		t.Fatalf(
			"ClaimAnalysis target=%#v claim=%#v claimed=%t replayed=%t error=%v",
			parsing,
			firstClaim,
			claimed,
			replayed,
			err,
		)
	}
	concurrentCommand := analysisCommand
	concurrentCommand.Intent = jobTargetIntent(
		"POST",
		"/v1/job-targets/"+target.ID+"/analyses",
		"target-analysis-concurrent",
		analysisRequest,
	)
	running, _, claimed, replayed, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		concurrentCommand,
	)
	if err != nil || claimed || replayed ||
		running.Stage != preparation.JobTargetStageParsing {
		t.Fatalf(
			"concurrent claim target=%#v claimed=%t replayed=%t error=%v",
			running,
			claimed,
			replayed,
			err,
		)
	}

	parsedCandidate := jobTargetCandidate(
		preparation.JobTargetSourceJobDescription,
	)
	awaiting, err := repository.CompleteAnalysis(
		ctx,
		firstClaim,
		parsedCandidate,
	)
	if err != nil ||
		awaiting.Stage != preparation.JobTargetStageAwaitingConfirmation ||
		awaiting.Confirmation != nil ||
		awaiting.Analysis == nil ||
		awaiting.Analysis.Candidate == nil {
		t.Fatalf("CompleteAnalysis target=%#v error=%v", awaiting, err)
	}

	confirmedCandidate := parsedCandidate
	confirmedCandidate.PracticeGoals = []string{
		"User-edited goal for a project deep dive.",
	}
	confirmationRequest := preparation.ConfirmJobTargetRequest{
		ExpectedInputVersion:    1,
		ExpectedAnalysisVersion: firstClaim.AnalysisVersion,
		Candidate:               confirmedCandidate,
	}
	confirmationCommand := preparation.ConfirmJobTargetCommand{
		TargetID: target.ID,
		Request:  confirmationRequest,
		Intent: jobTargetIntent(
			"POST",
			"/v1/job-targets/"+target.ID+"/confirmations",
			"target-confirm-key",
			confirmationRequest,
		),
	}
	confirmed, replayed, err := repository.Confirm(
		ctx,
		actorA,
		confirmationCommand,
	)
	if err != nil || replayed ||
		confirmed.Stage != preparation.JobTargetStageConfirmed ||
		confirmed.Confirmation == nil ||
		!reflect.DeepEqual(
			confirmed.Confirmation.Candidate,
			confirmedCandidate,
		) {
		t.Fatalf(
			"Confirm target=%#v replayed=%t error=%v",
			confirmed,
			replayed,
			err,
		)
	}

	updateRequest := preparation.UpdateJobTargetRequest{
		ExpectedInputVersion: 1,
		Source:               preparation.JobTargetSourceQuickStart,
		JobTitle:             "Engineering manager",
		PracticeFocus:        "Leadership communication",
	}
	updateCommand := preparation.UpdateJobTargetCommand{
		TargetID: target.ID,
		Request:  updateRequest,
		Intent: jobTargetIntent(
			"PUT",
			"/v1/job-targets/"+target.ID,
			"target-update-key",
			updateRequest,
		),
	}
	updated, replayed, err := repository.Update(
		ctx,
		actorA,
		updateCommand,
	)
	if err != nil || replayed ||
		updated.InputVersion != 2 ||
		updated.Stage != preparation.JobTargetStageDraft ||
		updated.Analysis != nil ||
		updated.Confirmation != nil {
		t.Fatalf(
			"Update target=%#v replayed=%t error=%v",
			updated,
			replayed,
			err,
		)
	}

	versionTwoAnalysis := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 2,
	}
	versionTwoCommand := preparation.AnalyzeJobTargetCommand{
		TargetID: target.ID,
		Request:  versionTwoAnalysis,
		Intent: jobTargetIntent(
			"POST",
			"/v1/job-targets/"+target.ID+"/analyses",
			"target-version-two-analysis",
			versionTwoAnalysis,
		),
		Lease: time.Minute,
	}
	_, staleClaim, claimed, _, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		versionTwoCommand,
	)
	if err != nil || !claimed {
		t.Fatalf("claim version two: claimed=%t error=%v", claimed, err)
	}
	versionThreeRequest := preparation.UpdateJobTargetRequest{
		ExpectedInputVersion: 2,
		Source:               preparation.JobTargetSourceJobDescription,
		JobDescription:       "New input must fence the old worker.",
	}
	if _, _, err := repository.Update(
		ctx,
		actorA,
		preparation.UpdateJobTargetCommand{
			TargetID: target.ID,
			Request:  versionThreeRequest,
			Intent: jobTargetIntent(
				"PUT",
				"/v1/job-targets/"+target.ID,
				"target-version-three-update",
				versionThreeRequest,
			),
		},
	); err != nil {
		t.Fatalf("update while parser runs: %v", err)
	}
	if _, err := repository.CompleteAnalysis(
		ctx,
		staleClaim,
		jobTargetCandidate(preparation.JobTargetSourceQuickStart),
	); !errors.Is(err, preparation.ErrJobTargetAnalysisClaimLost) {
		t.Fatalf("stale completion error = %v, want claim lost", err)
	}
	recovered, err := repository.Get(ctx, actorA, target.ID)
	if err != nil ||
		recovered.InputVersion != 3 ||
		recovered.Stage != preparation.JobTargetStageDraft ||
		recovered.Analysis != nil ||
		recovered.Confirmation != nil {
		t.Fatalf("recovered target=%#v error=%v", recovered, err)
	}

	versionThreeAnalysis := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 3,
	}
	versionThreeCommand := preparation.AnalyzeJobTargetCommand{
		TargetID: target.ID,
		Request:  versionThreeAnalysis,
		Intent: jobTargetIntent(
			"POST",
			"/v1/job-targets/"+target.ID+"/analyses",
			"target-version-three-analysis",
			versionThreeAnalysis,
		),
		Lease: time.Minute,
	}
	_, expiringClaim, claimed, _, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		versionThreeCommand,
	)
	if err != nil || !claimed {
		t.Fatalf("claim version three: claimed=%t error=%v", claimed, err)
	}
	live, _, claimed, replayed, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		versionThreeCommand,
	)
	if err != nil || claimed || replayed ||
		live.Stage != preparation.JobTargetStageParsing {
		t.Fatalf(
			"same-key live observation target=%#v claimed=%t replayed=%t error=%v",
			live,
			claimed,
			replayed,
			err,
		)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE preparation_job_target_analysis_attempts
		SET lease_until = transaction_timestamp() - interval '1 second'
		WHERE attempt_id = $1
	`, expiringClaim.AttemptID); err != nil {
		t.Fatalf("expire analysis lease: %v", err)
	}
	retryCommand := versionThreeCommand
	_, replacementClaim, claimed, _, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		retryCommand,
	)
	if err != nil || !claimed ||
		replacementClaim.AnalysisVersion !=
			expiringClaim.AnalysisVersion+1 {
		t.Fatalf(
			"replacement claim=%#v claimed=%t error=%v",
			replacementClaim,
			claimed,
			err,
		)
	}
	if _, err := repository.CompleteAnalysis(
		ctx,
		expiringClaim,
		jobTargetCandidate(
			preparation.JobTargetSourceJobDescription,
		),
	); !errors.Is(err, preparation.ErrJobTargetAnalysisClaimLost) {
		t.Fatalf("expired claim completion error = %v", err)
	}
	if _, err := repository.CompleteAnalysis(
		ctx,
		replacementClaim,
		jobTargetCandidate(
			preparation.JobTargetSourceJobDescription,
		),
	); err != nil {
		t.Fatalf("complete replacement claim: %v", err)
	}
	terminal, _, claimed, replayed, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		versionThreeCommand,
	)
	if err != nil || claimed || !replayed ||
		terminal.Stage !=
			preparation.JobTargetStageAwaitingConfirmation {
		t.Fatalf(
			"same-key terminal replay target=%#v claimed=%t replayed=%t error=%v",
			terminal,
			claimed,
			replayed,
			err,
		)
	}
	conflictingAnalysis := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 4,
	}
	conflictingCommand := versionThreeCommand
	conflictingCommand.Request = conflictingAnalysis
	conflictingCommand.Intent = jobTargetIntent(
		"POST",
		"/v1/job-targets/"+target.ID+"/analyses",
		versionThreeCommand.Intent.Key,
		conflictingAnalysis,
	)
	if _, _, _, _, err := repository.ClaimAnalysis(
		ctx,
		actorA,
		conflictingCommand,
	); !errors.Is(
		err,
		preparation.ErrJobTargetIdempotencyConflict,
	) {
		t.Fatalf(
			"changed same-key terminal replay error = %v",
			err,
		)
	}

	restarted := preparation.NewPostgresJobTargetRepository(pool)
	afterRestart, err := restarted.Get(ctx, actorA, target.ID)
	if err != nil ||
		afterRestart.Stage !=
			preparation.JobTargetStageAwaitingConfirmation ||
		afterRestart.Analysis == nil ||
		afterRestart.Analysis.AnalysisVersion !=
			replacementClaim.AnalysisVersion {
		t.Fatalf("restart recovery target=%#v error=%v", afterRestart, err)
	}
}

func TestPostgresJobTargetConfirmationRejectsMultiRoleInterview(
	t *testing.T,
) {
	_, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	repository := preparation.NewPostgresJobTargetRepository(pool)
	ctx := context.Background()
	actor := preparationActor(preparationUserA, preparationSessionA)
	createRequest := preparation.CreateJobTargetRequest{
		Source:         preparation.JobTargetSourceJobDescription,
		JobTitle:       "Platform engineer",
		JobDescription: "Own reliable APIs and explain system trade-offs.",
	}
	target, _, err := repository.Create(
		ctx,
		actor,
		preparation.CreateJobTargetCommand{
			TargetID: "target-multi-role-confirmation",
			Request:  createRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets",
				"multi-role-create-key",
				createRequest,
			),
		},
	)
	if err != nil {
		t.Fatalf("Create JobTarget: %v", err)
	}
	analysisRequest := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 1,
	}
	_, claim, claimed, _, err := repository.ClaimAnalysis(
		ctx,
		actor,
		preparation.AnalyzeJobTargetCommand{
			TargetID: target.ID,
			Request:  analysisRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets/"+target.ID+"/analyses",
				"multi-role-analysis-key",
				analysisRequest,
			),
			Lease: time.Minute,
		},
	)
	if err != nil || !claimed {
		t.Fatalf("ClaimAnalysis = (%+v, %t, %v)", claim, claimed, err)
	}
	multiRole := jobTargetCandidate(
		preparation.JobTargetSourceJobDescription,
	)
	multiRole.CatalogRecommendation.PracticeOptionID =
		preparation.FullSimulationOptionID
	multiRole.CatalogRecommendation.SelectedRoleIDs = []string{
		preparation.TechnicalInterviewerRoleID,
		preparation.HRInterviewerRoleID,
	}
	if _, err := repository.CompleteAnalysis(
		ctx,
		claim,
		multiRole,
	); err != nil {
		t.Fatalf("CompleteAnalysis: %v", err)
	}
	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("NewBuiltinCatalog: %v", err)
	}
	service, err := preparation.NewJobTargetService(
		repository,
		integrationJobTargetDependency{},
		integrationJobTargetDependency{},
		catalog,
	)
	if err != nil {
		t.Fatalf("NewJobTargetService: %v", err)
	}
	confirmation := preparation.ConfirmJobTargetRequest{
		ExpectedInputVersion:    1,
		ExpectedAnalysisVersion: 1,
		Candidate:               multiRole,
	}
	if _, _, err := service.Confirm(
		ctx,
		actor,
		target.ID,
		"multi-role-confirm-key",
		confirmation,
	); !errors.Is(err, preparation.ErrJobTargetInvalid) {
		t.Fatalf("multi-role Confirm error = %v", err)
	}
	persisted, err := repository.Get(ctx, actor, target.ID)
	if err != nil ||
		persisted.Stage != preparation.JobTargetStageAwaitingConfirmation ||
		persisted.Confirmation != nil {
		t.Fatalf("target after rejected Confirm = (%+v, %v)", persisted, err)
	}

	confirmation.Candidate.CatalogRecommendation.SelectedRoleIDs = []string{
		preparation.TechnicalInterviewerRoleID,
	}
	confirmed, _, err := service.Confirm(
		ctx,
		actor,
		target.ID,
		"single-role-full-confirm-key",
		confirmation,
	)
	if err != nil || confirmed.Stage != preparation.JobTargetStageConfirmed {
		t.Fatalf("single-role FULL Confirm = (%+v, %v)", confirmed, err)
	}
}

func TestPostgresJobTargetDraftDiscardIsActorScoped(t *testing.T) {
	_, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA, preparationUserB)
	repository := preparation.NewPostgresJobTargetRepository(pool)
	ctx := context.Background()
	actorA := preparationActor(preparationUserA, preparationSessionA)
	actorB := preparationActor(preparationUserB, preparationSessionB)
	request := preparation.CreateJobTargetRequest{
		Source:   preparation.JobTargetSourceQuickStart,
		JobTitle: "Backend engineer",
	}
	target, _, err := repository.Create(
		ctx,
		actorA,
		preparation.CreateJobTargetCommand{
			TargetID: "target-discard",
			Request:  request,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets",
				"target-discard-create",
				request,
			),
		},
	)
	if err != nil {
		t.Fatalf("create discard target: %v", err)
	}
	discardRequest := preparation.DiscardJobTargetRequest{
		ExpectedInputVersion: 1,
	}
	if _, _, err := repository.Discard(
		ctx,
		actorB,
		preparation.DiscardJobTargetCommand{
			TargetID: target.ID,
			Request:  discardRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets/"+target.ID+"/discard",
				"cross-user-discard",
				discardRequest,
			),
		},
	); !errors.Is(err, preparation.ErrJobTargetNotFound) {
		t.Fatalf("cross-user discard error = %v", err)
	}
	discarded, replayed, err := repository.Discard(
		ctx,
		actorA,
		preparation.DiscardJobTargetCommand{
			TargetID: target.ID,
			Request:  discardRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets/"+target.ID+"/discard",
				"owner-discard-key",
				discardRequest,
			),
		},
	)
	if err != nil || replayed ||
		discarded.Stage != preparation.JobTargetStageDiscarded {
		t.Fatalf(
			"Discard target=%#v replayed=%t error=%v",
			discarded,
			replayed,
			err,
		)
	}
	if _, err := repository.Get(
		ctx,
		actorA,
		target.ID,
	); !errors.Is(err, preparation.ErrJobTargetNotFound) {
		t.Fatalf("Get discarded error = %v", err)
	}
}

func TestPostgresJobTargetConcurrentConfirmationIsExactlyOnce(
	t *testing.T,
) {
	_, pool := newPreparationRepository(t)
	insertPreparationUsers(t, pool, preparationUserA)
	repository := preparation.NewPostgresJobTargetRepository(pool)
	ctx := context.Background()
	actor := preparationActor(preparationUserA, preparationSessionA)
	createRequest := preparation.CreateJobTargetRequest{
		Source:   preparation.JobTargetSourceQuickStart,
		JobTitle: "Platform engineer",
	}
	target, _, err := repository.Create(
		ctx,
		actor,
		preparation.CreateJobTargetCommand{
			TargetID: "target-concurrent-confirmation",
			Request:  createRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets",
				"concurrent-confirm-create",
				createRequest,
			),
		},
	)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	analysisRequest := preparation.AnalyzeJobTargetRequest{
		ExpectedInputVersion: 1,
	}
	_, claim, claimed, _, err := repository.ClaimAnalysis(
		ctx,
		actor,
		preparation.AnalyzeJobTargetCommand{
			TargetID: target.ID,
			Request:  analysisRequest,
			Intent: jobTargetIntent(
				"POST",
				"/v1/job-targets/"+target.ID+"/analyses",
				"concurrent-confirm-analysis",
				analysisRequest,
			),
			Lease: time.Minute,
		},
	)
	if err != nil || !claimed {
		t.Fatalf("claim analysis: claimed=%t error=%v", claimed, err)
	}
	candidate := jobTargetCandidate(
		preparation.JobTargetSourceQuickStart,
	)
	if _, err := repository.CompleteAnalysis(
		ctx,
		claim,
		candidate,
	); err != nil {
		t.Fatalf("complete analysis: %v", err)
	}
	request := preparation.ConfirmJobTargetRequest{
		ExpectedInputVersion:    1,
		ExpectedAnalysisVersion: claim.AnalysisVersion,
		Candidate:               candidate,
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			_, _, err := repository.Confirm(
				context.Background(),
				actor,
				preparation.ConfirmJobTargetCommand{
					TargetID: target.ID,
					Request:  request,
					Intent: jobTargetIntent(
						"POST",
						"/v1/job-targets/"+target.ID+
							"/confirmations",
						fmt.Sprintf(
							"concurrent-confirm-%d",
							index,
						),
						request,
					),
				},
			)
			results <- err
		}(worker)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, preparation.ErrJobTargetConflict):
			conflicts++
		default:
			t.Fatalf("concurrent confirmation error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf(
			"successes=%d conflicts=%d, want 1/1",
			successes,
			conflicts,
		)
	}
	var confirmationCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM preparation_job_target_confirmations
		WHERE owner_user_id = $1 AND target_id = $2
	`, actor.UserID, target.ID).Scan(&confirmationCount); err != nil {
		t.Fatalf("count confirmations: %v", err)
	}
	if confirmationCount != 1 {
		t.Fatalf(
			"confirmation count=%d, want 1",
			confirmationCount,
		)
	}
}

func jobTargetCandidate(
	source preparation.JobTargetSource,
) preparation.JobTargetCandidate {
	return preparation.JobTargetCandidate{
		Source:             source,
		GeneralAdviceOnly:  source == preparation.JobTargetSourceQuickStart,
		JobTitle:           "Platform engineer",
		Seniority:          "Senior",
		Responsibilities:   []string{"Build reliable services."},
		CoreSkills:         []string{"Distributed systems"},
		CommunicationFocus: []string{"Explain trade-offs."},
		PracticeGoals:      []string{"Practice a project deep dive."},
		ScopeNotice:        "Limited to the technical interview content pack.",
		CatalogRecommendation: preparation.JobTargetCatalogRecommendation{
			ScenarioDefinitionID:      preparation.ProgrammerInterviewScenarioID,
			ScenarioDefinitionVersion: 1,
			SelectedRoleIDs: []string{
				preparation.TechnicalInterviewerRoleID,
			},
			PracticeOptionID:      preparation.TechnicalFocusOptionID,
			PracticeOptionVersion: 1,
		},
	}
}

func jobTargetIntent(
	method string,
	path string,
	key string,
	request any,
) preparation.JobTargetOperationIntent {
	encoded, err := json.Marshal(request)
	if err != nil {
		panic("test request must be JSON encodable")
	}
	return preparation.JobTargetOperationIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(encoded),
	}
}

type integrationJobTargetDependency struct{}

func (integrationJobTargetDependency) NewID() (string, error) {
	return "unused-target-id", nil
}

func (integrationJobTargetDependency) ParseJobTarget(
	context.Context,
	preparation.JobTargetInput,
) (preparation.JobTargetCandidate, error) {
	return preparation.JobTargetCandidate{}, errors.New("unused parser")
}
