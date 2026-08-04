package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	practice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresEvidenceSnapshotIdempotencyAndIsolation(
	t *testing.T,
) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA, integrationOwnerB)
	repository := NewPostgresRepository(pool)
	ctx := context.Background()
	command := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, command)

	created, replayed, err := repository.EnsureEvidenceSnapshot(
		ctx,
		command,
	)
	if err != nil {
		t.Fatalf("EnsureEvidenceSnapshot: %v", err)
	}
	if replayed || created.InputRevision != 1 || !created.Valid() {
		t.Fatalf("created = %#v, replayed = %v", created, replayed)
	}
	replay, replayed, err := repository.EnsureEvidenceSnapshot(ctx, command)
	if err != nil {
		t.Fatalf("replay EnsureEvidenceSnapshot: %v", err)
	}
	if !replayed || replay.ID != created.ID ||
		replay.InputRevision != created.InputRevision {
		t.Fatalf("replay = %#v, replayed = %v", replay, replayed)
	}
	fetched, err := repository.GetEvidenceSnapshot(
		ctx,
		testOwnerA,
		created.ID,
	)
	if err != nil || fetched.ID != created.ID || !fetched.Valid() {
		t.Fatalf("GetEvidenceSnapshot = %#v, %v", fetched, err)
	}
	if _, err := repository.GetEvidenceSnapshot(
		ctx,
		integrationOwnerB,
		created.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner GetEvidenceSnapshot error = %v", err)
	}

	conflict := command
	conflict.CanonicalPayload = replaceEvidencePayloadTranscript(
		t,
		command.CanonicalPayload,
		"changed without a new source manifest",
	)
	if _, _, err := repository.EnsureEvidenceSnapshot(
		ctx,
		conflict,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mismatched source manifest error = %v", err)
	}

	const revisedQuestion = "Tell me about a production migration you led."
	if _, err := pool.Exec(ctx, `
		UPDATE practice_questions
		SET content = $1
		WHERE owner_user_id = $2
		  AND practice_session_id = $3
		  AND question_id = 'question-1'
	`, revisedQuestion, testOwnerA, command.PracticeSessionID); err != nil {
		t.Fatalf("update authoritative question source: %v", err)
	}
	var revisedPayload map[string]any
	if err := json.Unmarshal(
		command.CanonicalPayload,
		&revisedPayload,
	); err != nil {
		t.Fatalf("decode revision 2 payload: %v", err)
	}
	revisedPayload["opportunity_manifest"].([]any)[0].(map[string]any)["question_text"] =
		revisedQuestion
	revisedPayloadJSON, err := json.Marshal(revisedPayload)
	if err != nil {
		t.Fatalf("encode revision 2 payload: %v", err)
	}
	revisionTwoCommand := command
	revisionTwoCommand.SourceManifestHash, err = evidenceSourceManifestHash(
		revisedPayloadJSON,
	)
	if err != nil {
		t.Fatalf("derive revision 2 source manifest: %v", err)
	}
	revisionTwoCommand.SnapshotID = deriveEvidenceSnapshotID(
		revisionTwoCommand.OwnerUserID,
		revisionTwoCommand.PracticeSessionID,
		revisionTwoCommand.Scope,
		revisionTwoCommand.SourceManifestHash,
	)
	revisionTwoCommand.CanonicalPayload = replaceEvidencePayloadSnapshotID(
		t,
		revisedPayloadJSON,
		revisionTwoCommand.SnapshotID,
	)
	revisionTwo, replayed, err := repository.EnsureEvidenceSnapshot(
		ctx,
		revisionTwoCommand,
	)
	if err != nil {
		t.Fatalf("EnsureEvidenceSnapshot revision 2: %v", err)
	}
	if replayed || revisionTwo.InputRevision != 2 ||
		revisionTwo.ID == created.ID {
		t.Fatalf(
			"revision two = %#v, replayed = %v",
			revisionTwo,
			replayed,
		)
	}
	oldRevision, err := repository.GetEvidenceSnapshot(
		ctx,
		testOwnerA,
		created.ID,
	)
	if err != nil || oldRevision.InputRevision != 1 ||
		!bytes.Equal(oldRevision.Payload, created.Payload) {
		t.Fatalf("old immutable revision = %#v, error = %v", oldRevision, err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE evaluation_evidence_snapshots
		SET scene_type = 'IELTS_SPEAKING'
		WHERE id = $1
	`, created.ID); postgresCode(err) != "55000" {
		t.Fatalf("immutable update error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM evaluation_evidence_snapshots
		WHERE id = $1
	`, created.ID); postgresCode(err) != "55000" {
		t.Fatalf("immutable delete error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO evaluation_evidence_snapshots (
			owner_user_id,
			practice_session_id,
			scope,
			scene_type,
			input_revision,
			source_manifest_hash,
			snapshot_hash,
			canonical_payload
		)
		VALUES (
			$1,
			'private-locator-session',
			'SESSION',
			'INTERVIEW',
			1,
			decode(repeat('ab', 32), 'hex'),
			decode(repeat('cd', 32), 'hex'),
			'{
				"practice_context": {},
				"opportunity_manifest": {},
				"confirmed_turns": [{
					"audio": {"object_key": "private/audio.wav"}
				}],
				"evidence_refs": [],
				"provider_lineage": {},
				"version_manifest": {}
			}'::jsonb
		)
	`, testOwnerA); postgresCode(err) != "23514" {
		t.Fatalf("private storage locator constraint error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO evaluation_evidence_snapshots (
			id,
			owner_user_id,
			practice_session_id,
			scope,
			scene_type,
			input_revision,
			source_manifest_hash,
			snapshot_hash,
			canonical_payload
		)
		VALUES (
			'snapshot_expected',
			$1,
			'cross-snapshot-session',
			'SESSION',
			'INTERVIEW',
			1,
			decode(repeat('ab', 32), 'hex'),
			decode(repeat('cd', 32), 'hex'),
			$2::jsonb
		)
	`, testOwnerA, evidenceSnapshotPayloadForID(
		"snapshot_other",
	)); postgresCode(err) != "23514" {
		t.Fatalf("cross-snapshot EvidenceRef error = %v", err)
	}
	bindingPayload := evidenceSnapshotPayloadForMetadata(
		"snapshot_binding",
		"binding-session",
	)
	var bindingDecoded map[string]any
	if err := json.Unmarshal(bindingPayload, &bindingDecoded); err != nil {
		t.Fatalf("decode binding fixture: %v", err)
	}
	bindingDecoded["evidence_refs"].([]any)[0].(map[string]any)["turn_id"] = "turn-other"
	bindingPayload, err = json.Marshal(bindingDecoded)
	if err != nil {
		t.Fatalf("encode binding fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO evaluation_evidence_snapshots (
			id,
			owner_user_id,
			practice_session_id,
			scope,
			scene_type,
			input_revision,
			source_manifest_hash,
			snapshot_hash,
			canonical_payload
		)
		VALUES (
			'snapshot_binding',
			$1,
			'binding-session',
			'SESSION',
			'INTERVIEW',
			1,
			decode(repeat('ef', 32), 'hex'),
			decode(repeat('12', 32), 'hex'),
			$2::jsonb
		)
	`, testOwnerA, bindingPayload); postgresCode(err) != "23514" {
		t.Fatalf("cross-turn EvidenceRef error = %v", err)
	}
}

func TestPostgresEvidenceSnapshotRejectsDeletionFencedOwner(t *testing.T) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("install account deletion fence: %v", err)
	}
	repository := NewPostgresRepository(pool)
	if _, _, err := repository.EnsureEvidenceSnapshot(
		context.Background(),
		validEvidenceSnapshotCommand(),
	); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("deletion-fenced Ensure error = %v", err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM evaluation_evidence_snapshots
	`).Scan(&count); err != nil {
		t.Fatalf("count deletion-fenced snapshots: %v", err)
	}
	if count != 0 {
		t.Fatalf("deletion-fenced snapshot count = %d", count)
	}
}

