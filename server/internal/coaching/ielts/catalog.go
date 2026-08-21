package ielts

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var catalogResourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var catalogTagCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type catalogDocument struct {
	SchemaVersion int                  `json:"schema_version"`
	BankID        string               `json:"bank_id"`
	Season        string               `json:"season"`
	SeasonLabel   string               `json:"season_label"`
	SeasonStart   string               `json:"season_start"`
	SeasonEnd     string               `json:"season_end"`
	Region        string               `json:"region"`
	SourceCutoff  string               `json:"source_cutoff"`
	Sources       []catalogSource      `json:"sources"`
	Tags          []catalogTag         `json:"tags"`
	Part1Topics   []Part1PracticeTopic `json:"part1_topics"`
	Part1Sets     []catalogPart1Set    `json:"part1_sets"`
	TopicGroups   []TopicGroup         `json:"topic_groups"`
}

type catalogSource struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	CapturedAt string `json:"captured_at"`
}

type catalogTag struct {
	Code    string `json:"code"`
	LabelZH string `json:"label_zh"`
}

type catalogPart1Set struct {
	ID        string                    `json:"id"`
	Title     string                    `json:"title"`
	Questions []catalogPart1QuestionRef `json:"questions"`
}

type catalogPart1QuestionRef struct {
	TopicID          string `json:"topic_id"`
	QuestionPosition int    `json:"question_position"`
}

// Catalog is the immutable in-memory authority for the bundled question bank.
type Catalog struct {
	bank            QuestionBank
	part1TopicIndex map[string]int
	part1Sets       []catalogPart1Set
	part1SetIndex   map[string]int
	topicGroupIndex map[string]int
}

// LoadCatalog decodes and validates a complete versioned question bank.
func LoadCatalog(reader io.Reader) (*Catalog, error) {
	if reader == nil {
		return nil, ErrQuestionBankInvalid
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document catalogDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrQuestionBankInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(
			"%w: trailing JSON content",
			ErrQuestionBankInvalid,
		)
	}
	if err := validateCatalogDocument(document); err != nil {
		return nil, err
	}
	sourceCutoff, _ := time.Parse(time.RFC3339, document.SourceCutoff)
	bank := QuestionBank{
		SchemaVersion: document.SchemaVersion,
		BankID:        document.BankID,
		Season:        document.Season,
		SeasonLabel:   document.SeasonLabel,
		SeasonStart:   document.SeasonStart,
		SeasonEnd:     document.SeasonEnd,
		SourceCutoff:  sourceCutoff.UTC(),
		Filters: CatalogFilters{
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
			CueCardTypes: []FilterOption{
				{Code: "person", Label: "人物"},
				{Code: "place", Label: "地点"},
				{Code: "thing", Label: "事物"},
				{Code: "experience", Label: "经历"},
			},
		},
		Part1Topics: clonePart1Topics(document.Part1Topics),
		TopicGroups: cloneTopicGroups(document.TopicGroups),
	}
	for _, tag := range document.Tags {
		bank.Filters.TopicTags = append(bank.Filters.TopicTags, FilterOption{
			Code: tag.Code, Label: tag.LabelZH,
		})
	}
	catalog := &Catalog{
		bank:            bank,
		part1TopicIndex: make(map[string]int, len(bank.Part1Topics)),
		part1Sets:       clonePart1Sets(document.Part1Sets),
		part1SetIndex:   make(map[string]int, len(document.Part1Sets)),
		topicGroupIndex: make(map[string]int, len(bank.TopicGroups)),
	}
	for index, topic := range bank.Part1Topics {
		catalog.part1TopicIndex[topic.ID] = index
	}
	for index, set := range catalog.part1Sets {
		catalog.part1SetIndex[set.ID] = index
	}
	for index, group := range bank.TopicGroups {
		catalog.topicGroupIndex[group.ID] = index
	}
	return catalog, nil
}

func (catalog *Catalog) QuestionBank(ctx context.Context) (QuestionBank, error) {
	if err := catalogContextError(ctx, catalog); err != nil {
		return QuestionBank{}, err
	}
	return cloneQuestionBank(catalog.bank), nil
}

func (catalog *Catalog) ResolveQuestionSet(
	ctx context.Context,
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	if err := catalogContextError(ctx, catalog); err != nil {
		return ResolvedQuestionSet{}, err
	}
	return catalog.resolveQuestionSet(selection)
}

