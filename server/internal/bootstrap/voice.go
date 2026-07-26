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

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/qianwen"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversationpersistence "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	practicepersistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	voiceTurnLimit            = 3
	voiceReviewImplementation = "qianwen-voice-review-v1"
	voiceReviewMaxGeneration  = 20 * time.Second
	voiceQuestionObjective    = "targeted-english-practice"
	voiceInterviewerSubject   = "qianwen-interviewer-v1"
)

// VoiceReviewGateway is the narrow Review capability consumed by the Agent
// voice Saga. Review implementations keep their Repository private.
type VoiceReviewGateway interface {
	agent.VoiceReviewPort
	agent.VoiceReviewReader
}

// VoicePorts are application capabilities supplied by the owning modules.
// Repository types remain confined to their module and the composition root.
type VoicePorts struct {
	ConversationStore conversation.VoiceRoundStore
	Practice          agent.VoicePracticePort
	Sessions          agent.VoiceSessionPort
	Questions         agent.VoiceQuestionPort
	Checkpoints       agent.VoiceCheckpointPort
	Reviews           VoiceReviewGateway
}

type VoiceConfiguration struct {
	Recognizer              ai.SpeechRecognizer
	Synthesizer             ai.SpeechSynthesizer
	TemporaryAudio          conversation.TemporaryAudioVault
	Ports                   VoicePorts
	Recordings              conversation.VoiceRecordingLifecycle
	ObjectStore             objectstore.Store
	AudioStagedTTL          time.Duration
	ASRLease                time.Duration
	ReviewGenerationTimeout time.Duration
	AudioReadTimeout        time.Duration
}

// NewSpeechRecognizer is the server-side ASR registration boundary. Production
// never silently substitutes a Fake or another provider.
func NewSpeechRecognizer(
	configuration config.SpeechRecognitionConfig,
) (ai.SpeechRecognizer, error) {
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
	matters matter.Reader,
	configuration VoiceConfiguration,
) (*agent.VoiceSessionApplication, error) {
	ports := configuration.Ports
	if matters == nil ||
		configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.TemporaryAudio == nil ||
		ports.ConversationStore == nil ||
		ports.Practice == nil ||
		ports.Sessions == nil ||
		ports.Questions == nil ||
		ports.Checkpoints == nil ||
		ports.Reviews == nil {
		return nil, errors.New("bootstrap: voice dependencies are required")
	}
	conversations, err := conversation.NewVoiceRoundServiceWithRecordings(
		ports.ConversationStore,
		configuration.TemporaryAudio,
		configuration.Recognizer,
		configuration.Synthesizer,
		configuration.Recordings,
	)
	if err != nil {
		return nil, err
	}
	orchestrator, err := agent.NewVoiceRoundOrchestrator(
		conversations,
		ports.Practice,
		ports.Reviews,
	)
	if err != nil {
		return nil, err
	}
	return agent.NewVoiceSessionApplication(
		ports.Sessions,
		ports.Questions,
		ports.Checkpoints,
		orchestrator,
		ports.Reviews,
		matters,
	)
}

