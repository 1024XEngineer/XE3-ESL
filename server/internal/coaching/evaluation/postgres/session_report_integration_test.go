package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSessionReportReadsReadyFullMockAndIsOwnerScoped(
	t *testing.T,
) {
	pool, repository, configuration, value :=
		prepareIELTSSpeakingShadowRuntime(t)
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsPostgresQuestionCount)
	installIELTSSessionReportAuthorityFixture(t, pool, snapshot)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	result := evaluateIELTSSpeakingClaim(t, claim)
	if err := repository.CompleteIELTSSpeakingShadow(
		context.Background(),
		claim,
		result,
	); err != nil {
		t.Fatalf("CompleteIELTSSpeakingShadow: %v", err)
	}

	state, err := repository.GetCurrentSessionReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	)
	if err != nil {
		t.Fatalf("GetCurrentSessionReportState: %v", err)
	}
	if state.PracticeMode != string(practice.PracticeModeFullMock) ||
		state.Status != evaluationcore.StatusReady ||
		state.Evaluation == nil || state.Evaluation.ID != value.ID ||
		state.FormalReport == nil || !state.FormalReport.Valid() ||
		state.FormalReport.Report.DetailSchema !=
			"ielts-speaking-report/v1" ||
		len(state.AvailableSections) != 3 ||
		state.AvailableSections[0] != "PART_1" ||
		state.AvailableSections[1] != "PART_2" ||
		state.AvailableSections[2] != "PART_3" {
		t.Fatalf("ready Session report state = %#v", state)
	}
	if _, err := repository.GetCurrentSessionReportState(
		context.Background(),
		integrationOwnerB,
		value.PracticeSessionID,
	); !errors.Is(err, evaluationcore.ErrNotFound) {
		t.Fatalf("cross-owner Session report error = %v", err)
	}
}

