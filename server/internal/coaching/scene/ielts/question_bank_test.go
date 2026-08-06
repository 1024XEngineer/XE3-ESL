package ielts

import (
	"errors"
	"reflect"
	"testing"
)

func TestEmbeddedQuestionBankPublishesCompleteMainlandCatalog(t *testing.T) {
	t.Parallel()

	bank, err := loadEmbeddedQuestionBank()
	if err != nil {
		t.Fatalf("loadEmbeddedQuestionBank: %v", err)
	}
	published := publishedQuestionBank(bank)
	if len(published.Part1Sets) != 38 ||
		len(published.Part1Topics) != 38 ||
		len(published.TopicGroups) != 56 {
		t.Fatalf(
			"published counts = (%d, %d, %d)",
			len(published.Part1Sets),
			len(published.Part1Topics),
			len(published.TopicGroups),
		)
	}

	part1Questions := 0
	part1Releases := map[string]int{}
	part1Categories := map[string]int{}
	for _, topic := range published.Part1Topics {
		part1Questions += len(topic.Questions)
		part1Releases[topic.Release]++
		part1Categories[topic.Category]++
	}
	if part1Questions != 234 ||
		!reflect.DeepEqual(
			part1Releases,
			map[string]int{"new": 16, "carry_over": 17, "evergreen": 5},
		) ||
		!reflect.DeepEqual(
			part1Categories,
			map[string]int{"person": 2, "place": 10, "thing": 15, "event": 11},
		) {
		t.Fatalf(
			"Part 1 taxonomy = questions %d, releases %#v, categories %#v",
			part1Questions,
			part1Releases,
			part1Categories,
		)
	}

	part3Questions := 0
	groupCategories := map[string]int{}
	for _, group := range published.TopicGroups {
		if !group.Published ||
			group.Region != "mainland" ||
			len(group.Part3Questions) < 1 ||
			len(group.Part3Questions) > 6 ||
			group.SupplementedQuestionCount != 0 {
			t.Fatalf("published incomplete group: %#v", group)
		}
		part3Questions += len(group.Part3Questions)
		groupCategories[group.Category]++
	}
	if part3Questions != 317 || !reflect.DeepEqual(
		groupCategories,
		map[string]int{"person": 12, "place": 8, "thing": 17, "event": 19},
	) {
		t.Fatalf(
			"Part 2/3 taxonomy = questions %d, categories %#v",
			part3Questions,
			groupCategories,
		)
	}
}

func TestEmbeddedPart1TopicsPreserveSourceOrder(t *testing.T) {
	t.Parallel()

	bank, err := loadEmbeddedQuestionBank()
	if err != nil {
		t.Fatalf("loadEmbeddedQuestionBank: %v", err)
	}
	wants := map[string][]string{
		"Teachers": {
			"Do you have a favorite teacher?",
			"Do you want to be a teacher in the future?",
			"Do you have a teacher from your past that you still remember?",
			"Are you still in touch with your primary school teachers?",
			"In what way has your favourite teacher helped you?",
			"Do you like your primary school teachers more than your high school teachers?",
		},
		"Public gardens and parks": {
			"Did you like going to parks as a child?",
			"Do you still like going to parks now?",
			"Would you like to see more parks in your city?",
			"Are there any parks you want to go to in the future?",
			"Would you prefer to play in a personal garden or public garden?",
			"How are the parks today different from those you visited as a kid?",
			"What do you like to do when visiting a park?",
			"Would you like to play in a public garden or park?",
		},
	}
	for _, topic := range bank.Part1Topics {
		want, found := wants[topic.TitleEN]
		if found && !reflect.DeepEqual(topic.Questions, want) {
			t.Fatalf("%s questions = %#v, want %#v", topic.TitleEN, topic.Questions, want)
		}
		delete(wants, topic.TitleEN)
	}
	if len(wants) != 0 {
		t.Fatalf("missing canonical topics: %#v", wants)
	}
}

func TestResolvePart1PracticeUsesOnlySelectedTopic(t *testing.T) {
	t.Parallel()

	service := mustTestBank(t)
	bank, err := service.QuestionBank()
	if err != nil {
		t.Fatalf("QuestionBank: %v", err)
	}
	selected := bank.Part1Topics[0]
	resolved, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode:       PracticeModePart1,
		Part1SetID: selected.ID,
	})
	if err != nil {
		t.Fatalf("ResolveQuestionSet Part 1: %v", err)
	}
	if len(resolved.Parts) != 1 ||
		resolved.Parts[0].SourceID != selected.ID ||
		len(resolved.Parts[0].TurnBlueprints) != len(selected.Questions) {
		t.Fatalf("Part 1 resolution = %#v", resolved)
	}
}

