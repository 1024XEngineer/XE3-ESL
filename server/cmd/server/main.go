package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ieltsdata "github.com/1024XEngineer/XE3-ESL/server/data/ielts"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceavatar "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/avatar"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	reviewpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/database"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/spatius"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	shutdownTimeout                = 5 * time.Second
	voiceAudioUploadLease          = 2 * time.Minute
	voiceASRFinalizationTimeMargin = 15 * time.Second
)

type httpServerResult struct {
	name string
	err  error
}

func main() {
	os.Exit(run())
}

func run() int {
	if err := config.LoadDotEnvUpwards(); err != nil {
		_, _ = os.Stderr.WriteString("load .env failed: " + err.Error() + "\n")
		return 1
	}
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)
	metricsRegistry := prometheus.NewRegistry()
	providerObserver, err := providerobservability.New(metricsRegistry)
	if err != nil {
		logger.Error("provider observability startup failed")
		return 1
	}
	providerFactory, err := app.NewProviderFactory(providerObserver)
	if err != nil {
		logger.Error("provider composition startup failed")
		return 1
	}
	textConfig, err := config.LoadTextGeneration()
	if err != nil {
		logger.Error("text generation configuration failed")
		return 1
	}
	agentRunConfig, err := config.LoadAgentRun(textConfig.Timeout)
	if err != nil {
		logger.Error("Agent Run configuration failed")
		return 1
	}
	modelProviders, err := providerFactory.AgentModelProviders(textConfig)
	if err != nil {
		logger.Error("text generation startup failed")
		return 1
	}
	preparationJobTargets, err :=
		providerFactory.PreparationJobTargetGenerator(textConfig)
	if err != nil {
		logger.Error("Preparation job target generation startup failed")
		return 1
	}
	evaluationScoringGenerator, err :=
		providerFactory.EvaluationScoringGenerator(textConfig)
	if err != nil {
		logger.Error("evaluation scoring startup failed")
		return 1
	}
	evaluationSpeechFeedbackGenerator, err :=
		providerFactory.EvaluationSpeechFeedbackGenerator(textConfig)
	if err != nil {
		logger.Error("evaluation speech feedback startup failed")
		return 1
	}
	asrConfig, err := config.LoadSpeechRecognition()
	if err != nil {
		logger.Error("speech recognition configuration failed")
		return 1
	}
	ttsConfig, err := config.LoadSpeechSynthesis()
	if err != nil {
		logger.Error("speech synthesis configuration failed")
		return 1
	}
	recognizer, err := providerFactory.AgentSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("speech recognition startup failed")
		return 1
	}
	synthesizer, err := providerFactory.AgentSpeechSynthesizer(ttsConfig)
	if err != nil {
		logger.Error("speech synthesis startup failed")
		return 1
	}
	practiceRecognizer, err := providerFactory.PracticeSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("Practice speech recognition startup failed")
		return 1
	}
	practiceRecordedRecognizer, err :=
		providerFactory.PracticeRecordedSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("recorded Practice speech recognition startup failed")
		return 1
	}
	practiceSynthesizer, err := providerFactory.PracticeSpeechSynthesizer(ttsConfig)
	if err != nil {
		logger.Error("Practice speech synthesis startup failed")
		return 1
	}
	practiceQuestions, err := providerFactory.PracticeQuestionGenerator(textConfig)
	if err != nil {
		logger.Error("Practice question generation startup failed")
		return 1
	}
	practiceAnswerTips, err := providerFactory.PracticeAnswerTipGenerator(textConfig)
	if err != nil {
		logger.Error("Practice answer Tip generation startup failed")
		return 1
	}
	temporaryAudioConfig, err := config.LoadTemporaryAudio()
	if err != nil {
		logger.Error("temporary audio configuration failed")
		return 1
	}
	audioVault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory:              ttsConfig.TempDirectory,
			Lifetime:                      temporaryAudioConfig.Lifetime,
			MaxItems:                      temporaryAudioConfig.MaxItems,
			MaxBytes:                      temporaryAudioConfig.MaxBytes,
			MaxItemsPerActor:              temporaryAudioConfig.MaxItemsPerUser,
			MaxBytesPerActor:              temporaryAudioConfig.MaxBytesPerUser,
			MaxConcurrentCaptures:         temporaryAudioConfig.MaxConcurrentCaptures,
			MaxConcurrentCapturesPerActor: temporaryAudioConfig.MaxConcurrentCapturesPerUser,
		},
	)
	if err != nil {
		logger.Error("temporary audio startup failed")
		return 1
	}
	defer audioVault.Close()

	storageConfig, err := config.LoadObjectStorage()
	if err != nil {
		logger.Error(
			"object storage configuration invalid",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}
	spatiusConfig, err := config.LoadSpatius()
	if err != nil {
		logger.Error("Practice avatar configuration invalid")
		return 1
	}
	resumeOCRConfig, err := config.LoadResumeOCR()
	if err != nil {
		logger.Error(
			"Resume OCR configuration invalid",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}
	agentVoiceObjectReadHosts, err :=
		app.AgentVoiceObjectReadAllowedHosts(storageConfig)
	if err != nil {
		logger.Error(
			"agent voice object storage configuration invalid",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	databasePool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database startup failed", slog.Any("error", err))
		return 1
	}
	defer databasePool.Close()
	evaluationPolicies := evaluation.NewPolicyRegistry()
	sceneCatalog, err := scene.NewBuiltinCatalog(evaluationPolicies)
	if err != nil {
		logger.Error("Scene catalog startup failed", slog.Any("error", err))
		return 1
	}
	ieltsCatalogFile, err := ieltsdata.Files.Open(ieltsdata.CurrentFile)
	if err != nil {
		logger.Error("IELTS question bank asset unavailable", slog.Any("error", err))
		return 1
	}
	ieltsQuestionBank, err := ielts.LoadCatalog(ieltsCatalogFile)
	_ = ieltsCatalogFile.Close()
	if err != nil {
		logger.Error("IELTS question bank startup failed", slog.Any("error", err))
		return 1
	}
	ieltsSpeechService, err := ielts.NewSpeechService(
		ieltsQuestionBank,
		ieltsSpeechSynthesizer{practice: practiceSynthesizer},
	)
	if err != nil {
		logger.Error("IELTS speech startup failed")
		return 1
	}
	ieltsSpeechHTTP, err := ielts.NewSpeechHTTPHandler(ieltsSpeechService)
	if err != nil {
		logger.Error("IELTS speech HTTP startup failed")
		return 1
	}
	ieltsAnswerGenerator, err := providerFactory.IELTSAnswerGenerator(textConfig)
	if err != nil {
		logger.Error("IELTS answer generator startup failed", slog.String("error_kind", "dependency"))
		return 1
	}
	ieltsAnswerService, err := ielts.NewAnswerGenerationService(ieltsQuestionBank, ieltsAnswerGenerator)
	if err != nil {
		logger.Error("IELTS answer service startup failed", slog.String("error_kind", "dependency"))
		return 1
	}
	ieltsAnswerHTTP, err := ielts.NewAnswerGenerationHTTPHandler(ieltsAnswerService)
	if err != nil {
		logger.Error("IELTS answer HTTP startup failed", slog.String("error_kind", "dependency"))
		return 1
	}

	interviewResumeConfig, err := buildInterviewResumeConfiguration(
		ctx,
		storageConfig,
		textConfig,
		resumeOCRConfig,
		providerFactory,
		providerObserver,
	)
	if err != nil {
		logger.Error(
			"Interview Resume composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}

	threadSummaryWakeup := newWorkerWakeup()

	var recordingStore objectstore.Store
	var imageStore objectstore.Store
	var agentImageConfig *app.AgentImageConfiguration
	if storageConfig.Enabled {
		recordingStore, err = newProtectedObjectStore(
			ctx,
			storageConfig,
			storageConfig.AudioPrefix,
			providerObserver,
		)
		if err != nil {
			logger.Error(
				"object storage startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
		imageStore, err = newAgentImageStore(
			ctx,
			storageConfig,
			providerObserver,
		)
		if err != nil {
			logger.Error(
				"image object storage startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
		agentImageConfig = &app.AgentImageConfiguration{
			ObjectStore: imageStore,
			StagedTTL:   24 * time.Hour,
			UploadLease: 2 * time.Minute,
		}
	}
	var acousticEvaluator evaluation.AcousticEvaluator
	var acousticProviderTimeout time.Duration
	iseConfigured := config.ISEConfigured()
	if iseConfigured && recordingStore == nil {
		logger.Error(
			"Evaluation acoustics configured without object storage",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}
	acousticsEnabled := recordingStore != nil && iseConfigured
	if acousticsEnabled {
		iseConfig, configurationErr := config.LoadISE()
		if configurationErr != nil {
			logger.Error(
				"iFlytek ISE configuration failed",
				slog.String("error_kind", "configuration"),
			)
			return 1
		}
		acousticProviderTimeout = iseConfig.Timeout
		acousticEvaluator, err =
			app.NewEvaluationAcousticEvaluator(
				databasePool.Native(),
				recordingStore,
				iseConfig,
			)
		if err != nil {
			logger.Error(
				"Evaluation acoustic provider failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
	} else {
		logger.Info(
			"Evaluation acoustic assessment disabled",
			slog.String("reason", "ISE_NOT_CONFIGURED"),
		)
	}
	singleRoundDeadline := textConfig.Timeout + 10*time.Second
	ieltsDeadline := max(
		2*textConfig.Timeout+10*time.Second,
		evaluation.MinimumIELTSTwoRoundDeadline,
	)
	sessionLease := max(singleRoundDeadline, ieltsDeadline) + 30*time.Second
	speechDeadline := textConfig.Timeout + 30*time.Second
	if acousticsEnabled {
		speechDeadline += speechfeedback.AudioReadTimeout + acousticProviderTimeout
	}
	speechLease := speechDeadline + 30*time.Second
	evaluationComposition, err := app.NewEvaluationComposition(
		databasePool.Native(),
		evaluationScoringGenerator,
		evaluationSpeechFeedbackGenerator,
		acousticEvaluator,
		app.EvaluationConfiguration{
			Provider:     textConfig.Provider,
			SessionModel: textConfig.EvaluationModel,
			SpeechModel:  textConfig.SpeechFeedbackModel,
			Worker: evaluation.WorkerConfiguration{
				SessionLane: evaluation.ClaimLane{
					Kinds:         []evaluation.Kind{evaluation.KindSessionReport},
					LeaseDuration: sessionLease,
					MaxAttempts:   3,
				},
				SpeechLane: evaluation.ClaimLane{
					Kinds: []evaluation.Kind{
						evaluation.KindPracticeTurnFeedback,
						evaluation.KindAgentMessageFeedback,
					},
					LeaseDuration: speechLease,
					MaxAttempts:   3,
				},
				AcousticsEnabled:  acousticsEnabled,
				InterviewDeadline: singleRoundDeadline,
				IELTSDeadline:     ieltsDeadline,
				GeneralDeadline:   singleRoundDeadline,
				SpeechDeadline:    speechDeadline,
				RetryDelay:        time.Second,
				DependencyDelay:   5 * time.Second,
				FinalizeTimeout:   5 * time.Second,
			},
		},
	)
	if err != nil {
		logger.Error(
			"Evaluation composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	agentMessageFeedback := evaluationComposition.AgentMessageScheduler()
	if !storageConfig.Enabled {
		agentMessageFeedback = nil
	}

	applicationComposition, err :=
		app.NewIdentityAgentAndPracticeCompositionWithWorkerWakeupsAndImages(
			ctx,
			databasePool.Native(),
			cfg.TrustedProxyCIDRs,
			cfg.TrustedProxyHeader,
			modelProviders,
			agentrun.Configuration{
				Provider:           textConfig.Provider,
				Model:              textConfig.Model,
				MaxOutputTokens:    textConfig.MaxOutputTokens,
				MaxInputCharacters: textConfig.MaxContextChars,
			},
			app.AgentRunRuntimeConfiguration{
				LoopTimeout: agentRunConfig.LoopTimeout,
			},
			sceneCatalog,
			ieltsQuestionBank,
			preparationJobTargets,
			evaluationComposition.PracticeSchedulers(),
			app.AgentWorkerWakeups{
				ThreadSummary: threadSummaryWakeup,
			},
			agentImageConfig,
			interviewResumeConfig,
			app.RuntimeAudioConfiguration{
				AgentVoice: app.AgentVoiceConfiguration{
					Recognizer:             recognizer,
					Synthesizer:            synthesizer,
					AssistantSpeech:        synthesizer,
					MessageFeedback:        agentMessageFeedback,
					InputEnabled:           storageConfig.Enabled,
					ScratchDirectory:       ttsConfig.TempDirectory,
					ObjectReadAllowedHosts: agentVoiceObjectReadHosts,
					ReadTimeout:            temporaryAudioConfig.ReadTimeout,
					StagedTTL:              24 * time.Hour,
					ASRLease:               voiceASRLease(asrConfig),
				},
				PracticeInteraction: app.PracticeInteractionConfiguration{
					Recognizer:          practiceRecognizer,
					RecordedRecognizer:  practiceRecordedRecognizer,
					Synthesizer:         practiceSynthesizer,
					QuestionGenerator:   practiceQuestions,
					QuestionTranslator:  modelProviders.Translation,
					AnswerTipGenerator:  practiceAnswerTips,
					TemporaryAudio:      audioVault,
					AudioStagedTTL:      24 * time.Hour,
					ASRLease:            voiceASRLease(asrConfig),
					RealtimeReadTimeout: temporaryAudioConfig.ReadTimeout,
					RecordedReadTimeout: temporaryAudioConfig.RecordedReadTimeout,
				},
				Media: app.AudioMediaConfiguration{
					ObjectStore: recordingStore,
					UploadLease: voiceAudioUploadLease,
				},
			},
		)
	if err != nil {
		logger.Error("application startup failed", slog.Any("error", err))
		return 1
	}
	var avatarProvider practiceavatar.TokenProvider
	if spatiusConfig.Enabled {
		avatarProvider, err = spatius.NewClient(spatius.Config{
			Enabled: true, ConsoleBaseURL: spatiusConfig.ConsoleBaseURL,
			APIKey: spatiusConfig.APIKey.Reveal(), Timeout: spatiusConfig.Timeout,
		})
		if err != nil {
			logger.Error("Practice avatar provider startup failed")
			return 1
		}
	}
	avatarService, err := practiceavatar.NewService(
		practiceavatar.ServiceConfiguration{
			Enabled: spatiusConfig.Enabled, AppID: spatiusConfig.AppID,
			AvatarID: spatiusConfig.AvatarID, Region: spatiusConfig.Region,
			TokenTTL: spatiusConfig.TokenTTL,
		},
		applicationComposition.PracticeRepository(),
		avatarProvider,
	)
	if err != nil {
		logger.Error("Practice avatar startup failed")
		return 1
	}
	avatarHTTP, err := practiceavatar.NewHTTPHandler(avatarService)
	if err != nil {
		logger.Error("Practice avatar HTTP startup failed")
		return 1
	}
	reviewRetryService, err := reviewpostgres.New(
		databasePool.Native(),
		evaluationComposition.Store(),
		applicationComposition.PracticeRepository(),
	)
	if err != nil {
		logger.Error(
			"Review retry startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	reviewRetryHTTP, err := review.NewRetryHTTPHandler(reviewRetryService)
	if err != nil {
		logger.Error(
			"Review retry HTTP startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	threadSummary, err := buildThreadSummaryWorker(
		applicationComposition.ThreadSummaryProcessor(),
		logger,
		threadSummaryWakeup.Events(),
	)
	if err != nil {
		logger.Error(
			"thread summary startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	protectedRegistrars := []app.ProtectedRouteRegistrar{
		ieltsSpeechHTTP,
		ieltsAnswerHTTP,
		evaluationComposition.HTTPHandler(),
		reviewRetryHTTP,
		avatarHTTP,
	}
	contextRoutes, err := applicationComposition.ProtectedRoutes(protectedRegistrars...)
	if err != nil {
		logger.Error("context route startup failed", slog.Any("error", err))
		return 1
	}
	evaluationWorkers, err := buildEvaluationRuntime(
		evaluationComposition.Worker(),
		logger,
	)
	if err != nil {
		logger.Error(
			"Evaluation worker startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	mediaCleanup, err := buildMediaCleanupWorker(
		storageConfig,
		applicationComposition.MediaReclaimer(),
		logger,
	)
	if err != nil {
		logger.Error(
			"media cleanup startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	var mediaCleanupDone chan struct{}
	if mediaCleanup != nil {
		mediaCleanupDone = make(chan struct{})
		go func() {
			defer close(mediaCleanupDone)
			mediaCleanup.Run(ctx)
		}()
	}
	threadSummaryDone := make(chan struct{})
	go func() {
		defer close(threadSummaryDone)
		threadSummary.Run(ctx)
	}()
	evaluationWorkersDone := make(chan struct{})
	go func() {
		defer close(evaluationWorkersDone)
		evaluationWorkers.Run(ctx)
	}()

	router, metricsHandler, err := app.NewObservableRouterWithRegistry(
		logger,
		databasePool,
		metricsRegistry,
		[]app.RouteRegistrar{
			applicationComposition.IdentityModule(),
			applicationComposition.AgentModule(),
			contextRoutes,
		},
		preparation.New(),
		practice.New(),
	)
	if err != nil {
		logger.Error("HTTP observability startup failed")
		return 1
	}
	app.RegisterSceneCatalog(router, sceneCatalog)
	app.RegisterIELTSQuestionBank(router, ieltsQuestionBank)

	server := &http.Server{
		Addr:              cfg.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddress(),
		Handler:           metricsHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan httpServerResult, 2)
	go func() {
		logger.Info("server started", slog.String("address", cfg.Address()))
		serverErrors <- httpServerResult{
			name: "application",
			err:  server.ListenAndServe(),
		}
	}()
	go func() {
		logger.Info(
			"metrics server started",
			slog.String("address", cfg.MetricsAddress()),
		)
		serverErrors <- httpServerResult{
			name: "metrics",
			err:  metricsServer.ListenAndServe(),
		}
	}()

	exitCode := 0
	select {
	case result := <-serverErrors:
		if !errors.Is(result.err, http.ErrServerClosed) {
			logger.Error(
				"server stopped unexpectedly",
				slog.String("server", result.name),
				slog.Any("error", result.err),
			)
			exitCode = 1
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		exitCode = 1
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error(
			"metrics server graceful shutdown failed",
			slog.Any("error", err),
		)
		exitCode = 1
	}
	if mediaCleanupDone != nil {
		select {
		case <-mediaCleanupDone:
		case <-shutdownCtx.Done():
			logger.Error(
				"media cleanup shutdown failed",
				slog.String("error_kind", "timeout"),
			)
			exitCode = 1
		}
	}
	select {
	case <-threadSummaryDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"thread summary shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
	}
	select {
	case <-evaluationWorkersDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"Evaluation worker shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
	}

	return exitCode
}

func voiceASRLease(configuration config.SpeechRecognitionConfig) time.Duration {
	return voiceAudioUploadLease + max(
		configuration.Timeout,
		configuration.RecordedTimeout,
	) + voiceASRFinalizationTimeMargin
}
