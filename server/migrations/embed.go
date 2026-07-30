// Package migrations exposes the ordered SQL migration history as an embedded
// filesystem so every migration command runs the exact files reviewed in Git.
package migrations

import "embed"

// Files contains every executable up and down migration in this directory.
//
// The original Review policy files remain in Git as append-only history but
// share version 000026 with the earlier Thread Summary migration. Keep them
// outside Files; 000028 is the executable copy tracked by issue 223.
//
//go:embed 0000[01][0-9]_*.sql 00002[0-5]_*.sql
//go:embed 000026_agent_thread_summary_jobs.*.sql
//go:embed 000027_agent_summary_context_manifest.*.sql
//go:embed 000028_review_scenario_policies.*.sql
//go:embed 000029_agent_stable_profile_context_manifest.*.sql
//go:embed 000030_agent_tool_routing_cleanup.*.sql
//go:embed 000031_agent_memory_extraction_barrier.*.sql
//go:embed 000032_ielts_speaking_full_mock_model.*.sql
//go:embed 000033_matter_agent_tools.*.sql
//go:embed 000034_practice_optional_preview_snapshot.*.sql
//go:embed 000035_ielts_speaking_section_models.*.sql
//go:embed 000036_evaluation_evidence_snapshots.*.sql
//go:embed 000037_evaluation_interview_shadow_runtime.*.sql
//go:embed 000038_evaluation_practice_resource_ids.*.sql
//go:embed 000039_evaluation_ielts_speaking_shadow_runtime.*.sql
var Files embed.FS