func buildProductionVoiceApplication(
	database *pgxpool.Pool,
	textGenerator ai.TextGenerator,
	matters matter.Reader,
	configuration VoiceConfiguration,
) (
	*agent.VoiceSessionApplication,
	*conversation.AudioAssetService,
	error,
) {
	if database == nil || textGenerator == nil || matters == nil ||
		configuration.Recognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.ASRLease <= 0 ||
		configuration.ReviewGenerationTimeout <= 0 ||
		configuration.ReviewGenerationTimeout > voiceReviewMaxGeneration {
		return nil, nil,
			errors.New("bootstrap: voice dependencies are required")
	}

	practiceRepository := practicepostgres.New(database)
	practiceApplication, err := practice.NewVoiceApplication(
		practiceRepository,
		"speakup.user",
	)
	if err != nil {
		return nil, nil, err
	}
	practiceProgressPort, err := NewPracticeVoicePort(practiceApplication)
	if err != nil {
		return nil, nil, err
	}
	conversationRepository, err := conversationpostgres.New(database)
	if err != nil {
		return nil, nil, err
	}
	conversationStore := &voiceConversationStore{
		repository: conversationRepository,
		recordings: conversationRepository,
		asrLease:   configuration.ASRLease,
	}
	var audioAssets *conversation.AudioAssetService
	if configuration.ObjectStore != nil {
		audioRepository, repositoryErr :=
			conversationpostgres.NewAudioAssetRepository(database)
		if repositoryErr != nil {
			return nil, nil, repositoryErr
		}
		audioAssets, err = conversation.NewAudioAssetService(
			audioRepository,
			configuration.ObjectStore,
			conversation.SecureAudioAssetIDGenerator{},
			conversation.NewAudioAssetSystemClock(),
			audioRepository,
			configuration.AudioStagedTTL,
		)
		if err != nil {
			return nil, nil, err
		}
	}
	var recordingLifecycle conversation.VoiceRecordingLifecycle
	if audioAssets != nil {
		recordingLifecycle = audioAssets
	}
	conversationService, err :=
		conversation.NewVoiceRoundServiceWithRecordings(
			conversationStore,
			configuration.TemporaryAudio,
			configuration.Recognizer,
			configuration.Synthesizer,
			recordingLifecycle,
		)
	if err != nil {
		return nil, nil, err
	}
	practiceAdapter := &voicePracticeAdapter{
		repository: practiceRepository,
		matters:    matters,
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
	sourceReader := &voiceReviewSourceReader{
		conversations: conversationRepository,
		practice:      practiceRepository,
	}
	reviewRepository := review.NewPostgresRepository(database)
	reviewGenerator := &voiceReviewGenerator{
		generator: textGenerator,
		timeout:   configuration.ReviewGenerationTimeout,
	}
	ensureReviews := review.NewEnsureService(
		reviewRepository,
		sourceReader,
		reviewGenerator,
	)
	reviewAdapter := &voiceReviewAdapter{
		service:      ensureReviews,
		history:      review.NewHistoryService(reviewRepository),
		sourceReader: sourceReader,
	}
	orchestrator, err := agent.NewVoiceRoundOrchestrator(
		conversationService,
		practiceProgressPort,
		reviewAdapter,
	)
	if err != nil {
		return nil, nil, err
	}
	application, err := agent.NewVoiceSessionApplication(
		practiceAdapter,
		questionAdapter,
		checkpointAdapter,
		orchestrator,
		reviewAdapter,
		matters,
	)
	if err != nil {
		return nil, nil, err
	}
	return application, audioAssets, nil
}

type voicePracticeAdapter struct {
	repository practicepersistence.Repository
	matters    matter.Reader
}

func (adapter *voicePracticeAdapter) Start(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
	idempotencyKey string,
) (agent.VoicePracticeSession, error) {
	if adapter == nil || adapter.repository == nil || adapter.matters == nil ||
		!actor.Valid() ||
		strings.TrimSpace(threadID) == "" ||
		strings.TrimSpace(matterID) == "" ||
		strings.TrimSpace(idempotencyKey) == "" {
		return agent.VoicePracticeSession{}, agent.ErrInvalidRequest
	}
	sessionID := stableVoiceID(
		"voice_session",
		actor.UserID,
		threadID,
		idempotencyKey,
	)
	practiceActor := practiceActor(actor)
	existing, err := adapter.repository.GetSession(ctx, practiceActor, sessionID)
	if err == nil {
		// The idempotency key identifies the immutable Session Snapshot. A
		// later Thread selection change must not reinterpret that replay.
		return mapPracticeSession(existing, actor.UserID, threadID, "")
	}
	if !errors.Is(err, practicepersistence.ErrNotFound) {
		return agent.VoicePracticeSession{}, mapPracticeError(err)
	}
	selectedMatter, err := adapter.matters.ReadOwned(ctx, actor, matterID)
	if err != nil {
		return agent.VoicePracticeSession{}, mapVoiceMatterError(err)
	}
	if selectedMatter.ID != matterID ||
		selectedMatter.Status != matter.StatusActive {
		return agent.VoicePracticeSession{}, agent.ErrConflict
	}
	hashKey := actor.UserID + "\x00" + threadID
	created, err := adapter.repository.CreateSession(
		ctx,
		practiceActor,
		practicepersistence.CreateSessionCommand{
			SessionID: sessionID,
			// P0 has no separate Plan creation endpoint. Preserve the exact
			// authoritative Thread association in Practice's existing Plan
			// reference instead of selecting a recent Session later.
			PlanID: "agent-thread:" + threadID,
			Snapshot: practicepersistence.SessionSnapshot{
				Mode:      "INTERVIEW",
				TargetIDs: []string{matterID},
				Participants: []practicepersistence.ParticipantSnapshot{
					{
						ParticipantID: stableVoiceID(
							"participant_interviewer",
							hashKey,
						),
						ParticipantRole: "INTERVIEWER",
						SubjectRef: practicepersistence.SubjectRef{
							Namespace: "speakup.agent",
							SubjectID: voiceInterviewerSubject,
						},
						Order: 0,
					},
					{
						ParticipantID: stableVoiceID(
							"participant_candidate",
							hashKey,
						),
						ParticipantRole: "CANDIDATE",
						SubjectRef: practicepersistence.SubjectRef{
							Namespace: "speakup.user",
							SubjectID: actor.UserID,
						},
						Order: 1,
					},
				},
				TurnLimit: voiceTurnLimit,
			},
		},
	)
	if err != nil {
		// A concurrent replay may have committed the same stable Session.
		if errors.Is(err, practicepersistence.ErrConflict) {
			created, err = adapter.repository.GetSession(
				ctx,
				practiceActor,
				sessionID,
			)
			if errors.Is(err, practicepersistence.ErrNotFound) {
				// The Plan already has a different active Session. Only an
				// exact stable-ID replay is idempotent.
				return agent.VoicePracticeSession{}, agent.ErrConflict
			}
			if err == nil {
				return mapPracticeSession(
					created,
					actor.UserID,
					threadID,
					"",
				)
			}
		}
		if err != nil {
			return agent.VoicePracticeSession{}, mapPracticeError(err)
		}
	}
	return mapPracticeSession(created, actor.UserID, threadID, matterID)
}

func mapVoiceMatterError(err error) error {
	switch {
	case errors.Is(err, matter.ErrInvalidRequest):
		return agent.ErrInvalidRequest
	case errors.Is(err, matter.ErrNotFound):
		return agent.ErrNotFound
	case errors.Is(err, matter.ErrConflict):
		return agent.ErrConflict
	default:
		return err
	}
}

func (adapter *voicePracticeAdapter) GetByThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
) (agent.VoicePracticeSession, error) {
	sessions, err := adapter.repository.ListSessions(
		ctx,
		practiceActor(actor),
	)
	if err != nil {
		return agent.VoicePracticeSession{}, mapPracticeError(err)
	}
	planID := "agent-thread:" + threadID
	var selected practicepersistence.Session
	found := false
	for _, session := range sessions {
		if session.PlanID != planID {
			continue
		}
		if session.Status == practicepersistence.SessionStatusActive {
			selected = session
			found = true
			break
		}
		if !found || session.CreatedAt.After(selected.CreatedAt) {
			selected = session
			found = true
		}
	}
	if !found {
		return agent.VoicePracticeSession{}, agent.ErrNotFound
	}
	return mapPracticeSession(selected, actor.UserID, threadID, matterID)
}

