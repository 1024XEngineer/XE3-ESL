BEGIN;

CREATE TABLE ielts_question_bank_versions (
    bank_id text PRIMARY KEY,
    schema_version integer NOT NULL,
    season_code text NOT NULL,
    season_label text NOT NULL,
    season_start date NOT NULL,
    season_end date NOT NULL,
    region text NOT NULL,
    source_cutoff timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    retired_at timestamptz,
    CONSTRAINT ielts_question_bank_versions_id_check
        CHECK (bank_id = btrim(bank_id) AND length(bank_id) BETWEEN 1 AND 128),
    CONSTRAINT ielts_question_bank_versions_schema_check
        CHECK (schema_version = 3),
    CONSTRAINT ielts_question_bank_versions_season_check
        CHECK (
            season_code = btrim(season_code)
            AND length(season_code) BETWEEN 1 AND 64
            AND season_label = btrim(season_label)
            AND length(season_label) BETWEEN 1 AND 128
            AND season_start <= season_end
        ),
    CONSTRAINT ielts_question_bank_versions_region_check
        CHECK (region IN ('mainland', 'international')),
    CONSTRAINT ielts_question_bank_versions_status_check
        CHECK (
            (status = 'draft' AND published_at IS NULL AND retired_at IS NULL)
            OR (status = 'published' AND published_at IS NOT NULL AND retired_at IS NULL)
            OR (status = 'retired' AND published_at IS NOT NULL AND retired_at IS NOT NULL)
        )
);

CREATE UNIQUE INDEX ielts_question_bank_one_published_region_idx
    ON ielts_question_bank_versions (region)
    WHERE status = 'published';

CREATE TABLE ielts_question_bank_sources (
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    source_id text NOT NULL,
    source_url text NOT NULL,
    captured_at timestamptz NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (bank_id, source_id),
    UNIQUE (bank_id, display_order),
    CONSTRAINT ielts_question_bank_sources_value_check
        CHECK (
            source_id = btrim(source_id)
            AND length(source_id) BETWEEN 1 AND 128
            AND source_url = btrim(source_url)
            AND length(source_url) BETWEEN 1 AND 2048
            AND display_order > 0
        )
);

CREATE TABLE ielts_question_bank_tags (
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    tag_code text NOT NULL,
    label_zh text NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (bank_id, tag_code),
    UNIQUE (bank_id, display_order),
    CONSTRAINT ielts_question_bank_tags_value_check
        CHECK (
            tag_code ~ '^[a-z][a-z0-9_]{0,63}$'
            AND label_zh = btrim(label_zh)
            AND length(label_zh) BETWEEN 1 AND 64
            AND display_order > 0
        )
);

CREATE TABLE ielts_part1_topics (
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    topic_id text NOT NULL,
    title_zh text NOT NULL,
    title_en text NOT NULL,
    release_status text NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (bank_id, topic_id),
    UNIQUE (bank_id, title_en),
    UNIQUE (bank_id, display_order),
    CONSTRAINT ielts_part1_topics_value_check
        CHECK (
            topic_id = btrim(topic_id)
            AND length(topic_id) BETWEEN 1 AND 128
            AND title_zh = btrim(title_zh)
            AND length(title_zh) BETWEEN 1 AND 256
            AND title_en = btrim(title_en)
            AND length(title_en) BETWEEN 1 AND 256
            AND release_status IN ('new', 'carry_over', 'evergreen')
            AND display_order > 0
        )
);

CREATE TABLE ielts_part1_questions (
    bank_id text NOT NULL,
    topic_id text NOT NULL,
    question_position integer NOT NULL,
    prompt text NOT NULL,
    PRIMARY KEY (bank_id, topic_id, question_position),
    UNIQUE (bank_id, topic_id, prompt),
    FOREIGN KEY (bank_id, topic_id)
        REFERENCES ielts_part1_topics(bank_id, topic_id),
    CONSTRAINT ielts_part1_questions_value_check
        CHECK (
            question_position > 0
            AND prompt = btrim(prompt)
            AND length(prompt) BETWEEN 1 AND 2048
        )
);

CREATE TABLE ielts_part1_topic_tags (
    bank_id text NOT NULL,
    topic_id text NOT NULL,
    tag_code text NOT NULL,
    PRIMARY KEY (bank_id, topic_id, tag_code),
    FOREIGN KEY (bank_id, topic_id)
        REFERENCES ielts_part1_topics(bank_id, topic_id),
    FOREIGN KEY (bank_id, tag_code)
        REFERENCES ielts_question_bank_tags(bank_id, tag_code)
);

CREATE TABLE ielts_part1_sets (
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    set_id text NOT NULL,
    title text NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (bank_id, set_id),
    UNIQUE (bank_id, display_order),
    CONSTRAINT ielts_part1_sets_value_check
        CHECK (
            set_id = btrim(set_id)
            AND length(set_id) BETWEEN 1 AND 128
            AND title = btrim(title)
            AND length(title) BETWEEN 1 AND 256
            AND display_order > 0
        )
);

