package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

const (
	IELTSSpeakingReportSchemaVersion  = "ielts-speaking-report/v1"
	IELTSSpeakingReportDisclaimerCode = "AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS"
	IELTSSpeakingReportDisclaimer     = "AI 练习估分，非 IELTS 官方成绩。"
	IELTSSpeakingOverallAvailable     = "AVAILABLE"
	IELTSSpeakingOverallNotAvailable  = "NOT_AVAILABLE"
	IELTSTargetPlanNotConfigured      = "NOT_CONFIGURED"
	ieltsMaximumReportQuestions       = 64
)

var (
	ieltsCriterionOrder = scoring.IELTSCriteria()
	ieltsPartOrder      = scoring.IELTSParts()
)

type IELTSSpeakingReport struct {
	SchemaVersion      string                                  `json:"schema_version"`
	DisclaimerCode     string                                  `json:"disclaimer_code"`
	Disclaimer         string                                  `json:"disclaimer"`
	ScoreabilityStatus scoring.IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	GateStatus         scoring.IELTSSpeakingGateStatus         `json:"gate_status"`
	TestSummary        IELTSSpeakingReportTestSummary          `json:"test_summary"`
	Criteria           []IELTSSpeakingReportCriterion          `json:"criteria"`
	SpeakingOverall    IELTSSpeakingReportOverall              `json:"speaking_overall"`
	PartReviews        []IELTSSpeakingReportPartReview         `json:"part_reviews"`
	Questions          []IELTSSpeakingReportQuestion           `json:"questions"`
	TargetPlan         IELTSSpeakingReportTargetPlan           `json:"target_plan"`
	PriorityActions    []IELTSSpeakingReportPriorityRef        `json:"priority_actions"`
}

type IELTSSpeakingReportTestSummary struct {
	Part1Topic          string `json:"part_1_topic"`
	Part2Topic          string `json:"part_2_topic"`
	Part3Topic          string `json:"part_3_topic"`
	QuestionCount       int    `json:"question_count"`
	AnsweredCount       int    `json:"answered_count"`
	RecordingDurationMS int64  `json:"recording_duration_ms"`
}

type IELTSSpeakingReportCriterion struct {
	CriterionID     scoring.IELTSCriterion                  `json:"criterion_id"`
	Scoreability    scoring.IELTSSpeakingScoreabilityStatus `json:"scoreability_status"`
	Gate            scoring.IELTSSpeakingGateStatus         `json:"gate_status"`
	EstimatedBand   *int                                    `json:"estimated_band,omitempty"`
	BandDescriptor  string                                  `json:"band_descriptor,omitempty"`
	Explanation     string                                  `json:"explanation"`
	Coverage        float64                                 `json:"coverage"`
	Confidence      float64                                 `json:"confidence"`
	ReasonCodes     []scoring.IELTSSpeakingReasonCode       `json:"reason_codes"`
	EvidenceRefIDs  []string                                `json:"evidence_ref_ids"`
	Strengths       []scoring.IELTSSpeakingShadowFinding    `json:"strengths"`
	Improvements    []scoring.IELTSSpeakingShadowFinding    `json:"improvements"`
	UpgradeExamples []scoring.IELTSSpeakingShadowFinding    `json:"upgrade_examples"`
}

type IELTSSpeakingReportOverall struct {
	Status        string   `json:"status"`
	EstimatedBand *float64 `json:"estimated_band,omitempty"`
	Explanation   string   `json:"explanation"`
}

type IELTSSpeakingReportPartReview struct {
	PartID                   scoring.IELTSPart `json:"part_id"`
	QuestionIndexes          []int             `json:"question_indexes"`
	EvidenceRefIDs           []string          `json:"evidence_ref_ids"`
	StrengthFindingIDs       []string          `json:"strength_finding_ids"`
	ImprovementFindingIDs    []string          `json:"improvement_finding_ids"`
	UpgradeExampleFindingIDs []string          `json:"upgrade_example_finding_ids"`
}

type IELTSSpeakingReportQuestion struct {
	QuestionID          string                                              `json:"question_id"`
	PartID              scoring.IELTSPart                                   `json:"part_id"`
	Index               int                                                 `json:"index"`
	QuestionText        string                                              `json:"question_text"`
	OpportunityStatus   scoring.IELTSSpeakingOpportunityStatus              `json:"opportunity_status"`
	AssessmentStatus    scoring.IELTSSpeakingAssessmentStatus               `json:"assessment_status"`
	ConfirmedTranscript string                                              `json:"confirmed_transcript,omitempty"`
	ResponseTurnID      string                                              `json:"response_turn_id,omitempty"`
	EvidenceRefIDs      []string                                            `json:"evidence_ref_ids"`
	CriterionFindings   []scoring.IELTSSpeakingQuestionCriterionFindingRefs `json:"criterion_findings"`
}

type IELTSSpeakingReportTargetPlan struct {
	Status string `json:"status"`
}

type IELTSSpeakingReportPriorityRef struct {
	CriterionID scoring.IELTSCriterion `json:"criterion_id"`
	FindingID   string                 `json:"finding_id"`
}

func ProjectIELTSSpeakingReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.IELTSSpeakingShadowResult,
) (IELTSSpeakingReport, error) {
	if err := scoring.ValidateIELTSSpeakingShadowResult(
		snapshot,
		result,
	); err != nil {
		return IELTSSpeakingReport{}, err
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil ||
		ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingReport{},
			scoring.ErrInvalidIELTSSpeakingShadow
	}
	turnsByID := make(
		map[string]evidence.ConfirmedTurn,
		len(payload.ConfirmedTurns),
	)
	for _, turn := range payload.ConfirmedTurns {
		turnsByID[turn.TurnID] = turn
	}
	evidenceLocations := ieltsEvidenceLocations(result.QuestionResults)

	report := IELTSSpeakingReport{
		SchemaVersion:      IELTSSpeakingReportSchemaVersion,
		DisclaimerCode:     IELTSSpeakingReportDisclaimerCode,
		Disclaimer:         IELTSSpeakingReportDisclaimer,
		ScoreabilityStatus: result.Scoreability,
		GateStatus:         result.Gate,
		TestSummary: projectIELTSSpeakingTestSummary(
			payload,
			result.QuestionResults,
		),
		Criteria: make(
			[]IELTSSpeakingReportCriterion,
			len(result.Criteria),
		),
		SpeakingOverall: IELTSSpeakingReportOverall{
			Status:      IELTSSpeakingOverallNotAvailable,
			Explanation: "当前证据还不能同时支持四项评分，因此暂不计算口语总分。",
		},
		PartReviews: make(
			[]IELTSSpeakingReportPartReview,
			len(ieltsPartOrder),
		),
		Questions: make(
			[]IELTSSpeakingReportQuestion,
			len(result.QuestionResults),
		),
		TargetPlan: IELTSSpeakingReportTargetPlan{
			Status: IELTSTargetPlanNotConfigured,
		},
		PriorityActions: []IELTSSpeakingReportPriorityRef{},
	}
	for index, criterion := range result.Criteria {
		report.Criteria[index] =
			projectIELTSSpeakingReportCriterion(
				criterion,
				evidenceLocations,
			)
	}
	report.PriorityActions =
		projectIELTSSpeakingPriorityActions(result.Criteria)
	if overall, available := projectIELTSSpeakingOverall(report.Criteria); available {
		report.SpeakingOverall = overall
	}
	for index, question := range result.QuestionResults {
		opportunity := payload.OpportunityManifest[index]
		projected := IELTSSpeakingReportQuestion{
			QuestionID:        question.QuestionID,
			PartID:            question.PartID,
			Index:             question.Index,
			QuestionText:      opportunity.QuestionText,
			OpportunityStatus: question.OpportunityStatus,
			AssessmentStatus:  question.AssessmentStatus,
			ResponseTurnID:    question.ResponseTurnID,
			EvidenceRefIDs: slices.Clone(
				question.EvidenceRefIDs,
			),
			CriterionFindings: cloneIELTSQuestionCriterionRefs(
				question.CriterionFindings,
			),
		}
		if question.AssessmentStatus == scoring.IELTSAssessmentAssessed {
			turn, exists := turnsByID[question.ResponseTurnID]
			if !exists ||
				turn.QuestionID != question.QuestionID ||
				turn.Sequence != question.Index {
				return IELTSSpeakingReport{},
					scoring.ErrInvalidIELTSSpeakingShadow
			}
			projected.ConfirmedTranscript = turn.Transcript.Text
		}
		report.Questions[index] = projected
	}
	for index, part := range ieltsPartOrder {
		report.PartReviews[index] =
			projectIELTSSpeakingPartReview(
				part,
				report.Questions,
			)
	}
	if !report.Valid() {
		return IELTSSpeakingReport{},
			scoring.ErrInvalidIELTSSpeakingShadow
	}
	return report, nil
}

func projectIELTSSpeakingOverall(
	criteria []IELTSSpeakingReportCriterion,
) (IELTSSpeakingReportOverall, bool) {
	if len(criteria) != len(ieltsCriterionOrder) {
		return IELTSSpeakingReportOverall{}, false
	}
	total := 0
	for index, criterion := range criteria {
		if criterion.CriterionID != ieltsCriterionOrder[index] ||
			criterion.EstimatedBand == nil {
			return IELTSSpeakingReportOverall{}, false
		}
		total += *criterion.EstimatedBand
	}
	band := math.Round((float64(total)/float64(len(criteria)))*2) / 2
	return IELTSSpeakingReportOverall{
		Status:        IELTSSpeakingOverallAvailable,
		EstimatedBand: &band,
		Explanation: ieltsSpeakingOverallExplanation(
			band,
			criteria,
		),
	}, true
}

func projectIELTSSpeakingReportCriterion(
	source scoring.IELTSSpeakingShadowCriterionResult,
	locations map[string]ieltsEvidenceLocation,
) IELTSSpeakingReportCriterion {
	var estimatedBand *int
	if source.EstimatedBand != nil {
		value := *source.EstimatedBand
		estimatedBand = &value
	}
	return IELTSSpeakingReportCriterion{
		CriterionID:    source.CriterionID,
		Scoreability:   source.Scoreability,
		Gate:           source.Gate,
		EstimatedBand:  estimatedBand,
		BandDescriptor: source.BandDescriptor,
		Explanation: ieltsCriterionExplanation(
			source,
			locations,
		),
		Coverage:        source.Coverage,
		Confidence:      source.Confidence,
		ReasonCodes:     slices.Clone(source.ReasonCodes),
		EvidenceRefIDs:  slices.Clone(source.EvidenceRefIDs),
		Strengths:       cloneIELTSFindings(source.Strengths),
		Improvements:    cloneIELTSFindings(source.Improvements),
		UpgradeExamples: cloneIELTSFindings(source.UpgradeExamples),
	}
}

