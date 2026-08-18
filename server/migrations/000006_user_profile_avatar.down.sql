BEGIN;

DROP INDEX users_avatar_asset_unique_idx;

ALTER TABLE users
    DROP CONSTRAINT users_avatar_asset_fkey,
    DROP COLUMN avatar_asset_id;

COMMIT;
