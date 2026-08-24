package postgres

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestIELTSProfileBoundarySchedulesOnlyFullMockPartBoundaries(t *testing.T) {
	snapshot := practice.SessionSnapshot{
		PracticeMode: practice.PracticeModeFullMock,
		IELTSAssignment: &practice.IELTSAssignment{Parts: []practice.IELTSPart{
			{TurnBlueprints: []string{"p1-1", "p1-2"}},
			{TurnBlueprints: []string{"p2-1"}},
			{TurnBlueprints: []string{"p3-1", "p3-2"}},
		}},
	}

	tests := []struct {
		turn  int
		stage practice.IELTSProfileStage
		ok    bool
	}{
		{turn: 1},
		{turn: 2, stage: practice.IELTSProfileStagePart1, ok: true},
		{turn: 3, stage: practice.IELTSProfileStagePart2, ok: true},
		{turn: 5},
	}
	for _, test := range tests {
		stage, part1, part2, ok := ieltsProfileBoundary(snapshot, test.turn)
		if ok != test.ok || stage != test.stage || part1 != 2 || part2 != 3 {
			t.Fatalf("turn %d boundary = (%q,%d,%d,%t)",
				test.turn, stage, part1, part2, ok)
		}
	}

	snapshot.PracticeMode = practice.PracticeModePart1
	if _, _, _, ok := ieltsProfileBoundary(snapshot, 2); ok {
		t.Fatal("standalone Part 1 must not schedule a cumulative profile")
	}
}
