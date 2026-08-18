package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentaudiohttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/audio/http"
	agentconversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	summarypostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary/postgres"
	agenttranslation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/translation"
	agenttranslationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/translation/http"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentimagehttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image/http"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentvoicehttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/http"
	voicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	agentrunhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/http"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	coachingagentinstruction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/agentinstruction"
	evaluationagentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agentvoice"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	practiceinteractionhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction/http"
	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	profileagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile/agentcapability"
	profileagentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile/agentcontext"
	profilepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile/postgres"
	profilehttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile/transport/http"
	reviewagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	identityavatar "github.com/1024XEngineer/XE3-ESL/server/internal/identity/avatar"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewIdentityAndAgentModules builds the production Identity, Agent data, and
// text-generation composition. An optional voice composition is enabled only
// when every owning module supplies its explicit Port; there is no Fake
// fallback.
func NewIdentityAndAgentModules(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*identity.Module, RouteRegistrar, error) {
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].Media.ObjectStore != nil {
		return nil, nil, errors.New(
			"bootstrap: Agent voice input requires the cleanup-aware composition",
		)
	}
	composition, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		nil,
		nil,
		nil,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := composition.recoverInterruptedRuns(ctx); err != nil {
		return nil, nil, err
	}
	return composition.identity.module, composition.agentModule, nil
}

type identityAgentComposition struct {
	identity         *identityComposition
	agentModule      RouteRegistrar
	agentService     *agentconversation.Service
	conversationData *conversationpostgres.Repository
	mediaReclaimer   MediaObjectReclaimer
	mediaService     *sharedmedia.Service
	productionTools  *capability.Registry
	runService       *agentrun.Service
	summaryProcessor agentsummary.Processor
	ids              *identity.UUIDv4Generator
}

// MediaObjectReclaimer is the single shared object cleanup capability retained
// by the production composition.
type MediaObjectReclaimer interface {
	Reclaim(context.Context, int) (sharedmedia.CleanupResult, error)
}

func buildIdentityAgentComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	resumeConfiguration *InterviewResumeConfiguration,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*identityAgentComposition, error) {
	if ctx == nil || database == nil || modelProviders.Run == nil ||
		modelProviders.Summary == nil ||
		modelProviders.Translation == nil ||
		modelProviders.PracticeTurnIntent == nil ||
		len(voiceConfigurations) > 1 {
		return nil, errors.New(
			"bootstrap: Agent Run dependencies are required",
		)
	}
	realtimeRecognizer := agentRealtimePCMRecognizer(voiceConfigurations)
	// 1. 装配身份与 Conversation 主链。
	identityContext, err := buildIdentityComposition(
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
	)
	if err != nil {
		return nil, err
	}
	ids := identity.NewUUIDv4Generator(nil)
	conversationRepository, err := conversationpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	agentService, err := agentconversation.NewService(conversationRepository)
	if err != nil {
		return nil, err
	}
	messageTranslation, err := agenttranslation.NewService(
		conversationRepository,
		modelProviders.Translation,
	)
	if err != nil {
		return nil, err
	}
	agentMedia, err := buildAgentMediaApplication(
		imageConfiguration,
		resumeConfiguration,
		voiceConfigurations,
		database,
		ids,
	)
	if err != nil {
		return nil, err
	}
	agentImages, err := buildAgentImageApplication(
		imageConfiguration,
		agentMedia,
		agentService,
		database,
	)
	if err != nil {
		return nil, err
	}
	// 2. 注册跨会话 Profile 与复盘业务工具。
	evaluationStore, err := evaluationpostgres.NewStore(database)
	if err != nil {
		return nil, err
	}
	profileRepository, err := profilepostgres.New(database)
	if err != nil {
		return nil, err
	}
	profileService, err := coachingprofile.NewService(profileRepository, time.Now)
	if err != nil {
		return nil, err
	}
	profileContext, err := profileagentcontext.New(profileService)
	if err != nil {
		return nil, err
	}
	reviewTools, err := reviewagentcapability.NewServicePort(evaluationStore)
	if err != nil {
		return nil, err
	}
	productionTools, err := capability.NewRegistry(
		profileagentcapability.NewShowTool(profileService),
		profileagentcapability.NewUpdateTool(
			profileService,
			conversationRepository,
		),
		profileagentcapability.NewForgetTool(
			profileService,
			conversationRepository,
		),
		profileagentcapability.NewMemoryTool(
			profileService,
			conversationRepository,
		),
		reviewagentcapability.NewReviewSearchTool(reviewTools),
		reviewagentcapability.NewReviewGetTool(reviewTools),
	)
	if err != nil {
		return nil, err
	}
	// 3. 装配结构化 Profile 与 Thread Context。
	var contextOptions []agentcontext.Option
	if agentImages != nil {
		contextOptions = append(
			contextOptions,
			agentcontext.WithImageReader(agentImages),
		)
	}
	contextRepository, err := contextpostgres.New(database)
	if err != nil {
		return nil, err
	}
	contextAssembler, err := agentcontext.NewAssembler(
		contextRepository,
		coachingagentinstruction.Provider{},
		profileContext,
		contextOptions...,
	)
	if err != nil {
		return nil, err
	}
	// 4. 装配 Agent Run Service。
	toolOptions, err := agentRunServiceOptions(productionTools)
	if err != nil {
		return nil, err
	}
	runRepository, err := runpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	var runStore agentrun.Repository = runRepository
	notifiers := make([]interface{ Notify() }, 0, 1)
	if summaryNotifier != nil {
		notifiers = append(notifiers, summaryNotifier)
	}
	if len(notifiers) > 0 {
		runStore = &runCompletionNotifyingRepository{
			Repository: runStore,
			notifiers:  notifiers,
		}
	}
	runOptions := append([]agentrun.Option(nil), toolOptions.runOptions...)
	if agentImages != nil {
		imageSubmissions, imageSubmissionErr :=
			runpostgres.NewImageSubmissionRepository(database, ids)
		if imageSubmissionErr != nil {
			return nil, imageSubmissionErr
		}
		runOptions = append(
			runOptions,
			agentrun.WithImageSubmissions(imageSubmissions),
		)
	}
	runService, err := agentrun.NewService(
		runStore,
		conversationRepository,
		contextAssembler,
		modelProviders.Run,
		runConfiguration,
		runOptions...,
	)
	if err != nil {
		return nil, err
	}
	// 5. 装配 Thread 当前摘要；标题由首条用户消息确定性生成。
	summaryRepository, err := summarypostgres.New(database)
	if err != nil {
		return nil, err
	}
	summaryProcessor, err := agentsummary.NewProcessor(
		summaryRepository,
		modelProviders.Summary,
		runConfiguration.Provider,
		runConfiguration.Model,
		runConfiguration.MaxInputCharacters,
	)
	if err != nil {
		return nil, err
	}
	voiceRunProcessor := agentvoice.PendingRunProcessor(runService)
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoice.InputEnabled {
		voiceRunProcessor, err = agentvoice.NewDeferredRunProcessor(
			ctx,
			runService,
			slog.Default(),
		)
		if err != nil {
			return nil, err
		}
	}
	var voiceInputRepository agentvoice.Repository
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoice.InputEnabled {
		voiceInputRepository, err = voicepostgres.New(database, ids)
		if err != nil {
			return nil, err
		}
	}
	var agentMessageFeedback *evaluationagentvoice.Feedback
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoice.MessageFeedback != nil {
		if voiceInputRepository == nil {
			return nil, errors.New(
				"bootstrap: Agent message feedback requires Agent voice input",
			)
		}
		agentMessageFeedback, err = evaluationagentvoice.NewFeedback(
			voiceConfigurations[0].AgentVoice.MessageFeedback,
			voiceInputRepository,
		)
		if err != nil {
			return nil, err
		}
	}
	voiceInput, err := buildAgentVoiceInputApplication(
		voiceConfigurations,
		voiceInputRepository,
		voiceRunProcessor,
		agentMedia,
		runConfiguration,
		agentMessageFeedback,
	)
	if err != nil {
		return nil, err
	}
	var voiceApplication *practiceinteraction.SessionApplication
	var sameQuestionRetry *practiceinteraction.SameQuestionRetryApplication
	var practiceRecordings *practiceinteraction.RecordingService
	if len(voiceConfigurations) == 1 {
		schedulers := voiceConfigurations[0].PracticeInteraction.Evaluation
		voiceApplication, sameQuestionRetry, practiceRecordings, err =
			buildPracticeInteractionApplication(
				database,
				voiceConfigurations[0].PracticeInteraction,
				schedulers.Completion,
				schedulers.TurnFeedback,
				schedulers.FeedbackReader,
				ids,
				agentMedia,
				voiceConfigurations[0].Media.ObjectStore != nil,
			)
		if err != nil {
			return nil, err
		}
	}
	// 6. 装配 HTTP Handler 与受保护路由。
	errorRenderer := httpresponse.NewRenderer(nil)
	profileHTTP, err := profilehttp.New(profileService, errorRenderer)
	if err != nil {
		return nil, err
	}
	conversationHTTPOptions := []agentconversationhttp.Option{
		agentconversationhttp.WithClientActions(runService),
	}
	if agentImages != nil {
		conversationHTTPOptions = append(
			conversationHTTPOptions,
			agentconversationhttp.WithMessageImages(agentImages),
		)
	}
	if agentMessageFeedback != nil {
		conversationHTTPOptions = append(
			conversationHTTPOptions,
			agentconversationhttp.WithSpeechFeedback(agentMessageFeedback),
		)
	}
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoice.AssistantSpeech != nil {
		conversationHTTPOptions = append(
			conversationHTTPOptions,
			agentconversationhttp.WithAssistantSpeech(
				voiceConfigurations[0].AgentVoice.AssistantSpeech,
			),
		)
	}
	conversationHTTP, err := agentconversationhttp.NewHandler(
		agentService,
		errorRenderer,
		conversationHTTPOptions...,
	)
	if err != nil {
		return nil, err
	}
	translationHTTP, err := agenttranslationhttp.NewHandler(
		messageTranslation,
		errorRenderer,
	)
	if err != nil {
		return nil, err
	}
	runHTTP, err := agentrunhttp.NewHandler(runService, errorRenderer)
	if err != nil {
		return nil, err
	}
	registrars := []ProtectedRouteRegistrar{
		profileHTTP,
		conversationHTTP,
		translationHTTP,
		runHTTP,
	}
	if imageConfiguration != nil {
		avatarRepository, avatarErr := identityavatar.NewPostgresRepository(database)
		if avatarErr != nil {
			return nil, avatarErr
		}
		avatarService, avatarErr := identityavatar.NewService(
			avatarRepository,
			agentMedia,
			identityavatar.Config{StagedTTL: imageConfiguration.StagedTTL},
		)
		if avatarErr != nil {
			return nil, avatarErr
		}
		avatarHTTP, avatarErr := identityavatar.NewHandler(avatarService, errorRenderer)
		if avatarErr != nil {
			return nil, avatarErr
		}
		registrars = append(registrars, avatarHTTP)
	}
	if realtimeRecognizer != nil {
		ephemeralHTTP, ephemeralHTTPErr :=
			agentvoicehttp.NewEphemeralTranscriptionHandler(
				realtimeRecognizer,
				agentService,
				voiceConfigurations[0].AgentVoice.ReadTimeout,
				errorRenderer,
			)
		if ephemeralHTTPErr != nil {
			return nil, ephemeralHTTPErr
		}
		registrars = append(registrars, ephemeralHTTP)
	}
	if agentImages != nil {
		imageHTTP, imageHTTPErr := agentimagehttp.NewHandler(
			agentImages,
			agentService,
			0,
			errorRenderer,
		)
		if imageHTTPErr != nil {
			return nil, imageHTTPErr
		}
		registrars = append(registrars, imageHTTP)
	}
	if voiceInput != nil {
		voiceHTTP, voiceHTTPErr := agentvoicehttp.NewHandler(
			voiceInput,
			agentService,
			voiceConfigurations[0].AgentVoice.ReadTimeout,
			errorRenderer,
		)
		if voiceHTTPErr != nil {
			return nil, voiceHTTPErr
		}
		audioHTTP, audioHTTPErr := agentaudiohttp.NewHandler(
			voiceInput,
			errorRenderer,
		)
		if audioHTTPErr != nil {
			return nil, audioHTTPErr
		}
		registrars = append(registrars, voiceHTTP, audioHTTP)
	}
	if voiceApplication != nil {
		practiceHTTP, practiceHTTPErr := practiceinteractionhttp.NewHandler(
			voiceApplication,
			practiceinteractionhttp.Options{
				RealtimeReadTimeout: voiceConfigurations[0].PracticeInteraction.RealtimeReadTimeout,
				RecordedReadTimeout: voiceConfigurations[0].PracticeInteraction.RecordedReadTimeout,
				SameQuestionRetry:   sameQuestionRetry,
				Recordings:          practiceRecordings,
				RealtimeSpeech:      voiceConfigurations[0].AgentVoice.AssistantSpeech,
			},
			errorRenderer,
		)
		if practiceHTTPErr != nil {
			return nil, practiceHTTPErr
		}
		registrars = append(registrars, practiceHTTP)
	}
	handler := &bearerProtectedRoutes{
		authentication: identityContext.handler.AuthenticationMiddleware(),
		registrars:     registrars,
	}
	var mediaReclaimer MediaObjectReclaimer
	if agentMedia != nil {
		mediaReclaimer = agentMedia
	}
	return &identityAgentComposition{
		identity:         identityContext,
		agentModule:      handler,
		agentService:     agentService,
		conversationData: conversationRepository,
		mediaReclaimer:   mediaReclaimer,
		mediaService:     agentMedia,
		productionTools:  toolOptions.productionRegistry,
		runService:       runService,
		summaryProcessor: summaryProcessor,
		ids:              ids,
	}, nil
}

