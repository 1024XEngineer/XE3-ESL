package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/ossstore"
)

type audioCleanupFactories struct {
	newStore func(
		context.Context,
		config.ObjectStorageConfig,
	) (objectstore.Store, error)
	newRepository func(
		*pgxpool.Pool,
	) (practicevoice.AudioAssetLifecycleRepository, error)
}

var productionAudioCleanupFactories = audioCleanupFactories{
	newStore: func(
		ctx context.Context,
		storageConfig config.ObjectStorageConfig,
	) (objectstore.Store, error) {
		provider, err := ossstore.NewCredentialsProvider(storageConfig)
		if err != nil {
			return nil, err
		}
		return ossstore.New(ctx, storageConfig, provider)
	},
	newRepository: func(
		pool *pgxpool.Pool,
	) (practicevoice.AudioAssetLifecycleRepository, error) {
		return practicevoicepostgres.NewAudioAssetRepository(pool)
	},
}

func buildAudioCleanupWorker(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	databasePool *pgxpool.Pool,
	logger *slog.Logger,
	factories audioCleanupFactories,
) (*practicevoice.AudioAssetCleanupWorker, error) {
	if !storageConfig.Enabled {
		logger.Info(
			"audio cleanup disabled",
			slog.String("reason", "configuration_disabled"),
		)
		return nil, nil
	}

	store, err := factories.newStore(ctx, storageConfig)
	if err != nil {
		return nil, err
	}
	repository, err := factories.newRepository(databasePool)
	if err != nil {
		return nil, err
	}
	reclaimer, err := practicevoice.NewAudioAssetReclaimer(
		repository,
		store,
		practicevoice.NewAudioAssetSystemClock(),
	)
	if err != nil {
		return nil, err
	}
	return practicevoice.NewAudioAssetCleanupWorker(reclaimer, logger)
}
