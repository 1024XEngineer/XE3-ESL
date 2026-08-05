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

	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/avatar"
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/database"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const shutdownTimeout = 5 * time.Second

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
	textConfig, err := config.LoadTextGeneration()
	if err != nil {
		logger.Error("text generation configuration failed")
		return 1
	}
	modelProviders, err := bootstrap.NewAgentModelProviders(textConfig)
	if err != nil {
		logger.Error("text generation startup failed")
		return 1
	}
	memeConfig, err := config.LoadMeme()
	if err != nil {
		logger.Error("Agent Meme configuration failed")
		return 1
	}
	preparationJobTargets, err :=
		bootstrap.NewPreparationJobTargetGenerator(textConfig)
	if err != nil {
		logger.Error("Preparation job target generation startup failed")
		return 1
	}
	evaluationScoringGenerator, err :=
		bootstrap.NewEvaluationScoringGenerator(textConfig)
	if err != nil {
		logger.Error("evaluation scoring startup failed")
		return 1
	}
	evaluationSpeechFeedbackGenerator, err :=
		bootstrap.NewEvaluationSpeechFeedbackGenerator(textConfig)
	if err != nil {
		logger.Error("evaluation speech feedback startup failed")
		return 1
	}
	embeddingConfig, err := config.LoadEmbedding()
	if err != nil {
		logger.Error("embedding configuration failed")
		return 1
	}
	embedder, err := bootstrap.NewMemoryEmbedder(embeddingConfig)
	if err != nil {
		logger.Error("embedding startup failed")
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
	recognizer, err := bootstrap.NewAgentSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("speech recognition startup failed")
		return 1
	}
	synthesizer, err := bootstrap.NewAgentSpeechSynthesizer(ttsConfig)
	if err != nil {
		logger.Error("speech synthesis startup failed")
		return 1
	}
	practiceRecognizer, err := bootstrap.NewPracticeSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("Practice speech recognition startup failed")
		return 1
	}
	practiceSynthesizer, err := bootstrap.NewPracticeSpeechSynthesizer(ttsConfig)
	if err != nil {
		logger.Error("Practice speech synthesis startup failed")
		return 1
	}
	practiceQuestions, err := bootstrap.NewPracticeQuestionGenerator(textConfig)
	if err != nil {
		logger.Error("Practice question generation startup failed")
		return 1
	}
	practiceQuestionTranslator, err := bootstrap.NewPracticeQuestionTranslator(
		textConfig,
	)
	if err != nil {
		logger.Error("Practice question translation startup failed")
		return 1
	}
	practiceAnswerTips, err := bootstrap.NewPracticeAnswerTipGenerator(textConfig)
	if err != nil {
		logger.Error("Practice answer Tip generation startup failed")
		return 1
	}
	temporaryAudioConfig, err := config.LoadTemporaryAudio()
	if err != nil {
		logger.Error("temporary audio configuration failed")
		return 1
	}
	reviewHistoryConfig, err := config.LoadReviewHistory()
	if err != nil {
		logger.Error("Review history configuration failed")
		return 1
	}
	spatiusConfig, err := config.LoadSpatius()
	if err != nil {
		logger.Error(
			"avatar provider configuration failed",
			slog.String("error_kind", "configuration"),
		)
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
	resumeOCRConfig, err := config.LoadResumeOCR()
	if err != nil {
		logger.Error(
			"Resume OCR configuration invalid",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}
	agentVoiceObjectReadHosts, err :=
		bootstrap.AgentVoiceObjectReadAllowedHosts(storageConfig)
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
	evaluationPolicies := scoring.NewEvaluationPolicyRegistry()
	sceneCatalog, err := scene.NewPostgresCatalog(
		databasePool.Native(),
		evaluationPolicies,
	)
	if err != nil {
		logger.Error("Scene catalog startup failed", slog.Any("error", err))
		return 1
	}

	resumeComposition, err := buildResumeComposition(
		ctx,
		databasePool.Native(),
		storageConfig,
		textConfig,
		resumeOCRConfig,
	)
	if err != nil {
		logger.Error(
			"Resume composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}

	evaluationConfiguration, err := scoring.NewConfiguration(
		textConfig.Provider,
		textConfig.Model,
		textConfig.MaxOutputTokens,
	)
	if err != nil {
		logger.Error(
			"evaluation configuration failed",
			slog.String("error_kind", "configuration"),
		)
		return 1
	}
	evaluationComposition, err := bootstrap.NewEvaluationComposition(
		databasePool.Native(),
		evaluationScoringGenerator,
		evaluationPolicies,
		evaluationConfiguration,
	)
	if err != nil {
		logger.Error(
			"evaluation composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	memoryIndexComposition, err := bootstrap.NewMemoryIndexComposition(
		databasePool.Native(),
		embedder,
		embeddingConfig,
	)
	if err != nil {
		logger.Error(
			"memory index composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	memoryExtractionWakeup := newWorkerWakeup()
	memoryIndexWakeup := newWorkerWakeup()
	threadSummaryWakeup := newWorkerWakeup()

	var recordingStore objectstore.Store
	var imageStore objectstore.Store
	var agentImageConfig *bootstrap.AgentImageConfiguration
	if storageConfig.Enabled {
		recordingStore, err = productionAudioCleanupFactories.newStore(
			ctx,
			storageConfig,
		)
		if err != nil {
			logger.Error(
				"object storage startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
		imageStore, err = newAgentImageStore(ctx, storageConfig)
		if err != nil {
			logger.Error(
				"image object storage startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
		agentImageConfig = &bootstrap.AgentImageConfiguration{
			ObjectStore: imageStore,
			StagedTTL:   24 * time.Hour,
			UploadLease: 2 * time.Minute,
		}
	}
	var speechFeedbackAcoustics speechfeedback.SpeechFeedbackAcousticProvider
	speechFeedbackLease := 30 * time.Second
	if recordingStore != nil && config.ISEConfigured() {
		iseConfig, configurationErr := config.LoadISE()
		if configurationErr != nil {
			logger.Error(
				"iFlytek ISE configuration failed",
				slog.String("error_kind", "configuration"),
			)
			return 1
		}
		speechFeedbackLease = iseConfig.Timeout + 30*time.Second
		speechFeedbackAcoustics, err =
			bootstrap.NewSpeechFeedbackAcousticProvider(
				databasePool.Native(),
				recordingStore,
				iseConfig,
			)
		if err != nil {
			logger.Error(
				"SpeechFeedback acoustic provider failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
	} else if recordingStore != nil {
		logger.Warn(
			"iFlytek ISE is not configured; acoustic scoring is unavailable",
			slog.String("fallback", "transcript_only"),
		)
	}
	speechFeedbackComposition, err :=
		bootstrap.NewSpeechFeedbackComposition(
			databasePool.Native(),
			evaluationSpeechFeedbackGenerator,
			bootstrap.SpeechFeedbackConfiguration{
				Provider:      textConfig.Provider,
				Model:         textConfig.Model,
				LeaseDuration: speechFeedbackLease,
				Acoustics:     speechFeedbackAcoustics,
			},
		)
	if err != nil {
		logger.Error(
			"speech feedback composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}

	applicationComposition, err :=
		bootstrap.NewIdentityAgentAndPracticeCompositionWithWorkerWakeupsImagesAndMemes(
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
			memoryIndexComposition.Searcher(),
			sceneCatalog,
			preparationJobTargets,
			bootstrap.AgentWorkerWakeups{
				MemoryExtraction: memoryExtractionWakeup,
				ThreadSummary:    threadSummaryWakeup,
			},
			agentImageConfig,
			&bootstrap.AgentMemeConfiguration{
				AssetRoot: memeConfig.AssetRoot,
				Runtime:   memeConfig.Runtime,
			},
			bootstrap.VoiceConfiguration{
				Recognizer:             recognizer,
				Synthesizer:            synthesizer,
				PracticeRecognizer:     practiceRecognizer,
				PracticeSynthesizer:    practiceSynthesizer,
				QuestionGenerator:      practiceQuestions,
				QuestionTranslator:     practiceQuestionTranslator,
				AnswerTipGenerator:     practiceAnswerTips,
				TemporaryAudio:         audioVault,
				ObjectStore:            recordingStore,
				AgentVoiceInputEnabled: storageConfig.Enabled,
				ScratchDirectory:       ttsConfig.TempDirectory,
				ObjectReadAllowedHosts: agentVoiceObjectReadHosts,
				AudioStagedTTL:         24 * time.Hour,
				AudioUploadLease:       2 * time.Minute,
				ASRLease:               asrConfig.Timeout + 15*time.Second,
				AudioReadTimeout:       temporaryAudioConfig.ReadTimeout,
				ReviewHistoryCursorKey: []byte(
					reviewHistoryConfig.CursorSigningKey.Reveal(),
				),
				SpeechFeedbackCoordinator: speechFeedbackComposition.
					Coordinator(),
			},
		)
	if err != nil {
		logger.Error("application startup failed", slog.Any("error", err))
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
	avatarProvider, err := bootstrap.NewAvatarTokenProvider(spatiusConfig)
	if err != nil {
		logger.Error(
			"avatar provider startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	avatarService, err := avatar.NewService(
		avatar.ServiceConfiguration{
			Enabled:  spatiusConfig.Enabled,
			AppID:    spatiusConfig.AppID,
			AvatarID: spatiusConfig.AvatarID,
			Region:   spatiusConfig.Region,
			TokenTTL: spatiusConfig.TokenTTL,
		},
		applicationComposition.PracticeApplication(),
		avatarProvider,
	)
	if err != nil {
		logger.Error(
			"avatar service startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	avatarHTTP, err := avatar.NewHTTPHandler(avatarService)
	if err != nil {
		logger.Error(
			"avatar HTTP startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	protectedRegistrars := []bootstrap.ProtectedRouteRegistrar{
		avatarHTTP,
		evaluationComposition.HTTPHandler(),
		speechFeedbackComposition.HTTPHandler(),
		speechFeedbackComposition.RetryHTTPHandler(),
	}
	if resumeComposition != nil {
		protectedRegistrars = append(
			protectedRegistrars,
			resumeComposition.HTTPHandler(),
		)
	}
	contextRoutes, err := applicationComposition.ProtectedRoutes(protectedRegistrars...)
	if err != nil {
		logger.Error("context route startup failed", slog.Any("error", err))
		return 1
	}
	evaluationShadow, err := buildEvaluationShadowWorker(
		evaluationComposition.Worker(),
		logger,
	)
	if err != nil {
		logger.Error(
			"evaluation shadow startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	speechFeedback, err := buildSpeechFeedbackWorker(
		speechFeedbackComposition.Worker(),
		logger,
	)
	if err != nil {
		logger.Error(
			"speech feedback startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	agentVoiceCleanup, err := buildAgentVoiceCleanupWorker(
		storageConfig,
		applicationComposition.AgentVoiceReclaimer(),
		logger,
	)
	if err != nil {
		logger.Error(
			"agent voice cleanup startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	agentImageCleanup, err := buildAgentImageCleanupWorker(
		storageConfig,
		applicationComposition.AgentImageReclaimer(),
		logger,
	)
	if err != nil {
		logger.Error(
			"agent image cleanup startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	memoryExtraction, err := buildMemoryExtractionWorker(
		applicationComposition.MemoryExtractionProcessor(),
		logger,
		memoryExtractionWakeup.Events(),
		memoryIndexWakeup,
	)
	if err != nil {
		logger.Error(
			"memory extraction startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	memoryIndex, err := buildMemoryIndexWorker(
		memoryIndexComposition.Processor(),
		logger,
		memoryIndexWakeup.Events(),
	)
	if err != nil {
		logger.Error(
			"memory index startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}

	cleanupWorker, err := buildAudioCleanupWorker(
		ctx,
		storageConfig,
		databasePool.Native(),
		logger,
		productionAudioCleanupFactories,
	)
	if err != nil {
		logger.Error(
			"audio cleanup startup failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}
	var cleanupDone chan struct{}
	if cleanupWorker != nil {
		cleanupDone = make(chan struct{})
		go func() {
			defer close(cleanupDone)
			cleanupWorker.Run(ctx)
		}()
	}
	var agentVoiceCleanupDone chan struct{}
	if agentVoiceCleanup != nil {
		agentVoiceCleanupDone = make(chan struct{})
		go func() {
			defer close(agentVoiceCleanupDone)
			agentVoiceCleanup.Run(ctx)
		}()
	}
	var agentImageCleanupDone chan struct{}
	if agentImageCleanup != nil {
		agentImageCleanupDone = make(chan struct{})
		go func() {
			defer close(agentImageCleanupDone)
			agentImageCleanup.Run(ctx)
		}()
	}
	memoryExtractionDone := make(chan struct{})
	go func() {
		defer close(memoryExtractionDone)
		memoryExtraction.Run(ctx)
	}()
	memoryIndexDone := make(chan struct{})
	go func() {
		defer close(memoryIndexDone)
		memoryIndex.Run(ctx)
	}()
	threadSummaryDone := make(chan struct{})
	go func() {
		defer close(threadSummaryDone)
		threadSummary.Run(ctx)
	}()
	evaluationShadowDone := make(chan struct{})
	go func() {
		defer close(evaluationShadowDone)
		evaluationShadow.Run(ctx)
	}()
	speechFeedbackDone := make(chan struct{})
	go func() {
		defer close(speechFeedbackDone)
		speechFeedback.Run(ctx)
	}()
	var resumeWorkerDone chan struct{}
	if resumeComposition != nil {
		resumeWorkerDone = make(chan struct{})
		go func() {
			defer close(resumeWorkerDone)
			resumeComposition.Worker().Run(ctx)
		}()
	}

	router := bootstrap.NewRouterWithReadinessAndRoutes(
		logger,
		databasePool,
		[]bootstrap.RouteRegistrar{
			applicationComposition.IdentityModule(),
			applicationComposition.AgentModule(),
			contextRoutes,
		},
		preparation.New(),
		practice.New(),
		practicevoice.New(),
		review.New(),
	)
	bootstrap.RegisterSceneCatalog(router, sceneCatalog)

	server := &http.Server{
		Addr:              cfg.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server started", slog.String("address", cfg.Address()))
		serverErrors <- server.ListenAndServe()
	}()

	exitCode := 0
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", slog.Any("error", err))
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
	if cleanupDone != nil {
		select {
		case <-cleanupDone:
		case <-shutdownCtx.Done():
			logger.Error(
				"audio cleanup shutdown failed",
				slog.String("error_kind", "timeout"),
			)
			exitCode = 1
		}
	}
	if agentVoiceCleanupDone != nil {
		select {
		case <-agentVoiceCleanupDone:
		case <-shutdownCtx.Done():
			logger.Error(
				"agent voice cleanup shutdown failed",
				slog.String("error_kind", "timeout"),
			)
			exitCode = 1
		}
	}
	if agentImageCleanupDone != nil {
		select {
		case <-agentImageCleanupDone:
		case <-shutdownCtx.Done():
			logger.Error(
				"agent image cleanup shutdown failed",
				slog.String("error_kind", "timeout"),
			)
			exitCode = 1
		}
	}
	if resumeWorkerDone != nil {
		select {
		case <-resumeWorkerDone:
		case <-shutdownCtx.Done():
			logger.Error(
				"Resume worker shutdown failed",
				slog.String("error_kind", "timeout"),
			)
			exitCode = 1
		}
	}
	select {
	case <-memoryExtractionDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"memory extraction shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
	}
	select {
	case <-memoryIndexDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"memory index shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
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
	case <-evaluationShadowDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"evaluation shadow shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
	}
	select {
	case <-speechFeedbackDone:
	case <-shutdownCtx.Done():
		logger.Error(
			"speech feedback shutdown failed",
			slog.String("error_kind", "timeout"),
		)
		exitCode = 1
	}

	return exitCode
}
