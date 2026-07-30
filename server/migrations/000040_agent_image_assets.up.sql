BEGIN;

SET LOCAL lock_timeout = '15s';
SET LOCAL statement_timeout = '2min';

ALTER TABLE agent_messages
    DROP CONSTRAINT agent_messages_modality_check,
    ADD CONSTRAINT agent_messages_modality_check
        CHECK (modality IN ('text', 'voice', 'multimodal'));

CREATE TABLE agent_image_assets (
    image_asset_id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    upload_request_id text COLLATE "C" NOT NULL,
    object_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    checksum_sha256 text NOT NULL,
    etag text NOT NULL DEFAULT '',
    upload_lease_until timestamptz,
    upload_fencing_token bigint NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'staged',
    expires_at timestamptz NOT NULL,
    cleanup_lease_until timestamptz,
    cleanup_fencing_token bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attached_at timestamptz,
    deleted_at timestamptz,
    CONSTRAINT agent_image_assets_identity_key
        UNIQUE (image_asset_id, owner_user_id, thread_id),
    CONSTRAINT agent_image_assets_owner_upload_key
        UNIQUE (owner_user_id, thread_id, upload_request_id),
    CONSTRAINT agent_image_assets_object_key_key UNIQUE (object_key),
    CONSTRAINT agent_image_assets_thread_owner_fkey
        FOREIGN KEY (thread_id, owner_user_id)
        REFERENCES agent_threads (id, owner_user_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_image_assets_upload_request_check
        CHECK (
            octet_length(upload_request_id) BETWEEN 8 AND 128
            AND upload_request_id !~ '[[:cntrl:]]'
        ),
    CONSTRAINT agent_image_assets_object_key_check
        CHECK (
            octet_length(object_key) BETWEEN 22 AND 1024
            AND object_key LIKE 'image/v1/agent/%'
            AND object_key NOT LIKE '%..%'
            AND object_key !~ '[[:cntrl:]\\]'
        ),
    CONSTRAINT agent_image_assets_metadata_check
        CHECK (
            content_type IN ('image/jpeg', 'image/png', 'image/webp')
            AND size_bytes BETWEEN 1 AND 10485760
            AND width BETWEEN 1 AND 16384
            AND height BETWEEN 1 AND 16384
            AND width::bigint * height::bigint <= 16000000
            AND checksum_sha256 ~ '^[0-9a-f]{64}$'
            AND octet_length(etag) <= 512
        ),
    CONSTRAINT agent_image_assets_status_check
        CHECK (status IN ('staged', 'attached', 'deleting', 'deleted')),
    CONSTRAINT agent_image_assets_lease_check
        CHECK (
            upload_fencing_token >= 0
            AND cleanup_fencing_token >= 0
            AND (
                upload_lease_until IS NULL
                OR (
                    status = 'staged'
                    AND etag = ''
                )
            )
            AND (
                cleanup_lease_until IS NULL
                OR status = 'deleting'
            )
        ),
    CONSTRAINT agent_image_assets_state_shape_check
        CHECK (
            (
                status = 'staged'
                AND attached_at IS NULL
            )
            OR
            (
                status = 'attached'
                AND etag <> ''
                AND upload_lease_until IS NULL
                AND attached_at IS NOT NULL
            )
            OR status IN ('deleting', 'deleted')
        ),
    CONSTRAINT agent_image_assets_timestamps_check
        CHECK (
            expires_at > created_at
            AND updated_at >= created_at
            AND (attached_at IS NULL OR attached_at >= created_at)
            AND (
                (status = 'deleted' AND deleted_at IS NOT NULL)
                OR (status <> 'deleted' AND deleted_at IS NULL)
            )
        )
);

CREATE TABLE agent_message_images (
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    message_id uuid NOT NULL,
    image_asset_id uuid NOT NULL,
    position smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, position),
    CONSTRAINT agent_message_images_asset_key UNIQUE (image_asset_id),
    CONSTRAINT agent_message_images_message_owner_fkey
        FOREIGN KEY (message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE RESTRICT,
    CONSTRAINT agent_message_images_asset_owner_fkey
        FOREIGN KEY (image_asset_id, owner_user_id, thread_id)
        REFERENCES agent_image_assets (
            image_asset_id,
            owner_user_id,
            thread_id
        )
        ON DELETE RESTRICT,
    CONSTRAINT agent_message_images_position_check
        CHECK (position BETWEEN 0 AND 3)
);

CREATE INDEX agent_image_assets_staged_cleanup_idx
    ON agent_image_assets (expires_at, image_asset_id)
    WHERE status = 'staged';

CREATE INDEX agent_image_assets_deleting_cleanup_idx
    ON agent_image_assets (cleanup_lease_until, image_asset_id)
    WHERE status = 'deleting';

CREATE INDEX agent_message_images_thread_message_idx
    ON agent_message_images (owner_user_id, thread_id, message_id, position);

COMMIT;
