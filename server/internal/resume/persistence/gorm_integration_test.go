// 本文件在隔离 PostgreSQL Schema 中验证 Resume GORM Repository 的真实事务语义。
package persistence_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/persistence"
)

const (
	resumeOwnerA = "10000000-0000-4000-8000-000000000001"
	resumeOwnerB = "20000000-0000-4000-8000-000000000002"
)

// TestGormRepositoryCRUDAndIsolation 验证 CRUD、所有权隔离、乐观锁和手动修订。
func TestGormRepositoryCRUDAndIsolation(t *testing.T) {
	repository := newResumeRepository(t)
	ctx := context.Background()
	item := resumeCandidate("30000000-0000-4000-8000-000000000001", resumeOwnerA)
	if err := repository.CreateWithinLimit(ctx, item, app.MaxResumesPerUser); err != nil {
		t.Fatalf("create Resume: %v", err)
	}

	if _, err := repository.FindByOwnerAndID(ctx, resumeOwnerB, item.ID); errorCode(err) != "resume_not_found" {
		t.Fatalf("cross-owner read error = %v", err)
	}
	listed, err := repository.ListByOwner(ctx, resumeOwnerA, app.ListQuery{Limit: 3})
	if err != nil || len(listed) != 1 || listed[0].ID != item.ID {
		t.Fatalf("list = %#v, err = %v", listed, err)
	}

	updated, err := repository.UpdateMetadata(ctx, resumeOwnerA, app.UpdateMetadataCommand{
		ResumeID: item.ID, Title: "Primary Resume", ExpectedVersion: 1,
	})
	if err != nil || updated.Title != "Primary Resume" || updated.Version != 2 {
		t.Fatalf("update = %#v, err = %v", updated, err)
	}
	if _, err := repository.UpdateMetadata(ctx, resumeOwnerA, app.UpdateMetadataCommand{
		ResumeID: item.ID, Title: "Stale", ExpectedVersion: 1,
	}); errorCode(err) != "resume_version_conflict" {
		t.Fatalf("stale update error = %v", err)
	}

	if err := repository.MarkFileAvailableAndQueueParse(ctx, resumeOwnerA, item.ID); err != nil {
		t.Fatalf("mark available: %v", err)
	}
	current, err := repository.FindByOwnerAndID(ctx, resumeOwnerA, item.ID)
	if err != nil {
		t.Fatalf("find current: %v", err)
	}
	revision, err := repository.SaveManualRevision(ctx, resumeOwnerA, app.UpdateContentCommand{
		ResumeID: item.ID,
		Content: resume.Content{
			TargetPosition: "Backend Engineer",
			Skills:         []string{"Go", "PostgreSQL"},
		},
		ExpectedVersion: current.Version,
	})
	if err != nil || revision.Revision != 1 || revision.Source != resume.RevisionManual {
		t.Fatalf("manual revision = %#v, err = %v", revision, err)
	}
	detail, err := repository.FindDetailByOwnerAndID(ctx, resumeOwnerA, item.ID)
	if err != nil || detail.Revision == nil || detail.Revision.Content.TargetPosition != "Backend Engineer" {
		t.Fatalf("detail = %#v, err = %v", detail, err)
	}

	aborted := resumeCandidate("30000000-0000-4000-8000-000000000002", resumeOwnerA)
	if err := repository.CreateWithinLimit(ctx, aborted, app.MaxResumesPerUser); err != nil {
		t.Fatalf("create aborted Resume: %v", err)
	}
	if err := repository.AbortCreate(ctx, resumeOwnerA, aborted.ID); err != nil {
		t.Fatalf("abort create: %v", err)
	}
	if _, err := repository.FindByOwnerAndID(ctx, resumeOwnerA, aborted.ID); errorCode(err) != "resume_not_found" {
		t.Fatalf("aborted Resume remains visible: %v", err)
	}
}

