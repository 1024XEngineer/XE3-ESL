package memory

const (
	CanonicalProfilePreferredName    = "profile.preferred_name"
	CanonicalPreferenceFormOfAddress = "preference.form_of_address"
	CanonicalProfileGender           = "profile.gender"
	CanonicalCareerOccupation        = "career.occupation"
	CanonicalCareerExperienceYears   = "career.experience_years"
	CanonicalCoachingStyle           = "coaching.style"
)

type StableProfileField struct {
	CanonicalKey string
	Type         Type
}

var stableProfileV1Fields = [...]StableProfileField{
	{
		CanonicalKey: CanonicalProfilePreferredName,
		Type:         TypeProfile,
	},
	{
		CanonicalKey: CanonicalPreferenceFormOfAddress,
		Type:         TypePreference,
	},
	{
		CanonicalKey: CanonicalProfileGender,
		Type:         TypeProfile,
	},
	{
		CanonicalKey: CanonicalCareerOccupation,
		Type:         TypeProfile,
	},
	{
		CanonicalKey: CanonicalCareerExperienceYears,
		Type:         TypeProfile,
	},
	{
		CanonicalKey: CanonicalCoachingStyle,
		Type:         TypePreference,
	},
}

func StableProfileV1Fields() []StableProfileField {
	fields := make([]StableProfileField, len(stableProfileV1Fields))
	copy(fields, stableProfileV1Fields[:])
	return fields
}

func ValidStableProfileCanonicalKeys(keys []string) bool {
	if len(keys) > len(stableProfileV1Fields) {
		return false
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, found := stableProfileField(key); !found {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func stableProfileField(canonicalKey string) (StableProfileField, bool) {
	for _, field := range stableProfileV1Fields {
		if field.CanonicalKey == canonicalKey {
			return field, true
		}
	}
	return StableProfileField{}, false
}

func ValidStableProfileMemories(items []Memory, ownerID string) bool {
	if !validUUID(ownerID) || len(items) > len(stableProfileV1Fields) {
		return false
	}
	previousPosition := -1
	for _, item := range items {
		if !item.Valid() ||
			item.OwnerID != ownerID ||
			item.Scope != ScopeUser ||
			item.MatterID != "" ||
			item.Status != StatusActive {
			return false
		}
		field, found := stableProfileField(item.CanonicalKey)
		if !found || field.Type != item.Type {
			return false
		}
		position := stableProfilePosition(item.CanonicalKey)
		if position <= previousPosition {
			return false
		}
		previousPosition = position
	}
	return true
}

func stableProfilePosition(canonicalKey string) int {
	for position, field := range stableProfileV1Fields {
		if field.CanonicalKey == canonicalKey {
			return position
		}
	}
	return -1
}
