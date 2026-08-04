BEGIN;

CREATE TABLE evaluation_formal_reports (
    report_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id uuid NOT NULL,
    evaluation_revision_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    revision integer NOT NULL,
    scene_type text COLLATE "C" NOT NULL,
    scene_model text COLLATE "C" NOT NULL,
    scoreability_status text COLLATE "C" NOT NULL,
    schema_version text COLLATE "C" NOT NULL,
    report_payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_formal_reports_revision_fkey
        FOREIGN KEY (evaluation_id, evaluation_revision_id)
        REFERENCES evaluation_revisions (evaluation_id, id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_formal_reports_ledger_fkey
        FOREIGN KEY (evaluation_id, owner_user_id)
        REFERENCES evaluation_ledgers (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT evaluation_formal_reports_revision_unique
        UNIQUE (evaluation_revision_id),
    CONSTRAINT evaluation_formal_reports_identity_unique
        UNIQUE (evaluation_id, evaluation_revision_id, owner_user_id),
    CONSTRAINT evaluation_formal_reports_revision_check
        CHECK (revision > 0),
    CONSTRAINT evaluation_formal_reports_scene_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'INTERVIEW',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
            AND scene_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT evaluation_formal_reports_scoreability_check
        CHECK (scoreability_status IN ('PROVISIONAL', 'INSUFFICIENT')),
    CONSTRAINT evaluation_formal_reports_payload_check
        CHECK (
            schema_version = 'evaluation-report/v1'
            AND jsonb_typeof(report_payload) = 'object'
            AND report_payload->>'schema_version' = schema_version
            AND report_payload->>'scene_type' = scene_type
            AND report_payload->>'scene_model' = scene_model
            AND report_payload->>'scoreability_status' = scoreability_status
            AND octet_length(report_payload::text) <= 262144
        )
);

CREATE INDEX evaluation_formal_reports_owner_history_idx
    ON evaluation_formal_reports (
        owner_user_id,
        created_at DESC,
        report_id DESC
    );

CREATE TABLE learning_profile_contributions (
    owner_user_id uuid NOT NULL,
    evaluation_id uuid NOT NULL,
    evaluation_revision_id uuid NOT NULL,
    dimension_key text COLLATE "C" NOT NULL,
    score_scale text COLLATE "C" NOT NULL,
    score numeric(8, 3) NOT NULL,
    confidence numeric(6, 5) NOT NULL,
    recurring_issues jsonb NOT NULL DEFAULT '[]'::jsonb,
    evidence_ref_ids text[] COLLATE "C" NOT NULL,
    strategy_version text COLLATE "C" NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (
        owner_user_id,
        evaluation_revision_id,
        dimension_key
    ),
    CONSTRAINT learning_profile_contributions_report_fkey
        FOREIGN KEY (
            evaluation_id,
            evaluation_revision_id,
            owner_user_id
        )
        REFERENCES evaluation_formal_reports (
            evaluation_id,
            evaluation_revision_id,
            owner_user_id
        )
        ON DELETE CASCADE,
    CONSTRAINT learning_profile_contributions_dimension_check
        CHECK (dimension_key ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'),
    CONSTRAINT learning_profile_contributions_score_check
        CHECK (
            (score_scale = 'PERCENTAGE_100' AND score BETWEEN 0 AND 100)
            OR (score_scale = 'IELTS_BAND' AND score BETWEEN 0 AND 9)
        ),
    CONSTRAINT learning_profile_contributions_confidence_check
        CHECK (confidence BETWEEN 0 AND 1),
    CONSTRAINT learning_profile_contributions_issues_check
        CHECK (
            jsonb_typeof(recurring_issues) = 'array'
            AND jsonb_array_length(recurring_issues) <= 5
            AND octet_length(recurring_issues::text) <= 16384
        ),
    CONSTRAINT learning_profile_contributions_evidence_check
        CHECK (cardinality(evidence_ref_ids) BETWEEN 1 AND 64),
    CONSTRAINT learning_profile_contributions_strategy_check
        CHECK (strategy_version ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$')
);

CREATE INDEX learning_profile_contributions_dimension_idx
    ON learning_profile_contributions (
        owner_user_id,
        dimension_key,
        created_at DESC,
        evaluation_revision_id DESC
    );

CREATE TABLE learning_profile_dimensions (
    owner_user_id uuid NOT NULL,
    dimension_key text COLLATE "C" NOT NULL,
    score_scale text COLLATE "C" NOT NULL,
    estimated_value numeric(8, 3) NOT NULL,
    confidence numeric(6, 5) NOT NULL,
    trend text COLLATE "C" NOT NULL,
    recurring_issues jsonb NOT NULL,
    source_evaluation_refs jsonb NOT NULL,
    strategy_version text COLLATE "C" NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (owner_user_id, dimension_key),
    CONSTRAINT learning_profile_dimensions_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT learning_profile_dimensions_key_check
        CHECK (dimension_key ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$'),
    CONSTRAINT learning_profile_dimensions_score_check
        CHECK (
            (score_scale = 'PERCENTAGE_100' AND estimated_value BETWEEN 0 AND 100)
            OR (score_scale = 'IELTS_BAND' AND estimated_value BETWEEN 0 AND 9)
        ),
    CONSTRAINT learning_profile_dimensions_confidence_check
        CHECK (confidence BETWEEN 0 AND 1),
    CONSTRAINT learning_profile_dimensions_trend_check
        CHECK (trend IN ('INITIAL', 'STABLE', 'IMPROVING', 'DECLINING')),
    CONSTRAINT learning_profile_dimensions_issues_check
        CHECK (
            jsonb_typeof(recurring_issues) = 'array'
            AND jsonb_array_length(recurring_issues) <= 10
            AND octet_length(recurring_issues::text) <= 32768
        ),
    CONSTRAINT learning_profile_dimensions_sources_check
        CHECK (
            jsonb_typeof(source_evaluation_refs) = 'array'
            AND jsonb_array_length(source_evaluation_refs) BETWEEN 1 AND 20
            AND octet_length(source_evaluation_refs::text) <= 32768
        ),
    CONSTRAINT learning_profile_dimensions_strategy_check
        CHECK (strategy_version ~ '^[A-Za-z][A-Za-z0-9._:/-]{0,127}$')
);

COMMIT;
