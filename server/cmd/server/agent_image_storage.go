package main

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func newAgentImageStore(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
) (objectstore.Store, error) {
	return newProtectedObjectStore(
		ctx,
		storageConfig,
		storageConfig.ImagePrefix,
	)
}
