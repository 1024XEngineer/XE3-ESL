package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// VoiceRecordingLifecycle is Conversation's optional durable-recording
// boundary. The ASR/TTS provider contracts remain unaware of object storage.
type VoiceRecordingLifecycle interface {
	Upload(
		context.Context,
		AudioAssetActor,
		UploadRecordingRequest,
	) (AudioAsset, error)
	GetReadableByTurn(
		context.Context,
		AudioAssetActor,
		string,
	) (AudioAsset, error)
}

func (service *VoiceRoundService) stageRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	source platformmedia.AudioSource,
) (AudioAsset, error) {
	if service.recordings == nil ||
		reservationID == "" ||
		platformmedia.ValidateAudioSource(source) != nil {
		return AudioAsset{}, ErrVoiceRoundInvalid
	}
	reader, err := source.Open()
	if err != nil {
		return AudioAsset{}, ErrVoiceRoundInvalid
	}
	audio, readErr := io.ReadAll(io.LimitReader(
		reader,
		platformmedia.MaxAudioBytes+1,
	))
	closeErr := reader.Close()
	defer clear(audio)
	if readErr != nil ||
		closeErr != nil ||
		int64(len(audio)) != source.Size() ||
		int64(len(audio)) > platformmedia.MaxAudioBytes {
		return AudioAsset{}, ErrVoiceRoundInvalid
	}
	checksum := sha256.Sum256(audio)
	return service.recordings.Upload(
		ctx,
		AudioAssetActor{UserID: actor.UserID},
		UploadRecordingRequest{
			RequestID:      reservationID,
			Body:           bytes.NewReader(audio),
			Size:           int64(len(audio)),
			ContentType:    source.MediaType(),
			ChecksumSHA256: hex.EncodeToString(checksum[:]),
			Duration:       source.Duration(),
		},
	)
}

func (service *VoiceRoundService) withReadableRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate TranscriptionCandidate,
	turn practice.Turn,
) (practice.Turn, error) {
	if service.recordings == nil {
		return turn, nil
	}
	if candidate.ReservationID == "" ||
		candidate.ID == "" ||
		turn.ID == "" ||
		turn.CandidateID != candidate.ID {
		return practice.Turn{}, ErrVoiceRoundInvalid
	}
	asset, err := service.recordings.GetReadableByTurn(
		ctx,
		AudioAssetActor{UserID: actor.UserID},
		turn.ID,
	)
	if errors.Is(err, ErrAudioAssetNotFound) ||
		errors.Is(err, ErrAudioAssetInvalidTransition) {
		return turn, nil
	}
	if err != nil {
		return practice.Turn{}, err
	}
	if asset.ID == "" ||
		asset.OwnerID != actor.UserID ||
		asset.CandidateID != candidate.ID ||
		asset.TurnID != turn.ID ||
		asset.Status != AudioAssetReadable {
		return practice.Turn{}, ErrVoiceRoundInvalid
	}
	turn.AudioAssetID = asset.ID
	return turn, nil
}
