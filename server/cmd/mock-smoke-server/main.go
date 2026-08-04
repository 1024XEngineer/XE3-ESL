package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	"github.com/1024XEngineer/XE3-ESL/server/test/smoke"
)

func main() {
	if err := config.LoadDotEnvUpwards(); err != nil {
		logger := logging.New("info")
		logger.Error("load .env failed", slog.Any("error", err))
		os.Exit(1)
	}
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)
	toolConfig, err := config.LoadAgentTool()
	if err != nil || toolConfig.Mode != config.AgentToolModeMock {
		logger.Error("mock smoke server requires AGENT_TOOL_MODE=mock")
		os.Exit(1)
	}
	server := &http.Server{
		Addr:    "127.0.0.1:" + cfg.Port,
		Handler: smoke.NewServer(logger).Handler(),
	}
	logger.Info("deterministic mock smoke server started", slog.String("address", server.Addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("mock smoke server stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
