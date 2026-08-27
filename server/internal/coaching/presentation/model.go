// Package presentation owns the selectable coach avatars and voices together
// with each user's current presentation preference.
package presentation

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxOptionIDRunes        = 64
	MaxDisplayNameRunes     = 64
	MaxDescriptionRunes     = 200
	MaxPreviewAssetKeyRunes = 128
	MaxLocaleRunes          = 32
	MaxProviderValueRunes   = 128
)

var (
	ErrInvalidRequest  = errors.New("coach presentation: invalid request")
	ErrNotFound        = errors.New("coach presentation: not found")
	ErrVersionConflict = errors.New("coach presentation: version conflict")
	ErrRepository      = errors.New("coach presentation repository: operation failed")

	optionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

type AvatarOption struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	PreviewAssetKey  string `json:"preview_asset_key"`
	Provider         string `json:"-"`
	ProviderProfile  string `json:"-"`
	ProviderAvatarID string `json:"-"`
	BindingVersion   int64  `json:"-"`
	SortOrder        int    `json:"-"`
	Default          bool   `json:"-"`
}

func (option AvatarOption) Valid() bool {
	return validOptionID(option.ID) &&
		validText(option.DisplayName, MaxDisplayNameRunes) &&
		validText(option.Description, MaxDescriptionRunes) &&
		validToken(option.PreviewAssetKey, MaxPreviewAssetKeyRunes) &&
		validToken(option.Provider, MaxProviderValueRunes) &&
		validToken(option.ProviderProfile, MaxProviderValueRunes) &&
		validToken(option.ProviderAvatarID, MaxProviderValueRunes) &&
		option.BindingVersion > 0 && option.SortOrder >= 0
}

type VoiceOption struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Description     string `json:"description"`
	Locale          string `json:"locale"`
	Gender          string `json:"gender"`
	Provider        string `json:"-"`
	ProviderProfile string `json:"-"`
	ProviderModel   string `json:"-"`
	ProviderVoiceID string `json:"-"`
	BindingVersion  int64  `json:"-"`
	SortOrder       int    `json:"-"`
	Default         bool   `json:"-"`
}

func (option VoiceOption) Valid() bool {
	return validOptionID(option.ID) &&
		validText(option.DisplayName, MaxDisplayNameRunes) &&
		validText(option.Description, MaxDescriptionRunes) &&
		validToken(option.Locale, MaxLocaleRunes) &&
		(option.Gender == "female" || option.Gender == "male") &&
		validToken(option.Provider, MaxProviderValueRunes) &&
		validToken(option.ProviderProfile, MaxProviderValueRunes) &&
		validToken(option.ProviderModel, MaxProviderValueRunes) &&
		validToken(option.ProviderVoiceID, MaxProviderValueRunes) &&
		option.BindingVersion > 0 && option.SortOrder >= 0
}

type ResolvedSelection struct {
	Avatar AvatarOption
	Voice  VoiceOption
}

func (selection ResolvedSelection) Valid() bool {
	return selection.Avatar.Valid() && selection.Voice.Valid()
}

type Catalog struct {
	Avatars               []AvatarOption
	Voices                []VoiceOption
	DefaultAvatarOptionID string
	DefaultVoiceOptionID  string
}

func (catalog Catalog) Valid() bool {
	if len(catalog.Avatars) == 0 || len(catalog.Voices) == 0 ||
		!validOptionID(catalog.DefaultAvatarOptionID) ||
		!validOptionID(catalog.DefaultVoiceOptionID) {
		return false
	}
	avatarIDs := make(map[string]struct{}, len(catalog.Avatars))
	voiceIDs := make(map[string]struct{}, len(catalog.Voices))
	defaultAvatars := 0
	defaultVoices := 0
	for _, option := range catalog.Avatars {
		if !option.Valid() {
			return false
		}
		if _, duplicate := avatarIDs[option.ID]; duplicate {
			return false
		}
		avatarIDs[option.ID] = struct{}{}
		if option.Default {
			defaultAvatars++
			if option.ID != catalog.DefaultAvatarOptionID {
				return false
			}
		}
	}
	for _, option := range catalog.Voices {
		if !option.Valid() {
			return false
		}
		if _, duplicate := voiceIDs[option.ID]; duplicate {
			return false
		}
		voiceIDs[option.ID] = struct{}{}
		if option.Default {
			defaultVoices++
			if option.ID != catalog.DefaultVoiceOptionID {
				return false
			}
		}
	}
	return defaultAvatars == 1 && defaultVoices == 1
}

func (catalog Catalog) Contains(avatarOptionID, voiceOptionID string) bool {
	avatarFound := false
	voiceFound := false
	for _, option := range catalog.Avatars {
		avatarFound = avatarFound || option.ID == avatarOptionID
	}
	for _, option := range catalog.Voices {
		voiceFound = voiceFound || option.ID == voiceOptionID
	}
	return avatarFound && voiceFound
}

type Preference struct {
	UserID         string
	AvatarOptionID string    `json:"avatar_option_id"`
	VoiceOptionID  string    `json:"voice_option_id"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

func DefaultPreference(userID string, catalog Catalog) Preference {
	return Preference{
		UserID:         userID,
		AvatarOptionID: catalog.DefaultAvatarOptionID,
		VoiceOptionID:  catalog.DefaultVoiceOptionID,
	}
}

func (preference Preference) ValidLogical() bool {
	if !validToken(preference.UserID, MaxProviderValueRunes) ||
		!validOptionID(preference.AvatarOptionID) ||
		!validOptionID(preference.VoiceOptionID) || preference.Version < 0 {
		return false
	}
	if preference.Version == 0 {
		return preference.CreatedAt.IsZero() && preference.UpdatedAt.IsZero()
	}
	return !preference.CreatedAt.IsZero() && !preference.UpdatedAt.IsZero() &&
		preference.CreatedAt.Location() == time.UTC &&
		preference.UpdatedAt.Location() == time.UTC &&
		!preference.UpdatedAt.Before(preference.CreatedAt)
}

func (preference Preference) ValidForPersistence() bool {
	return preference.ValidLogical() && preference.Version > 0
}

type UpdateCommand struct {
	AvatarOptionID  string
	VoiceOptionID   string
	ExpectedVersion int64
}

func (command UpdateCommand) Valid() bool {
	return validOptionID(command.AvatarOptionID) &&
		validOptionID(command.VoiceOptionID) && command.ExpectedVersion >= 0
}

func validOptionID(value string) bool {
	return utf8.ValidString(value) && optionIDPattern.MatchString(value)
}

func validToken(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum &&
		!strings.ContainsAny(value, " \t\r\n")
}

func validText(value string, maximum int) bool {
	if value == "" || value != strings.TrimSpace(value) ||
		!utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
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