func (adapter *voicePracticeAdapter) GetByID(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (agent.VoicePracticeSession, error) {
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor(actor),
		sessionID,
	)
	if err != nil {
		return agent.VoicePracticeSession{}, mapPracticeError(err)
	}
	return mapPracticeSession(session, actor.UserID, "", "")
}

func (adapter *voicePracticeAdapter) ResolveActorParticipant(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (string, error) {
	session, err := adapter.GetByID(ctx, actor, sessionID)
	if err != nil {
		return "", err
	}
	if session.CandidateParticipantID == "" {
		return "", agent.ErrNotFound
	}
	return session.CandidateParticipantID, nil
}

func (adapter *voicePracticeAdapter) ApplyEffectiveTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	turnID string,
) (agent.VoiceTurnProgress, error) {
	result, err := adapter.repository.ConsumeTurn(
		ctx,
		practiceActor(actor),
		practicepersistence.ConsumeTurnCommand{
			SessionID: sessionID,
			TurnID:    turnID,
			Payload:   []byte("conversation-turn:" + turnID),
		},
	)
	if err != nil {
		return agent.VoiceTurnProgress{}, mapPracticeError(err)
	}
	session, err := adapter.repository.GetSession(
		ctx,
		practiceActor(actor),
		sessionID,
	)
	if err != nil {
		return agent.VoiceTurnProgress{}, mapPracticeError(err)
	}
	return agent.VoiceTurnProgress{
		EffectiveTurns:   result.EffectiveTurns,
		SessionVersion:   result.SessionVersion,
		TurnLimit:        session.Snapshot.TurnLimit,
		SessionCompleted: result.Completed,
	}, nil
}

func practiceActor(actor requestcontext.Actor) practicepersistence.Actor {
	return practicepersistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapPracticeSession(
	session practicepersistence.Session,
	actorUserID string,
	threadID string,
	matterID string,
) (agent.VoicePracticeSession, error) {
	result := agent.VoicePracticeSession{
		ID:             session.ID,
		PlanID:         session.PlanID,
		ThreadID:       threadID,
		SessionVersion: session.Version,
		EffectiveTurns: session.EffectiveTurns,
		TurnLimit:      session.Snapshot.TurnLimit,
		Completed:      session.Status == practicepersistence.SessionStatusCompleted,
	}
	if threadID == "" {
		const prefix = "agent-thread:"
		if !strings.HasPrefix(session.PlanID, prefix) ||
			strings.TrimSpace(strings.TrimPrefix(
				session.PlanID,
				prefix,
			)) == "" {
			return agent.VoicePracticeSession{}, agent.ErrInvalidContext
		}
		result.ThreadID = strings.TrimPrefix(session.PlanID, prefix)
	}
	if threadID != "" && session.PlanID != "agent-thread:"+threadID {
		return agent.VoicePracticeSession{}, agent.ErrConflict
	}
	if len(session.Snapshot.TargetIDs) != 1 ||
		(matterID != "" && session.Snapshot.TargetIDs[0] != matterID) {
		return agent.VoicePracticeSession{}, agent.ErrConflict
	}
	result.MatterID = session.Snapshot.TargetIDs[0]
	for _, participant := range session.Snapshot.Participants {
		switch participant.ParticipantRole {
		case "INTERVIEWER":
			if result.InterviewerParticipantID != "" {
				return agent.VoicePracticeSession{}, agent.ErrInvalidContext
			}
			result.InterviewerParticipantID = participant.ParticipantID
		case "CANDIDATE":
			if result.CandidateParticipantID != "" ||
				participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != actorUserID {
				return agent.VoicePracticeSession{}, agent.ErrNotFound
			}
			result.CandidateParticipantID = participant.ParticipantID
		}
	}
	if result.InterviewerParticipantID == "" ||
		result.CandidateParticipantID == "" ||
		result.TurnLimit <= 0 ||
		result.EffectiveTurns < 0 ||
		result.EffectiveTurns > result.TurnLimit ||
		result.Completed != (result.EffectiveTurns == result.TurnLimit) {
		return agent.VoicePracticeSession{}, agent.ErrInvalidContext
	}
	return result, nil
}

func mapPracticeError(err error) error {
	switch {
	case errors.Is(err, practicepersistence.ErrInvalidArgument):
		return agent.ErrInvalidRequest
	case errors.Is(err, practicepersistence.ErrNotFound):
		return agent.ErrNotFound
	case errors.Is(err, practicepersistence.ErrIdempotencyConflict):
		return agent.ErrIdempotencyConflict
	case errors.Is(err, practicepersistence.ErrConflict),
		errors.Is(err, practicepersistence.ErrSessionCompleted):
		return agent.ErrConflict
	default:
		return err
	}
}

type voiceConversationStore struct {
	repository conversationpersistence.PersistenceStore
	recordings conversationpersistence.RecordingConfirmationStore
	asrLease   time.Duration
}

func (store *voiceConversationStore) GetVoiceQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	questionID string,
) (conversation.VoiceQuestion, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil || question.SessionID != sessionID {
		if err == nil {
			return conversation.VoiceQuestion{},
				conversation.ErrVoiceRoundNotFound
		}
		return conversation.VoiceQuestion{}, mapConversationError(err)
	}
	return mapVoiceQuestion(question), nil
}

