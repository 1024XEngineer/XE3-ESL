package main

import (
	"context"
	"errors"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/kodostore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/ossstore"
)

type protectedObjectStore interface {
	objectstore.Store
	Open(context.Context, string) (io.ReadCloser, error)
}

func newProtectedObjectStore(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	prefix string,
) (protectedObjectStore, error) {
	switch storageConfig.Provider {
	case "", config.ObjectStorageProviderAliyunOSS:
		provider, err := ossstore.NewCredentialsProvider(storageConfig)
		if err != nil {
			return nil, err
		}
		return ossstore.NewForPrefix(ctx, storageConfig, prefix, provider)
	case config.ObjectStorageProviderQiniuKodo:
		return kodostore.NewForPrefix(ctx, storageConfig, prefix)
	default:
		return nil, errors.New("object storage provider is not registered")
	}
}
