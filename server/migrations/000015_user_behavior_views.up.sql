BEGIN;

CREATE INDEX user_behavior_sessions_created_day_idx
    ON practice_sessions (date_trunc('day', created_at, 'UTC'));
CREATE INDEX user_behavior_confirmed_turns_day_idx
    ON practice_turns (date_trunc('day', confirmed_at, 'UTC'))
    WHERE status = 'confirmed';
CREATE INDEX user_behavior_ready_session_reports_idx
    ON evaluations (source_id, context_id, user_id, finished_at)
    WHERE kind = 'SESSION_REPORT' AND status = 'READY';
CREATE INDEX user_behavior_nonterminal_sessions_updated_idx
    ON practice_sessions (updated_at)
    WHERE status IN ('starting', 'in_progress', 'paused');

CREATE VIEW user_behavior_daily_session_funnel
WITH (security_barrier = true) AS
WITH session_events AS (
    SELECT
        session.created_at,
        session.started_at,
        session.status,
        session.ended_at,
        first_effective.confirmed_at AS first_effective_confirmed_at,
        ready_report.finished_at AS ready_report_finished_at
    FROM practice_sessions AS session
    LEFT JOIN LATERAL (
        SELECT min(turn.confirmed_at) AS confirmed_at
        FROM practice_turns AS turn
        WHERE turn.session_id = session.session_id
          AND turn.status = 'confirmed'
          AND turn.turn_kind = 'EFFECTIVE'
          AND turn.confirmed_at >= session.started_at
    ) AS first_effective ON true
    LEFT JOIN LATERAL (
        SELECT min(evaluation.finished_at) AS finished_at
        FROM evaluations AS evaluation
        WHERE evaluation.user_id = session.user_id
          AND evaluation.kind = 'SESSION_REPORT'
          AND evaluation.source_id = session.session_id
          AND evaluation.context_id = session.session_id
          AND evaluation.status = 'READY'
          AND evaluation.finished_at >= session.ended_at
    ) AS ready_report ON true
), monotonic_steps AS (
    SELECT
        created_at,
        started_at IS NOT NULL
            AND started_at >= created_at AS reached_started,
        started_at IS NOT NULL
            AND started_at >= created_at
            AND first_effective_confirmed_at >= started_at
                AS reached_first_effective,
        started_at IS NOT NULL
            AND started_at >= created_at
            AND first_effective_confirmed_at >= started_at
            AND status = 'completed'
            AND ended_at >= first_effective_confirmed_at
                AS reached_completed,
        started_at IS NOT NULL
            AND started_at >= created_at
            AND first_effective_confirmed_at >= started_at
            AND status = 'completed'
            AND ended_at >= first_effective_confirmed_at
            AND ready_report_finished_at >= ended_at
                AS reached_ready_report,
        started_at IS NOT NULL
            AND started_at >= created_at
            AND status = 'ended_early'
            AND ended_at >= started_at AS reached_ended_early
    FROM session_events
), daily AS (
    SELECT
        date_trunc('day', created_at, 'UTC') AS day_utc,
        count(*)::bigint AS created_sessions,
        count(*) FILTER (WHERE reached_started)::bigint AS started_sessions,
        count(*) FILTER (WHERE reached_first_effective)::bigint
            AS first_effective_confirmed_sessions,
        count(*) FILTER (WHERE reached_completed)::bigint AS completed_sessions,
        count(*) FILTER (WHERE reached_ready_report)::bigint
            AS ready_session_report_sessions,
        count(*) FILTER (WHERE reached_ended_early)::bigint
            AS ended_early_sessions
    FROM monotonic_steps
    GROUP BY date_trunc('day', created_at, 'UTC')
)
SELECT
    day_utc,
    created_sessions,
    started_sessions,
    first_effective_confirmed_sessions,
    completed_sessions,
    ready_session_report_sessions,
    ended_early_sessions,
    started_sessions::double precision
        / NULLIF(created_sessions, 0) AS started_from_created_rate,
    first_effective_confirmed_sessions::double precision
        / NULLIF(started_sessions, 0) AS first_effective_from_started_rate,
    completed_sessions::double precision
        / NULLIF(first_effective_confirmed_sessions, 0)
            AS completed_from_first_effective_rate,
    ready_session_report_sessions::double precision
        / NULLIF(completed_sessions, 0) AS ready_report_from_completed_rate,
    ended_early_sessions::double precision
        / NULLIF(started_sessions, 0) AS ended_early_from_started_rate
FROM daily;

