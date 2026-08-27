package practice

import "strings"

const PresentationSnapshotSchemaVersion = 1

// PresentationSnapshot freezes the provider bindings selected for one
// Practice Session. Runtime consumers must not rebuild it from current user
// preferences or the mutable option catalog.
type PresentationSnapshot struct {
	SchemaVersion int                        `json:"schema_version"`
	Avatar        AvatarPresentationSnapshot `json:"avatar"`
	Voice         VoicePresentationSnapshot  `json:"voice"`
}

type AvatarPresentationSnapshot struct {
	OptionID         string `json:"option_id"`
	Provider         string `json:"provider"`
	ProviderProfile  string `json:"provider_profile"`
	ProviderAvatarID string `json:"provider_avatar_id"`
	BindingVersion   int64  `json:"binding_version"`
}

type VoicePresentationSnapshot struct {
	OptionID        string `json:"option_id"`
	Provider        string `json:"provider"`
	ProviderProfile string `json:"provider_profile"`
	ProviderModel   string `json:"provider_model"`
	ProviderVoiceID string `json:"provider_voice_id"`
	Locale          string `json:"locale"`
	BindingVersion  int64  `json:"binding_version"`
}

func (snapshot PresentationSnapshot) Valid() bool {
	return snapshot.SchemaVersion == PresentationSnapshotSchemaVersion &&
		validPresentationValue(snapshot.Avatar.OptionID) &&
		validPresentationValue(snapshot.Avatar.Provider) &&
		validPresentationValue(snapshot.Avatar.ProviderProfile) &&
		validPresentationValue(snapshot.Avatar.ProviderAvatarID) &&
		snapshot.Avatar.BindingVersion > 0 &&
		validPresentationValue(snapshot.Voice.OptionID) &&
		validPresentationValue(snapshot.Voice.Provider) &&
		validPresentationValue(snapshot.Voice.ProviderProfile) &&
		validPresentationValue(snapshot.Voice.ProviderModel) &&
		validPresentationValue(snapshot.Voice.ProviderVoiceID) &&
		validPresentationValue(snapshot.Voice.Locale) &&
		snapshot.Voice.BindingVersion > 0
}

func validPresentationValue(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		!strings.ContainsAny(value, " \t\r\n") && len(value) <= 128
}
