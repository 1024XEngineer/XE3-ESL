package bootstrap

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	inputvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/qianwen"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	voiceQuestionObjective = "targeted-english-practice"
)

// VoicePorts are application capabilities supplied by the owning modules.
// Repository types remain confined to their module and the composition root.
type VoicePorts struct {
	ConversationStore practiceinput.VoiceRoundStore
	Practice          practicevoice.PracticePort
	Sessions          practicevoice.SessionPort
	Questions         practicevoice.QuestionPort
	Checkpoints       practicevoice.CheckpointPort
	SpeechFeedback    practicevoice.TurnFeedbackPort
}

type VoiceConfiguration struct {
	Recognizer                ai.StreamingSpeechRecognizer
	Synthesizer               ai.SpeechSynthesizer
	TemporaryAudio            practiceinput.TemporaryAudioVault
	Ports                     VoicePorts
	Recordings                practiceinput.VoiceRecordingLifecycle
	ObjectStore               objectstore.Store
	AgentVoiceInputEnabled    bool
	ScratchDirectory          string
	ObjectReadAllowedHosts    []string
	AudioStagedTTL            time.Duration
	AudioUploadLease          time.Duration
	ASRLease                  time.Duration
	AudioReadTimeout          time.Duration
	ReviewHistoryCursorKey    []byte
	SpeechFeedbackCoordinator *evaluation.SpeechFeedbackCoordinator
}

type AgentImageConfiguration struct {
	ObjectStore objectstore.Store
	StagedTTL   time.Duration
	UploadLease time.Duration
}

// NewSpeechRecognizer is the server-side ASR registration boundary. Production
// never silently substitutes a Fake or another provider.
func NewSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (ai.StreamingSpeechRecognizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech recognition provider is not registered",
		)
	}
	return qianwen.NewRecognizer(
		qianwen.ASRConfig{
			BaseURL: configuration.BaseURL,
			Model:   configuration.Model,
			Timeout: configuration.Timeout,
		},
		configuration.APIKey.Reveal(),
	)
}

// NewSpeechSynthesizer is the server-side TTS registration boundary.
func NewSpeechSynthesizer(
	configuration config.SpeechSynthesisConfig,
) (ai.SpeechSynthesizer, error) {
	if configuration.Provider != config.SpeechProviderQianwen {
		return nil, errors.New(
			"bootstrap: speech synthesis provider is not registered",
		)
	}
	return qianwen.NewSynthesizer(
		qianwen.TTSConfig{
			BaseURL:       configuration.BaseURL,
			Model:         configuration.Model,
			Voice:         configuration.Voice,
			LanguageHint:  configuration.LanguageHint,
			Timeout:       configuration.Timeout,
			TempDirectory: configuration.TempDirectory,
		},
		configuration.APIKey.Reveal(),
	)
}

func buildVoiceApplication(
	configuration VoiceConfiguration,
) (*practicevoice.SessionApplication, error) {
	ports := configuration.Ports
	if configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.TemporaryAudio == nil ||
		ports.ConversationStore == nil ||
		ports.Practice == nil ||
		ports.Sessions == nil ||
		ports.Questions == nil ||
		ports.Checkpoints == nil {
		return nil, errors.New("bootstrap: voice dependencies are required")
	}
	conversations, err := practiceinput.NewVoiceRoundServiceWithRecordings(
		ports.ConversationStore,
		configuration.TemporaryAudio,
		configuration.Recognizer,
		configuration.Synthesizer,
		configuration.Recordings,
	)
	if err != nil {
		return nil, err
	}
	feedbackPorts := make([]practicevoice.TurnFeedbackPort, 0, 1)
	if ports.SpeechFeedback != nil {
		feedbackPorts = append(feedbackPorts, ports.SpeechFeedback)
	}
	orchestrator, err := practicevoice.NewRoundOrchestrator(
		conversations,
		ports.Practice,
		feedbackPorts...,
	)
	if err != nil {
		return nil, err
	}
	return practicevoice.NewSessionApplication(
		ports.Sessions,
		ports.Questions,
		ports.Checkpoints,
		orchestrator,
	)
}