func (catalog *Catalog) AssignQuestionSet(
	ctx context.Context,
	mode PracticeMode,
	cueCardType string,
) (ResolvedQuestionSet, error) {
	if err := catalogContextError(ctx, catalog); err != nil {
		return ResolvedQuestionSet{}, err
	}
	if (cueCardType != "" && !validCueCardType(cueCardType)) ||
		(mode == PracticeModeFullMock && cueCardType != "") {
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}
	selection := QuestionSetSelection{Mode: mode}
	switch mode {
	case PracticeModeFullMock:
		setIndex, err := randomIndex(len(catalog.part1Sets))
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		groupIndex, err := randomIndex(len(catalog.bank.TopicGroups))
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		selection.Part1SetID = catalog.part1Sets[setIndex].ID
		selection.TopicGroupID = catalog.bank.TopicGroups[groupIndex].ID
	case PracticeModePart1:
		candidates := make([]string, 0, len(catalog.bank.Part1Topics))
		for _, topic := range catalog.bank.Part1Topics {
			if cueCardType == "" || topic.CueCardType == cueCardType {
				candidates = append(candidates, topic.ID)
			}
		}
		selected, err := randomCandidate(candidates)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		selection.Part1SetID = selected
	case PracticeModePart2, PracticeModePart3:
		candidates := make([]string, 0, len(catalog.bank.TopicGroups))
		for _, group := range catalog.bank.TopicGroups {
			if cueCardType == "" || group.CueCardType == cueCardType {
				candidates = append(candidates, group.ID)
			}
		}
		selected, err := randomCandidate(candidates)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		selection.TopicGroupID = selected
	default:
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}
	return catalog.resolveQuestionSet(selection)
}

func (catalog *Catalog) ResolveQuestion(
	ctx context.Context,
	reference QuestionReference,
) (ResolvedQuestion, error) {
	if err := catalogContextError(ctx, catalog); err != nil {
		return ResolvedQuestion{}, err
	}
	if !validQuestionReference(reference) {
		return ResolvedQuestion{}, ErrQuestionBankInvalid
	}
	if reference.BankID != catalog.bank.BankID {
		return ResolvedQuestion{}, ErrQuestionSetNotFound
	}
	var prompt string
	switch reference.Part {
	case PracticeModePart1:
		index, found := catalog.part1TopicIndex[reference.SourceID]
		if !found || reference.QuestionPosition >
			len(catalog.bank.Part1Topics[index].Questions) {
			return ResolvedQuestion{}, ErrQuestionSetNotFound
		}
		prompt = catalog.bank.Part1Topics[index].Questions[reference.QuestionPosition-1]
	case PracticeModePart2:
		index, found := catalog.topicGroupIndex[reference.SourceID]
		if !found {
			return ResolvedQuestion{}, ErrQuestionSetNotFound
		}
		prompt = formatCueCard(catalog.bank.TopicGroups[index].Part2)
	case PracticeModePart3:
		index, found := catalog.topicGroupIndex[reference.SourceID]
		if !found || reference.QuestionPosition >
			len(catalog.bank.TopicGroups[index].Part3Questions) {
			return ResolvedQuestion{}, ErrQuestionSetNotFound
		}
		prompt = catalog.bank.TopicGroups[index].Part3Questions[reference.QuestionPosition-1]
	}
	return ResolvedQuestion{Reference: reference, Prompt: prompt}, nil
}

func (catalog *Catalog) resolveQuestionSet(
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	result := ResolvedQuestionSet{
		BankID: catalog.bank.BankID,
		Season: catalog.bank.Season,
		Mode:   selection.Mode,
	}
	switch selection.Mode {
	case PracticeModeFullMock:
		if selection.Part1SetID == "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part1, err := catalog.resolvePart1Set(selection.Part1SetID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		part2, part3, err := catalog.resolvePart23Group(selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part1, part2, part3}
	case PracticeModePart1:
		if selection.Part1SetID == "" || selection.TopicGroupID != "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part, err := catalog.resolvePart1Topic(selection.Part1SetID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part}
	case PracticeModePart2:
		if selection.Part1SetID != "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		part2, part3, err := catalog.resolvePart23Group(selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part2, part3}
	case PracticeModePart3:
		if selection.Part1SetID != "" || selection.TopicGroupID == "" {
			return ResolvedQuestionSet{}, ErrPracticeModeInvalid
		}
		_, part3, err := catalog.resolvePart23Group(selection.TopicGroupID)
		if err != nil {
			return ResolvedQuestionSet{}, err
		}
		result.Parts = []ResolvedPart{part3}
	default:
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}
	return result, nil
}

