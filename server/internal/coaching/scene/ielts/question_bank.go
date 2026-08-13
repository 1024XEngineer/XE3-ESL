package ielts

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

var (
	ErrQuestionBankUnavailable = errors.New("ielts question bank unavailable")
	ErrQuestionBankInvalid     = errors.New("ielts question bank invalid")
	ErrQuestionBankConflict    = errors.New("ielts question bank conflict")
	ErrQuestionSetNotFound     = errors.New("ielts question set not found")
	ErrPracticeModeInvalid     = errors.New("ielts practice mode invalid")
)

type PracticeMode = scene.PracticeMode

const (
	PracticeModeFullMock = scene.PracticeModeFullMock
	PracticeModePart1    = scene.PracticeModePart1
	PracticeModePart2    = scene.PracticeModePart2
	PracticeModePart3    = scene.PracticeModePart3
)

type FilterOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type CatalogFilters struct {
	Releases     []FilterOption `json:"releases"`
	Parts        []FilterOption `json:"parts"`
	TopicTags    []FilterOption `json:"topic_tags"`
	CueCardTypes []FilterOption `json:"cue_card_types"`
}

type QuestionBank struct {
	SchemaVersion int                  `json:"schema_version"`
	BankID        string               `json:"bank_id"`
	Season        string               `json:"season"`
	SeasonLabel   string               `json:"season_label"`
	SeasonStart   string               `json:"season_start"`
	SeasonEnd     string               `json:"season_end"`
	SourceCutoff  time.Time            `json:"source_cutoff"`
	Filters       CatalogFilters       `json:"filters"`
	Part1Topics   []Part1PracticeTopic `json:"part1_topics"`
	TopicGroups   []TopicGroup         `json:"topic_groups"`
}

type Part1PracticeTopic struct {
	ID            string   `json:"id"`
	TitleZH       string   `json:"title_zh"`
	TitleEN       string   `json:"title_en"`
	ReleaseStatus string   `json:"release_status"`
	CueCardType   string   `json:"cue_card_type"`
	TagCodes      []string `json:"tag_codes"`
	Questions     []string `json:"questions"`
}

type TopicGroup struct {
	ID             string       `json:"id"`
	TitleZH        string       `json:"title_zh"`
	ReleaseStatus  string       `json:"release_status"`
	CueCardType    string       `json:"cue_card_type"`
	TagCodes       []string     `json:"tag_codes"`
	Part2          Part2CueCard `json:"part2"`
	Part3Questions []string     `json:"part3_questions"`
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

type ResolvedPart struct {
	Part           PracticeMode
	SourceID       string
	TopicTitle     string
	CueCard        string
	TurnBlueprints []string
}

type QuestionBankReader interface {
	QuestionBank(context.Context) (QuestionBank, error)
}

type QuestionSetResolver interface {
	ResolveQuestionSet(
		context.Context,
		QuestionSetSelection,
	) (ResolvedQuestionSet, error)
	AssignQuestionSet(
		context.Context,
		PracticeMode,
		string,
	) (ResolvedQuestionSet, error)
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
