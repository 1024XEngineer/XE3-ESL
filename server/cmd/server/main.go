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

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
	"github.com/1024XEngineer/XE3-ESL/server/internal/avatar"
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/database"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
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
	toolConfig, err := config.LoadAgentTool()
	if err != nil {
		logger.Error("agent tool configuration failed")
		return 1
	}
	if toolConfig.Mode == config.AgentToolModeMock {
		logger.Error("agent tool mock mode is not supported by production server")
		return 1
	}
	textConfig, err := config.LoadTextGeneration()
	if err != nil {
		logger.Error("text generation configuration failed")
		return 1
	}
	textGenerator, err := bootstrap.NewTextGenerator(textConfig)
	if err != nil {
		logger.Error("text generation startup failed")
		return 1
	}
	embeddingConfig, err := config.LoadEmbedding()
	if err != nil {
		logger.Error("embedding configuration failed")
		return 1
	}
	embedder, err := bootstrap.NewEmbedder(embeddingConfig)
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
	recognizer, err := bootstrap.NewSpeechRecognizer(asrConfig)
	if err != nil {
		logger.Error("speech recognition startup failed")
		return 1
	}
	synthesizer, err := bootstrap.NewSpeechSynthesizer(ttsConfig)
	if err != nil {
		logger.Error("speech synthesis startup failed")
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

	preparationCatalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		logger.Error("preparation catalog startup failed", slog.Any("error", err))
		return 1
	}

	storageConfig, err := config.LoadObjectStorage()
	if err != nil {
		logger.Error(
			"object storage configuration invalid",
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

	resumeComposition, err := buildResumeComposition(
		ctx,
		databasePool.Native(),
		storageConfig,
	)
	if err != nil {
		logger.Error(
			"Resume composition failed",
			slog.String("error_kind", "dependency"),
		)
		return 1
	}

	evaluationComposition, err := bootstrap.NewEvaluationComposition(
		databasePool.Native(),
		textGenerator,
		bootstrap.EvaluationConfiguration{
			Provider:          textConfig.Provider,
			Model:             textConfig.Model,
			MaxOutputTokens:   textConfig.MaxOutputTokens,
			GenerationTimeout: 45 * time.Second,
			LeaseDuration:     60 * time.Second,
			MaxAttempts:       3,
			CursorSigningKey: []byte(
				reviewHistoryConfig.CursorSigningKey.Reveal(),
			),
		},
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
	var speechFeedbackAcoustics review.SpeechFeedbackAcousticProvider
	speechFeedbackLease := 30 * time.Second
	if recordingStore != nil {
		iseConfig, configurationErr := config.LoadISE()
		if configurationErr != nil {
			logger.Error(
				"iFlytek ISE configuration failed",
				slog.String("error_kind", "configuration"),
			)
			return 1
		}
		speechFeedbackLease = iseConfig.Timeout + 30*time.Second
		iseEvaluator, evaluatorErr := xfyun.NewEvaluator(
			xfyun.ISEConfig{
				Endpoint: iseConfig.Endpoint,
				Timeout:  iseConfig.Timeout,
			},
			iseConfig.AppID.Reveal(),
			iseConfig.APIKey.Reveal(),
			iseConfig.APISecret.Reveal(),
		)
		if evaluatorErr != nil {
			logger.Error(
				"iFlytek ISE startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
		speechFeedbackAcoustics, err =
			bootstrap.NewSpeechFeedbackAcousticProvider(
				databasePool.Native(),
				recordingStore,
				iseEvaluator,
			)
		if err != nil {
			logger.Error(
				"SpeechFeedback acoustic provider failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
	}
	speechFeedbackComposition, err :=
		bootstrap.NewSpeechFeedbackComposition(
			databasePool.Native(),
			textGenerator,
			bootstrap.SpeechFeedbackConfiguration{
				Provider:      textConfig.Provider,
				Model:         textConfig.Model,
				MaxAttempts:   3,
				LeaseDuration: speechFeedbackLease,
				RetryDelay:    2 * time.Second,
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
		bootstrap.NewIdentityAgentAndPracticeCompositionWithWorkerWakeupsAndImages(
			ctx,
			databasePool.Native(),
			cfg.TrustedProxyCIDRs,
			cfg.TrustedProxyHeader,
			textGenerator,
			agent.RunConfiguration{
				Provider:           textConfig.Provider,
				Model:              textConfig.Model,
				MaxOutputTokens:    textConfig.MaxOutputTokens,
				MaxInputCharacters: textConfig.MaxContextChars,
			},
			memoryIndexComposition.Searcher(),
			preparationCatalog,
			bootstrap.AgentWorkerWakeups{
				MemoryExtraction: memoryExtractionWakeup,
				ThreadSummary:    threadSummaryWakeup,
			},
			agentImageConfig,
			bootstrap.VoiceConfiguration{
				Recognizer:                recognizer,
				Synthesizer:               synthesizer,
				TemporaryAudio:            audioVault,
				ObjectStore:               recordingStore,
				AgentVoiceMessagesEnabled: storageConfig.Enabled,
				ScratchDirectory:          ttsConfig.TempDirectory,
				ObjectReadAllowedHosts:    agentVoiceObjectReadHosts,
				AudioStagedTTL:            24 * time.Hour,
				AudioUploadLease:          2 * time.Minute,
				ASRLease:                  asrConfig.Timeout + 15*time.Second,
				// Keep report generation within the Review lease while allowing
				// the same provider budget used by scene-level reports.
				ReviewGenerationTimeout: 45 * time.Second,
				AudioReadTimeout:        temporaryAudioConfig.ReadTimeout,
				ReviewHistoryCursorKey: []byte(
					reviewHistoryConfig.CursorSigningKey.Reveal(),
				),
				InterviewShadowCoordinator: evaluationComposition.
					InterviewShadowCoordinator(),
				IELTSSpeakingShadowCoordinator: evaluationComposition.
					IELTSSpeakingShadowCoordinator(),
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
	var avatarProvider avatar.TokenProvider
	if spatiusConfig.Enabled {
		avatarProvider, err = avatar.NewSpatiusClient(spatiusConfig)
		if err != nil {
			logger.Error(
				"avatar provider startup failed",
				slog.String("error_kind", "dependency"),
			)
			return 1
		}
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
		conversation.New(),
		review.New(),
	)
	bootstrap.RegisterPreparationCatalog(router, preparationCatalog)

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
