// Package profile owns the small, structured set of user facts and coaching
// preferences that may be reused across Agent conversations.
package profile

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxFormOfAddressRunes       = 64
	MaxOccupationRunes          = 120
	MaxProfessionalContextRunes = 500
	MaxLanguageRunes            = 64
	MaxInterests                = 8
	MaxInterestRunes            = 64
	MaxEvidenceRunes            = 500
)

var (
	ErrInvalidRequest  = errors.New("coaching profile: invalid request")
	ErrNotFound        = errors.New("coaching profile: not found")
	ErrVersionConflict = errors.New("coaching profile: version conflict")
	ErrRepository      = errors.New("coaching profile repository: operation failed")
)

type Field string

const (
	FieldFormOfAddress       Field = "form_of_address"
	FieldOccupation          Field = "occupation"
	FieldProfessionalContext Field = "professional_context"
	FieldNativeLanguage      Field = "native_language"
	FieldExplanationLanguage Field = "explanation_language"
	FieldResponseDetail      Field = "response_detail"
	FieldInterests           Field = "interests"
)

var fields = [...]Field{
	FieldFormOfAddress,
	FieldOccupation,
	FieldProfessionalContext,
	FieldNativeLanguage,
	FieldExplanationLanguage,
	FieldResponseDetail,
	FieldInterests,
}

func Fields() []Field {
	result := make([]Field, len(fields))
	copy(result, fields[:])
	return result
}

func (field Field) Valid() bool {
	for _, candidate := range fields {
		if field == candidate {
			return true
		}
	}
	return false
}

type ResponseDetail string

const (
	ResponseConcise  ResponseDetail = "CONCISE"
	ResponseBalanced ResponseDetail = "BALANCED"
	ResponseDetailed ResponseDetail = "DETAILED"
)

func (value ResponseDetail) Valid() bool {
	return value == ResponseConcise || value == ResponseBalanced ||
		value == ResponseDetailed
}

type Data struct {
	FormOfAddress       string         `json:"form_of_address,omitempty"`
	Occupation          string         `json:"occupation,omitempty"`
	ProfessionalContext string         `json:"professional_context,omitempty"`
	NativeLanguage      string         `json:"native_language,omitempty"`
	ExplanationLanguage string         `json:"explanation_language,omitempty"`
	ResponseDetail      ResponseDetail `json:"response_detail,omitempty"`
	Interests           []string       `json:"interests,omitempty"`
}

func (data Data) Valid() bool {
	if !validOptionalText(data.FormOfAddress, MaxFormOfAddressRunes) ||
		!validOptionalText(data.Occupation, MaxOccupationRunes) ||
		!validOptionalText(data.ProfessionalContext, MaxProfessionalContextRunes) ||
		!validOptionalText(data.NativeLanguage, MaxLanguageRunes) ||
		!validOptionalText(data.ExplanationLanguage, MaxLanguageRunes) ||
		(data.ResponseDetail != "" && !data.ResponseDetail.Valid()) ||
		len(data.Interests) > MaxInterests {
		return false
	}
	seen := make(map[string]struct{}, len(data.Interests))
	for _, interest := range data.Interests {
		if !validRequiredText(interest, MaxInterestRunes) {
			return false
		}
		key := strings.ToLower(interest)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func (data Data) Empty() bool {
	return data.FormOfAddress == "" && data.Occupation == "" &&
		data.ProfessionalContext == "" && data.NativeLanguage == "" &&
		data.ExplanationLanguage == "" && data.ResponseDetail == "" &&
		len(data.Interests) == 0
}

func (data Data) Has(field Field) bool {
	switch field {
	case FieldFormOfAddress:
		return data.FormOfAddress != ""
	case FieldOccupation:
		return data.Occupation != ""
	case FieldProfessionalContext:
		return data.ProfessionalContext != ""
	case FieldNativeLanguage:
		return data.NativeLanguage != ""
	case FieldExplanationLanguage:
		return data.ExplanationLanguage != ""
	case FieldResponseDetail:
		return data.ResponseDetail != ""
	case FieldInterests:
		return len(data.Interests) > 0
	default:
		return false
	}
}

type SourceType string

const (
	SourceUserSetting                 SourceType = "user_setting"
	SourceExplicitCurrentFact         SourceType = "explicit_current_fact"
	SourceExplicitRememberInstruction SourceType = "explicit_remember_instruction"
)

func (source SourceType) Valid() bool {
	return source == SourceUserSetting || source == SourceExplicitCurrentFact ||
		source == SourceExplicitRememberInstruction
}

type FieldSource struct {
	Type       SourceType `json:"type"`
	MessageID  string     `json:"message_id,omitempty"`
	RecordedAt time.Time  `json:"recorded_at"`
}

func (source FieldSource) Valid() bool {
	if !source.Type.Valid() || source.RecordedAt.IsZero() ||
		source.RecordedAt.Location() != time.UTC {
		return false
	}
	if source.Type == SourceUserSetting {
		return source.MessageID == ""
	}
	return validIdentifier(source.MessageID)
}

type Profile struct {
	UserID        string                `json:"user_id"`
	MemoryEnabled bool                  `json:"memory_enabled"`
	Data          Data                  `json:"profile"`
	FieldSources  map[Field]FieldSource `json:"field_sources"`
	Version       int64                 `json:"version"`
	CreatedAt     time.Time             `json:"created_at,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at,omitempty"`
}

func Empty(userID string) Profile {
	return Profile{
		UserID:        userID,
		MemoryEnabled: true,
		FieldSources:  map[Field]FieldSource{},
	}
}

func (profile Profile) ValidStored() bool {
	return validIdentifier(profile.UserID) && profile.Data.Valid() &&
		profile.Version > 0 && !profile.CreatedAt.IsZero() &&
		!profile.UpdatedAt.IsZero() &&
		profile.CreatedAt.Location() == time.UTC &&
		profile.UpdatedAt.Location() == time.UTC &&
		!profile.UpdatedAt.Before(profile.CreatedAt) &&
		validSources(profile.Data, profile.FieldSources)
}

func validSources(data Data, sources map[Field]FieldSource) bool {
	if sources == nil {
		return false
	}
	for _, field := range fields {
		source, exists := sources[field]
		if data.Has(field) != exists || exists && !source.Valid() {
			return false
		}
	}
	for field := range sources {
		if !field.Valid() {
			return false
		}
	}
	return true
}

func validRequiredText(value string, maximum int) bool {
	return value != "" && validOptionalText(value, maximum)
}

func validOptionalText(value string, maximum int) bool {
	if value == "" {
		return true
	}
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) ||
		utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Zl, character) ||
			unicode.Is(unicode.Zp, character) {
			return false
		}
	}
	return true
}

func ValidEvidence(value string) bool {
	return validRequiredText(value, MaxEvidenceRunes)
}

func validIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.RuneCountInString(value) <= 128 &&
		!strings.ContainsAny(value, " \t\r\n")
}
