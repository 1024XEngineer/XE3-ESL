package scene

import (
	"errors"
	"reflect"
	"testing"
)

func TestEmbeddedIELTSQuestionBankPublishesOnlyCompleteMainlandGroups(
	t *testing.T,
) {
	t.Parallel()

	bank, err := loadEmbeddedIELTSQuestionBank()
	if err != nil {
		t.Fatalf("loadEmbeddedIELTSQuestionBank: %v", err)
	}
	published := publishedIELTSQuestionBank(bank)
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
	part1QuestionsByTopic := make(map[string]map[string]struct{})
	for _, set := range published.Part1Sets {
		for _, topic := range set.Topics {
			questions, ok := part1QuestionsByTopic[topic.Title]
			if !ok {
				questions = make(map[string]struct{})
				part1QuestionsByTopic[topic.Title] = questions
			}
			for _, question := range topic.Questions {
				questions[question] = struct{}{}
			}
		}
	}
	part1QuestionCount := 0
	for _, questions := range part1QuestionsByTopic {
		part1QuestionCount += len(questions)
	}
	if len(part1QuestionsByTopic) != 38 || part1QuestionCount != 234 {
		t.Fatalf(
			"published Part 1 coverage = (%d topics, %d questions)",
			len(part1QuestionsByTopic),
			part1QuestionCount,
		)
	}
	practiceQuestionCount := 0
	part1Releases := map[string]int{}
	part1Categories := map[string]int{}
	for _, topic := range published.Part1Topics {
		practiceQuestionCount += len(topic.Questions)
		part1Releases[topic.Release]++
		part1Categories[topic.Category]++
	}
	if practiceQuestionCount != 234 ||
		!reflect.DeepEqual(
			part1Releases,
			map[string]int{"new": 16, "carry_over": 17, "evergreen": 5},
		) ||
		!reflect.DeepEqual(
			part1Categories,
			map[string]int{"person": 2, "place": 10, "thing": 15, "event": 11},
		) {
		t.Fatalf(
			"Part 1 practice taxonomy = questions %d, releases %#v, categories %#v",
			practiceQuestionCount,
			part1Releases,
			part1Categories,
		)
	}
	groupCategories := map[string]int{}
	for _, group := range published.TopicGroups {
		if !group.Published ||
			group.Region != "mainland" ||
			len(group.Part3Questions) < 1 ||
			len(group.Part3Questions) > 5 ||
			group.SupplementedQuestionCount != 0 {
			t.Fatalf("published incomplete group: %#v", group)
		}
		groupCategories[group.Category]++
	}
	if !reflect.DeepEqual(
		groupCategories,
		map[string]int{"person": 12, "place": 8, "thing": 17, "event": 19},
	) {
		t.Fatalf("Part 2/3 categories = %#v", groupCategories)
	}
	for _, group := range bank.TopicGroups {
		if group.Region == "international" && group.Published {
			t.Fatalf("international group was published: %#v", group)
		}
	}
}

func TestResolveIELTSPart1PracticeUsesOnlySelectedTopic(t *testing.T) {
	t.Parallel()

	catalog := mustTestCatalog(t)
	bank, err := catalog.IELTSQuestionBank()
	if err != nil {
		t.Fatalf("IELTSQuestionBank: %v", err)
	}
	selected := bank.Part1Topics[0]
	resolved, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:       IELTSPracticeModePart1,
			Part1SetID: selected.ID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet Part 1 topic: %v", err)
	}
	if resolved.Part1Questions != len(selected.Questions) ||
		len(resolved.TurnBlueprints) != len(selected.Questions) {
		t.Fatalf("Part 1 topic resolution = %#v", resolved)
	}
	for index, question := range selected.Questions {
		want := "Part 1 question: " + question
		if resolved.TurnBlueprints[index] != want {
			t.Fatalf(
				"turn %d = %q, want %q",
				index,
				resolved.TurnBlueprints[index],
				want,
			)
		}
	}
}

