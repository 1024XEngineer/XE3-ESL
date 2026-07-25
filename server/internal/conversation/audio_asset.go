package conversation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type AudioAssetStatus string

const (
	AudioAssetStaged AudioAssetStatus = "staged"
	// AudioAssetMetadataCommitted means object upload completed, but the asset
	// remains unconfirmed until it is bound to a Turn and becomes readable.
	AudioAssetMetadataCommitted AudioAssetStatus = "metadata_committed"
	AudioAssetReadable          AudioAssetStatus = "readable"
	AudioAssetDeleting          AudioAssetStatus = "deleting"
	AudioAssetDeleted           AudioAssetStatus = "deleted"
)

var (
	ErrAudioAssetNotFound            = errors.New("audio asset not found")
	ErrAudioAssetTurnNotFound        = errors.New("turn not found")
	ErrAudioAssetForbidden           = errors.New("audio asset does not belong to actor")
	ErrAudioAssetInvalid             = errors.New("audio asset is invalid")
	ErrAudioAssetInvalidDependency   = errors.New("audio asset service dependency is nil")
	ErrAudioAssetInvalidTransition   = errors.New("invalid audio asset state transition")
	ErrAudioAssetAlreadyBound        = errors.New("audio asset or turn is already bound")
	ErrAudioAssetIdempotencyConflict = errors.New("audio asset idempotency key was reused")
	ErrAudioAssetConcurrentUpdate    = errors.New("audio asset was concurrently updated")
	ErrAudioAssetPlaybackTTL         = errors.New("signed playback URL lifetime exceeds two minutes")
	ErrAudioAssetPlaybackURL         = errors.New("signed playback URL must be a non-empty HTTPS URL")
)

// AudioAsset is Conversation's durable metadata for a protected recording.
// Object bytes and signed URLs are deliberately not part of this model.
type AudioAsset struct {
	ID              string
	OwnerID         string
	UploadRequestID string
	ObjectKey       string
	CandidateID     string
	TurnID          string
	ContentType     string
	Size            int64
	ChecksumSHA256  string
	Duration        time.Duration
	ETag            string
	Status          AudioAssetStatus
	StagedUntil     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       time.Time
	Version         uint64
}

func newStagedAudioAsset(
	id string,
	ownerID string,
	uploadRequestID string,
	objectKey string,
	contentType string,
	size int64,
	checksumSHA256 string,
	duration time.Duration,
	now time.Time,
	stagedUntil time.Time,
) (AudioAsset, error) {
	asset := AudioAsset{
		ID:              strings.TrimSpace(id),
		OwnerID:         strings.TrimSpace(ownerID),
		UploadRequestID: strings.TrimSpace(uploadRequestID),
		ObjectKey:       strings.TrimSpace(objectKey),
		ContentType:     strings.TrimSpace(contentType),
		Size:            size,
		ChecksumSHA256:  strings.TrimSpace(checksumSHA256),
		Duration:        duration,
		Status:          AudioAssetStaged,
		StagedUntil:     stagedUntil,
		CreatedAt:       now,
		UpdatedAt:       now,
		Version:         1,
	}
	if err := asset.validateStaged(); err != nil {
		return AudioAsset{}, err
	}
	return asset, nil
}

func (a AudioAsset) validateStaged() error {
	if a.ID == "" ||
		a.OwnerID == "" ||
		a.UploadRequestID == "" ||
		a.ObjectKey == "" ||
		(a.ContentType != "audio/wav" && a.ContentType != "audio/x-wav") ||
		a.Size <= 0 ||
		!validSHA256(a.ChecksumSHA256) ||
		a.Duration <= 0 ||
		a.CreatedAt.IsZero() ||
		!a.StagedUntil.After(a.CreatedAt) {
		return ErrAudioAssetInvalid
	}
	return nil
}

func validSHA256(checksum string) bool {
	if len(checksum) != 64 {
		return false
	}
	for _, character := range checksum {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (a AudioAsset) ownedBy(actorID string) error {
	if strings.TrimSpace(actorID) == "" || a.OwnerID != actorID {
		return ErrAudioAssetForbidden
	}
	return nil
}

func (a AudioAsset) sameUpload(request UploadRecordingRequest) bool {
	return a.ContentType == strings.TrimSpace(request.ContentType) &&
		a.Size == request.Size &&
		a.ChecksumSHA256 == strings.TrimSpace(request.ChecksumSHA256) &&
		a.Duration == request.Duration
}

func (a *AudioAsset) commitMetadata(etag string, now time.Time) error {
	if a.Status == AudioAssetMetadataCommitted || a.Status == AudioAssetReadable {
		return nil
	}
	if a.Status != AudioAssetStaged {
		return fmt.Errorf("%w: %s to %s", ErrAudioAssetInvalidTransition, a.Status, AudioAssetMetadataCommitted)
	}
	a.ETag = strings.TrimSpace(etag)
	a.Status = AudioAssetMetadataCommitted
	a.UpdatedAt = now
	a.Version++
	return nil
}

func (a *AudioAsset) bindTurn(candidateID string, turnID string, now time.Time) error {
	candidateID = strings.TrimSpace(candidateID)
	turnID = strings.TrimSpace(turnID)
	if candidateID == "" || turnID == "" {
		return ErrAudioAssetInvalid
	}
	if a.Status == AudioAssetReadable &&
		a.CandidateID == candidateID &&
		a.TurnID == turnID {
		return nil
	}
	if a.CandidateID != "" && a.CandidateID != candidateID {
		return ErrAudioAssetAlreadyBound
	}
	if a.TurnID != "" && a.TurnID != turnID {
		return ErrAudioAssetAlreadyBound
	}
	if a.Status != AudioAssetMetadataCommitted {
		return fmt.Errorf("%w: %s to %s", ErrAudioAssetInvalidTransition, a.Status, AudioAssetReadable)
	}
	a.CandidateID = candidateID
	a.TurnID = turnID
	a.Status = AudioAssetReadable
	a.UpdatedAt = now
	a.Version++
	return nil
}

func (a *AudioAsset) beginDeleting(now time.Time) error {
	if a.Status == AudioAssetDeleting || a.Status == AudioAssetDeleted {
		return nil
	}
	switch a.Status {
	case AudioAssetStaged, AudioAssetMetadataCommitted, AudioAssetReadable:
		a.Status = AudioAssetDeleting
		a.UpdatedAt = now
		a.Version++
		return nil
	default:
		return fmt.Errorf("%w: %s to %s", ErrAudioAssetInvalidTransition, a.Status, AudioAssetDeleting)
	}
}

func (a *AudioAsset) finishDeleting(now time.Time) error {
	if a.Status == AudioAssetDeleted {
		return nil
	}
	if a.Status != AudioAssetDeleting {
		return fmt.Errorf("%w: %s to %s", ErrAudioAssetInvalidTransition, a.Status, AudioAssetDeleted)
	}
	a.Status = AudioAssetDeleted
	a.DeletedAt = now
	a.UpdatedAt = now
	a.Version++
	return nil
}
