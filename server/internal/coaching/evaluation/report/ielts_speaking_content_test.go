package report

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestIELTSSpeakingReportValidatesGeneratedExplanationText(t *testing.T) {
	t.Parallel()
	snapshot := ieltsReportTestSnapshot(t, ieltsReportQuestionCount)
	result := ieltsReportTestResult(t, snapshot)
	base, err := ProjectIELTSSpeakingReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSSpeakingReport: %v", err)
	}
	if !base.Valid() {
		t.Fatal("projected IELTS report is invalid")
	}

	exactlyMaximumUTF8Bytes := strings.Repeat("界", 682) + "ab"
	overMaximumUTF8Bytes := exactlyMaximumUTF8Bytes + "c"
	if len(exactlyMaximumUTF8Bytes) != reportMaximumTextBytes ||
		len(overMaximumUTF8Bytes) != reportMaximumTextBytes+1 {
		t.Fatal("test data does not exercise the intended UTF-8 byte boundary")
	}

	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "blank", value: " \t\n", valid: false},
		{name: "NUL", value: "有效说明\x00不可见内容", valid: false},
		{name: "invalid UTF-8", value: string([]byte{'a', 0xff}), valid: false},
		{name: "exactly 2048 UTF-8 bytes", value: exactlyMaximumUTF8Bytes, valid: true},
		{name: "2049 UTF-8 bytes", value: overMaximumUTF8Bytes, valid: false},
	}
	targets := []struct {
		name   string
		mutate func(*IELTSSpeakingReport, string)
	}{
		{
			name: "overall explanation",
			mutate: func(report *IELTSSpeakingReport, value string) {
				report.SpeakingOverall.Explanation = value
			},
		},
		{
			name: "criterion explanation",
			mutate: func(report *IELTSSpeakingReport, value string) {
				report.Criteria[0].Explanation = value
			},
		},
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					report := base
					report.Criteria = slices.Clone(base.Criteria)
					target.mutate(&report, test.value)
					if got := report.Valid(); got != test.valid {
						t.Fatalf("Valid() = %t, want %t", got, test.valid)
					}
				})
			}
		})
	}
}

