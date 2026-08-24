BEGIN;

CREATE VIEW product_health_daily_practice_activity
WITH (security_barrier = true) AS
SELECT
    date_trunc('day', turn.confirmed_at, 'UTC') AS day_utc,
    count(DISTINCT session.user_id)::bigint AS active_practice_users,
    count(DISTINCT turn.session_id)::bigint AS sessions_with_confirmed_turn,
    count(*)::bigint AS confirmed_turns,
    count(*) FILTER (WHERE turn.turn_kind = 'EFFECTIVE')::bigint
        AS effective_confirmed_turns,
    count(*) FILTER (WHERE turn.turn_kind = 'RETRY')::bigint
        AS retry_confirmed_turns,
    count(*) FILTER (WHERE turn.turn_kind = 'RETRY')::double precision
        / NULLIF(count(*), 0) AS retry_turn_share,
    count(DISTINCT turn.session_id) FILTER (
        WHERE turn.turn_kind = 'RETRY'
    )::bigint AS sessions_with_retry,
    count(DISTINCT turn.session_id) FILTER (
        WHERE turn.turn_kind = 'RETRY'
    )::double precision
        / NULLIF(count(DISTINCT turn.session_id), 0) AS retry_session_share,
    count(*) FILTER (WHERE turn.interaction_mode = 'TEXT')::bigint
        AS text_confirmed_turns,
    count(*) FILTER (WHERE turn.interaction_mode = 'PUSH_TO_TALK')::bigint
        AS voice_confirmed_turns,
    count(*) FILTER (
        WHERE turn.interaction_mode IS NULL
           OR turn.interaction_mode NOT IN ('TEXT', 'PUSH_TO_TALK')
    )::bigint AS unknown_mode_confirmed_turns
FROM practice_turns AS turn
JOIN practice_sessions AS session
  ON session.session_id = turn.session_id
WHERE turn.status = 'confirmed'
GROUP BY date_trunc('day', turn.confirmed_at, 'UTC');

CREATE VIEW product_health_daily_session_outcomes
WITH (security_barrier = true) AS
SELECT
    date_trunc('day', ended_at, 'UTC') AS day_utc,
    count(*) FILTER (WHERE status = 'completed')::bigint
        AS completed_sessions,
    count(*) FILTER (WHERE status = 'ended_early')::bigint
        AS ended_early_sessions,
    count(*)::bigint AS terminal_sessions,
    count(*) FILTER (WHERE status = 'completed')::double precision
        / NULLIF(count(*), 0) AS completion_rate,
    count(*) FILTER (WHERE status = 'ended_early')::double precision
        / NULLIF(count(*), 0) AS ended_early_rate
FROM practice_sessions
WHERE status IN ('completed', 'ended_early')
GROUP BY date_trunc('day', ended_at, 'UTC');