func (catalog *Catalog) resolvePart1Topic(topicID string) (ResolvedPart, error) {
	index, found := catalog.part1TopicIndex[topicID]
	if !found {
		return ResolvedPart{}, ErrQuestionSetNotFound
	}
	topic := catalog.bank.Part1Topics[index]
	part := ResolvedPart{
		Part:       PracticeModePart1,
		SourceID:   topic.ID,
		TopicTitle: topic.TitleZH,
	}
	for _, prompt := range topic.Questions {
		part.TurnBlueprints = append(
			part.TurnBlueprints,
			"Part 1 question: "+prompt,
		)
	}
	return part, nil
}

func (catalog *Catalog) resolvePart1Set(setID string) (ResolvedPart, error) {
	setIndex, found := catalog.part1SetIndex[setID]
	if !found {
		return ResolvedPart{}, ErrQuestionSetNotFound
	}
	part := ResolvedPart{Part: PracticeModePart1, SourceID: setID}
	for _, reference := range catalog.part1Sets[setIndex].Questions {
		topic := catalog.bank.Part1Topics[catalog.part1TopicIndex[reference.TopicID]]
		part.TurnBlueprints = append(
			part.TurnBlueprints,
			"Part 1 question: "+topic.Questions[reference.QuestionPosition-1],
		)
	}
	return part, nil
}

func (catalog *Catalog) resolvePart23Group(
	groupID string,
) (ResolvedPart, ResolvedPart, error) {
	index, found := catalog.topicGroupIndex[groupID]
	if !found {
		return ResolvedPart{}, ResolvedPart{}, ErrQuestionSetNotFound
	}
	group := catalog.bank.TopicGroups[index]
	card := formatCueCard(group.Part2)
	part2 := ResolvedPart{
		Part:           PracticeModePart2,
		SourceID:       group.ID,
		TopicTitle:     group.TitleZH,
		CueCard:        card,
		TurnBlueprints: []string{"Part 2 cue card: " + card},
	}
	part3 := ResolvedPart{
		Part:       PracticeModePart3,
		SourceID:   group.ID,
		TopicTitle: group.TitleZH,
	}
	for _, prompt := range group.Part3Questions {
		part3.TurnBlueprints = append(
			part3.TurnBlueprints,
			"Part 3 question: "+prompt,
		)
	}
	return part2, part3, nil
}

func validateCatalogDocument(document catalogDocument) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: %s", ErrQuestionBankInvalid, reason)
	}
	if document.SchemaVersion != 4 || !validCatalogID(document.BankID) ||
		!validCatalogID(document.Season) || strings.TrimSpace(document.SeasonLabel) == "" ||
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
		if !validCatalogID(source.ID) || strings.TrimSpace(source.URL) == "" {
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
		if !catalogTagCodePattern.MatchString(tag.Code) ||
			strings.TrimSpace(tag.LabelZH) == "" {
			return invalid("invalid tag")
		}
		if _, duplicate := tagCodes[tag.Code]; duplicate {
			return invalid("duplicate tag code")
		}
		tagCodes[tag.Code] = struct{}{}
	}
	topics := map[string]Part1PracticeTopic{}
	topicTitles := map[string]struct{}{}
	for _, topic := range document.Part1Topics {
		if !validCatalogID(topic.ID) || strings.TrimSpace(topic.TitleZH) == "" ||
			strings.TrimSpace(topic.TitleEN) == "" ||
			!validReleaseStatus(topic.ReleaseStatus, true) ||
			!validCueCardType(topic.CueCardType) || len(topic.TagCodes) == 0 ||
			len(topic.Questions) < 2 {
			return invalid("invalid Part 1 topic")
		}
		if _, duplicate := topics[topic.ID]; duplicate {
			return invalid("duplicate Part 1 topic id")
		}
		if _, duplicate := topicTitles[topic.TitleEN]; duplicate {
			return invalid("duplicate Part 1 topic title")
		}
		if !validTagReferences(topic.TagCodes, tagCodes) ||
			!validPrompts(topic.Questions) {
			return invalid("invalid Part 1 topic tags or questions")
		}
		topics[topic.ID] = topic
		topicTitles[topic.TitleEN] = struct{}{}
	}
	setIDs := map[string]struct{}{}
	for _, set := range document.Part1Sets {
		if !validCatalogID(set.ID) || strings.TrimSpace(set.Title) == "" ||
			len(set.Questions) != 8 {
			return invalid("invalid Part 1 set")
		}
		if _, duplicate := setIDs[set.ID]; duplicate {
			return invalid("duplicate Part 1 set id")
		}
		setIDs[set.ID] = struct{}{}
		references := map[catalogPart1QuestionRef]struct{}{}
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
		if !validCatalogID(group.ID) || strings.TrimSpace(group.TitleZH) == "" ||
			!validReleaseStatus(group.ReleaseStatus, false) ||
			!validCueCardType(group.CueCardType) || len(group.TagCodes) == 0 ||
			strings.TrimSpace(group.Part2.Prompt) == "" ||
			len(group.Part2.Points) < 3 || len(group.Part3Questions) < 1 ||
			len(group.Part3Questions) > 6 {
			return invalid("invalid Part 2/3 topic group")
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return invalid("duplicate Part 2/3 topic group id")
		}
		if _, duplicate := groupPrompts[group.Part2.Prompt]; duplicate {
			return invalid("duplicate Part 2 cue card")
		}
		if !validTagReferences(group.TagCodes, tagCodes) ||
			!validPrompts(group.Part2.Points) ||
			!validPrompts(group.Part3Questions) {
			return invalid("invalid Part 2/3 tags or prompts")
		}
		groupIDs[group.ID] = struct{}{}
		groupPrompts[group.Part2.Prompt] = struct{}{}
	}
	return nil
}

