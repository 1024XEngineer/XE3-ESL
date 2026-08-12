package report

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

const IELTSSpeakingPracticeReportSchemaVersion = "ielts-speaking-practice-report/v1"

type IELTSSpeakingPracticeReportScope string

const (
	IELTSSpeakingPracticeReportPart1  IELTSSpeakingPracticeReportScope = "PART_1"
	IELTSSpeakingPracticeReportPart23 IELTSSpeakingPracticeReportScope = "PART_2_3"
	IELTSSpeakingPracticeReportPart3  IELTSSpeakingPracticeReportScope = "PART_3"
)

type IELTSSpeakingPracticeReport struct {
	SchemaVersion     string                                  `json:"schema_version"`
	ReportScope       IELTSSpeakingPracticeReportScope        `json:"report_scope"`
	AvailableSections []scoring.IELTSPart                     `json:"available_sections"`
	Questions         []IELTSSpeakingPracticeReportQuestion   `json:"questions"`
	SectionReviews    []IELTSSpeakingPracticeReportPartReview `json:"section_reviews"`
}

type IELTSSpeakingPracticeReportQuestion struct {
	QuestionID          string            `json:"question_id"`
	PartID              scoring.IELTSPart `json:"part_id"`
	Index               int               `json:"index"`
	QuestionText        string            `json:"question_text"`
	ConfirmedTranscript string            `json:"confirmed_transcript,omitempty"`
	ResponseTurnID      string            `json:"response_turn_id,omitempty"`
	EvidenceRefIDs      []string          `json:"evidence_ref_ids"`
}

type IELTSSpeakingPracticeReportPartReview struct {
	PartID                   scoring.IELTSPart `json:"part_id"`
	QuestionIndexes          []int             `json:"question_indexes"`
	EvidenceRefIDs           []string          `json:"evidence_ref_ids"`
	StrengthFindingIDs       []string          `json:"strength_finding_ids"`
	ImprovementFindingIDs    []string          `json:"improvement_finding_ids"`
	UpgradeExampleFindingIDs []string          `json:"upgrade_example_finding_ids"`
}

func ProjectIELTSSpeakingPracticeReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.GeneralSceneResult,
) (IELTSSpeakingPracticeReport, error) {
	if err := scoring.ValidateGeneralSceneResult(snapshot, result); err != nil {
		return IELTSSpeakingPracticeReport{}, err
	}
	if snapshot.SceneType != evaluation.SceneIELTSSpeaking ||
		result.SceneType != evaluation.SceneIELTSSpeaking {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	parts, scope, ok := ieltsPracticeQuestionParts(payload.PracticeContext)
	if !ok || len(parts) != len(payload.OpportunityManifest) {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	turns := make(map[string]evidence.ConfirmedTurn, len(payload.ConfirmedTurns))
	refs := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	for _, turn := range payload.ConfirmedTurns {
		turns[turn.TurnID] = turn
	}
	for _, ref := range payload.EvidenceRefs {
		refs[ref.TurnID] = ref
	}
	available := ieltsPracticeAvailableSections(parts)
	report := IELTSSpeakingPracticeReport{
		SchemaVersion:     IELTSSpeakingPracticeReportSchemaVersion,
		ReportScope:       scope,
		AvailableSections: available,
		Questions: make(
			[]IELTSSpeakingPracticeReportQuestion,
			len(payload.OpportunityManifest),
		),
		SectionReviews: make(
			[]IELTSSpeakingPracticeReportPartReview,
			len(available),
		),
	}
	refParts := make(map[string]scoring.IELTSPart, len(payload.EvidenceRefs))
	for index, opportunity := range payload.OpportunityManifest {
		if opportunity.Sequence != index+1 {
			return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
		}
		question := IELTSSpeakingPracticeReportQuestion{
			QuestionID:     opportunity.QuestionID,
			PartID:         parts[index],
			Index:          index + 1,
			QuestionText:   opportunity.QuestionText,
			EvidenceRefIDs: []string{},
		}
		if opportunity.ResponseTurnID != "" {
			turn, turnOK := turns[opportunity.ResponseTurnID]
			ref, refOK := refs[opportunity.ResponseTurnID]
			if !turnOK || !refOK || turn.QuestionID != opportunity.QuestionID ||
				turn.Sequence != opportunity.Sequence {
				return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
			}
			question.ConfirmedTranscript = turn.Transcript.Text
			question.ResponseTurnID = turn.TurnID
			question.EvidenceRefIDs = []string{ref.EvidenceRefID}
			refParts[ref.EvidenceRefID] = parts[index]
		}
		report.Questions[index] = question
	}
	for index, part := range available {
		report.SectionReviews[index] = ieltsPracticePartReview(
			part,
			report.Questions,
			result.Dimensions,
			refParts,
		)
	}
	if !report.Valid() {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	return report, nil
}

func ProjectIELTSSpeakingBandPracticeReport(
	snapshot evidence.EvidenceSnapshot,
	result scoring.IELTSSpeakingShadowResult,
) (IELTSSpeakingPracticeReport, error) {
	if err := scoring.ValidateIELTSSpeakingShadowResult(snapshot, result); err != nil {
		return IELTSSpeakingPracticeReport{}, err
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	parts, scope, ok := ieltsPracticeQuestionParts(payload.PracticeContext)
	if !ok || len(parts) != len(result.QuestionResults) ||
		len(parts) != len(payload.OpportunityManifest) {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	turns := make(map[string]evidence.ConfirmedTurn, len(payload.ConfirmedTurns))
	for _, turn := range payload.ConfirmedTurns {
		turns[turn.TurnID] = turn
	}
	report := IELTSSpeakingPracticeReport{
		SchemaVersion:     IELTSSpeakingPracticeReportSchemaVersion,
		ReportScope:       scope,
		AvailableSections: ieltsPracticeAvailableSections(parts),
		Questions:         make([]IELTSSpeakingPracticeReportQuestion, len(parts)),
		SectionReviews:    []IELTSSpeakingPracticeReportPartReview{},
	}
	for index, resultQuestion := range result.QuestionResults {
		opportunity := payload.OpportunityManifest[index]
		question := IELTSSpeakingPracticeReportQuestion{
			QuestionID:     resultQuestion.QuestionID,
			PartID:         resultQuestion.PartID,
			Index:          resultQuestion.Index,
			QuestionText:   opportunity.QuestionText,
			ResponseTurnID: resultQuestion.ResponseTurnID,
			EvidenceRefIDs: slices.Clone(resultQuestion.EvidenceRefIDs),
		}
		if resultQuestion.ResponseTurnID != "" {
			turn, exists := turns[resultQuestion.ResponseTurnID]
			if !exists || turn.QuestionID != resultQuestion.QuestionID ||
				turn.Sequence != resultQuestion.Index {
				return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
			}
			question.ConfirmedTranscript = turn.Transcript.Text
		}
		report.Questions[index] = question
	}
	for _, section := range report.AvailableSections {
		review := IELTSSpeakingPracticeReportPartReview{
			PartID:                   section,
			QuestionIndexes:          []int{},
			EvidenceRefIDs:           []string{},
			StrengthFindingIDs:       []string{},
			ImprovementFindingIDs:    []string{},
			UpgradeExampleFindingIDs: []string{},
		}
		strengths := map[string]struct{}{}
		improvements := map[string]struct{}{}
		upgrades := map[string]struct{}{}
		for _, question := range result.QuestionResults {
			if question.PartID != section {
				continue
			}
			review.QuestionIndexes = append(review.QuestionIndexes, question.Index)
			review.EvidenceRefIDs = append(review.EvidenceRefIDs, question.EvidenceRefIDs...)
			for _, criterion := range question.CriterionFindings {
				appendUniqueStrings(&review.StrengthFindingIDs, strengths, criterion.StrengthFindingIDs)
				appendUniqueStrings(&review.ImprovementFindingIDs, improvements, criterion.ImprovementFindingIDs)
				appendUniqueStrings(&review.UpgradeExampleFindingIDs, upgrades, criterion.UpgradeExampleFindingIDs)
			}
		}
		report.SectionReviews = append(report.SectionReviews, review)
	}
	if !report.Valid() {
		return IELTSSpeakingPracticeReport{}, evaluation.ErrInvalidRequest
	}
	return report, nil
}

func (report IELTSSpeakingPracticeReport) Valid() bool {
	expectedSections := expectedIELTSPracticeSections(report.ReportScope)
	if report.SchemaVersion != IELTSSpeakingPracticeReportSchemaVersion ||
		expectedSections == nil ||
		!slices.Equal(report.AvailableSections, expectedSections) ||
		len(report.Questions) == 0 || len(report.Questions) > 64 ||
		!validIELTSPracticeQuestionSequence(report.Questions, expectedSections) ||
		len(report.SectionReviews) != len(expectedSections) {
		return false
	}
	seenQuestions := make(map[string]struct{}, len(report.Questions))
	seenTurns := make(map[string]struct{}, len(report.Questions))
	seenRefs := make(map[string]struct{}, len(report.Questions))
	for index, question := range report.Questions {
		if !slices.Contains(expectedSections, question.PartID) ||
			question.Index != index+1 || !validIdentifier(question.QuestionID) ||
			!validReportText(question.QuestionText, reportMaximumInputString) ||
			question.EvidenceRefIDs == nil || len(question.EvidenceRefIDs) > 1 {
			return false
		}
		if _, duplicate := seenQuestions[question.QuestionID]; duplicate {
			return false
		}
		seenQuestions[question.QuestionID] = struct{}{}
		if question.ResponseTurnID == "" {
			if question.ConfirmedTranscript != "" || len(question.EvidenceRefIDs) != 0 {
				return false
			}
			continue
		}
		if !validIdentifier(question.ResponseTurnID) ||
			!validReportText(question.ConfirmedTranscript, reportMaximumInputString) ||
			len(question.EvidenceRefIDs) != 1 ||
			!validIdentifier(question.EvidenceRefIDs[0]) {
			return false
		}
		if _, duplicate := seenTurns[question.ResponseTurnID]; duplicate {
			return false
		}
		if _, duplicate := seenRefs[question.EvidenceRefIDs[0]]; duplicate {
			return false
		}
		seenTurns[question.ResponseTurnID] = struct{}{}
		seenRefs[question.EvidenceRefIDs[0]] = struct{}{}
	}
	for index, review := range report.SectionReviews {
		if review.PartID != expectedSections[index] ||
			review.EvidenceRefIDs == nil ||
			!slices.Equal(
				review.QuestionIndexes,
				ieltsPracticeQuestionIndexes(report.Questions, review.PartID),
			) ||
			!slices.Equal(
				review.EvidenceRefIDs,
				ieltsPracticeEvidenceRefs(report.Questions, review.PartID),
			) ||
			!validStringList(review.StrengthFindingIDs, 64) ||
			!validStringList(review.ImprovementFindingIDs, 64) ||
			!validStringList(review.UpgradeExampleFindingIDs, 64) {
			return false
		}
	}
	return true
}

func ieltsPracticeQuestionParts(
	context evidence.PracticeContext,
) ([]scoring.IELTSPart, IELTSSpeakingPracticeReportScope, bool) {
	assignment := context.IELTSAssignment
	if context.PracticeExperience != "IELTS_SPEAKING" || assignment == nil ||
		context.EvaluationPolicyRef !=
			scoring.IELTSSpeakingPracticeEvaluationPolicyRef ||
		assignment.Mode != context.PracticeMode ||
		!validIdentifier(assignment.BankID) ||
		strings.TrimSpace(assignment.Season) == "" ||
		strings.TrimSpace(assignment.Season) != assignment.Season ||
		len(assignment.Parts) == 0 {
		return nil, "", false
	}
	var scope IELTSSpeakingPracticeReportScope
	expected := []scoring.IELTSPart{}
	switch context.PracticeMode {
	case "PART_1":
		scope = IELTSSpeakingPracticeReportPart1
		expected = []scoring.IELTSPart{scoring.IELTSPart1}
	case "PART_2":
		scope = IELTSSpeakingPracticeReportPart23
		expected = []scoring.IELTSPart{scoring.IELTSPart2, scoring.IELTSPart3}
	case "PART_3":
		scope = IELTSSpeakingPracticeReportPart3
		expected = []scoring.IELTSPart{scoring.IELTSPart3}
	default:
		return nil, "", false
	}
	if len(assignment.Parts) != len(expected) {
		return nil, "", false
	}
	parts := make([]scoring.IELTSPart, 0, len(context.TaskBlueprints))
	blueprints := make([]string, 0, len(context.TaskBlueprints))
	for index, assignmentPart := range assignment.Parts {
		part := scoring.IELTSPart(assignmentPart.Part)
		if part != expected[index] || !validIdentifier(assignmentPart.SourceID) ||
			len(assignmentPart.TurnBlueprints) == 0 {
			return nil, "", false
		}
		for _, blueprint := range assignmentPart.TurnBlueprints {
			if strings.TrimSpace(blueprint) == "" || strings.TrimSpace(blueprint) != blueprint {
				return nil, "", false
			}
			parts = append(parts, part)
			blueprints = append(blueprints, blueprint)
		}
	}
	if !validIELTSPracticeAssignmentDetails(scope, assignment.Parts) {
		return nil, "", false
	}
	if !slices.Equal(blueprints, context.TaskBlueprints) {
		return nil, "", false
	}
	return parts, scope, true
}

func validIELTSPracticeAssignmentDetails(
	scope IELTSSpeakingPracticeReportScope,
	parts []evidence.IELTSAssignmentPart,
) bool {
	switch scope {
	case IELTSSpeakingPracticeReportPart1:
		return len(parts) == 1 && parts[0].TopicTitle == "" && parts[0].CueCard == ""
	case IELTSSpeakingPracticeReportPart23:
		return len(parts) == 2 &&
			parts[0].SourceID == parts[1].SourceID &&
			strings.TrimSpace(parts[0].TopicTitle) != "" &&
			strings.TrimSpace(parts[0].TopicTitle) == parts[0].TopicTitle &&
			parts[0].TopicTitle == parts[1].TopicTitle &&
			strings.TrimSpace(parts[0].CueCard) != "" &&
			strings.TrimSpace(parts[0].CueCard) == parts[0].CueCard &&
			parts[1].CueCard == "" && len(parts[0].TurnBlueprints) == 1
	case IELTSSpeakingPracticeReportPart3:
		return len(parts) == 1 &&
			strings.TrimSpace(parts[0].TopicTitle) != "" &&
			strings.TrimSpace(parts[0].TopicTitle) == parts[0].TopicTitle &&
			parts[0].CueCard == ""
	default:
		return false
	}
}

func expectedIELTSPracticeSections(
	scope IELTSSpeakingPracticeReportScope,
) []scoring.IELTSPart {
	switch scope {
	case IELTSSpeakingPracticeReportPart1:
		return []scoring.IELTSPart{scoring.IELTSPart1}
	case IELTSSpeakingPracticeReportPart23:
		return []scoring.IELTSPart{scoring.IELTSPart2, scoring.IELTSPart3}
	case IELTSSpeakingPracticeReportPart3:
		return []scoring.IELTSPart{scoring.IELTSPart3}
	default:
		return nil
	}
}

func ieltsPracticeAvailableSections(parts []scoring.IELTSPart) []scoring.IELTSPart {
	sections := make([]scoring.IELTSPart, 0, len(parts))
	for _, part := range parts {
		if len(sections) == 0 || sections[len(sections)-1] != part {
			sections = append(sections, part)
		}
	}
	return sections
}

func ieltsPracticePartReview(
	part scoring.IELTSPart,
	questions []IELTSSpeakingPracticeReportQuestion,
	dimensions []scoring.GeneralSceneDimensionResult,
	refParts map[string]scoring.IELTSPart,
) IELTSSpeakingPracticeReportPartReview {
	review := IELTSSpeakingPracticeReportPartReview{
		PartID:                   part,
		QuestionIndexes:          ieltsPracticeQuestionIndexes(questions, part),
		EvidenceRefIDs:           []string{},
		StrengthFindingIDs:       []string{},
		ImprovementFindingIDs:    []string{},
		UpgradeExampleFindingIDs: []string{},
	}
	for _, question := range questions {
		if question.PartID == part {
			review.EvidenceRefIDs = append(review.EvidenceRefIDs, question.EvidenceRefIDs...)
		}
	}
	for _, dimension := range dimensions {
		appendIELTSPracticeFindingIDs(
			&review.StrengthFindingIDs,
			dimension.Strengths,
			part,
			refParts,
		)
		appendIELTSPracticeFindingIDs(
			&review.ImprovementFindingIDs,
			dimension.Improvements,
			part,
			refParts,
		)
		appendIELTSPracticeFindingIDs(
			&review.UpgradeExampleFindingIDs,
			dimension.Examples,
			part,
			refParts,
		)
	}
	return review
}

func appendIELTSPracticeFindingIDs(
	target *[]string,
	findings []scoring.GeneralSceneFinding,
	part scoring.IELTSPart,
	refParts map[string]scoring.IELTSPart,
) {
	for _, finding := range findings {
		if len(finding.Evidence) == 0 {
			continue
		}
		belongs := true
		for _, item := range finding.Evidence {
			if refParts[item.EvidenceRefID] != part {
				belongs = false
				break
			}
		}
		if belongs {
			*target = append(*target, finding.ID)
		}
	}
}

func ieltsPracticeQuestionIndexes(
	questions []IELTSSpeakingPracticeReportQuestion,
	part scoring.IELTSPart,
) []int {
	indexes := make([]int, 0, len(questions))
	for _, question := range questions {
		if question.PartID == part {
			indexes = append(indexes, question.Index)
		}
	}
	return indexes
}

func ieltsPracticeEvidenceRefs(
	questions []IELTSSpeakingPracticeReportQuestion,
	part scoring.IELTSPart,
) []string {
	refs := make([]string, 0, len(questions))
	for _, question := range questions {
		if question.PartID == part {
			refs = append(refs, question.EvidenceRefIDs...)
		}
	}
	return refs
}

func validIELTSPracticeQuestionSequence(
	questions []IELTSSpeakingPracticeReportQuestion,
	sections []scoring.IELTSPart,
) bool {
	if len(questions) == 0 || len(sections) == 0 ||
		questions[0].PartID != sections[0] {
		return false
	}
	sectionIndex := 0
	for _, question := range questions[1:] {
		if question.PartID == sections[sectionIndex] {
			continue
		}
		if sectionIndex+1 >= len(sections) ||
			question.PartID != sections[sectionIndex+1] {
			return false
		}
		sectionIndex++
	}
	return sectionIndex == len(sections)-1
}
