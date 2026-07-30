package main

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/ossstore"
)

func newAgentImageStore(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
) (objectstore.Store, error) {
	provider, err := ossstore.NewCredentialsProvider(storageConfig)
	if err != nil {
		return nil, err
	}
	return ossstore.NewForPrefix(
		ctx,
		storageConfig,
		storageConfig.ImagePrefix,
		provider,
	)
}