func buildProductionVoiceApplication(
	database *pgxpool.Pool,
	textGenerator ai.TextGenerator,
	configuration VoiceConfiguration,
) (
	*practicevoice.SessionApplication,
	*practicevoice.SameQuestionRetryApplication,
	*practiceinput.AudioAssetService,
	error,
) {
	if database == nil || textGenerator == nil ||
		configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.ASRLease <= 0 {
		return nil, nil, nil,
			errors.New("bootstrap: voice dependencies are required")
	}

	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, nil, nil, err
	}
	practiceApplication, err := practicevoice.NewApplication(
		practiceRepository,
		"speakup.user",
	)
	if err != nil {
		return nil, nil, nil, err
	}
	conversationRepository := practiceRepository
	conversationStore := &voiceConversationStore{
		repository: conversationRepository,
		recordings: conversationRepository,
		asrLease:   configuration.ASRLease,
	}
	var audioAssets *practiceinput.AudioAssetService
	if configuration.ObjectStore != nil {
		audioRepository, repositoryErr :=
			practicepostgres.NewAudioAssetRepository(database)
		if repositoryErr != nil {
			return nil, nil, nil, repositoryErr
		}
		audioAssets, err = practiceinput.NewAudioAssetService(
			audioRepository,
			configuration.ObjectStore,
			practiceinput.SecureAudioAssetIDGenerator{},
			practiceinput.NewAudioAssetSystemClock(),
			audioRepository,
			configuration.AudioStagedTTL,
		)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	var recordingLifecycle practiceinput.VoiceRecordingLifecycle
	if audioAssets != nil {
		recordingLifecycle = audioAssets
	}
	conversationService, err :=
		practiceinput.NewVoiceRoundServiceWithRecordings(
			conversationStore,
			configuration.TemporaryAudio,
			configuration.Recognizer,
			configuration.Synthesizer,
			recordingLifecycle,
		)
	if err != nil {
		return nil, nil, nil, err
	}
	retryTurnService, err := practiceinput.NewRetryTurnService(
		conversationRepository,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	retryPracticeApplication, err := practice.NewRetryTurnApplication(
		practiceRepository,
		"speakup.user",
	)
	if err != nil {
		return nil, nil, nil, err
	}
	retryApplication, err := practicevoice.NewSameQuestionRetryApplication(
		&voiceRetryTurnAdapter{service: retryTurnService},
		&voiceRetryPracticeAdapter{
			application: retryPracticeApplication,
		},
		conversationService,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	practiceAdapter := &voicePracticeAdapter{
		repository: practiceRepository,
	}
	questionAdapter := &voiceQuestionAdapter{
		repository: conversationRepository,
		generator:  textGenerator,
		speech:     conversationService,
	}
	checkpointAdapter := &voiceCheckpointAdapter{
		repository:  conversationRepository,
		audioAssets: audioAssets,
	}
	if configuration.SpeechFeedbackCoordinator != nil {
		checkpointAdapter.feedback = configuration.SpeechFeedbackCoordinator
	}
	feedbackPorts := make([]practicevoice.TurnFeedbackPort, 0, 1)
	if configuration.SpeechFeedbackCoordinator != nil {
		feedbackPorts = append(
			feedbackPorts,
			&voiceSpeechFeedbackAdapter{
				coordinator: configuration.SpeechFeedbackCoordinator,
			},
		)
	}
	orchestrator, err := practicevoice.NewRoundOrchestrator(
		conversationService,
		practiceApplication,
		feedbackPorts...,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	application, err := practicevoice.NewSessionApplication(
		practiceAdapter,
		questionAdapter,
		checkpointAdapter,
		orchestrator,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return application, retryApplication, audioAssets, nil
}

type voicePracticeAdapter struct {
	repository voiceContextRepository
}

type voiceContextRepository interface {
	GetSession(
		context.Context,
		practice.Actor,
		string,
	) (practice.Session, error)
	GetSessionSnapshot(
		context.Context,
		practice.Actor,
		string,
	) (practice.SessionSnapshot, error)
	ReplayVoiceStart(
		context.Context,
		practice.Actor,
		practice.IdempotencyIntent,
	) (practice.SessionBootstrap, bool, error)
	ActivateSession(
		context.Context,
		practice.Actor,
		string,
		string,
		practice.IdempotencyIntent,
	) (practice.SessionBootstrap, error)
}

func (adapter *voicePracticeAdapter) Start(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
) (practicevoice.Session, error) {
	if adapter == nil || adapter.repository == nil ||
		!actor.Valid() ||
		strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return practicevoice.Session{}, practicevoice.ErrInvalidRequest
	}
	practiceActor := practiceActor(actor)
	intent := practice.IdempotencyIntent{
		Method: "POST",
		CanonicalPath: "/v1/practice-sessions/" + sessionID +
			"/voice-activation",
		Key:                idempotencyKey,
		PayloadFingerprint: sha256.Sum256(nil),
	}
	replayed, found, err := adapter.repository.ReplayVoiceStart(
		ctx,
		practiceActor,
		intent,
	)
	if err != nil {
		return practicevoice.Session{}, mapPracticeError(err)
	}
	if found {
		if replayed.Session.ID != sessionID {
			return practicevoice.Session{}, practicevoice.ErrIdempotencyConflict
		}
		return mapContextPracticeSession(replayed, actor.UserID)
	}
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return practicevoice.Session{}, mapPracticeError(err)
	}
	if session.ID != sessionID || strings.TrimSpace(session.PlanID) == "" {
		return practicevoice.Session{}, practicevoice.ErrInvalidContext
	}
	activated, err := adapter.repository.ActivateSession(
		ctx,
		practiceActor,
		sessionID,
		session.PlanID,
		intent,
	)
	if err != nil {
		return practicevoice.Session{}, mapPracticeError(err)
	}
	if activated.Session.ID != sessionID ||
		activated.Session.PlanID != session.PlanID {
		return practicevoice.Session{}, practicevoice.ErrInvalidContext
	}
	return mapContextPracticeSession(activated, actor.UserID)
}

func (adapter *voicePracticeAdapter) GetByID(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (practicevoice.Session, error) {
	if adapter == nil || adapter.repository == nil || !actor.Valid() ||
		strings.TrimSpace(sessionID) == "" {
		return practicevoice.Session{}, practicevoice.ErrInvalidRequest
	}
	practiceActor := practiceActor(actor)
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return practicevoice.Session{}, mapPracticeError(err)
	}
	snapshot, err := adapter.repository.GetSessionSnapshot(
		ctx,
		practiceActor,
		sessionID,
	)
	if err != nil {
		return practicevoice.Session{}, mapPracticeError(err)
	}
	return mapContextPracticeSession(
		practice.SessionBootstrap{
			Session:  session,
			Snapshot: snapshot,
		},
		actor.UserID,
	)
}

func practiceActor(actor requestcontext.Actor) practice.Actor {
	return practice.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapContextPracticeSession(
	bootstrap practice.SessionBootstrap,
	actorUserID string,
) (practicevoice.Session, error) {
	session := bootstrap.Session
	snapshot := bootstrap.Snapshot
	selection := snapshot.SceneSelection
	if session.ID == "" ||
		session.PlanID == "" ||
		session.PlanRevision < 1 ||
		snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != session.ID ||
		snapshot.PlanRevision != session.PlanRevision ||
		snapshot.SceneFamily != session.SceneFamily ||
		snapshot.SceneModel != session.SceneModel ||
		selection.Scene.ID == "" ||
		selection.Scene.Version < 1 ||
		selection.Scene.Family != session.SceneFamily ||
		selection.Scene.Model != session.SceneModel ||
		len(selection.SelectedRoleIDs) == 0 {
		return practicevoice.Session{}, practicevoice.ErrInvalidContext
	}
	result := practicevoice.Session{
		ID:                      session.ID,
		PlanID:                  session.PlanID,
		SceneID:                 selection.Scene.ID,
		SceneVersion:            selection.Scene.Version,
		SceneFamily:             string(snapshot.SceneFamily),
		SceneModel:              string(snapshot.SceneModel),
		Prompt:                  cloneVoiceScenePrompt(selection.Scene.Prompt),
		SessionVersion:          session.Version,
		EffectiveTurns:          session.EffectiveTurns,
		TurnLimit:               snapshot.SessionPolicy.MaxEffectiveTurns,
		MaxFollowUpsPerQuestion: snapshot.SessionPolicy.MaxFollowUpsPerQuestion,
		Completed: session.Status ==
			practice.SessionCompleted,
		Status: string(session.Status),
	}
	participantIDs := make(map[string]struct{}, len(snapshot.Participants))
	participantOrders := make(map[int]struct{}, len(snapshot.Participants))
	facilitatorRoles := make(map[string]struct{})
	selectedRoles := make(map[string]struct{}, len(selection.SelectedRoleIDs))
	for _, roleID := range selection.SelectedRoleIDs {
		if strings.TrimSpace(roleID) == "" {
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
		if _, duplicate := selectedRoles[roleID]; duplicate {
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
		selectedRoles[roleID] = struct{}{}
	}
	facilitatorOrder := 0
	for _, participant := range snapshot.Participants {
		if participant.ID == "" ||
			participant.SessionID != session.ID ||
			participant.Order < 1 {
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
		if _, duplicate := participantIDs[participant.ID]; duplicate {
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
		if _, duplicate := participantOrders[participant.Order]; duplicate {
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
		participantIDs[participant.ID] = struct{}{}
		participantOrders[participant.Order] = struct{}{}
		switch participant.Role {
		case "FACILITATOR":
			if participant.SubjectRef.Namespace != "speakup.role" ||
				participant.SubjectRef.SubjectID !=
					participant.RoleDefinitionID ||
				participant.RoleDefinitionID == "" ||
				participant.RoleSnapshot == nil ||
				participant.RoleSnapshot.ID !=
					participant.RoleDefinitionID {
				return practicevoice.Session{}, practicevoice.ErrInvalidContext
			}
			if _, selected := selectedRoles[participant.RoleDefinitionID]; !selected {
				return practicevoice.Session{}, practicevoice.ErrInvalidContext
			}
			if _, duplicate := facilitatorRoles[participant.RoleDefinitionID]; duplicate {
				return practicevoice.Session{}, practicevoice.ErrInvalidContext
			}
			facilitatorRoles[participant.RoleDefinitionID] = struct{}{}
			if result.FacilitatorParticipantID == "" ||
				participant.Order < facilitatorOrder {
				result.FacilitatorParticipantID = participant.ID
				facilitatorOrder = participant.Order
			}
		case "LEARNER":
			if result.LearnerParticipantID != "" ||
				participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != actorUserID ||
				participant.RoleDefinitionID != "" ||
				participant.RoleSnapshot != nil {
				return practicevoice.Session{}, practicevoice.ErrNotFound
			}
			result.LearnerParticipantID = participant.ID
		default:
			return practicevoice.Session{}, practicevoice.ErrInvalidContext
		}
	}
	if result.FacilitatorParticipantID == "" ||
		result.LearnerParticipantID == "" ||
		len(facilitatorRoles) != len(selectedRoles) ||
		result.TurnLimit < 1 ||
		result.TurnLimit > 14 ||
		result.EffectiveTurns < 0 ||
		result.EffectiveTurns > result.TurnLimit ||
		(result.Status == string(
			practice.SessionCompleted,
		)) != result.Completed ||
		!validMappedContextVoiceLifecycle(session, result.TurnLimit) {
		return practicevoice.Session{}, practicevoice.ErrInvalidContext
	}
	return result, nil
}

func cloneVoiceScenePrompt(source scene.ScenePrompt) scene.ScenePrompt {
	result := source
	result.FocusAreas = slices.Clone(source.FocusAreas)
	result.TurnBlueprints = slices.Clone(source.TurnBlueprints)
	return result
}

func validMappedContextVoiceLifecycle(
	session practice.Session,
	turnLimit int,
) bool {
	if turnLimit < 1 || turnLimit > 14 ||
		session.EffectiveTurns < 0 ||
		session.EffectiveTurns > turnLimit {
		return false
	}
	switch session.Status {
	case practice.SessionStarting:
		return session.EffectiveTurns == 0 &&
			session.StartedAt == nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionInProgress,
		practice.SessionPaused:
		return session.EffectiveTurns < turnLimit &&
			session.StartedAt != nil &&
			session.EndedAt == nil &&
			session.EndReason == ""
	case practice.SessionCompleted:
		return session.EffectiveTurns > 0 &&
			session.StartedAt != nil &&
			session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	case practice.SessionEndedEarly:
		return session.EffectiveTurns < turnLimit &&
			session.StartedAt != nil &&
			session.EndedAt != nil &&
			strings.TrimSpace(session.EndReason) != ""
	default:
		return false
	}
}

func mapPracticeError(err error) error {
	switch {
	case errors.Is(err, practice.ErrInvalidArgument):
		return practicevoice.ErrInvalidRequest
	case errors.Is(err, practice.ErrNotFound):
		return practicevoice.ErrNotFound
	case errors.Is(err, practice.ErrIdempotencyConflict):
		return practicevoice.ErrIdempotencyConflict
	case errors.Is(err, practice.ErrConflict),
		errors.Is(err, practice.ErrSessionCompleted):
		return practicevoice.ErrConflict
	default:
		return err
	}
}

type voiceRetryTurnAdapter struct {
	service *practiceinput.RetryTurnService
}

func (adapter *voiceRetryTurnAdapter) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	retryTurnID string,
) (practiceinput.RetryTurnDraft, error) {
	if adapter == nil || adapter.service == nil {
		return practiceinput.RetryTurnDraft{}, practicevoice.ErrInvalidContext
	}
	draft, err := adapter.service.Get(ctx, actor, retryTurnID)
	switch {
	case errors.Is(err, practiceinput.ErrRetryTurnInvalid):
		return practiceinput.RetryTurnDraft{}, practicevoice.ErrInvalidRequest
	case errors.Is(err, practiceinput.ErrRetryTurnNotFound):
		return practiceinput.RetryTurnDraft{}, practicevoice.ErrNotFound
	case errors.Is(err, practiceinput.ErrRetryTurnConflict),
		errors.Is(err, practiceinput.ErrRetryTurnNotReady):
		return practiceinput.RetryTurnDraft{}, practicevoice.ErrConflict
	default:
		return draft, err
	}
}

type voiceRetryPracticeAdapter struct {
	application *practice.RetryTurnApplication
}

func (adapter *voiceRetryPracticeAdapter) ResolveAuthorizedParticipant(
	ctx context.Context,
	actor requestcontext.Actor,
	retryRequestID string,
) (string, error) {
	if adapter == nil || adapter.application == nil {
		return "", practicevoice.ErrInvalidContext
	}
	participantID, err := adapter.application.ResolveAuthorizedParticipant(
		ctx,
		actor,
		retryRequestID,
	)
	switch {
	case errors.Is(err, practice.ErrRetryTurnInvalid):
		return "", practicevoice.ErrInvalidRequest
	case errors.Is(err, practice.ErrRetryTurnNotAvailable):
		return "", practicevoice.ErrNotFound
	case errors.Is(err, practice.ErrRetryTurnConflict):
		return "", practicevoice.ErrConflict
	default:
		return participantID, err
	}
}

type voiceConversationStore struct {
	repository practiceinput.PersistenceStore
	recordings practiceinput.RecordingConfirmationStore
	asrLease   time.Duration
}

func (store *voiceConversationStore) GetVoiceQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	questionID string,
) (practice.Question, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil || question.SessionID != sessionID {
		if err == nil {
			return practice.Question{},
				practiceinput.ErrVoiceRoundNotFound
		}
		return practice.Question{}, mapConversationError(err)
	}
	return mapVoiceQuestion(question), nil
}

func (store *voiceConversationStore) ReserveTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.ReserveTranscriptionCommand,
) (practiceinput.TranscriptionReservation, error) {
	reservation, err := store.repository.ReserveTranscription(
		ctx,
		conversationActor(actor),
		practiceinput.StoreReserveTranscriptionCommand{
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: command.RespondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        command.InputFingerprint,
			LeaseDuration:           store.asrLease,
		},
	)
	if err != nil {
		return practiceinput.TranscriptionReservation{}, mapConversationError(err)
	}
	result := practiceinput.TranscriptionReservation{
		ID: reservation.ID,
	}
	switch reservation.Status {
	case practiceinput.StoredTranscriptionCompleted:
		result.Status = practiceinput.TranscriptionCompleted
		candidate, candidateErr := store.GetTranscriptionCandidate(
			ctx,
			actor,
			reservation.CandidateID,
		)
		if candidateErr != nil {
			return practiceinput.TranscriptionReservation{}, candidateErr
		}
		result.Candidate = candidate
	case practiceinput.StoredTranscriptionProcessing:
		if reservation.LeaseAcquired {
			result.Status = practiceinput.TranscriptionReserved
			result.LeaseToken = transcriptionLeaseToken(
				actor.UserID,
				reservation,
			)
		} else {
			result.Status = practiceinput.TranscriptionProcessing
		}
	default:
		return practiceinput.TranscriptionReservation{},
			practiceinput.ErrVoiceRoundConflict
	}
	return result, nil
}

func (store *voiceConversationStore) CompleteTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.CompleteTranscriptionCommand,
) (practiceinput.TranscriptionCandidate, error) {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return practiceinput.TranscriptionCandidate{}, err
	}
	candidate, err := store.repository.CompleteTranscription(
		ctx,
		job,
		practiceinput.StoreCompleteTranscriptionCommand{
			TranscriptID:      command.TranscriptID,
			EvidenceVersion:   command.EvidenceVersion,
			Provider:          command.Provider,
			Model:             command.Model,
			ProviderRequestID: command.ProviderRequestID,
			Text:              command.Transcript,
		},
	)
	if err != nil {
		return practiceinput.TranscriptionCandidate{}, mapConversationError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *voiceConversationStore) FailTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.FailTranscriptionCommand,
) error {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return err
	}
	return mapConversationError(store.repository.FailTranscription(
		ctx,
		job,
		practiceinput.ProcessingFailure{
			Code:              string(command.Attempt.Kind),
			Retryable:         command.Attempt.Retryable,
			ProviderRequestID: command.Attempt.RequestID,
			Duration:          command.Attempt.Duration,
		},
	))
}

func (store *voiceConversationStore) transcriptionJob(
	ctx context.Context,
	actor requestcontext.Actor,
	reservationID string,
	leaseToken string,
) (practiceinput.JobContext, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return practiceinput.JobContext{},
			practiceinput.ErrVoiceRoundConflict
	}
	reservation, err := store.repository.GetReservation(
		ctx,
		conversationActor(actor),
		reservationID,
	)
	if err != nil {
		return practiceinput.JobContext{}, mapConversationError(err)
	}
	expectedToken := transcriptionLeaseToken(actor.UserID, reservation)
	if subtle.ConstantTimeCompare(
		[]byte(leaseToken),
		[]byte(expectedToken),
	) != 1 {
		return practiceinput.JobContext{},
			practiceinput.ErrVoiceRoundConflict
	}
	return practiceinput.JobContext{
		OwnerUserID:        actor.UserID,
		DeletionGeneration: reservation.DeletionGeneration,
		ReservationID:      reservation.ID,
		FencingToken:       reservation.FencingToken,
	}, nil
}

func transcriptionLeaseToken(
	ownerUserID string,
	reservation practiceinput.StoredTranscriptionReservation,
) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(
		digest,
		"practiceinput.asr-lease/v1\x00%s\x00%s\x00%d\x00%d",
		ownerUserID,
		reservation.ID,
		reservation.DeletionGeneration,
		reservation.FencingToken,
	)
	return hex.EncodeToString(digest.Sum(nil))
}