// TestGormRepositoryConcurrentLimitAndCapacityRecovery 验证并发创建不突破三份上限且删除后释放容量。
func TestGormRepositoryConcurrentLimitAndCapacityRecovery(t *testing.T) {
	repository := newResumeRepository(t)
	ctx := context.Background()
	ids := []string{
		"40000000-0000-4000-8000-000000000001",
		"40000000-0000-4000-8000-000000000002",
		"40000000-0000-4000-8000-000000000003",
		"40000000-0000-4000-8000-000000000004",
	}
	var wait sync.WaitGroup
	errorsByID := make(map[string]error, len(ids))
	var mutex sync.Mutex
	for _, id := range ids {
		id := id
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := repository.CreateWithinLimit(ctx, resumeCandidate(id, resumeOwnerA), app.MaxResumesPerUser)
			mutex.Lock()
			errorsByID[id] = err
			mutex.Unlock()
		}()
	}
	wait.Wait()
	succeeded := 0
	limited := 0
	for _, err := range errorsByID {
		switch errorCode(err) {
		case "":
			succeeded++
		case "resume_limit_exceeded":
			limited++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if succeeded != 3 || limited != 1 {
		t.Fatalf("succeeded = %d, limited = %d", succeeded, limited)
	}

	items, err := repository.ListByOwner(ctx, resumeOwnerA, app.ListQuery{Limit: 3})
	if err != nil || len(items) != 3 {
		t.Fatalf("list after concurrent creates = %#v, %v", items, err)
	}
	deleting, err := repository.MarkDeleting(ctx, resumeOwnerA, app.DeleteCommand{
		ResumeID: items[0].ID, ExpectedVersion: items[0].Version,
	})
	if err != nil {
		t.Fatalf("mark deleting: %v", err)
	}
	if err := repository.MarkDeleted(ctx, resumeOwnerA, deleting.ID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	newID := "40000000-0000-4000-8000-000000000005"
	if err := repository.CreateWithinLimit(ctx, resumeCandidate(newID, resumeOwnerA), app.MaxResumesPerUser); err != nil {
		t.Fatalf("create after deletion: %v", err)
	}
}

// TestTemporaryResumeDoesNotConsumeSavedQuota verifies the interview-only
// resource stays out of both the saved list and its three-item quota.
func TestTemporaryResumeDoesNotConsumeSavedQuota(t *testing.T) {
	repository := newResumeRepository(t)
	ctx := context.Background()
	for index, id := range []string{
		"41000000-0000-4000-8000-000000000001",
		"41000000-0000-4000-8000-000000000002",
		"41000000-0000-4000-8000-000000000003",
	} {
		if err := repository.CreateWithinLimit(
			ctx,
			resumeCandidate(id, resumeOwnerA),
			app.MaxResumesPerUser,
		); err != nil {
			t.Fatalf("create saved Resume %d: %v", index+1, err)
		}
	}

	temporary := resumeCandidate(
		"41000000-0000-4000-8000-000000000004",
		resumeOwnerA,
	)
	temporary.Temporary = true
	expiresAt := temporary.CreatedAt.Add(app.TemporaryResumeLifetime)
	temporary.ExpiresAt = &expiresAt
	if err := repository.CreateWithinLimit(
		ctx,
		temporary,
		app.MaxResumesPerUser,
	); err != nil {
		t.Fatalf("create temporary Resume at saved quota: %v", err)
	}

	items, err := repository.ListByOwner(
		ctx,
		resumeOwnerA,
		app.ListQuery{Limit: app.MaxResumesPerUser},
	)
	if err != nil || len(items) != app.MaxResumesPerUser {
		t.Fatalf("saved list = %#v, err = %v", items, err)
	}
	for _, item := range items {
		if item.Temporary || item.ID == temporary.ID {
			t.Fatalf("temporary Resume leaked into saved list: %#v", item)
		}
	}

	fourthSaved := resumeCandidate(
		"41000000-0000-4000-8000-000000000005",
		resumeOwnerA,
	)
	if err := repository.CreateWithinLimit(
		ctx,
		fourthSaved,
		app.MaxResumesPerUser,
	); errorCode(err) != "resume_limit_exceeded" {
		t.Fatalf("fourth saved Resume error = %v", err)
	}
}

// TestGormRepositoryParseCompletionAndFailure 验证解析领取、完成、失败和重试状态迁移。
func TestGormRepositoryParseCompletionAndFailure(t *testing.T) {
	repository := newResumeRepository(t)
	ctx := context.Background()
	first := resumeCandidate("50000000-0000-4000-8000-000000000001", resumeOwnerA)
	if err := repository.CreateWithinLimit(ctx, first, 3); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := repository.MarkFileAvailableAndQueueParse(ctx, resumeOwnerA, first.ID); err != nil {
		t.Fatalf("queue first: %v", err)
	}
	claimed, found, err := repository.ClaimNextQueuedParse(ctx)
	if err != nil || !found || claimed.ID != first.ID || claimed.ParseStatus != resume.ParseRunning {
		t.Fatalf("claim = %#v, found = %v, err = %v", claimed, found, err)
	}
	if err := repository.CompleteParse(ctx, claimed, resume.Revision{
		Source:        resume.RevisionParser,
		ParserVersion: "resume-parser/v1",
		Content:       resume.Content{TargetPosition: "Platform Engineer"},
	}); err != nil {
		t.Fatalf("complete parse: %v", err)
	}
	detail, err := repository.FindDetailByOwnerAndID(ctx, resumeOwnerA, first.ID)
	if err != nil || detail.Resume.ParseStatus != resume.ParseReady ||
		detail.Revision == nil || detail.Revision.Source != resume.RevisionParser {
		t.Fatalf("parsed detail = %#v, err = %v", detail, err)
	}

	second := resumeCandidate("50000000-0000-4000-8000-000000000002", resumeOwnerA)
	if err := repository.CreateWithinLimit(ctx, second, 3); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := repository.MarkFileAvailableAndQueueParse(ctx, resumeOwnerA, second.ID); err != nil {
		t.Fatalf("queue second: %v", err)
	}
	failedClaim, found, err := repository.ClaimNextQueuedParse(ctx)
	if err != nil || !found || failedClaim.ID != second.ID {
		t.Fatalf("failed claim = %#v, found = %v, err = %v", failedClaim, found, err)
	}
	if err := repository.FailParse(ctx, failedClaim, "pdf_text_unavailable"); err != nil {
		t.Fatalf("fail parse: %v", err)
	}
	failed, err := repository.FindByOwnerAndID(ctx, resumeOwnerA, second.ID)
	if err != nil || failed.ParseStatus != resume.ParseFailed || failed.ParseFailureCode != "pdf_text_unavailable" {
		t.Fatalf("failed Resume = %#v, err = %v", failed, err)
	}
	if err := repository.QueueParse(ctx, resumeOwnerA, second.ID); err != nil {
		t.Fatalf("retry parse: %v", err)
	}
}

// TestGormRepositoryManualEditWinsConcurrentParse 验证解析期间的手动内容不会被稍后完成的解析结果覆盖。
func TestGormRepositoryManualEditWinsConcurrentParse(t *testing.T) {
	repository := newResumeRepository(t)
	ctx := context.Background()
	item := resumeCandidate("60000000-0000-4000-8000-000000000001", resumeOwnerA)
	if err := repository.CreateWithinLimit(ctx, item, app.MaxResumesPerUser); err != nil {
		t.Fatalf("create Resume: %v", err)
	}
	if err := repository.MarkFileAvailableAndQueueParse(ctx, resumeOwnerA, item.ID); err != nil {
		t.Fatalf("queue Resume: %v", err)
	}
	claimed, found, err := repository.ClaimNextQueuedParse(ctx)
	if err != nil || !found {
		t.Fatalf("claim = %#v, found = %v, err = %v", claimed, found, err)
	}
	manual, err := repository.SaveManualRevision(ctx, resumeOwnerA, app.UpdateContentCommand{
		ResumeID: item.ID,
		Content: resume.Content{
			TargetPosition: "User Edited Position",
		},
		ExpectedVersion: claimed.Version,
	})
	if err != nil {
		t.Fatalf("save manual revision: %v", err)
	}
	if err := repository.CompleteParse(ctx, claimed, resume.Revision{
		Source:        resume.RevisionParser,
		ParserVersion: "resume-parser/v1",
		Content:       resume.Content{TargetPosition: "Parsed Position"},
	}); err != nil {
		t.Fatalf("complete concurrent parse: %v", err)
	}
	detail, err := repository.FindDetailByOwnerAndID(ctx, resumeOwnerA, item.ID)
	if err != nil || detail.Resume.ParseStatus != resume.ParseReady || detail.Revision == nil ||
		detail.Revision.Revision != manual.Revision ||
		detail.Revision.Content.TargetPosition != "User Edited Position" {
		t.Fatalf("detail after concurrent parse = %#v, err = %v", detail, err)
	}
}

// resumeCandidate 创建满足数据库约束的测试简历。
func resumeCandidate(id string, ownerID string) resume.Resume {
	return resume.Resume{
		ID:               id,
		OwnerUserID:      ownerID,
		Title:            "Backend Resume",
		OriginalFilename: "backend.pdf",
		ContentType:      "application/pdf",
		SizeBytes:        1024,
		ChecksumSHA256:   strings.Repeat("a", 64),
		ObjectKey:        "resume/v1/" + id + ".pdf",
		FileStatus:       resume.FileUploading,
		ParseStatus:      resume.ParseQueued,
		Version:          1,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
}

// errorCode 返回公共应用错误的稳定机器码。
func errorCode(err error) string {
	if err == nil {
		return ""
	}
	applicationError, ok := apperror.From(err)
	if !ok {
		return "unknown"
	}
	return applicationError.Code()
}

// newResumeRepository 创建带完整迁移和测试身份的隔离 GORM Repository。
func newResumeRepository(t *testing.T) *persistence.GormRepository {
	t.Helper()
	pool := newResumeTestPool(t)
	repository, err := persistence.NewGormRepositoryFromPool(pool)
	if err != nil {
		t.Fatalf("new Resume Repository: %v", err)
	}
	return repository
}

// newResumeTestPool 创建一个仅供当前测试使用的 PostgreSQL Schema 和连接池。
func newResumeTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	admin, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal("connect to TEST_DATABASE_URL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "resume_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create Resume schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop Resume schema: %v", err)
		}
	})
	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse database URL")
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()
	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal("parse scoped pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal("open scoped pool")
	}
	t.Cleanup(pool.Close)
	for _, user := range []struct {
		id    string
		email string
	}{
		{id: resumeOwnerA, email: "resume-a@example.com"},
		{id: resumeOwnerB, email: "resume-b@example.com"},
	} {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO identity_users (id, canonical_email)
VALUES ($1, $2)`, user.id, user.email); err != nil {
			t.Fatalf("insert Resume owner: %v", err)
		}
	}
	return pool
}
