package ielts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestPostgresAnswerPreparationsEnforceIdentityIdempotencyVersionAndCleanup(t *testing.T) {
	pool := answerPreparationTestDatabase(t)
	ctx := context.Background()
	owner := requestcontext.Actor{UserID: "11111111-1111-4111-8111-111111111111", SessionID: "session-owner"}
	other := requestcontext.Actor{UserID: "22222222-2222-4222-8222-222222222222", SessionID: "session-other"}
	for _, row := range []struct{ id, email string }{{owner.UserID, "owner@example.test"}, {other.UserID, "other@example.test"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO identity_users(id, canonical_email) VALUES($1,$2)`, row.id, row.email); err != nil {
			t.Fatalf("insert identity: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ielts_question_bank_versions(bank_id,schema_version,season_code,season_label,season_start,season_end,region,source_cutoff) VALUES('bank-2026',3,'2026-05-08','season','2026-05-01','2026-08-31','mainland',now()); INSERT INTO ielts_part1_topics(bank_id,topic_id,title_zh,title_en,release_status,display_order) VALUES('bank-2026','p1-topic-001','音乐','Music','new',1); INSERT INTO ielts_part1_questions(bank_id,topic_id,question_position,prompt) VALUES('bank-2026','p1-topic-001',1,'Do you enjoy music?'),('bank-2026','p1-topic-001',2,'Do you play music?'); UPDATE ielts_question_bank_versions SET status='published',published_at=now() WHERE bank_id='bank-2026'`); err != nil {
		t.Fatalf("insert question: %v", err)
	}
	store, _ := NewPostgresStore(pool)
	service, _ := NewAnswerPreparationService(store, store, &answerGeneratorStub{}, answerIDStub{})
	request := CreateAnswerPreparationRequest{Question: QuestionReference{BankID: "bank-2026", Part: PracticeModePart1, SourceID: "p1-topic-001", QuestionPosition: 1}, PersonalPoints: []string{"I play piano"}, TargetBand: 6.5}
	created, replayed, err := service.Create(ctx, owner, "create-key-1", request)
	if err != nil || replayed {
		t.Fatalf("Create = %#v replay=%t err=%v", created, replayed, err)
	}
	restored, replayed, err := service.Create(ctx, owner, "create-key-1", request)
	if err != nil || !replayed || restored.ID != created.ID {
		t.Fatalf("replay = %#v replay=%t err=%v", restored, replayed, err)
	}
	existingRequest := request
	existingRequest.PersonalPoints = nil
	existing, replayed, err := service.Create(ctx, owner, "create-key-existing", existingRequest)
	if err != nil || !replayed || existing.ID != created.ID || existing.PersonalPoints[0] != "I play piano" {
		t.Fatalf("existing = %#v replay=%t err=%v", existing, replayed, err)
	}
	concurrentService, _ := NewAnswerPreparationService(store, store, &answerGeneratorStub{}, SecureAnswerPreparationIDGenerator{})
	concurrentRequest := request
	concurrentRequest.Question.QuestionPosition = 2
	type createResult struct {
		value    AnswerPreparation
		replayed bool
		err      error
	}
	results := make([]createResult, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			results[index].value, results[index].replayed, results[index].err = concurrentService.Create(ctx, owner, fmt.Sprintf("create-key-concurrent-%d", index), concurrentRequest)
		}(index)
	}
	close(start)
	group.Wait()
	if results[0].err != nil || results[1].err != nil || results[0].value.ID != results[1].value.ID || results[0].replayed == results[1].replayed {
		t.Fatalf("concurrent Create = %#v", results)
	}
	changed := request
	changed.TargetBand = 7
	if _, _, err := service.Create(ctx, owner, "create-key-1", changed); !errors.Is(err, ErrAnswerPreparationIdempotencyConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
	if _, err := service.Get(ctx, other, created.ID); !errors.Is(err, ErrAnswerPreparationNotFound) {
		t.Fatalf("foreign Get err=%v", err)
	}
	updated, _, err := service.Update(ctx, owner, created.ID, "update-key-1", UpdateAnswerPreparationRequest{ExpectedVersion: created.Version, PersonalPoints: []string{"I study piano"}, TargetBand: 7})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, _, err := service.Update(ctx, owner, created.ID, "update-key-stale", UpdateAnswerPreparationRequest{ExpectedVersion: created.Version, PersonalPoints: []string{"stale"}, TargetBand: 7}); !errors.Is(err, ErrAnswerPreparationConflict) {
		t.Fatalf("stale Update err=%v", err)
	}
	ready, _, err := service.Generate(ctx, owner, created.ID, "generate-key-1", GenerateAnswerPreparationRequest{ExpectedVersion: updated.Version})
	if err != nil || ready.Status != AnswerPreparationReady {
		t.Fatalf("Generate = %#v err=%v", ready, err)
	}
	if _, err := service.Get(ctx, other, created.ID); !errors.Is(err, ErrAnswerPreparationNotFound) {
		t.Fatalf("foreign ready Get err=%v", err)
	}
	if _, err := service.Delete(ctx, other, created.ID, "delete-key-other", ready.Version); !errors.Is(err, ErrAnswerPreparationNotFound) {
		t.Fatalf("foreign Delete err=%v", err)
	}
	if _, err := service.Delete(ctx, owner, created.ID, "delete-key-1", ready.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := service.Delete(ctx, owner, created.ID, "delete-key-1", ready.Version); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if _, err := service.Delete(ctx, owner, results[0].value.ID, "delete-key-concurrent", results[0].value.Version); err != nil {
		t.Fatalf("delete concurrent Create result: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ielts_answer_preparation_idempotency WHERE owner_user_id=$1 AND resource_id IS NOT NULL`, owner.UserID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("resource-bound idempotency count=%d err=%v", count, err)
	}
}

func TestPostgresAnswerQuestionResolverKeepsBankVersionInLocator(t *testing.T) {
	pool := answerPreparationTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO ielts_question_bank_versions(bank_id,schema_version,season_code,season_label,season_start,season_end,region,source_cutoff) VALUES('bank-old',3,'old','old','2026-01-01','2026-04-30','mainland',now()),('bank-new',3,'new','new','2026-05-01','2026-08-31','mainland',now()); INSERT INTO ielts_part1_topics(bank_id,topic_id,title_zh,title_en,release_status,display_order) VALUES('bank-old','topic','旧','Old','new',1),('bank-new','topic','新','New','new',1); INSERT INTO ielts_part1_questions(bank_id,topic_id,question_position,prompt) VALUES('bank-old','topic',1,'Old prompt?'),('bank-new','topic',1,'New prompt?'); INSERT INTO ielts_part23_groups(bank_id,topic_group_id,title_zh,release_status,cue_card_type,cue_card_prompt,cue_card_points,display_order) VALUES('bank-new','group','地点','new','place','Describe a place.','["where it is","why you like it","when you visit"]',1); INSERT INTO ielts_part3_questions(bank_id,topic_group_id,question_position,prompt) VALUES('bank-new','group',1,'Why do people like public places?'); UPDATE ielts_question_bank_versions SET status='published',published_at=now() WHERE bank_id='bank-old'; UPDATE ielts_question_bank_versions SET status='retired',retired_at=now() WHERE bank_id='bank-old'; UPDATE ielts_question_bank_versions SET status='published',published_at=now() WHERE bank_id='bank-new'`); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	store, _ := NewPostgresStore(pool)
	old, err := store.ResolveAnswerQuestion(ctx, QuestionReference{BankID: "bank-old", Part: PracticeModePart1, SourceID: "topic", QuestionPosition: 1})
	if err != nil || old.Prompt != "Old prompt?" {
		t.Fatalf("old=%#v err=%v", old, err)
	}
	newer, err := store.ResolveAnswerQuestion(ctx, QuestionReference{BankID: "bank-new", Part: PracticeModePart1, SourceID: "topic", QuestionPosition: 1})
	if err != nil || newer.Prompt != "New prompt?" {
		t.Fatalf("new=%#v err=%v", newer, err)
	}
	part2, err := store.ResolveAnswerQuestion(ctx, QuestionReference{BankID: "bank-new", Part: PracticeModePart2, SourceID: "group", QuestionPosition: 1})
	if err != nil || !strings.Contains(part2.Prompt, "You should say:\n• where it is") {
		t.Fatalf("Part 2=%#v err=%v", part2, err)
	}
	part3, err := store.ResolveAnswerQuestion(ctx, QuestionReference{BankID: "bank-new", Part: PracticeModePart3, SourceID: "group", QuestionPosition: 1})
	if err != nil || part3.Prompt != "Why do people like public places?" {
		t.Fatalf("Part 3=%#v err=%v", part3, err)
	}
}

func TestPostgresAnswerPreparationAccountCleanupRejectsActiveAndRemovesDeletingOwner(t *testing.T) {
	pool := answerPreparationTestDatabase(t)
	ctx := context.Background()
	userID := "33333333-3333-4333-8333-333333333333"
	if _, err := pool.Exec(ctx, `INSERT INTO identity_users(id,canonical_email) VALUES($1,'cleanup@example.test')`, userID); err != nil {
		t.Fatal(err)
	}
	store, _ := NewPostgresStore(pool)
	if err := store.DeleteUserAnswerPreparations(ctx, userID); !errors.Is(err, ErrAnswerPreparationConflict) {
		t.Fatalf("active cleanup err=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_users SET account_status='deleting' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUserAnswerPreparations(ctx, userID); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if err := store.DeleteUserAnswerPreparations(ctx, userID); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func answerPreparationTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "ielts_answer_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
	})
	for _, name := range []string{"000002_identity_schema.up.sql", "000080_ielts_versioned_question_bank.up.sql", "000084_ielts_answer_preparations.up.sql"} {
		content, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}