func TestResolveQuestionSetKeepsPart2AndPart3Bound(t *testing.T) {
	t.Parallel()

	service := mustTestBank(t)
	bank, err := service.QuestionBank()
	if err != nil {
		t.Fatalf("QuestionBank: %v", err)
	}
	part1 := bank.Part1Sets[0]
	group := bank.TopicGroups[0]
	full, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode:         PracticeModeFullMock,
		Part1SetID:   part1.ID,
		TopicGroupID: group.ID,
	})
	if err != nil {
		t.Fatalf("ResolveQuestionSet full mock: %v", err)
	}
	if len(full.Parts) != 3 ||
		full.Parts[0].Part != PracticeModePart1 ||
		full.Parts[1].Part != PracticeModePart2 ||
		full.Parts[2].Part != PracticeModePart3 {
		t.Fatalf("full mock resolution = %#v", full)
	}

	part2, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode: PracticeModePart2, TopicGroupID: group.ID,
	})
	if err != nil {
		t.Fatalf("ResolveQuestionSet part 2: %v", err)
	}
	part3, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode: PracticeModePart3, TopicGroupID: group.ID,
	})
	if err != nil {
		t.Fatalf("ResolveQuestionSet part 3: %v", err)
	}
	if len(part2.Parts) != 2 || len(part3.Parts) != 1 ||
		!reflect.DeepEqual(part2.Parts[1], part3.Parts[0]) {
		t.Fatalf("bound topic changed: part2=%#v part3=%#v", part2, part3)
	}
}

func TestEveryPublishedQuestionSetResolves(t *testing.T) {
	t.Parallel()

	service := mustTestBank(t)
	bank, err := service.QuestionBank()
	if err != nil {
		t.Fatalf("QuestionBank: %v", err)
	}
	for _, topic := range bank.Part1Topics {
		resolved, resolveErr := service.ResolveQuestionSet(QuestionSetSelection{
			Mode: PracticeModePart1, Part1SetID: topic.ID,
		})
		if resolveErr != nil || len(resolved.Parts) != 1 {
			t.Fatalf("resolve Part 1 %s: %#v, %v", topic.ID, resolved, resolveErr)
		}
	}
	for _, group := range bank.TopicGroups {
		for _, mode := range []PracticeMode{PracticeModePart2, PracticeModePart3} {
			resolved, resolveErr := service.ResolveQuestionSet(QuestionSetSelection{
				Mode: mode, TopicGroupID: group.ID,
			})
			if resolveErr != nil || len(resolved.Parts) == 0 {
				t.Fatalf("resolve %s %s: %#v, %v", mode, group.ID, resolved, resolveErr)
			}
		}
	}
	for _, part1 := range bank.Part1Sets {
		for _, group := range bank.TopicGroups {
			resolved, resolveErr := service.ResolveQuestionSet(QuestionSetSelection{
				Mode: PracticeModeFullMock, Part1SetID: part1.ID, TopicGroupID: group.ID,
			})
			if resolveErr != nil || len(resolved.Parts) != 3 {
				t.Fatalf("resolve full mock %s/%s: %#v, %v", part1.ID, group.ID, resolved, resolveErr)
			}
		}
	}
}

func TestResolveQuestionSetPreservesShortOriginalPart3(t *testing.T) {
	t.Parallel()

	service := mustTestBank(t)
	bank, err := service.QuestionBank()
	if err != nil {
		t.Fatalf("QuestionBank: %v", err)
	}
	const groupID = "p23-new-021"
	full, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode: PracticeModeFullMock, Part1SetID: bank.Part1Sets[0].ID, TopicGroupID: groupID,
	})
	if err != nil {
		t.Fatalf("ResolveQuestionSet full mock: %v", err)
	}
	if len(full.Parts) != 3 || len(full.Parts[2].TurnBlueprints) != 1 {
		t.Fatalf("short-group full mock resolution = %#v", full)
	}
}

func TestResolveQuestionSetRejectsHiddenGroup(t *testing.T) {
	t.Parallel()

	service := mustTestBank(t)
	var hiddenID string
	for _, group := range service.data.TopicGroups {
		if !group.Published {
			hiddenID = group.ID
			break
		}
	}
	if hiddenID == "" {
		t.Fatal("fixture has no hidden IELTS group")
	}
	_, err := service.ResolveQuestionSet(QuestionSetSelection{
		Mode: PracticeModePart2, TopicGroupID: hiddenID,
	})
	if !errors.Is(err, ErrQuestionSetNotFound) {
		t.Fatalf("hidden group error = %v", err)
	}
}

func mustTestBank(t *testing.T) *Bank {
	t.Helper()
	bank, err := NewBank()
	if err != nil {
		t.Fatalf("NewBank: %v", err)
	}
	return bank
}
