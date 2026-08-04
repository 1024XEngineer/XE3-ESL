package postgres

import (
	"context"
	"errors"
	"testing"

	evaluationcore "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestIELTSSpeakingShadowStateRejectsNilRepository(t *testing.T) {
	t.Parallel()
	var repository *PostgresRepository
	_, err := repository.GetIELTSSpeakingShadowState(
		context.Background(),
		testOwnerA,
		"30000000-0000-4000-8000-000000000003",
		"40000000-0000-4000-8000-000000000004",
	)
	if !errors.Is(err, evaluationcore.ErrInvalidRequest) {
		t.Fatalf("nil repository error = %v", err)
	}
}
