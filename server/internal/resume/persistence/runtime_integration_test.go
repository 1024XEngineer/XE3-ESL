// 本文件在真实 PostgreSQL 上验证 Resume Service、存储和 Worker 的端到端生命周期。
package persistence_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/identifier"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/parser"
	resumestorage "github.com/1024XEngineer/XE3-ESL/server/internal/resume/storage"
)

// TestResumeRuntimeEndToEnd 验证真实 PostgreSQL 与内存私有对象存储上的完整 Resume 生命周期。
func TestResumeRuntimeEndToEnd(t *testing.T) {
	repository := newResumeRepository(t)
	objectStore, err := fake.New("resume/v1", time.Minute)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	files, err := resumestorage.NewObjectStore(objectStore)
	if err != nil {
		t.Fatalf("new Resume storage: %v", err)
	}
	ids, err := identifier.NewGenerator("resume/v1")
	if err != nil {
		t.Fatalf("new ID generator: %v", err)
	}
	service, err := app.NewService(repository, files, ids, app.ServiceConfiguration{
		MaximumFileBytes: app.DefaultMaximumFileBytes,
		ReadURLLifetime:  time.Minute,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	worker, err := app.NewParseWorker(repository, files, parser.NewPDFParser(), time.Millisecond)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	actor := requestcontext.Actor{UserID: resumeOwnerA, SessionID: "runtime-session"}
	ctx := context.Background()
	body := runtimeTestPDF(strings.Join([]string{
		"Target Position: Backend Engineer",
		"Professional Summary",
		"Backend engineer building reliable APIs",
		"Skills",
		"Go, PostgreSQL, Docker",
	}, "\n"))
	sum := sha256.Sum256(body)
	created, err := service.Create(ctx, actor, app.CreateCommand{
		Title: "Backend Resume", Filename: "resume.pdf", ContentType: "application/pdf",
		SizeBytes: int64(len(body)), ChecksumSHA256: hex.EncodeToString(sum[:]),
		File: bytes.NewReader(body), IdempotencyKey: "runtime-request-1",
	})
	if err != nil || created.FileStatus != resume.FileAvailable || !objectStore.Has(created.ObjectKey) {
		t.Fatalf("created = %#v, stored = %v, err = %v", created, objectStore.Has(created.ObjectKey), err)
	}
	if processed, err := worker.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("ProcessNext = %v, %v", processed, err)
	}
	detail, err := service.Get(ctx, actor, created.ID)
	if err != nil || detail.Resume.ParseStatus != resume.ParseReady || detail.Revision == nil ||
		detail.Revision.Content.TargetPosition != "Backend Engineer" {
		t.Fatalf("parsed detail = %#v, err = %v", detail, err)
	}
	manual, err := service.UpdateContent(ctx, actor, app.UpdateContentCommand{
		ResumeID: created.ID,
		Content: resume.Content{
			TargetPosition: "Platform Engineer", WorkExperiences: []resume.WorkExperience{},
			ProjectExperiences: []resume.ProjectExperience{}, EducationExperiences: []resume.EducationExperience{},
			Skills: []string{"Go"},
		},
		ExpectedVersion: detail.Resume.Version,
	})
	if err != nil || manual.Source != resume.RevisionManual {
		t.Fatalf("manual revision = %#v, err = %v", manual, err)
	}
	current, err := repository.FindByOwnerAndID(ctx, resumeOwnerA, created.ID)
	if err != nil {
		t.Fatalf("find current: %v", err)
	}
	contentURL, err := service.GetContentURL(ctx, actor, created.ID)
	if err != nil || contentURL.URL == "" || contentURL.ExpiresAt.IsZero() {
		t.Fatalf("content URL = %#v, err = %v", contentURL, err)
	}
	if err := service.Delete(ctx, actor, app.DeleteCommand{
		ResumeID: created.ID, ExpectedVersion: current.Version,
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if objectStore.Has(created.ObjectKey) {
		t.Fatal("deleted PDF remains in object storage")
	}
	page, err := service.List(ctx, actor, app.ListQuery{Limit: app.MaxResumesPerUser})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("items after delete = %#v, err = %v", page.Items, err)
	}
}

// runtimeTestPDF 构造可被文本提取器读取的最小测试 PDF。
func runtimeTestPDF(text string) []byte {
	commands := "BT /F1 10 Tf 14 TL 72 760 Td "
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			commands += "T* "
		}
		line = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
		commands += "(" + line + ") Tj "
	}
	commands += "ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(commands), commands),
	}
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, buffer.Len())
		fmt.Fprintf(&buffer, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := buffer.Len()
	fmt.Fprintf(&buffer, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&buffer, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buffer, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buffer.Bytes()
}