func (store *voiceConversationStore) GetTranscriptionCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidateID string,
) (practiceinput.TranscriptionCandidate, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		candidateID,
	)
	if err != nil {
		return practiceinput.TranscriptionCandidate{}, mapConversationError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *voiceConversationStore) mapCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate practiceinput.StoredTranscriptCandidate,
) (practiceinput.TranscriptionCandidate, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		candidate.QuestionID,
	)
	if err != nil {
		return practiceinput.TranscriptionCandidate{}, mapConversationError(err)
	}
	return practiceinput.TranscriptionCandidate{
		ID:                      candidate.ID,
		ReservationID:           candidate.ReservationID,
		SessionID:               candidate.SessionID,
		QuestionID:              candidate.QuestionID,
		QuestionSpeakerID:       question.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
		RespondentParticipantID: candidate.RespondentParticipantID,
		TranscriptID:            candidate.TranscriptID,
		EvidenceVersion:         candidate.EvidenceVersion,
		Transcript:              candidate.Text,
		Provider:                candidate.Provider,
		Model:                   candidate.Model,
		ProviderRequestID:       candidate.ProviderRequestID,
		CreatedAt:               candidate.CreatedAt,
	}, nil
}

func (store *voiceConversationStore) ReserveConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.ReserveConfirmationCommand,
) (practice.Turn, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, mapConversationError(err)
	}
	turn, err := store.repository.ConfirmTurn(
		ctx,
		conversationActor(actor),
		practiceinput.ConfirmTurnCommand{
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ConfirmedText:   candidate.Text,
			IdempotencyKey:  command.IdempotencyKey,
			RetryTurnID:     command.RetryTurnID,
		},
	)
	if err != nil {
		return practice.Turn{}, mapConversationError(err)
	}
	return mapVoiceTurnWithCandidate(turn, candidate)
}

