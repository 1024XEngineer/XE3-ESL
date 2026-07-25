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

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
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
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)
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

	var recordingStore objectstore.Store
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
	}

	identityModule, agentModule, err :=
		bootstrap.NewIdentityAndAgentModules(
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
			bootstrap.VoiceConfiguration{
				Recognizer:     recognizer,
				Synthesizer:    synthesizer,
				TemporaryAudio: audioVault,
				ObjectStore:    recordingStore,
				AudioStagedTTL: 24 * time.Hour,
				ASRLease:       asrConfig.Timeout + 15*time.Second,
				// The existing Review lease is 30s. Bound the parent context
				// below it even when the shared provider client allows 60s.
				ReviewGenerationTimeout: 20 * time.Second,
				AudioReadTimeout:        temporaryAudioConfig.ReadTimeout,
			},
		)
	if err != nil {
		logger.Error("application startup failed", slog.Any("error", err))
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

	router := bootstrap.NewRouterWithReadinessAndRoutes(
		logger,
		databasePool,
		[]bootstrap.RouteRegistrar{identityModule, agentModule},
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

	return exitCode
}
