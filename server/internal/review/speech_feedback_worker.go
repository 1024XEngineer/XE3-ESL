package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

const maxSpeechFeedbackSweepLimit = 20

type SpeechFeedbackWorkerConfiguration struct {
	MaxAttempts     int
	LeaseDuration   time.Duration
	RetryDelay      time.Duration
	StrategyRef     string
	PipelineVersion string
	PromptVersion   string
	Provider        string
	Model           string
}

func (configuration SpeechFeedbackWorkerConfiguration) Valid() bool {
	return configuration.MaxAttempts >= 1 &&
		configuration.MaxAttempts <= 10 &&
		configuration.LeaseDuration >= time.Second &&
		configuration.LeaseDuration <= 10*time.Minute &&
		configuration.RetryDelay >= time.Second &&
		configuration.RetryDelay <= time.Hour &&
		configuration.StrategyRef == SpeechFeedbackStrategyRef &&
		configuration.PipelineVersion ==
			SpeechFeedbackPipelineVersion &&
		configuration.PromptVersion == SpeechFeedbackPromptVersion &&
		validSpeechFeedbackIdentifier(configuration.Provider) &&
		validSpeechFeedbackIdentifier(configuration.Model)
}

type SpeechFeedbackClaim struct {
	SpeechFeedbackID   string
	OwnerUserID        string
	Source             SpeechFeedbackSource
	CanonicalText      string
	EvidenceRefID      string
	AudioAssetID       string
	AudioAssetVersion  int64
	AudioChecksum      string
	AudioObjectKey     string
	SourceDigest       [sha256.Size]byte
	DeletionGeneration int64
	AttemptCount       int
	FencingToken       int64
	LeaseExpiresAt     time.Time
	StrategyRef        string
	PipelineVersion    string
	SourceConsistent   bool
}

func (claim SpeechFeedbackClaim) Valid() bool {
	return validUUID(claim.SpeechFeedbackID) &&
		validUUID(claim.OwnerUserID) &&
		claim.Source.valid() &&
		validSpeechFeedbackText(claim.CanonicalText, 16*1024) &&
		((claim.Source.SourceKind ==
			SpeechFeedbackSourceConversationTurn &&
			validSpeechFeedbackIdentifier(claim.EvidenceRefID)) ||
			(claim.Source.SourceKind ==
				SpeechFeedbackSourceAgentVoiceMessage &&
				claim.EvidenceRefID == "")) &&
		claim.SourceDigest != [sha256.Size]byte{} &&
		claim.DeletionGeneration >= 0 &&
		claim.AttemptCount >= 1 &&
		claim.FencingToken >= 1 &&
		!claim.LeaseExpiresAt.IsZero() &&
		claim.StrategyRef == SpeechFeedbackStrategyRef &&
		claim.PipelineVersion == SpeechFeedbackPipelineVersion
}

func (claim SpeechFeedbackClaim) hasAcousticSource() bool {
	if !validSpeechFeedbackIdentifier(claim.AudioAssetID) ||
		claim.AudioAssetVersion <= 0 ||
		len(claim.AudioChecksum) != 64 {
		return false
	}
	switch claim.Source.SourceKind {
	case SpeechFeedbackSourceConversationTurn:
		return claim.AudioObjectKey == ""
	case SpeechFeedbackSourceAgentVoiceMessage:
		return strings.HasPrefix(
			claim.AudioObjectKey,
			"audio/v1/agent/",
		)
	default:
		return false
	}
}

type SpeechFeedbackSweepResult struct {
	Claimed      int
	Completed    int
	Insufficient int
	Retried      int
	Failed       int
}

type SpeechFeedbackWorker struct {
	repository         SpeechFeedbackRepository
	provider           SpeechFeedbackProvider
	acousticRepository SpeechFeedbackAcousticRepository
	acousticProvider   SpeechFeedbackAcousticProvider
	configuration      SpeechFeedbackWorkerConfiguration
}

