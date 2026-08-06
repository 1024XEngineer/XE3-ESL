package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	preparationmodel "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	preparationport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
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
	maxPreparationReferenceLength = 16 * 1024
	maxPreparationSummaryLength   = 64 * 1024
)

// Profile is Preparation's production, actor-owned profile record.
type Profile struct {
	ID                           string                            `json:"preparation_profile_id"`
	UserID                       string                            `json:"user_id"`
	Context                      *preparationmodel.ResolvedContext `json:"preparation_context,omitempty"`
	ResumeID                     string                            `json:"resume_id,omitempty"`
	ResumeRevision               int64                             `json:"resume_revision,omitempty"`
	JobDescriptionRef            string                            `json:"job_description_ref,omitempty"`
	BackgroundSummary            string                            `json:"background_summary"`
	JobTargetID                  string                            `json:"job_target_id,omitempty"`
	JobTargetConfirmationVersion int                               `json:"job_target_confirmation_version,omitempty"`
	Version                      int                               `json:"version"`
	UpdatedAt                    time.Time                         `json:"updated_at"`
}

// Snapshot is an immutable copy of the exact Profile version accepted by the
// create request. Optional source references are copied as frozen values; no
// later Profile or external reference change can reinterpret this record.
type Snapshot struct {
	ID                                 string                            `json:"preparation_snapshot_id"`
	SourceProfileID                    string                            `json:"source_profile_id"`
	SourceVersion                      int                               `json:"source_version"`
	Context                            *preparationmodel.ResolvedContext `json:"preparation_context,omitempty"`
	SourceJobTargetID                  string                            `json:"source_job_target_id,omitempty"`
	SourceJobTargetConfirmationVersion int                               `json:"source_job_target_confirmation_version,omitempty"`
	JobTargetInputSnapshot             *JobTargetInput                   `json:"job_target_input_snapshot,omitempty"`
	JobTargetCandidateSnapshot         *JobTargetCandidate               `json:"job_target_candidate_snapshot,omitempty"`
	ResumeSnapshot                     *ResumeRevisionSnapshot           `json:"resume_snapshot,omitempty"`
	JobDescriptionSnapshot             string                            `json:"job_description_snapshot,omitempty"`
	BackgroundSnapshot                 string                            `json:"background_snapshot"`
	CreatedAt                          time.Time                         `json:"created_at"`
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
	ProfileID      string
	Request        CreateProfileRequest
	ResumeRevision *ResumeRevisionSnapshot
	Context        *preparationmodel.ResolvedContext
	Intent         IdempotencyIntent
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
	// Create methods return replayed=true only when an existing, matching
	// idempotency result is returned. A newly persisted resource returns false.
	ReplayProfile(
		context.Context,
		requestcontext.Actor,
		IdempotencyIntent,
	) (profile Profile, found bool, err error)
	CreateProfile(
		context.Context,
		requestcontext.Actor,
		CreateProfileCommand,
	) (profile Profile, replayed bool, err error)
	ReplaySnapshot(
		context.Context,
		requestcontext.Actor,
		IdempotencyIntent,
	) (snapshot Snapshot, found bool, err error)
	CreateSnapshot(
		context.Context,
		requestcontext.Actor,
		CreateSnapshotCommand,
	) (snapshot Snapshot, replayed bool, err error)
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

type ContextResolver interface {
	Resolve(
		context.Context,
		preparationport.ResolveCommand,
	) (preparationmodel.ResolvedContext, error)
}

// PersistenceService validates trusted actor input and canonicalizes create
// intents before handing them to the transactional repository.
type PersistenceService struct {
	repository ProfileRepository
	ids        ResourceIDGenerator
	resumes    ResumeRevisionReader
	contexts   ContextResolver
}

func NewPersistenceService(
	repository ProfileRepository,
	ids ResourceIDGenerator,
	resumes ResumeRevisionReader,
) (*PersistenceService, error) {
	return newPersistenceService(repository, ids, resumes, nil)
}

func NewPersistenceServiceWithContext(
	repository ProfileRepository,
	ids ResourceIDGenerator,
	resumes ResumeRevisionReader,
	contexts ContextResolver,
) (*PersistenceService, error) {
	if contexts == nil {
		return nil, errors.New("preparation: context resolver is required")
	}
	return newPersistenceService(repository, ids, resumes, contexts)
}

func newPersistenceService(
	repository ProfileRepository,
	ids ResourceIDGenerator,
	resumes ResumeRevisionReader,
	contexts ContextResolver,
) (*PersistenceService, error) {
	if repository == nil || ids == nil || resumes == nil {
		return nil, errors.New("preparation: persistence dependency is required")
	}
	return &PersistenceService{
		repository: repository,
		ids:        ids,
		resumes:    resumes,
		contexts:   contexts,
	}, nil
}

func (s *PersistenceService) CreateProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request CreateProfileRequest,
) (profile Profile, replayed bool, err error) {
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
	replayedProfile, found, err := s.repository.ReplayProfile(
		ctx,
		actor,
		intent,
	)
	if err != nil {
		return Profile{}, false, err
	}
	if found {
		return replayedProfile, true, nil
	}
	var resolvedContext *preparationmodel.ResolvedContext
	if request.Kind != "" {
		if s.contexts == nil {
			return Profile{}, false, ErrProfileInvalid
		}
		input := preparationmodel.ContextInput{Kind: request.Kind}
		switch request.Kind {
		case preparationmodel.PreparationKindInterview:
			interview := &preparationmodel.InterviewContextInput{
				JobTarget: preparationmodel.ConfirmedJobTargetRef{
					JobTargetID:         request.JobTargetID,
					ConfirmationVersion: request.JobTargetConfirmationVersion,
				},
			}
			if request.ResumeID != "" {
				interview.Resume = &preparationmodel.ResumeRevisionRef{
					ResumeID: request.ResumeID,
					Revision: request.ResumeRevision,
				}
			}
			input.Interview = interview
		case preparationmodel.PreparationKindScenario:
			input.Scenario = request.Scenario
		default:
			return Profile{}, false, ErrProfileInvalid
		}
		resolved, err := s.contexts.Resolve(ctx, preparationport.ResolveCommand{
			Actor: actor,
			Input: input,
		})
		if err != nil {
			return Profile{}, false, ErrProfileInvalid
		}
		resolvedContext = &resolved
	}
	var resumeRevision *ResumeRevisionSnapshot
	if request.ResumeID != "" {
		resolved, err := s.resumes.ReadOwnedRevision(
			ctx,
			actor,
			request.ResumeID,
			request.ResumeRevision,
		)
		if err != nil {
			return Profile{}, false, err
		}
		if !validResumeRevisionSnapshot(resolved) ||
			resolved.ResumeID != request.ResumeID ||
			resolved.Revision != request.ResumeRevision {
			return Profile{}, false, ErrProfileConflict
		}
		resolved = cloneResumeRevisionSnapshot(resolved)
		resumeRevision = &resolved
	}
	profileID, err := s.ids.NewID()
	if err != nil {
		return Profile{}, false, ErrProfileRepository
	}
	return s.repository.CreateProfile(ctx, actor, CreateProfileCommand{
		ProfileID:      profileID,
		Request:        request,
		ResumeRevision: resumeRevision,
		Context:        resolvedContext,
		Intent:         intent,
	})
}