func agentRealtimePCMRecognizer(
	configurations []RuntimeAudioConfiguration,
) agentvoice.PCMStreamingSpeechRecognizer {
	if len(configurations) != 1 || configurations[0].AgentVoice.Recognizer == nil {
		return nil
	}
	recognizer, _ := configurations[0].AgentVoice.Recognizer.(agentvoice.PCMStreamingSpeechRecognizer)
	return recognizer
}

func buildAgentImageApplication(
	configuration *AgentImageConfiguration,
	mediaService *sharedmedia.Service,
	threads *agentconversation.Service,
	database *pgxpool.Pool,
) (*agentimage.Service, error) {
	if configuration == nil {
		return nil, nil
	}
	if mediaService == nil || threads == nil {
		return nil, errors.New(
			"bootstrap: Agent image media dependencies are required",
		)
	}
	return agentimage.NewService(
		mediaService,
		threads,
		database,
		agentimage.Config{
			StagedTTL: configuration.StagedTTL,
		},
		slog.Default(),
	)
}

func buildAgentMediaApplication(
	imageConfiguration *AgentImageConfiguration,
	resumeConfiguration *InterviewResumeConfiguration,
	voiceConfigurations []RuntimeAudioConfiguration,
	database *pgxpool.Pool,
	ids sharedmedia.IDGenerator,
) (*sharedmedia.Service, error) {
	stores := sharedmedia.Stores{}
	uploadLease := time.Duration(0)
	if imageConfiguration != nil {
		stores.Images = imageConfiguration.ObjectStore
		uploadLease = imageConfiguration.UploadLease
	}
	if resumeConfiguration != nil {
		stores.Documents = resumeConfiguration.ObjectStore
		resumeUploadLease := resumeConfiguration.UploadLease
		if uploadLease != 0 && uploadLease != resumeUploadLease {
			return nil, errors.New(
				"bootstrap: media upload lease configurations conflict",
			)
		}
		uploadLease = resumeUploadLease
	}
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].Media.ObjectStore != nil {
		stores.Audio = voiceConfigurations[0].Media.ObjectStore
		voiceUploadLease := voiceConfigurations[0].Media.UploadLease
		if uploadLease != 0 && uploadLease != voiceUploadLease {
			return nil, errors.New(
				"bootstrap: Agent media upload lease configurations conflict",
			)
		}
		uploadLease = voiceUploadLease
	}
	if stores.Images == nil && stores.Audio == nil && stores.Documents == nil {
		return nil, nil
	}
	repository, err := mediapostgres.New(database)
	if err != nil {
		return nil, err
	}
	return sharedmedia.NewService(
		repository,
		stores,
		ids,
		sharedmedia.Config{
			UploadLease:  uploadLease,
			CleanupLease: 5 * time.Minute,
			PlaybackTTL:  2 * time.Minute,
			CleanupBatch: 8,
		},
	)
}