func projectIELTSSpeakingTestSummary(
	payload evidence.SnapshotPayload,
	questions []scoring.IELTSSpeakingShadowQuestionResult,
) IELTSSpeakingReportTestSummary {
	summary := IELTSSpeakingReportTestSummary{
		QuestionCount: len(payload.OpportunityManifest),
	}
	for _, turn := range payload.ConfirmedTurns {
		summary.AnsweredCount++
		summary.RecordingDurationMS += turn.Audio.DurationMS
	}
	for index, question := range questions {
		topic := payload.OpportunityManifest[index].QuestionText
		switch question.PartID {
		case scoring.IELTSPart1:
			if summary.Part1Topic == "" {
				summary.Part1Topic = topic
			}
		case scoring.IELTSPart2:
			if summary.Part2Topic == "" {
				summary.Part2Topic = topic
			}
		case scoring.IELTSPart3:
			if summary.Part3Topic == "" {
				summary.Part3Topic = topic
			}
		}
	}
	return summary
}

func ieltsCriterionExplanation(
	criterion scoring.IELTSSpeakingShadowCriterionResult,
	locations map[string]ieltsEvidenceLocation,
) string {
	if criterion.Scoreability == scoring.IELTSSpeakingScoreabilityInsufficient {
		if criterion.CriterionID == scoring.IELTSCriterionPR {
			return "本次缺少可用于整场评分的可信发音工件，因此不展示发音分数。"
		}
		return "本次可确认的回答证据不足，因此不展示该维度分数。"
	}
	parts := []string{ieltsCriterionBandSummary(criterion)}
	if len(criterion.Strengths) > 0 {
		parts = append(parts, ieltsCriterionFindingSentence(
			criterion.CriterionID,
			criterion.Strengths[0],
			locations,
			true,
		))
	}
	if len(criterion.Improvements) > 0 {
		parts = append(parts, ieltsCriterionFindingSentence(
			criterion.CriterionID,
			criterion.Improvements[0],
			locations,
			false,
		))
	}
	if criterion.EstimatedBand == nil {
		parts = append(parts, "由于缺少完整的时序声学证据，本次只能对观点展开与衔接作定性反馈，暂不展示该维度分数。")
	} else {
		parts = append(parts, fmt.Sprintf(
			"综合这些已核验表现，本维度练习估分为 %d 分。",
			*criterion.EstimatedBand,
		))
	}
	return truncateIELTSReportText(
		strings.Join(nonEmptyIELTSReportParts(parts), " "),
		reportMaximumTextBytes,
	)
}

type ieltsEvidenceLocation struct {
	part    scoring.IELTSPart
	ordinal int
}

func ieltsEvidenceLocations(
	questions []scoring.IELTSSpeakingShadowQuestionResult,
) map[string]ieltsEvidenceLocation {
	result := make(map[string]ieltsEvidenceLocation)
	partOrdinals := make(map[scoring.IELTSPart]int, len(ieltsPartOrder))
	for _, question := range questions {
		partOrdinals[question.PartID]++
		location := ieltsEvidenceLocation{
			part:    question.PartID,
			ordinal: partOrdinals[question.PartID],
		}
		for _, refID := range question.EvidenceRefIDs {
			result[refID] = location
		}
	}
	return result
}

func ieltsEvidenceLocationLabel(location ieltsEvidenceLocation) string {
	switch location.part {
	case scoring.IELTSPart1:
		return fmt.Sprintf("Part 1 第 %d 题", location.ordinal)
	case scoring.IELTSPart2:
		if location.ordinal == 1 {
			return "Part 2"
		}
		return fmt.Sprintf("Part 2 第 %d 题", location.ordinal)
	case scoring.IELTSPart3:
		return fmt.Sprintf("Part 3 第 %d 题", location.ordinal)
	default:
		return "本次回答"
	}
}

func ieltsCriterionFindingSentence(
	criterion scoring.IELTSCriterion,
	finding scoring.IELTSSpeakingShadowFinding,
	locations map[string]ieltsEvidenceLocation,
	strength bool,
) string {
	if len(finding.Evidence) == 0 {
		return ""
	}
	evidence := finding.Evidence[0]
	where := "本次回答中"
	if location, exists := locations[evidence.EvidenceRefID]; exists {
		where = "在" + ieltsEvidenceLocationLabel(location)
	}
	quote := truncateIELTSReportText(evidence.OriginalExcerpt, 240)
	message := truncateIELTSReportText(finding.Message, 300)
	if criterion == scoring.IELTSCriterionPR {
		if strength {
			return fmt.Sprintf(
				"%s，对应原句“%s”的整轮录音为整体可理解度提供了正向声学证据。",
				where,
				quote,
			)
		}
		result := fmt.Sprintf(
			"%s，对应原句“%s”的整轮录音显示整体清晰度或稳定性仍可提升。",
			where,
			quote,
		)
		result += "建议：选取完整句子进行两轮跟读和录音对比，复听时只检查整句是否容易听清、整体表达是否稳定。"
		return result
	}
	if strength {
		return fmt.Sprintf(
			"%s，原句“%s”是本维度的一项优势证据：%s",
			where,
			quote,
			message,
		)
	}
	result := fmt.Sprintf(
		"%s，原句“%s”需要优先关注：%s",
		where,
		quote,
		message,
	)
	if finding.Suggestion != "" {
		result += " 建议：" +
			truncateIELTSReportText(finding.Suggestion, 420)
	}
	return result
}

