BEGIN;

CREATE TABLE conversation_audio_assets (
    audio_asset_id text PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    upload_request_id text NOT NULL,
    object_key text NOT NULL,
    candidate_id text,
    turn_id text,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_sha256 text NOT NULL,
    duration_ns bigint NOT NULL,
    etag text NOT NULL DEFAULT '',
    status text NOT NULL,
    staged_until timestamptz NOT NULL,
    upload_lease_until timestamptz,
    upload_fencing_token bigint NOT NULL DEFAULT 0,
    cleanup_lease_until timestamptz,
    cleanup_fencing_token bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    version bigint NOT NULL,
    CONSTRAINT conversation_audio_assets_owner_user_id_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT conversation_audio_assets_owner_upload_request_key
        UNIQUE (owner_user_id, upload_request_id),
    CONSTRAINT conversation_audio_assets_object_key_key
        UNIQUE (object_key),
    CONSTRAINT conversation_audio_assets_id_length_check
        CHECK (octet_length(audio_asset_id) BETWEEN 1 AND 128),
    CONSTRAINT conversation_audio_assets_upload_request_length_check
        CHECK (octet_length(upload_request_id) BETWEEN 1 AND 128),
    CONSTRAINT conversation_audio_assets_object_key_check
        CHECK (
            octet_length(object_key) BETWEEN 22 AND 1024
            AND object_key LIKE 'audio/v1/assets/%.wav'
            AND object_key NOT LIKE '%..%'
            AND object_key !~ '[[:cntrl:]\\]'
        ),
    CONSTRAINT conversation_audio_assets_binding_lengths_check
        CHECK (
            (candidate_id IS NULL OR octet_length(candidate_id) BETWEEN 1 AND 128)
            AND (turn_id IS NULL OR octet_length(turn_id) BETWEEN 1 AND 128)
        ),
    CONSTRAINT conversation_audio_assets_binding_pair_check
        CHECK ((candidate_id IS NULL) = (turn_id IS NULL)),
    CONSTRAINT conversation_audio_assets_content_type_check
        CHECK (content_type IN ('audio/wav', 'audio/x-wav')),
    CONSTRAINT conversation_audio_assets_size_check
        CHECK (size_bytes > 0),
    CONSTRAINT conversation_audio_assets_checksum_check
        CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT conversation_audio_assets_duration_check
        CHECK (duration_ns > 0),
    CONSTRAINT conversation_audio_assets_etag_length_check
        CHECK (octet_length(etag) <= 512),
    CONSTRAINT conversation_audio_assets_staged_etag_check
        CHECK (status <> 'staged' OR etag = ''),
    CONSTRAINT conversation_audio_assets_committed_etag_check
        CHECK (
            status NOT IN ('metadata_committed', 'readable')
            OR btrim(etag) <> ''
        ),
    CONSTRAINT conversation_audio_assets_status_check
        CHECK (
            status IN (
                'staged',
                'metadata_committed',
                'readable',
                'deleting',
                'deleted'
            )
        ),
    CONSTRAINT conversation_audio_assets_readable_binding_check
        CHECK (
            status <> 'readable'
            OR (candidate_id IS NOT NULL AND turn_id IS NOT NULL)
        ),
    CONSTRAINT conversation_audio_assets_unconfirmed_binding_check
        CHECK (
            status NOT IN ('staged', 'metadata_committed')
            OR (candidate_id IS NULL AND turn_id IS NULL)
        ),
    CONSTRAINT conversation_audio_assets_cleanup_lease_check
        CHECK (
            cleanup_lease_until IS NULL
            OR status = 'deleting'
        ),
    CONSTRAINT conversation_audio_assets_upload_lease_check
        CHECK (
            upload_lease_until IS NULL
            OR status = 'staged'
        ),
    CONSTRAINT conversation_audio_assets_upload_fence_check
        CHECK (upload_fencing_token >= 0),
    CONSTRAINT conversation_audio_assets_cleanup_fence_check
        CHECK (cleanup_fencing_token >= 0),
    CONSTRAINT conversation_audio_assets_timestamps_check
        CHECK (
            staged_until > created_at
            AND updated_at >= created_at
        ),
    CONSTRAINT conversation_audio_assets_deleted_at_check
        CHECK (
            (
                status = 'deleted'
                AND deleted_at IS NOT NULL
                AND deleted_at >= created_at
            )
            OR (status <> 'deleted' AND deleted_at IS NULL)
        ),
    CONSTRAINT conversation_audio_assets_version_check
        CHECK (version > 0)
);

CREATE UNIQUE INDEX conversation_audio_assets_owner_candidate_key
    ON conversation_audio_assets (owner_user_id, candidate_id)
    WHERE candidate_id IS NOT NULL;

CREATE UNIQUE INDEX conversation_audio_assets_owner_turn_key
    ON conversation_audio_assets (owner_user_id, turn_id)
    WHERE turn_id IS NOT NULL;

CREATE INDEX conversation_audio_assets_expired_cleanup_idx
    ON conversation_audio_assets (
        staged_until,
        cleanup_lease_until,
        audio_asset_id
    )
    WHERE (
        status IN ('staged', 'metadata_committed')
        AND candidate_id IS NULL
        AND turn_id IS NULL
    );

CREATE INDEX conversation_audio_assets_upload_recovery_idx
    ON conversation_audio_assets (
        upload_lease_until,
        audio_asset_id
    )
    WHERE (
        status = 'staged'
        AND upload_lease_until IS NOT NULL
    );

CREATE INDEX conversation_audio_assets_deleting_cleanup_idx
    ON conversation_audio_assets (
        cleanup_lease_until,
        updated_at,
        audio_asset_id
    )
    WHERE status = 'deleting';

CREATE INDEX conversation_audio_assets_owner_cleanup_idx
    ON conversation_audio_assets (
        owner_user_id,
        updated_at,
        audio_asset_id
    )
    WHERE status <> 'deleted';

CREATE INDEX conversation_audio_assets_owner_deleted_purge_idx
    ON conversation_audio_assets (
        owner_user_id,
        deleted_at,
        audio_asset_id
    )
    WHERE status = 'deleted';

COMMIT;
