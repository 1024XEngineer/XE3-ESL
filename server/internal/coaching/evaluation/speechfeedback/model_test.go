package speechfeedback

import (
	"strings"
	"testing"
	"time"
)

func TestSpeechFeedbackAnchorUsesExactUTF8ByteOffsets(t *testing.T) {
	t.Parallel()

	source := SpeechFeedbackSource{
		SourceKind:         SpeechFeedbackSourceConversationTurn,
		PracticeSessionID:  "practice-1",
		TurnID:             "turn-1",
		InputRevision:      1,
		EvidenceSnapshotID: "evaluation_snapshot_1",
	}
	text := "我 said hello."
	start := len("我 said ")
	anchor := SpeechFeedbackAnchor{
		AnchorKind:      SpeechFeedbackAnchorConversationTranscript,
		EvidenceRefID:   "evaluation_evidence_1",
		TurnID:          source.TurnID,
		StartUTF8Byte:   start,
		EndUTF8Byte:     start + len("hello"),
		OriginalExcerpt: "hello",
	}
	if !anchor.validFor(source, "evaluation_evidence_1", text) {
		t.Fatal("exact UTF-8 byte anchor was rejected")
	}

	insideRune := anchor
	insideRune.StartUTF8Byte = 1
	insideRune.OriginalExcerpt = text[1:insideRune.EndUTF8Byte]
	if insideRune.validFor(source, "evaluation_evidence_1", text) {
		t.Fatal("anchor starting inside a UTF-8 rune was accepted")
	}

	wrongExcerpt := anchor
	wrongExcerpt.OriginalExcerpt = "Hello"
	if wrongExcerpt.validFor(source, "evaluation_evidence_1", text) {
		t.Fatal("non-exact excerpt was accepted")
	}

	if anchor.validFor(source, "other_review_evidence", text) {
		t.Fatal("anchor for a different frozen evidence snapshot was accepted")
	}
}

func TestSpeechFeedbackSourceUnionRejectsCrossModuleIdentity(t *testing.T) {
	t.Parallel()

	conversation := SpeechFeedbackSource{
		SourceKind:         SpeechFeedbackSourceConversationTurn,
		PracticeSessionID:  "practice-1",
		TurnID:             "turn-1",
		InputRevision:      1,
		EvidenceSnapshotID: "evaluation_snapshot_1",
	}
	if !conversation.valid() {
		t.Fatal("valid Conversation Turn source was rejected")
	}
	conversation.MessageID = "47d04075-2a5f-45b6-a580-6327717ce16a"
	if conversation.valid() {
		t.Fatal("Conversation source accepted Agent Message identity")
	}

	agent := SpeechFeedbackSource{
		SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
		ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
		MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
		TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
		CandidateVersion:     1,
	}
	if !agent.valid() {
		t.Fatal("valid Agent Voice Message source was rejected")
	}
	agent.PracticeSessionID = "practice-1"
	if agent.valid() {
		t.Fatal("Agent source accepted fake Practice Session identity")
	}
}

