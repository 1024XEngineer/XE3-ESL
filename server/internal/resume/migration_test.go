// 本文件验证 Resume 迁移冻结了所有权、三份上限所需锁点和不可变修订边界。
package resume_test

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

// TestResumeMigrationDefinesOwnedImmutableResources 验证迁移包含关键约束和索引。
func TestResumeMigrationDefinesOwnedImmutableResources(t *testing.T) {
	t.Parallel()

	content, err := migrations.Files.ReadFile("000060_resumes.up.sql")
	if err != nil {
		t.Fatalf("read Resume migration: %v", err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table resumes",
		"create table resume_revisions",
		"resumes_owner_fkey",
		"resumes_owner_active_updated_idx",
		"resumes_parse_queue_idx",
		"resumes_current_revision_fkey",
		"resume_revisions_immutable",
		"on delete cascade",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("Resume migration is missing %q", required)
		}
	}
}
