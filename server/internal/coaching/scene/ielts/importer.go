package ielts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var importResourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var importTagCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type ImportDocument struct {
	SchemaVersion int                `json:"schema_version"`
	BankID        string             `json:"bank_id"`
	Season        string             `json:"season"`
	SeasonLabel   string             `json:"season_label"`
	SeasonStart   string             `json:"season_start"`
	SeasonEnd     string             `json:"season_end"`
	Region        string             `json:"region"`
	SourceCutoff  string             `json:"source_cutoff"`
	Sources       []ImportSource     `json:"sources"`
	Tags          []ImportTag        `json:"tags"`
	Part1Topics   []ImportPart1Topic `json:"part1_topics"`
	Part1Sets     []ImportPart1Set   `json:"part1_sets"`
	TopicGroups   []ImportTopicGroup `json:"topic_groups"`
}

type ImportSource struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	CapturedAt string `json:"captured_at"`
}

type ImportTag struct {
	Code    string `json:"code"`
	LabelZH string `json:"label_zh"`
}

type ImportPart1Topic struct {
	ID            string   `json:"id"`
	TitleZH       string   `json:"title_zh"`
	TitleEN       string   `json:"title_en"`
	ReleaseStatus string   `json:"release_status"`
	CueCardType   string   `json:"cue_card_type"`
	TagCodes      []string `json:"tag_codes"`
	Questions     []string `json:"questions"`
}

type ImportPart1Set struct {
	ID        string                   `json:"id"`
	Title     string                   `json:"title"`
	Questions []ImportPart1QuestionRef `json:"questions"`
}

type ImportPart1QuestionRef struct {
	TopicID          string `json:"topic_id"`
	QuestionPosition int    `json:"question_position"`
}

type ImportTopicGroup struct {
	ID             string       `json:"id"`
	TitleZH        string       `json:"title_zh"`
	ReleaseStatus  string       `json:"release_status"`
	CueCardType    string       `json:"cue_card_type"`
	TagCodes       []string     `json:"tag_codes"`
	Part2          Part2CueCard `json:"part2"`
	Part3Questions []string     `json:"part3_questions"`
}

type ImportResult struct {
	BankID         string
	Published      bool
	Part1Topics    int
	Part1Questions int
	Part1Sets      int
	TopicGroups    int
	Part3Questions int
}

