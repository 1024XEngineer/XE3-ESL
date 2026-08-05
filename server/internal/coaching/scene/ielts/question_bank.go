package ielts

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

var (
	ErrQuestionBankUnavailable = errors.New("ielts question bank unavailable")
	ErrQuestionSetNotFound     = errors.New("ielts question set not found")
	ErrPracticeModeInvalid     = errors.New("ielts practice mode invalid")
)

//go:embed question_bank.json
var embeddedQuestionBank []byte

type PracticeMode = scene.PracticeMode

const (
	PracticeModeFullMock = scene.PracticeModeFullMock
	PracticeModePart1    = scene.PracticeModePart1
	PracticeModePart2    = scene.PracticeModePart2
	PracticeModePart3    = scene.PracticeModePart3
)

type QuestionBank struct {
	SchemaVersion int                  `json:"schema_version"`
	BankID        string               `json:"bank_id"`
	Season        string               `json:"season"`
	SourceCutoff  string               `json:"source_cutoff"`
	Part1Sets     []Part1Set           `json:"part1_sets"`
	Part1Topics   []Part1PracticeTopic `json:"part1_topics"`
	TopicGroups   []TopicGroup         `json:"topic_groups"`
}

type Part1Set struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Topics        []Part1Topic `json:"topics"`
	QuestionCount int          `json:"question_count"`
	Published     bool         `json:"published"`
}

type Part1Topic struct {
	Title     string   `json:"title"`
	Release   string   `json:"release"`
	Questions []string `json:"questions"`
}

type Part1PracticeTopic struct {
	ID        string   `json:"id"`
	TitleZH   string   `json:"title_zh"`
	TitleEN   string   `json:"title_en"`
	Release   string   `json:"release"`
	Category  string   `json:"category"`
	Questions []string `json:"questions"`
	Published bool     `json:"published"`
}

type TopicGroup struct {
	ID                        string       `json:"id"`
	TitleZH                   string       `json:"title_zh"`
	Release                   string       `json:"release"`
	Region                    string       `json:"region"`
	Category                  string       `json:"category"`
	Part2                     Part2CueCard `json:"part2"`
	Part3Questions            []string     `json:"part3_questions"`
	Published                 bool         `json:"published"`
	SupplementedQuestionCount int          `json:"supplemented_question_count"`
}

type Part2CueCard struct {
	Prompt string   `json:"prompt"`
	Points []string `json:"points"`
}

type QuestionSetSelection struct {
	Mode         PracticeMode
	Part1SetID   string
	TopicGroupID string
}

type ResolvedQuestionSet struct {
	BankID string
	Season string
	Mode   PracticeMode
	Parts  []ResolvedPart
}

// ResolvedPart keeps each IELTS Part's source and turn sequence together.
// Part 2 and Part 3 deliberately share a source ID when they continue the
// same topic group.
type ResolvedPart struct {
	Part           PracticeMode
	SourceID       string
	TopicTitle     string
	CueCard        string
	TurnBlueprints []string
}

type QuestionBankReader interface {
	QuestionBank() (QuestionBank, error)
}

type QuestionSetResolver interface {
	ResolveQuestionSet(QuestionSetSelection) (ResolvedQuestionSet, error)
}

// Bank owns the validated, embedded IELTS question-bank content.
type Bank struct {
	data QuestionBank
}

func NewBank() (*Bank, error) {
	data, err := loadEmbeddedQuestionBank()
	if err != nil {
		return nil, err
	}
	return &Bank{data: data}, nil
}

func (bank *Bank) QuestionBank() (QuestionBank, error) {
	if bank == nil {
		return QuestionBank{}, ErrQuestionBankUnavailable
	}
	return publishedQuestionBank(bank.data), nil
}

func (bank *Bank) ResolveQuestionSet(
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	if bank == nil {
		return ResolvedQuestionSet{}, ErrQuestionBankUnavailable
	}
	return resolveQuestionSet(bank.data, selection)
}

func loadEmbeddedQuestionBank() (QuestionBank, error) {
	var bank QuestionBank
	if err := json.Unmarshal(embeddedQuestionBank, &bank); err != nil {
		return QuestionBank{}, fmt.Errorf(
			"%w: decode embedded data: %v",
			ErrQuestionBankUnavailable,
			err,
		)
	}
	if err := validateQuestionBank(bank); err != nil {
		return QuestionBank{}, err
	}
	return bank, nil
}