func NewSpeechFeedbackWorkerWithAcoustics(
	repository SpeechFeedbackRepository,
	provider SpeechFeedbackProvider,
	acousticRepository SpeechFeedbackAcousticRepository,
	acousticProvider SpeechFeedbackAcousticProvider,
	configuration SpeechFeedbackWorkerConfiguration,
) (*SpeechFeedbackWorker, error) {
	worker, err := NewSpeechFeedbackWorker(
		repository,
		provider,
		configuration,
	)
	if err != nil {
		return nil, err
	}
	if acousticRepository == nil || acousticProvider == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	worker.acousticRepository = acousticRepository
	worker.acousticProvider = acousticProvider
	return worker, nil
}

func NewSpeechFeedbackWorker(
	repository SpeechFeedbackRepository,
	provider SpeechFeedbackProvider,
	configuration SpeechFeedbackWorkerConfiguration,
) (*SpeechFeedbackWorker, error) {
	if repository == nil || provider == nil || !configuration.Valid() {
		return nil, ErrInvalidSpeechFeedback
	}
	return &SpeechFeedbackWorker{
		repository:    repository,
		provider:      provider,
		configuration: configuration,
	}, nil
}

func (worker *SpeechFeedbackWorker) ProcessPending(
	ctx context.Context,
	limit int,
) (SpeechFeedbackSweepResult, error) {
	if worker == nil || worker.repository == nil ||
		worker.provider == nil || ctx == nil ||
		limit < 1 || limit > maxSpeechFeedbackSweepLimit {
		return SpeechFeedbackSweepResult{}, ErrInvalidSpeechFeedback
	}
	var sweep SpeechFeedbackSweepResult
	for range limit {
		claim, acquired, err := worker.repository.
			ClaimSpeechFeedback(ctx, worker.configuration)
		if err != nil {
			return sweep, err
		}
		if !acquired {
			return sweep, nil
		}
		sweep.Claimed++
		status, insufficient, err := worker.processClaim(ctx, claim)
		if err != nil {
			return sweep, err
		}
		switch status {
		case SpeechFeedbackReady:
			sweep.Completed++
			if insufficient {
				sweep.Insufficient++
			}
		case SpeechFeedbackQueued:
			sweep.Retried++
		case SpeechFeedbackFailed:
			sweep.Failed++
		default:
			return sweep, ErrInvalidSpeechFeedback
		}
	}
	return sweep, nil
}