CREATE VIEW user_behavior_daily_time_to_first_effective_turn
WITH (security_barrier = true) AS
WITH valid_effective_turns AS (
    SELECT
        session.user_id,
        session.session_id,
        turn.confirmed_at
    FROM practice_sessions AS session
    JOIN practice_turns AS turn
      ON turn.session_id = session.session_id
     AND turn.status = 'confirmed'
     AND turn.turn_kind = 'EFFECTIVE'
     AND session.started_at IS NOT NULL
     AND turn.confirmed_at >= session.started_at
), account_first_turn AS (
    SELECT user_id, min(confirmed_at) AS confirmed_at
    FROM valid_effective_turns
    GROUP BY user_id
), account_daily AS (
    SELECT
        date_trunc('day', account.created_at, 'UTC') AS day_utc,
        'ACCOUNT'::text AS scope,
        count(*)::bigint AS eligible_subjects,
        count(first_turn.confirmed_at) FILTER (
            WHERE first_turn.confirmed_at >= account.created_at
        )::bigint AS reached_subjects,
        percentile_cont(0.50) WITHIN GROUP (
            ORDER BY extract(
                epoch FROM first_turn.confirmed_at - account.created_at
            )::double precision
        ) FILTER (
            WHERE first_turn.confirmed_at >= account.created_at
        ) AS p50_seconds,
        percentile_cont(0.95) WITHIN GROUP (
            ORDER BY extract(
                epoch FROM first_turn.confirmed_at - account.created_at
            )::double precision
        ) FILTER (
            WHERE first_turn.confirmed_at >= account.created_at
        ) AS p95_seconds
    FROM users AS account
    LEFT JOIN account_first_turn AS first_turn
      ON first_turn.user_id = account.id
    GROUP BY date_trunc('day', account.created_at, 'UTC')
), session_first_turn AS (
    SELECT session_id, min(confirmed_at) AS confirmed_at
    FROM valid_effective_turns
    GROUP BY session_id
), session_daily AS (
    SELECT
        date_trunc('day', session.created_at, 'UTC') AS day_utc,
        'SESSION'::text AS scope,
        count(*)::bigint AS eligible_subjects,
        count(first_turn.confirmed_at) FILTER (
            WHERE first_turn.confirmed_at >= session.created_at
        )::bigint AS reached_subjects,
        percentile_cont(0.50) WITHIN GROUP (
            ORDER BY extract(
                epoch FROM first_turn.confirmed_at - session.created_at
            )::double precision
        ) FILTER (
            WHERE first_turn.confirmed_at >= session.created_at
        ) AS p50_seconds,
        percentile_cont(0.95) WITHIN GROUP (
            ORDER BY extract(
                epoch FROM first_turn.confirmed_at - session.created_at
            )::double precision
        ) FILTER (
            WHERE first_turn.confirmed_at >= session.created_at
        ) AS p95_seconds
    FROM practice_sessions AS session
    LEFT JOIN session_first_turn AS first_turn
      ON first_turn.session_id = session.session_id
    GROUP BY date_trunc('day', session.created_at, 'UTC')
), daily AS (
    SELECT * FROM account_daily
    UNION ALL
    SELECT * FROM session_daily
)
SELECT
    day_utc,
    scope,
    eligible_subjects,
    reached_subjects,
    reached_subjects::double precision
        / NULLIF(eligible_subjects, 0) AS reach_rate,
    p50_seconds,
    p95_seconds
FROM daily;

CREATE VIEW user_behavior_daily_retention
WITH (security_barrier = true) AS
WITH first_effective_turn AS (
    SELECT
        session.user_id,
        min(turn.confirmed_at) AS confirmed_at
    FROM practice_sessions AS session
    JOIN practice_turns AS turn
      ON turn.session_id = session.session_id
     AND turn.status = 'confirmed'
     AND turn.turn_kind = 'EFFECTIVE'
     AND session.started_at IS NOT NULL
     AND turn.confirmed_at >= session.started_at
    GROUP BY session.user_id
), cohort AS (
    SELECT
        user_id,
        date_trunc('day', confirmed_at, 'UTC') AS cohort_day_utc
    FROM first_effective_turn
), daily_activity AS (
    SELECT DISTINCT
        session.user_id,
        date_trunc('day', turn.confirmed_at, 'UTC') AS activity_day_utc
    FROM practice_sessions AS session
    JOIN practice_turns AS turn
      ON turn.session_id = session.session_id
     AND turn.status = 'confirmed'
), cohort_counts AS (
    SELECT
        cohort.cohort_day_utc,
        count(DISTINCT cohort.user_id)::bigint AS cohort_users,
        count(DISTINCT cohort.user_id) FILTER (
            WHERE activity.activity_day_utc = cohort.cohort_day_utc
                + interval '1 day'
        )::bigint AS d1_returned_users,
        count(DISTINCT cohort.user_id) FILTER (
            WHERE activity.activity_day_utc = cohort.cohort_day_utc
                + interval '7 days'
        )::bigint AS d7_returned_users
    FROM cohort
    LEFT JOIN daily_activity AS activity
      ON activity.user_id = cohort.user_id
     AND activity.activity_day_utc IN (
         cohort.cohort_day_utc + interval '1 day',
         cohort.cohort_day_utc + interval '7 days'
     )
    GROUP BY cohort.cohort_day_utc
)
SELECT
    cohort_day_utc,
    cohort_users,
    CASE
        WHEN date_trunc('day', CURRENT_TIMESTAMP, 'UTC')
            >= cohort_day_utc + interval '2 days'
        THEN d1_returned_users
        ELSE NULL
    END AS d1_returned_users,
    CASE
        WHEN date_trunc('day', CURRENT_TIMESTAMP, 'UTC')
            >= cohort_day_utc + interval '2 days'
        THEN d1_returned_users::double precision / NULLIF(cohort_users, 0)
        ELSE NULL
    END AS d1_retention_rate,
    CASE
        WHEN date_trunc('day', CURRENT_TIMESTAMP, 'UTC')
            >= cohort_day_utc + interval '8 days'
        THEN d7_returned_users
        ELSE NULL
    END AS d7_returned_users,
    CASE
        WHEN date_trunc('day', CURRENT_TIMESTAMP, 'UTC')
            >= cohort_day_utc + interval '8 days'
        THEN d7_returned_users::double precision / NULLIF(cohort_users, 0)
        ELSE NULL
    END AS d7_retention_rate
