BEGIN;

CREATE TABLE evaluation_deletion_fences (
    owner_user_id uuid PRIMARY KEY,
    deletion_generation bigint NOT NULL CHECK (deletion_generation > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (updated_at >= created_at)
);

CREATE FUNCTION evaluation_evidence_refs_are_consistent(
    expected_snapshot_id text,
    payload jsonb
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    reference_item jsonb;
    turn_item jsonb;
    asr_item jsonb;
    version_item jsonb;
    reference_turn_id text;
BEGIN
    IF jsonb_typeof(payload -> 'confirmed_turns') <> 'array'
       OR jsonb_typeof(payload -> 'evidence_refs') <> 'array'
       OR jsonb_typeof(payload #> '{provider_lineage,asr}') <> 'array'
       OR jsonb_typeof(
           payload #> '{version_manifest,turn_evidence}'
       ) <> 'array'
    THEN
        RETURN false;
    END IF;
    IF jsonb_array_length(payload -> 'confirmed_turns') = 0
       OR jsonb_array_length(payload -> 'evidence_refs')
            <> jsonb_array_length(payload -> 'confirmed_turns')
       OR jsonb_array_length(payload #> '{provider_lineage,asr}')
            <> jsonb_array_length(payload -> 'confirmed_turns')
       OR jsonb_array_length(
           payload #> '{version_manifest,turn_evidence}'
       ) <> jsonb_array_length(payload -> 'confirmed_turns')
    THEN
        RETURN false;
    END IF;
    IF (
        SELECT count(DISTINCT item ->> 'turn_id')
        FROM jsonb_array_elements(
            payload -> 'confirmed_turns'
        ) AS item
    ) <> jsonb_array_length(payload -> 'confirmed_turns')
       OR (
        SELECT count(DISTINCT item ->> 'turn_id')
        FROM jsonb_array_elements(payload -> 'evidence_refs') AS item
    ) <> jsonb_array_length(payload -> 'evidence_refs')
       OR (
        SELECT count(DISTINCT item ->> 'evidence_ref_id')
        FROM jsonb_array_elements(payload -> 'evidence_refs') AS item
    ) <> jsonb_array_length(payload -> 'evidence_refs')
       OR (
        SELECT count(DISTINCT item ->> 'turn_id')
        FROM jsonb_array_elements(
            payload #> '{provider_lineage,asr}'
        ) AS item
    ) <> jsonb_array_length(payload #> '{provider_lineage,asr}')
       OR (
        SELECT count(DISTINCT item ->> 'turn_id')
        FROM jsonb_array_elements(
            payload #> '{version_manifest,turn_evidence}'
        ) AS item
    ) <> jsonb_array_length(
        payload #> '{version_manifest,turn_evidence}'
    )
    THEN
        RETURN false;
    END IF;

    FOR reference_item IN
        SELECT value
        FROM jsonb_array_elements(payload -> 'evidence_refs')
    LOOP
        reference_turn_id := reference_item ->> 'turn_id';
        SELECT value
          INTO turn_item
          FROM jsonb_array_elements(
              payload -> 'confirmed_turns'
          )
         WHERE value ->> 'turn_id' = reference_turn_id;
        SELECT value
          INTO asr_item
          FROM jsonb_array_elements(
              payload #> '{provider_lineage,asr}'
          )
         WHERE value ->> 'turn_id' = reference_turn_id;
        SELECT value
          INTO version_item
          FROM jsonb_array_elements(
              payload #> '{version_manifest,turn_evidence}'
          )
         WHERE value ->> 'turn_id' = reference_turn_id;
        IF turn_item IS NULL OR asr_item IS NULL OR version_item IS NULL
           OR reference_item ->> 'snapshot_id' <> expected_snapshot_id
           OR reference_item #>> '{lineage,transcript_id}'
                <> turn_item #>> '{transcript,transcript_id}'
           OR reference_item #>> '{lineage,transcript_id}'
                <> asr_item ->> 'transcript_id'
           OR reference_item #>> '{lineage,candidate_id}'
                <> asr_item ->> 'candidate_id'
           OR reference_item #>> '{lineage,evidence_version}'
                <> turn_item #>> '{transcript,evidence_version}'
           OR reference_item #>> '{lineage,evidence_version}'
                <> asr_item ->> 'evidence_version'
           OR reference_item #>> '{lineage,evidence_version}'
                <> version_item ->> 'evidence_version'
           OR reference_item #>> '{lineage,asr_provider}'
                <> asr_item ->> 'provider'
           OR reference_item #>> '{lineage,asr_model}'
                <> asr_item ->> 'model'
           OR reference_item #>> '{transcript_span,start_utf8_byte}'
                <> '0'
           OR (
                reference_item #>>
                    '{transcript_span,end_utf8_byte}'
              )::integer <> octet_length(
                turn_item #>> '{transcript,text}'
              )
        THEN
            RETURN false;
        END IF;
        IF turn_item #>> '{audio,availability}' = 'AVAILABLE' THEN
            IF reference_item -> 'audio_span' IS NULL
               OR reference_item #>> '{audio_span,audio_asset_id}'
                    <> turn_item #>> '{audio,audio_asset_id}'
               OR reference_item #>> '{audio_span,start_ms}' <> '0'
               OR reference_item #>> '{audio_span,end_ms}'
                    <> turn_item #>> '{audio,duration_ms}'
               OR reference_item #>>
                    '{lineage,audio_asset_version}'
                    <> turn_item #>> '{audio,version}'
               OR version_item ->> 'audio_version'
                    <> turn_item #>> '{audio,version}'
            THEN
                RETURN false;
            END IF;
        ELSE
            IF reference_item ? 'audio_span'
               OR coalesce(
                    reference_item #>>
                        '{lineage,audio_asset_version}',
                    '0'
                  ) <> '0'
               OR coalesce(version_item ->> 'audio_version', '0')
                    <> coalesce(turn_item #>> '{audio,version}', '0')
            THEN
                RETURN false;
            END IF;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN others THEN
        RETURN false;
END;
$$;

CREATE TABLE evaluation_evidence_snapshots (
    id text COLLATE "C" PRIMARY KEY DEFAULT (
        'snapshot_' || replace(gen_random_uuid()::text, '-', '')
    ),
    owner_user_id uuid NOT NULL,
    practice_session_id text COLLATE "C" NOT NULL,
    scope text COLLATE "C" NOT NULL,
    scene_type text COLLATE "C" NOT NULL,
    input_revision integer NOT NULL,
    source_manifest_hash bytea NOT NULL,
    snapshot_hash bytea NOT NULL,
    canonical_payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT evaluation_evidence_snapshots_owner_fkey
        FOREIGN KEY (owner_user_id)
        REFERENCES identity_users (id)
        ON DELETE RESTRICT,
    CONSTRAINT evaluation_evidence_snapshots_owner_identity_unique
        UNIQUE (id, owner_user_id),
    CONSTRAINT evaluation_evidence_snapshots_id_check
        CHECK (id ~ '^[A-Za-z][A-Za-z0-9_-]{0,127}$'),
    CONSTRAINT evaluation_evidence_snapshots_source_unique
        UNIQUE (
            owner_user_id,
            practice_session_id,
            scope,
            source_manifest_hash
        ),
    CONSTRAINT evaluation_evidence_snapshots_revision_unique
        UNIQUE (
            owner_user_id,
            practice_session_id,
            scope,
            input_revision
        ),
    CONSTRAINT evaluation_evidence_snapshots_practice_session_check
        CHECK (
            practice_session_id ~
                '^[A-Za-z][A-Za-z0-9._:-]{0,127}$'
        ),
    CONSTRAINT evaluation_evidence_snapshots_scope_check
        CHECK (scope IN ('TURN', 'SESSION')),
    CONSTRAINT evaluation_evidence_snapshots_scene_type_check
        CHECK (
            scene_type IN (
                'IELTS_SPEAKING',
                'INTERVIEW',
                'OVERSEAS_DAILY_LIFE',
                'OVERSEAS_WORKPLACE'
            )
        ),
    CONSTRAINT evaluation_evidence_snapshots_revision_check
        CHECK (input_revision > 0),
    CONSTRAINT evaluation_evidence_snapshots_source_hash_check
        CHECK (
            octet_length(source_manifest_hash) = 32
            AND source_manifest_hash <> decode(repeat('00', 32), 'hex')
        ),
    CONSTRAINT evaluation_evidence_snapshots_snapshot_hash_check
        CHECK (octet_length(snapshot_hash) = 32),
    CONSTRAINT evaluation_evidence_snapshots_payload_check
        CHECK (
            jsonb_typeof(canonical_payload) = 'object'
            AND canonical_payload ?& ARRAY[
                'practice_context',
                'opportunity_manifest',
                'confirmed_turns',
                'evidence_refs',
                'provider_lineage',
                'version_manifest'
            ]
            AND (
                canonical_payload - ARRAY[
                    'practice_context',
                    'opportunity_manifest',
                    'confirmed_turns',
                    'evidence_refs',
                    'provider_lineage',
                    'version_manifest'
                ]
            ) = '{}'::jsonb
            AND jsonb_typeof(
                canonical_payload -> 'confirmed_turns'
            ) = 'array'
            AND jsonb_typeof(
                canonical_payload -> 'evidence_refs'
            ) = 'array'
        ),
    CONSTRAINT evaluation_evidence_snapshots_ref_scope_check
        CHECK (
            evaluation_evidence_refs_are_consistent(
                id,
                canonical_payload
            )
        ),
    CONSTRAINT evaluation_evidence_snapshots_no_storage_locator_check
        CHECK (
            NOT jsonb_path_exists(
                canonical_payload,
                '$.** ? (@.type() == "object").keyvalue() ? (
                    @.key like_regex
                    "^(object[-_]?key|signed[-_]?url|audio[-_]?url|url)$"
                    flag "i"
                )'
            )
        )
);

CREATE INDEX evaluation_evidence_snapshots_owner_session_idx
    ON evaluation_evidence_snapshots (
        owner_user_id,
        practice_session_id,
        scope,
        input_revision DESC
    );

CREATE FUNCTION reject_evaluation_evidence_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owner_status text;
    deletion_fenced boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT account_status
          INTO owner_status
          FROM identity_users
         WHERE id = OLD.owner_user_id;
        IF owner_status IN ('deleting', 'deleted') THEN
            RETURN OLD;
        END IF;
        IF owner_status IS NULL THEN
            SELECT EXISTS (
                SELECT 1
                FROM evaluation_deletion_fences
                WHERE owner_user_id = OLD.owner_user_id
            ) INTO deletion_fenced;
            IF deletion_fenced THEN
                RETURN OLD;
            END IF;
        END IF;
    END IF;
    RAISE EXCEPTION 'evaluation evidence snapshots are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER evaluation_evidence_snapshots_immutable
BEFORE UPDATE OR DELETE ON evaluation_evidence_snapshots
FOR EACH ROW
EXECUTE FUNCTION reject_evaluation_evidence_snapshot_mutation();

COMMIT;
