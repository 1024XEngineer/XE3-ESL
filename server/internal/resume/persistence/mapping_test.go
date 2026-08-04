// 本文件验证 Resume 领域模型与 GORM Record 的无损转换。
package persistence

import (
	"reflect"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// TestResumeRecordRoundTrip 验证简历元数据、状态和可空字段能够往返转换。
func TestResumeRecordRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	deletedAt := now.Add(time.Minute)
	want := resume.Resume{
		ID:               "10000000-0000-4000-8000-000000000001",
		OwnerUserID:      "20000000-0000-4000-8000-000000000001",
		Title:            "Backend Resume",
		OriginalFilename: "backend.pdf",
		ContentType:      "application/pdf",
		SizeBytes:        1024,
		ChecksumSHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ObjectKey:        "resume/v1/backend.pdf",
		FileStatus:       resume.FileDeleted,
		ParseStatus:      resume.ParseFailed,
		ParseFailureCode: "parser_unavailable",
		CurrentRevision:  2,
		Version:          4,
		CreatedAt:        now,
		UpdatedAt:        deletedAt,
		DeletedAt:        &deletedAt,
	}

	got, err := resumeFromRecord(resumeToRecord(want))
	if err != nil {
		t.Fatalf("round trip Resume: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resume round trip = %#v, want %#v", got, want)
	}
}

// TestRevisionRecordRoundTrip 验证结构化内容能够通过 JSONB Record 往返转换。
func TestRevisionRecordRoundTrip(t *testing.T) {
	t.Parallel()

	want := resume.Revision{
		ResumeID:      "10000000-0000-4000-8000-000000000001",
		Revision:      3,
		Source:        resume.RevisionParser,
		ParserVersion: "resume-parser/v1",
		Content: resume.Content{
			TargetPosition:      "Backend Engineer",
			ProfessionalSummary: "Go developer",
			ProjectExperiences: []resume.ProjectExperience{{
				ProjectName:  "API Platform",
				Role:         "Backend Engineer",
				Technologies: []string{"Go", "PostgreSQL"},
			}},
			Skills: []string{"Go", "SQL"},
		},
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}

	record, err := revisionToRecord(want)
	if err != nil {
		t.Fatalf("revision to record: %v", err)
	}
	got, err := revisionFromRecord(record)
	if err != nil {
		t.Fatalf("revision from record: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Revision round trip = %#v, want %#v", got, want)
	}
}