func validateQuestionBank(bank QuestionBank) error {
	if bank.SchemaVersion != 2 ||
		!nonBlank(bank.BankID) ||
		!nonBlank(bank.Season) ||
		!nonBlank(bank.SourceCutoff) ||
		len(bank.Part1Sets) != 38 ||
		len(bank.Part1Topics) != 38 {
		return ErrQuestionBankUnavailable
	}
	part1IDs := make(map[string]struct{}, len(bank.Part1Sets))
	part1QuestionsByTopic := make(map[string]map[string]struct{})
	for _, set := range bank.Part1Sets {
		if !validResourceID(set.ID) ||
			!nonBlank(set.Title) ||
			!set.Published ||
			set.QuestionCount != 8 ||
			len(set.Topics) != 3 {
			return ErrQuestionBankUnavailable
		}
		if _, duplicate := part1IDs[set.ID]; duplicate {
			return ErrQuestionBankUnavailable
		}
		part1IDs[set.ID] = struct{}{}
		questionCount := 0
		topicTitles := make(map[string]struct{}, len(set.Topics))
		for _, topic := range set.Topics {
			if !nonBlank(topic.Title) ||
				!nonBlank(topic.Release) ||
				len(topic.Questions) < 2 {
				return ErrQuestionBankUnavailable
			}
			if _, duplicate := topicTitles[topic.Title]; duplicate {
				return ErrQuestionBankUnavailable
			}
			topicTitles[topic.Title] = struct{}{}
			questions, ok := part1QuestionsByTopic[topic.Title]
			if !ok {
				questions = make(map[string]struct{})
				part1QuestionsByTopic[topic.Title] = questions
			}
			for _, question := range topic.Questions {
				if !nonBlank(question) {
					return ErrQuestionBankUnavailable
				}
				questions[question] = struct{}{}
				questionCount++
			}
		}
		if questionCount != set.QuestionCount {
			return ErrQuestionBankUnavailable
		}
	}
	part1QuestionCount := 0
	for _, questions := range part1QuestionsByTopic {
		part1QuestionCount += len(questions)
	}
	if len(part1QuestionsByTopic) != 38 || part1QuestionCount != 234 {
		return ErrQuestionBankUnavailable
	}
	practiceTopicIDs := make(map[string]struct{}, len(bank.Part1Topics))
	practiceTopicTitles := make(map[string]struct{}, len(bank.Part1Topics))
	practiceQuestionCount := 0
	for _, topic := range bank.Part1Topics {
		if !validResourceID(topic.ID) ||
			!nonBlank(topic.TitleZH) ||
			!nonBlank(topic.TitleEN) ||
			!validRelease(topic.Release, true) ||
			!validCategory(topic.Category) ||
			!topic.Published ||
			len(topic.Questions) < 2 {
			return ErrQuestionBankUnavailable
		}
		if _, duplicate := practiceTopicIDs[topic.ID]; duplicate {
			return ErrQuestionBankUnavailable
		}
		if _, duplicate := practiceTopicTitles[topic.TitleEN]; duplicate {
			return ErrQuestionBankUnavailable
		}
		practiceTopicIDs[topic.ID] = struct{}{}
		practiceTopicTitles[topic.TitleEN] = struct{}{}
		canonicalQuestions, exists := part1QuestionsByTopic[topic.TitleEN]
		if !exists || len(canonicalQuestions) != len(topic.Questions) {
			return ErrQuestionBankUnavailable
		}
		questions := make(map[string]struct{}, len(topic.Questions))
		for _, question := range topic.Questions {
			if !nonBlank(question) {
				return ErrQuestionBankUnavailable
			}
			if _, duplicate := questions[question]; duplicate {
				return ErrQuestionBankUnavailable
			}
			if _, canonical := canonicalQuestions[question]; !canonical {
				return ErrQuestionBankUnavailable
			}
			questions[question] = struct{}{}
			practiceQuestionCount++
		}
	}
	if practiceQuestionCount != 234 {
		return ErrQuestionBankUnavailable
	}
	published := 0
	hidden := 0
	publishedPart3Questions := 0
	groupIDs := make(map[string]struct{}, len(bank.TopicGroups))
	publishedTitles := make(map[string]struct{}, 56)
	publishedPrompts := make(map[string]struct{}, 56)
	for _, group := range bank.TopicGroups {
		if !validResourceID(group.ID) ||
			!nonBlank(group.TitleZH) ||
			!nonBlank(group.Release) ||
			!nonBlank(group.Region) ||
			!validCategory(group.Category) ||
			!nonBlank(group.Part2.Prompt) ||
			len(group.Part2.Points) < 3 {
			return ErrQuestionBankUnavailable
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return ErrQuestionBankUnavailable
		}
		groupIDs[group.ID] = struct{}{}
		for _, point := range group.Part2.Points {
			if !nonBlank(point) {
				return ErrQuestionBankUnavailable
			}
		}
		part3Questions := make(map[string]struct{}, len(group.Part3Questions))
		for _, question := range group.Part3Questions {
			if !nonBlank(question) {
				return ErrQuestionBankUnavailable
			}
			if _, duplicate := part3Questions[question]; duplicate {
				return ErrQuestionBankUnavailable
			}
			part3Questions[question] = struct{}{}
		}
		if group.Published {
			published++
			if group.Region != "mainland" ||
				len(group.Part3Questions) < 1 ||
				len(group.Part3Questions) > 6 {
				return ErrQuestionBankUnavailable
			}
			if _, duplicate := publishedTitles[group.TitleZH]; duplicate {
				return ErrQuestionBankUnavailable
			}
			if _, duplicate := publishedPrompts[group.Part2.Prompt]; duplicate {
				return ErrQuestionBankUnavailable
			}
			publishedTitles[group.TitleZH] = struct{}{}
			publishedPrompts[group.Part2.Prompt] = struct{}{}
			publishedPart3Questions += len(group.Part3Questions)
		} else {
			hidden++
		}
	}
	if published != 56 || hidden != 8 || publishedPart3Questions != 317 {
		return ErrQuestionBankUnavailable
	}
	return nil
}