func TestIELTSSpeakingNarrativesUseManualEvidenceContexts(t *testing.T) {
	t.Parallel()
	band := func(value int) *int { return &value }
	criteria := []IELTSSpeakingReportCriterion{
		{CriterionID: scoring.IELTSCriterionFC, EstimatedBand: band(7)},
		{CriterionID: scoring.IELTSCriterionLR, EstimatedBand: band(5)},
		{CriterionID: scoring.IELTSCriterionGRA, EstimatedBand: band(6)},
		{CriterionID: scoring.IELTSCriterionPR, EstimatedBand: band(4)},
	}
	overall := ieltsSpeakingOverallExplanation(5.5, criteria)
	for _, expected := range []string{
		"本次口语练习估分为 5.5 分",
		"流利性与连贯性（7 分）",
		"发音（4 分）",
		"下一步先做",
	} {
		if !strings.Contains(overall, expected) {
			t.Errorf("overall explanation %q does not contain %q", overall, expected)
		}
	}
	for _, unexpected := range []string{"考生", "已核验", "综合声学证据"} {
		if strings.Contains(overall, unexpected) {
			t.Errorf("overall explanation %q contains system-facing wording %q", overall, unexpected)
		}
	}

	locations := map[string]ieltsEvidenceLocation{
		"ref-fc":  {part: scoring.IELTSPart1, ordinal: 2},
		"ref-lr":  {part: scoring.IELTSPart2, ordinal: 1},
		"ref-gra": {part: scoring.IELTSPart3, ordinal: 1},
		"ref-pr":  {part: scoring.IELTSPart3, ordinal: 2},
	}
	tests := []struct {
		criterion scoring.IELTSCriterion
		band      int
		refID     string
		part      string
		excerpt   string
	}{
		{
			criterion: scoring.IELTSCriterionFC,
			band:      7,
			refID:     "ref-fc",
			part:      "Part 1 第 2 题",
			excerpt:   "I linked the reason to a concrete example.",
		},
		{
			criterion: scoring.IELTSCriterionLR,
			band:      5,
			refID:     "ref-lr",
			part:      "Part 2",
			excerpt:   "I learned pottery from a patient local artist.",
		},
		{
			criterion: scoring.IELTSCriterionGRA,
			band:      6,
			refID:     "ref-gra",
			part:      "Part 3 第 1 题",
			excerpt:   "Although it takes time, the skill remains valuable.",
		},
		{
			criterion: scoring.IELTSCriterionPR,
			band:      4,
			refID:     "ref-pr",
			part:      "Part 3 第 2 题",
			excerpt:   "People can practise the whole sentence together.",
		},
	}
	for _, test := range tests {
		t.Run(string(test.criterion), func(t *testing.T) {
			t.Parallel()
			score := test.band
			source := scoring.IELTSSpeakingShadowCriterionResult{
				CriterionID:   test.criterion,
				Scoreability:  scoring.IELTSSpeakingScoreabilityProvisional,
				EstimatedBand: &score,
				Strengths: []scoring.IELTSSpeakingShadowFinding{{
					Message: "这是由已核验证据支持的表现。",
					Evidence: []scoring.IELTSSpeakingShadowEvidence{{
						EvidenceRefID:   test.refID,
						OriginalExcerpt: test.excerpt,
					}},
				}},
			}
			explanation := ieltsCriterionExplanation(source, locations)
			for _, expected := range []string{test.part, test.excerpt} {
				if !strings.Contains(explanation, expected) {
					t.Errorf(
						"explanation %q does not contain %q",
						explanation,
						expected,
					)
				}
			}
			if test.criterion == scoring.IELTSCriterionPR {
				assertIELTSExplanationHasNoInventedPhoneme(t, explanation)
			}
		})
	}
}

func TestProjectIELTSSpeakingPriorityActionsOrdersDeduplicatesAndCaps(
	t *testing.T,
) {
	t.Parallel()
	band := func(value int) *int { return &value }
	finding := func(id, suggestion string) scoring.IELTSSpeakingShadowFinding {
		return scoring.IELTSSpeakingShadowFinding{
			ID: id, Message: "改进说明", Suggestion: suggestion,
		}
	}
	criteria := []scoring.IELTSSpeakingShadowCriterionResult{
		{
			CriterionID:   scoring.IELTSCriterionFC,
			EstimatedBand: band(7),
			Improvements: []scoring.IELTSSpeakingShadowFinding{
				finding("fc-primary", "Link the idea with one reason and example."),
				finding("fc-secondary", "Record a second connected answer."),
			},
		},
		{
			CriterionID:   scoring.IELTSCriterionLR,
			EstimatedBand: band(5),
			Improvements: []scoring.IELTSSpeakingShadowFinding{
				finding("lr-primary", "Replace general words with one precise topic noun."),
			},
		},
		{
			CriterionID:   scoring.IELTSCriterionGRA,
			EstimatedBand: band(6),
			Improvements: []scoring.IELTSSpeakingShadowFinding{
				finding("gra-duplicate", "  replace GENERAL words with one precise topic noun.  "),
			},
		},
		{
			CriterionID:   scoring.IELTSCriterionPR,
			EstimatedBand: band(4),
			Improvements: []scoring.IELTSSpeakingShadowFinding{
				finding("pr-primary", "Record the whole sentence and compare overall clarity."),
				finding("pr-secondary", "Repeat the sentence at a steady pace."),
			},
		},
	}

	actions := projectIELTSSpeakingPriorityActions(criteria)
	want := []IELTSSpeakingReportPriorityRef{
		{CriterionID: scoring.IELTSCriterionPR, FindingID: "pr-primary"},
		{CriterionID: scoring.IELTSCriterionLR, FindingID: "lr-primary"},
		{CriterionID: scoring.IELTSCriterionFC, FindingID: "fc-primary"},
	}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("priority actions = %#v, want %#v", actions, want)
	}
}

