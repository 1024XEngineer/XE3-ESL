package practice

import (
	"strings"
	"testing"
)

func TestPresentationSnapshotValidatesEveryFrozenBinding(t *testing.T) {
	valid := PresentationSnapshot{
		SchemaVersion: PresentationSnapshotSchemaVersion,
		Avatar: AvatarPresentationSnapshot{
			OptionID: "avatar_lisa", Provider: "spatialreal",
			ProviderProfile: "spatialreal_default", ProviderAvatarID: "avatar-1",
			BindingVersion: 1,
		},
		Voice: VoicePresentationSnapshot{
			OptionID: "voice_ava", Provider: "qianwen",
			ProviderProfile: "qianwen_default", ProviderModel: "cosyvoice-v3-flash",
			ProviderVoiceID: "loongeva_v3.6", Locale: "en-US", BindingVersion: 1,
		},
	}
	if !valid.Valid() {
		t.Fatal("expected complete presentation snapshot to be valid")
	}

	tests := map[string]func(*PresentationSnapshot){
		"schema":          func(value *PresentationSnapshot) { value.SchemaVersion = 2 },
		"avatar option":   func(value *PresentationSnapshot) { value.Avatar.OptionID = "" },
		"avatar provider": func(value *PresentationSnapshot) { value.Avatar.Provider = "bad value" },
		"avatar profile":  func(value *PresentationSnapshot) { value.Avatar.ProviderProfile = " profile" },
		"avatar id":       func(value *PresentationSnapshot) { value.Avatar.ProviderAvatarID = "avatar\n1" },
		"avatar binding":  func(value *PresentationSnapshot) { value.Avatar.BindingVersion = 0 },
		"voice option":    func(value *PresentationSnapshot) { value.Voice.OptionID = "" },
		"voice provider":  func(value *PresentationSnapshot) { value.Voice.Provider = "qian wen" },
		"voice profile":   func(value *PresentationSnapshot) { value.Voice.ProviderProfile = "" },
		"voice model":     func(value *PresentationSnapshot) { value.Voice.ProviderModel = " model" },
		"voice id":        func(value *PresentationSnapshot) { value.Voice.ProviderVoiceID = "" },
		"voice locale":    func(value *PresentationSnapshot) { value.Voice.Locale = "en US" },
		"voice binding":   func(value *PresentationSnapshot) { value.Voice.BindingVersion = -1 },
		"value too long": func(value *PresentationSnapshot) {
			value.Voice.ProviderModel = strings.Repeat("m", 129)
		},
	}
	for name, invalidate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			invalidate(&candidate)
			if candidate.Valid() {
				t.Fatal("expected invalid presentation snapshot")
			}
		})
	}
}
