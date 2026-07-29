package transport

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
)

func TestThreadResponseIncludesRequiredNullableTitle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	withoutTitle := threadResponse(core.Thread{
		ID:        "10000000-0000-4000-8000-000000000001",
		OwnerID:   "20000000-0000-4000-8000-000000000001",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if value, present := withoutTitle["title"]; !present || value != nil {
		t.Fatalf("empty Thread title = %#v, present = %t", value, present)
	}

	withTitle := threadResponse(core.Thread{
		ID:        "10000000-0000-4000-8000-000000000002",
		OwnerID:   "20000000-0000-4000-8000-000000000001",
		Title:     "英文自我介绍",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if withTitle["title"] != "英文自我介绍" {
		t.Fatalf("Thread title = %#v", withTitle["title"])
	}
}