func cloneQuestionBank(source QuestionBank) QuestionBank {
	result := source
	result.Part1Sets = make([]Part1Set, len(source.Part1Sets))
	for index, set := range source.Part1Sets {
		result.Part1Sets[index] = set
		result.Part1Sets[index].Topics = make(
			[]Part1Topic,
			len(set.Topics),
		)
		for topicIndex, topic := range set.Topics {
			result.Part1Sets[index].Topics[topicIndex] = topic
			result.Part1Sets[index].Topics[topicIndex].Questions = append(
				[]string(nil),
				topic.Questions...,
			)
		}
	}
	result.Part1Topics = make(
		[]Part1PracticeTopic,
		len(source.Part1Topics),
	)
	for index, topic := range source.Part1Topics {
		result.Part1Topics[index] = topic
		result.Part1Topics[index].Questions = append(
			[]string(nil),
			topic.Questions...,
		)
	}
	result.TopicGroups = make([]TopicGroup, len(source.TopicGroups))
	for index, group := range source.TopicGroups {
		result.TopicGroups[index] = group
		result.TopicGroups[index].Part2.Points = append(
			[]string(nil),
			group.Part2.Points...,
		)
		result.TopicGroups[index].Part3Questions = append(
			[]string(nil),
			group.Part3Questions...,
		)
	}
	return result
}

func publishedQuestionBank(source QuestionBank) QuestionBank {
	result := cloneQuestionBank(source)
	part1Sets := result.Part1Sets[:0]
	for _, set := range result.Part1Sets {
		if set.Published {
			part1Sets = append(part1Sets, set)
		}
	}
	result.Part1Sets = part1Sets
	part1Topics := result.Part1Topics[:0]
	for _, topic := range result.Part1Topics {
		if topic.Published {
			part1Topics = append(part1Topics, topic)
		}
	}
	result.Part1Topics = part1Topics
	topicGroups := result.TopicGroups[:0]
	for _, group := range result.TopicGroups {
		if group.Published {
			topicGroups = append(topicGroups, group)
		}
	}
	result.TopicGroups = topicGroups
	return result
}

