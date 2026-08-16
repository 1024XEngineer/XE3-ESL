package evaluation

import (
	"context"
	"strings"
	"time"
)

type agentMessageQueue interface {
	Queue(context.Context, QueueCommand) (Record, bool, error)
}

type AgentMessageEvidence struct {
	UserID      string
	ThreadID    string
	MessageID   string
	Transcript  string
	ConfirmedAt time.Time
}

type AgentMessageFeedbackScheduler struct {
	queue   agentMessageQueue
	lineage ConfigLineage
}

func NewAgentMessageFeedbackScheduler(
	queue agentMessageQueue,
	lineage ConfigLineage,
) (*AgentMessageFeedbackScheduler, error) {
	if queue == nil || !lineage.Valid() {
		return nil, ErrInvalidRequest
	}
	return &AgentMessageFeedbackScheduler{queue: queue, lineage: lineage}, nil
}

func (scheduler *AgentMessageFeedbackScheduler) Schedule(
	ctx context.Context,
	evidence AgentMessageEvidence,
) (Record, bool, error) {
	if scheduler == nil || scheduler.queue == nil || ctx == nil ||
		!validUUID(evidence.UserID) || !validUUID(evidence.ThreadID) ||
		!validUUID(evidence.MessageID) ||
		strings.TrimSpace(evidence.Transcript) == "" ||
		len(evidence.Transcript) > 16*1024 || evidence.ConfirmedAt.IsZero() {
		return Record{}, false, ErrInvalidRequest
	}
	snapshot := SpeechInputSnapshot{
		SchemaVersion: SpeechInputSchemaVersion,
		Transcript:    evidence.Transcript,
		EvidenceRefID: evidence.MessageID,
		Acoustic: &AcousticCheckpoint{
			Status: AcousticNotAssessed,
			Reason: "AGENT_MESSAGE_ACOUSTICS_NOT_ASSESSED",
		},
	}
	inputJSON, inputHash, err := EncodeStrict(snapshot)
	if err != nil {
		return Record{}, false, err
	}
	configJSON, configHash, err := EncodeStrict(scheduler.lineage)
	if err != nil {
		return Record{}, false, err
	}
	return scheduler.queue.Queue(ctx, QueueCommand{
		UserID:        evidence.UserID,
		Kind:          KindAgentMessageFeedback,
		SourceID:      evidence.MessageID,
		ContextID:     evidence.ThreadID,
		InputSnapshot: inputJSON,
		InputHash:     inputHash,
		ConfigLineage: configJSON,
		ConfigHash:    configHash,
		AvailableAt:   evidence.ConfirmedAt.UTC(),
	})
}
