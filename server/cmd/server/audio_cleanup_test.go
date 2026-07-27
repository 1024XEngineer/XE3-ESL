package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestBuildAudioCleanupWorkerDoesNotConstructWhenDisabled(t *testing.T) {
	const sensitiveBucket = "private-bucket-must-not-be-logged"
	var (
		storeCalls      atomic.Int32
		repositoryCalls atomic.Int32
		logs            bytes.Buffer
	)
	worker, err := buildAudioCleanupWorker(
		context.Background(),
		config.ObjectStorageConfig{
			Enabled: false,
			Bucket:  sensitiveBucket,
		},
		nil,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		audioCleanupFactories{
			newStore: func(
				context.Context,
				config.ObjectStorageConfig,
			) (objectstore.Store, error) {
				storeCalls.Add(1)
				return nil, nil
			},
			newRepository: func(
				*pgxpool.Pool,
			) (conversation.AudioAssetLifecycleRepository, error) {
				repositoryCalls.Add(1)
				return nil, nil
			},
		},
	)
	if err != nil || worker != nil {
		t.Fatalf("buildAudioCleanupWorker() = %#v, %v", worker, err)
	}
	if storeCalls.Load() != 0 || repositoryCalls.Load() != 0 {
		t.Fatalf(
			"disabled factories called: store=%d repository=%d",
			storeCalls.Load(),
			repositoryCalls.Load(),
		)
	}
	if output := logs.String(); strings.Contains(output, sensitiveBucket) ||
		!strings.Contains(output, `"reason":"configuration_disabled"`) {
		t.Fatalf("unexpected disabled log: %s", output)
	}
}

func TestBuildAudioCleanupWorkerFailsEnabledStoreInitialization(t *testing.T) {
	startupErr := errors.New("storage startup failed")
	var repositoryCalls atomic.Int32
	worker, err := buildAudioCleanupWorker(
		context.Background(),
		config.ObjectStorageConfig{Enabled: true},
		nil,
		slog.Default(),
		audioCleanupFactories{
			newStore: func(
				context.Context,
				config.ObjectStorageConfig,
			) (objectstore.Store, error) {
				return nil, startupErr
			},
			newRepository: func(
				*pgxpool.Pool,
			) (conversation.AudioAssetLifecycleRepository, error) {
				repositoryCalls.Add(1)
				return nil, nil
			},
		},
	)
	if worker != nil || !errors.Is(err, startupErr) {
		t.Fatalf("buildAudioCleanupWorker() = %#v, %v", worker, err)
	}
	if repositoryCalls.Load() != 0 {
		t.Fatal("repository constructed after storage initialization failed")
	}
}
