package evaluation

import (
	"slices"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

type IELTSProfileCommandBuilder struct {
	lineage          ConfigLineage
	acousticsEnabled bool
}

func NewIELTSProfileCommandBuilder(
	lineage ConfigLineage,
	acousticsEnabled bool,
) (*IELTSProfileCommandBuilder, error) {
	if !lineage.Valid() || lineage.StrategyRef != "ielts-cumulative-profile/v1" {
		return nil, ErrInvalidRequest
	}
	return &IELTSProfileCommandBuilder{
		lineage: lineage, acousticsEnabled: acousticsEnabled,
	}, nil
}

func (builder *IELTSProfileCommandBuilder) Build(
	source practice.IELTSPartProfileEvidence,
) (QueueCommand, error) {
	if builder == nil {
		return QueueCommand{}, ErrInvalidRequest
	}
	stage := IELTSProfileStage(source.Stage)
	kind := KindIELTSPart1Profile
	if stage == IELTSProfileStagePart2 {
		kind = KindIELTSPart2Profile
	} else if stage != IELTSProfileStagePart1 {
		return QueueCommand{}, ErrInvalidRequest
	}
	snapshot := IELTSProfileInputSnapshot{
		SchemaVersion: IELTSProfileInputSchemaVersion,
		SessionID:     source.SessionID, SessionVersion: source.SessionVersion,
		Stage: stage, CompletedAt: source.CompletedAt.UTC(),
		Part1Boundary: source.Part1Boundary, Part2Boundary: source.Part2Boundary,
		AcousticCapability:   AcousticCapabilityEnabled,
		Questions:            make([]SessionEvidenceQuestion, len(source.Questions)),
		Turns:                make([]SessionEvidenceTurn, 0, len(source.Turns)),
		DependencyResolution: IELTSProfileDependencyPending,
	}
	if !builder.acousticsEnabled {
		snapshot.AcousticCapability = AcousticCapabilityNotConfigured
	}
	for index, question := range source.Questions {
		snapshot.Questions[index] = SessionEvidenceQuestion{
			ID: question.ID, Position: question.Position,
			ParentQuestionID: question.ParentQuestionID, Text: question.Text,
			SpeakerParticipantID:    question.SpeakerParticipantID,
			AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
		}
	}
	for _, turn := range source.Turns {
		snapshot.Turns = append(snapshot.Turns, SessionEvidenceTurn{
			ID: turn.ID, Position: turn.Position, QuestionID: turn.QuestionID,
			RespondentParticipantID: turn.RespondentParticipantID,
			Transcript:              turn.Transcript, Effective: true,
			ConfirmedAt: turn.ConfirmedAt.UTC(), AudioAssetID: turn.AudioAssetID,
		})
	}
	if !snapshot.Valid() || source.UserID == "" {
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
		UserID: source.UserID, Kind: kind,
		SourceID: source.SessionID, ContextID: source.SessionID,
		InputSnapshot: inputJSON, InputHash: inputHash,
		ConfigLineage: configJSON, ConfigHash: configHash,
		AvailableAt: source.CompletedAt.UTC(),
	}, nil
}
