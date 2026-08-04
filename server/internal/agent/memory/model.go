package memory

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	maxCanonicalKeyBytes  = 128
	maxContentRunes       = 4096
	maxContentBytes       = 16384
	maxPolicyVersionBytes = 64
	maxSourceIDBytes      = 128
)

var (
	canonicalKeyPattern = regexp.MustCompile(
		`^[a-z][a-z0-9._:-]{0,127}$`,
	)
	policyVersionPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
	)
)

type Type string

const (
	TypeIdentity   Type = "identity"
	TypeProfile    Type = "profile"
	TypePreference Type = "preference"
	TypeGoal       Type = "goal"
	TypeInterest   Type = "interest"
	TypeTopic      Type = "topic"
)

func (memoryType Type) Valid() bool {
	switch memoryType {
	case TypeIdentity,
		TypeProfile,
		TypePreference,
		TypeGoal,
		TypeInterest,
		TypeTopic:
		return true
	default:
		return false
	}
}

type ScopeType string

const (
	ScopeUser ScopeType = "user"
	ScopeGoal ScopeType = "goal"
)

func (scope ScopeType) Valid() bool {
	return scope == ScopeUser || scope == ScopeGoal
}

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

func (status Status) Valid() bool {
	return status == StatusActive || status == StatusInactive
}

type SourceType string

const (
	SourceAgentMessage    SourceType = "agent_message"
	SourceAgentRun        SourceType = "agent_run"
	SourcePracticeSession SourceType = "practice_session"
)

func (sourceType SourceType) Valid() bool {
	switch sourceType {
	case SourceAgentMessage,
		SourceAgentRun,
		SourcePracticeSession:
		return true
	default:
		return false
	}
}

type Memory struct {
	ID            string
	OwnerID       string
	Type          Type
	CanonicalKey  string
	Content       string
	Scope         ScopeType
	GoalID        string
	Status        Status
	Version       int64
	PolicyVersion string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	InactivatedAt *time.Time
}

func (item Memory) Valid() bool {
	return validUUID(item.ID) &&
		validUUID(item.OwnerID) &&
		item.Type.Valid() &&
		validCanonicalKey(item.CanonicalKey) &&
		validContent(item.Content) &&
		validScope(item.Scope, item.GoalID) &&
		item.Status.Valid() &&
		item.Version > 0 &&
		validPolicyVersion(item.PolicyVersion) &&
		validOptionalTime(item.ExpiresAt) &&
		!item.CreatedAt.IsZero() &&
		!item.UpdatedAt.IsZero() &&
		validLifecycle(item)
}

type Source struct {
	ID        string
	OwnerID   string
	MemoryID  string
	Type      SourceType
	SourceID  string
	Version   int64
	Checksum  [sha256.Size]byte
	CreatedAt time.Time
}

func (source Source) Valid() bool {
	return validUUID(source.ID) &&
		validUUID(source.OwnerID) &&
		validUUID(source.MemoryID) &&
		source.Input().Valid() &&
		!source.CreatedAt.IsZero()
}

type SourceInput struct {
	Type     SourceType
	SourceID string
	Version  int64
	Checksum [sha256.Size]byte
}

func (source SourceInput) Valid() bool {
	return source.Type.Valid() &&
		validSourceID(source.SourceID) &&
		source.Version > 0
}

func (source Source) Input() SourceInput {
	return SourceInput{
		Type:     source.Type,
		SourceID: source.SourceID,
		Version:  source.Version,
		Checksum: source.Checksum,
	}
}

func validUUID(value string) bool {
	var identifier pgtype.UUID
	return identifier.Scan(value) == nil && identifier.Valid
}

func validCanonicalKey(value string) bool {
	return len(value) <= maxCanonicalKeyBytes &&
		canonicalKeyPattern.MatchString(value)
}

func validContent(value string) bool {
	return value == strings.TrimSpace(value) &&
		value != "" &&
		len([]rune(value)) <= maxContentRunes &&
		len(value) <= maxContentBytes
}

func validPolicyVersion(value string) bool {
	return len(value) <= maxPolicyVersionBytes &&
		policyVersionPattern.MatchString(value)
}

func validSourceID(value string) bool {
	return value == strings.TrimSpace(value) &&
		value != "" &&
		len(value) <= maxSourceIDBytes &&
		!strings.ContainsAny(value, " \t\r\n")
}

func validScope(scope ScopeType, goalID string) bool {
	if !scope.Valid() {
		return false
	}
	if scope == ScopeUser {
		return goalID == ""
	}
	return validUUID(goalID)
}

func validOptionalTime(value *time.Time) bool {
	return value == nil || !value.IsZero()
}

func validLifecycle(item Memory) bool {
	if item.UpdatedAt.Before(item.CreatedAt) {
		return false
	}
	if item.ExpiresAt != nil && !item.ExpiresAt.After(item.CreatedAt) {
		return false
	}
	if item.Status == StatusActive {
		return item.InactivatedAt == nil
	}
	return item.InactivatedAt != nil &&
		!item.InactivatedAt.Before(item.CreatedAt) &&
		!item.UpdatedAt.Before(*item.InactivatedAt)
}
