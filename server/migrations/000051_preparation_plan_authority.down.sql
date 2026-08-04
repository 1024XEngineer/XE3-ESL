BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM preparation_practice_plans)
       OR EXISTS (SELECT 1 FROM preparation_practice_plan_revisions)
       OR EXISTS (SELECT 1 FROM practice_sessions)
       OR EXISTS (SELECT 1 FROM practice_session_snapshots)
       OR EXISTS (
           SELECT 1
           FROM reviews
           WHERE evaluation_context IS NOT NULL
       )
       OR EXISTS (
           SELECT 1
           FROM preparation_idempotency_records
           WHERE resource_kind = 'plan'
       ) THEN
        RAISE EXCEPTION
            'Preparation Plan authority downgrade requires empty Plan, Session, and Review context data slices; recreate the development or test database before reverting migration 000051'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE reviews
    DROP CONSTRAINT reviews_evaluation_context_check,
    ADD CONSTRAINT reviews_evaluation_context_check
        CHECK (
            evaluation_context IS NULL
            OR (
                jsonb_typeof(evaluation_context) = 'object'
                AND evaluation_context ?& ARRAY[
                    'schema_version',
                    'context_type',
                    'scene_key',
                    'scenario_definition_id',
                    'scenario_definition_version',
                    'practice_option_type',
                    'difficulty_ref',
                    'assistance_ref',
                    'turn_policy_ref',
                    'session_policy_ref',
                    'scene_specific_context'
                ]
                AND evaluation_context->>'schema_version' =
                    'evaluation-context.v1'
                AND octet_length(evaluation_context::text) <= 16384
            )
        );

ALTER TABLE practice_retry_turn_authorizations
    DROP CONSTRAINT practice_retry_turn_authorizations_scene_check;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scene_family TO scenario_type;

ALTER TABLE practice_retry_turn_authorizations
    RENAME COLUMN scene_model TO scenario_model;

