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
//go:embed 000040_review_speech_feedback.*.sql
//go:embed 000041_speech_feedback_retry_turns.*.sql
//go:embed 000042_agent_image_assets.*.sql
//go:embed 000043_speech_feedback_ise_evidence.*.sql
//go:embed 000044_speech_feedback_practice_session_ids.*.sql
//go:embed 000045_speech_feedback_ise_topic.*.sql
//go:embed 000046_agent_thread_sidebar_deletion.*.sql
//go:embed 000047_practice_follow_up_turns.*.sql
//go:embed 000048_follow_up_confirmed_turn_shape.*.sql
//go:embed 000049_goal_authority_models.*.sql
//go:embed 000050_scene_authority_catalog.*.sql
//go:embed 000051_preparation_plan_authority.*.sql
//go:embed 000052_practice_runtime_authority.*.sql
//go:embed 000053_evaluation_speech_feedback_authority.*.sql
//go:embed 000054_review_repractice_requests.*.sql
//go:embed 000055_practice_evaluation_handoff.*.sql
//go:embed 000056_evaluation_reports_learning_profile.*.sql
//go:embed 000057_agent_learning_profile_context_manifest.*.sql
//go:embed 000058_evaluation_report_authority.*.sql
//go:embed 000059_evaluation_general_scene_runtime.*.sql
//go:embed 000060_resumes.*.sql
//go:embed 000061_agent_practice_handoffs.*.sql
//go:embed 000062_agent_memory_extraction_context_barrier.*.sql
//go:embed 000063_speech_feedback_acoustic_provider_boundary.*.sql
//go:embed 000064_evaluation_ielts_speaking_prompt_v2.*.sql
//go:embed 000065_evaluation_ielts_speaking_prompt_v3.*.sql
//go:embed 000066_evaluation_ielts_speaking_prompt_v4.*.sql
//go:embed 000067_evaluation_ielts_speaking_acoustic_payload.*.sql
//go:embed 000068_preparation_resume_revision.*.sql
//go:embed 000069_scene_evaluation_policy_ref.*.sql
//go:embed 000070_practice_question_tips.*.sql
//go:embed 000071_practice_execution_policy_refs.*.sql
//go:embed 000072_ielts_dedicated_assignment_limits.*.sql
//go:embed 000073_agent_message_memes.*.sql
//go:embed 000074_scene_experience_policy_boundary.*.sql
//go:embed 000075_remove_agent_message_memes.*.sql
//go:embed 000076_temporary_resumes.*.sql
//go:embed 000077_scene_four_experiences.*.sql
//go:embed 000078_preparation_typed_contexts.*.sql
//go:embed 000079_preparation_plan_typed_context.*.sql
//go:embed 000080_ielts_versioned_question_bank.*.sql
//go:embed 000081_restore_interview_follow_ups.*.sql
//go:embed 000082_user_controlled_practice_sessions.*.sql
//go:embed 000083_agent_thread_titles.*.sql
//go:embed 000084_ielts_answer_preparations.*.sql
//go:embed 000085_evaluation_ielts_speaking_prompt_v5.*.sql
//go:embed 000086_provider_qualified_model_ids.*.sql
var Files embed.FS
