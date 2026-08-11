package postgres

import (
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestIELTSSessionReportAuthorityMapsEveryPracticeMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     practice.PracticeMode
		parts    []practice.PracticeMode
		sections []string
		strategy string
		pipeline string
		policy   string
	}{
		{
			name: "Part 1", mode: practice.PracticeModePart1,
			parts:    []practice.PracticeMode{practice.PracticeModePart1},
			sections: []string{"PART_1"},
			strategy: scoring.GeneralSceneStrategyRef,
			pipeline: scoring.GeneralScenePipelineVersion,
			policy:   scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
		{
			name: "Part 2 and Part 3", mode: practice.PracticeModePart2,
			parts: []practice.PracticeMode{
				practice.PracticeModePart2,
				practice.PracticeModePart3,
			},
			sections: []string{"PART_2", "PART_3"},
			strategy: scoring.GeneralSceneStrategyRef,
			pipeline: scoring.GeneralScenePipelineVersion,
			policy:   scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
		{
			name: "Part 3", mode: practice.PracticeModePart3,
			parts:    []practice.PracticeMode{practice.PracticeModePart3},
			sections: []string{"PART_3"},
			strategy: scoring.GeneralSceneStrategyRef,
			pipeline: scoring.GeneralScenePipelineVersion,
			policy:   scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		},
		{
			name: "full mock", mode: practice.PracticeModeFullMock,
			parts: []practice.PracticeMode{
				practice.PracticeModePart1,
				practice.PracticeModePart2,
				practice.PracticeModePart3,
			},
			sections: []string{"PART_1", "PART_2", "PART_3"},
			strategy: scoring.IELTSSpeakingShadowStrategyRef,
			pipeline: scoring.IELTSSpeakingShadowPipelineVersion,
			policy:   scoring.IELTSSpeakingFullMockEvaluationPolicyRef,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assignment := &practice.IELTSAssignment{
				Mode:  test.mode,
				Parts: make([]practice.IELTSPart, len(test.parts)),
			}
			for index, part := range test.parts {
				assignment.Parts[index].Part = part
			}
			sections, strategy, pipeline, _, policy, ok :=
				ieltsSessionReportAuthority(test.mode, assignment)
			if !ok || !reflect.DeepEqual(sections, test.sections) ||
				strategy != test.strategy || pipeline != test.pipeline ||
				policy != test.policy {
				t.Fatalf(
					"authority = (%#v,%q,%q,%q,%t)",
					sections, strategy, pipeline, policy, ok,
				)
			}
		})
	}
}