func TestPostgresEvidenceSnapshotRejectsTamperedTurnAudience(t *testing.T) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	command := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, command)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		UPDATE practice_turns
		SET speaker_participant_id = 'participant-other'
		WHERE owner_user_id = $1 AND turn_id = 'turn-1'
	`, testOwnerA); err != nil {
		t.Fatalf("tamper confirmed Turn audience: %v", err)
	}
	if _, _, err := NewPostgresRepository(pool).EnsureEvidenceSnapshot(
		ctx,
		command,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("tampered Turn audience Ensure error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM evaluation_evidence_snapshots
	`).Scan(&count); err != nil {
		t.Fatalf("count snapshots after Turn audience tamper: %v", err)
	}
	if count != 0 {
		t.Fatalf("snapshot count after Turn audience tamper = %d", count)
	}
}

func TestPostgresEvidenceSnapshotWaitsForConcurrentDeletionFence(
	t *testing.T,
) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	ctx := context.Background()
	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deletion fence: %v", err)
	}
	defer func() { _ = deletion.Rollback(ctx) }()
	if _, err := deletion.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("stage deletion fence: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, ensureErr := NewPostgresRepository(pool).
			EnsureEvidenceSnapshot(
				context.Background(),
				validEvidenceSnapshotCommand(),
			)
		result <- ensureErr
	}()
	select {
	case ensureErr := <-result:
		t.Fatalf(
			"Ensure completed before deletion fence committed: %v",
			ensureErr,
		)
	case <-time.After(100 * time.Millisecond):
	}
	if err := deletion.Commit(ctx); err != nil {
		t.Fatalf("commit deletion fence: %v", err)
	}
	select {
	case ensureErr := <-result:
		if !errors.Is(ensureErr, ErrAccountUnavailable) {
			t.Fatalf("concurrent deletion Ensure error = %v", ensureErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ensure did not resume after deletion fence committed")
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM evaluation_evidence_snapshots
	`).Scan(&count); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if count != 0 {
		t.Fatalf("snapshot count after deletion fence = %d", count)
	}
}

func TestPostgresEvidenceSnapshotWaitsForConcurrentSourceDeletion(
	t *testing.T,
) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	command := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, command)
	ctx := context.Background()
	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Practice source deletion: %v", err)
	}
	defer func() { _ = deletion.Rollback(ctx) }()
	if _, err := deletion.Exec(ctx, `
		DELETE FROM practice_sessions
		WHERE owner_user_id = $1 AND session_id = $2
	`, command.OwnerUserID, command.PracticeSessionID); err != nil {
		t.Fatalf("stage Practice source deletion: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, ensureErr := NewPostgresRepository(pool).
			EnsureEvidenceSnapshot(
				context.Background(),
				command,
			)
		result <- ensureErr
	}()
	select {
	case ensureErr := <-result:
		t.Fatalf(
			"Ensure completed before source deletion committed: %v",
			ensureErr,
		)
	case <-time.After(100 * time.Millisecond):
	}
	if err := deletion.Commit(ctx); err != nil {
		t.Fatalf("commit Practice source deletion: %v", err)
	}
	select {
	case ensureErr := <-result:
		if !errors.Is(ensureErr, ErrNotFound) {
			t.Fatalf("concurrent source deletion Ensure error = %v", ensureErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ensure did not resume after source deletion committed")
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM evaluation_evidence_snapshots
	`).Scan(&count); err != nil {
		t.Fatalf("count snapshots after source deletion: %v", err)
	}
	if count != 0 {
		t.Fatalf("snapshot count after source deletion = %d", count)
	}
}

func TestPostgresEvaluationDeletionPurgesImmutableUserData(t *testing.T) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	repository := NewPostgresRepository(pool)
	ctx := context.Background()
	snapshotCommand := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, snapshotCommand)
	if _, _, err := repository.EnsureEvidenceSnapshot(
		ctx,
		snapshotCommand,
	); err != nil {
		t.Fatalf("create EvidenceSnapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `
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
			decode(repeat('ab', 32), 'hex'),
			decode(repeat('cd', 32), 'hex'),
			'practice-session-1',
			'snapshot-test',
			1,
			'SESSION',
			'INTERVIEW'
		)
	`, testOwnerA); err != nil {
		t.Fatalf("create Evaluation ledger: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("set owner deleting: %v", err)
	}
	command := DeleteUserDataCommand{
		OwnerUserID:        testOwnerA,
		DeletionGeneration: 2,
	}
	if err := repository.DeleteUserData(ctx, command); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}
	if err := repository.DeleteUserData(ctx, command); err != nil {
		t.Fatalf("idempotent DeleteUserData: %v", err)
	}
	stale := command
	stale.DeletionGeneration = 1
	if err := repository.DeleteUserData(ctx, stale); !errors.Is(
		err,
		ErrDeletionGenerationStale,
	) {
		t.Fatalf("stale deletion generation error = %v", err)
	}
	var snapshots int
	var ledgers int
	var generation int64
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM evaluation_evidence_snapshots),
			(SELECT count(*) FROM evaluation_ledgers),
			(SELECT deletion_generation
			 FROM evaluation_deletion_fences
			 WHERE owner_user_id = $1)
	`, testOwnerA).Scan(
		&snapshots,
		&ledgers,
		&generation,
	); err != nil {
		t.Fatalf("inspect deleted Evaluation data: %v", err)
	}
	if snapshots != 0 || ledgers != 0 || generation != 2 {
		t.Fatalf(
			"deleted counts snapshots=%d ledgers=%d generation=%d",
			snapshots,
			ledgers,
			generation,
		)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_users
		SET account_status = 'active'
		WHERE id = $1
	`, testOwnerA); err != nil {
		t.Fatalf("restore active status for fence test: %v", err)
	}
	if _, _, err := repository.EnsureEvidenceSnapshot(
		ctx,
		validEvidenceSnapshotCommand(),
	); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("persisted deletion fence Ensure error = %v", err)
	}
}

func TestPostgresConcurrentEvidenceSnapshotReplayCreatesOneRevision(
	t *testing.T,
) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	repository := NewPostgresRepository(pool)
	command := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, command)

	const callers = 12
	start := make(chan struct{})
	results := make(chan EvidenceSnapshot, callers)
	failures := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			snapshot, _, err := repository.EnsureEvidenceSnapshot(
				context.Background(),
				command,
			)
			if err != nil {
				failures <- err
				return
			}
			results <- snapshot
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		t.Errorf("concurrent EnsureEvidenceSnapshot: %v", failure)
	}
	var snapshotID string
	for result := range results {
		if result.InputRevision != 1 {
			t.Errorf("revision = %d, want 1", result.InputRevision)
		}
		if snapshotID == "" {
			snapshotID = result.ID
		} else if result.ID != snapshotID {
			t.Errorf("snapshot id = %q, want %q", result.ID, snapshotID)
		}
	}
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM evaluation_evidence_snapshots
	`).Scan(&count); err != nil {
		t.Fatalf("count EvidenceSnapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("snapshot count = %d, want 1", count)
	}
}