CREATE VIEW product_health_daily_artifact_coverage
WITH (security_barrier = true) AS
WITH turn_feedback AS (
    SELECT
        date_trunc('day', turn.confirmed_at, 'UTC') AS day_utc,
        'TURN_FEEDBACK'::text AS artifact_kind,
        count(*) FILTER (
            WHERE session.plan_snapshot #>
                '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
        )::bigint AS eligible_sources,
        count(*) FILTER (
            WHERE session.plan_snapshot #>
                    '{session_policy,speech_feedback_allowed}' IS NULL
               OR jsonb_typeof(
                    session.plan_snapshot #>
                        '{session_policy,speech_feedback_allowed}'
               ) <> 'boolean'
        )::bigint AS unknown_eligibility_sources,
        count(evaluation.id) FILTER (
            WHERE session.plan_snapshot #>
                '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
        )::bigint AS scheduled_evaluations,
        count(evaluation.id) FILTER (
            WHERE session.plan_snapshot #>
                    '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
              AND evaluation.status = 'QUEUED'
        )::bigint AS queued_evaluations,
        count(evaluation.id) FILTER (
            WHERE session.plan_snapshot #>
                    '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
              AND evaluation.status = 'RUNNING'
        )::bigint AS running_evaluations,
        count(evaluation.id) FILTER (
            WHERE session.plan_snapshot #>
                    '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
              AND evaluation.status = 'READY'
        )::bigint AS ready_evaluations,
        count(evaluation.id) FILTER (
            WHERE session.plan_snapshot #>
                    '{session_policy,speech_feedback_allowed}' = 'true'::jsonb
              AND evaluation.status = 'FAILED'
        )::bigint AS failed_evaluations
    FROM practice_turns AS turn
    JOIN practice_sessions AS session
      ON session.session_id = turn.session_id
    LEFT JOIN evaluations AS evaluation
      ON evaluation.user_id = session.user_id
     AND evaluation.kind = 'PRACTICE_TURN_FEEDBACK'
     AND evaluation.source_id = turn.turn_id
     AND evaluation.context_id = turn.session_id
    WHERE turn.status = 'confirmed'
    GROUP BY date_trunc('day', turn.confirmed_at, 'UTC')
), session_report AS (
    SELECT
        date_trunc('day', session.ended_at, 'UTC') AS day_utc,
        'SESSION_REPORT'::text AS artifact_kind,
        count(*)::bigint AS eligible_sources,
        0::bigint AS unknown_eligibility_sources,
        count(evaluation.id)::bigint AS scheduled_evaluations,
        count(evaluation.id) FILTER (
            WHERE evaluation.status = 'QUEUED'
        )::bigint AS queued_evaluations,
        count(evaluation.id) FILTER (
            WHERE evaluation.status = 'RUNNING'
        )::bigint AS running_evaluations,
        count(evaluation.id) FILTER (
            WHERE evaluation.status = 'READY'
        )::bigint AS ready_evaluations,
        count(evaluation.id) FILTER (
            WHERE evaluation.status = 'FAILED'
        )::bigint AS failed_evaluations
    FROM practice_sessions AS session
    LEFT JOIN evaluations AS evaluation
      ON evaluation.user_id = session.user_id
     AND evaluation.kind = 'SESSION_REPORT'
     AND evaluation.source_id = session.session_id
     AND evaluation.context_id = session.session_id
    WHERE session.status = 'completed'
    GROUP BY date_trunc('day', session.ended_at, 'UTC')
), artifacts AS (
    SELECT * FROM turn_feedback
    UNION ALL
    SELECT * FROM session_report
)
SELECT
    day_utc,
    artifact_kind,
    eligible_sources,
    unknown_eligibility_sources,
    scheduled_evaluations,
    eligible_sources - scheduled_evaluations AS missing_evaluations,
    queued_evaluations,
    running_evaluations,
    ready_evaluations,
    failed_evaluations,
    scheduled_evaluations::double precision
        / NULLIF(eligible_sources, 0) AS scheduled_coverage_rate,
    ready_evaluations::double precision
        / NULLIF(eligible_sources, 0) AS ready_coverage_rate
FROM artifacts;

