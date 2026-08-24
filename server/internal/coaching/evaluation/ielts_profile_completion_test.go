package evaluation

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestIELTSProfileCommandBuilderFreezesPart2Evidence(t *testing.T) {
	lineage := ConfigLineage{
		SchemaVersion:   ConfigLineageSchemaVersion,
		StrategyRef:     "ielts-cumulative-profile/v1",
		PipelineVersion: "ielts-cumulative-profile/v1",
		PromptVersion:   "ielts-cumulative-profile/v1",
		ResultSchema:    IELTSCumulativeProfileSchemaVersion,
		Provider:        "qianwen", Model: "qwen-plus",
	}
	builder, err := NewIELTSProfileCommandBuilder(lineage, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	source := practice.IELTSPartProfileEvidence{
		UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SessionID:      "20000000-0000-4000-8000-000000000001",
		SessionVersion: 3, Stage: practice.IELTSProfileStagePart2,
		CompletedAt: now, Part1Boundary: 1, Part2Boundary: 2,
		Questions: []practice.EvidenceQuestion{
			{ID: "40000000-0000-4000-8000-000000000001", Position: 1,
				Text: "Part 1", SpeakerParticipantID: "assistant",
				AddresseeParticipantIDs: []string{"learner"}},
			{ID: "40000000-0000-4000-8000-000000000002", Position: 2,
				Text: "Part 2", SpeakerParticipantID: "assistant",
				AddresseeParticipantIDs: []string{"learner"}},
		},
		Turns: []practice.EvidenceTurn{
			{ID: "30000000-0000-4000-8000-000000000001", Position: 1,
				QuestionID:              "40000000-0000-4000-8000-000000000001",
				RespondentParticipantID: "learner", Transcript: "Part 1 answer",
				Effective: true, ConfirmedAt: now},
			{ID: "30000000-0000-4000-8000-000000000002", Position: 2,
				QuestionID:              "40000000-0000-4000-8000-000000000002",
				RespondentParticipantID: "learner", Transcript: "Part 2 answer",
				Effective: true, ConfirmedAt: now},
		},
	}

	command, err := builder.Build(source)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if command.Kind != KindIELTSPart2Profile ||
		command.SourceID != source.SessionID || command.ContextID != source.SessionID ||
		command.UserID != source.UserID || command.AvailableAt != now {
		t.Fatalf("command = %#v", command)
	}
	var snapshot IELTSProfileInputSnapshot
	if DecodeStrict(command.InputSnapshot, &snapshot) != nil || !snapshot.Valid() ||
		snapshot.Stage != IELTSProfileStagePart2 || len(snapshot.Turns) != 2 ||
		snapshot.AcousticCapability != AcousticCapabilityNotConfigured ||
		snapshot.DependencyResolution != IELTSProfileDependencyPending {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if string(command.ConfigLineage) == "" || command.InputHash == ([32]byte{}) ||
		command.ConfigHash == ([32]byte{}) {
		t.Fatal("builder omitted immutable hashes or lineage")
	}
}

func TestIELTSProfileCommandBuilderRejectsInvalidStage(t *testing.T) {
	lineage := ConfigLineage{
		SchemaVersion:   ConfigLineageSchemaVersion,
		StrategyRef:     "ielts-cumulative-profile/v1",
		PipelineVersion: "ielts-cumulative-profile/v1",
		PromptVersion:   "ielts-cumulative-profile/v1",
		ResultSchema:    IELTSCumulativeProfileSchemaVersion,
		Provider:        "qianwen", Model: "qwen-plus",
	}
	builder, err := NewIELTSProfileCommandBuilder(lineage, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(practice.IELTSPartProfileEvidence{
		Stage: "PART_3",
	}); err == nil {
		t.Fatal("Build accepted an unsupported Part 3 profile")
	}
	if _, err := builder.Build(practice.IELTSPartProfileEvidence{
		Stage: practice.IELTSProfileStagePart1,
	}); err == nil {
		t.Fatal("Build accepted incomplete Part 1 evidence")
	}
	var nilBuilder *IELTSProfileCommandBuilder
	if _, err := nilBuilder.Build(practice.IELTSPartProfileEvidence{}); err == nil {
		t.Fatal("nil builder accepted evidence")
	}
}

func TestIELTSProfileEvidenceValidation(t *testing.T) {
	evidence := IELTSProfileEvidence{
		TurnID: "30000000-0000-4000-8000-000000000001",
		Quote:  "exact evidence", Occurrence: 1, Part: 1,
	}
	if !evidence.Valid() {
		t.Fatal("valid IELTS profile evidence was rejected")
	}
	observation := IELTSProfileObservation{
		Kind: "STRENGTH", ReasonCode: "CLEAR_LINKING",
		Evidence: []IELTSProfileEvidence{evidence},
	}
	if !observation.Valid() {
		t.Fatal("valid IELTS profile observation was rejected")
	}
	observation.Kind = "GUESS"
	if observation.Valid() {
		t.Fatal("invalid IELTS profile observation was accepted")
	}
	evidence.Part = 3
	if evidence.Valid() {
		t.Fatal("Part 3 evidence was accepted in a Part 1/2 profile")
	}
}

func TestSessionCommandBuilderKeepsStandaloneIELTSOnV2(t *testing.T) {
	lineage := func(strategy string, prompt string) ConfigLineage {
		return ConfigLineage{
			SchemaVersion: ConfigLineageSchemaVersion,
			StrategyRef:   strategy, PipelineVersion: "session-evaluation/v1",
			PromptVersion: prompt, ResultSchema: "report/v1",
			Provider: "qianwen", Model: "qwen-plus",
		}
	}
	builder, err := NewSessionCommandBuilder(SessionLineages{
		Interview:     lineage(InterviewStrategyRef, "interview-report/v2"),
		IELTSPractice: lineage(IELTSStrategyRef, "ielts-report/v2"),
		IELTS:         lineage(IELTSStrategyRef, "ielts-report/v3"),
		General:       lineage(GeneralStrategyRef, "general-report/v2"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	standalone, err := builder.lineageFor(IELTSSpeakingPracticeEvaluationPolicyRef)
	if err != nil || standalone.PromptVersion != "ielts-report/v2" {
		t.Fatalf("standalone IELTS lineage = %#v, %v", standalone, err)
	}
	fullMock, err := builder.lineageFor(IELTSSpeakingFullMockEvaluationPolicyRef)
	if err != nil || fullMock.PromptVersion != "ielts-report/v3" {
		t.Fatalf("full mock IELTS lineage = %#v, %v", fullMock, err)
	}
}
