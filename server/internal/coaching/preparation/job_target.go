package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

type JobTargetStage string

const (
	JobTargetStageDraft                JobTargetStage = "draft"
	JobTargetStageParsing              JobTargetStage = "parsing"
	JobTargetStageAnalysisFailed       JobTargetStage = "analysis_failed"
	JobTargetStageAwaitingConfirmation JobTargetStage = "awaiting_confirmation"
	JobTargetStageConfirmed            JobTargetStage = "confirmed"
	JobTargetStageDiscarded            JobTargetStage = "discarded"
)

type JobTargetAnalysisStatus string

const (
	JobTargetAnalysisRunning   JobTargetAnalysisStatus = "running"
	JobTargetAnalysisSucceeded JobTargetAnalysisStatus = "succeeded"
	JobTargetAnalysisFailed    JobTargetAnalysisStatus = "failed"
)

var (
	ErrJobTargetInvalid             = errors.New("preparation: invalid job target request")
	ErrJobTargetNotFound            = errors.New("preparation: job target not found")
	ErrJobTargetConflict            = errors.New("preparation: job target version conflict")
	ErrJobTargetIdempotencyConflict = errors.New("preparation: job target idempotency conflict")
	ErrJobTargetAnalysisFailed      = errors.New("preparation: job target analysis failed")
	ErrJobTargetAnalysisClaimLost   = errors.New("preparation: job target analysis claim lost")
	ErrJobTargetRepository          = errors.New("preparation: job target repository failure")
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

	defaultJobTargetAnalysisLease = 2 * time.Minute
	jobTargetPersistenceTimeout   = 5 * time.Second
)

// JobTargetInput is the exact editable, untrusted material for one input
// version. It is never interpreted as a system instruction or tool request.
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

// JobTargetCandidate is an unconfirmed parser proposal until a separate
// confirmation command persists an editable copy.
type JobTargetCandidate struct {
	Source                JobTargetSource                `json:"source"`
	GeneralAdviceOnly     bool                           `json:"general_advice_only"`
	JobTitle              string                         `json:"job_title"`
	Seniority             string                         `json:"seniority"`
	Responsibilities      []string                       `json:"responsibilities"`
	CoreSkills            []string                       `json:"core_skills"`
	CommunicationFocus    []string                       `json:"communication_focus"`
	PracticeGoals         []string                       `json:"practice_goals"`
	ScopeNotice           string                         `json:"scope_notice"`
	CatalogRecommendation JobTargetCatalogRecommendation `json:"catalog_recommendation"`
}

type JobTargetAnalysis struct {
	InputVersion        int                     `json:"input_version"`
	AnalysisVersion     int                     `json:"analysis_version"`
	Attempt             int                     `json:"attempt"`
	Status              JobTargetAnalysisStatus `json:"status"`
	Candidate           *JobTargetCandidate     `json:"candidate,omitempty"`
	StableErrorCategory string                  `json:"stable_error_category,omitempty"`
	StartedAt           time.Time               `json:"started_at"`
	FinishedAt          *time.Time              `json:"finished_at,omitempty"`
}

type JobTargetConfirmation struct {
	InputVersion        int                `json:"input_version"`
	AnalysisVersion     int                `json:"analysis_version"`
	ConfirmationVersion int                `json:"confirmation_version"`
	Candidate           JobTargetCandidate `json:"candidate"`
	ConfirmedAt         time.Time          `json:"confirmed_at"`
}