func TestPostgresSessionReportReadsLegacyPart1GeneralSceneReport(
	t *testing.T,
) {
	snapshot := generalSceneTestSnapshot(
		t,
		evaluationcore.SceneIELTSSpeaking,
		scene.PracticeExperienceIELTSSpeaking,
		scene.SceneCategoryIELTSSpeaking,
		scene.PracticeModePart1,
		"I read every evening because it helps me relax.",
	)
	pool, repository, configuration, evaluationID :=
		prepareGeneralScenePostgresEvaluation(t, snapshot)
	installIELTSSessionReportAuthorityFixture(t, pool, snapshot)
	worker, err := scoring.NewGeneralSceneWorker(
		repository,
		scoring.NewGeneralSceneEngine(&atomicGeneralSceneProviderStub{
			calls: make(map[scoring.GeneralSceneDimension]int),
		}),
		configuration,
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil || sweep.Completed != 1 {
		t.Fatalf("legacy general Scene sweep=%#v err=%v", sweep, err)
	}

	state, err := repository.GetCurrentSessionReportState(
		context.Background(),
		testOwnerA,
		snapshot.PracticeSessionID,
	)
	if err != nil || state.PracticeMode != string(practice.PracticeModePart1) ||
		state.Status != evaluationcore.StatusReady ||
		state.Evaluation == nil || state.Evaluation.ID != evaluationID ||
		state.Evaluation.Revision.SceneStrategyRef !=
			scoring.GeneralSceneStrategyRef ||
		state.FormalReport == nil || !state.FormalReport.Valid() ||
		state.FormalReport.Report.DetailSchema !=
			report.IELTSSpeakingPracticeReportSchemaVersion ||
		len(state.AvailableSections) != 1 ||
		state.AvailableSections[0] != "PART_1" {
		t.Fatalf("legacy Part 1 Session report state=%#v err=%v", state, err)
	}
}

func TestPostgresSessionReportReadsMigratedActiveLeaseAsValidating(
	t *testing.T,
) {
	pool, repository, configuration, value :=
		prepareIELTSSpeakingShadowRuntime(t)
	snapshot := ieltsSpeakingTestSnapshot(t, ieltsPostgresQuestionCount)
	installIELTSSessionReportAuthorityFixture(t, pool, snapshot)
	claim := claimIELTSSpeakingShadow(t, repository, configuration)
	if claim.AttemptCount != 1 || !claim.LeaseExpiresAt.After(time.Now()) {
		t.Fatalf("active claim=%#v", claim)
	}
	reapplyIELTSAcousticSnapshotMigration(t, pool)

	state, err := repository.GetCurrentSessionReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	)
	if err != nil || state.Status != evaluationcore.StatusValidating ||
		state.Evaluation == nil || state.Evaluation.ID != value.ID ||
		state.FormalReport != nil || state.Failure != nil {
		t.Fatalf("migrated active Session report state=%#v err=%v", state, err)
	}
	var leaseCleared bool
	if err := pool.QueryRow(context.Background(), `
		SELECT lease_expires_at IS NULL
		FROM evaluation_outbox
		WHERE id = $1
	`, claim.OutboxID).Scan(&leaseCleared); err != nil || !leaseCleared {
		t.Fatalf("migrated active lease cleared=%t err=%v", leaseCleared, err)
	}

	draft, err := scoring.BuildIELTSAcousticSnapshot(
		value.ID,
		claim.Snapshot,
		scoring.IELTSSpeakingAcousticRead{},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repository.EnsureIELTSAcousticSnapshot(
		context.Background(),
		scoring.IELTSAcousticSnapshotClaim{
			EvaluationID:         value.ID,
			EvaluationRevisionID: value.Revision.ID,
			OwnerUserID:          testOwnerA,
			RevisionCreatedAt:    value.Revision.CreatedAt,
			Snapshot:             claim.Snapshot,
		},
		draft,
	)
	if err != nil {
		t.Fatalf("freeze migrated active lease: %v", err)
	}
	state, err = repository.GetCurrentSessionReportState(
		context.Background(),
		testOwnerA,
		value.PracticeSessionID,
	)
	if err != nil || state.Status != evaluationcore.StatusQueued ||
		state.Evaluation == nil || state.FormalReport != nil ||
		state.Failure != nil {
		t.Fatalf("frozen migrated Session report state=%#v err=%v", state, err)
	}
}

func installIELTSSessionReportAuthorityFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	snapshot evidence.EvidenceSnapshot,
) {
	t.Helper()
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatalf("decode IELTS Evidence fixture: %v", err)
	}
	practiceContext := payload.PracticeContext
	assignment := practiceContext.IELTSAssignment
	mode := practice.PracticeMode(practiceContext.PracticeMode)
	if assignment == nil ||
		(mode != practice.PracticeModePart1 &&
			mode != practice.PracticeModePart2 &&
			mode != practice.PracticeModePart3 &&
			mode != practice.PracticeModeFullMock) ||
		len(payload.ConfirmedTurns) == 0 {
		t.Fatalf("unsupported IELTS Session report fixture: %#v", payload)
	}

	const (
		sceneID  = "session-report-ielts-scene"
		roleID   = "session-report-examiner"
		planID   = "session-report-plan"
		threadID = "30000000-0000-4000-8000-000000000594"
	)
	authorityAt := time.Date(2026, time.August, 11, 1, 0, 0, 0, time.UTC)
	objectives := make(
		[]practice.PracticeObjective,
		len(practiceContext.PracticeObjectives),
	)
	roleObjectives := make(
		[]practice.PracticeObjectiveDefinition,
		len(practiceContext.PracticeObjectives),
	)
	for index, objective := range practiceContext.PracticeObjectives {
		objectives[index] = practice.PracticeObjective{
			ID: objective.ID, Description: objective.Description,
		}
		roleObjectives[index] = practice.PracticeObjectiveDefinition{
			ID: objective.ID, Description: objective.Description,
		}
	}
	ieltsAssignment := &practice.IELTSAssignment{
		BankID: assignment.BankID,
		Season: assignment.Season,
		Mode:   practice.PracticeMode(assignment.Mode),
		Parts:  make([]practice.IELTSPart, len(assignment.Parts)),
	}
	for index, part := range assignment.Parts {
		ieltsAssignment.Parts[index] = practice.IELTSPart{
			Part:           practice.PracticeMode(part.Part),
			SourceID:       part.SourceID,
			TopicTitle:     part.TopicTitle,
			CueCard:        part.CueCard,
			TurnBlueprints: append([]string(nil), part.TurnBlueprints...),
		}
	}
	turnPolicyRef, sessionPolicyRef, evaluationPolicyRef :=
		ieltsSessionReportFixturePolicies(t, mode)
	option := practice.PracticeOption{
		ID:                       practiceContext.PracticeOption.ID,
		SceneID:                  sceneID,
		Mode:                     mode,
		DisplayName:              "IELTS " + string(mode),
		SuggestedDurationSeconds: practiceContext.TaskContext.SuggestedDurationSeconds,
		TurnPolicyRef:            turnPolicyRef,
		SessionPolicyRef:         sessionPolicyRef,
		EvaluationPolicyRef:      evaluationPolicyRef,
	}
	options := []practice.PracticeOption{
		{
			ID: "session-report-full-mock", SceneID: sceneID,
			Mode: practice.PracticeModeFullMock, DisplayName: "IELTS full mock",
			SuggestedDurationSeconds: 900,
			TurnPolicyRef:            practice.IELTSSpeakingFullMockTurnPolicy,
			SessionPolicyRef:         practice.IELTSSpeakingFullMockSessionPolicy,
			EvaluationPolicyRef:      scoring.IELTSSpeakingFullMockEvaluationPolicyRef,
		},
		{
			ID: "session-report-part-1", SceneID: sceneID,
			Mode: practice.PracticeModePart1, DisplayName: "IELTS Part 1",
			SuggestedDurationSeconds: 300,
			TurnPolicyRef:            practice.IELTSSpeakingPart1TurnPolicy,
			SessionPolicyRef:         practice.IELTSSpeakingPart1SessionPolicy,
			EvaluationPolicyRef:      scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
		{
			ID: "session-report-part-2", SceneID: sceneID,
			Mode: practice.PracticeModePart2, DisplayName: "IELTS Part 2",
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            practice.IELTSSpeakingPart2TurnPolicy,
			SessionPolicyRef:         practice.IELTSSpeakingPart2SessionPolicy,
			EvaluationPolicyRef:      scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
		{
			ID: "session-report-part-3", SceneID: sceneID,
			Mode: practice.PracticeModePart3, DisplayName: "IELTS Part 3",
			SuggestedDurationSeconds: 300,
			TurnPolicyRef:            practice.IELTSSpeakingPart3TurnPolicy,
			SessionPolicyRef:         practice.IELTSSpeakingPart3SessionPolicy,
			EvaluationPolicyRef:      scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
	}
	for index := range options {
		if options[index].Mode == mode {
			options[index] = option
		}
	}
	role := practice.RoleDefinition{
		ID:                 roleID,
		SceneID:            sceneID,
		Type:               "IELTS_EXAMINER",
		DisplayName:        "IELTS examiner",
		Responsibilities:   "Deliver the frozen IELTS questions.",
		Style:              "Neutral",
		PracticeObjectives: roleObjectives,
	}
	selection := practice.SceneSelection{
		Scene: practice.SceneDefinition{
			ID:         sceneID,
			Experience: practice.PracticeExperienceIELTSSpeaking,
			Category:   practice.SceneCategory("IELTS_SPEAKING"),
			Name:       "IELTS Session report fixture",
			Version:    1,
			Status:     practice.SceneStatusActive,
			Prompt: practice.ScenePrompt{
				PublicSceneBrief: practiceContext.TaskContext.PublicSceneBrief,
				PracticeGoal:     practiceContext.PracticeGoal,
				UserRole:         practiceContext.UserRole,
				AIRole:           practiceContext.FacilitatorRole,
				PersonaSummary:   practiceContext.TaskContext.PersonaSummary,
				FocusAreas: append(
					[]string(nil),
					practiceContext.TaskContext.FocusAreas...,
				),
				TurnBlueprints: append(
					[]string(nil),
					practiceContext.TaskBlueprints...,
				),
			},
			Roles:           []practice.RoleDefinition{role},
			PracticeOptions: options,
		},
		SelectedRoleIDs:  []string{roleID},
		PracticeOptionID: option.ID,
	}
	participants := make([]practice.Participant, len(practiceContext.Participants))
	for index, participant := range practiceContext.Participants {
		subject := practice.SubjectRef{
			Namespace: "speakup.fixture", SubjectID: participant.ID,
		}
		if participant.Role == "LEARNER" {
			subject = practice.SubjectRef{
				Namespace: "speakup.user", SubjectID: snapshot.OwnerUserID,
			}
		}
		participants[index] = practice.Participant{
			ID: participant.ID, SessionID: snapshot.PracticeSessionID,
			Role: participant.Role, SubjectRef: subject, Order: participant.Order,
		}
	}
	policy := practice.SessionPolicy{
		CompletionMode:           practice.CompletionModeTurnLimited,
		SuggestedDurationSeconds: practiceContext.TaskContext.SuggestedDurationSeconds,
		MinEffectiveTurns:        len(practiceContext.TaskBlueprints),
		MaxEffectiveTurns:        len(practiceContext.TaskBlueprints),
		CoverageCheckpointTurn:   len(practiceContext.TaskBlueprints),
		MaxFollowUpsPerQuestion:  1,
		EarlyCompletionRule: practice.
			EarlyCompletionCoverageSatisfiedAfterCheckpoint,
	}
	sessionSnapshot := practice.SessionSnapshot{
		ID:             practiceContext.SessionSnapshotID,
		SessionID:      snapshot.PracticeSessionID,
		PlanRevision:   practiceContext.PlanRevision,
		Experience:     practice.PracticeExperienceIELTSSpeaking,
		Category:       practice.SceneCategory("IELTS_SPEAKING"),
		PracticeMode:   mode,
		SceneSelection: selection,
		Preparation: practice.PreparationSnapshot{
			ID:                 practiceContext.Preparation.SnapshotID,
			SourceProfileID:    practiceContext.Preparation.SourceProfileID,
			SourceVersion:      practiceContext.Preparation.SourceVersion,
			BackgroundSnapshot: evidenceTestPreparationBackground,
			CreatedAt:          authorityAt,
		},
		Participants:       participants,
		SessionPolicy:      policy,
		PracticeObjectives: objectives,
		IELTSAssignment:    ieltsAssignment,
		CreatedAt:          authorityAt,
	}
	if !practice.ValidIELTSAssignment(
		sessionSnapshot.IELTSAssignment,
		sessionSnapshot.PracticeMode,
		sessionSnapshot.SceneSelection.Scene.Prompt.TurnBlueprints,
	) {
		t.Fatal("invalid frozen IELTS Session assignment fixture")
	}

	snapshotDocument := mustJSONFixture(t, sessionSnapshot)
	preparationDocument := mustJSONFixture(t, sessionSnapshot.Preparation)
	selectionDocument := mustJSONFixture(t, selection)
	policyDocument := mustJSONFixture(t, policy)
	objectivesDocument := mustJSONFixture(t, objectives)
	assignmentDocument := mustJSONFixture(t, ieltsAssignment)
	participantsDocument := mustJSONFixture(t, participants)
	promptDocument := mustJSONFixture(t, selection.Scene.Prompt)
	rolesDocument := mustJSONFixture(t, []map[string]any{{
		"role_definition_id":  role.ID,
		"scene_id":            role.SceneID,
		"role_type":           role.Type,
		"display_name":        role.DisplayName,
		"responsibilities":    role.Responsibilities,
		"style":               role.Style,
		"practice_objectives": role.PracticeObjectives,
		"display_order":       1,
	}})
	databaseOptions := make([]map[string]any, len(options))
	for index, candidate := range options {
		databaseOptions[index] = map[string]any{
			"practice_option_id":         candidate.ID,
			"scene_id":                   candidate.SceneID,
			"practice_mode":              candidate.Mode,
			"display_name":               candidate.DisplayName,
			"suggested_duration_seconds": candidate.SuggestedDurationSeconds,
			"turn_policy_ref":            candidate.TurnPolicyRef,
			"session_policy_ref":         candidate.SessionPolicyRef,
			"evaluation_policy_ref":      candidate.EvaluationPolicyRef,
			"display_order":              index + 1,
		}
	}
	optionsDocument := mustJSONFixture(t, databaseOptions)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin IELTS Session report authority fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		t.Fatalf("defer IELTS Session authority constraints: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_threads (id, owner_user_id) VALUES ($1, $2)
	`, threadID, snapshot.OwnerUserID); err != nil {
		t.Fatalf("insert Agent Thread fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_profiles (
			owner_user_id, profile_id, background_summary, version,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5)
	`, snapshot.OwnerUserID, practiceContext.Preparation.SourceProfileID,
		evidenceTestPreparationBackground,
		practiceContext.Preparation.SourceVersion, authorityAt); err != nil {
		t.Fatalf("insert Preparation Profile fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id, snapshot_id, source_profile_id, source_version,
			background_snapshot, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, snapshot.OwnerUserID, practiceContext.Preparation.SnapshotID,
		practiceContext.Preparation.SourceProfileID,
		practiceContext.Preparation.SourceVersion,
		evidenceTestPreparationBackground, authorityAt); err != nil {
		t.Fatalf("insert Preparation Snapshot fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coaching_scenes (scene_id, created_at) VALUES ($1, $2)
	`, sceneID, authorityAt); err != nil {
		t.Fatalf("insert Scene fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO coaching_scene_versions (
			scene_id, scene_version, practice_experience, scene_category,
			name, status, prompt, roles, practice_options, display_order,
			created_at
		) VALUES (
			$1, 1, 'IELTS_SPEAKING', 'IELTS_SPEAKING',
			'IELTS Session report fixture', 'active', $2, $3, $4, 1, $5
		)
	`, sceneID, promptDocument, rolesDocument, optionsDocument,
		authorityAt); err != nil {
		t.Fatalf("insert Scene version fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_practice_plans (
			owner_user_id, plan_id, current_revision, status,
			source_thread_id, created_at, updated_at
		) VALUES ($1, $2, $3, 'ready', $4, $5, $5)
	`, snapshot.OwnerUserID, planID, practiceContext.PlanRevision,
		threadID, authorityAt); err != nil {
		t.Fatalf("insert Preparation Plan fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_practice_plan_revisions (
			owner_user_id, plan_id, revision, preparation_snapshot_id,
			preparation_snapshot, scene_id, scene_version, scene_selection,
			session_policy, practice_objectives, ielts_assignment, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, $10, $11)
	`, snapshot.OwnerUserID, planID, practiceContext.PlanRevision,
		practiceContext.Preparation.SnapshotID, preparationDocument,
		sceneID, selectionDocument, policyDocument, objectivesDocument,
		assignmentDocument, authorityAt); err != nil {
		t.Fatalf("insert Preparation Plan revision fixture: %v", err)
	}
	completedAt := authorityAt.Add(time.Minute)
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id, session_id, plan_id, status, version,
			effective_turns, created_at, updated_at, started_at, completed_at,
			snapshot_id, practice_experience, scene_category, practice_mode,
			evaluation_policy_ref, end_reason, plan_revision
		) VALUES (
			$1, $2, $3, 'completed', $4, $5, $6, $6, $6, $7,
			$8, 'IELTS_SPEAKING', 'IELTS_SPEAKING', $9,
			$10, 'COMPLETED', $11
		)
	`, snapshot.OwnerUserID, snapshot.PracticeSessionID, planID,
		practiceContext.SessionVersion, len(payload.ConfirmedTurns),
		authorityAt, completedAt, practiceContext.SessionSnapshotID,
		mode, evaluationPolicyRef,
		practiceContext.PlanRevision); err != nil {
		t.Fatalf("insert Practice Session fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id, session_id, practice_mode, target_ids,
			participants, turn_limit, created_at, snapshot_id,
			snapshot_document
		) VALUES ($1, $2, $3, '[]'::jsonb, $4, $5, $6, $7, $8)
	`, snapshot.OwnerUserID, snapshot.PracticeSessionID,
		mode, participantsDocument, len(payload.OpportunityManifest), authorityAt,
		practiceContext.SessionSnapshotID, snapshotDocument); err != nil {
		t.Fatalf("insert Practice Session Snapshot fixture: %v", err)
	}
	finalTurn := payload.ConfirmedTurns[len(payload.ConfirmedTurns)-1]
	fingerprint := sha256.Sum256([]byte("session-report-final-turn"))
	const completionToken = "session-report-completion-token"
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_turn_results (
			owner_user_id, session_id, turn_id, payload_fingerprint,
			round_number, effective_turns, session_version, completed,
			completion_token
		) VALUES ($1, $2, $3, $4, $5, $5, $6, true, $7)
	`, snapshot.OwnerUserID, snapshot.PracticeSessionID, finalTurn.TurnID,
		fingerprint[:], len(payload.ConfirmedTurns),
		practiceContext.SessionVersion, completionToken); err != nil {
		t.Fatalf("insert final Turn result fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_completed (
			owner_user_id, session_id, final_turn_id, session_version,
			completion_token, delivery_status, attempt_count, fencing_token,
			available_at, created_at, updated_at, delivered_at
		) VALUES (
			$1, $2, $3, $4, $5, 'DELIVERED', 1, 1, $6, $6, $6, $6
		)
	`, snapshot.OwnerUserID, snapshot.PracticeSessionID, finalTurn.TurnID,
		practiceContext.SessionVersion, completionToken,
		completedAt); err != nil {
		t.Fatalf("insert Practice completion handoff fixture: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit IELTS Session report authority fixture: %v", err)
	}
}

func ieltsSessionReportFixturePolicies(
	t *testing.T,
	mode practice.PracticeMode,
) (string, string, string) {
	t.Helper()
	switch mode {
	case practice.PracticeModePart1:
		return practice.IELTSSpeakingPart1TurnPolicy,
			practice.IELTSSpeakingPart1SessionPolicy,
			scoring.IELTSSpeakingPracticeEvaluationPolicyRef
	case practice.PracticeModePart2:
		return practice.IELTSSpeakingPart2TurnPolicy,
			practice.IELTSSpeakingPart2SessionPolicy,
			scoring.IELTSSpeakingPracticeEvaluationPolicyRef
	case practice.PracticeModePart3:
		return practice.IELTSSpeakingPart3TurnPolicy,
			practice.IELTSSpeakingPart3SessionPolicy,
			scoring.IELTSSpeakingPracticeEvaluationPolicyRef
	case practice.PracticeModeFullMock:
		return practice.IELTSSpeakingFullMockTurnPolicy,
			practice.IELTSSpeakingFullMockSessionPolicy,
			scoring.IELTSSpeakingFullMockEvaluationPolicyRef
	default:
		t.Fatalf("unsupported IELTS fixture mode %q", mode)
		return "", "", ""
	}
}

func mustJSONFixture(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode Session report fixture: %v", err)
	}
	return encoded
}
