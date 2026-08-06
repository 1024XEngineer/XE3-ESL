BEGIN;

CREATE TABLE agent_message_memes (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    thread_id uuid NOT NULL,
    message_id uuid NOT NULL,
    run_id uuid NOT NULL,
    meme_id text COLLATE "C" NOT NULL,
    pack_id text COLLATE "C" NOT NULL,
    pack_version text COLLATE "C" NOT NULL,
    category text COLLATE "C" NOT NULL,
    asset_key text COLLATE "C" NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    checksum_sha256 text COLLATE "C" NOT NULL,
    weight integer NOT NULL DEFAULT 1,
    position integer NOT NULL,
    classification_policy_version text COLLATE "C" NOT NULL,
    selection_policy_version text COLLATE "C" NOT NULL,
    classifier_provider text COLLATE "C" NOT NULL,
    classifier_model text COLLATE "C" NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT agent_message_memes_message_owner_fkey
        FOREIGN KEY (message_id, owner_user_id, thread_id)
        REFERENCES agent_messages (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_message_memes_run_owner_fkey
        FOREIGN KEY (run_id, owner_user_id, thread_id)
        REFERENCES agent_runs (id, owner_user_id, thread_id)
        ON DELETE CASCADE,
    CONSTRAINT agent_message_memes_message_position_key
        UNIQUE (owner_user_id, message_id, position),
    CONSTRAINT agent_message_memes_run_meme_key
        UNIQUE (owner_user_id, run_id, meme_id),
    CONSTRAINT agent_message_memes_position_check CHECK (position BETWEEN 0 AND 3),
    CONSTRAINT agent_message_memes_dimensions_check
        CHECK (
            size_bytes BETWEEN 1 AND 20971520
            AND width BETWEEN 1 AND 16384
            AND height BETWEEN 1 AND 16384
            AND weight BETWEEN 1 AND 1000
        ),
    CONSTRAINT agent_message_memes_content_type_check
        CHECK (content_type IN ('image/gif', 'image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT agent_message_memes_checksum_check
        CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT agent_message_memes_stable_ids_check
        CHECK (
            meme_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND pack_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND pack_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND category ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND classification_policy_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND selection_policy_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            AND classifier_provider ~ '^[a-z][a-z0-9_-]{0,63}$'
            AND classifier_model ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT agent_message_memes_asset_key_check
        CHECK (
            octet_length(asset_key) BETWEEN 1 AND 1024
            AND asset_key !~ '(^/|\\|(^|/)\.\.(/|$)|(^|/)\.(/|$))'
        )
);

CREATE INDEX agent_message_memes_thread_recent_idx
    ON agent_message_memes (owner_user_id, thread_id, created_at DESC, id DESC);

COMMIT;