func (store *voiceConversationStore) ReserveRecordingConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command practiceinput.ConfirmVoiceTurnCommand,
	uploadRequestID string,
) (practiceinput.VoiceRecordingConfirmation, error) {
	if store.recordings == nil {
		return practiceinput.VoiceRecordingConfirmation{},
			practiceinput.ErrVoiceRoundInvalid
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return practiceinput.VoiceRecordingConfirmation{},
			mapConversationError(err)
	}
	persisted, err :=
		store.recordings.ConfirmTurnWithRecording(
			ctx,
			conversationActor(actor),
			practiceinput.ConfirmTurnCommand{
				CandidateID:     candidate.ID,
				EvidenceVersion: candidate.EvidenceVersion,
				ConfirmedText:   candidate.Text,
				IdempotencyKey:  command.IdempotencyKey,
				RetryTurnID:     command.RetryTurnID,
			},
			uploadRequestID,
		)
	if err != nil {
		return practiceinput.VoiceRecordingConfirmation{},
			mapRecordingConfirmationError(err)
	}
	mapped, err := mapVoiceTurnWithCandidate(persisted.Turn, candidate)
	if err != nil {
		return practiceinput.VoiceRecordingConfirmation{}, err
	}
	mapped.AudioAssetID = persisted.AudioAssetID
	return practiceinput.VoiceRecordingConfirmation{
		Turn:             mapped,
		RecordingDeleted: persisted.RecordingDeleted,
	}, nil
}