func (store *voiceConversationStore) ReserveTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ReserveTranscriptionCommand,
) (conversation.TranscriptionReservation, error) {
	reservation, err := store.repository.ReserveTranscription(
		ctx,
		conversationActor(actor),
		conversationpersistence.ReserveTranscriptionCommand{
			SessionID:               command.SessionID,
			QuestionID:              command.QuestionID,
			RespondentParticipantID: command.RespondentParticipantID,
			IdempotencyKey:          command.IdempotencyKey,
			InputFingerprint:        command.InputFingerprint,
			LeaseDuration:           store.asrLease,
		},
	)
	if err != nil {
		return conversation.TranscriptionReservation{}, mapConversationError(err)
	}
	result := conversation.TranscriptionReservation{
		ID: reservation.ID,
	}
	switch reservation.Status {
	case conversationpersistence.TranscriptionCompleted:
		result.Status = conversation.TranscriptionCompleted
		candidate, candidateErr := store.GetTranscriptionCandidate(
			ctx,
			actor,
			reservation.CandidateID,
		)
		if candidateErr != nil {
			return conversation.TranscriptionReservation{}, candidateErr
		}
		result.Candidate = candidate
	case conversationpersistence.TranscriptionProcessing:
		if reservation.LeaseAcquired {
			result.Status = conversation.TranscriptionReserved
			result.LeaseToken = transcriptionLeaseToken(
				actor.UserID,
				reservation,
			)
		} else {
			result.Status = conversation.TranscriptionProcessing
		}
	default:
		return conversation.TranscriptionReservation{},
			conversation.ErrVoiceRoundConflict
	}
	return result, nil
}

