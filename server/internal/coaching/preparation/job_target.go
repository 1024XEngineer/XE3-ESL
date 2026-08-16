package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type JobTargetSource string

const (
	JobTargetSourceJobDescription JobTargetSource = "job_description"
	JobTargetSourceQuickStart     JobTargetSource = "quick_start"
)

const (
	maxJobTargetTitleBytes       = 512
	maxJobTargetDescriptionBytes = 64 * 1024
	maxJobTargetCompanyBytes     = 512
	maxJobTargetSeniorityBytes   = 256
	maxJobTargetBackgroundBytes  = 16 * 1024
	maxJobTargetFocusBytes       = 8 * 1024

	maxJobTargetCandidateItemCharacters = 2048
	maxJobTargetCandidateItems          = 20
	maxJobTargetCandidateJSONBytes      = 64 * 1024
	maxJobTargetNoticeBytes             = 2048
)

type JobTargetInput struct {
	Source              JobTargetSource `json:"source"`
	JobTitle            string          `json:"job_title,omitempty"`
	JobDescription      string          `json:"job_description,omitempty"`
	Company             string          `json:"company,omitempty"`
	Seniority           string          `json:"seniority,omitempty"`
	CandidateBackground string          `json:"candidate_background,omitempty"`
	PracticeFocus       string          `json:"practice_focus,omitempty"`
}

type JobTargetCatalogRecommendation struct {
	SceneID          string   `json:"scene_id"`
	SceneVersion     int      `json:"scene_version"`
	SelectedRoleIDs  []string `json:"selected_role_ids"`
	PracticeOptionID string   `json:"practice_option_id"`
}

type JobTargetCandidate struct {
	Source                JobTargetSource                `json:"source"`
	GeneralAdviceOnly     bool                           `json:"general_advice_only"`
	JobTitle              string                         `json:"job_title"`
	Company               string                         `json:"company,omitempty"`
	Seniority             string                         `json:"seniority"`
	Responsibilities      []string                       `json:"responsibilities"`
	CoreSkills            []string                       `json:"core_skills"`
	CommunicationFocus    []string                       `json:"communication_focus"`
	PracticeGoals         []string                       `json:"practice_goals"`
	ScopeNotice           string                         `json:"scope_notice"`
	CatalogRecommendation JobTargetCatalogRecommendation `json:"catalog_recommendation"`
}

type JobTargetParser interface {
	ParseJobTarget(context.Context, JobTargetInput) (JobTargetCandidate, error)
}

type StableJobTargetParserError interface {
	error
	StableCategory() string
}

func validJobTargetInput(input JobTargetInput) bool {
	switch input.Source {
	case JobTargetSourceJobDescription:
		if !validJobTargetText(input.JobDescription, maxJobTargetDescriptionBytes, true) {
			return false
		}
	case JobTargetSourceQuickStart:
		if input.JobDescription != "" ||
			!validJobTargetText(input.JobTitle, maxJobTargetTitleBytes, true) {
			return false
		}
	default:
		return false
	}
	return validJobTargetText(input.JobTitle, maxJobTargetTitleBytes, false) &&
		validJobTargetText(input.Company, maxJobTargetCompanyBytes, false) &&
		validJobTargetText(input.Seniority, maxJobTargetSeniorityBytes, false) &&
		validJobTargetText(input.CandidateBackground, maxJobTargetBackgroundBytes, false) &&
		validJobTargetText(input.PracticeFocus, maxJobTargetFocusBytes, false)
}

func ValidJobTargetInput(input JobTargetInput) bool { return validJobTargetInput(input) }