func TestPostgresEvidenceSnapshotFencesConcurrentQuestionInsert(
	t *testing.T,
) {
	pool := evidenceSnapshotDatabase(t)
	insertEvaluationUsers(t, pool, testOwnerA)
	command := validEvidenceSnapshotCommand()
	installEvidenceAuthorities(t, pool, command)
	enteredFence := make(chan struct{})
	releaseFenceSignal := make(chan struct{})
	var releaseOnce sync.Once
	releaseFence := func() {
		releaseOnce.Do(func() {
			close(releaseFenceSignal)
		})
	}
	t.Cleanup(releaseFence)
	repository := NewPostgresRepository(pool)
	repository.afterEvidenceSourceFence = func() {
		close(enteredFence)
		<-releaseFenceSignal
	}
	type ensureResult struct {
		snapshot EvidenceSnapshot
		replayed bool
		err      error
	}
	result := make(chan ensureResult, 1)
	go func() {
		snapshot, replayed, err := repository.EnsureEvidenceSnapshot(
			context.Background(),
			command,
		)
		result <- ensureResult{
			snapshot: snapshot,
			replayed: replayed,
			err:      err,
		}
	}()
	select {
	case <-enteredFence:
	case <-time.After(5 * time.Second):
		t.Fatal("EvidenceSnapshot did not reach its source fence")
	}

	conversationRepository, err := conversationpostgres.New(pool)
	if err != nil {
		t.Fatalf("new Conversation repository: %v", err)
	}
	writer := make(chan error, 1)
	go func() {
		_, saveErr := conversationRepository.SaveQuestion(
			context.Background(),
			practiceinput.Actor{
				UserID:    command.OwnerUserID,
				SessionID: "trusted-session",
			},
			practice.Question{
				ID:                      "question-concurrent",
				SessionID:               command.PracticeSessionID,
				SpeakerParticipantID:    "participant-interviewer",
				AddresseeParticipantIDs: []string{"participant-candidate"},
				ObjectiveID:             "clear_answer",
				Type:                    "PRIMARY",
				Content:                 "What did you learn from that migration?",
				Sequence:                2,
			},
		)
		writer <- saveErr
	}()
	select {
	case saveErr := <-writer:
		t.Fatalf(
			"Question insert bypassed EvidenceSnapshot source fence: %v",
			saveErr,
		)
	case <-time.After(100 * time.Millisecond):
	}

	releaseFence()
	var created EvidenceSnapshot
	select {
	case ensure := <-result:
		if ensure.err != nil {
			t.Fatalf("EnsureEvidenceSnapshot: %v", ensure.err)
		}
		if ensure.replayed {
			t.Fatal("EnsureEvidenceSnapshot unexpectedly replayed")
		}
		created = ensure.snapshot
	case <-time.After(5 * time.Second):
		t.Fatal("EvidenceSnapshot did not commit after releasing source fence")
	}
	select {
	case saveErr := <-writer:
		if saveErr != nil {
			t.Fatalf("save Question after EvidenceSnapshot commit: %v", saveErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Question insert did not resume after EvidenceSnapshot commit")
	}
	var payload evidencePayload
	if err := json.Unmarshal(created.Payload, &payload); err != nil {
		t.Fatalf("decode created EvidenceSnapshot: %v", err)
	}
	if len(payload.OpportunityManifest) != 1 {
		t.Fatalf(
			"snapshot opportunity count = %d, want 1",
			len(payload.OpportunityManifest),
		)
	}
	var questionCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM practice_questions
		WHERE owner_user_id = $1 AND practice_session_id = $2
	`, command.OwnerUserID, command.PracticeSessionID).Scan(
		&questionCount,
	); err != nil {
		t.Fatalf("count Questions after concurrent insert: %v", err)
	}
	if questionCount != 2 {
		t.Fatalf("Question count = %d, want 2", questionCount)
	}
}

func TestEvidenceSnapshotMigrationDownRemovesOwnedSchema(t *testing.T) {
	pool := evidenceSnapshotDatabase(t)
	down, err := migrations.Files.ReadFile(
		"000036_evaluation_evidence_snapshots.down.sql",
	)
	if err != nil {
		t.Fatalf("read EvidenceSnapshot down migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(down)); err != nil {
		t.Fatalf("apply EvidenceSnapshot down migration: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(context.Background(), `
		SELECT to_regclass('evaluation_evidence_snapshots') IS NOT NULL
	`).Scan(&exists); err != nil {
		t.Fatalf("inspect EvidenceSnapshot table: %v", err)
	}
	if exists {
		t.Fatal("evaluation_evidence_snapshots still exists after down")
	}
}

func evidenceSnapshotDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return evaluationDatabase(t)
}

func validEvidenceSnapshotCommand() EnsureEvidenceSnapshotCommand {
	command := EnsureEvidenceSnapshotCommand{
		OwnerUserID:       testOwnerA,
		PracticeSessionID: "practice-session-1",
		Scope:             ScopeSession,
		SceneType:         SceneInterview,
	}
	provisionalPayload := evidenceAuthorityPayloadForID("snapshot_provisional")
	sourceManifestHash, err := evidenceSourceManifestHash(provisionalPayload)
	if err != nil {
		panic(err)
	}
	command.SourceManifestHash = sourceManifestHash
	command.SnapshotID = deriveEvidenceSnapshotID(
		command.OwnerUserID,
		command.PracticeSessionID,
		command.Scope,
		command.SourceManifestHash,
	)
	command.CanonicalPayload = evidenceAuthorityPayloadForID(
		command.SnapshotID,
	)
	return command
}

func evidenceAuthorityPayloadForID(snapshotID string) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(
		evidenceSnapshotPayloadForID(snapshotID),
		&decoded,
	); err != nil {
		panic(err)
	}
	preparation := decoded["practice_context"].(map[string]any)["preparation"].(map[string]any)
	preparation["background_snapshot_hash"] = evidenceTextHash(
		evidenceTestPreparationBackground,
	)
	decoded["practice_context"].(map[string]any)["task_context"].(map[string]any)["focus_areas"] = []any{"clarity"}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		panic(err)
	}
	return encoded
}

func installEvidenceAuthorities(
	t *testing.T,
	pool *pgxpool.Pool,
	command EnsureEvidenceSnapshotCommand,
) {
	t.Helper()
	var payload evidencePayload
	if err := json.Unmarshal(command.CanonicalPayload, &payload); err != nil {
		t.Fatalf("decode Evidence authority payload: %v", err)
	}
	practiceContext := payload.PracticeContext
	switch practiceContext.Preparation.BackgroundSnapshotHash {
	case evidenceTextHash(evidenceTestPreparationBackground):
	default:
		t.Fatalf(
			"unsupported Evidence background hash %q",
			practiceContext.Preparation.BackgroundSnapshotHash,
		)
	}
	snapshotBackground := evidenceTestPreparationBackground
	if practiceContext.PracticeSessionID != command.PracticeSessionID ||
		len(payload.OpportunityManifest) == 0 ||
		len(payload.ConfirmedTurns) == 0 ||
		len(payload.ConfirmedTurns) != len(payload.ProviderLineage.ASR) ||
		len(payload.ConfirmedTurns) != len(payload.EvidenceRefs) {
		t.Fatalf("unsupported Evidence authority fixture: %#v", payload)
	}
	authorityAt := time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC)
	minEffectiveTurns := 1
	coverageCheckpointTurn := 1
	maxEffectiveTurns := len(payload.ConfirmedTurns)
	if practiceContext.SceneModel ==
		string(scene.SceneModelIELTSSpeakingFullMock) {
		minEffectiveTurns = 14
		coverageCheckpointTurn = 14
		maxEffectiveTurns = 14
	}
	fixtureRoleObjectives := evidenceAuthorityRoleObjectives(
		practiceContext.PracticeObjectives,
	)
	sceneSelection := scene.SelectionSnapshot{
		Scene: scene.SceneDefinition{
			ID:               practiceContext.Scene.ID,
			Family:           scene.SceneFamily(practiceContext.SceneFamily),
			Model:            scene.SceneModel(practiceContext.SceneModel),
			Name:             "Evidence fixture scene",
			Version:          practiceContext.Scene.Version,
			Status:           scene.SceneStatusActive,
			TurnPolicyRef:    "evidence.fixture.turn.v1",
			SessionPolicyRef: "evidence.fixture.session.v1",
			Prompt: scene.ScenePrompt{
				PublicSceneBrief: practiceContext.TaskContext.PublicSceneBrief,
				PracticeGoal:     practiceContext.PracticeGoal,
				UserRole:         practiceContext.UserRole,
				AIRole:           practiceContext.FacilitatorRole,
				PersonaSummary:   practiceContext.TaskContext.PersonaSummary,
				FocusAreas: append(
					[]string{},
					practiceContext.TaskContext.FocusAreas...,
				),
				TurnBlueprints: append(
					[]string{},
					practiceContext.TaskBlueprints...,
				),
				SuggestedDurationSeconds: practiceContext.TaskContext.
					SuggestedDurationSeconds,
			},
			Roles: []scene.RoleDefinition{
				{
					ID:                 "fixture-role",
					SceneID:            practiceContext.Scene.ID,
					Type:               "FIXTURE_FACILITATOR",
					DisplayName:        "Fixture facilitator",
					Responsibilities:   "Deliver the frozen fixture tasks.",
					Style:              "Structured",
					PracticeObjectives: fixtureRoleObjectives,
					DisplayOrder:       1,
				},
			},
			PracticeOptions: []scene.PracticeOption{
				{
					ID:      practiceContext.PracticeOption.ID,
					SceneID: practiceContext.Scene.ID,
					Type: scene.PracticeOptionType(
						practiceContext.PracticeOption.Type,
					),
					DisplayName:  "Fixture option",
					DisplayOrder: 1,
				},
				{
					ID:               "fixture-role-focus",
					SceneID:          practiceContext.Scene.ID,
					RoleDefinitionID: "fixture-role",
					Type:             scene.PracticeOptionFocus,
					DisplayName:      "Fixture role focus",
					DisplayOrder:     2,
				},
			},
			DisplayOrder: 1,
		},
		SelectedRoleIDs:  []string{"fixture-role"},
		PracticeOptionID: practiceContext.PracticeOption.ID,
	}
	usesPublishedIELTSScene := practiceContext.Scene.ID ==
		ieltsFullMockSceneID &&
		practiceContext.Scene.Version == ieltsFullMockSceneVersion
	var ieltsAssignment *preparation.IELTSAssignmentSnapshot
	if usesPublishedIELTSScene {
		catalog, err := scene.NewPostgresCatalog(pool)
		if err != nil {
			t.Fatalf("open published Scene catalog: %v", err)
		}
		resolved, err := catalog.ResolveSelection(
			context.Background(),
			ieltsFullMockSceneID,
			ieltsFullMockSceneVersion,
			[]string{"role_ielts_speaking_full_counterpart"},
			"option_ielts_speaking_full_full",
		)
		if err != nil {
			t.Fatalf("resolve published IELTS Scene: %v", err)
		}
		sceneSelection = resolved
		ieltsAssignment = evidenceIELTSFullMockAssignment(
			t,
			sceneSelection.Scene.Prompt.TurnBlueprints,
		)
	}
	sessionSnapshot := practice.SessionSnapshot{
		ID:             practiceContext.SessionSnapshotID,
		SessionID:      command.PracticeSessionID,
		PlanRevision:   practiceContext.PlanRevision,
		SceneFamily:    scene.SceneFamily(practiceContext.SceneFamily),
		SceneModel:     scene.SceneModel(practiceContext.SceneModel),
		SceneSelection: sceneSelection,
		Preparation: preparation.Snapshot{
			ID:                 practiceContext.Preparation.SnapshotID,
			SourceProfileID:    practiceContext.Preparation.SourceProfileID,
			SourceVersion:      practiceContext.Preparation.SourceVersion,
			BackgroundSnapshot: snapshotBackground,
			CreatedAt:          authorityAt,
		},
		Participants: make(
			[]practice.Participant,
			len(practiceContext.Participants),
		),
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: practiceContext.TaskContext.
				SuggestedDurationSeconds,
			MinEffectiveTurns:       minEffectiveTurns,
			MaxEffectiveTurns:       maxEffectiveTurns,
			CoverageCheckpointTurn:  coverageCheckpointTurn,
			MaxFollowUpsPerQuestion: 1,
			EarlyCompletionRule: preparation.
				EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: evidenceAuthorityObjectives(
			practiceContext.PracticeObjectives,
		),
		IELTSAssignment: ieltsAssignment,
		CreatedAt:       authorityAt,
	}
	for index, participant := range practiceContext.Participants {
		subject := practice.SubjectRef{
			Namespace: "speakup.fixture",
			SubjectID: participant.ID,
		}
		if participant.Role == "LEARNER" {
			subject = practice.SubjectRef{
				Namespace: "speakup.user",
				SubjectID: command.OwnerUserID,
			}
		}
		sessionSnapshot.Participants[index] = practice.Participant{
			ID:         participant.ID,
			SessionID:  command.PracticeSessionID,
			Role:       participant.Role,
			SubjectRef: subject,
			Order:      participant.Order,
		}
	}
	completedAt := authorityAt.Add(time.Minute)
	expectedContext, _, _, ok := evidencePracticeContextFromSnapshot(
		command.OwnerUserID,
		practice.Session{
			ID:             command.PracticeSessionID,
			SceneFamily:    sessionSnapshot.SceneFamily,
			SceneModel:     sessionSnapshot.SceneModel,
			SnapshotID:     sessionSnapshot.ID,
			Status:         practice.SessionCompleted,
			Version:        practiceContext.SessionVersion,
			EffectiveTurns: len(payload.ConfirmedTurns),
			StartedAt:      &authorityAt,
			EndedAt:        &completedAt,
			EndReason:      "COMPLETED",
		},
		sessionSnapshot,
	)
	if !ok || !reflect.DeepEqual(expectedContext, practiceContext) {
		expectedDocument, _ := json.Marshal(expectedContext)
		actualDocument, _ := json.Marshal(practiceContext)
		t.Fatalf(
			"Evidence Practice fixture mismatch:\nexpected %s\nactual   %s",
			expectedDocument,
			actualDocument,
		)
	}
	snapshotDocument, err := json.Marshal(sessionSnapshot)
	if err != nil {
		t.Fatalf("encode Practice Session Snapshot fixture: %v", err)
	}
	sceneSelectionDocument, err := json.Marshal(sessionSnapshot.SceneSelection)
	if err != nil {
		t.Fatalf("encode Practice Scene selection fixture: %v", err)
	}
	sessionPolicyDocument, err := json.Marshal(sessionSnapshot.SessionPolicy)
	if err != nil {
		t.Fatalf("encode Practice Session policy fixture: %v", err)
	}
	practiceObjectivesDocument, err := json.Marshal(
		sessionSnapshot.PracticeObjectives,
	)
	if err != nil {
		t.Fatalf("encode Practice focuses fixture: %v", err)
	}
	var ieltsAssignmentDocument any
	if sessionSnapshot.IELTSAssignment != nil {
		encoded, encodeErr := json.Marshal(sessionSnapshot.IELTSAssignment)
		if encodeErr != nil {
			t.Fatalf("encode IELTS assignment fixture: %v", encodeErr)
		}
		ieltsAssignmentDocument = encoded
	}
	preparationSnapshotDocument, err := json.Marshal(
		sessionSnapshot.Preparation,
	)
	if err != nil {
		t.Fatalf("encode Preparation Snapshot fixture: %v", err)
	}
	scenePromptDocument, err := json.Marshal(
		sessionSnapshot.SceneSelection.Scene.Prompt,
	)
	if err != nil {
		t.Fatalf("encode Scene prompt fixture: %v", err)
	}
	databaseRoles := make(
		[]map[string]any,
		len(sessionSnapshot.SceneSelection.Scene.Roles),
	)
	for index, role := range sessionSnapshot.SceneSelection.Scene.Roles {
		databaseRole := map[string]any{
			"role_definition_id": role.ID,
			"scene_id":           role.SceneID,
			"role_type":          role.Type,
			"display_name":       role.DisplayName,
			"responsibilities":   role.Responsibilities,
			"style":              role.Style,
			"practice_objectives": append(
				[]scene.PracticeObjectiveDefinition(nil),
				role.PracticeObjectives...,
			),
			"display_order": role.DisplayOrder,
		}
		if role.VoiceConfigRef != "" {
			databaseRole["voice_config_ref"] = role.VoiceConfigRef
		}
		databaseRoles[index] = databaseRole
	}
	rolesDocument, err := json.Marshal(databaseRoles)
	if err != nil {
		t.Fatalf("encode Scene roles fixture: %v", err)
	}
	optionsDocument, err := json.Marshal([]map[string]any{
		{
			"practice_option_id":   practiceContext.PracticeOption.ID,
			"scene_id":             practiceContext.Scene.ID,
			"practice_option_type": practiceContext.PracticeOption.Type,
			"display_name":         "Fixture option",
			"display_order":        1,
		},
		{
			"practice_option_id":   "fixture-role-focus",
			"scene_id":             practiceContext.Scene.ID,
			"role_definition_id":   "fixture-role",
			"practice_option_type": string(scene.PracticeOptionFocus),
			"display_name":         "Fixture role focus",
			"display_order":        2,
		},
	})
	if err != nil {
		t.Fatalf("encode Scene options fixture: %v", err)
	}
	participantsDocument, err := json.Marshal(sessionSnapshot.Participants)
	if err != nil {
		t.Fatalf("encode Practice participants fixture: %v", err)
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Evidence authority fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		t.Fatalf("defer Evidence authority constraints: %v", err)
	}
	const threadID = "30000000-0000-4000-8000-000000000270"
	const planID = "evidence-plan-fixture"
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_threads (id, owner_user_id)
		VALUES ($1, $2)
	`, threadID, command.OwnerUserID); err != nil {
		t.Fatalf("insert Evidence Agent Thread: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_profiles (
			owner_user_id,
			profile_id,
			background_summary,
			version,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, command.OwnerUserID, practiceContext.Preparation.SourceProfileID,
		snapshotBackground, practiceContext.Preparation.SourceVersion,
		authorityAt); err != nil {
		t.Fatalf("insert Evidence Preparation Profile: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_snapshots (
			owner_user_id,
			snapshot_id,
			source_profile_id,
			source_version,
			background_snapshot,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, command.OwnerUserID, practiceContext.Preparation.SnapshotID,
		practiceContext.Preparation.SourceProfileID,
		practiceContext.Preparation.SourceVersion,
		snapshotBackground, authorityAt); err != nil {
		t.Fatalf("insert Evidence Preparation Snapshot: %v", err)
	}
	if !usesPublishedIELTSScene {
		if _, err := tx.Exec(ctx, `
		INSERT INTO coaching_scenes (scene_id, created_at)
		VALUES ($1, $2)
		`, practiceContext.Scene.ID, authorityAt); err != nil {
			t.Fatalf("insert Evidence Scene identity: %v", err)
		}
		if _, err := tx.Exec(ctx, `
		INSERT INTO coaching_scene_versions (
			scene_id,
			scene_version,
			scene_family,
			scene_model,
			name,
			status,
			turn_policy_ref,
			session_policy_ref,
			prompt,
			roles,
			practice_options,
			display_order,
			created_at
		)
		VALUES (
			$1, $2, $3, $4, 'Evidence fixture scene', 'active',
			'evidence.fixture.turn.v1', 'evidence.fixture.session.v1',
			$5, $6, $7, 1, $8
		)
		`, practiceContext.Scene.ID, practiceContext.Scene.Version,
			practiceContext.SceneFamily, practiceContext.SceneModel,
			scenePromptDocument, rolesDocument, optionsDocument,
			authorityAt); err != nil {
			t.Fatalf("insert Evidence Scene version: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_practice_plans (
			owner_user_id,
			plan_id,
			current_revision,
			status,
			source_thread_id,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, 'ready', $4, $5, $5)
	`, command.OwnerUserID, planID, practiceContext.PlanRevision,
		threadID, authorityAt); err != nil {
		t.Fatalf("insert Evidence Preparation Plan: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO preparation_practice_plan_revisions (
			owner_user_id,
			plan_id,
			revision,
			preparation_snapshot_id,
			preparation_snapshot,
			scene_id,
			scene_version,
			scene_selection,
			session_policy,
			practice_objectives,
			ielts_assignment,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, command.OwnerUserID, planID, practiceContext.PlanRevision,
		practiceContext.Preparation.SnapshotID,
		preparationSnapshotDocument,
		practiceContext.Scene.ID,
		practiceContext.Scene.Version,
		sceneSelectionDocument,
		sessionPolicyDocument,
		practiceObjectivesDocument,
		ieltsAssignmentDocument,
		authorityAt); err != nil {
		t.Fatalf("insert Evidence Preparation Plan revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id,
			session_id,
			plan_id,
			status,
			version,
			effective_turns,
			created_at,
			updated_at,
			started_at,
			completed_at,
			snapshot_id,
			scene_family,
			scene_model,
			end_reason,
			plan_revision
		)
		VALUES (
			$1, $2, $3, 'completed', $4, $5, $6, $6, $6, $7,
			$8, $9, $10, 'COMPLETED', $11
		)
	`, command.OwnerUserID, command.PracticeSessionID, planID,
		practiceContext.SessionVersion, len(payload.ConfirmedTurns),
		authorityAt, completedAt,
		practiceContext.SessionSnapshotID, practiceContext.SceneFamily,
		practiceContext.SceneModel, practiceContext.PlanRevision); err != nil {
		t.Fatalf("insert Evidence Practice Session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_session_snapshots (
			owner_user_id,
			session_id,
			mode,
			target_ids,
			participants,
			turn_limit,
			created_at,
			snapshot_id,
			snapshot_document
		)
		VALUES (
			$1, $2, $3, '[]'::jsonb, $4, $5, $6, $7, $8
		)
	`, command.OwnerUserID, command.PracticeSessionID,
		practiceContext.SceneFamily, participantsDocument,
		len(payload.OpportunityManifest), authorityAt,
		practiceContext.SessionSnapshotID, snapshotDocument); err != nil {
		t.Fatalf("insert Evidence Practice Session Snapshot: %v", err)
	}

	refsByTurnID := make(map[string]evidenceRef, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		refsByTurnID[ref.TurnID] = ref
	}
	lineageByTurnID := make(
		map[string]evidenceASRLineage,
		len(payload.ProviderLineage.ASR),
	)
	for _, lineage := range payload.ProviderLineage.ASR {
		lineageByTurnID[lineage.TurnID] = lineage
	}
	for _, opportunity := range payload.OpportunityManifest {
		if _, err := tx.Exec(ctx, `
			INSERT INTO practice_questions (
				owner_user_id,
				question_id,
				practice_session_id,
				speaker_participant_id,
				addressee_participant_ids,
				objective_id,
				question_type,
				parent_question_id,
				content,
				sequence,
				created_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				nullif($8, ''), $9, $10, $11
			)
		`, command.OwnerUserID, opportunity.QuestionID,
			command.PracticeSessionID,
			opportunity.SpeakerParticipantID,
			opportunity.AddresseeParticipantIDs,
			opportunity.ObjectiveID,
			opportunity.QuestionType,
			opportunity.ParentQuestionID,
			opportunity.QuestionText,
			opportunity.Sequence,
			authorityAt); err != nil {
			t.Fatalf(
				"insert Evidence Conversation Question %d: %v",
				opportunity.Sequence,
				err,
			)
		}
	}
	for _, turn := range payload.ConfirmedTurns {
		opportunity := payload.OpportunityManifest[turn.Sequence-1]
		ref := refsByTurnID[turn.TurnID]
		lineage := lineageByTurnID[turn.TurnID]
		reservationID := fmt.Sprintf(
			"reservation-%d",
			turn.Sequence,
		)
		attemptID := fmt.Sprintf("attempt-%d", turn.Sequence)
		if _, err := tx.Exec(ctx, `
			INSERT INTO practice_transcription_reservations (
				owner_user_id,
				reservation_id,
				question_id,
				practice_session_id,
				idempotency_key,
				input_fingerprint,
				respondent_participant_id,
				status,
				fencing_token,
				deletion_generation,
				lease_expires_at,
				candidate_id,
				current_attempt_id,
				created_at,
				updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, 'completed',
				1, 0, $8, $9, $10, $8, $8
			)
		`, command.OwnerUserID, reservationID,
			opportunity.QuestionID,
			command.PracticeSessionID,
			fmt.Sprintf(
				"fixture-reservation-key-%d",
				turn.Sequence,
			),
			fmt.Sprintf("fixture-input-%d", turn.Sequence),
			turn.RespondentParticipantID,
			authorityAt,
			ref.Lineage.CandidateID,
			attemptID); err != nil {
			t.Fatalf(
				"insert Evidence transcription reservation %d: %v",
				turn.Sequence,
				err,
			)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO practice_transcript_candidates (
				owner_user_id,
				candidate_id,
				reservation_id,
				question_id,
				practice_session_id,
				respondent_participant_id,
				transcript_id,
				evidence_version,
				provider,
				model,
				provider_request_id,
				transcript_text,
				status,
				created_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, 'confirmed', $13
			)
		`, command.OwnerUserID, ref.Lineage.CandidateID,
			reservationID, opportunity.QuestionID,
			command.PracticeSessionID,
			turn.RespondentParticipantID,
			turn.Transcript.ID,
			turn.Transcript.EvidenceVersion,
			lineage.Provider,
			lineage.Model,
			lineage.ProviderRequestID,
			turn.Transcript.Text,
			authorityAt); err != nil {
			t.Fatalf(
				"insert Evidence transcript candidate %d: %v",
				turn.Sequence,
				err,
			)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO practice_turns (
				owner_user_id,
				turn_id,
				candidate_id,
				question_id,
				practice_session_id,
				speaker_participant_id,
				addressee_participant_ids,
				respondent_participant_id,
				sequence,
				interaction_mode,
				answer_text,
				evidence_version,
				effective_turns,
				session_completed,
				progress_recorded_at,
				confirmed_at,
				created_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9,
				$10, $11, $12, $9, $13, $14, $14, $14
			)
		`, command.OwnerUserID, turn.TurnID,
			ref.Lineage.CandidateID,
			opportunity.QuestionID,
			command.PracticeSessionID,
			opportunity.SpeakerParticipantID,
			opportunity.AddresseeParticipantIDs,
			turn.RespondentParticipantID,
			turn.Sequence,
			turn.InteractionMode,
			turn.Transcript.Text,
			turn.Transcript.EvidenceVersion,
			turn.Sequence == len(payload.ConfirmedTurns),
			authorityAt); err != nil {
			t.Fatalf(
				"insert Evidence Confirmed Turn %d: %v",
				turn.Sequence,
				err,
			)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit Evidence authority fixture: %v", err)
	}
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Evidence authority probe: %v", err)
	}
	defer func() { _ = probe.Rollback(ctx) }()
	for _, opportunity := range payload.OpportunityManifest {
		if err := lockEvidenceQuestion(
			ctx,
			probe,
			command,
			opportunity,
		); err != nil {
			t.Fatalf(
				"probe Evidence Question authority %d: %v",
				opportunity.Sequence,
				err,
			)
		}
	}
	for _, turn := range payload.ConfirmedTurns {
		opportunity := payload.OpportunityManifest[turn.Sequence-1]
		if err := lockEvidenceTurn(
			ctx,
			probe,
			command,
			turn,
			refsByTurnID[turn.TurnID],
			lineageByTurnID[turn.TurnID],
			opportunity,
		); err != nil {
			t.Fatalf(
				"probe Evidence Turn authority %d: %v",
				turn.Sequence,
				err,
			)
		}
	}
	if err := lockCurrentEvidenceSources(ctx, probe, command); err != nil {
		t.Fatalf("probe complete Evidence authority: %v", err)
	}
}

func evidenceAuthorityObjectives(
	source []evidenceObjective,
) []preparation.PracticeObjective {
	result := make([]preparation.PracticeObjective, len(source))
	for index, objective := range source {
		result[index] = preparation.PracticeObjective{
			ID:          objective.ID,
			Description: objective.Description,
		}
	}
	return result
}

func evidenceIELTSFullMockAssignment(
	t *testing.T,
	turnBlueprints []string,
) *preparation.IELTSAssignmentSnapshot {
	t.Helper()
	const (
		part1Questions = 8
		part2Questions = 1
		part1Prefix    = "Part 1 question: "
		part2Prefix    = "Part 2 cue card: "
		part3Prefix    = "Part 3 question: "
	)
	part3Questions := len(turnBlueprints) - part1Questions - part2Questions
	if part3Questions < 1 || part3Questions > 5 {
		t.Fatalf(
			"IELTS Evidence fixture has %d Part 3 questions",
			part3Questions,
		)
	}
	for index := range part1Questions {
		if !strings.HasPrefix(turnBlueprints[index], part1Prefix) {
			t.Fatalf("IELTS Evidence fixture task %d is not Part 1", index+1)
		}
	}
	part2CueCard, found := strings.CutPrefix(
		turnBlueprints[part1Questions],
		part2Prefix,
	)
	if !found || strings.TrimSpace(part2CueCard) != part2CueCard ||
		part2CueCard == "" {
		t.Fatal("IELTS Evidence fixture has no valid Part 2 cue card")
	}
	for index := part1Questions + part2Questions; index < len(turnBlueprints); index++ {
		if !strings.HasPrefix(turnBlueprints[index], part3Prefix) {
			t.Fatalf("IELTS Evidence fixture task %d is not Part 3", index+1)
		}
	}
	return &preparation.IELTSAssignmentSnapshot{
		BankID:         "evidence-ielts-bank-v1",
		Season:         "Evaluation fixture 2026",
		Mode:           scene.IELTSPracticeModeFullMock,
		Part1SetID:     "evidence-part1-hometown-ai-free-time",
		TopicGroupID:   "evidence-topic-learning-skill",
		TopicTitle:     "Learning a skill",
		Part2CueCard:   part2CueCard,
		Part1Questions: part1Questions,
		Part2Questions: part2Questions,
		Part3Questions: part3Questions,
		TurnBlueprints: append([]string(nil), turnBlueprints...),
	}
}

func evidenceAuthorityRoleObjectives(
	source []evidenceObjective,
) []scene.PracticeObjectiveDefinition {
	result := make([]scene.PracticeObjectiveDefinition, len(source))
	for index, objective := range source {
		result[index] = scene.PracticeObjectiveDefinition{
			ID:          objective.ID,
			Description: objective.Description,
		}
	}
	return result
}

func replaceEvidencePayloadTranscript(
	t *testing.T,
	payload json.RawMessage,
	transcript string,
) json.RawMessage {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode EvidenceSnapshot payload: %v", err)
	}
	turns := decoded["confirmed_turns"].([]any)
	turns[0].(map[string]any)["transcript"].(map[string]any)["text"] =
		transcript
	refs := decoded["evidence_refs"].([]any)
	refs[0].(map[string]any)["transcript_span"].(map[string]any)["end_utf8_byte"] = len([]byte(transcript))
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode EvidenceSnapshot payload: %v", err)
	}
	return encoded
}

func replaceEvidencePayloadSnapshotID(
	t *testing.T,
	payload json.RawMessage,
	snapshotID string,
) json.RawMessage {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode EvidenceSnapshot payload: %v", err)
	}
	turnsByID := make(map[string]map[string]any)
	for _, value := range decoded["confirmed_turns"].([]any) {
		turn := value.(map[string]any)
		turnsByID[turn["turn_id"].(string)] = turn
	}
	for _, value := range decoded["evidence_refs"].([]any) {
		ref := value.(map[string]any)
		turn := turnsByID[ref["turn_id"].(string)]
		transcript := turn["transcript"].(map[string]any)
		audio := turn["audio"].(map[string]any)
		checksum, _ := audio["checksum_sha256"].(string)
		ref["snapshot_id"] = snapshotID
		ref["evidence_ref_id"] = stableEvidenceRefID(
			snapshotID,
			ref["turn_id"].(string),
			int64(transcript["evidence_version"].(float64)),
			checksum,
		)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode EvidenceSnapshot payload: %v", err)
	}
	return encoded
}