func (worker *SpeechFeedbackWorker) processClaim(
	ctx context.Context,
	claim SpeechFeedbackClaim,
) (SpeechFeedbackStatus, bool, error) {
	if !claim.Valid() ||
		claim.StrategyRef != worker.configuration.StrategyRef ||
		claim.PipelineVersion !=
			worker.configuration.PipelineVersion ||
		claim.AttemptCount > worker.configuration.MaxAttempts {
		return "", false, ErrInvalidSpeechFeedback
	}
	if !claim.SourceConsistent {
		_, err := worker.repository.
			CompleteSpeechFeedbackInsufficient(
				ctx,
				claim,
				[]SpeechFeedbackReasonCode{
					SpeechFeedbackReasonEvidenceInconsistent,
				},
			)
		return SpeechFeedbackReady, true, err
	}
	if worker.acousticProvider != nil && claim.hasAcousticSource() {
		evidence, acousticErr :=
			worker.acousticProvider.EvaluateSpeechFeedbackAcoustics(
				ctx,
				SpeechFeedbackAcousticInput{
					OwnerUserID:       claim.OwnerUserID,
					AudioAssetID:      claim.AudioAssetID,
					AudioAssetVersion: claim.AudioAssetVersion,
					AudioChecksum:     claim.AudioChecksum,
					AudioObjectKey:    claim.AudioObjectKey,
					ConfirmedText:     claim.CanonicalText,
				},
			)
		if acousticErr == nil {
			if err := worker.acousticRepository.
				SaveSpeechFeedbackAcousticEvidence(
					context.WithoutCancel(ctx),
					claim,
					evidence,
				); err != nil {
				return "", false, err
			}
		}
	}
	if !speechFeedbackTextHasEnoughEvidence(claim.CanonicalText) {
		_, err := worker.repository.
			CompleteSpeechFeedbackInsufficient(
				ctx,
				claim,
				[]SpeechFeedbackReasonCode{
					SpeechFeedbackReasonTextTooShort,
				},
			)
		return SpeechFeedbackReady, true, err
	}

	input := SpeechFeedbackProviderInput{
		SchemaVersion: SpeechFeedbackSchemaVersion,
		PromptVersion: worker.configuration.PromptVersion,
		Source:        claim.Source,
		EvidenceRefID: claim.EvidenceRefID,
		ConfirmedText: claim.CanonicalText,
	}
	generated, err := worker.provider.GenerateSpeechFeedback(ctx, input)
	if err != nil {
		status, failureErr := worker.repository.FailSpeechFeedback(
			context.WithoutCancel(ctx),
			claim,
			classifySpeechFeedbackFailure(err),
			worker.configuration,
		)
		return status, false, failureErr
	}
	if generated.Provider != worker.configuration.Provider ||
		generated.Model != worker.configuration.Model {
		status, failureErr := worker.repository.FailSpeechFeedback(
			context.WithoutCancel(ctx),
			claim,
			SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureProviderResponseInvalid,
				Retryable:  false,
			},
			worker.configuration,
		)
		return status, false, failureErr
	}
	items, err := normalizeSpeechFeedbackProviderResult(input, generated)
	if err != nil {
		status, failureErr := worker.repository.FailSpeechFeedback(
			context.WithoutCancel(ctx),
			claim,
			SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureProviderResponseInvalid,
				Retryable:  false,
			},
			worker.configuration,
		)
		return status, false, failureErr
	}
	if _, err := worker.repository.CompleteSpeechFeedback(
		context.WithoutCancel(ctx),
		claim,
		items,
	); err != nil {
		return "", false, err
	}
	return SpeechFeedbackReady, false, nil
}

func classifySpeechFeedbackFailure(
	cause error,
) SpeechFeedbackStableFailure {
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		return SpeechFeedbackStableFailure{
			ReasonCode: SpeechFeedbackFailureProcessingTimeout,
			Retryable:  true,
		}
	case errors.Is(cause, context.Canceled):
		return SpeechFeedbackStableFailure{
			ReasonCode: SpeechFeedbackFailureProviderUnavailable,
			Retryable:  true,
		}
	case errors.Is(cause, ErrInvalidSpeechFeedback):
		return SpeechFeedbackStableFailure{
			ReasonCode: SpeechFeedbackFailureProviderResponseInvalid,
			Retryable:  false,
		}
	}
	var generationError *ai.GenerationError
	if errors.As(cause, &generationError) {
		switch generationError.Kind {
		case ai.ErrorTimeout:
			return SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureProcessingTimeout,
				Retryable:  true,
			}
		case ai.ErrorRateLimited,
			ai.ErrorQuotaExhausted,
			ai.ErrorProviderUnavailable,
			ai.ErrorCancelled:
			return SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureProviderUnavailable,
				Retryable:  generationError.Retryable(),
			}
		case ai.ErrorInvalidResponse:
			return SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureProviderResponseInvalid,
				Retryable:  false,
			}
		default:
			return SpeechFeedbackStableFailure{
				ReasonCode: SpeechFeedbackFailureInternalProcessing,
				Retryable:  false,
			}
		}
	}
	return SpeechFeedbackStableFailure{
		ReasonCode: SpeechFeedbackFailureInternalProcessing,
		Retryable:  true,
	}
}

func speechFeedbackTextHasEnoughEvidence(text string) bool {
	words := 0
	inWord := false
	for _, character := range strings.TrimSpace(text) {
		isWord := unicode.IsLetter(character) ||
			unicode.IsNumber(character)
		if isWord && !inWord {
			words++
			if words >= 2 {
				return true
			}
		}
		inWord = isWord
	}
	return false
}