// JobTarget is Preparation's actor-owned recovery projection. Analysis and
// confirmation are present only when they belong to the current input version.
type JobTarget struct {
	ID           string                 `json:"job_target_id"`
	UserID       string                 `json:"user_id"`
	Input        JobTargetInput         `json:"input"`
	InputVersion int                    `json:"input_version"`
	Stage        JobTargetStage         `json:"stage"`
	Analysis     *JobTargetAnalysis     `json:"analysis,omitempty"`
	Confirmation *JobTargetConfirmation `json:"confirmation,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type CreateJobTargetRequest struct {
	Source              JobTargetSource `json:"source"`
	JobTitle            string          `json:"job_title,omitempty"`
	JobDescription      string          `json:"job_description,omitempty"`
	Company             string          `json:"company,omitempty"`
	Seniority           string          `json:"seniority,omitempty"`
	CandidateBackground string          `json:"candidate_background,omitempty"`
	PracticeFocus       string          `json:"practice_focus,omitempty"`
}

type UpdateJobTargetRequest struct {
	ExpectedInputVersion int             `json:"expected_input_version"`
	Source               JobTargetSource `json:"source"`
	JobTitle             string          `json:"job_title,omitempty"`
	JobDescription       string          `json:"job_description,omitempty"`
	Company              string          `json:"company,omitempty"`
	Seniority            string          `json:"seniority,omitempty"`
	CandidateBackground  string          `json:"candidate_background,omitempty"`
	PracticeFocus        string          `json:"practice_focus,omitempty"`
}

type AnalyzeJobTargetRequest struct {
	ExpectedInputVersion int `json:"expected_input_version"`
}

type ConfirmJobTargetRequest struct {
	ExpectedInputVersion    int                `json:"expected_input_version"`
	ExpectedAnalysisVersion int                `json:"expected_analysis_version"`
	Candidate               JobTargetCandidate `json:"candidate"`
}

type DiscardJobTargetRequest struct {
	ExpectedInputVersion int `json:"expected_input_version"`
}

type JobTargetOperationIntent struct {
	Method             string
	CanonicalPath      string
	Key                string
	PayloadFingerprint [sha256.Size]byte
}

type CreateJobTargetCommand struct {
	TargetID string
	Request  CreateJobTargetRequest
	Intent   JobTargetOperationIntent
}

type UpdateJobTargetCommand struct {
	TargetID string
	Request  UpdateJobTargetRequest
	Intent   JobTargetOperationIntent
}

type AnalyzeJobTargetCommand struct {
	TargetID string
	Request  AnalyzeJobTargetRequest
	Intent   JobTargetOperationIntent
	Lease    time.Duration
}

type ConfirmJobTargetCommand struct {
	TargetID string
	Request  ConfirmJobTargetRequest
	Intent   JobTargetOperationIntent
}

type DiscardJobTargetCommand struct {
	TargetID string
	Request  DiscardJobTargetRequest
	Intent   JobTargetOperationIntent
}

// JobTargetAnalysisClaim is a worker-only capability. Its opaque token and
// input snapshot must be checked by the repository on every terminal write.
type JobTargetAnalysisClaim struct {
	AttemptID       string
	TargetID        string
	OwnerUserID     string
	InputVersion    int
	AnalysisVersion int
	WorkerToken     string
	LeaseUntil      time.Time
	Input           JobTargetInput
	Intent          JobTargetOperationIntent
}

type JobTargetRepository interface {
	Create(
		context.Context,
		requestcontext.Actor,
		CreateJobTargetCommand,
	) (JobTarget, bool, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (JobTarget, error)
	Update(
		context.Context,
		requestcontext.Actor,
		UpdateJobTargetCommand,
	) (JobTarget, bool, error)
	ClaimAnalysis(
		context.Context,
		requestcontext.Actor,
		AnalyzeJobTargetCommand,
	) (JobTarget, JobTargetAnalysisClaim, bool, bool, error)
	CompleteAnalysis(
		context.Context,
		JobTargetAnalysisClaim,
		JobTargetCandidate,
	) (JobTarget, error)
	FailAnalysis(
		context.Context,
		JobTargetAnalysisClaim,
		string,
	) (JobTarget, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		ConfirmJobTargetCommand,
	) (JobTarget, bool, error)
	Discard(
		context.Context,
		requestcontext.Actor,
		DiscardJobTargetCommand,
	) (JobTarget, bool, error)
}

// JobTargetParser is the Preparation-owned Provider Port. Implementations
// receive data only and have no tool, URL-fetching, repository, or Actor access.
type JobTargetParser interface {
	ParseJobTarget(context.Context, JobTargetInput) (JobTargetCandidate, error)
}

type StableJobTargetParserError interface {
	error
	StableCategory() string
}

type JobTargetService struct {
	repository JobTargetRepository
	ids        ResourceIDGenerator
	parser     JobTargetParser
	catalog    scene.CatalogReader
	lease      time.Duration
}

func NewJobTargetService(
	repository JobTargetRepository,
	ids ResourceIDGenerator,
	parser JobTargetParser,
	catalog scene.CatalogReader,
) (*JobTargetService, error) {
	if repository == nil || ids == nil || parser == nil || catalog == nil {
		return nil, errors.New("preparation: job target dependency is required")
	}
	return &JobTargetService{
		repository: repository,
		ids:        ids,
		parser:     parser,
		catalog:    catalog,
		lease:      defaultJobTargetAnalysisLease,
	}, nil
}

func (s *JobTargetService) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request CreateJobTargetRequest,
) (JobTarget, bool, error) {
	if ctx == nil || !actor.Valid() || !validJobTargetInput(request.input()) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	intent, err := newJobTargetIntent(
		"POST",
		"/v1/job-targets",
		idempotencyKey,
		request,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	targetID, err := s.ids.NewID()
	if err != nil {
		return JobTarget{}, false, ErrJobTargetRepository
	}
	return s.repository.Create(ctx, actor, CreateJobTargetCommand{
		TargetID: targetID,
		Request:  request,
		Intent:   intent,
	})
}

func (s *JobTargetService) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
) (JobTarget, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(targetID) {
		return JobTarget{}, ErrJobTargetNotFound
	}
	return s.repository.Get(ctx, actor, targetID)
}

func (s *JobTargetService) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	idempotencyKey string,
	request UpdateJobTargetRequest,
) (JobTarget, bool, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(targetID) ||
		request.ExpectedInputVersion < 1 ||
		!validJobTargetInput(request.input()) {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	intent, err := newJobTargetIntent(
		"PUT",
		"/v1/job-targets/"+targetID,
		idempotencyKey,
		request,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	return s.repository.Update(ctx, actor, UpdateJobTargetCommand{
		TargetID: targetID,
		Request:  request,
		Intent:   intent,
	})
}

func (s *JobTargetService) Analyze(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	idempotencyKey string,
	request AnalyzeJobTargetRequest,
) (target JobTarget, replayed bool, err error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(targetID) ||
		request.ExpectedInputVersion < 1 {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	intent, err := newJobTargetIntent(
		"POST",
		"/v1/job-targets/"+targetID+"/analyses",
		idempotencyKey,
		request,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	target, claim, claimed, replayed, err := s.repository.ClaimAnalysis(
		ctx,
		actor,
		AnalyzeJobTargetCommand{
			TargetID: targetID,
			Request:  request,
			Intent:   intent,
			Lease:    s.lease,
		},
	)
	if err != nil {
		return target, replayed, err
	}
	if replayed && target.Stage == JobTargetStageAnalysisFailed {
		return target, true, ErrJobTargetAnalysisFailed
	}
	if replayed || !claimed {
		return target, replayed, nil
	}

	candidate, parseErr := s.parser.ParseJobTarget(ctx, claim.Input)
	persistContext, cancel := jobTargetPersistenceContext(ctx)
	defer cancel()
	if parseErr != nil {
		_, failErr := s.repository.FailAnalysis(
			persistContext,
			claim,
			jobTargetParserErrorCategory(parseErr),
		)
		if failErr != nil {
			return JobTarget{}, false, errors.Join(
				ErrJobTargetAnalysisFailed,
				parseErr,
				failErr,
			)
		}
		return JobTarget{}, false, errors.Join(
			ErrJobTargetAnalysisFailed,
			parseErr,
		)
	}
	if err := validateJobTargetCandidate(
		persistContext,
		candidate,
		claim.Input.Source,
		s.catalog,
	); err != nil {
		_, failErr := s.repository.FailAnalysis(
			persistContext,
			claim,
			"invalid_result",
		)
		if failErr != nil {
			return JobTarget{}, false, errors.Join(
				ErrJobTargetAnalysisFailed,
				err,
				failErr,
			)
		}
		return JobTarget{}, false, errors.Join(
			ErrJobTargetAnalysisFailed,
			err,
		)
	}
	target, err = s.repository.CompleteAnalysis(
		persistContext,
		claim,
		candidate,
	)
	return target, false, err
}

func (s *JobTargetService) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	idempotencyKey string,
	request ConfirmJobTargetRequest,
) (JobTarget, bool, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(targetID) ||
		request.ExpectedInputVersion < 1 ||
		request.ExpectedAnalysisVersion < 1 {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	if err := validateJobTargetCandidate(
		ctx,
		request.Candidate,
		request.Candidate.Source,
		s.catalog,
	); err != nil {
		return JobTarget{}, false, err
	}
	intent, err := newJobTargetIntent(
		"POST",
		"/v1/job-targets/"+targetID+"/confirmations",
		idempotencyKey,
		request,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	return s.repository.Confirm(ctx, actor, ConfirmJobTargetCommand{
		TargetID: targetID,
		Request:  request,
		Intent:   intent,
	})
}

func (s *JobTargetService) Discard(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
	idempotencyKey string,
	request DiscardJobTargetRequest,
) (JobTarget, bool, error) {
	if ctx == nil || !actor.Valid() || !validResourceIdentifier(targetID) ||
		request.ExpectedInputVersion < 1 {
		return JobTarget{}, false, ErrJobTargetInvalid
	}
	intent, err := newJobTargetIntent(
		"POST",
		"/v1/job-targets/"+targetID+"/discard",
		idempotencyKey,
		request,
	)
	if err != nil {
		return JobTarget{}, false, err
	}
	return s.repository.Discard(ctx, actor, DiscardJobTargetCommand{
		TargetID: targetID,
		Request:  request,
		Intent:   intent,
	})
}

func (r CreateJobTargetRequest) input() JobTargetInput {
	return JobTargetInput{
		Source:              r.Source,
		JobTitle:            r.JobTitle,
		JobDescription:      r.JobDescription,
		Company:             r.Company,
		Seniority:           r.Seniority,
		CandidateBackground: r.CandidateBackground,
		PracticeFocus:       r.PracticeFocus,
	}
}

func (r CreateJobTargetRequest) Input() JobTargetInput { return r.input() }

func (r UpdateJobTargetRequest) input() JobTargetInput {
	return JobTargetInput{
		Source:              r.Source,
		JobTitle:            r.JobTitle,
		JobDescription:      r.JobDescription,
		Company:             r.Company,
		Seniority:           r.Seniority,
		CandidateBackground: r.CandidateBackground,
		PracticeFocus:       r.PracticeFocus,
	}
}

func (r UpdateJobTargetRequest) Input() JobTargetInput { return r.input() }

func newJobTargetIntent(
	method string,
	path string,
	key string,
	payload any,
) (JobTargetOperationIntent, error) {
	if (method != "POST" && method != "PUT") ||
		!validCanonicalPath(path) ||
		!validIdempotencyKey(key) {
		return JobTargetOperationIntent{}, ErrJobTargetInvalid
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return JobTargetOperationIntent{}, ErrJobTargetInvalid
	}
	return JobTargetOperationIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(encoded),
	}, nil
}

func validJobTargetInput(input JobTargetInput) bool {
	switch input.Source {
	case JobTargetSourceJobDescription:
		if !validJobTargetText(
			input.JobDescription,
			maxJobTargetDescriptionBytes,
			true,
		) {
			return false
		}
	case JobTargetSourceQuickStart:
		if input.JobDescription != "" ||
			!validJobTargetText(
				input.JobTitle,
				maxJobTargetTitleBytes,
				true,
			) {
			return false
		}
	default:
		return false
	}
	return validJobTargetText(
		input.JobTitle,
		maxJobTargetTitleBytes,
		false,
	) &&
		validJobTargetText(
			input.Company,
			maxJobTargetCompanyBytes,
			false,
		) &&
		validJobTargetText(
			input.Seniority,
			maxJobTargetSeniorityBytes,
			false,
		) &&
		validJobTargetText(
			input.CandidateBackground,
			maxJobTargetBackgroundBytes,
			false,
		) &&
		validJobTargetText(
			input.PracticeFocus,
			maxJobTargetFocusBytes,
			false,
		)
}

func ValidJobTargetInput(input JobTargetInput) bool {
	return validJobTargetInput(input)
}

func validJobTargetText(value string, maxBytes int, required bool) bool {
	if value == "" {
		return !required
	}
	return utf8.ValidString(value) &&
		len(value) <= maxBytes &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validateJobTargetCandidate(
	ctx context.Context,
	candidate JobTargetCandidate,
	source JobTargetSource,
	catalog scene.CatalogReader,
) error {
	if ctx == nil || catalog == nil ||
		!validJobTargetCandidateShape(candidate, source) {
		return ErrJobTargetInvalid
	}
	recommendation := candidate.CatalogRecommendation
	selection, err := catalog.ResolveSelection(
		ctx,
		recommendation.SceneID,
		recommendation.SceneVersion,
		append([]string(nil), recommendation.SelectedRoleIDs...),
		recommendation.PracticeOptionID,
	)
	if err != nil {
		return errors.Join(ErrJobTargetInvalid, err)
	}
	if selection.Scene.Experience == scene.PracticeExperienceInterview &&
		len(selection.SelectedRoleIDs) != 1 {
		return ErrJobTargetInvalid
	}
	return nil
}

func validJobTargetCandidateShape(
	candidate JobTargetCandidate,
	source JobTargetSource,
) bool {
	if candidate.Source != source ||
		!validJobTargetText(
			candidate.JobTitle,
			maxJobTargetTitleBytes,
			true,
		) ||
		!validJobTargetText(
			candidate.Seniority,
			maxJobTargetSeniorityBytes,
			true,
		) ||
		!validJobTargetText(
			candidate.ScopeNotice,
			maxJobTargetNoticeBytes,
			true,
		) ||
		!validJobTargetCandidateList(candidate.Responsibilities) ||
		!validJobTargetCandidateList(candidate.CoreSkills) ||
		!validJobTargetCandidateList(candidate.CommunicationFocus) ||
		!validJobTargetCandidateList(candidate.PracticeGoals) {
		return false
	}
	if source == JobTargetSourceQuickStart && !candidate.GeneralAdviceOnly {
		return false
	}
	if source == JobTargetSourceJobDescription && candidate.GeneralAdviceOnly {
		return false
	}
	recommendation := candidate.CatalogRecommendation
	if recommendation.SceneVersion < 1 {
		return false
	}
	return validResourceIdentifier(
		recommendation.SceneID,
	) &&
		validResourceIdentifier(recommendation.PracticeOptionID) &&
		len(recommendation.SelectedRoleIDs) > 0 &&
		validJobTargetCandidateJSONSize(candidate)
}

func ValidJobTargetCandidateShape(
	candidate JobTargetCandidate,
	source JobTargetSource,
) bool {
	return validJobTargetCandidateShape(candidate, source)
}

func validJobTargetCandidateList(values []string) bool {
	if len(values) == 0 || len(values) > maxJobTargetCandidateItems {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !utf8.ValidString(value) ||
			value == "" ||
			strings.ContainsRune(value, '\x00') ||
			strings.TrimSpace(value) != value ||
			utf8.RuneCountInString(value) >
				maxJobTargetCandidateItemCharacters {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validJobTargetCandidateJSONSize(
	candidate JobTargetCandidate,
) bool {
	encoded, err := json.Marshal(candidate)
	return err == nil &&
		len(encoded) <= maxJobTargetCandidateJSONBytes
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

func jobTargetPersistenceContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		jobTargetPersistenceTimeout,
	)
}

func cloneJobTargetCandidate(source JobTargetCandidate) JobTargetCandidate {
	result := source
	result.Responsibilities = append([]string(nil), source.Responsibilities...)
	result.CoreSkills = append([]string(nil), source.CoreSkills...)
	result.CommunicationFocus = append(
		[]string(nil),
		source.CommunicationFocus...,
	)
	result.PracticeGoals = append([]string(nil), source.PracticeGoals...)
	result.CatalogRecommendation.SelectedRoleIDs = append(
		[]string(nil),
		source.CatalogRecommendation.SelectedRoleIDs...,
	)
	return result
}

func CloneJobTargetCandidate(source JobTargetCandidate) JobTargetCandidate {
	return cloneJobTargetCandidate(source)
}
