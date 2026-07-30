package persistence

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
)

func TestNewGormImageAssetRepositoryRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := NewGormImageAssetRepository(nil); !errors.Is(
		err,
		core.ErrRepository,
	) {
		t.Fatalf("NewGormImageAssetRepository error = %v", err)
	}
}

func TestNewGormImageAssetRepositoryFromPoolRequiresPool(t *testing.T) {
	t.Parallel()

	if _, err := NewGormImageAssetRepositoryFromPool(nil); !errors.Is(
		err,
		core.ErrRepository,
	) {
		t.Fatalf("NewGormImageAssetRepositoryFromPool error = %v", err)
	}
}