func ieltsCriterionBandSummary(
	criterion scoring.IELTSSpeakingShadowCriterionResult,
) string {
	if criterion.EstimatedBand == nil {
		if criterion.CriterionID == scoring.IELTSCriterionFC {
			return "从已确认的回答看，考生能够表达主要观点；本次重点观察回答是否围绕问题展开，以及观点之间是否容易跟随。"
		}
		return ieltsCriterionDefaultSummary(criterion.CriterionID)
	}
	band := *criterion.EstimatedBand
	summaries := map[scoring.IELTSCriterion][]string{
		scoring.IELTSCriterionFC: {
			"能够表达部分基本意思，但观点展开与衔接还不稳定，听者需要较多推断。",
			"能够围绕熟悉话题给出基本回答，但展开常较短，连接或自我修正会影响连贯性。",
			"能够较清楚地回答并展开主要观点，整体容易跟随；较长回答中的衔接、重复或停顿控制仍不稳定。",
			"能够较自然地延展观点并保持逻辑联系，偶尔的重复或犹豫通常不妨碍交流。",
			"能够流畅、连贯且有层次地发展观点，衔接自然，听者几乎无需额外推断。",
		},
		scoring.IELTSCriterionLR: {
			"能够使用基础词汇表达部分意思，但范围和准确性会限制信息传达。",
			"具备熟悉话题所需的常用词汇，但重复、模糊选词或不自然搭配会影响精确度。",
			"词汇范围足以讨论常见话题并表达主要意思，较复杂内容中的选词、搭配和改述仍有不稳定之处。",
			"能够较灵活、准确地选择词汇并进行改述，少量不恰当用词通常不妨碍理解。",
			"能够广泛、自然且精确地使用词汇和搭配，并根据话题灵活调整表达。",
		},
		scoring.IELTSCriterionGRA: {
			"能够构成部分基础句，但语法错误和句子控制会明显限制意思表达。",
			"能够使用简单句并尝试复杂结构，但准确性不稳定，错误有时会增加理解负担。",
			"能够混合使用简单句和部分复杂句，主要意思清楚；复杂结构、时态或句子完整性仍会出现偏差。",
			"能够使用多样句式并较好控制复杂结构，多数句子准确，少量错误通常不影响交流。",
			"能够灵活运用丰富句式并持续保持较高准确性，复杂表达自然且清楚。",
		},
		scoring.IELTSCriterionPR: {
			"部分内容可以听懂，但整体清晰度与语流稳定性会给理解带来明显负担。",
			"多数基本内容可以听懂，但整句清晰度不够稳定，需要听者适应。",
			"整体发音清楚、主要意思容易听懂；较长表达中的清晰度与语流稳定性仍可提升。",
			"整体容易听懂，语流较自然，偶尔的清晰度波动通常不影响交流。",
			"发音持续清晰自然，整体语流控制成熟，听者几乎不需要额外适应。",
		},
	}
	values, exists := summaries[criterion.CriterionID]
	if !exists {
		return ""
	}
	index := 0
	switch {
	case band >= 8:
		index = 4
	case band >= 7:
		index = 3
	case band >= 6:
		index = 2
	case band >= 5:
		index = 1
	}
	return values[index]
}

func ieltsCriterionDefaultSummary(criterion scoring.IELTSCriterion) string {
	switch criterion {
	case scoring.IELTSCriterionFC:
		return "根据已确认回答评估观点衔接与话题展开；缺少完整时序证据时只提供定性反馈。"
	case scoring.IELTSCriterionLR:
		return "根据已确认回答评估词汇范围、准确性、搭配和改述能力。"
	case scoring.IELTSCriterionGRA:
		return "根据已确认回答评估句式范围、复杂结构控制和语法准确性。"
	case scoring.IELTSCriterionPR:
		return "根据整轮录音的声学证据评估整体可理解度、清晰度与语流稳定性。"
	default:
		return ""
	}
}

