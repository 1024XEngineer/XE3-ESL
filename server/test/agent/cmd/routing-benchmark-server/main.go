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
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/database"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/benchmark"
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
	textConfiguration, err := config.LoadTextGeneration()
	if err != nil {
		logger.Error("text generation configuration failed")
		return 1
	}
	providers, err := bootstrap.NewAgentModelProviders(textConfiguration)
	if err != nil {
		logger.Error("text generation startup failed")
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
		logger.Error("database startup failed")
		return 1
	}
	defer databasePool.Close()
	handler, err := benchmark.NewHandler(
		ctx,
		databasePool.Native(),
		logger,
		providers.Run,
		agentrun.Configuration{
			Provider:           textConfiguration.Provider,
			Model:              textConfiguration.Model,
			MaxOutputTokens:    textConfiguration.MaxOutputTokens,
			MaxInputCharacters: textConfiguration.MaxContextChars,
		},
		cfg.TrustedProxyCIDRs,
		cfg.TrustedProxyHeader,
	)
	if err != nil {
		logger.Error("Agent routing benchmark startup failed")
		return 1
	}
	server := &http.Server{
		Addr:              cfg.Address(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	logger.Info(
		"Agent routing benchmark server started",
		slog.String("address", server.Addr),
	)
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Agent routing benchmark server stopped")
			return 1
		}
		return 0
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("Agent routing benchmark shutdown failed")
			return 1
		}
		return 0
	}
}
