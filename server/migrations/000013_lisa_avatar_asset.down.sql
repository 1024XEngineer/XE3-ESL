BEGIN;

DO $$
DECLARE
    current_provider TEXT;
    current_profile TEXT;
    current_avatar_id TEXT;
    current_binding_version BIGINT;
BEGIN
    SELECT provider, provider_profile, provider_avatar_id, binding_version
    INTO current_provider, current_profile, current_avatar_id, current_binding_version
    FROM coach_avatar_options
    WHERE id = 'avatar_lisa';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'cannot restore Lisa avatar asset: avatar_lisa is missing';
    END IF;
    IF current_provider <> 'spatialreal'
        OR current_profile <> 'spatialreal_default'
        OR current_avatar_id <> 'ca9c5c22-6dba-4b59-ae3b-d26066f8c017'
        OR current_binding_version <> 2 THEN
        RAISE EXCEPTION 'cannot restore Lisa avatar asset: binding has changed';
    END IF;

    UPDATE coach_avatar_options
    SET provider_avatar_id = '94a60c13-e835-4bde-aa93-00a1cf178dcd',
        binding_version = 1
    WHERE id = 'avatar_lisa';
END
$$;

COMMIT;