func ieltsSpeakingOverallExplanation(
	band float64,
	criteria []IELTSSpeakingReportCriterion,
) string {
	intro := "你能够表达部分基本信息，但回答的完整度、语言准确性和清晰度仍会明显影响交流。"
	switch {
	case band >= 8:
		intro = "你能够流畅、清晰且准确地讨论熟悉和抽象话题，语言运用自然，交流几乎不受影响。"
	case band >= 7:
		intro = "你能够自然地展开观点并保持连贯交流，词汇和句式运用较灵活；少量犹豫或错误通常不影响理解。"
	case band >= 6:
		intro = "你能够清楚回答问题并表达主要观点，交流大多可以顺利进行；回答变长或内容更复杂时，连贯性、准确性或清晰度仍会波动。"
	case band >= 5:
		intro = "你能够就熟悉话题表达主要意思，听者通常可以理解；但回答变长或内容更复杂时，观点展开和语言控制还不够稳定。"
	case band < 4.5:
		intro = "你能够传达部分基本信息，但回答的完整度、语言准确性和清晰度仍会明显影响交流。"
	}
	intro = fmt.Sprintf("本次口语练习估分为 %s 分。%s", formatIELTSBand(band), intro)
	if len(criteria) == 0 {
		return intro
	}
	minimum := 10
	maximum := 0
	for _, criterion := range criteria {
		if criterion.EstimatedBand == nil {
			continue
		}
		if *criterion.EstimatedBand < minimum {
			minimum = *criterion.EstimatedBand
		}
		if *criterion.EstimatedBand > maximum {
			maximum = *criterion.EstimatedBand
		}
	}
	if maximum == 0 || minimum == 10 {
		return intro
	}
	if maximum == minimum {
		return truncateIELTSReportText(
			fmt.Sprintf(
				"%s 四项均为 %d 分，当前表现较为均衡，没有明显的单项优势或短板。下一步建议进行完整回答训练：每次连续回答 60 秒，并依次检查观点是否展开、用词是否准确、句式是否完整、表达是否容易听懂。",
				intro,
				maximum,
			),
			reportMaximumTextBytes,
		)
	}
	highNames := []string{}
	lowNames := []string{}
	for _, criterion := range criteria {
		if criterion.EstimatedBand == nil {
			continue
		}
		if *criterion.EstimatedBand == maximum {
			highNames = append(
				highNames,
				ieltsCriterionName(criterion.CriterionID),
			)
		}
		if *criterion.EstimatedBand == minimum {
			lowNames = append(
				lowNames,
				ieltsCriterionName(criterion.CriterionID),
			)
		}
	}
	result := intro
	if len(highNames) == 1 {
		result += fmt.Sprintf(
			" 当前优势是%s（%d 分）：%s",
			strings.Join(highNames, "、"),
			maximum,
			ieltsOverallCriterionMeaning(criteria, maximum),
		)
	} else {
		result += fmt.Sprintf(
			" 当前相对优势是%s（%d 分），这些维度共同支撑了你的整体表现。",
			strings.Join(highNames, "、"),
			maximum,
		)
	}
	if len(lowNames) == 1 {
		result += fmt.Sprintf(
			" 首要提升的是%s（%d 分）：%s",
			strings.Join(lowNames, "、"),
			minimum,
			ieltsOverallCriterionMeaning(criteria, minimum),
		)
	} else {
		result += fmt.Sprintf(
			" 优先提升的是%s（%d 分），需要结合下方反馈逐项练习。",
			strings.Join(lowNames, "、"),
			minimum,
		)
	}
	result += " 下一步先做：" + ieltsOverallNextStep(criteria, minimum)
	return truncateIELTSReportText(result, reportMaximumTextBytes)
}

func formatIELTSBand(band float64) string {
	if band == float64(int(band)) {
		return fmt.Sprintf("%d", int(band))
	}
	return fmt.Sprintf("%.1f", band)
}

func ieltsOverallCriterionMeaning(
	criteria []IELTSSpeakingReportCriterion,
	band int,
) string {
	for _, criterion := range criteria {
		if criterion.EstimatedBand == nil || *criterion.EstimatedBand != band {
			continue
		}
		switch criterion.CriterionID {
		case scoring.IELTSCriterionFC:
			if band >= 6 {
				return "主要观点通常容易跟随，较长回答中的衔接和展开仍可更稳定。"
			}
			return "你能给出基本回答，但停顿、重复或重新起句会打断观点推进。"
		case scoring.IELTSCriterionLR:
			if band >= 6 {
				return "现有词汇能支撑话题表达，复杂内容中的选词和改述仍可更准确。"
			}
			return "常用词汇可以传达基本意思，但重复、模糊选词或搭配不自然会降低表达的准确度。"
		case scoring.IELTSCriterionGRA:
			if band >= 6 {
				return "你能使用简单句和部分复杂句表达主要意思，较复杂结构的稳定性仍可提升。"
			}
			return "你能够使用基础句式，但语法错误或句子不完整有时会让意思变得不够清楚。"
		case scoring.IELTSCriterionPR:
			if band >= 6 {
				return "整体发音较清楚，主要意思容易听懂，较长表达中的清晰度仍可更稳定。"
			}
			return "多数基本内容可以听懂，但整句清晰度和语流稳定性仍需要加强。"
		}
	}
	return ""
}

func ieltsOverallNextStep(
	criteria []IELTSSpeakingReportCriterion,
	minimum int,
) string {
	for _, criterion := range criteria {
		if criterion.EstimatedBand == nil || *criterion.EstimatedBand != minimum {
			continue
		}
		switch criterion.CriterionID {
		case scoring.IELTSCriterionFC:
			return "用“观点—原因—例子”结构完成 60 秒连续作答；录音复听时标出停顿、重复和重新起句的位置，再完整重答一次。"
		case scoring.IELTSCriterionLR:
			return "围绕一个高频话题准备 5 组常用搭配和 2 种改述方式，并在 60 秒回答中实际使用，避免只背单词。"
		case scoring.IELTSCriterionGRA:
			return "选取本次回答中的 3 个基础句，分别加入原因、条件或对比信息，改写后朗读并检查句子是否完整准确。"
		case scoring.IELTSCriterionPR:
			return "选取一段完整回答做两轮跟读和录音对比，复听时只检查整句是否容易听清、节奏是否稳定。"
		}
	}
	return "结合下方各维度的原句和练法，先完成一轮针对性练习。"
}

