BEGIN;

ALTER TABLE preparation_profiles
    ADD COLUMN preparation_kind text,
    ADD COLUMN preparation_context jsonb,
    ADD CONSTRAINT preparation_profiles_context_shape_check
        CHECK (
            (
                preparation_kind IS NULL
                AND preparation_context IS NULL
            )
            OR
            (
                preparation_kind IN ('interview', 'scenario')
                AND jsonb_typeof(preparation_context) = 'object'
                AND preparation_context ->> 'kind' = preparation_kind
                AND octet_length(preparation_context::text) <= 65536
            )
        );

ALTER TABLE preparation_snapshots
    ADD COLUMN preparation_kind text,
    ADD COLUMN preparation_context jsonb,
    ADD CONSTRAINT preparation_snapshots_context_shape_check
        CHECK (
            (
                preparation_kind IS NULL
                AND preparation_context IS NULL
            )
            OR
            (
                preparation_kind IN ('interview', 'scenario')
                AND jsonb_typeof(preparation_context) = 'object'
                AND preparation_context ->> 'kind' = preparation_kind
                AND octet_length(preparation_context::text) <= 65536
            )
        );

COMMIT;
