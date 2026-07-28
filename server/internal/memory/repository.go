package memory

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrInvalidArgument    = errors.New("memory: invalid argument")
	ErrNotFound           = errors.New("memory: not found")
	ErrConflict           = errors.New("memory: conflict")
	ErrAccountDeleted     = errors.New("memory: account is not active")
	ErrDeletionGeneration = errors.New("memory: stale deletion generation")
	ErrRepository         = errors.New("memory repository: operation failed")
)

type CreateCommand struct {
	Type          Type
	CanonicalKey  string
	Content       string
	Scope         ScopeType
	MatterID      string
	PolicyVersion string
	ExpiresAt     *time.Time
	Source        SourceInput
}

func (command CreateCommand) Valid() bool {
	return command.Type.Valid() &&
		validCanonicalKey(command.CanonicalKey) &&
		validContent(command.Content) &&
		validScope(command.Scope, command.MatterID) &&
		validPolicyVersion(command.PolicyVersion) &&
		validOptionalTime(command.ExpiresAt) &&
		command.Source.Valid()
}

type ScopeFilter struct {
	Scope    ScopeType
	MatterID string
	Limit    int
}

func (filter ScopeFilter) Valid() bool {
	return validScope(filter.Scope, filter.MatterID) &&
		filter.Limit >= 1 &&
		filter.Limit <= 100
}

type UpdateCommand struct {
	MemoryID        string
	ExpectedVersion int64
	Content         string
	PolicyVersion   string
	ExpiresAt       *time.Time
	Source          SourceInput
}

func (command UpdateCommand) Valid() bool {
	return validUUID(command.MemoryID) &&
		command.ExpectedVersion > 0 &&
		validContent(command.Content) &&
		validPolicyVersion(command.PolicyVersion) &&
		validOptionalTime(command.ExpiresAt) &&
		command.Source.Valid()
}

type InactivateCommand struct {
	MemoryID        string
	ExpectedVersion int64
	Source          SourceInput
}

func (command InactivateCommand) Valid() bool {
	return validUUID(command.MemoryID) &&
		command.ExpectedVersion > 0 &&
		command.Source.Valid()
}

type DeleteOwnerCommand struct {
	UserID     string
	Generation uint64
}

func (command DeleteOwnerCommand) Valid() bool {
	return validUUID(command.UserID) &&
		command.Generation > 0 &&
		command.Generation <= uint64(^uint64(0)>>1)
}

type Repository interface {
	Create(
		context.Context,
		requestcontext.Actor,
		CreateCommand,
	) (Memory, error)
	Find(
		context.Context,
		requestcontext.Actor,
		string,
	) (Memory, error)
	ListActive(
		context.Context,
		requestcontext.Actor,
		ScopeFilter,
	) ([]Memory, error)
	ListSources(
		context.Context,
		requestcontext.Actor,
		string,
	) ([]Source, error)
	Update(
		context.Context,
		requestcontext.Actor,
		UpdateCommand,
	) (Memory, error)
	Inactivate(
		context.Context,
		requestcontext.Actor,
		InactivateCommand,
	) (Memory, error)
	Delete(
		context.Context,
		requestcontext.Actor,
		string,
	) error
	DeleteOwnerData(context.Context, DeleteOwnerCommand) error
}

type IDGenerator interface {
	NewID() (string, error)
}