func ieltsCriterionName(criterion scoring.IELTSCriterion) string {
	switch criterion {
	case scoring.IELTSCriterionFC:
		return "流利性与连贯性"
	case scoring.IELTSCriterionLR:
		return "词汇丰富度"
	case scoring.IELTSCriterionGRA:
		return "语法多样性及准确性"
	case scoring.IELTSCriterionPR:
		return "发音"
	default:
		return string(criterion)
	}
}

func projectIELTSSpeakingPriorityActions(
	criteria []scoring.IELTSSpeakingShadowCriterionResult,
) []IELTSSpeakingReportPriorityRef {
	ordered := slices.Clone(criteria)
	slices.SortStableFunc(
		ordered,
		func(left, right scoring.IELTSSpeakingShadowCriterionResult) int {
			leftBand := 10
			rightBand := 10
			if left.EstimatedBand != nil {
				leftBand = *left.EstimatedBand
			}
			if right.EstimatedBand != nil {
				rightBand = *right.EstimatedBand
			}
			return leftBand - rightBand
		},
	)
	result := make([]IELTSSpeakingReportPriorityRef, 0, 3)
	seen := make(map[string]struct{})
	appendFinding := func(
		criterion scoring.IELTSSpeakingShadowCriterionResult,
		finding scoring.IELTSSpeakingShadowFinding,
	) {
		if len(result) == 3 {
			return
		}
		content := finding.Suggestion
		if content == "" {
			content = finding.Message
		}
		key := ieltsPriorityActionKey(content)
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		result = append(result, IELTSSpeakingReportPriorityRef{
			CriterionID: criterion.CriterionID,
			FindingID:   finding.ID,
		})
	}
	for _, criterion := range ordered {
		if len(criterion.Improvements) > 0 {
			appendFinding(criterion, criterion.Improvements[0])
		}
	}
	for _, criterion := range ordered {
		for index := 1; index < len(criterion.Improvements); index++ {
			finding := criterion.Improvements[index]
			appendFinding(criterion, finding)
		}
	}
	return result
}

func ieltsPriorityActionKey(content string) string {
	const providerAdjustmentMarker = "结合本次原句，还可以这样调整："
	if index := strings.LastIndex(
		content,
		providerAdjustmentMarker,
	); index >= 0 {
		content = content[index+len(providerAdjustmentMarker):]
	}
	return strings.ToLower(strings.Join(strings.Fields(content), " "))
}

func nonEmptyIELTSReportParts(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return result
}

func truncateIELTSReportText(value string, maximumBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximumBytes {
		return value
	}
	if maximumBytes < len("…") {
		return ""
	}
	if maximumBytes == len("…") {
		return "…"
	}
	limit := maximumBytes - len("…")
	cut := 0
	for index, current := range value {
		end := index + utf8.RuneLen(current)
		if end > limit {
			break
		}
		cut = end
	}
	return strings.TrimSpace(value[:cut]) + "…"
}

func cloneIELTSFindings(
	source []scoring.IELTSSpeakingShadowFinding,
) []scoring.IELTSSpeakingShadowFinding {
	result := make([]scoring.IELTSSpeakingShadowFinding, len(source))
	for index, finding := range source {
		result[index] = finding
		result[index].Evidence = slices.Clone(finding.Evidence)
	}
	return result
}

func cloneIELTSQuestionCriterionRefs(
	source []scoring.IELTSSpeakingQuestionCriterionFindingRefs,
) []scoring.IELTSSpeakingQuestionCriterionFindingRefs {
	result := make(
		[]scoring.IELTSSpeakingQuestionCriterionFindingRefs,
		len(source),
	)
	for index, refs := range source {
		result[index] = scoring.IELTSSpeakingQuestionCriterionFindingRefs{
			CriterionID: refs.CriterionID,
			StrengthFindingIDs: slices.Clone(
				refs.StrengthFindingIDs,
			),
			ImprovementFindingIDs: slices.Clone(
				refs.ImprovementFindingIDs,
			),
			UpgradeExampleFindingIDs: slices.Clone(
				refs.UpgradeExampleFindingIDs,
			),
		}
	}
	return result
}