func (store *voiceConversationStore) CompleteTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.CompleteTranscriptionCommand,
) (conversation.TranscriptionCandidate, error) {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return conversation.TranscriptionCandidate{}, err
	}
	candidate, err := store.repository.CompleteTranscription(
		ctx,
		job,
		conversationpersistence.CompleteTranscriptionCommand{
			TranscriptID:      command.TranscriptID,
			EvidenceVersion:   command.EvidenceVersion,
			Provider:          command.Provider,
			Model:             command.Model,
			ProviderRequestID: command.ProviderRequestID,
			Text:              command.Transcript,
		},
	)
	if err != nil {
		return conversation.TranscriptionCandidate{}, mapConversationError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *voiceConversationStore) FailTranscription(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.FailTranscriptionCommand,
) error {
	job, err := store.transcriptionJob(ctx, actor, command.ReservationID, command.LeaseToken)
	if err != nil {
		return err
	}
	return mapConversationError(store.repository.FailTranscription(
		ctx,
		job,
		conversationpersistence.ProcessingFailure{
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
) (conversationpersistence.JobContext, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return conversationpersistence.JobContext{},
			conversation.ErrVoiceRoundConflict
	}
	reservation, err := store.repository.GetReservation(
		ctx,
		conversationActor(actor),
		reservationID,
	)
	if err != nil {
		return conversationpersistence.JobContext{}, mapConversationError(err)
	}
	expectedToken := transcriptionLeaseToken(actor.UserID, reservation)
	if subtle.ConstantTimeCompare(
		[]byte(leaseToken),
		[]byte(expectedToken),
	) != 1 {
		return conversationpersistence.JobContext{},
			conversation.ErrVoiceRoundConflict
	}
	return conversationpersistence.JobContext{
		OwnerUserID:        actor.UserID,
		DeletionGeneration: reservation.DeletionGeneration,
		ReservationID:      reservation.ID,
		FencingToken:       reservation.FencingToken,
	}, nil
}

func transcriptionLeaseToken(
	ownerUserID string,
	reservation conversationpersistence.TranscriptionReservation,
) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(
		digest,
		"conversation.asr-lease/v1\x00%s\x00%s\x00%d\x00%d",
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
) (conversation.TranscriptionCandidate, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		candidateID,
	)
	if err != nil {
		return conversation.TranscriptionCandidate{}, mapConversationError(err)
	}
	return store.mapCandidate(ctx, actor, candidate)
}

func (store *voiceConversationStore) mapCandidate(
	ctx context.Context,
	actor requestcontext.Actor,
	candidate conversationpersistence.TranscriptCandidate,
) (conversation.TranscriptionCandidate, error) {
	question, err := store.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		candidate.QuestionID,
	)
	if err != nil {
		return conversation.TranscriptionCandidate{}, mapConversationError(err)
	}
	return conversation.TranscriptionCandidate{
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
	command conversation.ReserveConfirmationCommand,
) (conversation.ConfirmedVoiceTurn, error) {
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	turn, err := store.repository.ConfirmTurn(
		ctx,
		conversationActor(actor),
		conversationpersistence.ConfirmTurnCommand{
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ConfirmedText:   candidate.Text,
			IdempotencyKey:  command.IdempotencyKey,
		},
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	return mapVoiceTurnWithCandidate(turn, candidate)
}

func (store *voiceConversationStore) ReserveRecordingConfirmation(
	ctx context.Context,
	actor requestcontext.Actor,
	command conversation.ConfirmVoiceTurnCommand,
	uploadRequestID string,
) (conversation.VoiceRecordingConfirmation, error) {
	if store.recordings == nil {
		return conversation.VoiceRecordingConfirmation{},
			conversation.ErrVoiceRoundInvalid
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		command.CandidateID,
	)
	if err != nil {
		return conversation.VoiceRecordingConfirmation{},
			mapConversationError(err)
	}
	persisted, err :=
		store.recordings.ConfirmTurnWithRecording(
			ctx,
			conversationActor(actor),
			conversationpersistence.ConfirmTurnCommand{
				CandidateID:     candidate.ID,
				EvidenceVersion: candidate.EvidenceVersion,
				ConfirmedText:   candidate.Text,
				IdempotencyKey:  command.IdempotencyKey,
			},
			uploadRequestID,
		)
	if err != nil {
		return conversation.VoiceRecordingConfirmation{},
			mapRecordingConfirmationError(err)
	}
	mapped, err := mapVoiceTurnWithCandidate(persisted.Turn, candidate)
	if err != nil {
		return conversation.VoiceRecordingConfirmation{}, err
	}
	mapped.AudioAssetID = persisted.AudioAssetID
	return conversation.VoiceRecordingConfirmation{
		Turn:             mapped,
		RecordingDeleted: persisted.RecordingDeleted,
	}, nil
}

func mapRecordingConfirmationError(err error) error {
	switch {
	case errors.Is(err, conversation.ErrAudioAssetNotFound),
		errors.Is(err, conversation.ErrAudioAssetForbidden),
		errors.Is(err, conversation.ErrAudioAssetAlreadyBound),
		errors.Is(err, conversation.ErrAudioAssetInvalidTransition),
		errors.Is(err, conversation.ErrAudioAssetUploadTerminated):
		// A recording that cleanup removed, another Turn already bound, or
		// otherwise left the confirmable state is a terminal request conflict.
		return agent.ErrConflict
	case errors.Is(err, conversation.ErrAudioAssetConcurrentUpdate):
		// A lost optimistic update can be retried safely with the same
		// idempotency key. The HTTP boundary exposes resource_processing,
		// Retry-After: 1, and retryable=true for this sentinel.
		return conversation.ErrVoiceRoundProcessing
	default:
		return mapConversationError(err)
	}
}

func (store *voiceConversationStore) SaveTurnProgress(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
	progress conversation.VoiceTurnProgress,
) (conversation.ConfirmedVoiceTurn, error) {
	turn, err := store.repository.SaveTurnProgress(
		ctx,
		conversationActor(actor),
		turnID,
		conversationpersistence.TurnProgress(progress),
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		turn.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	return mapVoiceTurnWithCandidate(turn, candidate)
}

func (store *voiceConversationStore) SaveTurnReview(
	ctx context.Context,
	actor requestcontext.Actor,
	turnID string,
	reviewID string,
) (conversation.ConfirmedVoiceTurn, error) {
	turn, err := store.repository.SaveTurnReview(
		ctx,
		conversationActor(actor),
		turnID,
		conversationpersistence.TurnReviewCheckpoint{
			ReviewID:     reviewID,
			SourceTurnID: turnID,
		},
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	candidate, err := store.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		turn.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, mapConversationError(err)
	}
	return mapVoiceTurnWithCandidate(turn, candidate)
}

func conversationActor(
	actor requestcontext.Actor,
) conversationpersistence.Actor {
	return conversationpersistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func mapConversationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, conversationpersistence.ErrPersistenceInvalid):
		return conversation.ErrVoiceRoundInvalid
	case errors.Is(err, conversationpersistence.ErrPersistenceNotFound):
		return conversation.ErrVoiceRoundNotFound
	case errors.Is(err, conversationpersistence.ErrPersistenceConflict):
		return conversation.ErrVoiceRoundConflict
	case errors.Is(err, conversationpersistence.ErrActorDeleted):
		return agent.ErrNotFound
	default:
		return err
	}
}

func mapVoiceQuestion(
	question conversationpersistence.PersistentQuestion,
) conversation.VoiceQuestion {
	return conversation.VoiceQuestion{
		ID:                      question.ID,
		SessionID:               question.SessionID,
		Text:                    question.Content,
		SpeakerParticipantID:    question.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(question.AddresseeParticipantIDs),
	}
}

func mapVoiceTurn(
	turn conversationpersistence.ConfirmedTurn,
) conversation.ConfirmedVoiceTurn {
	return conversation.ConfirmedVoiceTurn{
		ID:                      turn.ID,
		SessionID:               turn.SessionID,
		QuestionID:              turn.QuestionID,
		QuestionSpeakerID:       turn.SpeakerParticipantID,
		AddresseeParticipantIDs: slices.Clone(turn.AddresseeParticipantIDs),
		RespondentParticipantID: turn.RespondentParticipantID,
		CandidateID:             turn.CandidateID,
		EvidenceVersion:         turn.EvidenceVersion,
		AnswerText:              turn.AnswerText,
		EffectiveTurns:          turn.Progress.EffectiveTurns,
		SessionCompleted:        turn.Progress.SessionCompleted,
		ReviewID:                turn.Review.ReviewID,
	}
}

func mapVoiceTurnWithCandidate(
	turn conversationpersistence.ConfirmedTurn,
	candidate conversationpersistence.TranscriptCandidate,
) (conversation.ConfirmedVoiceTurn, error) {
	if candidate.ID == "" ||
		candidate.ID != turn.CandidateID ||
		candidate.SessionID != turn.SessionID ||
		candidate.QuestionID != turn.QuestionID ||
		candidate.RespondentParticipantID != turn.RespondentParticipantID ||
		candidate.EvidenceVersion != turn.EvidenceVersion ||
		candidate.Text != turn.AnswerText ||
		candidate.TranscriptID == "" {
		return conversation.ConfirmedVoiceTurn{},
			conversation.ErrVoiceRoundConflict
	}
	mapped := mapVoiceTurn(turn)
	mapped.TranscriptID = candidate.TranscriptID
	return mapped, nil
}

type voiceQuestionAdapter struct {
	repository conversationpersistence.PersistenceStore
	generator  ai.TextGenerator
	speech     *conversation.VoiceRoundService
}

func (adapter *voiceQuestionAdapter) EnsureQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	session agent.VoicePracticeSession,
	sequence int,
) (conversation.VoiceQuestion, error) {
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
	if strings.TrimSpace(session.MatterTitle) == "" {
		return conversation.VoiceQuestion{}, agent.ErrInvalidContext
	}
	if !errors.Is(err, conversationpersistence.ErrPersistenceNotFound) {
		return conversation.VoiceQuestion{}, mapConversationError(err)
	}
	generated, err := adapter.generator.Generate(ctx, ai.TextRequest{
		Messages: []ai.TextMessage{
			{
				Role:    ai.TextRoleSystem,
				Content: "You are an English interview coach. Return exactly one concise English interview question, with no numbering or explanation.",
			},
			{
				Role: ai.TextRoleUser,
				Content: fmt.Sprintf(
					"Create question %d of %d for a targeted professional English practice session about this real-world Matter: %q. Focus on the scenario goal and avoid generic greetings.",
					sequence,
					session.TurnLimit,
					session.MatterTitle,
				),
			},
		},
	})
	if err != nil {
		return conversation.VoiceQuestion{}, err
	}
	content := strings.TrimSpace(generated.Content)
	if content == "" {
		return conversation.VoiceQuestion{}, agent.ErrInvalidContext
	}
	saved, err := adapter.repository.SaveQuestion(
		ctx,
		conversationActor,
		conversationpersistence.PersistentQuestion{
			ID:                      questionID,
			SessionID:               session.ID,
			SpeakerParticipantID:    session.InterviewerParticipantID,
			AddresseeParticipantIDs: []string{session.CandidateParticipantID},
			ObjectiveID:             voiceQuestionObjective,
			Type:                    "PRIMARY",
			Content:                 content,
			Sequence:                sequence,
		},
	)
	if err != nil {
		if errors.Is(err, conversationpersistence.ErrPersistenceConflict) {
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
		return conversation.VoiceQuestion{}, mapConversationError(err)
	}
	return mapVoiceQuestion(saved), nil
}

func (adapter *voiceQuestionAdapter) GetQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (conversation.VoiceQuestion, error) {
	question, err := adapter.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil {
		return conversation.VoiceQuestion{}, mapConversationError(err)
	}
	return mapVoiceQuestion(question), nil
}

func (adapter *voiceQuestionAdapter) SynthesizeQuestion(
	ctx context.Context,
	actor requestcontext.Actor,
	questionID string,
) (conversation.QuestionSpeech, error) {
	question, err := adapter.repository.GetQuestion(
		ctx,
		conversationActor(actor),
		questionID,
	)
	if err != nil {
		return conversation.QuestionSpeech{}, mapConversationError(err)
	}
	return adapter.speech.SynthesizeQuestion(ctx, question.Content)
}

type voiceCheckpointAdapter struct {
	repository  conversationpersistence.PersistenceStore
	audioAssets *conversation.AudioAssetService
}

func (adapter *voiceCheckpointAdapter) LatestTurn(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (conversation.ConfirmedVoiceTurn, bool, error) {
	turns, err := adapter.repository.ListSessionTurns(
		ctx,
		conversationActor(actor),
		sessionID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, false,
			mapConversationError(err)
	}
	if len(turns) == 0 {
		return conversation.ConfirmedVoiceTurn{}, false, nil
	}
	persistedTurn := turns[len(turns)-1]
	candidate, err := adapter.repository.GetCandidate(
		ctx,
		conversationActor(actor),
		persistedTurn.CandidateID,
	)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, false,
			mapConversationError(err)
	}
	turn, err := mapVoiceTurnWithCandidate(persistedTurn, candidate)
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, false, err
	}
	if adapter.audioAssets == nil {
		return turn, true, nil
	}
	asset, err := adapter.audioAssets.GetReadableByTurn(
		ctx,
		conversation.AudioAssetActor{UserID: actor.UserID},
		turn.ID,
	)
	if errors.Is(err, conversation.ErrAudioAssetNotFound) ||
		errors.Is(err, conversation.ErrAudioAssetInvalidTransition) {
		return turn, true, nil
	}
	if err != nil {
		return conversation.ConfirmedVoiceTurn{}, false, err
	}
	turn.AudioAssetID = asset.ID
	return turn, true, nil
}