ALTER TABLE practice_retry_turn_authorizations
    ADD CONSTRAINT practice_retry_turn_authorizations_scenario_check
        CHECK (
            (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        );

DROP TRIGGER practice_sessions_require_current_ready_plan_revision
    ON practice_sessions;
DROP FUNCTION require_current_ready_preparation_plan_revision();

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_snapshot_fkey;

ALTER TABLE practice_session_snapshots
    DROP CONSTRAINT practice_session_snapshots_session_binding_fkey,
    DROP CONSTRAINT practice_session_snapshots_document_check,
    DROP CONSTRAINT practice_session_snapshots_session_binding_key,
    DROP CONSTRAINT practice_session_snapshots_snapshot_key,
    ALTER COLUMN snapshot_document DROP NOT NULL,
    ALTER COLUMN snapshot_id DROP NOT NULL,
    ADD COLUMN context_plan_id text,
    ADD COLUMN preparation_snapshot_id text;

DROP INDEX practice_one_effective_session_per_plan;

ALTER TABLE practice_sessions
    DROP CONSTRAINT practice_sessions_scene_model_check,
    DROP CONSTRAINT practice_sessions_lifecycle_check,
    DROP CONSTRAINT practice_sessions_context_fields_check,
    DROP CONSTRAINT practice_sessions_snapshot_binding_key,
    DROP CONSTRAINT practice_sessions_snapshot_key,
    DROP CONSTRAINT practice_sessions_plan_revision_check,
    DROP CONSTRAINT practice_sessions_plan_revision_fkey,
    ALTER COLUMN scene_model DROP NOT NULL,
    ALTER COLUMN scene_family DROP NOT NULL,
    ALTER COLUMN snapshot_id DROP NOT NULL,
    DROP COLUMN plan_revision,
    ADD COLUMN context_plan_id text,
    ADD COLUMN agent_thread_id uuid,
    ADD COLUMN goal_id uuid;

ALTER TABLE practice_sessions
    RENAME COLUMN scene_family TO scenario_type;

ALTER TABLE practice_sessions
    RENAME COLUMN scene_model TO scenario_model;

ALTER TABLE preparation_idempotency_records
    DROP CONSTRAINT preparation_idempotency_plan_revision_fkey,
    DROP CONSTRAINT preparation_idempotency_resource_check,
    DROP COLUMN resource_revision,
    ADD CONSTRAINT preparation_idempotency_resource_check
        CHECK (
            resource_kind IN ('profile', 'snapshot')
            AND btrim(resource_id) <> ''
        );

ALTER TABLE practice_idempotency_records
    DROP CONSTRAINT practice_idempotency_resource_check,
    ADD CONSTRAINT practice_idempotency_resource_check
        CHECK (
            resource_kind IN (
                'plan',
                'plan_revision',
                'session',
                'pause',
                'resume',
                'end_early'
            )
            AND btrim(resource_id) <> ''
        );

ALTER TABLE preparation_practice_plans
    DROP CONSTRAINT preparation_practice_plans_current_revision_fkey;

DROP TRIGGER preparation_practice_plans_current_revision_advances
    ON preparation_practice_plans;
DROP FUNCTION validate_preparation_practice_plan_current_revision_advance();

DROP TRIGGER preparation_practice_plan_revisions_are_immutable
    ON preparation_practice_plan_revisions;
DROP FUNCTION reject_preparation_practice_plan_revision_mutation();

DROP TRIGGER preparation_practice_plan_revision_is_current
    ON preparation_practice_plan_revisions;
DROP FUNCTION require_preparation_practice_plan_revision_publication();

DROP TRIGGER preparation_practice_plan_revisions_validate_scene
    ON preparation_practice_plan_revisions;
DROP FUNCTION validate_preparation_practice_plan_scene_selection();

DROP TABLE preparation_practice_plan_revisions;
DROP TABLE preparation_practice_plans;

DROP FUNCTION preparation_plan_goal_snapshot_is_valid_v1(
    uuid,
    bigint,
    jsonb
);
DROP FUNCTION preparation_plan_ielts_assignment_is_valid_v1(jsonb, jsonb);
DROP FUNCTION preparation_plan_snapshot_is_valid_v1(text, jsonb);
DROP FUNCTION preparation_plan_session_policy_is_valid_v1(jsonb);
DROP FUNCTION preparation_plan_objectives_are_valid_v1(jsonb);

CREATE TABLE practice_plans (
    owner_user_id uuid NOT NULL,
    plan_id text NOT NULL CHECK (btrim(plan_id) <> ''),
    agent_thread_id uuid NOT NULL,
    goal_id uuid,
    scenario_definition_id text NOT NULL,
    scenario_definition_version integer NOT NULL
        CHECK (scenario_definition_version > 0),
    scenario_type text NOT NULL,
    scenario_config_id text NOT NULL,
    scenario_config_version integer NOT NULL
        CHECK (scenario_config_version > 0),
    preparation_profile_id text NOT NULL,
    selected_role_ids jsonb NOT NULL,
    plan_revision integer NOT NULL DEFAULT 1 CHECK (plan_revision > 0),
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    preparation_snapshot_id text,
    catalog_snapshot jsonb,
    session_policy jsonb,
    practice_focuses jsonb,
    scenario_model text NOT NULL,
    PRIMARY KEY (owner_user_id, plan_id),
    CONSTRAINT practice_plans_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_thread_owner_fkey
        FOREIGN KEY (agent_thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_profile_owner_fkey
        FOREIGN KEY (owner_user_id, preparation_profile_id)
        REFERENCES preparation_profiles (owner_user_id, profile_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_preparation_snapshot_fkey
        FOREIGN KEY (
            owner_user_id,
            preparation_snapshot_id,
            preparation_profile_id
        )
        REFERENCES preparation_snapshots (
            owner_user_id,
            snapshot_id,
            source_profile_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_goal_owner_fkey
        FOREIGN KEY (goal_id, owner_user_id)
        REFERENCES coaching_goals (goal_id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_thread_goal_link_fkey
        FOREIGN KEY (owner_user_id, agent_thread_id, goal_id)
        REFERENCES agent_thread_goal_links (
            owner_user_id,
            thread_id,
            goal_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT practice_plans_catalog_fields_check
        CHECK (
            btrim(scenario_definition_id) <> ''
            AND btrim(scenario_type) <> ''
            AND btrim(scenario_model) <> ''
            AND btrim(scenario_config_id) <> ''
            AND jsonb_typeof(selected_role_ids) = 'array'
            AND jsonb_array_length(selected_role_ids) > 0
        ),
    CONSTRAINT practice_plans_status_check
        CHECK (
            status IN (
                'configuring',
                'configuration_failed',
                'ready',
                'archived'
            )
        ),
    CONSTRAINT practice_plans_timestamps_check
        CHECK (updated_at >= created_at),
    CONSTRAINT practice_plans_preview_shape_check
        CHECK (
            (
                preparation_snapshot_id IS NULL
                AND catalog_snapshot IS NULL
                AND session_policy IS NULL
                AND practice_focuses IS NULL
            )
            OR (
                (
                    preparation_snapshot_id IS NULL
                    OR (
                        btrim(preparation_snapshot_id) =
                            preparation_snapshot_id
                        AND btrim(preparation_snapshot_id) <> ''
                    )
                )
                AND jsonb_typeof(catalog_snapshot) = 'object'
                AND jsonb_typeof(session_policy) = 'object'
                AND jsonb_typeof(practice_focuses) = 'array'
                AND jsonb_array_length(practice_focuses) > 0
            )
        ),
    CONSTRAINT practice_plans_scenario_model_check
        CHECK (
            (
                scenario_type = 'INTERVIEW'
                AND scenario_model IN (
                    'PROJECT_EXPERIENCE_DEEP_DIVE',
                    'INTERVIEW_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'EXAM'
                AND scenario_model IN (
                    'IELTS_SPEAKING_PART_1',
                    'IELTS_SPEAKING_PART_2',
                    'IELTS_SPEAKING_PART_3',
                    'IELTS_SPEAKING_FULL_MOCK',
                    'EXAM_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        ),
    CONSTRAINT practice_plans_context_thread_key
        UNIQUE (owner_user_id, plan_id, agent_thread_id),
    CONSTRAINT practice_plans_goal_context_anchor_key
        UNIQUE (
            owner_user_id,
            plan_id,
            agent_thread_id,
            goal_id
        )
);

CREATE INDEX practice_plans_owner_updated_idx
    ON practice_plans (owner_user_id, updated_at DESC, plan_id);

CREATE INDEX practice_plans_owner_thread_idx
    ON practice_plans (
        owner_user_id,
        agent_thread_id,
        goal_id,
        updated_at DESC,
        plan_id
    );

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_context_plan_fkey
        FOREIGN KEY (owner_user_id, context_plan_id)
        REFERENCES practice_plans (owner_user_id, plan_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_sessions_goal_context_anchor_fkey
        FOREIGN KEY (
            owner_user_id,
            context_plan_id,
            agent_thread_id,
            goal_id
        )
        REFERENCES practice_plans (
            owner_user_id,
            plan_id,
            agent_thread_id,
            goal_id
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_sessions_context_thread_fkey
        FOREIGN KEY (
            owner_user_id,
            context_plan_id,
            agent_thread_id
        )
        REFERENCES practice_plans (
            owner_user_id,
            plan_id,
            agent_thread_id
        )
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_sessions_context_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_sessions_context_binding_key
        UNIQUE (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        ),
    ADD CONSTRAINT practice_sessions_context_fields_check
        CHECK (
            (
                context_plan_id IS NULL
                AND agent_thread_id IS NULL
                AND goal_id IS NULL
                AND snapshot_id IS NULL
                AND scenario_type IS NULL
                AND scenario_model IS NULL
            )
            OR (
                context_plan_id IS NOT NULL
                AND agent_thread_id IS NOT NULL
                AND snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND scenario_type IS NOT NULL
                AND btrim(scenario_type) <> ''
                AND scenario_model IS NOT NULL
                AND btrim(scenario_model) <> ''
                AND plan_id = context_plan_id
            )
        ),
    ADD CONSTRAINT practice_sessions_lifecycle_check
        CHECK (
            (
                context_plan_id IS NULL
                AND (
                    (status = 'active' AND completed_at IS NULL)
                    OR (status = 'completed' AND completed_at IS NOT NULL)
                )
            )
            OR (
                context_plan_id IS NOT NULL
                AND (
                    (
                        status = 'starting'
                        AND started_at IS NULL
                        AND completed_at IS NULL
                        AND end_reason IS NULL
                    )
                    OR (
                        status IN ('in_progress', 'paused')
                        AND started_at IS NOT NULL
                        AND completed_at IS NULL
                        AND end_reason IS NULL
                    )
                    OR (
                        status IN ('completed', 'ended_early')
                        AND started_at IS NOT NULL
                        AND completed_at IS NOT NULL
                        AND end_reason IS NOT NULL
                        AND btrim(end_reason) <> ''
                    )
                )
            )
        ),
    ADD CONSTRAINT practice_sessions_scenario_model_check
        CHECK (
            context_plan_id IS NULL
            OR (
                scenario_type = 'INTERVIEW'
                AND scenario_model IN (
                    'PROJECT_EXPERIENCE_DEEP_DIVE',
                    'INTERVIEW_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'EXAM'
                AND scenario_model IN (
                    'IELTS_SPEAKING_PART_1',
                    'IELTS_SPEAKING_PART_2',
                    'IELTS_SPEAKING_PART_3',
                    'IELTS_SPEAKING_FULL_MOCK',
                    'EXAM_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'WORKPLACE'
                AND scenario_model IN (
                    'PROGRESS_AND_RISK_UPDATE',
                    'WORKPLACE_BASIC_DIALOGUE'
                )
            )
            OR (
                scenario_type = 'DAILY'
                AND scenario_model IN (
                    'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
                    'DAILY_BASIC_DIALOGUE'
                )
            )
        );

CREATE UNIQUE INDEX practice_one_effective_session_per_context_plan
    ON practice_sessions (owner_user_id, context_plan_id)
    WHERE
        context_plan_id IS NOT NULL
        AND status IN ('starting', 'in_progress', 'paused');

CREATE UNIQUE INDEX practice_one_effective_session_per_agent_thread
    ON practice_sessions (owner_user_id, agent_thread_id)
    WHERE
        context_plan_id IS NOT NULL
        AND status IN ('starting', 'in_progress', 'paused');

ALTER TABLE practice_session_snapshots
    ADD CONSTRAINT practice_session_snapshots_context_plan_fkey
        FOREIGN KEY (owner_user_id, context_plan_id)
        REFERENCES practice_plans (owner_user_id, plan_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_session_snapshots_preparation_fkey
        FOREIGN KEY (owner_user_id, preparation_snapshot_id)
        REFERENCES preparation_snapshots (owner_user_id, snapshot_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT practice_session_snapshots_context_snapshot_key
        UNIQUE (owner_user_id, snapshot_id),
    ADD CONSTRAINT practice_session_snapshots_context_binding_key
        UNIQUE (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        ),
    ADD CONSTRAINT practice_session_snapshots_context_fields_check
        CHECK (
            (
                snapshot_id IS NULL
                AND context_plan_id IS NULL
                AND preparation_snapshot_id IS NULL
                AND snapshot_document IS NULL
            )
            OR (
                snapshot_id IS NOT NULL
                AND btrim(snapshot_id) <> ''
                AND context_plan_id IS NOT NULL
                AND preparation_snapshot_id IS NOT NULL
                AND snapshot_document IS NOT NULL
                AND jsonb_typeof(snapshot_document) = 'object'
            )
        ),
    ADD CONSTRAINT practice_session_snapshots_context_session_fkey
        FOREIGN KEY (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        REFERENCES practice_sessions (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        ON DELETE CASCADE
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE practice_sessions
    ADD CONSTRAINT practice_sessions_context_snapshot_fkey
        FOREIGN KEY (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        REFERENCES practice_session_snapshots (
            owner_user_id,
            session_id,
            context_plan_id,
            snapshot_id
        )
        DEFERRABLE INITIALLY DEFERRED;

COMMIT;