func projectIELTSSpeakingPartReview(
	part scoring.IELTSPart,
	questions []IELTSSpeakingReportQuestion,
) IELTSSpeakingReportPartReview {
	result := IELTSSpeakingReportPartReview{
		PartID:                   part,
		QuestionIndexes:          []int{},
		EvidenceRefIDs:           []string{},
		StrengthFindingIDs:       []string{},
		ImprovementFindingIDs:    []string{},
		UpgradeExampleFindingIDs: []string{},
	}
	evidenceSet := make(map[string]struct{})
	strengthSet := make(map[string]struct{})
	improvementSet := make(map[string]struct{})
	upgradeSet := make(map[string]struct{})
	for _, question := range questions {
		if question.PartID != part {
			continue
		}
		result.QuestionIndexes = append(
			result.QuestionIndexes,
			question.Index,
		)
		for _, refID := range question.EvidenceRefIDs {
			evidenceSet[refID] = struct{}{}
		}
		for _, refs := range question.CriterionFindings {
			appendUniqueStrings(
				&result.StrengthFindingIDs,
				strengthSet,
				refs.StrengthFindingIDs,
			)
			appendUniqueStrings(
				&result.ImprovementFindingIDs,
				improvementSet,
				refs.ImprovementFindingIDs,
			)
			appendUniqueStrings(
				&result.UpgradeExampleFindingIDs,
				upgradeSet,
				refs.UpgradeExampleFindingIDs,
			)
		}
	}
	for refID := range evidenceSet {
		result.EvidenceRefIDs = append(result.EvidenceRefIDs, refID)
	}
	slices.Sort(result.EvidenceRefIDs)
	return result
}

func appendUniqueStrings(
	target *[]string,
	seen map[string]struct{},
	values []string,
) {
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		*target = append(*target, value)
	}
}

