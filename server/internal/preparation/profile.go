package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrProfileInvalid             = errors.New("preparation: invalid profile request")
	ErrProfileNotFound            = errors.New("preparation: profile resource not found")
	ErrProfileConflict            = errors.New("preparation: profile resource conflict")
	ErrProfileIdempotencyConflict = errors.New("preparation: idempotency key conflict")
	ErrProfileDeletionGeneration  = errors.New("preparation: stale deletion generation")
	ErrProfileRepository          = errors.New("preparation: profile repository failure")
)

const (
	maxPreparationReferenceBytes = 16 * 1024
	maxPreparationSummaryBytes   = 64 * 1024
)

// Profile is Preparation's production, actor-owned profile record.
type Profile struct {
	ID                string    `json:"preparation_profile_id"`
	UserID            string    `json:"user_id"`
	ResumeRef         string    `json:"resume_ref,omitempty"`
	JobDescriptionRef string    `json:"job_description_ref,omitempty"`
	BackgroundSummary string    `json:"background_summary"`
	Version           int       `json:"version"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Snapshot is an immutable copy of the exact Profile version accepted by the
// create request. Optional source references are copied as frozen values; no
// later Profile or external reference change can reinterpret this record.
type Snapshot struct {
	ID                     string    `json:"preparation_snapshot_id"`
	SourceProfileID        string    `json:"source_profile_id"`
	SourceVersion          int       `json:"source_version"`
	ResumeSnapshot         string    `json:"resume_snapshot,omitempty"`
	JobDescriptionSnapshot string    `json:"job_description_snapshot,omitempty"`
	BackgroundSnapshot     string    `json:"background_snapshot"`
	CreatedAt              time.Time `json:"created_at"`
}

// IdempotencyIntent is derived from trusted transport routing plus a canonical
// request representation. Repository implementations persist it atomically
// with the created resource.
type IdempotencyIntent struct {
	Method             string
	CanonicalPath      string
	Key                string
	PayloadFingerprint [sha256.Size]byte
}

type CreateProfileCommand struct {
	ProfileID string
	Request   CreateProfileRequest
	Intent    IdempotencyIntent
}

type CreateSnapshotCommand struct {
	SnapshotID string
	ProfileID  string
	Request    CreateSnapshotRequest
	Intent     IdempotencyIntent
}

type DeleteProfileDataCommand struct {
	UserID     string
	Generation uint64
}

// ProfileRepository is Preparation's production persistence Port.
type ProfileRepository interface {
	CreateProfile(
		context.Context,
		requestcontext.Actor,
		CreateProfileCommand,
	) (Profile, bool, error)
	CreateSnapshot(
		context.Context,
		requestcontext.Actor,
		CreateSnapshotCommand,
	) (Snapshot, bool, error)
	ReadProfile(
		context.Context,
		requestcontext.Actor,
		string,
	) (Profile, error)
	ReadSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
	) (Snapshot, error)
	DeleteProfileData(context.Context, DeleteProfileDataCommand) error
}

// ProfileSnapshotReader is the narrow Preparation capability consumed by
// callers such as Practice. It exposes no Repository implementation.
type ProfileSnapshotReader interface {
	ReadProfile(
		context.Context,
		requestcontext.Actor,
		string,
	) (Profile, error)
	ReadSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
	) (Snapshot, error)
}

type ResourceIDGenerator interface {
	NewID() (string, error)
}

// PersistenceService validates trusted actor input and canonicalizes create
// intents before handing them to the transactional repository.
type PersistenceService struct {
	repository ProfileRepository
	ids        ResourceIDGenerator
}

func NewPersistenceService(
	repository ProfileRepository,
	ids ResourceIDGenerator,
) (*PersistenceService, error) {
	if repository == nil || ids == nil {
		return nil, errors.New("preparation: persistence dependency is required")
	}
	return &PersistenceService{repository: repository, ids: ids}, nil
}

func (s *PersistenceService) CreateProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request CreateProfileRequest,
) (Profile, bool, error) {
	if ctx == nil || !actor.Valid() || !validCreateProfileRequest(request) {
		return Profile{}, false, ErrProfileInvalid
	}
	intent, err := newPreparationIntent(
		"POST",
		"/v1/preparation-profiles",
		idempotencyKey,
		request,
	)
	if err != nil {
		return Profile{}, false, err
	}
	profileID, err := s.ids.NewID()
	if err != nil {
		return Profile{}, false, ErrProfileRepository
	}
	return s.repository.CreateProfile(ctx, actor, CreateProfileCommand{
		ProfileID: profileID,
		Request:   request,
		Intent:    intent,
	})
}

func (s *PersistenceService) CreateSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
	idempotencyKey string,
	request CreateSnapshotRequest,
) (Snapshot, bool, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(profileID) ||
		request.SourceVersion < 1 {
		return Snapshot{}, false, ErrProfileInvalid
	}
	intent, err := newPreparationIntent(
		"POST",
		"/v1/preparation-profiles/"+profileID+"/snapshots",
		idempotencyKey,
		request,
	)
	if err != nil {
		return Snapshot{}, false, err
	}
	snapshotID, err := s.ids.NewID()
	if err != nil {
		return Snapshot{}, false, ErrProfileRepository
	}
	return s.repository.CreateSnapshot(ctx, actor, CreateSnapshotCommand{
		SnapshotID: snapshotID,
		ProfileID:  profileID,
		Request:    request,
		Intent:     intent,
	})
}

func (s *PersistenceService) ReadProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
) (Profile, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(profileID) {
		return Profile{}, ErrProfileNotFound
	}
	return s.repository.ReadProfile(ctx, actor, profileID)
}

func (s *PersistenceService) ReadSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	snapshotID string,
) (Snapshot, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(snapshotID) {
		return Snapshot{}, ErrProfileNotFound
	}
	return s.repository.ReadSnapshot(ctx, actor, snapshotID)
}

func (s *PersistenceService) DeleteProfileData(
	ctx context.Context,
	command DeleteProfileDataCommand,
) error {
	if ctx == nil || strings.TrimSpace(command.UserID) == "" ||
		command.Generation == 0 {
		return ErrProfileInvalid
	}
	return s.repository.DeleteProfileData(ctx, command)
}

func newPreparationIntent(
	method string,
	path string,
	key string,
	payload any,
) (IdempotencyIntent, error) {
	if method != "POST" || !validCanonicalPath(path) ||
		!validIdempotencyKey(key) {
		return IdempotencyIntent{}, ErrProfileInvalid
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return IdempotencyIntent{}, ErrProfileInvalid
	}
	return IdempotencyIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(canonicalPayload),
	}, nil
}

func validCreateProfileRequest(request CreateProfileRequest) bool {
	return validOptionalPreparationText(
		request.ResumeRef,
		maxPreparationReferenceBytes,
	) &&
		validOptionalPreparationText(
			request.JobDescriptionRef,
			maxPreparationReferenceBytes,
		) &&
		validRequiredPreparationText(
			request.BackgroundSummary,
			maxPreparationSummaryBytes,
		)
}

func validOptionalPreparationText(value string, maxBytes int) bool {
	return value == "" || validRequiredPreparationText(value, maxBytes)
}

func validRequiredPreparationText(value string, maxBytes int) bool {
	return utf8.ValidString(value) &&
		value != "" &&
		len(value) <= maxBytes &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validResourceIdentifier(value string) bool {
	return value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validCanonicalPath(value string) bool {
	return strings.HasPrefix(value, "/") &&
		len(value) <= 1024 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validIdempotencyKey(value string) bool {
	return len(value) >= 8 &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

var _ ProfileSnapshotReader = (*PersistenceService)(nil)
