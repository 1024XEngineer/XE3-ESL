BEGIN;

ALTER TABLE preparation_practice_plan_revisions
    DISABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

UPDATE preparation_practice_plan_revisions AS revision
SET scene_selection = jsonb_set(
        revision.scene_selection,
        '{scene,practice_options}',
        (
            SELECT jsonb_agg(
                CASE
                    WHEN option.value ->> 'turn_policy_ref' =
                        'interview.practice.turn.v1'
                    THEN jsonb_set(
                        option.value,
                        '{turn_policy_ref}',
                        '"generic.practice.turn.v1"'::jsonb
                    )
                    ELSE option.value
                END
                ORDER BY option.ordinal
            )
            FROM jsonb_array_elements(
                revision.scene_selection #> '{scene,practice_options}'
            ) WITH ORDINALITY AS option(value, ordinal)
        )
    ),
    session_policy = jsonb_set(
        revision.session_policy,
        '{max_follow_ups_per_question}',
        '1'::jsonb
    )
FROM preparation_practice_plans AS plan
WHERE plan.owner_user_id = revision.owner_user_id
  AND plan.plan_id = revision.plan_id
  AND plan.current_revision = revision.revision
  AND plan.status = 'ready'
  AND revision.scene_selection #>> '{scene,practice_experience}' =
      'INTERVIEW'
  AND NOT EXISTS (
      SELECT 1
      FROM practice_sessions AS session
      WHERE session.owner_user_id = plan.owner_user_id
        AND session.plan_id = plan.plan_id
  )
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(
          revision.scene_selection #> '{scene,practice_options}'
      ) AS option(value)
      WHERE option.value ->> 'session_policy_ref' =
          'interview.practice.session.v1'
        AND option.value ->> 'turn_policy_ref' =
          'interview.practice.turn.v1'
  );

ALTER TABLE preparation_practice_plan_revisions
    ENABLE TRIGGER preparation_practice_plan_revisions_are_immutable;

ALTER TABLE coaching_scene_versions
    DISABLE TRIGGER coaching_scene_versions_are_immutable;

UPDATE coaching_scene_versions AS version
SET practice_options = (
    SELECT jsonb_agg(
        CASE
            WHEN option.value ->> 'session_policy_ref' =
                'interview.practice.session.v1'
             AND option.value ->> 'turn_policy_ref' =
                'interview.practice.turn.v1'
            THEN jsonb_set(
                option.value,
                '{turn_policy_ref}',
                '"generic.practice.turn.v1"'::jsonb
            )
            ELSE option.value
        END
        ORDER BY
            (option.value ->> 'display_order')::integer,
            option.value ->> 'practice_option_id'
    )
    FROM jsonb_array_elements(version.practice_options) AS option(value)
)
WHERE version.practice_experience = 'INTERVIEW'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(version.practice_options) AS option(value)
      WHERE option.value ->> 'session_policy_ref' =
          'interview.practice.session.v1'
        AND option.value ->> 'turn_policy_ref' =
          'interview.practice.turn.v1'
  );

ALTER TABLE coaching_scene_versions
    ENABLE TRIGGER coaching_scene_versions_are_immutable;

COMMIT;