CREATE TABLE ielts_part1_set_questions (
    bank_id text NOT NULL,
    set_id text NOT NULL,
    question_position integer NOT NULL,
    topic_id text NOT NULL,
    topic_question_position integer NOT NULL,
    PRIMARY KEY (bank_id, set_id, question_position),
    UNIQUE (
        bank_id,
        set_id,
        topic_id,
        topic_question_position
    ),
    FOREIGN KEY (bank_id, set_id)
        REFERENCES ielts_part1_sets(bank_id, set_id),
    FOREIGN KEY (bank_id, topic_id, topic_question_position)
        REFERENCES ielts_part1_questions(
            bank_id,
            topic_id,
            question_position
        ),
    CONSTRAINT ielts_part1_set_questions_position_check
        CHECK (question_position > 0 AND topic_question_position > 0)
);

CREATE TABLE ielts_part23_groups (
    bank_id text NOT NULL REFERENCES ielts_question_bank_versions(bank_id),
    topic_group_id text NOT NULL,
    title_zh text NOT NULL,
    release_status text NOT NULL,
    cue_card_type text NOT NULL,
    cue_card_prompt text NOT NULL,
    cue_card_points jsonb NOT NULL,
    display_order integer NOT NULL,
    PRIMARY KEY (bank_id, topic_group_id),
    UNIQUE (bank_id, title_zh),
    UNIQUE (bank_id, cue_card_prompt),
    UNIQUE (bank_id, display_order),
    CONSTRAINT ielts_part23_groups_value_check
        CHECK (
            topic_group_id = btrim(topic_group_id)
            AND length(topic_group_id) BETWEEN 1 AND 128
            AND title_zh = btrim(title_zh)
            AND length(title_zh) BETWEEN 1 AND 256
            AND release_status IN ('new', 'carry_over')
            AND cue_card_type IN ('person', 'place', 'thing', 'experience')
            AND cue_card_prompt = btrim(cue_card_prompt)
            AND length(cue_card_prompt) BETWEEN 1 AND 2048
            AND jsonb_typeof(cue_card_points) = 'array'
            AND jsonb_array_length(cue_card_points) >= 3
            AND display_order > 0
        )
);

CREATE TABLE ielts_part3_questions (
    bank_id text NOT NULL,
    topic_group_id text NOT NULL,
    question_position integer NOT NULL,
    prompt text NOT NULL,
    PRIMARY KEY (bank_id, topic_group_id, question_position),
    UNIQUE (bank_id, topic_group_id, prompt),
    FOREIGN KEY (bank_id, topic_group_id)
        REFERENCES ielts_part23_groups(bank_id, topic_group_id),
    CONSTRAINT ielts_part3_questions_value_check
        CHECK (
            question_position > 0
            AND prompt = btrim(prompt)
            AND length(prompt) BETWEEN 1 AND 2048
        )
);

CREATE TABLE ielts_part23_group_tags (
    bank_id text NOT NULL,
    topic_group_id text NOT NULL,
    tag_code text NOT NULL,
    PRIMARY KEY (bank_id, topic_group_id, tag_code),
    FOREIGN KEY (bank_id, topic_group_id)
        REFERENCES ielts_part23_groups(bank_id, topic_group_id),
    FOREIGN KEY (bank_id, tag_code)
        REFERENCES ielts_question_bank_tags(bank_id, tag_code)
);

CREATE FUNCTION ielts_reject_published_bank_content_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_bank_id text;
    bank_status text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_bank_id := OLD.bank_id;
    ELSE
        target_bank_id := NEW.bank_id;
    END IF;

    SELECT status INTO bank_status
    FROM ielts_question_bank_versions
    WHERE bank_id = target_bank_id;

    IF bank_status IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'published IELTS question bank content is immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;

CREATE TRIGGER ielts_question_bank_sources_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_question_bank_sources
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_question_bank_tags_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_question_bank_tags
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part1_topics_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part1_topics
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part1_questions_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part1_questions
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part1_topic_tags_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part1_topic_tags
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part1_sets_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part1_sets
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part1_set_questions_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part1_set_questions
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part23_groups_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part23_groups
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part3_questions_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part3_questions
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE TRIGGER ielts_part23_group_tags_immutable
BEFORE INSERT OR UPDATE OR DELETE ON ielts_part23_group_tags
FOR EACH ROW EXECUTE FUNCTION ielts_reject_published_bank_content_mutation();

CREATE FUNCTION ielts_guard_question_bank_version_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status <> 'draft' THEN
            RAISE EXCEPTION 'published IELTS question bank version is immutable'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;

    IF ROW(
        NEW.bank_id,
        NEW.schema_version,
        NEW.season_code,
        NEW.season_label,
        NEW.season_start,
        NEW.season_end,
        NEW.region,
        NEW.source_cutoff,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.bank_id,
        OLD.schema_version,
        OLD.season_code,
        OLD.season_label,
        OLD.season_start,
        OLD.season_end,
        OLD.region,
        OLD.source_cutoff,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'IELTS question bank version metadata is immutable'
            USING ERRCODE = 'check_violation';
    END IF;

    IF NOT (
        (OLD.status = 'draft' AND NEW.status = 'published'
            AND NEW.published_at IS NOT NULL AND NEW.retired_at IS NULL)
        OR (OLD.status = 'published' AND NEW.status = 'retired'
            AND NEW.published_at = OLD.published_at
            AND NEW.retired_at IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'invalid IELTS question bank status transition'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER ielts_question_bank_versions_immutable
BEFORE UPDATE OR DELETE ON ielts_question_bank_versions
FOR EACH ROW EXECUTE FUNCTION ielts_guard_question_bank_version_mutation();

COMMIT;