type voiceReviewAdapter struct {
	service      *review.EnsureService
	history      *review.HistoryService
	sourceReader review.ReviewSourceReader
}

func (adapter *voiceReviewAdapter) EnsureSessionReview(
	ctx context.Context,
	actor requestcontext.Actor,
	source agent.VoiceReviewSource,
) (agent.VoiceReviewCheckpoint, error) {
	reviewActor := review.Actor{
		UserID: actor.UserID,
	}
	snapshot, err := adapter.sourceReader.ReadReviewSource(
		ctx,
		reviewActor,
		source.SessionID,
	)
	if err != nil {
		return agent.VoiceReviewCheckpoint{}, mapReviewError(err)
	}
	if snapshot.SourceTurnID != source.TurnID ||
		snapshot.PracticeSessionID != source.SessionID {
		return agent.VoiceReviewCheckpoint{}, agent.ErrInvalidContext
	}
	formalReview, err := adapter.service.EnsureReview(
		ctx,
		review.EnsureReviewCommand{
			Actor:                     reviewActor,
			PracticeSessionID:         source.SessionID,
			ImplementationVersion:     voiceReviewImplementation,
			SourceTurnID:              snapshot.SourceTurnID,
			SourceTurnVersion:         snapshot.SourceTurnVersion,
			SourceManifestFingerprint: snapshot.ManifestFingerprint,
		},
	)
	if err != nil {
		return agent.VoiceReviewCheckpoint{}, mapReviewError(err)
	}
	return agent.VoiceReviewCheckpoint{
		ID:           formalReview.ID,
		SessionID:    formalReview.PracticeSessionID,
		SourceTurnID: formalReview.SourceTurnID,
	}, nil
}

func (adapter *voiceReviewAdapter) GetReview(
	ctx context.Context,
	actor requestcontext.Actor,
	reviewID string,
) (agent.VoiceSessionReview, error) {
	formalReview, err := adapter.history.Get(ctx, review.Actor{
		UserID: actor.UserID,
	}, reviewID)
	if err != nil {
		return agent.VoiceSessionReview{}, mapReviewReadError(err)
	}
	return mapVoiceSessionReview(formalReview), nil
}

