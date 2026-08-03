package bootstrap

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
)

type agentStableProfileReader struct {
	reader memory.StableProfileReader
}

func newAgentStableProfileReader(
	reader memory.StableProfileReader,
) (*agentStableProfileReader, error) {
	if reader == nil {
		return nil, errors.New(
			"bootstrap: Stable Profile read dependency is required",
		)
	}
	return &agentStableProfileReader{reader: reader}, nil
}

func (reader *agentStableProfileReader) ReadStableProfile(
	ctx context.Context,
	request agentruntime.StableProfileReadRequest,
) ([]agentruntime.StableProfileMemory, error) {
	if reader == nil || reader.reader == nil {
		return nil, errors.New(
			"bootstrap: Stable Profile read dependency is required",
		)
	}
	if ctx == nil || !request.Valid() {
		return nil, memory.ErrInvalidArgument
	}
	items, err := reader.reader.ListStableProfile(ctx, request.Actor)
	if err != nil {
		return nil, err
	}
	if !memory.ValidStableProfileMemories(items, request.Actor.UserID) {
		return nil, memory.ErrRepository
	}
	result := make([]agentruntime.StableProfileMemory, 0, len(items))
	for _, item := range items {
		mapped := agentruntime.StableProfileMemory{
			MemoryID:      item.ID,
			MemoryVersion: item.Version,
			CanonicalKey:  item.CanonicalKey,
			Type:          string(item.Type),
			Content:       item.Content,
			Scope:         string(item.Scope),
		}
		if !mapped.Valid() {
			return nil, memory.ErrRepository
		}
		result = append(result, mapped)
	}
	return result, nil
}

var _ agentruntime.StableProfileReader = (*agentStableProfileReader)(nil)