func (s *PersistenceService) CreateSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
	idempotencyKey string,
	request CreateSnapshotRequest,
) (snapshot Snapshot, replayed bool, err error) {
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
	replayedSnapshot, found, err := s.repository.ReplaySnapshot(
		ctx,
		actor,
		intent,
	)
	if err != nil {
		return Snapshot{}, false, err
	}
	if found {
		return replayedSnapshot, true, nil
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
	resumePairValid := (request.ResumeID == "" && request.ResumeRevision == 0) ||
		(validResourceIdentifier(request.ResumeID) && request.ResumeRevision > 0)
	targetPairValid := (request.JobTargetID == "" &&
		request.JobTargetConfirmationVersion == 0) ||
		(validResourceIdentifier(request.JobTargetID) &&
			request.JobTargetConfirmationVersion > 0)
	kindShapeValid := false
	switch request.Kind {
	case "":
		kindShapeValid = request.Scenario == nil
	case preparationmodel.PreparationKindInterview:
		kindShapeValid = request.Scenario == nil && request.JobTargetID != ""
	case preparationmodel.PreparationKindScenario:
		kindShapeValid = request.Scenario != nil && request.ResumeID == "" &&
			request.JobTargetID == "" && request.JobDescriptionRef == ""
	}
	backgroundValid := validRequiredPreparationText(
		request.BackgroundSummary,
		maxPreparationSummaryLength,
	)
	if request.Kind == preparationmodel.PreparationKindScenario {
		backgroundValid = request.BackgroundSummary == "" || backgroundValid
	}
	return kindShapeValid && resumePairValid && targetPairValid &&
		validOptionalPreparationText(
			request.JobDescriptionRef,
			maxPreparationReferenceLength,
		) &&
		backgroundValid
}

// ValidCreateProfileRequest is exported only for Preparation transport
// adapters. Domain callers should enter through PersistenceService.
func ValidCreateProfileRequest(request CreateProfileRequest) bool {
	return validCreateProfileRequest(request)
}

func targetedPreparationSnapshot(snapshot Snapshot) bool {
	return validResourceIdentifier(snapshot.SourceJobTargetID) &&
		snapshot.SourceJobTargetConfirmationVersion > 0 &&
		snapshot.JobTargetInputSnapshot != nil &&
		snapshot.JobTargetCandidateSnapshot != nil
}

func cloneSnapshotJobTargetInput(input *JobTargetInput) *JobTargetInput {
	if input == nil {
		return nil
	}
	result := *input
	return &result
}

func cloneSnapshotJobTargetCandidate(
	candidate *JobTargetCandidate,
) *JobTargetCandidate {
	if candidate == nil {
		return nil
	}
	result := cloneJobTargetCandidate(*candidate)
	return &result
}

func validOptionalPreparationText(value string, maxLength int) bool {
	return value == "" || validRequiredPreparationText(value, maxLength)
}

func validRequiredPreparationText(value string, maxLength int) bool {
	return utf8.ValidString(value) &&
		value != "" &&
		utf8.RuneCountInString(value) <= maxLength &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validResourceIdentifier(value string) bool {
	return value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func ValidResourceIdentifier(value string) bool {
	return validResourceIdentifier(value)
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

func ValidIdempotencyKey(value string) bool {
	return validIdempotencyKey(value)
}

var _ ProfileSnapshotReader = (*PersistenceService)(nil)
