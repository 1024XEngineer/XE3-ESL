package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxRealtimePCMBytes = platformmedia.MaxAudioBytes - 44

type TranscribeVoiceStreamCommand struct {
	SessionID      string
	QuestionID     string
	IdempotencyKey string
	PCM            io.Reader
	SampleRate     int
}

func (service *VoiceRoundService) TranscribeStream(
	ctx context.Context,
	actor requestcontext.Actor,
	respondentParticipantID string,
	command TranscribeVoiceStreamCommand,
	observer TranscriptionObserver,
) (candidate TranscriptionCandidate, returnErr error) {
	if err := validateVoiceContext(ctx, actor); err != nil ||
		strings.TrimSpace(respondentParticipantID) == "" ||
		strings.TrimSpace(command.SessionID) == "" ||
		strings.TrimSpace(command.QuestionID) == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" ||
		command.PCM == nil || command.SampleRate != 16_000 || observer == nil {
		return TranscriptionCandidate{}, ErrVoiceRoundInvalid
	}
	question, err := service.store.GetVoiceQuestion(
		ctx,
		actor,
		command.SessionID,
		command.QuestionID,
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	if !validVoiceQuestion(
		question,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	) {
		return TranscriptionCandidate{}, ErrVoiceRoundNotFound
	}

	// A live request must acquire its idempotency lease before its final audio
	// hash exists. Bind the key to the immutable PCM/session scope; the key
	// itself identifies this recording attempt.
	reservation, err := service.store.ReserveTranscription(
		ctx,
		actor,
		ReserveTranscriptionCommand{
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: respondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint: realtimeVoiceInputFingerprint(
				command.SessionID,
				command.QuestionID,
				command.SampleRate,
			),
		},
	)
	if err != nil {
		return TranscriptionCandidate{}, err
	}
	switch reservation.Status {
	case TranscriptionCompleted:
		if !validTranscriptionCandidate(
			reservation.Candidate,
			command.SessionID,
			command.QuestionID,
			respondentParticipantID,
		) {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
		if err := drainRealtimePCM(command.PCM, command.SampleRate); err != nil {
			return TranscriptionCandidate{}, err
		}
		return reservation.Candidate, nil
	case TranscriptionProcessing:
		if strings.TrimSpace(reservation.ID) == "" ||
			reservation.LeaseToken != "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
		if err := drainRealtimePCM(command.PCM, command.SampleRate); err != nil {
			return TranscriptionCandidate{}, err
		}
		return TranscriptionCandidate{}, ErrVoiceRoundProcessing
	case TranscriptionReserved:
		if strings.TrimSpace(reservation.ID) == "" ||
			strings.TrimSpace(reservation.LeaseToken) == "" {
			return TranscriptionCandidate{}, ErrVoiceRoundConflict
		}
	default:
		return TranscriptionCandidate{}, ErrVoiceRoundConflict
	}

	streamingRecognizer, ok := service.recognizer.(StreamingSpeechRecognizer)
	if !ok {
		err = NewProviderError(
			ProviderOperationTranscription,
			ProviderErrorConfiguration,
			"",
			errors.New("practice voice: streaming recognizer is required"),
		)
		if saveErr := service.failTranscription(
			ctx,
			actor,
			reservation,
			err,
			service.now(),
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, err
	}

	startedAt := service.now()
	var pcm bytes.Buffer
	result, err := streamingRecognizer.TranscribeStream(
		ctx,
		StreamingTranscriptionRequest{
			PCM: io.TeeReader(
				io.LimitReader(command.PCM, maxRealtimePCMBytes+1),
				&pcm,
			),
			SampleRate: command.SampleRate,
		},
		observer,
	)
	defer clear(pcm.Bytes())
	if err != nil {
		if saveErr := service.failTranscription(
			ctx,
			actor,
			reservation,
			err,
			startedAt,
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, err
	}

	metadata, source, err := service.captureRealtimePCM(
		ctx,
		actor,
		pcm.Bytes(),
		command.SampleRate,
	)
	if err != nil {
		invalidAudio := NewProviderError(
			ProviderOperationTranscription,
			ProviderErrorInvalidRequest,
			result.ID,
			err,
		)
		if saveErr := service.failTranscription(
			ctx,
			actor,
			reservation,
			invalidAudio,
			startedAt,
		); saveErr != nil {
			return TranscriptionCandidate{}, saveErr
		}
		return TranscriptionCandidate{}, err
	}
	defer func() {
		if cleanupErr := service.vault.Delete(actor, metadata.ID); cleanupErr != nil {
			candidate = TranscriptionCandidate{}
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()
	if service.recordings != nil {
		if _, err := service.stageRecording(
			ctx,
			actor,
			reservation.ID,
			source,
		); err != nil {
			return TranscriptionCandidate{}, err
		}
	}
	return service.completeTranscription(
		ctx,
		actor,
		reservation,
		result,
		startedAt,
		command.SessionID,
		command.QuestionID,
		respondentParticipantID,
	)
}

func realtimeVoiceInputFingerprint(
	sessionID string,
	questionID string,
	sampleRate int,
) string {
	hash := sha256.New()
	_, _ = io.WriteString(
		hash,
		"conversation.voice-stream/v1\x00"+
			sessionID+"\x00"+questionID+"\x00pcm-s16le-mono\x00"+
			strconv.Itoa(sampleRate),
	)
	return hex.EncodeToString(hash.Sum(nil))
}

func drainRealtimePCM(input io.Reader, sampleRate int) error {
	pcm, err := readRealtimePCM(input, sampleRate)
	clear(pcm)
	return err
}

func readRealtimePCM(input io.Reader, sampleRate int) ([]byte, error) {
	if input == nil || sampleRate != 16_000 {
		return nil, ErrVoiceRoundInvalid
	}
	pcm, err := io.ReadAll(io.LimitReader(input, maxRealtimePCMBytes+1))
	if err != nil {
		clear(pcm)
		return nil, err
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		clear(pcm)
		return nil, ErrVoiceRoundInvalid
	}
	if int64(len(pcm)) > maxRealtimePCMBytes {
		clear(pcm)
		return nil, ErrVoiceRoundCapacity
	}
	return pcm, nil
}

func (service *VoiceRoundService) captureRealtimePCM(
	ctx context.Context,
	actor requestcontext.Actor,
	pcm []byte,
	sampleRate int,
) (
	platformmedia.TemporaryAudioMetadata,
	platformmedia.AudioSource,
	error,
) {
	wav, err := pcm16MonoWAV(pcm, sampleRate)
	if err != nil {
		return platformmedia.TemporaryAudioMetadata{}, nil, err
	}
	metadata, err := service.vault.Capture(
		ctx,
		actor,
		platformmedia.ContentTypeWAV,
		bytes.NewReader(wav),
	)
	clear(wav)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return platformmedia.TemporaryAudioMetadata{}, nil, contextErr
		}
		if errors.Is(err, platformmedia.ErrTemporaryAudioCapacity) {
			return platformmedia.TemporaryAudioMetadata{}, nil,
				ErrVoiceRoundCapacity
		}
		return platformmedia.TemporaryAudioMetadata{}, nil,
			ErrVoiceRoundInvalid
	}
	source, err := service.vault.Source(actor, metadata.ID)
	if err != nil {
		_ = service.vault.Delete(actor, metadata.ID)
		return platformmedia.TemporaryAudioMetadata{}, nil,
			ErrVoiceRoundInvalid
	}
	return metadata, source, nil
}

func pcm16MonoWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 ||
		int64(len(pcm)) > maxRealtimePCMBytes || sampleRate != 16_000 {
		return nil, ErrVoiceRoundInvalid
	}
	result := make([]byte, 44+len(pcm))
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(result[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(len(pcm)))
	copy(result[44:], pcm)
	return result, nil
}