func mapRecordingConfirmationError(err error) error {
	switch {
	case errors.Is(err, practiceinput.ErrAudioAssetNotFound),
		errors.Is(err, practiceinput.ErrAudioAssetForbidden),
		errors.Is(err, practiceinput.ErrAudioAssetAlreadyBound),
		errors.Is(err, practiceinput.ErrAudioAssetInvalidTransition),
		errors.Is(err, practiceinput.ErrAudioAssetUploadTerminated):
		// A recording that cleanup removed, another Turn already bound, or
		// otherwise left the confirmable state is a terminal request conflict.
		return practicevoice.ErrConflict
	case errors.Is(err, practiceinput.ErrAudioAssetConcurrentUpdate):
		// A lost optimistic update can be retried safely with the same
		// idempotency key. The HTTP boundary exposes resource_processing,
		// Retry-After: 1, and retryable=true for this sentinel.
		return practiceinput.ErrVoiceRoundProcessing
	default:
		return mapConversationError(err)
	}
}

func conversationActor(
	actor requestcontext.Actor,
) practiceinput.Actor {
	return practiceinput.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapConversationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, practiceinput.ErrPersistenceInvalid):
		return practiceinput.ErrVoiceRoundInvalid
	case errors.Is(err, practiceinput.ErrPersistenceNotFound):
		return practiceinput.ErrVoiceRoundNotFound
	case errors.Is(err, practiceinput.ErrPersistenceConflict):
		return practiceinput.ErrVoiceRoundConflict
	case errors.Is(err, practiceinput.ErrActorDeleted):
		return practicevoice.ErrNotFound
	default:
		return err
	}
}

func mapVoiceQuestion(
	question practice.Question,
) practice.Question {
	return practice.Question{
		ID:                      question.ID,
		SessionID:               question.SessionID,
		Type:                    question.Type,
		ParentQuestionID:        question.ParentQuestionID,
		Content:                 question.Content,
		SpeakerParticipantID:    question.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
	}
}

func mapVoiceTurn(
	turn practice.Turn,
) practice.Turn {
	turn.AddresseeParticipantIDs = slices.Clone(
		turn.AddresseeParticipantIDs,
	)
	return turn
}

func mapVoiceTurnWithCandidate(
	turn practice.Turn,
	candidate practiceinput.StoredTranscriptCandidate,
) (practice.Turn, error) {
	if candidate.ID == "" ||
		candidate.ID != turn.CandidateID ||
		candidate.SessionID != turn.SessionID ||
		candidate.QuestionID != turn.QuestionID ||
		candidate.RespondentParticipantID != turn.RespondentParticipantID ||
		candidate.EvidenceVersion != turn.EvidenceVersion ||
		candidate.Text != turn.AnswerText ||
		candidate.TranscriptID == "" {
		return practice.Turn{},
			practiceinput.ErrVoiceRoundConflict
	}
	mapped := mapVoiceTurn(turn)
	mapped.TranscriptID = candidate.TranscriptID
	return mapped, nil
}

type voiceQuestionAdapter struct {
	repository practiceinput.PersistenceStore
	generator  ai.TextGenerator
	speech     *practiceinput.VoiceRoundService
}