func (adapter *voiceReviewAdapter) ListReviews(
	ctx context.Context,
	actor requestcontext.Actor,
	query agent.VoiceReviewHistoryQuery,
) (agent.VoiceReviewHistoryPage, error) {
	reviewQuery := review.HistoryQuery{Limit: query.Limit}
	if query.Before != nil {
		reviewQuery.Before = &review.HistoryCursor{
			CreatedAt: query.Before.CreatedAt,
			ReviewID:  query.Before.ReviewID,
		}
	}
	page, err := adapter.history.ListCompleted(
		ctx,
		review.Actor{UserID: actor.UserID},
		reviewQuery,
	)
	if err != nil {
		return agent.VoiceReviewHistoryPage{}, mapReviewReadError(err)
	}
	result := agent.VoiceReviewHistoryPage{
		Items: make([]agent.VoiceSessionReview, len(page.Items)),
	}
	for index, item := range page.Items {
		result.Items[index] = mapVoiceSessionReview(item)
	}
	if page.Next != nil {
		result.Next = &agent.VoiceReviewHistoryCursor{
			CreatedAt: page.Next.CreatedAt,
			ReviewID:  page.Next.ReviewID,
		}
	}
	return result, nil
}

func mapReviewReadError(err error) error {
	if errors.Is(err, review.ErrInvalidReview) {
		return agent.ErrInvalidContext
	}
	return mapReviewError(err)
}

func mapReviewError(err error) error {
	var generationError *ai.GenerationError
	if errors.As(err, &generationError) {
		return err
	}
	var categorized review.StableGenerationError
	if errors.As(err, &categorized) {
		kind := ai.ErrorKind(strings.TrimSpace(
			categorized.StableCategory(),
		))
		switch kind {
		case ai.ErrorInvalidRequest,
			ai.ErrorConfiguration,
			ai.ErrorAuthentication,
			ai.ErrorAuthorization,
			ai.ErrorQuotaExhausted,
			ai.ErrorRateLimited,
			ai.ErrorTimeout,
			ai.ErrorProviderUnavailable,
			ai.ErrorInvalidResponse,
			ai.ErrorCancelled:
			return ai.NewGenerationError(
				kind,
				0,
				"",
				"",
				review.ErrGenerationFailed,
			)
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, review.ErrInvalidReview):
		return agent.ErrInvalidRequest
	case errors.Is(err, review.ErrReviewNotFound),
		errors.Is(err, review.ErrAccountDeleted):
		return agent.ErrNotFound
	case errors.Is(err, review.ErrReviewSourceConflict),
		errors.Is(err, review.ErrReviewImplementationConflict),
		errors.Is(err, review.ErrGenerationClaimLost),
		errors.Is(err, review.ErrDeletionGenerationStale):
		return agent.ErrConflict
	case errors.Is(err, review.ErrGenerationFailed):
		return ai.NewGenerationError(
			ai.ErrorProviderUnavailable,
			0,
			"",
			"",
			review.ErrGenerationFailed,
		)
	default:
		return err
	}
}

func mapVoiceSessionReview(
	formalReview review.FormalReview,
) agent.VoiceSessionReview {
	item := agent.VoiceSessionReview{
		ID:                    formalReview.ID,
		SessionID:             formalReview.PracticeSessionID,
		Status:                string(formalReview.Status),
		ImplementationVersion: formalReview.ImplementationVersion,
		SourceTurnID:          formalReview.SourceTurnID,
		SourceTurnVersion:     formalReview.SourceTurnVersion,
		CreatedAt:             formalReview.CreatedAt,
		UpdatedAt:             formalReview.UpdatedAt,
		CompletedAt:           formalReview.CompletedAt,
	}
	if formalReview.Result == nil {
		return item
	}
	conclusions := make(
		[]agent.VoiceReviewConclusion,
		len(formalReview.Result.Conclusions),
	)
	for index, conclusion := range formalReview.Result.Conclusions {
		conclusions[index] = agent.VoiceReviewConclusion{
			Key:        conclusion.Key,
			Category:   conclusion.Category,
			Message:    conclusion.Message,
			Suggestion: conclusion.Suggestion,
		}
	}
	item.Result = &agent.VoiceReviewResult{
		OverallScore: formalReview.Result.OverallScore,
		Summary:      formalReview.Result.Summary,
		Conclusions:  conclusions,
	}
	return item
}

type voiceReviewSourceReader struct {
	conversations conversationpersistence.PersistenceStore
	practice      practicepersistence.Repository
}

func (reader *voiceReviewSourceReader) ReadReviewSource(
	ctx context.Context,
	actor review.Actor,
	practiceSessionID string,
) (review.ReviewSourceSnapshot, error) {
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor.UserID != actor.UserID {
		return review.ReviewSourceSnapshot{}, review.ErrInvalidReview
	}
	conversationActor := conversationpersistence.Actor{
		UserID:    actor.UserID,
		SessionID: trustedActor.SessionID,
	}
	turns, err := reader.conversations.ListSessionTurns(
		ctx,
		conversationActor,
		practiceSessionID,
	)
	if err != nil {
		return review.ReviewSourceSnapshot{}, mapConversationError(err)
	}
	session, err := reader.practice.GetSession(
		ctx,
		practicepersistence.Actor{
			UserID:    actor.UserID,
			SessionID: trustedActor.SessionID,
		},
		practiceSessionID,
	)
	if err != nil {
		return review.ReviewSourceSnapshot{}, mapPracticeError(err)
	}
	if session.Status != practicepersistence.SessionStatusCompleted ||
		len(turns) != session.Snapshot.TurnLimit {
		return review.ReviewSourceSnapshot{}, review.ErrInvalidReview
	}
	sources := make([]review.SourceObject, 0, len(turns))
	for _, turn := range turns {
		question, questionErr := reader.conversations.GetQuestion(
			ctx,
			conversationActor,
			turn.QuestionID,
		)
		if questionErr != nil {
			return review.ReviewSourceSnapshot{},
				mapConversationError(questionErr)
		}
		snapshot, marshalErr := json.Marshal(struct {
			QuestionID              string   `json:"question_id"`
			QuestionText            string   `json:"question_text"`
			QuestionSpeakerID       string   `json:"question_speaker_participant_id"`
			AddresseeParticipantIDs []string `json:"addressee_participant_ids"`
			RespondentParticipantID string   `json:"respondent_participant_id"`
			AnswerText              string   `json:"answer_text"`
		}{
			QuestionID:              turn.QuestionID,
			QuestionText:            question.Content,
			QuestionSpeakerID:       turn.SpeakerParticipantID,
			AddresseeParticipantIDs: turn.AddresseeParticipantIDs,
			RespondentParticipantID: turn.RespondentParticipantID,
			AnswerText:              turn.AnswerText,
		})
		if marshalErr != nil {
			return review.ReviewSourceSnapshot{}, review.ErrInvalidReview
		}
		checksum := sha256.Sum256(snapshot)
		sources = append(sources, review.SourceObject{
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      turn.ID,
			SourceVersion: turnEvidenceVersion(turn.EvidenceVersion),
			Checksum:      hex.EncodeToString(checksum[:]),
			Snapshot:      snapshot,
		})
	}
	trigger := turns[len(turns)-1]
	fingerprint := reviewManifestFingerprint(
		session.Version,
		sources,
	)
	return review.ReviewSourceSnapshot{
		PracticeSessionID:   practiceSessionID,
		SessionVersion:      fmt.Sprintf("practice-session:v%d", session.Version),
		SourceTurnID:        trigger.ID,
		SourceTurnVersion:   turnEvidenceVersion(trigger.EvidenceVersion),
		ManifestFingerprint: fingerprint,
		Sources:             sources,
	}, nil
}