func validCatalogID(value string) bool {
	return catalogResourceIDPattern.MatchString(value)
}

func validReleaseStatus(value string, allowEvergreen bool) bool {
	return value == "new" || value == "carry_over" ||
		(allowEvergreen && value == "evergreen")
}

func validCueCardType(value string) bool {
	return value == "person" || value == "place" || value == "thing" ||
		value == "experience"
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

func catalogContextError(ctx context.Context, catalog *Catalog) error {
	if catalog == nil || ctx == nil {
		return ErrQuestionBankUnavailable
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrQuestionBankUnavailable, err)
	}
	return nil
}

func randomCandidate(candidates []string) (string, error) {
	index, err := randomIndex(len(candidates))
	if err != nil {
		return "", err
	}
	return candidates[index], nil
}

func randomIndex(length int) (int, error) {
	if length == 0 {
		return 0, ErrQuestionSetNotFound
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return 0, fmt.Errorf("%w: choose question set", ErrQuestionBankUnavailable)
	}
	return int(value.Int64()), nil
}

func cloneQuestionBank(source QuestionBank) QuestionBank {
	result := source
	result.Filters.Releases = append([]FilterOption(nil), source.Filters.Releases...)
	result.Filters.Parts = append([]FilterOption(nil), source.Filters.Parts...)
	result.Filters.TopicTags = append([]FilterOption(nil), source.Filters.TopicTags...)
	result.Filters.CueCardTypes = append([]FilterOption(nil), source.Filters.CueCardTypes...)
	result.Part1Topics = clonePart1Topics(source.Part1Topics)
	result.TopicGroups = cloneTopicGroups(source.TopicGroups)
	return result
}

func clonePart1Topics(source []Part1PracticeTopic) []Part1PracticeTopic {
	result := make([]Part1PracticeTopic, len(source))
	for index, topic := range source {
		result[index] = topic
		result[index].TagCodes = append([]string(nil), topic.TagCodes...)
		result[index].Questions = append([]string(nil), topic.Questions...)
	}
	return result
}

func cloneTopicGroups(source []TopicGroup) []TopicGroup {
	result := make([]TopicGroup, len(source))
	for index, group := range source {
		result[index] = group
		result[index].TagCodes = append([]string(nil), group.TagCodes...)
		result[index].Part2.Points = append([]string(nil), group.Part2.Points...)
		result[index].Part3Questions = append(
			[]string(nil),
			group.Part3Questions...,
		)
	}
	return result
}

func clonePart1Sets(source []catalogPart1Set) []catalogPart1Set {
	result := make([]catalogPart1Set, len(source))
	for index, set := range source {
		result[index] = set
		result[index].Questions = append(
			[]catalogPart1QuestionRef(nil),
			set.Questions...,
		)
	}
	return result
}

var _ QuestionBankReader = (*Catalog)(nil)
var _ QuestionSetResolver = (*Catalog)(nil)
var _ QuestionResolver = (*Catalog)(nil)
