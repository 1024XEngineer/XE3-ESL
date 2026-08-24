package evaluation

import (
	"slices"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

const (
	InterviewEvaluationPolicyRef             = "interview.shadow.evaluation.v1"
	IELTSSpeakingPracticeEvaluationPolicyRef = "ielts.speaking_practice.evaluation.v1"
	IELTSSpeakingFullMockEvaluationPolicyRef = "ielts.speaking_full_mock.evaluation.v1"
	WorkplaceEvaluationPolicyRef             = "workplace.general.evaluation.v1"
	DailyEvaluationPolicyRef                 = "daily.general.evaluation.v1"

	InterviewStrategyRef = "interview-scene-shadow/v1"
	IELTSStrategyRef     = "ielts-speaking-full-mock-shadow/v1"
	GeneralStrategyRef   = "general-scene-evaluation/v1"
)

type SessionLineages struct {
	Interview     ConfigLineage
	IELTSPractice ConfigLineage
	IELTS         ConfigLineage
	General       ConfigLineage
}

func (lineages SessionLineages) Valid() bool {
	return lineages.Interview.Valid() &&
		lineages.Interview.StrategyRef == InterviewStrategyRef &&
		lineages.IELTSPractice.Valid() &&
		lineages.IELTSPractice.StrategyRef == IELTSStrategyRef &&
		lineages.IELTS.Valid() && lineages.IELTS.StrategyRef == IELTSStrategyRef &&
		lineages.General.Valid() && lineages.General.StrategyRef == GeneralStrategyRef
}

type SessionCommandBuilder struct {
	lineages         SessionLineages
	acousticsEnabled bool
}

func NewSessionCommandBuilder(
	lineages SessionLineages,
	acousticsEnabled bool,
) (*SessionCommandBuilder, error) {
	if !lineages.Valid() {
		return nil, ErrInvalidRequest
	}
	return &SessionCommandBuilder{
		lineages: lineages, acousticsEnabled: acousticsEnabled,
	}, nil
}

func (builder *SessionCommandBuilder) Build(
	source practice.SessionEvidence,
) (QueueCommand, error) {
	if builder == nil {
		return QueueCommand{}, ErrInvalidRequest
	}
	lineage, err := builder.lineageFor(source.EvaluationPolicyRef)
	if err != nil {
		return QueueCommand{}, err
	}
	snapshot := SessionInputSnapshot{
		SchemaVersion:       SessionInputSchemaVersion,
		SessionID:           source.SessionID,
		SessionVersion:      source.Version,
		EvaluationPolicyRef: source.EvaluationPolicyRef,
		PracticeExperience:  source.PracticeExperience,
		SceneCategory:       source.SceneCategory,
		PracticeMode:        source.PracticeMode,
		CompletedAt:         source.CompletedAt.UTC(),
		AcousticCapability:  AcousticCapabilityEnabled,
		PlanSnapshot:        slices.Clone(source.PlanSnapshot),
		Participants:        slices.Clone(source.Participants),
		Questions:           make([]SessionEvidenceQuestion, len(source.Questions)),
		Turns:               make([]SessionEvidenceTurn, 0, len(source.Turns)),
	}
	if !builder.acousticsEnabled {
		snapshot.AcousticCapability = AcousticCapabilityNotConfigured
	}
	for index, question := range source.Questions {
		snapshot.Questions[index] = SessionEvidenceQuestion{
			ID:                      question.ID,
			Position:                question.Position,
			ParentQuestionID:        question.ParentQuestionID,
			Text:                    question.Text,
			SpeakerParticipantID:    question.SpeakerParticipantID,
			AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
		}
	}
	for _, turn := range source.Turns {
		snapshot.Turns = append(snapshot.Turns, SessionEvidenceTurn{
			ID:                      turn.ID,
			Position:                turn.Position,
			QuestionID:              turn.QuestionID,
			RespondentParticipantID: turn.RespondentParticipantID,
			Transcript:              turn.Transcript,
			Effective:               turn.Effective,
			ConfirmedAt:             turn.ConfirmedAt.UTC(),
			AudioAssetID:            turn.AudioAssetID,
		})
	}
	if source.UserID == "" || !snapshot.Valid() {
		return QueueCommand{}, ErrInvalidRequest
	}
	inputJSON, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		return QueueCommand{}, err
	}
	configJSON, configHash, err := EncodeStrict(lineage)
	if err != nil {
		return QueueCommand{}, err
	}
	return QueueCommand{
		UserID:        source.UserID,
		Kind:          KindSessionReport,
		SourceID:      source.SessionID,
		ContextID:     source.SessionID,
		InputSnapshot: inputJSON,
		InputHash:     inputHash,
		ConfigLineage: configJSON,
		ConfigHash:    configHash,
		AvailableAt:   source.CompletedAt.UTC(),
	}, nil
}