func (importer *Importer) HasPublishedBank(
	ctx context.Context,
	region string,
) (bool, error) {
	if importer == nil || importer.database == nil || ctx == nil {
		return false, ErrQuestionBankUnavailable
	}
	var exists bool
	err := importer.database.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ielts_question_bank_versions
			WHERE region = $1 AND status = 'published'
		)
	`, region).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("%w: check published bank: %v", ErrQuestionBankUnavailable, err)
	}
	return exists, nil
}

type Importer struct {
	database *pgxpool.Pool
}

func NewImporter(database *pgxpool.Pool) (*Importer, error) {
	if database == nil {
		return nil, errors.New("ielts importer database is required")
	}
	return &Importer{database: database}, nil
}

func DecodeImportDocument(reader io.Reader) (ImportDocument, error) {
	if reader == nil {
		return ImportDocument{}, ErrQuestionBankInvalid
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document ImportDocument
	if err := decoder.Decode(&document); err != nil {
		return ImportDocument{}, fmt.Errorf("%w: decode: %v", ErrQuestionBankInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ImportDocument{}, fmt.Errorf("%w: trailing JSON content", ErrQuestionBankInvalid)
	}
	if err := validateImportDocument(document); err != nil {
		return ImportDocument{}, err
	}
	return document, nil
}

func (importer *Importer) Import(
	ctx context.Context,
	document ImportDocument,
	publish bool,
) (ImportResult, error) {
	if importer == nil || importer.database == nil || ctx == nil {
		return ImportResult{}, ErrQuestionBankUnavailable
	}
	if err := validateImportDocument(document); err != nil {
		return ImportResult{}, err
	}
	seasonStart, _ := time.Parse(time.DateOnly, document.SeasonStart)
	seasonEnd, _ := time.Parse(time.DateOnly, document.SeasonEnd)
	sourceCutoff, _ := time.Parse(time.RFC3339, document.SourceCutoff)

	tx, err := importer.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: begin import: %v", ErrQuestionBankUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO ielts_question_bank_versions (
			bank_id,
			schema_version,
			season_code,
			season_label,
			season_start,
			season_end,
			region,
			source_cutoff
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		document.BankID,
		document.SchemaVersion,
		document.Season,
		document.SeasonLabel,
		seasonStart,
		seasonEnd,
		document.Region,
		sourceCutoff,
	)
	if err != nil {
		return ImportResult{}, importWriteError(err)
	}

	for index, source := range document.Sources {
		capturedAt, _ := time.Parse(time.RFC3339, source.CapturedAt)
		if _, err := tx.Exec(ctx, `
			INSERT INTO ielts_question_bank_sources (
				bank_id, source_id, source_url, captured_at, display_order
			) VALUES ($1, $2, $3, $4, $5)
		`, document.BankID, source.ID, source.URL, capturedAt, index+1); err != nil {
			return ImportResult{}, importWriteError(err)
		}
	}
	for index, tag := range document.Tags {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ielts_question_bank_tags (
				bank_id, tag_code, label_zh, display_order
			) VALUES ($1, $2, $3, $4)
		`, document.BankID, tag.Code, tag.LabelZH, index+1); err != nil {
			return ImportResult{}, importWriteError(err)
		}
	}

	result := ImportResult{BankID: document.BankID, Published: publish}
	for topicIndex, topic := range document.Part1Topics {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ielts_part1_topics (
				bank_id,
				topic_id,
				title_zh,
				title_en,
				release_status,
				cue_card_type,
				display_order
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
			document.BankID,
			topic.ID,
			topic.TitleZH,
			topic.TitleEN,
			topic.ReleaseStatus,
			topic.CueCardType,
			topicIndex+1,
		); err != nil {
			return ImportResult{}, importWriteError(err)
		}
		for questionIndex, prompt := range topic.Questions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ielts_part1_questions (
					bank_id, topic_id, question_position, prompt
				) VALUES ($1, $2, $3, $4)
			`, document.BankID, topic.ID, questionIndex+1, prompt); err != nil {
				return ImportResult{}, importWriteError(err)
			}
			result.Part1Questions++
		}
		for _, tagCode := range topic.TagCodes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ielts_part1_topic_tags (
					bank_id, topic_id, tag_code
				) VALUES ($1, $2, $3)
			`, document.BankID, topic.ID, tagCode); err != nil {
				return ImportResult{}, importWriteError(err)
			}
		}
		result.Part1Topics++
	}

	for setIndex, set := range document.Part1Sets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ielts_part1_sets (
				bank_id, set_id, title, display_order
			) VALUES ($1, $2, $3, $4)
		`, document.BankID, set.ID, set.Title, setIndex+1); err != nil {
			return ImportResult{}, importWriteError(err)
		}
		for questionIndex, reference := range set.Questions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ielts_part1_set_questions (
					bank_id,
					set_id,
					question_position,
					topic_id,
					topic_question_position
				) VALUES ($1, $2, $3, $4, $5)
			`,
				document.BankID,
				set.ID,
				questionIndex+1,
				reference.TopicID,
				reference.QuestionPosition,
			); err != nil {
				return ImportResult{}, importWriteError(err)
			}
		}
		result.Part1Sets++
	}

	for groupIndex, group := range document.TopicGroups {
		points, err := json.Marshal(group.Part2.Points)
		if err != nil {
			return ImportResult{}, fmt.Errorf("%w: encode cue card points: %v", ErrQuestionBankInvalid, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ielts_part23_groups (
				bank_id,
				topic_group_id,
				title_zh,
				release_status,
				cue_card_type,
				cue_card_prompt,
				cue_card_points,
				display_order
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
		`,
			document.BankID,
			group.ID,
			group.TitleZH,
			group.ReleaseStatus,
			group.CueCardType,
			group.Part2.Prompt,
			string(points),
			groupIndex+1,
		); err != nil {
			return ImportResult{}, importWriteError(err)
		}
		for questionIndex, prompt := range group.Part3Questions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ielts_part3_questions (
					bank_id, topic_group_id, question_position, prompt
				) VALUES ($1, $2, $3, $4)
			`, document.BankID, group.ID, questionIndex+1, prompt); err != nil {
				return ImportResult{}, importWriteError(err)
			}
			result.Part3Questions++
		}
		for _, tagCode := range group.TagCodes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ielts_part23_group_tags (
					bank_id, topic_group_id, tag_code
				) VALUES ($1, $2, $3)
			`, document.BankID, group.ID, tagCode); err != nil {
				return ImportResult{}, importWriteError(err)
			}
		}
		result.TopicGroups++
	}

	if publish {
		if _, err := tx.Exec(ctx, `
			UPDATE ielts_question_bank_versions
			SET status = 'retired', retired_at = now()
			WHERE region = $1 AND status = 'published'
		`, document.Region); err != nil {
			return ImportResult{}, importWriteError(err)
		}
		commandTag, err := tx.Exec(ctx, `
			UPDATE ielts_question_bank_versions
			SET status = 'published', published_at = now()
			WHERE bank_id = $1 AND status = 'draft'
		`, document.BankID)
		if err != nil {
			return ImportResult{}, importWriteError(err)
		}
		if commandTag.RowsAffected() != 1 {
			return ImportResult{}, ErrQuestionBankConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportResult{}, importWriteError(err)
	}
	return result, nil
}

func validateImportDocument(document ImportDocument) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: %s", ErrQuestionBankInvalid, reason)
	}
	if document.SchemaVersion != 4 || !validImportID(document.BankID) ||
		!validImportID(document.Season) || strings.TrimSpace(document.SeasonLabel) == "" ||
		(document.Region != "mainland" && document.Region != "international") {
		return invalid("invalid bank metadata")
	}
	seasonStart, err := time.Parse(time.DateOnly, document.SeasonStart)
	if err != nil {
		return invalid("invalid season_start")
	}
	seasonEnd, err := time.Parse(time.DateOnly, document.SeasonEnd)
	if err != nil || seasonEnd.Before(seasonStart) {
		return invalid("invalid season_end")
	}
	if _, err := time.Parse(time.RFC3339, document.SourceCutoff); err != nil {
		return invalid("invalid source_cutoff")
	}
	if len(document.Sources) == 0 || len(document.Tags) == 0 ||
		len(document.Part1Topics) == 0 || len(document.Part1Sets) == 0 ||
		len(document.TopicGroups) == 0 {
		return invalid("question bank collections must not be empty")
	}

	sourceIDs := map[string]struct{}{}
	for _, source := range document.Sources {
		if !validImportID(source.ID) || strings.TrimSpace(source.URL) == "" {
			return invalid("invalid source")
		}
		if _, err := time.Parse(time.RFC3339, source.CapturedAt); err != nil {
			return invalid("invalid source captured_at")
		}
		if _, duplicate := sourceIDs[source.ID]; duplicate {
			return invalid("duplicate source id")
		}
		sourceIDs[source.ID] = struct{}{}
	}

	tagCodes := map[string]struct{}{}
	for _, tag := range document.Tags {
		if !validTagCode(tag.Code) || strings.TrimSpace(tag.LabelZH) == "" {
			return invalid("invalid tag")
		}
		if _, duplicate := tagCodes[tag.Code]; duplicate {
			return invalid("duplicate tag code")
		}
		tagCodes[tag.Code] = struct{}{}
	}

	topics := map[string]ImportPart1Topic{}
	topicTitles := map[string]struct{}{}
	for _, topic := range document.Part1Topics {
		if !validImportID(topic.ID) || strings.TrimSpace(topic.TitleZH) == "" ||
			strings.TrimSpace(topic.TitleEN) == "" ||
			!validReleaseStatus(topic.ReleaseStatus, true) ||
			!validCueCardType(topic.CueCardType) ||
			len(topic.TagCodes) == 0 || len(topic.Questions) < 2 {
			return invalid("invalid Part 1 topic")
		}
		if _, duplicate := topics[topic.ID]; duplicate {
			return invalid("duplicate Part 1 topic id")
		}
		if _, duplicate := topicTitles[topic.TitleEN]; duplicate {
			return invalid("duplicate Part 1 topic title")
		}
		if !validTagReferences(topic.TagCodes, tagCodes) || !validPrompts(topic.Questions) {
			return invalid("invalid Part 1 topic tags or questions")
		}
		topics[topic.ID] = topic
		topicTitles[topic.TitleEN] = struct{}{}
	}

	setIDs := map[string]struct{}{}
	for _, set := range document.Part1Sets {
		if !validImportID(set.ID) || strings.TrimSpace(set.Title) == "" || len(set.Questions) != 8 {
			return invalid("invalid Part 1 set")
		}
		if _, duplicate := setIDs[set.ID]; duplicate {
			return invalid("duplicate Part 1 set id")
		}
		setIDs[set.ID] = struct{}{}
		references := map[ImportPart1QuestionRef]struct{}{}
		topicCount := map[string]struct{}{}
		for _, reference := range set.Questions {
			topic, exists := topics[reference.TopicID]
			if !exists || reference.QuestionPosition < 1 ||
				reference.QuestionPosition > len(topic.Questions) {
				return invalid("Part 1 set references an unknown question")
			}
			if _, duplicate := references[reference]; duplicate {
				return invalid("Part 1 set contains a duplicate question")
			}
			references[reference] = struct{}{}
			topicCount[reference.TopicID] = struct{}{}
		}
		if len(topicCount) != 3 {
			return invalid("Part 1 set must contain exactly three topics")
		}
	}

	groupIDs := map[string]struct{}{}
	groupPrompts := map[string]struct{}{}
	for _, group := range document.TopicGroups {
		if !validImportID(group.ID) || strings.TrimSpace(group.TitleZH) == "" ||
			!validReleaseStatus(group.ReleaseStatus, false) ||
			!validCueCardType(group.CueCardType) || len(group.TagCodes) == 0 ||
			strings.TrimSpace(group.Part2.Prompt) == "" || len(group.Part2.Points) < 3 ||
			len(group.Part3Questions) < 1 || len(group.Part3Questions) > 6 {
			return invalid("invalid Part 2/3 topic group")
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return invalid("duplicate Part 2/3 topic group id")
		}
		if _, duplicate := groupPrompts[group.Part2.Prompt]; duplicate {
			return invalid("duplicate Part 2 cue card")
		}
		if !validTagReferences(group.TagCodes, tagCodes) ||
			!validPrompts(group.Part2.Points) || !validPrompts(group.Part3Questions) {
			return invalid("invalid Part 2/3 tags or prompts")
		}
		groupIDs[group.ID] = struct{}{}
		groupPrompts[group.Part2.Prompt] = struct{}{}
	}
	return nil
}

func validImportID(value string) bool {
	return importResourceIDPattern.MatchString(value)
}

func validTagCode(value string) bool {
	return importTagCodePattern.MatchString(value)
}

func validReleaseStatus(value string, allowEvergreen bool) bool {
	if value == "new" || value == "carry_over" {
		return true
	}
	return allowEvergreen && value == "evergreen"
}

func validCueCardType(value string) bool {
	return value == "person" || value == "place" || value == "thing" || value == "experience"
}

func validTagReferences(values []string, tags map[string]struct{}) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, exists := tags[value]; !exists {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validPrompts(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 2048 {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func importWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "23505" || postgresError.Code == "23514" || postgresError.Code == "23503") {
		return fmt.Errorf("%w: database rejected imported content", ErrQuestionBankConflict)
	}
	return fmt.Errorf("%w: write imported content: %v", ErrQuestionBankUnavailable, err)
}