type voiceSessionQuestionLister interface {
	ListSessionQuestions(
		context.Context,
		practiceinput.Actor,
		string,
	) ([]practice.Question, error)
}

type generatedInterviewQuestion struct {
	QuestionType string `json:"question_type"`
	Content      string `json:"content"`
}

func (adapter *voiceQuestionAdapter) EnsureQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	session practicevoice.Session,
	sequence int,
) (practice.Question, error) {
	questionID := fmt.Sprintf(
		"%s_%d",
		stableVoiceID("voice_question", session.ID),
		sequence,
	)
	conversationActor := conversationActor(actor)
	existing, err := adapter.repository.GetQuestion(
		ctx,
		conversationActor,
		questionID,
	)
	if err == nil {
		return mapVoiceQuestion(existing), nil
	}
	if !errors.Is(err, practiceinput.ErrPersistenceNotFound) {
		return practice.Question{}, mapConversationError(err)
	}
	var request ai.TextRequest
	parentQuestionID := ""
	followUpAllowed := false
	if session.SceneFamily == "INTERVIEW" &&
		session.MaxFollowUpsPerQuestion > 0 && sequence > 1 {
		lister, ok := adapter.repository.(voiceSessionQuestionLister)
		if !ok {
			return practice.Question{}, practicevoice.ErrInvalidContext
		}
		questions, listErr := lister.ListSessionQuestions(
			ctx,
			conversationActor,
			session.ID,
		)
		if listErr != nil {
			return practice.Question{}, mapConversationError(listErr)
		}
		parentQuestionID, followUpAllowed = voiceFollowUpParent(
			questions,
			session.MaxFollowUpsPerQuestion,
		)
		request, err = voiceInterviewQuestionRequest(
			session,
			sequence,
			followUpAllowed,
		)
	} else {
		request, err = voiceQuestionRequest(session, sequence)
	}
	if err != nil {
		return practice.Question{}, err
	}
	content := ""
	questionType := "PRIMARY"
	if isFrozenIELTSSpeakingModel(session.SceneModel) {
		content, err = frozenIELTSFullMockQuestion(session, sequence)
	} else {
		var generated ai.TextResult
		generated, err = adapter.generator.Generate(ctx, request)
		content = strings.TrimSpace(generated.Content)
		if err == nil && session.SceneFamily == "INTERVIEW" &&
			session.MaxFollowUpsPerQuestion > 0 && sequence > 1 {
			var decision generatedInterviewQuestion
			if decodeErr := json.Unmarshal([]byte(content), &decision); decodeErr != nil {
				return practice.Question{}, practicevoice.ErrInvalidContext
			}
			questionType = strings.TrimSpace(decision.QuestionType)
			content = strings.TrimSpace(decision.Content)
			if questionType != "PRIMARY" && questionType != "FOLLOW_UP" {
				return practice.Question{}, practicevoice.ErrInvalidContext
			}
		}
	}
	if err != nil {
		return practice.Question{}, err
	}
	if strings.TrimSpace(content) == "" {
		return practice.Question{}, practicevoice.ErrInvalidContext
	}
	if questionType == "FOLLOW_UP" {
		if !followUpAllowed || parentQuestionID == "" {
			return practice.Question{}, practicevoice.ErrInvalidContext
		}
	} else {
		parentQuestionID = ""
	}
	saved, err := adapter.repository.SaveQuestion(
		ctx,
		conversationActor,
		practice.Question{
			ID:                      questionID,
			SessionID:               session.ID,
			SpeakerParticipantID:    session.FacilitatorParticipantID,
			AddresseeParticipantIDs: []string{session.LearnerParticipantID},
			ObjectiveID:             voiceQuestionObjective,
			Type:                    questionType,
			ParentQuestionID:        parentQuestionID,
			Content:                 content,
			Sequence:                sequence,
		},
	)
	if err != nil {
		if errors.Is(err, practiceinput.ErrPersistenceConflict) {
			existing, getErr := adapter.repository.GetQuestion(
				ctx,
				conversationActor,
				questionID,
			)
			if getErr == nil &&
				existing.SessionID == session.ID &&
				existing.Sequence == sequence {
				return mapVoiceQuestion(existing), nil
			}
		}
		return practice.Question{}, mapConversationError(err)
	}
	return mapVoiceQuestion(saved), nil
}

func voiceFollowUpParent(
	questions []practice.Question,
	maximum int,
) (string, bool) {
	if maximum < 1 {
		return "", false
	}
	followUps := 0
	for index := len(questions) - 1; index >= 0; index-- {
		question := questions[index]
		if question.Type == "PRIMARY" {
			return question.ID, question.ID != "" && followUps < maximum
		}
		if question.Type != "FOLLOW_UP" {
			return "", false
		}
		followUps++
	}
	return "", false
}

func isFrozenIELTSSpeakingModel(model string) bool {
	switch model {
	case "IELTS_SPEAKING_PART_1",
		"IELTS_SPEAKING_PART_2",
		"IELTS_SPEAKING_PART_3",
		"IELTS_SPEAKING_FULL_MOCK":
		return true
	default:
		return false
	}
}

func frozenIELTSFullMockQuestion(
	session practicevoice.Session,
	sequence int,
) (string, error) {
	blueprints := session.Prompt.TurnBlueprints
	if sequence < 1 || sequence > len(blueprints) {
		return "", practicevoice.ErrInvalidContext
	}
	blueprint := strings.TrimSpace(blueprints[sequence-1])
	separator := strings.Index(blueprint, ":")
	if separator < 0 || separator == len(blueprint)-1 {
		return "", practicevoice.ErrInvalidContext
	}
	return strings.TrimSpace(blueprint[separator+1:]), nil
}

