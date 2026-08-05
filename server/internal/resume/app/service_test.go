// 本文件验证 Resume Service 补偿、幂等、文件生命周期和解析 Worker。
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

var serviceTestActor = requestcontext.Actor{
	UserID: "10000000-0000-4000-8000-000000000001", SessionID: "session-1",
}

// TestServiceCreateIsDurableAndReplayable 验证上传成功后入队且同一幂等请求返回原资源。
func TestServiceCreateIsDurableAndReplayable(t *testing.T) {
	repository := &repositoryFake{}
	files := newFileStorageFake()
	service := newTestService(t, repository, files)
	body := []byte("%PDF-1.4 application service test")
	command := createTestCommand(body)

	created, err := service.Create(context.Background(), serviceTestActor, command)
	if err != nil || created.FileStatus != resume.FileAvailable ||
		created.ParseStatus != resume.ParseQueued || created.Version != 2 {
		t.Fatalf("created = %#v, err = %v", created, err)
	}
	replayed, err := service.Create(context.Background(), serviceTestActor, createTestCommand(body))
	if err != nil || replayed.ID != created.ID || len(files.objects) != 1 {
		t.Fatalf("replayed = %#v, objects = %d, err = %v", replayed, len(files.objects), err)
	}
}

// TestServiceCreateCompensatesFailedUpload 验证对象保存失败会硬删除 UPLOADING 记录。
func TestServiceCreateCompensatesFailedUpload(t *testing.T) {
	repository := &repositoryFake{}
	files := newFileStorageFake()
	files.putErr = errors.New("storage unavailable")
	service := newTestService(t, repository, files)
	body := []byte("%PDF-1.4 failed upload")

	if _, err := service.Create(context.Background(), serviceTestActor, createTestCommand(body)); err == nil {
		t.Fatal("Create error = nil")
	}
	if !repository.abortCalled || repository.item.ID != "" {
		t.Fatalf("abortCalled = %v, item = %#v", repository.abortCalled, repository.item)
	}
}

