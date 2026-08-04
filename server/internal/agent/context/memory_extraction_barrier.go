package context

import (
	stdcontext "context"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const MemoryExtractionBarrierPolicyV1 = "memory-extraction-barrier-v1"

const memoryExtractionBarrierNotRequired = "not_required"

type MemoryExtractionBarrierStatus string

const (
	MemoryExtractionBarrierReady  MemoryExtractionBarrierStatus = "ready"
	MemoryExtractionBarrierWaited MemoryExtractionBarrierStatus = "waited"
)

type MemoryExtractionBarrierRequest struct {
	Actor  requestcontext.Actor
	Cutoff time.Time
}

type MemoryExtractionBarrierResult struct {
	PolicyVersion  string
	Cutoff         time.Time
	Status         MemoryExtractionBarrierStatus
	Waited         time.Duration
	CoveredThrough time.Time
}

func (result MemoryExtractionBarrierResult) Valid() bool {
	if result.PolicyVersion != MemoryExtractionBarrierPolicyV1 ||
		result.Cutoff.IsZero() ||
		result.Cutoff.Location() != time.UTC ||
		result.Waited < 0 || result.Waited > 5*time.Second ||
		(!result.CoveredThrough.IsZero() &&
			(result.CoveredThrough.Location() != time.UTC ||
				result.CoveredThrough.After(result.Cutoff))) {
		return false
	}
	switch result.Status {
	case MemoryExtractionBarrierReady:
		return result.Waited == 0
	case MemoryExtractionBarrierWaited:
		return result.Waited > 0 && !result.CoveredThrough.IsZero()
	default:
		return false
	}
}

type MemoryExtractionBarrier interface {
	Await(
		stdcontext.Context,
		MemoryExtractionBarrierRequest,
	) (MemoryExtractionBarrierResult, error)
}