func voiceQuestionRequest(
	session practicevoice.Session,
	sequence int,
) (ai.TextRequest, error) {
	prompt := session.Prompt
	if sequence < 1 || sequence > session.TurnLimit ||
		strings.TrimSpace(session.SceneFamily) == "" ||
		strings.TrimSpace(session.SceneModel) == "" ||
		strings.TrimSpace(prompt.PublicSceneBrief) == "" ||
		strings.TrimSpace(prompt.PracticeGoal) == "" ||
		strings.TrimSpace(prompt.UserRole) == "" ||
		strings.TrimSpace(prompt.AIRole) == "" ||
		strings.TrimSpace(prompt.PersonaSummary) == "" ||
		len(prompt.FocusAreas) == 0 ||
		len(prompt.TurnBlueprints) == 0 {
		return ai.TextRequest{}, practicevoice.ErrInvalidContext
	}
	blueprintIndex := sequence - 1
	if blueprintIndex >= len(prompt.TurnBlueprints) {
		blueprintIndex = len(prompt.TurnBlueprints) - 1
	}
	contextParts := []string{
		fmt.Sprintf("Scenario family: %s.", session.SceneFamily),
		fmt.Sprintf("Scenario model: %s.", session.SceneModel),
		fmt.Sprintf("Scene: %s", prompt.PublicSceneBrief),
		fmt.Sprintf("Practice goal: %s", prompt.PracticeGoal),
		fmt.Sprintf("Learner role: %s", prompt.UserRole),
		fmt.Sprintf("Your role: %s", prompt.AIRole),
		fmt.Sprintf("Your persona: %s", prompt.PersonaSummary),
		fmt.Sprintf("Focus areas: %s", strings.Join(prompt.FocusAreas, "; ")),
		fmt.Sprintf(
			"Current turn blueprint: %s",
			prompt.TurnBlueprints[blueprintIndex],
		),
	}
	if answer := strings.TrimSpace(session.PreviousUserResponse); answer != "" {
		contextParts = append(
			contextParts,
			fmt.Sprintf("Previous learner response: %s", answer),
		)
	}
	contextParts = append(
		contextParts,
		fmt.Sprintf("This is turn %d of at most %d.", sequence, session.TurnLimit),
	)
	return ai.TextRequest{
		Messages: []ai.TextMessage{
			{
				Role: ai.TextRoleSystem,
				Content: fmt.Sprintf(
					"You are %s. Stay in character as %s. Conduct a natural English conversation with the learner. Return exactly one concise question or conversational action, with no numbering, coaching notes, scoring, or explanation.",
					prompt.AIRole,
					prompt.PersonaSummary,
				),
			},
			{
				Role:    ai.TextRoleUser,
				Content: strings.Join(contextParts, "\n"),
			},
		},
	}, nil
}

func voiceInterviewQuestionRequest(
	session practicevoice.Session,
	sequence int,
	followUpAllowed bool,
) (ai.TextRequest, error) {
	prompt := session.Prompt
	maxQuestions := session.TurnLimit * (session.MaxFollowUpsPerQuestion + 1)
	if sequence < 2 || sequence > maxQuestions ||
		session.EffectiveTurns < 1 ||
		session.EffectiveTurns >= session.TurnLimit ||
		session.MaxFollowUpsPerQuestion < 1 ||
		strings.TrimSpace(session.PreviousQuestion) == "" ||
		strings.TrimSpace(session.PreviousUserResponse) == "" ||
		strings.TrimSpace(prompt.PublicSceneBrief) == "" ||
		strings.TrimSpace(prompt.PracticeGoal) == "" ||
		strings.TrimSpace(prompt.UserRole) == "" ||
		strings.TrimSpace(prompt.AIRole) == "" ||
		strings.TrimSpace(prompt.PersonaSummary) == "" ||
		len(prompt.FocusAreas) == 0 || len(prompt.TurnBlueprints) == 0 {
		return ai.TextRequest{}, practicevoice.ErrInvalidContext
	}
	nextBlueprintIndex := session.EffectiveTurns
	if nextBlueprintIndex >= len(prompt.TurnBlueprints) {
		nextBlueprintIndex = len(prompt.TurnBlueprints) - 1
	}
	decisionRule := "A FOLLOW_UP is available when the latest answer needs clarification or useful depth; otherwise choose PRIMARY."
	if !followUpAllowed {
		decisionRule = "The follow-up limit for this displayed round has been reached. You MUST choose PRIMARY."
	}
	return ai.TextRequest{Messages: []ai.TextMessage{
		{
			Role: ai.TextRoleSystem,
			Content: fmt.Sprintf(
				"You are %s, acting as %s in an English interview. %s Return only valid JSON with exactly two string fields: {\"question_type\":\"PRIMARY|FOLLOW_UP\",\"content\":\"...\"}. Do not include markdown, numbering, coaching, scoring, or explanations.",
				prompt.AIRole,
				prompt.PersonaSummary,
				decisionRule,
			),
		},
		{
			Role: ai.TextRoleUser,
			Content: strings.Join([]string{
				fmt.Sprintf("Scene: %s", prompt.PublicSceneBrief),
				fmt.Sprintf("Practice goal: %s", prompt.PracticeGoal),
				fmt.Sprintf("Focus areas: %s", strings.Join(prompt.FocusAreas, "; ")),
				fmt.Sprintf("Current displayed round: %d of %d.", session.EffectiveTurns, session.TurnLimit),
				fmt.Sprintf("Previous interviewer question: %s", session.PreviousQuestion),
				fmt.Sprintf("Latest learner answer: %s", session.PreviousUserResponse),
				fmt.Sprintf("Next independent-question blueprint: %s", prompt.TurnBlueprints[nextBlueprintIndex]),
				fmt.Sprintf("The server permits at most %d follow-ups for one displayed round.", session.MaxFollowUpsPerQuestion),
			}, "\n"),
		},
	}}, nil
}

func (adapter *voiceQuestionAdapter) GetQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (practice.Question, error) {
	question, err := adapter.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil {
		return practice.Question{}, mapConversationError(err)
	}
	return mapVoiceQuestion(question), nil
}

func (adapter *voiceQuestionAdapter) SynthesizeQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (practiceinput.QuestionSpeech, error) {
	question, err := adapter.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil {
		return practiceinput.QuestionSpeech{}, mapConversationError(err)
	}
	return adapter.speech.SynthesizeQuestion(ctx, question.Content)
}