func TestProjectIELTSSpeakingReportPublishesEvidenceBoundContentAndActions(
	t *testing.T,
) {
	t.Parallel()
	snapshot, acoustics := ieltsContentReportAcousticFixture(t)
	result, err := scoring.NewIELTSSpeakingShadowEngine(
		&ieltsContentReportProvider{},
	).EvaluateWithAcousticSnapshot(
		context.Background(),
		snapshot,
		acoustics,
	)
	if err != nil {
		t.Fatalf("EvaluateWithAcousticSnapshot: %v", err)
	}
	report, err := ProjectIELTSSpeakingReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSSpeakingReport: %v", err)
	}
	if !report.Valid() {
		t.Fatal("projected IELTS report is invalid")
	}

	for _, expected := range []string{
		"流利性与连贯性（7 分）",
		"发音（4 分）",
	} {
		if !strings.Contains(report.SpeakingOverall.Explanation, expected) {
			t.Errorf(
				"overall explanation %q does not contain %q",
				report.SpeakingOverall.Explanation,
				expected,
			)
		}
	}

	expectedEvidence := map[scoring.IELTSCriterion]struct {
		part    string
		excerpt string
	}{
		scoring.IELTSCriterionFC: {
			part: "Part 1", excerpt: "I explain answer 1 clearly with a concrete example.",
		},
		scoring.IELTSCriterionLR: {
			part: "Part 2", excerpt: "I explain answer 4 clearly with a concrete example.",
		},
		scoring.IELTSCriterionGRA: {
			part: "Part 3", excerpt: "I explain answer 5 clearly with a concrete example.",
		},
		scoring.IELTSCriterionPR: {
			part: "Part 3", excerpt: "I explain answer 7 clearly with a concrete example.",
		},
	}
	for _, criterion := range report.Criteria {
		expected := expectedEvidence[criterion.CriterionID]
		for _, fragment := range []string{expected.part, expected.excerpt} {
			if !strings.Contains(criterion.Explanation, fragment) {
				t.Errorf(
					"%s explanation %q does not contain %q",
					criterion.CriterionID,
					criterion.Explanation,
					fragment,
				)
			}
		}
		if criterion.CriterionID == scoring.IELTSCriterionPR {
			assertIELTSExplanationHasNoInventedPhoneme(
				t,
				criterion.Explanation,
			)
		}
	}

	wantOrder := []scoring.IELTSCriterion{
		scoring.IELTSCriterionPR,
		scoring.IELTSCriterionLR,
		scoring.IELTSCriterionFC,
	}
	if len(report.PriorityActions) != len(wantOrder) {
		t.Fatalf(
			"priority action count = %d, want %d: %#v",
			len(report.PriorityActions),
			len(wantOrder),
			report.PriorityActions,
		)
	}
	seenSuggestions := make(map[string]struct{}, len(report.PriorityActions))
	for index, action := range report.PriorityActions {
		if action.CriterionID != wantOrder[index] {
			t.Errorf(
				"priority action %d criterion = %s, want %s",
				index,
				action.CriterionID,
				wantOrder[index],
			)
		}
		finding := ieltsContentReportFinding(report, action)
		normalized := strings.ToLower(strings.Join(
			strings.Fields(finding.Suggestion),
			" ",
		))
		if _, duplicate := seenSuggestions[normalized]; duplicate {
			t.Errorf("duplicate priority suggestion %q", finding.Suggestion)
		}
		seenSuggestions[normalized] = struct{}{}
	}

	formal, err := ProjectIELTSFormalReport(snapshot, result)
	if err != nil {
		t.Fatalf("ProjectIELTSFormalReport: %v", err)
	}
	var detail IELTSSpeakingReport
	if err := json.Unmarshal(formal.Detail, &detail); err != nil {
		t.Fatalf("decode IELTS formal detail: %v", err)
	}
	if !reflect.DeepEqual(detail, report) {
		t.Errorf("formal detail differs from standalone report")
	}
	if len(formal.PriorityActions) != len(detail.PriorityActions) {
		t.Fatalf(
			"formal priority action count = %d, detail = %d",
			len(formal.PriorityActions),
			len(detail.PriorityActions),
		)
	}
	for index, action := range detail.PriorityActions {
		formalAction := formal.PriorityActions[index]
		if formalAction.DimensionKey != string(action.CriterionID) ||
			formalAction.FindingID != action.FindingID {
			t.Errorf(
				"formal priority action %d = %#v, detail = %#v",
				index,
				formalAction,
				action,
			)
		}
	}
}