FROM cohort_counts;

CREATE VIEW user_behavior_daily_repractice
WITH (security_barrier = true) AS
WITH daily AS (
    SELECT
        date_trunc('day', created_at, 'UTC') AS day_utc,
        count(*)::bigint AS created_retry_turns,
        count(*) FILTER (WHERE status = 'confirmed')::bigint
            AS confirmed_retry_turns,
        count(*) FILTER (WHERE status = 'failed')::bigint
            AS failed_retry_turns,
        count(*) FILTER (
            WHERE status IN ('answering', 'transcribing', 'transcribed')
              AND updated_at <= CURRENT_TIMESTAMP - interval '24 hours'
        )::bigint AS stale_retry_turns
    FROM practice_turns
    WHERE turn_kind = 'RETRY'
    GROUP BY date_trunc('day', created_at, 'UTC')
)
SELECT
    day_utc,
    created_retry_turns,
    confirmed_retry_turns,
    failed_retry_turns,
    stale_retry_turns,
    confirmed_retry_turns::double precision
        / NULLIF(created_retry_turns, 0) AS confirmation_rate,
    failed_retry_turns::double precision
        / NULLIF(created_retry_turns, 0) AS failure_rate,
    stale_retry_turns::double precision
        / NULLIF(created_retry_turns, 0) AS stale_rate
FROM daily;

CREATE VIEW user_behavior_daily_early_end
WITH (security_barrier = true) AS
WITH bucketed AS (
    SELECT
        date_trunc('day', ended_at, 'UTC') AS day_utc,
        CASE effective_turns
            WHEN 0 THEN '0'
            WHEN 1 THEN '1'
            WHEN 2 THEN '2'
            WHEN 3 THEN '3'
            WHEN 4 THEN '4'
            ELSE '5_PLUS'
        END::text AS effective_turn_depth
    FROM practice_sessions
    WHERE status = 'ended_early'
), daily AS (
    SELECT
        day_utc,
        effective_turn_depth,
        count(*)::bigint AS ended_early_sessions
    FROM bucketed
    GROUP BY day_utc, effective_turn_depth
)
SELECT
    day_utc,
    effective_turn_depth,
    ended_early_sessions,
    ended_early_sessions::double precision
        / NULLIF(
            sum(ended_early_sessions) OVER (PARTITION BY day_utc),
            0
        ) AS ended_early_share
FROM daily;

CREATE VIEW user_behavior_current_nonterminal_sessions
WITH (security_barrier = true) AS
SELECT
    CASE status
        WHEN 'starting' THEN 'STARTING'
        WHEN 'in_progress' THEN 'IN_PROGRESS'
        WHEN 'paused' THEN 'PAUSED'
    END::text AS session_status,
    CASE
        WHEN updated_at > CURRENT_TIMESTAMP - interval '1 hour'
            THEN 'UNDER_1H'
        WHEN updated_at > CURRENT_TIMESTAMP - interval '24 hours'
            THEN '1H_TO_24H'
        WHEN updated_at > CURRENT_TIMESTAMP - interval '7 days'
            THEN '1D_TO_7D'
        ELSE '7D_PLUS'
    END::text AS staleness_bucket,
    count(*)::bigint AS sessions
