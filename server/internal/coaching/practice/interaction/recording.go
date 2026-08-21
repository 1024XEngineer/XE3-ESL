package interaction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

// RecordingReferenceStore is Practice Interaction's narrow PostgreSQL boundary for
// the relationship owned by practice_turns. Media owns only object metadata.
type RecordingReferenceStore interface {
	VerifyOwnedRecording(
		context.Context,
		requestcontext.Actor,
		string,
	) error
	DetachRecording(
		context.Context,
		requestcontext.Actor,
		string,
	) error
}

// RecordingUploader is the only recording capability needed by the ASR flow.
// Keeping playback and deletion out of this port avoids exposing HTTP concerns
// to the round service.
type RecordingUploader interface {
	Upload(
		context.Context,
		requestcontext.Actor,
		string,
		platformmedia.AudioSource,
	) (string, error)
}

// RecordingService adapts Practice Interaction to the shared media lifecycle while
// keeping the Practice-owned turn relationship in its own repository.
type RecordingService struct {
	media      *sharedmedia.Service
	references RecordingReferenceStore
	stagedTTL  time.Duration
}

func NewRecordingService(
	mediaService *sharedmedia.Service,
	references RecordingReferenceStore,
	stagedTTL time.Duration,
) (*RecordingService, error) {
	if mediaService == nil || references == nil ||
		stagedTTL <= 0 || stagedTTL > 7*24*time.Hour {
		return nil, errors.New("practice interaction: recording dependencies are required")
	}
	return &RecordingService{
		media: mediaService, references: references, stagedTTL: stagedTTL,
	}, nil
}

func (service *RecordingService) Upload(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	source platformmedia.AudioSource,
) (string, error) {
	if service == nil || ctx == nil || !actor.Valid() ||
		!sharedmedia.ValidUUID(actor.UserID) ||
		!sharedmedia.ValidIdempotencyKey(reservationID) ||
		platformmedia.ValidateAudioSource(source) != nil {
		return "", ErrVoiceRoundInvalid
	}
	reader, err := source.Open()
	if err != nil {
		return "", ErrVoiceRoundInvalid
	}
	audio, readErr := io.ReadAll(io.LimitReader(
		reader,
		platformmedia.MaxAudioBytes+1,
	))
	closeErr := reader.Close()
	defer clear(audio)
	if readErr != nil || closeErr != nil ||
		int64(len(audio)) != source.Size() ||
		int64(len(audio)) > platformmedia.MaxAudioBytes {
		return "", ErrVoiceRoundInvalid
	}
	checksum := sha256.Sum256(audio)
	asset, err := service.media.Upload(ctx, sharedmedia.Upload{
		UserID:         actor.UserID,
		Kind:           sharedmedia.KindAudio,
		IdempotencyKey: reservationID,
		ContentType:    platformmedia.ContentTypeWAV,
		Body:           bytes.NewReader(audio),
		Size:           int64(len(audio)),
		ChecksumSHA256: hex.EncodeToString(checksum[:]),
		Duration:       source.Duration(),
		SampleRate:     source.SampleRate(),
		ExpiresAt:      time.Now().UTC().Add(service.stagedTTL),
	})
	if err != nil {
		switch {
		case errors.Is(err, sharedmedia.ErrInvalidRequest):
			return "", ErrVoiceRoundInvalid
		case errors.Is(err, sharedmedia.ErrIdempotencyConflict):
			return "", ErrVoiceRoundConflict
		case errors.Is(err, sharedmedia.ErrConflict):
			return "", ErrVoiceRoundProcessing
		default:
			return "", err
		}
	}
	if asset.Kind != sharedmedia.KindAudio || asset.UserID != actor.UserID {
		return "", ErrVoiceRoundConflict
	}
	if asset.Status == sharedmedia.StatusStaged {
		return "", ErrVoiceRoundProcessing
	}
	if asset.Status != sharedmedia.StatusReady || asset.ETag == "" {
		return "", ErrVoiceRoundConflict
	}
	return asset.ID, nil
}

func (service *RecordingService) Playback(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) (objectstore.SignedGetResult, error) {
	if service == nil || ctx == nil || !actor.Valid() ||
		!sharedmedia.ValidUUID(actor.UserID) ||
		!sharedmedia.ValidUUID(assetID) {
		return objectstore.SignedGetResult{}, sharedmedia.ErrNotFound
	}
	if err := service.references.VerifyOwnedRecording(ctx, actor, assetID); err != nil {
		return objectstore.SignedGetResult{}, err
	}
	return service.media.SignedGet(ctx, actor.UserID, assetID)
}

// Delete detaches the optional recording from its confirmed Turn and schedules
// shared object cleanup. The confirmed transcript and Turn remain authoritative.
func (service *RecordingService) Delete(
	ctx context.Context,
	actor requestcontext.Actor,
	assetID string,
) error {
	if service == nil || ctx == nil || !actor.Valid() ||
		!sharedmedia.ValidUUID(actor.UserID) ||
		!sharedmedia.ValidUUID(assetID) {
		return sharedmedia.ErrNotFound
	}
	return service.references.DetachRecording(ctx, actor, assetID)
}

func (service *RoundService) stageRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	source platformmedia.AudioSource,
) (string, error) {
	if service.recordings == nil {
		return "", ErrVoiceRoundInvalid
	}
	return service.recordings.Upload(ctx, actor, reservationID, source)
}