type voiceCheckpointAdapter struct {
	repository  practiceinput.PersistenceStore
	audioAssets *practiceinput.AudioAssetService
	feedback    evaluation.SpeechFeedbackReader
}

func (adapter *voiceCheckpointAdapter) ListTurnHistory(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) ([]practicevoice.TurnExchange, error) {
	turns, err := adapter.repository.ListSessionTurns(
		ctx,
		conversationActor(actor),
		sessionID,
	)
	if err != nil {
		return nil, mapConversationError(err)
	}
	history := make([]practicevoice.TurnExchange, 0, len(turns))
	for _, persistedTurn := range turns {
		candidate, candidateErr := adapter.repository.GetCandidate(
			ctx,
			conversationActor(actor),
			persistedTurn.CandidateID,
		)
		if candidateErr != nil {
			return nil, mapConversationError(candidateErr)
		}
		turn, turnErr := mapVoiceTurnWithCandidate(
			persistedTurn,
			candidate,
		)
		if turnErr != nil {
			return nil, turnErr
		}
		turn, turnErr = adapter.withReadableRecording(
			ctx,
			actor,
			turn,
		)
		if turnErr != nil {
			return nil, turnErr
		}
		turn, turnErr = adapter.withSpeechFeedback(
			ctx,
			actor,
			turn,
		)
		if turnErr != nil {
			return nil, turnErr
		}
		question, questionErr := adapter.repository.GetQuestion(
			ctx,
			conversationActor(actor),
			turn.QuestionID,
		)
		if questionErr != nil {
			return nil, mapConversationError(questionErr)
		}
		history = append(history, practicevoice.TurnExchange{
			Question: mapVoiceQuestion(question),
			Turn:     turn,
		})
	}
	return history, nil
}

func (adapter *voiceCheckpointAdapter) LatestTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (practice.Turn, bool, error) {
	turns, err := adapter.repository.ListSessionTurns(
		ctx,
		conversationActor(actor),
		sessionID,
	)
	if err != nil {
		return practice.Turn{}, false,
			mapConversationError(err)
	}
	if len(turns) == 0 {
		return practice.Turn{}, false, nil
	}
	persistedTurn := turns[len(turns)-1]
	candidate, err := adapter.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		persistedTurn.CandidateID,
	)
	if err != nil {
		return practice.Turn{}, false,
			mapConversationError(err)
	}
	turn, err := mapVoiceTurnWithCandidate(persistedTurn, candidate)
	if err != nil {
		return practice.Turn{}, false, err
	}
	turn, err = adapter.withReadableRecording(ctx, actor, turn)
	if err != nil {
		return practice.Turn{}, false, err
	}
	turn, err = adapter.withSpeechFeedback(ctx, actor, turn)
	if err != nil {
		return practice.Turn{}, false, err
	}
	return turn, true, nil
}

func (adapter *voiceCheckpointAdapter) withReadableRecording(
	ctx context.Context,
	actor requestcontext.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if adapter.audioAssets == nil {
		return turn, nil
	}
	asset, err := adapter.audioAssets.GetReadableByTurn(
		ctx,
		practiceinput.AudioAssetActor{UserID: actor.UserID},
		turn.ID,
	)
	if errors.Is(err, practiceinput.ErrAudioAssetNotFound) ||
		errors.Is(err, practiceinput.ErrAudioAssetInvalidTransition) {
		return turn, nil
	}
	if err != nil {
		return practice.Turn{}, err
	}
	turn.AudioAssetID = asset.ID
	return turn, nil
}

func (adapter *voiceCheckpointAdapter) withSpeechFeedback(
	ctx context.Context,
	actor requestcontext.Actor,
	turn practice.Turn,
) (practice.Turn, error) {
	if adapter.feedback == nil {
		return turn, nil
	}
	statusURL, found, err :=
		adapter.feedback.StatusURLForConversationTurn(
			ctx,
			actor,
			turn.ID,
		)
	if err != nil {
		return practice.Turn{}, err
	}
	if found {
		turn.SpeechFeedbackStatusURL = statusURL
	}
	return turn, nil
}

type voiceSpeechFeedbackAdapter struct {
	coordinator *evaluation.SpeechFeedbackCoordinator
}

func (adapter *voiceSpeechFeedbackAdapter) EnsureMessage(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) (inputvoice.FeedbackReference, error) {
	if adapter == nil || adapter.coordinator == nil {
		return inputvoice.FeedbackReference{},
			inputvoice.ErrInvalidContext
	}
	reference, err := adapter.coordinator.EnsureAgentVoiceMessage(
		ctx,
		actor,
		threadID,
		messageID,
	)
	if errors.Is(err, evaluation.ErrSpeechFeedbackNotApplicable) {
		return inputvoice.FeedbackReference{}, nil
	}
	if err != nil {
		return inputvoice.FeedbackReference{}, err
	}
	return inputvoice.FeedbackReference{
		StatusURL: reference.StatusURL,
	}, nil
}

func (adapter *voiceSpeechFeedbackAdapter) EnsureConversationTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
) (practicevoice.TurnFeedbackReference, error) {
	if adapter == nil || adapter.coordinator == nil {
		return practicevoice.TurnFeedbackReference{},
			practicevoice.ErrInvalidContext
	}
	reference, err := adapter.coordinator.EnsureConversationTurn(
		ctx,
		actor,
		sessionID,
		turnID,
	)
	if errors.Is(err, evaluation.ErrSpeechFeedbackNotApplicable) {
		return practicevoice.TurnFeedbackReference{}, nil
	}
	if err != nil {
		return practicevoice.TurnFeedbackReference{}, err
	}
	return practicevoice.TurnFeedbackReference{
		StatusURL:  reference.StatusURL,
		Applicable: true,
	}, nil
}

func stableVoiceID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

var (
	_ practicevoice.SessionPort     = (*voicePracticeAdapter)(nil)
	_ practiceinput.VoiceRoundStore = (*voiceConversationStore)(nil)
	_ practicevoice.QuestionPort    = (*voiceQuestionAdapter)(nil)
	_ practicevoice.CheckpointPort  = (*voiceCheckpointAdapter)(nil)
)