func (composition *identityAgentComposition) recoverInterruptedRuns(
	ctx context.Context,
) error {
	if composition == nil || composition.runService == nil {
		return errors.New("bootstrap: Agent Run service is required")
	}
	_, err := composition.runService.RecoverInterrupted(ctx)
	return err
}

func buildAgentVoiceInputApplication(
	voiceConfigurations []RuntimeAudioConfiguration,
	repository agentvoice.Repository,
	runs agentvoice.PendingRunProcessor,
	mediaService *sharedmedia.Service,
	runConfiguration agentrun.Configuration,
	feedback agentvoice.FeedbackPort,
) (*agentvoice.Service, error) {
	if len(voiceConfigurations) == 0 ||
		!voiceConfigurations[0].AgentVoice.InputEnabled {
		return nil, nil
	}
	runtime := voiceConfigurations[0]
	configuration := runtime.AgentVoice
	if runtime.Media.ObjectStore == nil || mediaService == nil || feedback == nil {
		return nil, errors.New(
			"bootstrap: Agent voice input media and feedback are required",
		)
	}
	var client *http.Client
	if configuration.ReadTimeout > 0 {
		client = &http.Client{
			Timeout: configuration.ReadTimeout,
			CheckRedirect: func(
				*http.Request,
				[]*http.Request,
			) error {
				return http.ErrUseLastResponse
			},
		}
	}
	sources, err := agentvoice.NewSignedAudioLoader(
		runtime.Media.ObjectStore,
		client,
		configuration.ScratchDirectory,
		configuration.ObjectReadAllowedHosts,
	)
	if err != nil {
		return nil, err
	}
	return agentvoice.NewService(
		repository,
		mediaService,
		sources,
		configuration.Recognizer,
		configuration.Synthesizer,
		runs,
		agentvoice.Config{
			Configuration:    runConfiguration,
			ScratchDirectory: configuration.ScratchDirectory,
			DraftTTL:         configuration.StagedTTL,
			ASRLease:         configuration.ASRLease,
		},
		feedback,
	)
}