type voiceReviewGenerator struct {
	generator ai.TextGenerator
	timeout   time.Duration
}

func (generator *voiceReviewGenerator) GenerateReview(
	ctx context.Context,
	input review.ReviewGenerationInput,
) (review.GeneratedReview, error) {
	generationContext, cancel := context.WithTimeout(ctx, generator.timeout)
	defer cancel()
	providerEvidence := make([]struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}, 0, len(input.Source.Sources))
	for _, source := range input.Source.Sources {
		var snapshot struct {
			Question string `json:"question_text"`
			Answer   string `json:"answer_text"`
		}
		if err := json.Unmarshal(source.Snapshot, &snapshot); err != nil ||
			strings.TrimSpace(snapshot.Question) == "" ||
			strings.TrimSpace(snapshot.Answer) == "" {
			return review.GeneratedReview{}, review.ErrInvalidReview
		}
		providerEvidence = append(providerEvidence, struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		}{
			Question: snapshot.Question,
			Answer:   snapshot.Answer,
		})
	}
	sourceJSON, err := json.Marshal(providerEvidence)
	if err != nil {
		return review.GeneratedReview{}, err
	}
	result, err := generator.generator.Generate(
		generationContext,
		ai.TextRequest{Messages: []ai.TextMessage{
			{
				Role:    ai.TextRoleSystem,
				Content: "You are an English coach. Return only valid JSON with this shape: {\"overall_score\":0,\"summary\":\"...\",\"conclusions\":[{\"key\":\"overall\",\"category\":\"...\",\"message\":\"...\",\"suggestion\":\"...\"}]}. Score must be 0-100 and every string must be non-empty.",
			},
			{
				Role: ai.TextRoleUser,
				Content: fmt.Sprintf(
					"Review these %d confirmed interview answers. Base every conclusion only on this evidence: %s",
					len(providerEvidence),
					sourceJSON,
				),
			},
		}},
	)
	if err != nil {
		return review.GeneratedReview{}, err
	}
	var reviewResult review.ReviewResult
	if err := json.Unmarshal(
		[]byte(stripJSONFence(result.Content)),
		&reviewResult,
	); err != nil {
		return review.GeneratedReview{}, err
	}
	links := make(
		[]review.EvidenceLink,
		0,
		len(reviewResult.Conclusions)*len(input.Source.Sources),
	)
	for _, conclusion := range reviewResult.Conclusions {
		for _, source := range input.Source.Sources {
			links = append(links, review.EvidenceLink{
				ConclusionKey: conclusion.Key,
				SourceType:    source.SourceType,
				SourceID:      source.SourceID,
				SourceVersion: source.SourceVersion,
			})
		}
	}
	return review.GeneratedReview{
		Result:        reviewResult,
		EvidenceLinks: links,
	}, nil
}

func stableVoiceID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

func turnEvidenceVersion(version int64) string {
	return fmt.Sprintf("conversation-turn:evidence-v%d", version)
}

func reviewManifestFingerprint(
	sessionVersion int,
	sources []review.SourceObject,
) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "practice-session:v%d", sessionVersion)
	for _, source := range sources {
		_, _ = fmt.Fprintf(
			hash,
			"\x00%s\x00%s\x00%s\x00%s",
			source.SourceType,
			source.SourceID,
			source.SourceVersion,
			source.Checksum,
		)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func stripJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```json") {
		value = strings.TrimPrefix(value, "```json")
		value = strings.TrimSuffix(value, "```")
	}
	return strings.TrimSpace(value)
}

var (
	_ agent.VoicePracticePort      = (*voicePracticeAdapter)(nil)
	_ agent.VoiceSessionPort       = (*voicePracticeAdapter)(nil)
	_ conversation.VoiceRoundStore = (*voiceConversationStore)(nil)
	_ agent.VoiceQuestionPort      = (*voiceQuestionAdapter)(nil)
	_ agent.VoiceCheckpointPort    = (*voiceCheckpointAdapter)(nil)
	_ agent.VoiceReviewPort        = (*voiceReviewAdapter)(nil)
	_ agent.VoiceReviewReader      = (*voiceReviewAdapter)(nil)
	_ review.ReviewSourceReader    = (*voiceReviewSourceReader)(nil)
	_ review.ReviewGenerator       = (*voiceReviewGenerator)(nil)
)