func (builder *SessionCommandBuilder) lineageFor(
	policyRef string,
) (ConfigLineage, error) {
	switch policyRef {
	case InterviewEvaluationPolicyRef:
		return builder.lineages.Interview, nil
	case IELTSSpeakingPracticeEvaluationPolicyRef:
		return builder.lineages.IELTSPractice, nil
	case IELTSSpeakingFullMockEvaluationPolicyRef:
		return builder.lineages.IELTS, nil
	case WorkplaceEvaluationPolicyRef, DailyEvaluationPolicyRef:
		return builder.lineages.General, nil
	default:
		return ConfigLineage{}, ErrInvalidRequest
	}
}

type TurnFeedbackCommandBuilder struct {
	lineage          ConfigLineage
	acousticsEnabled bool
}

func NewTurnFeedbackCommandBuilder(
	lineage ConfigLineage,
	acousticsEnabled bool,
) (*TurnFeedbackCommandBuilder, error) {
	if !lineage.Valid() {
		return nil, ErrInvalidRequest
	}
	return &TurnFeedbackCommandBuilder{
		lineage:          lineage,
		acousticsEnabled: acousticsEnabled,
	}, nil
}

func (builder *TurnFeedbackCommandBuilder) Build(
	source practice.TurnFeedbackEvidence,
) (QueueCommand, error) {
	if builder == nil ||
		!validUUID(source.UserID) || !validIdentifier(source.SessionID) ||
		!validIdentifier(source.TurnID) || !validIdentifier(source.QuestionID) ||
		strings.TrimSpace(source.QuestionText) == "" ||
		strings.TrimSpace(source.Transcript) == "" ||
		!validIdentifier(source.RespondentParticipantID) ||
		(source.AudioAssetID != "" && !validIdentifier(source.AudioAssetID)) ||
		source.ConfirmedAt.IsZero() || !validVersion(source.EvaluationPolicyRef) ||
		!validIdentifier(source.PracticeExperience) ||
		!validIdentifier(source.SceneCategory) || !validIdentifier(source.PracticeMode) {
		return QueueCommand{}, ErrInvalidRequest
	}
	snapshot := SpeechInputSnapshot{
		SchemaVersion: SpeechInputSchemaVersion,
		Transcript:    source.Transcript,
		EvidenceRefID: source.TurnID,
		QuestionID:    source.QuestionID,
		PromptText:    source.QuestionText,
		AudioAssetID:  source.AudioAssetID,
	}
	if !builder.acousticsEnabled {
		snapshot.Acoustic = &AcousticCheckpoint{
			Status: AcousticNotAssessed,
			Reason: "ACOUSTIC_ASSESSMENT_NOT_CONFIGURED",
		}
	} else if source.AudioAssetID == "" {
		snapshot.Acoustic = &AcousticCheckpoint{
			Status: AcousticNotAssessed,
			Reason: "PRACTICE_TURN_AUDIO_UNAVAILABLE",
		}
	}
	if !snapshot.Valid(KindPracticeTurnFeedback) {
		return QueueCommand{}, ErrInvalidRequest
	}
	inputJSON, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		return QueueCommand{}, err
	}
	configJSON, configHash, err := EncodeStrict(builder.lineage)
	if err != nil {
		return QueueCommand{}, err
	}
	return QueueCommand{
		UserID:        source.UserID,
		Kind:          KindPracticeTurnFeedback,
		SourceID:      source.TurnID,
		ContextID:     source.SessionID,
		InputSnapshot: inputJSON,
		InputHash:     inputHash,
		ConfigLineage: configJSON,
		ConfigHash:    configHash,
		AvailableAt:   source.ConfirmedAt.UTC(),
	}, nil
}
