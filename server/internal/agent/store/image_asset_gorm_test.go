package store

import (
	"errors"
	"testing"

	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
)

func TestNewGormImageAssetRepositoryRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := NewGormImageAssetRepository(nil); !errors.Is(
		err,
		agentimage.ErrRepository,
	) {
		t.Fatalf("NewGormImageAssetRepository error = %v", err)
	}
}

func TestNewGormImageAssetRepositoryFromPoolRequiresPool(t *testing.T) {
	t.Parallel()

	if _, err := NewGormImageAssetRepositoryFromPool(nil); !errors.Is(
		err,
		agentimage.ErrRepository,
	) {
		t.Fatalf("NewGormImageAssetRepositoryFromPool error = %v", err)
	}
}