CREATE VIEW product_health_daily_evaluation_health
WITH (security_barrier = true) AS
SELECT
    date_trunc('day', created_at, 'UTC') AS day_utc,
    kind AS evaluation_kind,
    count(*)::bigint AS total_jobs,
    count(*) FILTER (WHERE status = 'QUEUED')::bigint AS queued_jobs,
    count(*) FILTER (WHERE status = 'RUNNING')::bigint AS running_jobs,
    count(*) FILTER (WHERE status = 'READY')::bigint AS ready_jobs,
    count(*) FILTER (WHERE status = 'FAILED')::bigint AS failed_jobs,
    count(*) FILTER (WHERE status IN ('READY', 'FAILED'))::bigint
        AS terminal_jobs,
    count(*) FILTER (WHERE status = 'READY')::double precision
        / NULLIF(count(*) FILTER (WHERE status IN ('READY', 'FAILED')), 0)
        AS terminal_success_rate,
    count(*) FILTER (WHERE status = 'FAILED')::double precision
        / NULLIF(count(*) FILTER (WHERE status IN ('READY', 'FAILED')), 0)
        AS terminal_failure_rate,
    count(*) FILTER (WHERE started_at IS NOT NULL)::bigint
        AS initial_queue_samples,
    avg(extract(epoch FROM started_at - created_at)) FILTER (
        WHERE started_at IS NOT NULL
    ) AS initial_queue_avg_seconds,
    percentile_cont(0.95) WITHIN GROUP (
        ORDER BY extract(epoch FROM started_at - created_at)
    ) FILTER (WHERE started_at IS NOT NULL) AS initial_queue_p95_seconds,
    count(*) FILTER (
        WHERE status IN ('READY', 'FAILED')
          AND started_at IS NOT NULL
          AND finished_at IS NOT NULL
    )::bigint AS processing_lifecycle_samples,
    avg(extract(epoch FROM finished_at - started_at)) FILTER (
        WHERE status IN ('READY', 'FAILED')
          AND started_at IS NOT NULL
          AND finished_at IS NOT NULL
    ) AS processing_lifecycle_avg_seconds,
    percentile_cont(0.95) WITHIN GROUP (
        ORDER BY extract(epoch FROM finished_at - started_at)
    ) FILTER (
        WHERE status IN ('READY', 'FAILED')
          AND started_at IS NOT NULL
          AND finished_at IS NOT NULL
    ) AS processing_lifecycle_p95_seconds,
    count(*) FILTER (
        WHERE status IN ('READY', 'FAILED') AND finished_at IS NOT NULL
    )::bigint AS total_lifecycle_samples,
    avg(extract(epoch FROM finished_at - created_at)) FILTER (
        WHERE status IN ('READY', 'FAILED') AND finished_at IS NOT NULL
    ) AS total_lifecycle_avg_seconds,
    percentile_cont(0.95) WITHIN GROUP (
        ORDER BY extract(epoch FROM finished_at - created_at)
    ) FILTER (
        WHERE status IN ('READY', 'FAILED') AND finished_at IS NOT NULL
    ) AS total_lifecycle_p95_seconds
FROM evaluations
GROUP BY date_trunc('day', created_at, 'UTC'), kind;

CREATE VIEW product_health_daily_scoreability
WITH (security_barrier = true) AS
SELECT
    date_trunc('day', created_at, 'UTC') AS day_utc,
    kind AS evaluation_kind,
    count(*)::bigint AS ready_evaluations,
    count(*) FILTER (
        WHERE result->>'scoreability_status' = 'PROVISIONAL'
    )::bigint AS provisional_evaluations,
    count(*) FILTER (
        WHERE result->>'scoreability_status' = 'INSUFFICIENT'
    )::bigint AS insufficient_evaluations,
    count(*) FILTER (
        WHERE result->>'scoreability_status' IS NULL
           OR result->>'scoreability_status' NOT IN ('PROVISIONAL', 'INSUFFICIENT')
    )::bigint AS unknown_scoreability_evaluations,
    count(*) FILTER (
        WHERE result->>'scoreability_status' = 'INSUFFICIENT'
    )::double precision / NULLIF(count(*), 0) AS insufficient_share
FROM evaluations
WHERE status = 'READY'
GROUP BY date_trunc('day', created_at, 'UTC'), kind;

REVOKE ALL ON product_health_daily_practice_activity FROM PUBLIC;
REVOKE ALL ON product_health_daily_session_outcomes FROM PUBLIC;
REVOKE ALL ON product_health_daily_artifact_coverage FROM PUBLIC;
REVOKE ALL ON product_health_daily_evaluation_health FROM PUBLIC;
REVOKE ALL ON product_health_daily_scoreability FROM PUBLIC;

COMMIT;
