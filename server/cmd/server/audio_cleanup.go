package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
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
	) (conversation.AudioAssetLifecycleRepository, error)
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
	) (conversation.AudioAssetLifecycleRepository, error) {
		return conversationpostgres.NewAudioAssetRepository(pool)
	},
}

func buildAudioCleanupWorker(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	databasePool *pgxpool.Pool,
	logger *slog.Logger,
	factories audioCleanupFactories,
) (*conversation.AudioAssetCleanupWorker, error) {
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
	reclaimer, err := conversation.NewAudioAssetReclaimer(
		repository,
		store,
		conversation.NewAudioAssetSystemClock(),
	)
	if err != nil {
		return nil, err
	}
	return conversation.NewAudioAssetCleanupWorker(reclaimer, logger)
}