FROM practice_sessions
WHERE status IN ('starting', 'in_progress', 'paused')
GROUP BY
    CASE status
        WHEN 'starting' THEN 'STARTING'
        WHEN 'in_progress' THEN 'IN_PROGRESS'
        WHEN 'paused' THEN 'PAUSED'
    END,
    CASE
        WHEN updated_at > CURRENT_TIMESTAMP - interval '1 hour'
            THEN 'UNDER_1H'
        WHEN updated_at > CURRENT_TIMESTAMP - interval '24 hours'
            THEN '1H_TO_24H'
        WHEN updated_at > CURRENT_TIMESTAMP - interval '7 days'
            THEN '1D_TO_7D'
        ELSE '7D_PLUS'
    END;

CREATE VIEW user_behavior_daily_feature_usage
WITH (security_barrier = true) AS
WITH confirmed_activity AS NOT MATERIALIZED (
    SELECT
        date_trunc('day', turn.confirmed_at, 'UTC') AS day_utc,
        session.user_id,
        session.session_id,
        session.practice_experience,
        session.scene_category,
        session.practice_mode,
        turn.interaction_mode
    FROM practice_turns AS turn
    JOIN practice_sessions AS session
      ON session.session_id = turn.session_id
    WHERE turn.status = 'confirmed'
), feature_activity AS (
    SELECT
        day_utc,
        user_id,
        session_id,
        'PRACTICE_EXPERIENCE'::text AS feature_kind,
        CASE practice_experience
            WHEN 'INTERVIEW' THEN 'INTERVIEW'
            WHEN 'IELTS_SPEAKING' THEN 'IELTS_SPEAKING'
            WHEN 'WORKPLACE' THEN 'WORKPLACE'
            WHEN 'LIFE_AND_TRAVEL' THEN 'LIFE_AND_TRAVEL'
            ELSE 'UNKNOWN'
        END::text AS feature_value
    FROM confirmed_activity
    UNION ALL
    SELECT
        day_utc,
        user_id,
        session_id,
        'SCENE_CATEGORY'::text,
        CASE scene_category
            WHEN 'INTERVIEW_RECRUITER' THEN 'INTERVIEW_RECRUITER'
            WHEN 'INTERVIEW_BEHAVIORAL' THEN 'INTERVIEW_BEHAVIORAL'
            WHEN 'INTERVIEW_PROFESSIONAL' THEN 'INTERVIEW_PROFESSIONAL'
            WHEN 'INTERVIEW_HIRING_MANAGER' THEN 'INTERVIEW_HIRING_MANAGER'
            WHEN 'INTERVIEW_CUSTOM' THEN 'INTERVIEW_CUSTOM'
            WHEN 'IELTS_SPEAKING' THEN 'IELTS_SPEAKING'
            WHEN 'WORKPLACE_GENERAL' THEN 'WORKPLACE_GENERAL'
            WHEN 'LIFE_TRAVEL' THEN 'LIFE_TRAVEL'
            WHEN 'LIFE_DAILY' THEN 'LIFE_DAILY'
            ELSE 'UNKNOWN'
        END::text
    FROM confirmed_activity
    UNION ALL
    SELECT
        day_utc,
        user_id,
        session_id,
        'PRACTICE_MODE'::text,
        CASE practice_mode
            WHEN 'FULL_SIMULATION' THEN 'FULL_SIMULATION'
            WHEN 'FOCUS' THEN 'FOCUS'
            WHEN 'FULL_MOCK' THEN 'FULL_MOCK'
            WHEN 'PART_1' THEN 'PART_1'
            WHEN 'PART_2' THEN 'PART_2'
            WHEN 'PART_3' THEN 'PART_3'
            ELSE 'UNKNOWN'
        END::text
    FROM confirmed_activity
    UNION ALL
    SELECT
        day_utc,
        user_id,
        session_id,
        'INTERACTION_MODE'::text,
        CASE interaction_mode
            WHEN 'TEXT' THEN 'TEXT'
            WHEN 'PUSH_TO_TALK' THEN 'PUSH_TO_TALK'
            ELSE 'UNKNOWN'
        END::text
    FROM confirmed_activity
)
SELECT
    day_utc,
    feature_kind,
    feature_value,
    count(DISTINCT user_id)::bigint AS active_users,
    count(DISTINCT session_id)::bigint AS sessions,
    count(*)::bigint AS confirmed_turns
FROM feature_activity
GROUP BY day_utc, feature_kind, feature_value;

REVOKE ALL ON user_behavior_daily_session_funnel FROM PUBLIC;
REVOKE ALL ON user_behavior_daily_time_to_first_effective_turn FROM PUBLIC;
REVOKE ALL ON user_behavior_daily_retention FROM PUBLIC;
REVOKE ALL ON user_behavior_daily_repractice FROM PUBLIC;
REVOKE ALL ON user_behavior_daily_early_end FROM PUBLIC;
REVOKE ALL ON user_behavior_current_nonterminal_sessions FROM PUBLIC;
REVOKE ALL ON user_behavior_daily_feature_usage FROM PUBLIC;

COMMIT;
