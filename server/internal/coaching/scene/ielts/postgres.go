package ielts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) (*PostgresStore, error) {
	if database == nil {
		return nil, errors.New("ielts question bank database is required")
	}
	return &PostgresStore{database: database}, nil
}

func (store *PostgresStore) QuestionBank(
	ctx context.Context,
) (QuestionBank, error) {
	if store == nil || store.database == nil || ctx == nil {
		return QuestionBank{}, ErrQuestionBankUnavailable
	}
	tx, err := store.database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return QuestionBank{}, fmt.Errorf("%w: begin read: %v", ErrQuestionBankUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bank, err := loadPublishedCatalog(ctx, tx)
	if err != nil {
		return QuestionBank{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return QuestionBank{}, fmt.Errorf("%w: commit read: %v", ErrQuestionBankUnavailable, err)
	}
	return bank, nil
}

func (store *PostgresStore) ResolveQuestionSet(
	ctx context.Context,
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	if store == nil || store.database == nil || ctx == nil {
		return ResolvedQuestionSet{}, ErrQuestionBankUnavailable
	}
	tx, err := store.database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return ResolvedQuestionSet{}, fmt.Errorf("%w: begin resolution: %v", ErrQuestionBankUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resolved, err := resolveQuestionSet(ctx, tx, selection)
	if err != nil {
		return ResolvedQuestionSet{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ResolvedQuestionSet{}, fmt.Errorf("%w: commit resolution: %v", ErrQuestionBankUnavailable, err)
	}
	return resolved, nil
}

func (store *PostgresStore) AssignQuestionSet(
	ctx context.Context,
	mode PracticeMode,
) (ResolvedQuestionSet, error) {
	if store == nil || store.database == nil || ctx == nil {
		return ResolvedQuestionSet{}, ErrQuestionBankUnavailable
	}
	tx, err := store.database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return ResolvedQuestionSet{}, fmt.Errorf("%w: begin assignment: %v", ErrQuestionBankUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bankID, _, err := currentPublishedBank(ctx, tx)
	if err != nil {
		return ResolvedQuestionSet{}, err
	}
	selection := QuestionSetSelection{Mode: mode}
	switch mode {
	case PracticeModeFullMock:
		if err := tx.QueryRow(ctx, `
			SELECT set_id
			FROM ielts_part1_sets
			WHERE bank_id = $1
			ORDER BY random()
			LIMIT 1
		`, bankID).Scan(&selection.Part1SetID); err != nil {
			return ResolvedQuestionSet{}, assignmentLookupError(err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT topic_group_id
			FROM ielts_part23_groups
			WHERE bank_id = $1
			ORDER BY random()
			LIMIT 1
		`, bankID).Scan(&selection.TopicGroupID); err != nil {
			return ResolvedQuestionSet{}, assignmentLookupError(err)
		}
	case PracticeModePart1:
		if err := tx.QueryRow(ctx, `
			SELECT topic_id
			FROM ielts_part1_topics
			WHERE bank_id = $1
			ORDER BY random()
			LIMIT 1
		`, bankID).Scan(&selection.Part1SetID); err != nil {
			return ResolvedQuestionSet{}, assignmentLookupError(err)
		}
	case PracticeModePart2, PracticeModePart3:
		if err := tx.QueryRow(ctx, `
			SELECT topic_group_id
			FROM ielts_part23_groups
			WHERE bank_id = $1
			ORDER BY random()
			LIMIT 1
		`, bankID).Scan(&selection.TopicGroupID); err != nil {
			return ResolvedQuestionSet{}, assignmentLookupError(err)
		}
	default:
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}

	resolved, err := resolveQuestionSet(ctx, tx, selection)
	if err != nil {
		return ResolvedQuestionSet{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ResolvedQuestionSet{}, fmt.Errorf("%w: commit assignment: %v", ErrQuestionBankUnavailable, err)
	}
	return resolved, nil
}

func loadPublishedCatalog(
	ctx context.Context,
	tx pgx.Tx,
) (QuestionBank, error) {
	var bank QuestionBank
	var seasonStart time.Time
	var seasonEnd time.Time
	err := tx.QueryRow(ctx, `
		SELECT
			schema_version,
			bank_id,
			season_code,
			season_label,
			season_start,
			season_end,
			source_cutoff
		FROM ielts_question_bank_versions
		WHERE region = 'mainland' AND status = 'published'
	`).Scan(
		&bank.SchemaVersion,
		&bank.BankID,
		&bank.Season,
		&bank.SeasonLabel,
		&seasonStart,
		&seasonEnd,
		&bank.SourceCutoff,
	)
	if err != nil {
		return QuestionBank{}, catalogLookupError(err)
	}
	bank.SeasonStart = seasonStart.Format(time.DateOnly)
	bank.SeasonEnd = seasonEnd.Format(time.DateOnly)
	bank.Filters = CatalogFilters{
		Releases: []FilterOption{
			{Code: "new", Label: "本季新增"},
			{Code: "carry_over", Label: "本季延续"},
			{Code: "evergreen", Label: "常驻话题"},
		},
		Parts: []FilterOption{
			{Code: "PART_1", Label: "Part 1"},
			{Code: "PART_2", Label: "Part 2"},
			{Code: "PART_3", Label: "Part 3"},
		},
		TopicTags: []FilterOption{},
		CueCardTypes: []FilterOption{
			{Code: "person", Label: "人物"},
			{Code: "place", Label: "地点"},
			{Code: "thing", Label: "事物"},
			{Code: "experience", Label: "经历"},
		},
	}

	rows, err := tx.Query(ctx, `
		SELECT topic_id, title_zh, title_en, release_status, cue_card_type
		FROM ielts_part1_topics
		WHERE bank_id = $1
		ORDER BY display_order
	`, bank.BankID)
	if err != nil {
		return QuestionBank{}, fmt.Errorf("%w: read Part 1 topics: %v", ErrQuestionBankUnavailable, err)
	}
	for rows.Next() {
		var topic Part1PracticeTopic
		if err := rows.Scan(
			&topic.ID,
			&topic.TitleZH,
			&topic.TitleEN,
			&topic.ReleaseStatus,
			&topic.CueCardType,
		); err != nil {
			rows.Close()
			return QuestionBank{}, fmt.Errorf("%w: scan Part 1 topic: %v", ErrQuestionBankUnavailable, err)
		}
		bank.Part1Topics = append(bank.Part1Topics, topic)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return QuestionBank{}, fmt.Errorf("%w: iterate Part 1 topics: %v", ErrQuestionBankUnavailable, err)
	}
	rows.Close()
	part1ByID := make(map[string]*Part1PracticeTopic, len(bank.Part1Topics))
	for index := range bank.Part1Topics {
		part1ByID[bank.Part1Topics[index].ID] = &bank.Part1Topics[index]
	}
	if err := loadPart1QuestionsAndTags(ctx, tx, bank.BankID, part1ByID); err != nil {
		return QuestionBank{}, err
	}

	rows, err = tx.Query(ctx, `
		SELECT
			topic_group_id,
			title_zh,
			release_status,
			cue_card_type,
			cue_card_prompt,
			cue_card_points
		FROM ielts_part23_groups
		WHERE bank_id = $1
		ORDER BY display_order
	`, bank.BankID)
	if err != nil {
		return QuestionBank{}, fmt.Errorf("%w: read Part 2/3 groups: %v", ErrQuestionBankUnavailable, err)
	}
	for rows.Next() {
		var group TopicGroup
		var points []byte
		if err := rows.Scan(
			&group.ID,
			&group.TitleZH,
			&group.ReleaseStatus,
			&group.CueCardType,
			&group.Part2.Prompt,
			&points,
		); err != nil {
			rows.Close()
			return QuestionBank{}, fmt.Errorf("%w: scan Part 2/3 group: %v", ErrQuestionBankUnavailable, err)
		}
		if err := json.Unmarshal(points, &group.Part2.Points); err != nil {
			rows.Close()
			return QuestionBank{}, fmt.Errorf("%w: decode cue card points: %v", ErrQuestionBankUnavailable, err)
		}
		bank.TopicGroups = append(bank.TopicGroups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return QuestionBank{}, fmt.Errorf("%w: iterate Part 2/3 groups: %v", ErrQuestionBankUnavailable, err)
	}
	rows.Close()
	groupsByID := make(map[string]*TopicGroup, len(bank.TopicGroups))
	for index := range bank.TopicGroups {
		groupsByID[bank.TopicGroups[index].ID] = &bank.TopicGroups[index]
	}
	if err := loadPart3QuestionsAndTags(ctx, tx, bank.BankID, groupsByID); err != nil {
		return QuestionBank{}, err
	}
	return bank, nil
}

func loadPart1QuestionsAndTags(
	ctx context.Context,
	tx pgx.Tx,
	bankID string,
	topics map[string]*Part1PracticeTopic,
) error {
	rows, err := tx.Query(ctx, `
		SELECT topic_id, prompt
		FROM ielts_part1_questions
		WHERE bank_id = $1
		ORDER BY topic_id, question_position
	`, bankID)
	if err != nil {
		return fmt.Errorf("%w: read Part 1 questions: %v", ErrQuestionBankUnavailable, err)
	}
	for rows.Next() {
		var topicID string
		var prompt string
		if err := rows.Scan(&topicID, &prompt); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scan Part 1 question: %v", ErrQuestionBankUnavailable, err)
		}
		topic := topics[topicID]
		if topic == nil {
			rows.Close()
			return ErrQuestionBankUnavailable
		}
		topic.Questions = append(topic.Questions, prompt)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: iterate Part 1 questions: %v", ErrQuestionBankUnavailable, err)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `
		SELECT topic_id, tag_code
		FROM ielts_part1_topic_tags
		WHERE bank_id = $1
		ORDER BY topic_id, tag_code
	`, bankID)
	if err != nil {
		return fmt.Errorf("%w: read Part 1 tags: %v", ErrQuestionBankUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		var topicID string
		var tagCode string
		if err := rows.Scan(&topicID, &tagCode); err != nil {
			return fmt.Errorf("%w: scan Part 1 tag: %v", ErrQuestionBankUnavailable, err)
		}
		topic := topics[topicID]
		if topic == nil {
			return ErrQuestionBankUnavailable
		}
		topic.TagCodes = append(topic.TagCodes, tagCode)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterate Part 1 tags: %v", ErrQuestionBankUnavailable, err)
	}
	return nil
}

func loadPart3QuestionsAndTags(
	ctx context.Context,
	tx pgx.Tx,
	bankID string,
	groups map[string]*TopicGroup,
) error {
	rows, err := tx.Query(ctx, `
		SELECT topic_group_id, prompt
		FROM ielts_part3_questions
		WHERE bank_id = $1
		ORDER BY topic_group_id, question_position
	`, bankID)
	if err != nil {
		return fmt.Errorf("%w: read Part 3 questions: %v", ErrQuestionBankUnavailable, err)
	}
	for rows.Next() {
		var groupID string
		var prompt string
		if err := rows.Scan(&groupID, &prompt); err != nil {
			rows.Close()
			return fmt.Errorf("%w: scan Part 3 question: %v", ErrQuestionBankUnavailable, err)
		}
		group := groups[groupID]
		if group == nil {
			rows.Close()
			return ErrQuestionBankUnavailable
		}
		group.Part3Questions = append(group.Part3Questions, prompt)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: iterate Part 3 questions: %v", ErrQuestionBankUnavailable, err)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `
		SELECT topic_group_id, tag_code
		FROM ielts_part23_group_tags
		WHERE bank_id = $1
		ORDER BY topic_group_id, tag_code
	`, bankID)
	if err != nil {
		return fmt.Errorf("%w: read Part 2/3 tags: %v", ErrQuestionBankUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		var groupID string
		var tagCode string
		if err := rows.Scan(&groupID, &tagCode); err != nil {
			return fmt.Errorf("%w: scan Part 2/3 tag: %v", ErrQuestionBankUnavailable, err)
		}
		group := groups[groupID]
		if group == nil {
			return ErrQuestionBankUnavailable
		}
		group.TagCodes = append(group.TagCodes, tagCode)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterate Part 2/3 tags: %v", ErrQuestionBankUnavailable, err)
	}
	return nil
}

func resolveQuestionSet(
	ctx context.Context,
	tx pgx.Tx,
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	bankID, season, err := currentPublishedBank(ctx, tx)
	if err != nil {
		return ResolvedQuestionSet{}, err
	}
	result := ResolvedQuestionSet{BankID: bankID, Season: season, Mode: selection.Mode}
	switch selection.Mode {
	case PracticeModeFullMock:
		if selection.Part1SetID == "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part1, err := resolvePart1Set(ctx, tx, bankID, selection.Part1SetID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		part2, part3, err := resolvePart23Group(ctx, tx, bankID, selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part1, part2, part3}
	case PracticeModePart1:
		if selection.Part1SetID == "" || selection.TopicGroupID != "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part, err := resolvePart1Topic(ctx, tx, bankID, selection.Part1SetID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part}
	case PracticeModePart2:
		if selection.Part1SetID != "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part2, part3, err := resolvePart23Group(ctx, tx, bankID, selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part2, part3}
	case PracticeModePart3:
		if selection.Part1SetID != "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		_, part3, err := resolvePart23Group(ctx, tx, bankID, selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part3}
	default:
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}
	return result, nil
}

func currentPublishedBank(
	ctx context.Context,
	tx pgx.Tx,
) (string, string, error) {
	var bankID string
	var season string
	if err := tx.QueryRow(ctx, `
		SELECT bank_id, season_code
		FROM ielts_question_bank_versions
		WHERE region = 'mainland' AND status = 'published'
	`).Scan(&bankID, &season); err != nil {
		return "", "", catalogLookupError(err)
	}
	return bankID, season, nil
}

func resolvePart1Topic(
	ctx context.Context,
	tx pgx.Tx,
	bankID string,
	topicID string,
) (ResolvedPart, error) {
	var title string
	if err := tx.QueryRow(ctx, `
		SELECT title_en
		FROM ielts_part1_topics
		WHERE bank_id = $1 AND topic_id = $2
	`, bankID, topicID).Scan(&title); err != nil {
		return ResolvedPart{}, questionSetLookupError(err)
	}
	rows, err := tx.Query(ctx, `
		SELECT prompt
		FROM ielts_part1_questions
		WHERE bank_id = $1 AND topic_id = $2
		ORDER BY question_position
	`, bankID, topicID)
	if err != nil {
		return ResolvedPart{}, fmt.Errorf("%w: read Part 1 topic: %v", ErrQuestionBankUnavailable, err)
	}
	defer rows.Close()
	part := ResolvedPart{Part: PracticeModePart1, SourceID: topicID}
	for rows.Next() {
		var prompt string
		if err := rows.Scan(&prompt); err != nil {
			return ResolvedPart{}, fmt.Errorf("%w: scan Part 1 topic: %v", ErrQuestionBankUnavailable, err)
		}
		part.TurnBlueprints = append(part.TurnBlueprints, "Part 1 question: "+prompt)
	}
	if err := rows.Err(); err != nil {
		return ResolvedPart{}, fmt.Errorf("%w: iterate Part 1 topic: %v", ErrQuestionBankUnavailable, err)
	}
	return part, nil
}

func resolvePart1Set(
	ctx context.Context,
	tx pgx.Tx,
	bankID string,
	setID string,
) (ResolvedPart, error) {
	rows, err := tx.Query(ctx, `
		SELECT question.prompt
		FROM ielts_part1_set_questions AS member
		JOIN ielts_part1_questions AS question
		  ON question.bank_id = member.bank_id
		 AND question.topic_id = member.topic_id
		 AND question.question_position = member.topic_question_position
		WHERE member.bank_id = $1 AND member.set_id = $2
		ORDER BY member.question_position
	`, bankID, setID)
	if err != nil {
		return ResolvedPart{}, fmt.Errorf("%w: read Part 1 set: %v", ErrQuestionBankUnavailable, err)
	}
	defer rows.Close()
	part := ResolvedPart{Part: PracticeModePart1, SourceID: setID}
	for rows.Next() {
		var prompt string
		if err := rows.Scan(&prompt); err != nil {
			return ResolvedPart{}, fmt.Errorf("%w: scan Part 1 set: %v", ErrQuestionBankUnavailable, err)
		}
		part.TurnBlueprints = append(part.TurnBlueprints, "Part 1 question: "+prompt)
	}
	if err := rows.Err(); err != nil {
		return ResolvedPart{}, fmt.Errorf("%w: iterate Part 1 set: %v", ErrQuestionBankUnavailable, err)
	}
	if len(part.TurnBlueprints) == 0 {
		return ResolvedPart{}, ErrQuestionSetNotFound
	}
	return part, nil
}

func resolvePart23Group(
	ctx context.Context,
	tx pgx.Tx,
	bankID string,
	groupID string,
) (ResolvedPart, ResolvedPart, error) {
	var title string
	var card Part2CueCard
	var points []byte
	if err := tx.QueryRow(ctx, `
		SELECT title_zh, cue_card_prompt, cue_card_points
		FROM ielts_part23_groups
		WHERE bank_id = $1 AND topic_group_id = $2
	`, bankID, groupID).Scan(&title, &card.Prompt, &points); err != nil {
		return ResolvedPart{}, ResolvedPart{}, questionSetLookupError(err)
	}
	if err := json.Unmarshal(points, &card.Points); err != nil {
		return ResolvedPart{}, ResolvedPart{}, fmt.Errorf("%w: decode cue card: %v", ErrQuestionBankUnavailable, err)
	}
	part2 := ResolvedPart{
		Part:           PracticeModePart2,
		SourceID:       groupID,
		TopicTitle:     title,
		CueCard:        formatCueCard(card),
		TurnBlueprints: []string{"Part 2 cue card: " + formatCueCard(card)},
	}
	part3 := ResolvedPart{Part: PracticeModePart3, SourceID: groupID, TopicTitle: title}
	rows, err := tx.Query(ctx, `
		SELECT prompt
		FROM ielts_part3_questions
		WHERE bank_id = $1 AND topic_group_id = $2
		ORDER BY question_position
	`, bankID, groupID)
	if err != nil {
		return ResolvedPart{}, ResolvedPart{}, fmt.Errorf("%w: read Part 3 group: %v", ErrQuestionBankUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		var prompt string
		if err := rows.Scan(&prompt); err != nil {
			return ResolvedPart{}, ResolvedPart{}, fmt.Errorf("%w: scan Part 3 group: %v", ErrQuestionBankUnavailable, err)
		}
		part3.TurnBlueprints = append(part3.TurnBlueprints, "Part 3 question: "+prompt)
	}
	if err := rows.Err(); err != nil {
		return ResolvedPart{}, ResolvedPart{}, fmt.Errorf("%w: iterate Part 3 group: %v", ErrQuestionBankUnavailable, err)
	}
	if len(part3.TurnBlueprints) == 0 {
		return ResolvedPart{}, ResolvedPart{}, ErrQuestionSetNotFound
	}
	return part2, part3, nil
}

func catalogLookupError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuestionBankUnavailable
	}
	return fmt.Errorf("%w: read published version: %v", ErrQuestionBankUnavailable, err)
}

func questionSetLookupError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuestionSetNotFound
	}
	return fmt.Errorf("%w: read question set: %v", ErrQuestionBankUnavailable, err)
}

func assignmentLookupError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuestionBankUnavailable
	}
	return fmt.Errorf("%w: select assignment: %v", ErrQuestionBankUnavailable, err)
}