func (report IELTSSpeakingReport) Valid() bool {
	if report.SchemaVersion != IELTSSpeakingReportSchemaVersion ||
		report.DisclaimerCode !=
			IELTSSpeakingReportDisclaimerCode ||
		report.Disclaimer != IELTSSpeakingReportDisclaimer ||
		!validIELTSRootGate(
			report.ScoreabilityStatus,
			report.GateStatus,
		) ||
		!validIELTSSpeakingTestSummary(report.TestSummary) ||
		len(report.Criteria) != len(ieltsCriterionOrder) ||
		!validIELTSSpeakingOverall(report.SpeakingOverall, report.Criteria) ||
		len(report.PartReviews) != len(ieltsPartOrder) ||
		len(report.Questions) != report.TestSummary.QuestionCount ||
		!validIELTSReportQuestionSequence(report.Questions) ||
		report.TargetPlan.Status != IELTSTargetPlanNotConfigured ||
		report.PriorityActions == nil ||
		len(report.PriorityActions) > 3 {
		return false
	}
	findings := make(
		map[scoring.IELTSCriterion]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	improvements := make(
		map[scoring.IELTSCriterion]map[string]struct{},
		len(ieltsCriterionOrder),
	)
	for index, criterion := range report.Criteria {
		if criterion.CriterionID != ieltsCriterionOrder[index] ||
			!validIELTSReportCriterion(
				report.ScoreabilityStatus,
				criterion,
			) {
			return false
		}
		findings[criterion.CriterionID] = make(map[string]struct{})
		improvements[criterion.CriterionID] =
			make(map[string]struct{})
		for _, collection := range []struct {
			values      []scoring.IELTSSpeakingShadowFinding
			improvement bool
		}{
			{criterion.Strengths, false},
			{criterion.Improvements, true},
			{criterion.UpgradeExamples, false},
		} {
			for _, finding := range collection.values {
				if _, duplicate :=
					findings[criterion.CriterionID][finding.ID]; duplicate {
					return false
				}
				findings[criterion.CriterionID][finding.ID] =
					struct{}{}
				if collection.improvement {
					improvements[criterion.CriterionID][finding.ID] =
						struct{}{}
				}
			}
		}
	}
	answeredCount := 0
	for index, question := range report.Questions {
		if !validIdentifier(question.QuestionID) ||
			question.Index != index+1 ||
			!validReportText(
				question.QuestionText,
				reportMaximumInputString,
			) ||
			question.EvidenceRefIDs == nil ||
			len(question.CriterionFindings) !=
				len(ieltsCriterionOrder) {
			return false
		}
		if question.AssessmentStatus == scoring.IELTSAssessmentAssessed {
			answeredCount++
			if question.OpportunityStatus !=
				scoring.IELTSOpportunityProvided ||
				!validIdentifier(question.ResponseTurnID) ||
				!validReportText(
					question.ConfirmedTranscript,
					reportMaximumInputString,
				) ||
				len(question.EvidenceRefIDs) != 1 {
				return false
			}
		} else if question.AssessmentStatus !=
			scoring.IELTSAssessmentNotAssessed ||
			question.OpportunityStatus !=
				scoring.IELTSOpportunityNotProvided ||
			question.ResponseTurnID != "" ||
			question.ConfirmedTranscript != "" ||
			len(question.EvidenceRefIDs) != 0 {
			return false
		}
		for criterionIndex, refs := range question.CriterionFindings {
			if refs.CriterionID !=
				ieltsCriterionOrder[criterionIndex] ||
				!validStringList(refs.StrengthFindingIDs, 3) ||
				!validStringList(refs.ImprovementFindingIDs, 3) ||
				!validStringList(
					refs.UpgradeExampleFindingIDs,
					3,
				) {
				return false
			}
		}
	}
	if answeredCount != report.TestSummary.AnsweredCount {
		return false
	}
	for index, part := range report.PartReviews {
		if part.PartID != ieltsPartOrder[index] ||
			!slices.Equal(
				part.QuestionIndexes,
				ieltsQuestionIndexes(report.Questions, part.PartID),
			) ||
			!validStringList(
				part.EvidenceRefIDs,
				report.TestSummary.QuestionCount,
			) ||
			!validStringList(part.StrengthFindingIDs, 36) ||
			!validStringList(part.ImprovementFindingIDs, 36) ||
			!validStringList(
				part.UpgradeExampleFindingIDs,
				36,
			) {
			return false
		}
	}
	for _, priority := range report.PriorityActions {
		if _, exists :=
			improvements[priority.CriterionID][priority.FindingID]; !exists {
			return false
		}
	}
	return true
}

func validIELTSSpeakingOverall(
	overall IELTSSpeakingReportOverall,
	criteria []IELTSSpeakingReportCriterion,
) bool {
	if !validReportText(
		overall.Explanation,
		reportMaximumTextBytes,
	) {
		return false
	}
	expected, available := projectIELTSSpeakingOverall(criteria)
	if !available {
		return overall.Status == IELTSSpeakingOverallNotAvailable &&
			overall.EstimatedBand == nil
	}
	return overall.Status == IELTSSpeakingOverallAvailable &&
		overall.EstimatedBand != nil &&
		*overall.EstimatedBand == *expected.EstimatedBand
}

func validIELTSSpeakingTestSummary(
	summary IELTSSpeakingReportTestSummary,
) bool {
	return summary.QuestionCount > 0 &&
		summary.QuestionCount <= ieltsMaximumReportQuestions &&
		summary.AnsweredCount >= 0 &&
		summary.AnsweredCount <= summary.QuestionCount &&
		summary.RecordingDurationMS >= 0 &&
		validReportText(summary.Part1Topic, reportMaximumInputString) &&
		validReportText(summary.Part2Topic, reportMaximumInputString) &&
		validReportText(summary.Part3Topic, reportMaximumInputString)
}

func validIELTSRootGate(
	scoreability scoring.IELTSSpeakingScoreabilityStatus,
	gate scoring.IELTSSpeakingGateStatus,
) bool {
	return (scoreability == scoring.IELTSSpeakingScoreabilityProvisional &&
		gate == scoring.IELTSSpeakingGateFeedbackOnly) ||
		(scoreability == scoring.IELTSSpeakingScoreabilityInsufficient &&
			gate == scoring.IELTSSpeakingGateBlocked)
}

func validIELTSReportCriterion(
	root scoring.IELTSSpeakingScoreabilityStatus,
	criterion IELTSSpeakingReportCriterion,
) bool {
	if !validIELTSRootGate(
		criterion.Scoreability,
		criterion.Gate,
	) ||
		!validReportText(
			criterion.Explanation,
			reportMaximumTextBytes,
		) ||
		criterion.Coverage < 0 || criterion.Coverage > 1 ||
		criterion.Confidence != 0 ||
		criterion.ReasonCodes == nil ||
		criterion.EvidenceRefIDs == nil ||
		criterion.Strengths == nil ||
		criterion.Improvements == nil ||
		criterion.UpgradeExamples == nil {
		return false
	}
	if root == scoring.IELTSSpeakingScoreabilityInsufficient &&
		criterion.Scoreability !=
			scoring.IELTSSpeakingScoreabilityInsufficient {
		return false
	}
	if criterion.Scoreability ==
		scoring.IELTSSpeakingScoreabilityInsufficient {
		return criterion.EstimatedBand == nil &&
			criterion.BandDescriptor == "" &&
			len(criterion.EvidenceRefIDs) == 0 &&
			len(criterion.Strengths) == 0 &&
			len(criterion.Improvements) == 0 &&
			len(criterion.UpgradeExamples) == 0 &&
			len(criterion.ReasonCodes) == 1
	}
	if len(criterion.EvidenceRefIDs) == 0 ||
		len(criterion.Strengths)+len(criterion.Improvements) == 0 {
		return false
	}
	switch criterion.CriterionID {
	case scoring.IELTSCriterionFC:
		if criterion.EstimatedBand == nil {
			return criterion.BandDescriptor == ""
		}
		fallthrough
	case scoring.IELTSCriterionLR, scoring.IELTSCriterionGRA,
		scoring.IELTSCriterionPR:
		if criterion.EstimatedBand == nil {
			return false
		}
		_, descriptor, valid := scoring.MapIELTSBand(
			criterion.CriterionID,
			*criterion.EstimatedBand,
		)
		return valid && criterion.BandDescriptor == descriptor
	default:
		return false
	}
}

func validIELTSReportQuestionSequence(
	questions []IELTSSpeakingReportQuestion,
) bool {
	if len(questions) == 0 || questions[0].PartID != scoring.IELTSPart1 {
		return false
	}
	partIndex := 0
	for _, question := range questions[1:] {
		if question.PartID == ieltsPartOrder[partIndex] {
			continue
		}
		if partIndex+1 >= len(ieltsPartOrder) ||
			question.PartID != ieltsPartOrder[partIndex+1] {
			return false
		}
		partIndex++
	}
	return partIndex == len(ieltsPartOrder)-1
}

func ieltsQuestionIndexes(
	questions []IELTSSpeakingReportQuestion,
	part scoring.IELTSPart,
) []int {
	result := make([]int, 0, len(questions))
	for _, question := range questions {
		if question.PartID == part {
			result = append(result, question.Index)
		}
	}
	return result
}

func validStringList(values []string, maximum int) bool {
	if values == nil || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validIdentifier(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
