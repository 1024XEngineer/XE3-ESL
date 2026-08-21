package ielts

import (
	"context"
	"errors"
	"strings"
	"testing"

	ieltsdata "github.com/1024XEngineer/XE3-ESL/server/data/ielts"
)

func TestCurrentQuestionBankLoadsFromRepositoryAsset(t *testing.T) {
	catalog := mustCurrentCatalog(t)
	bank, err := catalog.QuestionBank(context.Background())
	if err != nil {
		t.Fatalf("QuestionBank() error = %v", err)
	}
	if bank.SchemaVersion != 4 ||
		bank.BankID != "ielts-speaking-2026-05-08-mainland" ||
		len(bank.Part1Topics) != 38 || len(catalog.part1Sets) != 38 ||
		len(bank.TopicGroups) != 56 {
		t.Fatalf("current bank metadata = %#v", bank)
	}
	part1Questions := 0
	part3Questions := 0
	for _, topic := range bank.Part1Topics {
		part1Questions += len(topic.Questions)
	}
	for _, group := range bank.TopicGroups {
		part3Questions += len(group.Part3Questions)
	}
	if part1Questions != 234 || part3Questions != 317 ||
		len(bank.Filters.TopicTags) != 10 {
		t.Fatalf(
			"question/filter counts = %d/%d/%d",
			part1Questions,
			part3Questions,
			len(bank.Filters.TopicTags),
		)
	}
}

func TestCatalogResolvesEveryPracticeMode(t *testing.T) {
	catalog := mustCurrentCatalog(t)
	fullMock, err := catalog.ResolveQuestionSet(
		context.Background(),
		QuestionSetSelection{
			Mode: PracticeModeFullMock, Part1SetID: "p1-001",
			TopicGroupID: "p23-new-001",
		},
	)
	if err != nil || len(fullMock.Parts) != 3 ||
		len(fullMock.Parts[0].TurnBlueprints) != 8 ||
		len(fullMock.Parts[2].TurnBlueprints) != 6 {
		t.Fatalf("full mock = %#v, error = %v", fullMock, err)
	}
	part1, err := catalog.ResolveQuestionSet(
		context.Background(),
		QuestionSetSelection{Mode: PracticeModePart1, Part1SetID: "p1-topic-002"},
	)
	if err != nil || len(part1.Parts) != 1 || part1.Parts[0].TopicTitle != "老师" {
		t.Fatalf("Part 1 = %#v, error = %v", part1, err)
	}
	part2, err := catalog.ResolveQuestionSet(
		context.Background(),
		QuestionSetSelection{Mode: PracticeModePart2, TopicGroupID: "p23-new-001"},
	)
	if err != nil || len(part2.Parts) != 1 ||
		part2.Parts[0].Part != PracticeModePart2 ||
		!strings.Contains(part2.Parts[0].CueCard, "You should say:") {
		t.Fatalf("Part 2 = %#v, error = %v", part2, err)
	}
	part3, err := catalog.ResolveQuestionSet(
		context.Background(),
		QuestionSetSelection{Mode: PracticeModePart3, TopicGroupID: "p23-new-001"},
	)
	if err != nil || len(part3.Parts) != 1 ||
		part3.Parts[0].Part != PracticeModePart3 {
		t.Fatalf("Part 3 = %#v, error = %v", part3, err)
	}
}

func TestCatalogAssignmentHonorsCueCardType(t *testing.T) {
	catalog := mustCurrentCatalog(t)
	bank, _ := catalog.QuestionBank(context.Background())
	part1Types := make(map[string]string, len(bank.Part1Topics))
	for _, topic := range bank.Part1Topics {
		part1Types[topic.ID] = topic.CueCardType
	}
	groupTypes := make(map[string]string, len(bank.TopicGroups))
	for _, group := range bank.TopicGroups {
		groupTypes[group.ID] = group.CueCardType
	}
	for _, cueCardType := range []string{"person", "place", "thing", "experience"} {
		part1, err := catalog.AssignQuestionSet(
			context.Background(),
			PracticeModePart1,
			cueCardType,
		)
		if err != nil || part1Types[part1.Parts[0].SourceID] != cueCardType {
			t.Fatalf("Part 1/%s = %#v, error = %v", cueCardType, part1, err)
		}
		part3, err := catalog.AssignQuestionSet(
			context.Background(),
			PracticeModePart3,
			cueCardType,
		)
		if err != nil || groupTypes[part3.Parts[0].SourceID] != cueCardType {
			t.Fatalf("Part 3/%s = %#v, error = %v", cueCardType, part3, err)
		}
	}
}

func TestCatalogResolvesQuestionsOnlyFromCurrentBank(t *testing.T) {
	catalog := mustCurrentCatalog(t)
	bank, _ := catalog.QuestionBank(context.Background())
	part1, err := catalog.ResolveQuestion(
		context.Background(),
		QuestionReference{
			BankID: bank.BankID, Part: PracticeModePart1,
			SourceID: "p1-topic-001", QuestionPosition: 1,
		},
	)
	if err != nil || part1.Prompt != "Do you prefer sad or happy music?" {
		t.Fatalf("Part 1 question = %#v, error = %v", part1, err)
	}
	part2, err := catalog.ResolveQuestion(
		context.Background(),
		QuestionReference{
			BankID: bank.BankID, Part: PracticeModePart2,
			SourceID: "p23-new-001", QuestionPosition: 1,
		},
	)
	if err != nil || !strings.Contains(part2.Prompt, "You should say:\n• What it is used for") {
		t.Fatalf("Part 2 question = %#v, error = %v", part2, err)
	}
	_, err = catalog.ResolveQuestion(
		context.Background(),
		QuestionReference{
			BankID: "retired-bank", Part: PracticeModePart1,
			SourceID: "p1-topic-001", QuestionPosition: 1,
		},
	)
	if !errors.Is(err, ErrQuestionSetNotFound) {
		t.Fatalf("retired bank error = %v", err)
	}
}

func TestQuestionBankReturnsIndependentCopies(t *testing.T) {
	catalog := mustCurrentCatalog(t)
	first, _ := catalog.QuestionBank(context.Background())
	first.Part1Topics[0].Questions[0] = "mutated"
	second, _ := catalog.QuestionBank(context.Background())
	if second.Part1Topics[0].Questions[0] == "mutated" {
		t.Fatal("QuestionBank exposed mutable catalog content")
	}
}

func TestLoadCatalogRejectsUnknownFields(t *testing.T) {
	_, err := LoadCatalog(strings.NewReader(`{"schema_version":4,"unknown":true}`))
	if !errors.Is(err, ErrQuestionBankInvalid) {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func mustCurrentCatalog(t *testing.T) *Catalog {
	t.Helper()
	input, err := ieltsdata.Files.Open(ieltsdata.CurrentFile)
	if err != nil {
		t.Fatalf("open current question bank: %v", err)
	}
	defer input.Close()
	catalog, err := LoadCatalog(input)
	if err != nil {
		t.Fatalf("LoadCatalog() error = %v", err)
	}
	return catalog
}