func TestResolveIELTSQuestionSetKeepsPart2AndPart3Bound(t *testing.T) {
	t.Parallel()

	catalog := mustTestCatalog(t)
	bank, err := catalog.IELTSQuestionBank()
	if err != nil {
		t.Fatalf("IELTSQuestionBank: %v", err)
	}
	part1 := bank.Part1Sets[0]
	group := bank.TopicGroups[0]
	full, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModeFullMock,
			Part1SetID:   part1.ID,
			TopicGroupID: group.ID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet full mock: %v", err)
	}
	if full.Part1Questions != 8 ||
		full.Part2Questions != 1 ||
		full.Part3Questions != 5 ||
		len(full.TurnBlueprints) != 14 ||
		full.TopicGroupID != group.ID {
		t.Fatalf("full mock resolution = %#v", full)
	}

	part2, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModePart2,
			TopicGroupID: group.ID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet part 2: %v", err)
	}
	part3, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModePart3,
			TopicGroupID: group.ID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet part 3: %v", err)
	}
	if part2.TopicGroupID != part3.TopicGroupID ||
		part2.TopicTitle != part3.TopicTitle ||
		part2.Part2CueCard != part3.Part2CueCard ||
		!reflect.DeepEqual(
			part2.TurnBlueprints[1:],
			part3.TurnBlueprints,
		) {
		t.Fatalf(
			"bound topic changed between parts:\npart2=%#v\npart3=%#v",
			part2,
			part3,
		)
	}
}

func TestResolveIELTSQuestionSetPreservesShortOriginalPart3(t *testing.T) {
	t.Parallel()

	catalog := mustTestCatalog(t)
	const topicGroupID = "p23-new-021"
	bank, err := catalog.IELTSQuestionBank()
	if err != nil {
		t.Fatalf("IELTSQuestionBank: %v", err)
	}
	part1SetID := bank.Part1Sets[0].ID

	full, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModeFullMock,
			Part1SetID:   part1SetID,
			TopicGroupID: topicGroupID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet full mock: %v", err)
	}
	if full.Part1Questions != 8 ||
		full.Part2Questions != 1 ||
		full.Part3Questions != 1 ||
		len(full.TurnBlueprints) != 10 {
		t.Fatalf("short full mock resolution = %#v", full)
	}

	part2, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModePart2,
			TopicGroupID: topicGroupID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet part 2: %v", err)
	}
	part3, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModePart3,
			TopicGroupID: topicGroupID,
		},
	)
	if err != nil {
		t.Fatalf("ResolveIELTSQuestionSet part 3: %v", err)
	}
	if part2.Part3Questions != 1 ||
		len(part2.TurnBlueprints) != 2 ||
		part3.Part3Questions != 1 ||
		len(part3.TurnBlueprints) != 1 ||
		!reflect.DeepEqual(part2.TurnBlueprints[1:], part3.TurnBlueprints) {
		t.Fatalf("short bound topic changed: part2=%#v part3=%#v", part2, part3)
	}
}

func TestResolveIELTSQuestionSetRejectsHiddenGroup(t *testing.T) {
	t.Parallel()

	catalog := mustTestCatalog(t)
	bank := *catalog.ieltsQuestionBank
	var hiddenID string
	for _, group := range bank.TopicGroups {
		if !group.Published {
			hiddenID = group.ID
			break
		}
	}
	if hiddenID == "" {
		t.Fatal("fixture has no hidden IELTS group")
	}
	_, err := catalog.ResolveIELTSQuestionSet(
		IELTSQuestionSetSelection{
			Mode:         IELTSPracticeModePart2,
			TopicGroupID: hiddenID,
		},
	)
	if !errors.Is(err, ErrIELTSQuestionSetNotFound) {
		t.Fatalf("hidden group error = %v", err)
	}
}
