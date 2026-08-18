BEGIN;

ALTER TABLE users
    ADD COLUMN avatar_asset_id uuid;

ALTER TABLE users
    ADD CONSTRAINT users_avatar_asset_fkey
        FOREIGN KEY (avatar_asset_id)
        REFERENCES media_assets (id) ON DELETE SET NULL;

CREATE UNIQUE INDEX users_avatar_asset_unique_idx
    ON users (avatar_asset_id)
    WHERE avatar_asset_id IS NOT NULL;

COMMIT;