func TestSpeechFeedbackStateShapesKeepInsufficientAndFailedSeparate(
	t *testing.T,
) {
	t.Parallel()

	now := time.Now().UTC()
	completed := now.Add(time.Second)
	source := SpeechFeedbackSource{
		SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
		ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
		MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
		TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
		CandidateVersion:     1,
	}
	scoreability := SpeechFeedbackInsufficient
	gate := SpeechFeedbackBlocked
	feedback := SpeechFeedback{
		SpeechFeedbackID:   "729cdce7-4d33-418c-8497-d2932c651003",
		Source:             source,
		FeedbackStatus:     SpeechFeedbackReady,
		ScoreabilityStatus: &scoreability,
		GateStatus:         &gate,
		ReasonCodes: []SpeechFeedbackReasonCode{
			SpeechFeedbackReasonTextTooShort,
		},
		SchemaVersion:      SpeechFeedbackSchemaVersion,
		StrategyRef:        SpeechFeedbackStrategyRef,
		PipelineVersion:    SpeechFeedbackPipelineVersion,
		Items:              []SpeechFeedbackItem{},
		AcousticAssessment: unavailableSpeechFeedbackAcoustics(),
		StatusURL: SpeechFeedbackStatusURL(
			"729cdce7-4d33-418c-8497-d2932c651003",
		),
		CreatedAt:   now,
		UpdatedAt:   completed,
		CompletedAt: &completed,
	}
	if !feedback.valid("", "Hi") {
		t.Fatal("valid insufficient feedback was rejected")
	}

	feedback.FeedbackStatus = SpeechFeedbackFailed
	feedback.ScoreabilityStatus = nil
	feedback.GateStatus = nil
	feedback.ReasonCodes = nil
	feedback.StableFailure = &SpeechFeedbackStableFailure{
		ReasonCode: SpeechFeedbackFailureProviderUnavailable,
		Retryable:  true,
	}
	if !feedback.valid("", "Hi") {
		t.Fatal("valid technical failure was rejected")
	}

	feedback.ReasonCodes = []SpeechFeedbackReasonCode{
		SpeechFeedbackReasonEvidenceInconsistent,
	}
	if feedback.valid("", "Hi") {
		t.Fatal("FAILED feedback accepted performance reason codes")
	}
}

func TestSpeechFeedbackItemsEnforceSourceSpecificRepractice(t *testing.T) {
	t.Parallel()

	source := SpeechFeedbackSource{
		SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
		ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
		MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
		TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
		CandidateVersion:     1,
	}
	suggestion := "I have worked on this project."
	item := SpeechFeedbackItem{
		FeedbackItemID:   "00ae54b5-3d8a-4ca0-bc0c-49beadf14117",
		SpeechFeedbackID: "729cdce7-4d33-418c-8497-d2932c651003",
		Kind:             SpeechFeedbackItemCorrection,
		Anchor: SpeechFeedbackAnchor{
			AnchorKind:           SpeechFeedbackAnchorAgentTranscript,
			TranscriptEvidenceID: source.TranscriptEvidenceID,
			MessageID:            source.MessageID,
			StartUTF8Byte:        0,
			EndUTF8Byte:          len("I work this project."),
			OriginalExcerpt:      "I work this project.",
		},
		Explanation:    "Use the present perfect for relevant experience.",
		SuggestedText:  &suggestion,
		RepracticeMode: SpeechFeedbackRepracticeSameThread,
		CreatedAt:      time.Now().UTC(),
	}
	if !item.validFor(source, "", "I work this project.") {
		t.Fatal("valid Agent correction was rejected")
	}
	item.RepracticeMode = SpeechFeedbackRepracticeSameQuestion
	if item.validFor(source, "", "I work this project.") {
		t.Fatal("Agent correction accepted Conversation repractice mode")
	}

	item.Kind = SpeechFeedbackItemStrength
	item.SuggestedText = nil
	item.RepracticeMode = SpeechFeedbackRepracticeNone
	if !item.validFor(source, "", "I work this project.") {
		t.Fatal("valid strength was rejected")
	}
	empty := ""
	item.SuggestedText = &empty
	if item.validFor(source, "", "I work this project.") {
		t.Fatal("strength accepted an empty suggested_text field")
	}
}