// TestServiceListBuildsStableNextCursor 验证三份上限内的小页查询仍能返回真实下一游标。
func TestServiceListBuildsStableNextCursor(t *testing.T) {
	repository := &repositoryFake{listItems: []resume.Resume{
		{ID: "50000000-0000-4000-8000-000000000003"},
		{ID: "50000000-0000-4000-8000-000000000002"},
		{ID: "50000000-0000-4000-8000-000000000001"},
	}}
	service := newTestService(t, repository, newFileStorageFake())
	page, err := service.List(context.Background(), serviceTestActor, ListQuery{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor != page.Items[1].ID {
		t.Fatalf("page = %#v, err = %v", page, err)
	}
}

// TestServiceGetNormalizesLegacyAwards 验证旧 Revision 缺少 awards 时公开响应仍返回空数组。
func TestServiceGetNormalizesLegacyAwards(t *testing.T) {
	item := resume.Resume{
		ID: "50000000-0000-4000-8000-000000000004", OwnerUserID: serviceTestActor.UserID,
	}
	revision := &resume.Revision{Content: resume.Content{
		WorkExperiences:      []resume.WorkExperience{},
		ProjectExperiences:   []resume.ProjectExperience{},
		EducationExperiences: []resume.EducationExperience{},
		Skills:               []string{},
	}}
	repository := &repositoryFake{item: item, detailRevision: revision}
	service := newTestService(t, repository, newFileStorageFake())

	detail, err := service.Get(context.Background(), serviceTestActor, item.ID)
	if err != nil || detail.Revision == nil || detail.Revision.Content.Awards == nil ||
		len(detail.Revision.Content.Awards) != 0 {
		t.Fatalf("detail = %#v, err = %v", detail, err)
	}
}

// TestServiceReplaceAndDeleteLifecycle 验证替换切换对象并删除旧文件，删除完成后状态落库。
func TestServiceReplaceAndDeleteLifecycle(t *testing.T) {
	repository := &repositoryFake{item: resume.Resume{
		ID: "30000000-0000-4000-8000-000000000003", OwnerUserID: serviceTestActor.UserID,
		Title: "Resume", OriginalFilename: "old.pdf", ContentType: "application/pdf",
		SizeBytes: 10, ChecksumSHA256: strings.Repeat("a", 64), ObjectKey: "resume/v1/old.pdf",
		FileStatus: resume.FileAvailable, ParseStatus: resume.ParseReady, Version: 2,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}}
	files := newFileStorageFake()
	files.objects[repository.item.ObjectKey] = []byte("old")
	service := newTestService(t, repository, files)
	body := []byte("%PDF-1.4 replacement")
	sum := sha256.Sum256(body)

	updated, err := service.ReplaceFile(context.Background(), serviceTestActor, ReplaceFileCommand{
		ResumeID: repository.item.ID, Filename: "new.pdf", ContentType: "application/pdf",
		SizeBytes: int64(len(body)), ChecksumSHA256: hex.EncodeToString(sum[:]),
		File: bytes.NewReader(body), ExpectedVersion: 2,
	})
	if err != nil || updated.Version != 3 || updated.ObjectKey == "resume/v1/old.pdf" {
		t.Fatalf("updated = %#v, err = %v", updated, err)
	}
	if _, exists := files.objects["resume/v1/old.pdf"]; exists {
		t.Fatal("old object was not deleted")
	}
	if err := service.Delete(context.Background(), serviceTestActor, DeleteCommand{
		ResumeID: updated.ID, ExpectedVersion: updated.Version,
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repository.item.ID != "" {
		t.Fatalf("deleted item still visible: %#v", repository.item)
	}
}

// TestParseWorkerCompletesAndPersistsFailures 验证 Worker 成功修订与稳定失败码路径。
func TestParseWorkerCompletesAndPersistsFailures(t *testing.T) {
	claimed := resume.Resume{
		ID: "40000000-0000-4000-8000-000000000004", OwnerUserID: serviceTestActor.UserID,
		ObjectKey: "resume/v1/file.pdf", FileStatus: resume.FileAvailable,
		ParseStatus: resume.ParseRunning, Version: 3,
	}
	repository := &repositoryFake{claim: claimed, claimFound: true}
	files := newFileStorageFake()
	files.objects[claimed.ObjectKey] = []byte("pdf")
	parser := &parserFake{content: resume.Content{
		TargetPosition: "Backend Engineer", WorkExperiences: []resume.WorkExperience{},
		ProjectExperiences: []resume.ProjectExperience{}, EducationExperiences: []resume.EducationExperience{},
		Skills: []string{"Go"},
	}}
	worker, err := NewParseWorker(repository, files, parser, time.Millisecond)
	if err != nil {
		t.Fatalf("NewParseWorker: %v", err)
	}
	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed || repository.completedRevision.ParserVersion != "test-parser/v1" {
		t.Fatalf("processed = %v, revision = %#v, err = %v", processed, repository.completedRevision, err)
	}

	repository.claimFound = true
	repository.failedCode = ""
	parser.err = &codedParseError{code: "pdf_text_unavailable"}
	processed, err = worker.ProcessNext(context.Background())
	if err != nil || !processed || repository.failedCode != "pdf_text_unavailable" {
		t.Fatalf("processed = %v, failedCode = %q, err = %v", processed, repository.failedCode, err)
	}
}

func TestParseWorkerFallsBackToOCRForTextlessPDFOnly(t *testing.T) {
	claimed := resume.Resume{
		ID: "40000000-0000-4000-8000-000000000004", OwnerUserID: serviceTestActor.UserID,
		ObjectKey: "resume/v1/scanned.pdf", FileStatus: resume.FileAvailable,
		ParseStatus: resume.ParseRunning, Version: 3,
	}
	repository := &repositoryFake{claim: claimed, claimFound: true}
	files := newFileStorageFake()
	files.objects[claimed.ObjectKey] = []byte("scanned pdf")
	parser := &ocrFallbackParserFake{
		primaryErr: &codedParseError{code: "pdf_text_unavailable"},
		ocrContent: resume.Content{
			TargetPosition: "Backend Engineer", WorkExperiences: []resume.WorkExperience{},
			ProjectExperiences:   []resume.ProjectExperience{},
			EducationExperiences: []resume.EducationExperience{}, Skills: []string{"Go"},
		},
	}
	worker, err := NewParseWorker(repository, files, parser, time.Millisecond)
	if err != nil {
		t.Fatalf("NewParseWorker: %v", err)
	}
	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed || !parser.ocrCalled || files.signedURLCalls != 1 ||
		repository.completedRevision.ParserVersion != "paddleocr-vl-1.6/v1+fields/v1" {
		t.Fatalf(
			"processed=%v OCR=%v signed=%d revision=%#v error=%v",
			processed,
			parser.ocrCalled,
			files.signedURLCalls,
			repository.completedRevision,
			err,
		)
	}

	repository.claimFound = true
	repository.completedRevision = resume.Revision{}
	repository.failedCode = ""
	files.signedURLCalls = 0
	parser.primaryErr = &codedParseError{code: "pdf_invalid"}
	parser.ocrCalled = false
	processed, err = worker.ProcessNext(context.Background())
	if err != nil || !processed || parser.ocrCalled || files.signedURLCalls != 0 ||
		repository.failedCode != "pdf_invalid" {
		t.Fatalf(
			"non-textless processed=%v OCR=%v signed=%d code=%q error=%v",
			processed,
			parser.ocrCalled,
			files.signedURLCalls,
			repository.failedCode,
			err,
		)
	}

	repository.claimFound = true
	repository.failedCode = ""
	parser.primaryErr = &codedParseError{code: "pdf_text_unavailable", pageCount: 6}
	processed, err = worker.ProcessNext(context.Background())
	if err != nil || !processed || parser.ocrCalled || files.signedURLCalls != 0 ||
		repository.failedCode != "ocr_page_limit_exceeded" {
		t.Fatalf(
			"page limit processed=%v OCR=%v signed=%d code=%q error=%v",
			processed,
			parser.ocrCalled,
			files.signedURLCalls,
			repository.failedCode,
			err,
		)
	}
}

// newTestService 创建固定配置的应用服务测试夹具。
func newTestService(t *testing.T, repository Repository, files FileStorage) *Service {
	t.Helper()
	service, err := NewService(repository, files, idGeneratorFake{}, ServiceConfiguration{
		MaximumFileBytes: DefaultMaximumFileBytes, ReadURLLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

// createTestCommand 创建摘要匹配的 PDF 上传命令。
func createTestCommand(body []byte) CreateCommand {
	sum := sha256.Sum256(body)
	return CreateCommand{
		Title: "Backend Resume", Filename: "resume.pdf", ContentType: "application/pdf",
		SizeBytes: int64(len(body)), ChecksumSHA256: hex.EncodeToString(sum[:]),
		File: bytes.NewReader(body), IdempotencyKey: "request-key-1",
	}
}

// idGeneratorFake 为测试返回固定稳定标识和对象键。
type idGeneratorFake struct{}

// NewResumeID 返回固定测试 UUID。
func (idGeneratorFake) NewResumeID(string, string) string {
	return "20000000-0000-4000-8000-000000000002"
}

// NewObjectKey 返回由种子区分的测试对象键。
func (idGeneratorFake) NewObjectKey(_ string, seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "resume/v1/" + hex.EncodeToString(sum[:]) + ".pdf"
}

// fileStorageFake 是内存文件存储测试替身。
type fileStorageFake struct {
	objects        map[string][]byte
	putErr         error
	deleteErr      error
	signedURLCalls int
}

// newFileStorageFake 创建空内存文件存储。
func newFileStorageFake() *fileStorageFake {
	return &fileStorageFake{objects: make(map[string][]byte)}
}

// Put 保存一份测试对象。
func (storage *fileStorageFake) Put(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	if storage.putErr != nil {
		return storage.putErr
	}
	body, _ := io.ReadAll(reader)
	storage.objects[key] = append([]byte(nil), body...)
	return nil
}

// Open 打开一份测试对象。
func (storage *fileStorageFake) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, exists := storage.objects[key]
	if !exists {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// SignedReadURL 返回固定测试地址。
func (storage *fileStorageFake) SignedReadURL(_ context.Context, _ string, lifetime time.Duration) (string, time.Time, error) {
	storage.signedURLCalls++
	return "https://files.example.test/resume.pdf", time.Now().UTC().Add(lifetime), nil
}

// Delete 删除测试对象。
func (storage *fileStorageFake) Delete(_ context.Context, key string) error {
	if storage.deleteErr != nil {
		return storage.deleteErr
	}
	delete(storage.objects, key)
	return nil
}

// parserFake 返回固定解析内容或错误。
type parserFake struct {
	content resume.Content
	err     error
}

// Parse 返回测试解析结果。
func (parser *parserFake) Parse(context.Context, io.Reader) (resume.Content, error) {
	return parser.content, parser.err
}

// Version 返回固定解析器版本。
func (*parserFake) Version() string { return "test-parser/v1" }

type ocrFallbackParserFake struct {
	primaryErr error
	ocrContent resume.Content
	ocrErr     error
	ocrCalled  bool
}

func (parser *ocrFallbackParserFake) Parse(
	context.Context,
	io.Reader,
) (resume.Content, error) {
	return resume.Content{}, parser.primaryErr
}

func (parser *ocrFallbackParserFake) ParseURL(
	_ context.Context,
	_ string,
) (resume.Content, error) {
	parser.ocrCalled = true
	return parser.ocrContent, parser.ocrErr
}

func (*ocrFallbackParserFake) Version() string { return "pdf-native-text/v1+fields/v1" }

func (*ocrFallbackParserFake) OCRVersion() string {
	return "paddleocr-vl-1.6/v1+fields/v1"
}

// codedParseError 提供稳定 Worker 失败码。
type codedParseError struct {
	code      string
	pageCount int
}

// Error 返回测试错误描述。
func (failure *codedParseError) Error() string { return failure.code }

// FailureCode 返回稳定失败码。
func (failure *codedParseError) FailureCode() string { return failure.code }

func (failure *codedParseError) PageCount() int { return failure.pageCount }

// repositoryFake 实现应用端口并记录关键状态迁移。
type repositoryFake struct {
	item              resume.Resume
	listItems         []resume.Resume
	abortCalled       bool
	claim             resume.Resume
	claimFound        bool
	completedRevision resume.Revision
	failedCode        string
	detailRevision    *resume.Revision
}

// CreateWithinLimit 创建测试记录或返回幂等冲突。
func (repository *repositoryFake) CreateWithinLimit(_ context.Context, item resume.Resume, _ int) error {
	if repository.item.ID == item.ID {
		return ResumeVersionConflictError()
	}
	repository.item = item
	return nil
}

// AbortCreate 删除未完成测试记录。
func (repository *repositoryFake) AbortCreate(context.Context, string, string) error {
	repository.abortCalled = true
	repository.item = resume.Resume{}
	return nil
}

// ListByOwner 返回当前测试记录。
func (repository *repositoryFake) ListByOwner(context.Context, string, ListQuery) ([]resume.Resume, error) {
	if repository.listItems != nil {
		return append([]resume.Resume(nil), repository.listItems...), nil
	}
	if repository.item.ID == "" {
		return []resume.Resume{}, nil
	}
	return []resume.Resume{repository.item}, nil
}

// FindByOwnerAndID 返回当前测试记录。
func (repository *repositoryFake) FindByOwnerAndID(context.Context, string, string) (resume.Resume, error) {
	if repository.item.ID == "" {
		return resume.Resume{}, ResumeNotFoundError()
	}
	return repository.item, nil
}

// FindDetailByOwnerAndID 返回当前测试详情。
func (repository *repositoryFake) FindDetailByOwnerAndID(context.Context, string, string) (Detail, error) {
	return Detail{Resume: repository.item, Revision: repository.detailRevision}, nil
}

// UpdateMetadata 更新测试标题。
func (repository *repositoryFake) UpdateMetadata(_ context.Context, _ string, command UpdateMetadataCommand) (resume.Resume, error) {
	repository.item.Title = command.Title
	repository.item.Version++
	return repository.item, nil
}

// ReplaceFileRecord 切换测试文件记录。
func (repository *repositoryFake) ReplaceFileRecord(_ context.Context, _ string, command ReplaceFileRecordCommand) (resume.Resume, error) {
	repository.item.OriginalFilename = command.Filename
	repository.item.SizeBytes = command.SizeBytes
	repository.item.ChecksumSHA256 = command.ChecksumSHA256
	repository.item.ObjectKey = command.ObjectKey
	repository.item.ParseStatus = resume.ParseQueued
	repository.item.Version++
	return repository.item, nil
}

// MarkFileAvailableAndQueueParse 完成测试上传。
func (repository *repositoryFake) MarkFileAvailableAndQueueParse(context.Context, string, string) error {
	repository.item.FileStatus = resume.FileAvailable
	repository.item.ParseStatus = resume.ParseQueued
	repository.item.Version++
	return nil
}

// SaveManualRevision 返回测试手动修订。
func (repository *repositoryFake) SaveManualRevision(_ context.Context, _ string, command UpdateContentCommand) (resume.Revision, error) {
	return resume.Revision{ResumeID: command.ResumeID, Revision: 1, Source: resume.RevisionManual, Content: command.Content}, nil
}

// MarkDeleting 标记测试记录删除中。
func (repository *repositoryFake) MarkDeleting(_ context.Context, _ string, _ DeleteCommand) (resume.Resume, error) {
	repository.item.FileStatus = resume.FileDeleting
	repository.item.Version++
	return repository.item, nil
}

// MarkDeleted 隐藏已删除测试记录。
func (repository *repositoryFake) MarkDeleted(context.Context, string, string) error {
	repository.item = resume.Resume{}
	return nil
}

// QueueParse 把测试记录重新排队。
func (repository *repositoryFake) QueueParse(context.Context, string, string) error {
	repository.item.ParseStatus = resume.ParseQueued
	return nil
}

// ClaimNextQueuedParse 返回预设测试任务。
func (repository *repositoryFake) ClaimNextQueuedParse(context.Context) (resume.Resume, bool, error) {
	found := repository.claimFound
	repository.claimFound = false
	return repository.claim, found, nil
}

// CompleteParse 记录测试解析修订。
func (repository *repositoryFake) CompleteParse(_ context.Context, _ resume.Resume, revision resume.Revision) error {
	repository.completedRevision = revision
	return nil
}

// FailParse 记录测试失败码。
func (repository *repositoryFake) FailParse(_ context.Context, _ resume.Resume, code string) error {
	repository.failedCode = code
	return nil
}
