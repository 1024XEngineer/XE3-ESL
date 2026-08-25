package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/logging"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/iserelay"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/xfyun"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const relayShutdownTimeout = 10 * time.Second

type runtimeConfig struct {
	Address         string
	InternalAddress string
	ServerCertFile  string
	ServerKeyFile   string
	ClientCAFile    string
	Retention       time.Duration
	MaxJobs         int
	MaxInFlight     int
	LogLevel        string
}

func main() {
	os.Exit(run())
}

func run() int {
	if err := config.LoadDotEnvUpwards(); err != nil {
		_, _ = os.Stderr.WriteString("load .env failed\n")
		return 1
	}
	runtimeConfiguration, err := loadRuntimeConfig()
	if err != nil {
		_, _ = os.Stderr.WriteString("ISE relay configuration failed\n")
		return 1
	}
	logger := logging.New(runtimeConfiguration.LogLevel)
	slog.SetDefault(logger)
	iseConfiguration, err := config.LoadISE()
	if err != nil {
		logger.Error("iFlytek ISE configuration failed")
		return 1
	}
	registry := prometheus.NewRegistry()
	observer, err := providerobservability.New(registry)
	if err != nil {
		logger.Error("provider observability startup failed")
		return 1
	}
	evaluator, err := xfyun.NewSpeechFeedbackEvaluator(
		xfyun.ISEConfig{
			Endpoint: iseConfiguration.Endpoint,
			Timeout:  iseConfiguration.Timeout,
			Observer: observer,
		},
		iseConfiguration.AppID.Reveal(),
		iseConfiguration.APIKey.Reveal(),
		iseConfiguration.APISecret.Reveal(),
	)
	if err != nil {
		logger.Error("iFlytek ISE provider startup failed")
		return 1
	}
	handler, err := iserelay.NewHandler(evaluator, iserelay.HandlerConfig{
		ProviderTimeout: iseConfiguration.Timeout,
		Retention:       runtimeConfiguration.Retention,
		MaxJobs:         runtimeConfiguration.MaxJobs,
		MaxInFlight:     runtimeConfiguration.MaxInFlight,
		Logger:          logger,
	})
	if err != nil {
		logger.Error("ISE relay handler startup failed")
		return 1
	}
	tlsConfiguration, err := loadTLSConfig(runtimeConfiguration)
	if err != nil {
		logger.Error("ISE relay TLS startup failed")
		return 1
	}
	server := &http.Server{
		Addr:              runtimeConfiguration.Address,
		Handler:           handler.Routes(),
		TLSConfig:         tlsConfiguration,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	internalMux := http.NewServeMux()
	internalMux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	internalMux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	internalServer := &http.Server{
		Addr:              runtimeConfiguration.InternalAddress,
		Handler:           internalMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- server.ListenAndServeTLS("", "") }()
	go func() { errorsChannel <- internalServer.ListenAndServe() }()
	logger.Info("ISE relay started", slog.String("address", runtimeConfiguration.Address))

	exitCode := 0
	select {
	case serveErr := <-errorsChannel:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("ISE relay stopped unexpectedly")
			exitCode = 1
		}
	case <-ctx.Done():
		logger.Info("ISE relay shutdown requested")
	}
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), relayShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		exitCode = 1
	}
	if err := internalServer.Shutdown(shutdownContext); err != nil {
		exitCode = 1
	}
	return exitCode
}

func loadRuntimeConfig() (runtimeConfig, error) {
	configuration := runtimeConfig{
		Address:         valueOrDefault("ISE_RELAY_ADDRESS", ":18443"),
		InternalAddress: valueOrDefault("ISE_RELAY_INTERNAL_ADDRESS", ":18080"),
		ServerCertFile:  strings.TrimSpace(os.Getenv("ISE_RELAY_SERVER_CERT_FILE")),
		ServerKeyFile:   strings.TrimSpace(os.Getenv("ISE_RELAY_SERVER_KEY_FILE")),
		ClientCAFile:    strings.TrimSpace(os.Getenv("ISE_RELAY_CLIENT_CA_FILE")),
		LogLevel:        valueOrDefault("LOG_LEVEL", "info"),
	}
	if configuration.ServerCertFile == "" || configuration.ServerKeyFile == "" ||
		configuration.ClientCAFile == "" {
		return runtimeConfig{}, errors.New("ISE relay TLS files are required")
	}
	var err error
	configuration.Retention, err = durationOrDefault("ISE_RELAY_RETENTION", 15*time.Minute)
	if err != nil || configuration.Retention < time.Minute || configuration.Retention > time.Hour {
		return runtimeConfig{}, errors.New("ISE_RELAY_RETENTION must be between 1m and 1h")
	}
	configuration.MaxJobs, err = positiveIntOrDefault("ISE_RELAY_MAX_JOBS", 16)
	if err != nil || configuration.MaxJobs > 64 {
		return runtimeConfig{}, errors.New("ISE_RELAY_MAX_JOBS must be between 1 and 64")
	}
	configuration.MaxInFlight, err = positiveIntOrDefault("ISE_RELAY_MAX_IN_FLIGHT", 2)
	if err != nil || configuration.MaxInFlight > configuration.MaxJobs {
		return runtimeConfig{}, errors.New("ISE_RELAY_MAX_IN_FLIGHT must not exceed max jobs")
	}
	return configuration, nil
}

func loadTLSConfig(configuration runtimeConfig) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(
		configuration.ServerCertFile,
		configuration.ServerKeyFile,
	)
	if err != nil {
		return nil, fmt.Errorf("load ISE relay server certificate: %w", err)
	}
	clientCAPEM, err := os.ReadFile(configuration.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read ISE relay client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCAPEM) {
		return nil, errors.New("ISE relay client CA is invalid")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}

func valueOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

func positiveIntOrDefault(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return parsed, nil
}
