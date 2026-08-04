package memory

import (
	"testing"
	"time"
)

func TestStableProfileV1FieldsAreFixedAndCopied(t *testing.T) {
	t.Parallel()
	fields := StableProfileV1Fields()
	expected := []StableProfileField{
		{CanonicalKey: CanonicalProfilePreferredName, Type: TypeProfile},
		{
			CanonicalKey: CanonicalPreferenceFormOfAddress,
			Type:         TypePreference,
		},
		{CanonicalKey: CanonicalProfileGender, Type: TypeProfile},
		{CanonicalKey: CanonicalCareerOccupation, Type: TypeProfile},
		{CanonicalKey: CanonicalCareerExperienceYears, Type: TypeProfile},
		{CanonicalKey: CanonicalCoachingStyle, Type: TypePreference},
	}
	if len(fields) != len(expected) {
		t.Fatalf("Stable Profile field count = %d", len(fields))
	}
	for index := range expected {
		if fields[index] != expected[index] {
			t.Fatalf("Stable Profile field %d = %#v", index, fields[index])
		}
	}
	fields[0].CanonicalKey = "profile.mutated"
	if StableProfileV1Fields()[0].CanonicalKey !=
		CanonicalProfilePreferredName {
		t.Fatal("Stable Profile fields exposed mutable policy state")
	}
}

func TestValidStableProfileMemoriesRequiresWhitelistAndOrder(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ownerID := "10000000-0000-4000-8000-000000000001"
	preferredName := stableProfileMemory(
		"20000000-0000-4000-8000-000000000001",
		ownerID,
		TypeProfile,
		CanonicalProfilePreferredName,
		"小花",
		now,
	)
	occupation := stableProfileMemory(
		"20000000-0000-4000-8000-000000000002",
		ownerID,
		TypeProfile,
		CanonicalCareerOccupation,
		"Java 后端工程师",
		now,
	)
	if !ValidStableProfileMemories(
		[]Memory{preferredName, occupation},
		ownerID,
	) {
		t.Fatal("valid Stable Profile was rejected")
	}
	for name, mutate := range map[string]func([]Memory) []Memory{
		"reordered": func(items []Memory) []Memory {
			return []Memory{items[1], items[0]}
		},
		"duplicate": func(items []Memory) []Memory {
			return []Memory{items[0], items[0]}
		},
		"unknown key": func(items []Memory) []Memory {
			items[0].CanonicalKey = "profile.unapproved"
			return items
		},
		"incompatible type": func(items []Memory) []Memory {
			items[0].Type = TypePreference
			return items
		},
		"wrong owner": func(items []Memory) []Memory {
			items[0].OwnerID = "30000000-0000-4000-8000-000000000001"
			return items
		},
		"goal scope": func(items []Memory) []Memory {
			items[0].Scope = ScopeGoal
			items[0].GoalID = "40000000-0000-4000-8000-000000000001"
			return items
		},
		"inactive": func(items []Memory) []Memory {
			inactivatedAt := now.Add(time.Second)
			items[0].Status = StatusInactive
			items[0].Version++
			items[0].UpdatedAt = inactivatedAt
			items[0].InactivatedAt = &inactivatedAt
			return items
		},
	} {
		t.Run(name, func(t *testing.T) {
			items := []Memory{preferredName, occupation}
			if ValidStableProfileMemories(mutate(items), ownerID) {
				t.Fatalf("%s Stable Profile was accepted", name)
			}
		})
	}
}

func stableProfileMemory(
	id string,
	ownerID string,
	memoryType Type,
	canonicalKey string,
	content string,
	now time.Time,
) Memory {
	return Memory{
		ID:            id,
		OwnerID:       ownerID,
		Type:          memoryType,
		CanonicalKey:  canonicalKey,
		Content:       content,
		Scope:         ScopeUser,
		Status:        StatusActive,
		Version:       1,
		PolicyVersion: "memory-v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}
