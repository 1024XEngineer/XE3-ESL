package service

import (
	"context"
	"testing"

	ieltsdata "github.com/1024XEngineer/XE3-ESL/server/data/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func TestStaticIELTSCatalogProducesPlanCompatibleAssignments(t *testing.T) {
	input, err := ieltsdata.Files.Open(ieltsdata.CurrentFile)
	if err != nil {
		t.Fatalf("open current IELTS question bank: %v", err)
	}
	defer input.Close()
	resolver, err := ielts.LoadCatalog(input)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	bank, err := resolver.QuestionBank(context.Background())
	if err != nil {
		t.Fatalf("QuestionBank: %v", err)
	}
	part1, err := resolver.AssignQuestionSet(
		context.Background(),
		ielts.PracticeModePart1,
		"person",
	)
	if err != nil || len(part1.Parts) != 1 {
		t.Fatalf("resolve Part 1 = (%#v, %v)", part1, err)
	}

	assertCompatible := func(
		t *testing.T,
		mode scene.PracticeMode,
		request *IELTSQuestionSelection,
	) *IELTSAssignmentSnapshot {
		t.Helper()
		selection := planIELTSSelectionFixture()
		selection.Scene.PracticeOptions[0].Mode = mode
		frozen, assignment, err := freezeIELTSAssignment(
			context.Background(),
			resolver,
			selection,
			request,
		)
		if err != nil {
			t.Fatalf("freezeIELTSAssignment: %v", err)
		}
		if !ValidPlanIELTSAssignment(frozen, assignment) {
			t.Fatalf("catalog assignment is incompatible with Plan: %#v", assignment)
		}
		return assignment
	}

	t.Run("full mock assignment", func(t *testing.T) {
		assertCompatible(t, scene.PracticeModeFullMock, nil)
	})
	for _, mode := range []scene.PracticeMode{
		scene.PracticeModePart1,
		scene.PracticeModePart2,
		scene.PracticeModePart3,
	} {
		t.Run(string(mode)+" random assignment", func(t *testing.T) {
			assignment := assertCompatible(t, mode, nil)
			if mode == scene.PracticeModePart2 &&
				(len(assignment.Parts) != 1 ||
					assignment.Parts[0].Part != scene.PracticeModePart2) {
				t.Fatalf("Part 2 assignment = %#v", assignment.Parts)
			}
		})
	}
	for _, topic := range bank.Part1Topics {
		t.Run("Part 1 topic/"+topic.ID, func(t *testing.T) {
			assertCompatible(
				t,
				scene.PracticeModePart1,
				&IELTSQuestionSelection{Part1SetID: topic.ID},
			)
		})
	}
	for _, mode := range []scene.PracticeMode{
		scene.PracticeModePart2,
		scene.PracticeModePart3,
	} {
		t.Run(string(mode)+" explicit topic", func(t *testing.T) {
			assertCompatible(
				t,
				mode,
				&IELTSQuestionSelection{TopicGroupID: bank.TopicGroups[0].ID},
			)
		})
	}

	part1Types := make(map[string]string, len(bank.Part1Topics))
	for _, topic := range bank.Part1Topics {
		part1Types[topic.ID] = topic.CueCardType
	}
	groupTypes := make(map[string]string, len(bank.TopicGroups))
	for _, group := range bank.TopicGroups {
		groupTypes[group.ID] = group.CueCardType
	}
	for _, cueCardType := range []string{
		"person",
		"place",
		"thing",
		"experience",
	} {
		for _, mode := range []scene.PracticeMode{
			scene.PracticeModePart1,
			scene.PracticeModePart2,
			scene.PracticeModePart3,
		} {
			t.Run(string(mode)+"/"+cueCardType, func(t *testing.T) {
				assignment := assertCompatible(
					t,
					mode,
					&IELTSQuestionSelection{CueCardType: cueCardType},
				)
				assignedType := groupTypes[assignment.Parts[0].SourceID]
				if mode == scene.PracticeModePart1 {
					assignedType = part1Types[assignment.Parts[0].SourceID]
				}
				if assignedType != cueCardType {
					t.Fatalf(
						"assigned Cue Card type = %q, want %q",
						assignedType,
						cueCardType,
					)
				}
			})
		}
	}
}

func planIELTSSelectionFixture() scene.SelectionSnapshot {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(func(string) error { return nil }),
	)
	if err != nil {
		panic(err)
	}
	definition, err := catalog.GetScene(context.Background(), "scn_ielts_speaking")
	if err != nil {
		panic(err)
	}
	selection, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		definition.Version,
		[]string{definition.Roles[0].ID},
		definition.PracticeOptions[0].ID,
	)
	if err != nil {
		panic(err)
	}
	return selection
}