func TestSpeechFeedbackResourceCapsStoredItemsAtEight(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	completed := now.Add(time.Second)
	source := SpeechFeedbackSource{
		SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
		ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
		MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
		TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
		CandidateVersion:     1,
	}
	scoreability := SpeechFeedbackProvisional
	gate := SpeechFeedbackFeedbackOnly
	item := SpeechFeedbackItem{
		FeedbackItemID:   "00ae54b5-3d8a-4ca0-bc0c-49beadf14117",
		SpeechFeedbackID: "729cdce7-4d33-418c-8497-d2932c651003",
		Kind:             SpeechFeedbackItemStrength,
		Anchor: SpeechFeedbackAnchor{
			AnchorKind:           SpeechFeedbackAnchorAgentTranscript,
			TranscriptEvidenceID: source.TranscriptEvidenceID,
			MessageID:            source.MessageID,
			StartUTF8Byte:        0,
			EndUTF8Byte:          2,
			OriginalExcerpt:      "Hi",
		},
		Explanation:    "The answer is direct.",
		RepracticeMode: SpeechFeedbackRepracticeNone,
		CreatedAt:      completed,
	}
	feedback := SpeechFeedback{
		SpeechFeedbackID:   item.SpeechFeedbackID,
		Source:             source,
		FeedbackStatus:     SpeechFeedbackReady,
		ScoreabilityStatus: &scoreability,
		GateStatus:         &gate,
		SchemaVersion:      SpeechFeedbackSchemaVersion,
		StrategyRef:        SpeechFeedbackStrategyRef,
		PipelineVersion:    SpeechFeedbackPipelineVersion,
		Items: make(
			[]SpeechFeedbackItem,
			maxSpeechFeedbackProviderItems+1,
		),
		AcousticAssessment: unavailableSpeechFeedbackAcoustics(),
		StatusURL: SpeechFeedbackStatusURL(
			item.SpeechFeedbackID,
		),
		CreatedAt:   now,
		UpdatedAt:   completed,
		CompletedAt: &completed,
	}
	for index := range feedback.Items {
		feedback.Items[index] = item
	}
	if feedback.valid("", "Hi") {
		t.Fatal("stored resource accepted more than eight feedback items")
	}
}

func TestSpeechFeedbackTextBoundsAreUTF8Bytes(t *testing.T) {
	t.Parallel()
	if validSpeechFeedbackText(strings.Repeat("你", 683), 2048) {
		t.Fatal("oversized UTF-8 text was accepted by rune count")
	}
	if !validSpeechFeedbackText(strings.Repeat("你", 682), 2048) {
		t.Fatal("text within UTF-8 byte bound was rejected")
	}
}

func TestSpeechFeedbackScenarioGateUsesExactTypeModelPairs(t *testing.T) {
	t.Parallel()
	for _, eligible := range [][2]string{
		{"DAILY", "HOTEL_CHECKIN_AND_ISSUE_HANDLING"},
		{"DAILY", "DAILY_BASIC_DIALOGUE"},
		{"WORKPLACE", "PROGRESS_AND_RISK_UPDATE"},
		{"WORKPLACE", "WORKPLACE_BASIC_DIALOGUE"},
		{"INTERVIEW", "PROJECT_EXPERIENCE_DEEP_DIVE"},
		{"INTERVIEW", "INTERVIEW_BASIC_DIALOGUE"},
		{"EXAM", "IELTS_SPEAKING_PART_1"},
		{"EXAM", "IELTS_SPEAKING_PART_2"},
		{"EXAM", "IELTS_SPEAKING_PART_3"},
		{"EXAM", "IELTS_SPEAKING_FULL_MOCK"},
		{"EXAM", "EXAM_BASIC_DIALOGUE"},
	} {
		if !eligibleSpeechFeedbackScenario(eligible[0], eligible[1]) {
			t.Fatalf("eligible pair rejected: %#v", eligible)
		}
	}
	for _, hidden := range [][2]string{
		{"DAILY", "WORKPLACE_BASIC_DIALOGUE"},
		{"WORKPLACE", "DAILY_BASIC_DIALOGUE"},
		{"INTERVIEW", "IELTS_SPEAKING_PART_2"},
		{"EXAM", "INTERVIEW_BASIC_DIALOGUE"},
		{"", ""},
	} {
		if eligibleSpeechFeedbackScenario(hidden[0], hidden[1]) {
			t.Fatalf("hidden or mismatched pair accepted: %#v", hidden)
		}
	}
}

func TestSpeechFeedbackTextRejectsForbiddenControls(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"hello\x00world",
		"hello\u007fworld",
		"hello\u0080world",
		"hello\u009fworld",
	} {
		if validSpeechFeedbackText(value, 2048) {
			t.Fatalf("forbidden control accepted: %q", value)
		}
	}
	if !validSpeechFeedbackText("hello\tworld\nagain", 2048) {
		t.Fatal("TAB/LF text was rejected")
	}
}
