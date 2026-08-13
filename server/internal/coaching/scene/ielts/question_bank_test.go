package ielts

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCurrentQuestionBankImportDocumentIsValid(t *testing.T) {
	t.Parallel()

	input, err := os.Open("../../../../data/ielts/2026-05-08-mainland.json")
	if err != nil {
		t.Fatalf("open current import document: %v", err)
	}
	defer input.Close()
	document, err := DecodeImportDocument(input)
	if err != nil {
		t.Fatalf("DecodeImportDocument: %v", err)
	}
	if document.SchemaVersion != 4 ||
		document.BankID != "ielts-speaking-2026-05-08-mainland" ||
		len(document.Part1Topics) != 38 ||
		len(document.Part1Sets) != 38 ||
		len(document.TopicGroups) != 56 {
		t.Fatalf("current document metadata = %#v", document)
	}
	part1Questions := 0
	categoryCounts := map[string]int{}
	part1Categories := map[string]string{}
	for _, topic := range document.Part1Topics {
		part1Questions += len(topic.Questions)
		categoryCounts[topic.CueCardType]++
		part1Categories[topic.ID] = topic.CueCardType
		if len(topic.TagCodes) == 0 {
			t.Fatalf("Part 1 topic %s has no semantic tag", topic.ID)
		}
	}
	part3Questions := 0
	for _, group := range document.TopicGroups {
		part3Questions += len(group.Part3Questions)
		categoryCounts[group.CueCardType]++
		if len(group.TagCodes) == 0 {
			t.Fatalf("Part 2/3 group %s has no semantic tag", group.ID)
		}
	}
	if part1Questions != 234 || part3Questions != 317 {
		t.Fatalf("question counts = %d/%d", part1Questions, part3Questions)
	}
	if categoryCounts["person"]+categoryCounts["place"]+
		categoryCounts["thing"]+categoryCounts["experience"] != 94 {
		t.Fatalf("category counts = %#v", categoryCounts)
	}
	if part1Categories["p1-topic-002"] != "person" ||
		part1Categories["p1-topic-019"] != "experience" {
		t.Fatalf("corrected Part 1 categories = %#v", part1Categories)
	}
}

func TestDecodeImportDocumentRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := DecodeImportDocument(strings.NewReader(`{"schema_version":3,"unknown":true}`))
	if !errors.Is(err, ErrQuestionBankInvalid) {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func TestValidateImportDocumentRejectsUnknownTagReference(t *testing.T) {
	t.Parallel()

	document := validImportDocumentFixture()
	document.Part1Topics[0].TagCodes = []string{"missing"}
	if err := validateImportDocument(document); !errors.Is(err, ErrQuestionBankInvalid) {
		t.Fatalf("unknown-tag error = %v", err)
	}
}

func validImportDocumentFixture() ImportDocument {
	return ImportDocument{
		SchemaVersion: 4,
		BankID:        "ielts-test-bank",
		Season:        "2026-05-08",
		SeasonLabel:   "5–8 月题库",
		SeasonStart:   "2026-05-01",
		SeasonEnd:     "2026-08-31",
		Region:        "mainland",
		SourceCutoff:  "2026-06-18T18:00:00+08:00",
		Sources: []ImportSource{{
			ID: "source-1", URL: "https://example.com/ielts", CapturedAt: "2026-06-18T18:00:00+08:00",
		}},
		Tags: []ImportTag{{Code: "daily_life", LabelZH: "日常生活"}},
		Part1Topics: []ImportPart1Topic{
			{
				ID: "topic-1", TitleZH: "话题一", TitleEN: "Topic one", ReleaseStatus: "new",
				CueCardType: "thing",
				TagCodes:    []string{"daily_life"}, Questions: []string{"Question 1?", "Question 2?", "Question 3?"},
			},
			{
				ID: "topic-2", TitleZH: "话题二", TitleEN: "Topic two", ReleaseStatus: "new",
				CueCardType: "person",
				TagCodes:    []string{"daily_life"}, Questions: []string{"Question 4?", "Question 5?", "Question 6?"},
			},
			{
				ID: "topic-3", TitleZH: "话题三", TitleEN: "Topic three", ReleaseStatus: "carry_over",
				CueCardType: "experience",
				TagCodes:    []string{"daily_life"}, Questions: []string{"Question 7?", "Question 8?"},
			},
		},
		Part1Sets: []ImportPart1Set{{
			ID: "set-1", Title: "Set one", Questions: []ImportPart1QuestionRef{
				{TopicID: "topic-1", QuestionPosition: 1},
				{TopicID: "topic-1", QuestionPosition: 2},
				{TopicID: "topic-1", QuestionPosition: 3},
				{TopicID: "topic-2", QuestionPosition: 1},
				{TopicID: "topic-2", QuestionPosition: 2},
				{TopicID: "topic-2", QuestionPosition: 3},
				{TopicID: "topic-3", QuestionPosition: 1},
				{TopicID: "topic-3", QuestionPosition: 2},
			},
		}},
		TopicGroups: []ImportTopicGroup{{
			ID: "group-1", TitleZH: "主题组", ReleaseStatus: "new", CueCardType: "experience",
			TagCodes:       []string{"daily_life"},
			Part2:          Part2CueCard{Prompt: "Describe an experience", Points: []string{"When", "Where", "Why"}},
			Part3Questions: []string{"Why is it important?"},
		}},
	}
}
