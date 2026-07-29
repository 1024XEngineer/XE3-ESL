package runtime

import (
	"context"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var stableProfileCanonicalKeyPattern = regexp.MustCompile(
	`^[a-z][a-z0-9._:-]{0,127}$`,
)

type StableProfileReadRequest struct {
	Actor requestcontext.Actor
}

func (request StableProfileReadRequest) Valid() bool {
	return request.Actor.Valid()
}

type StableProfileMemory struct {
	MemoryID      string
	MemoryVersion int64
	CanonicalKey  string
	Type          string
	Content       string
	Scope         string
}

func (item StableProfileMemory) Valid() bool {
	return core.ValidUUID(item.MemoryID) &&
		item.MemoryVersion > 0 &&
		stableProfileCanonicalKeyPattern.MatchString(item.CanonicalKey) &&
		item.Type != "" &&
		item.Type == strings.TrimSpace(item.Type) &&
		len(item.Type) <= 32 &&
		item.Content != "" &&
		item.Content == strings.TrimSpace(item.Content) &&
		len(item.Content) <= 16384 &&
		item.Scope == memoryScopeUser
}

type StableProfileReader interface {
	ReadStableProfile(
		context.Context,
		StableProfileReadRequest,
	) ([]StableProfileMemory, error)
}