func resolveQuestionSet(
	bank QuestionBank,
	selection QuestionSetSelection,
) (ResolvedQuestionSet, error) {
	var part1Set *Part1Set
	if selection.Part1SetID != "" && selection.Mode == PracticeModeFullMock {
		for index := range bank.Part1Sets {
			if bank.Part1Sets[index].ID == selection.Part1SetID &&
				bank.Part1Sets[index].Published {
				part1Set = &bank.Part1Sets[index]
				break
			}
		}
		if part1Set == nil {
			return ResolvedQuestionSet{}, ErrQuestionSetNotFound
		}
	}
	var part1Topic *Part1PracticeTopic
	if selection.Part1SetID != "" && selection.Mode == PracticeModePart1 {
		for index := range bank.Part1Topics {
			if bank.Part1Topics[index].ID == selection.Part1SetID &&
				bank.Part1Topics[index].Published {
				part1Topic = &bank.Part1Topics[index]
				break
			}
		}
		if part1Topic == nil {
			return ResolvedQuestionSet{}, ErrQuestionSetNotFound
		}
	}
	var topicGroup *TopicGroup
	if selection.TopicGroupID != "" {
		for index := range bank.TopicGroups {
			if bank.TopicGroups[index].ID == selection.TopicGroupID &&
				bank.TopicGroups[index].Published {
				topicGroup = &bank.TopicGroups[index]
				break
			}
		}
		if topicGroup == nil {
			return ResolvedQuestionSet{}, ErrQuestionSetNotFound
		}
	}
	if !validSelectionShape(selection, part1Set, part1Topic, topicGroup) {
		return ResolvedQuestionSet{}, ErrPracticeModeInvalid
	}
	result := ResolvedQuestionSet{
		BankID: bank.BankID,
		Season: bank.Season,
		Mode:   selection.Mode,
	}
	if part1Set != nil {
		part := ResolvedPart{
			Part:     PracticeModePart1,
			SourceID: part1Set.ID,
		}
		for _, topic := range part1Set.Topics {
			for _, question := range topic.Questions {
				part.TurnBlueprints = append(
					part.TurnBlueprints,
					"Part 1 question: "+question,
				)
			}
		}
		result.Parts = append(result.Parts, part)
	}
	if part1Topic != nil {
		part := ResolvedPart{
			Part:     PracticeModePart1,
			SourceID: part1Topic.ID,
		}
		for _, question := range part1Topic.Questions {
			part.TurnBlueprints = append(
				part.TurnBlueprints,
				"Part 1 question: "+question,
			)
		}
		result.Parts = append(result.Parts, part)
	}
	if topicGroup != nil {
		if selection.Mode == PracticeModeFullMock ||
			selection.Mode == PracticeModePart2 {
			cueCard := formatCueCard(topicGroup.Part2)
			result.Parts = append(result.Parts, ResolvedPart{
				Part:           PracticeModePart2,
				SourceID:       topicGroup.ID,
				TopicTitle:     topicGroup.TitleZH,
				CueCard:        cueCard,
				TurnBlueprints: []string{"Part 2 cue card: " + cueCard},
			})
		}
		part3 := ResolvedPart{
			Part:       PracticeModePart3,
			SourceID:   topicGroup.ID,
			TopicTitle: topicGroup.TitleZH,
		}
		for _, question := range topicGroup.Part3Questions {
			part3.TurnBlueprints = append(
				part3.TurnBlueprints,
				"Part 3 question: "+question,
			)
		}
		result.Parts = append(result.Parts, part3)
	}
	return result, nil
}

func validSelectionShape(
	selection QuestionSetSelection,
	part1Set *Part1Set,
	part1Topic *Part1PracticeTopic,
	topicGroup *TopicGroup,
) bool {
	switch selection.Mode {
	case PracticeModeFullMock:
		return part1Set != nil && part1Topic == nil && topicGroup != nil
	case PracticeModePart1:
		return part1Set == nil && part1Topic != nil && topicGroup == nil
	case PracticeModePart2, PracticeModePart3:
		return part1Set == nil && part1Topic == nil && topicGroup != nil
	default:
		return false
	}
}

func validRelease(value string, allowEvergreen bool) bool {
	if value == "new" || value == "carry_over" {
		return true
	}
	return allowEvergreen && value == "evergreen"
}

func validCategory(value string) bool {
	switch value {
	case "person", "place", "thing", "event":
		return true
	default:
		return false
	}
}

func formatCueCard(card Part2CueCard) string {
	var builder strings.Builder
	builder.WriteString(card.Prompt)
	builder.WriteString("\nYou should say:")
	for _, point := range card.Points {
		builder.WriteString("\n• ")
		builder.WriteString(point)
	}
	return builder.String()
}

func validResourceID(value string) bool {
	return value != "" && len(value) <= 128 &&
		strings.TrimSpace(value) == value
}

func nonBlank(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}
