package preparation

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrIELTSQuestionBankUnavailable = errors.New("ielts question bank unavailable")
	ErrIELTSQuestionSetNotFound     = errors.New("ielts question set not found")
	ErrIELTSPracticeModeInvalid     = errors.New("ielts practice mode invalid")
)

//go:embed ielts_question_bank.json
var embeddedIELTSQuestionBank []byte

type IELTSPracticeMode string

const (
	IELTSPracticeModeFullMock IELTSPracticeMode = "FULL_MOCK"
	IELTSPracticeModePart1    IELTSPracticeMode = "PART_1"
	IELTSPracticeModePart2    IELTSPracticeMode = "PART_2"
	IELTSPracticeModePart3    IELTSPracticeMode = "PART_3"
)

type IELTSQuestionBank struct {
	SchemaVersion int               `json:"schema_version"`
	BankID        string            `json:"bank_id"`
	Season        string            `json:"season"`
	SourceCutoff  string            `json:"source_cutoff"`
	Part1Sets     []IELTSPart1Set   `json:"part1_sets"`
	TopicGroups   []IELTSTopicGroup `json:"topic_groups"`
}

type IELTSPart1Set struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Topics        []IELTSPart1Topic `json:"topics"`
	QuestionCount int               `json:"question_count"`
	Published     bool              `json:"published"`
}

type IELTSPart1Topic struct {
	Title     string   `json:"title"`
	Release   string   `json:"release"`
	Questions []string `json:"questions"`
}

type IELTSTopicGroup struct {
	ID                        string            `json:"id"`
	TitleZH                   string            `json:"title_zh"`
	Release                   string            `json:"release"`
	Region                    string            `json:"region"`
	Part2                     IELTSPart2CueCard `json:"part2"`
	Part3Questions            []string          `json:"part3_questions"`
	Published                 bool              `json:"published"`
	SupplementedQuestionCount int               `json:"supplemented_question_count"`
}

type IELTSPart2CueCard struct {
	Prompt string   `json:"prompt"`
	Points []string `json:"points"`
}

type IELTSQuestionSetSelection struct {
	Mode         IELTSPracticeMode
	Part1SetID   string
	TopicGroupID string
}

type IELTSResolvedQuestionSet struct {
	BankID         string
	Season         string
	Mode           IELTSPracticeMode
	Part1SetID     string
	TopicGroupID   string
	TopicTitle     string
	Part2CueCard   string
	TurnBlueprints []string
	Part1Questions int
	Part2Questions int
	Part3Questions int
}

type IELTSQuestionBankReader interface {
	IELTSQuestionBank() (IELTSQuestionBank, error)
	ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection,
	) (IELTSResolvedQuestionSet, error)
}

func loadEmbeddedIELTSQuestionBank() (IELTSQuestionBank, error) {
	var bank IELTSQuestionBank
	if err := json.Unmarshal(embeddedIELTSQuestionBank, &bank); err != nil {
		return IELTSQuestionBank{}, fmt.Errorf(
			"%w: decode embedded data: %v",
			ErrIELTSQuestionBankUnavailable,
			err,
		)
	}
	if err := validateIELTSQuestionBank(bank); err != nil {
		return IELTSQuestionBank{}, err
	}
	return bank, nil
}

func validateIELTSQuestionBank(bank IELTSQuestionBank) error {
	if bank.SchemaVersion != 1 ||
		!nonBlank(bank.BankID) ||
		!nonBlank(bank.Season) ||
		!nonBlank(bank.SourceCutoff) ||
		len(bank.Part1Sets) != 38 {
		return ErrIELTSQuestionBankUnavailable
	}
	part1IDs := make(map[string]struct{}, len(bank.Part1Sets))
	part1QuestionsByTopic := make(map[string]map[string]struct{})
	for _, set := range bank.Part1Sets {
		if !validResourceID(set.ID) ||
			!nonBlank(set.Title) ||
			!set.Published ||
			set.QuestionCount != 8 ||
			len(set.Topics) != 3 {
			return ErrIELTSQuestionBankUnavailable
		}
		if _, duplicate := part1IDs[set.ID]; duplicate {
			return ErrIELTSQuestionBankUnavailable
		}
		part1IDs[set.ID] = struct{}{}
		questionCount := 0
		topicTitles := make(map[string]struct{}, len(set.Topics))
		for _, topic := range set.Topics {
			if !nonBlank(topic.Title) ||
				!nonBlank(topic.Release) ||
				len(topic.Questions) < 2 {
				return ErrIELTSQuestionBankUnavailable
			}
			if _, duplicate := topicTitles[topic.Title]; duplicate {
				return ErrIELTSQuestionBankUnavailable
			}
			topicTitles[topic.Title] = struct{}{}
			questions, ok := part1QuestionsByTopic[topic.Title]
			if !ok {
				questions = make(map[string]struct{})
				part1QuestionsByTopic[topic.Title] = questions
			}
			for _, question := range topic.Questions {
				if !nonBlank(question) {
					return ErrIELTSQuestionBankUnavailable
				}
				questions[question] = struct{}{}
				questionCount++
			}
		}
		if questionCount != set.QuestionCount {
			return ErrIELTSQuestionBankUnavailable
		}
	}
	part1QuestionCount := 0
	for _, questions := range part1QuestionsByTopic {
		part1QuestionCount += len(questions)
	}
	if len(part1QuestionsByTopic) != 38 || part1QuestionCount != 234 {
		return ErrIELTSQuestionBankUnavailable
	}
	published := 0
	hidden := 0
	groupIDs := make(map[string]struct{}, len(bank.TopicGroups))
	for _, group := range bank.TopicGroups {
		if !validResourceID(group.ID) ||
			!nonBlank(group.TitleZH) ||
			!nonBlank(group.Release) ||
			!nonBlank(group.Region) ||
			!nonBlank(group.Part2.Prompt) ||
			len(group.Part2.Points) < 3 {
			return ErrIELTSQuestionBankUnavailable
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return ErrIELTSQuestionBankUnavailable
		}
		groupIDs[group.ID] = struct{}{}
		for _, point := range group.Part2.Points {
			if !nonBlank(point) {
				return ErrIELTSQuestionBankUnavailable
			}
		}
		for _, question := range group.Part3Questions {
			if !nonBlank(question) {
				return ErrIELTSQuestionBankUnavailable
			}
		}
		if group.Published {
			published++
			if group.Region != "mainland" ||
				len(group.Part3Questions) < 1 ||
				len(group.Part3Questions) > 5 {
				return ErrIELTSQuestionBankUnavailable
			}
		} else {
			hidden++
		}
	}
	if published != 56 || hidden != 8 {
		return ErrIELTSQuestionBankUnavailable
	}
	return nil
}