func validJobTargetText(value string, maxBytes int, required bool) bool {
	if value == "" {
		return !required
	}
	return utf8.ValidString(value) && len(value) <= maxBytes &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validateJobTargetCandidate(ctx context.Context, candidate JobTargetCandidate, source JobTargetSource, catalog scene.CatalogReader) error {
	if ctx == nil || catalog == nil || !validJobTargetCandidateShape(candidate, source) {
		return ErrInterviewPreparationInvalid
	}
	recommendation := candidate.CatalogRecommendation
	selection, err := catalog.ResolveSelection(ctx, recommendation.SceneID, recommendation.SceneVersion, append([]string(nil), recommendation.SelectedRoleIDs...), recommendation.PracticeOptionID)
	if err != nil {
		return errors.Join(ErrInterviewPreparationInvalid, err)
	}
	if selection.Scene.Experience != scene.PracticeExperienceInterview || len(selection.SelectedRoleIDs) != 1 {
		return ErrInterviewPreparationInvalid
	}
	return nil
}

func validJobTargetCandidateShape(candidate JobTargetCandidate, source JobTargetSource) bool {
	if candidate.Source != source ||
		!validJobTargetText(candidate.JobTitle, maxJobTargetTitleBytes, true) ||
		!validJobTargetText(candidate.Company, maxJobTargetCompanyBytes, false) ||
		!validJobTargetText(candidate.Seniority, maxJobTargetSeniorityBytes, true) ||
		!validJobTargetText(candidate.ScopeNotice, maxJobTargetNoticeBytes, true) ||
		!validJobTargetCandidateList(candidate.Responsibilities) ||
		!validJobTargetCandidateList(candidate.CoreSkills) ||
		!validJobTargetCandidateList(candidate.CommunicationFocus) ||
		!validJobTargetCandidateList(candidate.PracticeGoals) {
		return false
	}
	if (source == JobTargetSourceQuickStart) != candidate.GeneralAdviceOnly {
		return false
	}
	recommendation := candidate.CatalogRecommendation
	return recommendation.SceneVersion > 0 &&
		validResourceIdentifier(recommendation.SceneID) &&
		validResourceIdentifier(recommendation.PracticeOptionID) &&
		len(recommendation.SelectedRoleIDs) > 0 &&
		validJobTargetCandidateJSONSize(candidate)
}

func ValidJobTargetCandidateShape(candidate JobTargetCandidate, source JobTargetSource) bool {
	return validJobTargetCandidateShape(candidate, source)
}

func validJobTargetCandidateList(values []string) bool {
	if len(values) == 0 || len(values) > maxJobTargetCandidateItems {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !utf8.ValidString(value) || value == "" || strings.ContainsRune(value, '\x00') ||
			strings.TrimSpace(value) != value || utf8.RuneCountInString(value) > maxJobTargetCandidateItemCharacters {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validJobTargetCandidateJSONSize(candidate JobTargetCandidate) bool {
	encoded, err := json.Marshal(candidate)
	return err == nil && len(encoded) <= maxJobTargetCandidateJSONBytes
}

func ValidJobTargetCandidateJSONSize(candidate JobTargetCandidate) bool {
	return validJobTargetCandidateJSONSize(candidate)
}

var stableJobTargetCategoryPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidStableJobTargetCategory(value string) bool {
	return stableJobTargetCategoryPattern.MatchString(value)
}

func jobTargetParserErrorCategory(err error) string {
	var stable StableJobTargetParserError
	if errors.As(err, &stable) {
		category := strings.TrimSpace(stable.StableCategory())
		if stableJobTargetCategoryPattern.MatchString(category) {
			return category
		}
	}
	return "provider_failure"
}

func cloneJobTargetCandidate(source JobTargetCandidate) JobTargetCandidate {
	result := source
	result.Responsibilities = append([]string(nil), source.Responsibilities...)
	result.CoreSkills = append([]string(nil), source.CoreSkills...)
	result.CommunicationFocus = append([]string(nil), source.CommunicationFocus...)
	result.PracticeGoals = append([]string(nil), source.PracticeGoals...)
	result.CatalogRecommendation.SelectedRoleIDs = append([]string(nil), source.CatalogRecommendation.SelectedRoleIDs...)
	return result
}

func CloneJobTargetCandidate(source JobTargetCandidate) JobTargetCandidate {
	return cloneJobTargetCandidate(source)
}

var (
	ErrInterviewPreparationInvalid      = errors.New("preparation: invalid interview preparation")
	ErrInterviewPreparationNotFound     = errors.New("preparation: interview preparation not found")
	ErrInterviewPreparationConflict     = errors.New("preparation: interview preparation conflict")
	ErrInterviewPreparationRequestReuse = errors.New("preparation: client request id reused with different input")
	ErrInterviewPreparationGeneration   = errors.New("preparation: interview preparation generation failed")
)

type InterviewPreparationStatus string

const (
	InterviewPreparationDraft     InterviewPreparationStatus = "draft"
	InterviewPreparationConfirmed InterviewPreparationStatus = "confirmed"
	InterviewPreparationDiscarded InterviewPreparationStatus = "discarded"
)

type InterviewPreparation struct {
	ID            string                     `json:"interview_preparation_id"`
	UserID        string                     `json:"user_id"`
	Input         JobTargetInput             `json:"input"`
	Candidate     JobTargetCandidate         `json:"candidate"`
	ResumeContent *ResumeMaterial            `json:"resume_content,omitempty"`
	Status        InterviewPreparationStatus `json:"status"`
	Version       int                        `json:"version"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

type InterviewPreparationSnapshot struct {
	ID            string             `json:"interview_preparation_id"`
	Version       int                `json:"version"`
	Input         JobTargetInput     `json:"input"`
	Candidate     JobTargetCandidate `json:"candidate"`
	ResumeContent *ResumeMaterial    `json:"resume_content,omitempty"`
}

type CreateInterviewPreparationRequest struct {
	Input  JobTargetInput         `json:"input"`
	Resume *InterviewResumeUpload `json:"-"`
}

type InterviewResumeUpload struct {
	Body           io.ReadSeeker
	Size           int64
	ChecksumSHA256 string
}

type InterviewResumeExtractor interface {
	Extract(context.Context, string, string, InterviewResumeUpload) (ResumeMaterial, error)
}

type InterviewPreparationPatchAction string

const (
	InterviewPreparationRegenerate InterviewPreparationPatchAction = "regenerate"
	InterviewPreparationConfirm    InterviewPreparationPatchAction = "confirm"
	InterviewPreparationDiscard    InterviewPreparationPatchAction = "discard"
)

type PatchInterviewPreparationRequest struct {
	ExpectedVersion int                             `json:"expected_version"`
	Action          InterviewPreparationPatchAction `json:"action"`
	Input           *JobTargetInput                 `json:"input,omitempty"`
	Candidate       *JobTargetCandidate             `json:"candidate,omitempty"`
}

type CreateInterviewPreparationCommand struct {
	ID                 string
	Input              JobTargetInput
	Candidate          JobTargetCandidate
	ResumeContent      *ResumeMaterial
	ClientRequestID    string
	RequestFingerprint [sha256.Size]byte
}

type PatchInterviewPreparationCommand struct {
	ID                 string
	ExpectedVersion    int
	Action             InterviewPreparationPatchAction
	Input              *JobTargetInput
	Candidate          *JobTargetCandidate
	ClientRequestID    string
	RequestFingerprint [sha256.Size]byte
}

type InterviewPreparationRepository interface {
	ReplayCreate(context.Context, requestcontext.Actor, string, [sha256.Size]byte) (InterviewPreparation, bool, error)
	Create(context.Context, requestcontext.Actor, CreateInterviewPreparationCommand) (InterviewPreparation, bool, error)
	Get(context.Context, requestcontext.Actor, string) (InterviewPreparation, error)
	Patch(context.Context, requestcontext.Actor, PatchInterviewPreparationCommand) (InterviewPreparation, bool, error)
}

type InterviewPreparationReader interface {
	ReadConfirmed(context.Context, requestcontext.Actor, string, int) (InterviewPreparationSnapshot, error)
}

type InterviewPreparationService struct {
	repository InterviewPreparationRepository
	ids        ResourceIDGenerator
	parser     JobTargetParser
	catalog    scene.CatalogReader
	resumes    InterviewResumeExtractor
}

func NewInterviewPreparationService(repository InterviewPreparationRepository, ids ResourceIDGenerator, parser JobTargetParser, catalog scene.CatalogReader, resumes InterviewResumeExtractor) (*InterviewPreparationService, error) {
	if repository == nil || ids == nil || parser == nil || catalog == nil {
		return nil, errors.New("preparation: interview preparation dependencies are required")
	}
	return &InterviewPreparationService{repository: repository, ids: ids, parser: parser, catalog: catalog, resumes: resumes}, nil
}

func (s *InterviewPreparationService) Create(ctx context.Context, actor requestcontext.Actor, clientRequestID string, request CreateInterviewPreparationRequest) (InterviewPreparation, bool, error) {
	if ctx == nil || !actor.Valid() || !validClientRequestID(clientRequestID) ||
		!validJobTargetInput(request.Input) || !validInterviewResumeUpload(request.Resume) {
		return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
	}
	fingerprint, err := fingerprintJSON(struct {
		Input          JobTargetInput `json:"input"`
		ResumeChecksum string         `json:"resume_checksum,omitempty"`
	}{Input: request.Input, ResumeChecksum: interviewResumeChecksum(request.Resume)})
	if err != nil {
		return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
	}
	if existing, found, err := s.repository.ReplayCreate(ctx, actor, clientRequestID, fingerprint); err != nil || found {
		return existing, found, err
	}
	var resume *ResumeMaterial
	if request.Resume != nil {
		if s.resumes == nil {
			return InterviewPreparation{}, false, ErrInterviewPreparationGeneration
		}
		material, extractErr := s.resumes.Extract(
			ctx, actor.UserID, clientRequestID, *request.Resume,
		)
		if extractErr != nil {
			return InterviewPreparation{}, false, extractErr
		}
		resume = &material
	}
	candidate, err := s.parser.ParseJobTarget(ctx, request.Input)
	if err != nil {
		return InterviewPreparation{}, false, errors.Join(ErrInterviewPreparationGeneration, err)
	}
	if err := validateJobTargetCandidate(ctx, candidate, request.Input.Source, s.catalog); err != nil {
		return InterviewPreparation{}, false, err
	}
	id, err := s.ids.NewID()
	if err != nil || !ValidAggregateID(id) {
		return InterviewPreparation{}, false, ErrInterviewPreparationConflict
	}
	return s.repository.Create(ctx, actor, CreateInterviewPreparationCommand{
		ID: id, Input: request.Input, Candidate: candidate,
		ResumeContent: resume, ClientRequestID: clientRequestID,
		RequestFingerprint: fingerprint,
	})
}

func (s *InterviewPreparationService) Get(ctx context.Context, actor requestcontext.Actor, id string) (InterviewPreparation, error) {
	if ctx == nil || !actor.Valid() || !ValidAggregateID(id) {
		return InterviewPreparation{}, ErrInterviewPreparationNotFound
	}
	return s.repository.Get(ctx, actor, id)
}

func (s *InterviewPreparationService) Patch(ctx context.Context, actor requestcontext.Actor, id, clientRequestID string, request PatchInterviewPreparationRequest) (InterviewPreparation, bool, error) {
	if ctx == nil || !actor.Valid() || !ValidAggregateID(id) || !validClientRequestID(clientRequestID) || request.ExpectedVersion < 1 {
		return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
	}
	fingerprint, err := fingerprintJSON(request)
	if err != nil {
		return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
	}
	command := PatchInterviewPreparationCommand{ID: id, ExpectedVersion: request.ExpectedVersion, Action: request.Action, ClientRequestID: clientRequestID, RequestFingerprint: fingerprint}
	switch request.Action {
	case InterviewPreparationRegenerate:
		if request.Input == nil || request.Candidate != nil || !validJobTargetInput(*request.Input) {
			return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
		}
		candidate, err := s.parser.ParseJobTarget(ctx, *request.Input)
		if err != nil {
			return InterviewPreparation{}, false, errors.Join(ErrInterviewPreparationGeneration, err)
		}
		if err := validateJobTargetCandidate(ctx, candidate, request.Input.Source, s.catalog); err != nil {
			return InterviewPreparation{}, false, err
		}
		command.Input, command.Candidate = request.Input, &candidate
	case InterviewPreparationConfirm:
		if request.Input != nil || request.Candidate == nil {
			return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
		}
		current, err := s.repository.Get(ctx, actor, id)
		if err != nil {
			return InterviewPreparation{}, false, err
		}
		if current.Version != request.ExpectedVersion || current.Status != InterviewPreparationDraft || validateJobTargetCandidate(ctx, *request.Candidate, current.Input.Source, s.catalog) != nil {
			return InterviewPreparation{}, false, ErrInterviewPreparationConflict
		}
		command.Candidate = request.Candidate
	case InterviewPreparationDiscard:
		if request.Input != nil || request.Candidate != nil {
			return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
		}
	default:
		return InterviewPreparation{}, false, ErrInterviewPreparationInvalid
	}
	return s.repository.Patch(ctx, actor, command)
}

func (s *InterviewPreparationService) ReadConfirmed(ctx context.Context, actor requestcontext.Actor, id string, version int) (InterviewPreparationSnapshot, error) {
	value, err := s.Get(ctx, actor, id)
	if err != nil {
		return InterviewPreparationSnapshot{}, err
	}
	if value.Version != version || value.Status != InterviewPreparationConfirmed {
		return InterviewPreparationSnapshot{}, ErrInterviewPreparationConflict
	}
	return snapshotInterviewPreparation(value), nil
}

func validInterviewResumeUpload(value *InterviewResumeUpload) bool {
	return value == nil || (value.Body != nil && value.Size > 0 &&
		value.Size <= 10*1024*1024 && len(value.ChecksumSHA256) == 64)
}

func interviewResumeChecksum(value *InterviewResumeUpload) string {
	if value == nil {
		return ""
	}
	return value.ChecksumSHA256
}

func validClientRequestID(value string) bool {
	return validIdempotencyKey(value)
}

func fingerprintJSON(value any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func snapshotInterviewPreparation(value InterviewPreparation) InterviewPreparationSnapshot {
	result := InterviewPreparationSnapshot{ID: value.ID, Version: value.Version, Input: value.Input, Candidate: cloneJobTargetCandidate(value.Candidate)}
	if value.ResumeContent != nil {
		resume := cloneResumeMaterial(*value.ResumeContent)
		result.ResumeContent = &resume
	}
	return result
}