func assertIELTSExplanationHasNoInventedPhoneme(
	t *testing.T,
	explanation string,
) {
	t.Helper()
	for _, unsupported := range []string{
		"/θ/", "/ð/", "th 音", "元音发成", "辅音发成", "重音落在",
	} {
		if strings.Contains(explanation, unsupported) {
			t.Errorf(
				"pronunciation explanation invents unsupported phoneme evidence: %q",
				explanation,
			)
		}
	}
}

type ieltsContentReportProvider struct{}

func (*ieltsContentReportProvider) AnalyzeIELTSCriterion(
	_ context.Context,
	request scoring.IELTSSpeakingCriterionProviderRequest,
) (scoring.IELTSSpeakingShadowProviderResult, error) {
	input := request.Input
	criterion := input.AssessableCriteria[0]
	responseIndex := map[scoring.IELTSCriterion]int{
		scoring.IELTSCriterionFC:  0,
		scoring.IELTSCriterionLR:  3,
		scoring.IELTSCriterionGRA: 4,
		scoring.IELTSCriterionPR:  6,
	}[criterion]
	response := input.Questions[responseIndex].Response
	if response == nil {
		panic("IELTS content report fixture requires every response")
	}
	band := map[scoring.IELTSCriterion]int{
		scoring.IELTSCriterionFC:  7,
		scoring.IELTSCriterionLR:  5,
		scoring.IELTSCriterionGRA: 6,
		scoring.IELTSCriterionPR:  4,
	}[criterion]
	suggestion := map[scoring.IELTSCriterion]string{
		scoring.IELTSCriterionFC:  "Link the idea with one clear reason and example.",
		scoring.IELTSCriterionLR:  "Replace general words with one precise topic noun.",
		scoring.IELTSCriterionGRA: "replace GENERAL words with one precise topic noun.",
		scoring.IELTSCriterionPR:  "Record the whole sentence again and compare its overall clarity.",
	}[criterion]
	token := strings.ToLower(strings.TrimPrefix(string(criterion), "IELTS_"))
	anchor := ieltsReportProviderAnchor{
		EvidenceRefID: response.EvidenceRefID,
		Quote:         response.Transcript,
		Occurrence:    1,
	}
	payload := ieltsReportProviderPayload{
		SchemaVersion: scoring.IELTSSpeakingShadowProviderSchemaVersion,
		Criteria: []ieltsReportProviderCriterion{{
			CriterionID: criterion,
			RubricDescriptor: ieltsContentReportDescriptor(
				input,
				criterion,
				band,
			),
			Strengths: []ieltsReportProviderFinding{{
				TemplateID: "ielts." + token + ".strength.v1",
				Evidence:   []ieltsReportProviderAnchor{anchor},
			}},
			Improvements: []ieltsReportProviderFinding{{
				TemplateID: "ielts." + token + ".improvement.v1",
				Suggestion: suggestion,
				Evidence:   []ieltsReportProviderAnchor{anchor},
			}},
			UpgradeExamples: []ieltsReportProviderFinding{},
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return scoring.IELTSSpeakingShadowProviderResult{}, err
	}
	return scoring.IELTSSpeakingShadowProviderResult{
		Payload:   raw,
		Provider:  "provider",
		Model:     "model",
		RequestID: "content-request-" + token,
	}, nil
}

func ieltsContentReportDescriptor(
	input scoring.IELTSSpeakingShadowProviderInput,
	criterion scoring.IELTSCriterion,
	band int,
) string {
	for _, set := range input.RubricDescriptors {
		if set.CriterionID == criterion {
			return set.Descriptors[band-1].ID
		}
	}
	panic("IELTS content report fixture has no descriptor")
}

func ieltsContentReportAcousticFixture(
	t *testing.T,
) (evidence.EvidenceSnapshot, scoring.IELTSAcousticSnapshot) {
	t.Helper()
	base := ieltsReportTestSnapshot(t, ieltsReportQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(base.Payload, &payload); err != nil {
		t.Fatalf("decode IELTS content fixture: %v", err)
	}
	for index := range payload.ConfirmedTurns {
		turn := &payload.ConfirmedTurns[index]
		turn.Audio = evidence.Audio{
			Availability: "AVAILABLE",
			AudioAssetID: "audio-" + turn.TurnID,
			ChecksumSHA256: strings.Repeat(
				string("abcdef0123456789"[index%16]),
				64,
			),
			DurationMS:  4_000,
			ContentType: "audio/wav",
			SizeBytes:   64_000,
			Status:      "readable",
			Version:     1,
			Quality:     reportEvidenceNotAssessed,
			ISE:         reportEvidenceNotAssessed,
		}
		payload.EvidenceRefs[index].AudioSpan = &evidence.AudioSpan{
			AudioAssetID: turn.Audio.AudioAssetID,
			StartMS:      0,
			EndMS:        turn.Audio.DurationMS,
		}
		payload.EvidenceRefs[index].Lineage.AudioAssetVersion =
			turn.Audio.Version
		payload.VersionManifest.TurnEvidence[index].AudioVersion =
			turn.Audio.Version
	}
	snapshot := rebuildReportTestSnapshot(t, payload, base.SceneType)
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatalf("decode rebuilt IELTS content fixture: %v", err)
	}
	read := scoring.IELTSSpeakingAcousticRead{
		Values: make(
			[]scoring.IELTSSpeakingTurnAcoustics,
			0,
			len(payload.ConfirmedTurns),
		),
	}
	for index, turn := range payload.ConfirmedTurns {
		fluency := 76.0
		read.Values = append(read.Values, scoring.IELTSSpeakingTurnAcoustics{
			TurnID:               turn.TurnID,
			EvidenceRefID:        payload.EvidenceRefs[index].EvidenceRefID,
			PronunciationScore:   72,
			AcousticFluencyScore: &fluency,
			Provider:             "xfyun_ise",
			ProviderRun:          "run_0123456789abcdef01234567",
		})
	}
	acoustics, err := scoring.BuildIELTSAcousticSnapshot(
		"20000000-0000-4000-8000-000000000002",
		snapshot,
		read,
		true,
	)
	if err != nil {
		t.Fatalf("BuildIELTSAcousticSnapshot: %v", err)
	}
	return snapshot, acoustics
}

func ieltsContentReportFinding(
	report IELTSSpeakingReport,
	action IELTSSpeakingReportPriorityRef,
) scoring.IELTSSpeakingShadowFinding {
	for _, criterion := range report.Criteria {
		if criterion.CriterionID != action.CriterionID {
			continue
		}
		for _, finding := range criterion.Improvements {
			if finding.ID == action.FindingID {
				return finding
			}
		}
	}
	panic("priority action does not reference an improvement")
}
