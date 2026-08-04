package evidence

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestEvidenceSnapshotMigrationIsImmutableAndContainsNoStorageLocator(
	t *testing.T,
) {
	content, err := migrations.Files.ReadFile(
		"000036_evaluation_evidence_snapshots.up.sql",
	)
	if err != nil {
		t.Fatalf("read EvidenceSnapshot migration: %v", err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"create table evaluation_evidence_snapshots",
		"evaluation_evidence_snapshots_source_unique",
		"evaluation_evidence_snapshots_revision_unique",
		"evaluation_evidence_snapshots_ref_scope_check",
		"evaluation_evidence_refs_are_consistent",
		"create table evaluation_deletion_fences",
		"evaluation_evidence_snapshots_immutable",
		"before update or delete",
		"canonical_payload",
		"source_manifest_hash",
		"snapshot_hash",
		"evaluation_evidence_snapshots_no_storage_locator_check",
		"jsonb_path_exists",
		"object[-_]?key",
		"signed[-_]?url",
		"audio[-_]?url",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
	if strings.Contains(sql, "provider_key") {
		t.Error("migration contains forbidden Provider credential field")
	}
}
