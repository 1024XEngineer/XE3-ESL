package main

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func newAgentImageStore(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	observer providerobservability.Recorder,
) (objectstore.Store, error) {
	return newProtectedObjectStore(
		ctx,
		storageConfig,
		storageConfig.ImagePrefix,
		observer,
	)
}