func cloneIELTSQuestionBank(source IELTSQuestionBank) IELTSQuestionBank {
	result := source
	result.Part1Sets = make([]IELTSPart1Set, len(source.Part1Sets))
	for index, set := range source.Part1Sets {
		result.Part1Sets[index] = set
		result.Part1Sets[index].Topics = make(
			[]IELTSPart1Topic,
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
	result.TopicGroups = make([]IELTSTopicGroup, len(source.TopicGroups))
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

func publishedIELTSQuestionBank(source IELTSQuestionBank) IELTSQuestionBank {
	result := cloneIELTSQuestionBank(source)
	part1Sets := result.Part1Sets[:0]
	for _, set := range result.Part1Sets {
		if set.Published {
			part1Sets = append(part1Sets, set)
		}
	}
	result.Part1Sets = part1Sets
	topicGroups := result.TopicGroups[:0]
	for _, group := range result.TopicGroups {
		if group.Published {
			topicGroups = append(topicGroups, group)
		}
	}
	result.TopicGroups = topicGroups
	return result
}

func resolveIELTSQuestionSet(
	bank IELTSQuestionBank,
	selection IELTSQuestionSetSelection,
) (IELTSResolvedQuestionSet, error) {
	var part1Set *IELTSPart1Set
	if selection.Part1SetID != "" {
		for index := range bank.Part1Sets {
			if bank.Part1Sets[index].ID == selection.Part1SetID &&
				bank.Part1Sets[index].Published {
				part1Set = &bank.Part1Sets[index]
				break
			}
		}
		if part1Set == nil {
			return IELTSResolvedQuestionSet{}, ErrIELTSQuestionSetNotFound
		}
	}
	var topicGroup *IELTSTopicGroup
	if selection.TopicGroupID != "" {
		for index := range bank.TopicGroups {
			if bank.TopicGroups[index].ID == selection.TopicGroupID &&
				bank.TopicGroups[index].Published {
				topicGroup = &bank.TopicGroups[index]
				break
			}
		}
		if topicGroup == nil {
			return IELTSResolvedQuestionSet{}, ErrIELTSQuestionSetNotFound
		}
	}
	if !validIELTSSelectionShape(selection, part1Set, topicGroup) {
		return IELTSResolvedQuestionSet{}, ErrIELTSPracticeModeInvalid
	}
	result := IELTSResolvedQuestionSet{
		BankID:         bank.BankID,
		Season:         bank.Season,
		Mode:           selection.Mode,
		Part1SetID:     selection.Part1SetID,
		TopicGroupID:   selection.TopicGroupID,
		Part1Questions: 0,
		Part2Questions: 0,
		Part3Questions: 0,
	}
	if part1Set != nil {
		for _, topic := range part1Set.Topics {
			for _, question := range topic.Questions {
				result.TurnBlueprints = append(
					result.TurnBlueprints,
					"Part 1 question: "+question,
				)
				result.Part1Questions++
			}
		}
	}
	if topicGroup != nil {
		result.TopicTitle = topicGroup.TitleZH
		result.Part2CueCard = formatIELTSCueCard(topicGroup.Part2)
		if selection.Mode == IELTSPracticeModeFullMock ||
			selection.Mode == IELTSPracticeModePart2 {
			result.TurnBlueprints = append(
				result.TurnBlueprints,
				"Part 2 cue card: "+result.Part2CueCard,
			)
			result.Part2Questions = 1
		}
		for _, question := range topicGroup.Part3Questions {
			result.TurnBlueprints = append(
				result.TurnBlueprints,
				"Part 3 question: "+question,
			)
			result.Part3Questions++
		}
	}
	return result, nil
}

func validIELTSSelectionShape(
	selection IELTSQuestionSetSelection,
	part1Set *IELTSPart1Set,
	topicGroup *IELTSTopicGroup,
) bool {
	switch selection.Mode {
	case IELTSPracticeModeFullMock:
		return part1Set != nil && topicGroup != nil
	case IELTSPracticeModePart1:
		return part1Set != nil && topicGroup == nil
	case IELTSPracticeModePart2, IELTSPracticeModePart3:
		return part1Set == nil && topicGroup != nil
	default:
		return false
	}
}

func formatIELTSCueCard(card IELTSPart2CueCard) string {
	var builder strings.Builder
	builder.WriteString(card.Prompt)
	builder.WriteString("\nYou should say:")
	for _, point := range card.Points {
		builder.WriteString("\n• ")
		builder.WriteString(point)
	}
	return builder.String()
}
