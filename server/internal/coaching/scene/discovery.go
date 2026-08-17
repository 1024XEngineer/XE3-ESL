package scene

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const builtinDiscoverySchemaVersion = 1

//go:embed catalog.discovery.v1.json
var builtinDiscoveryData []byte

type ExperienceDiscoveryProfile struct {
	Experience              PracticeExperience `json:"practice_experience"`
	Aliases                 []string           `json:"aliases"`
	DefaultSceneID          string             `json:"default_scene_id"`
	DefaultPracticeOptionID string             `json:"default_practice_option_id"`
}

type SceneDiscoveryProfile struct {
	SceneID string   `json:"scene_id"`
	Aliases []string `json:"aliases"`
}

type discoveryDocument struct {
	SchemaVersion int                          `json:"schema_version"`
	Experiences   []ExperienceDiscoveryProfile `json:"experiences"`
	Scenes        []SceneDiscoveryProfile      `json:"scenes"`
}

func loadBuiltinDiscovery(catalog *Catalog) error {
	return loadDiscovery(bytes.NewReader(builtinDiscoveryData), catalog)
}

func loadDiscovery(reader io.Reader, catalog *Catalog) error {
	if reader == nil || catalog == nil {
		return invalidDefinition("discovery document and catalog are required")
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document discoveryDocument
	if err := decoder.Decode(&document); err != nil {
		return invalidDefinition("decode discovery document: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return invalidDefinition("discovery document contains trailing JSON content")
	}
	if document.SchemaVersion != builtinDiscoverySchemaVersion {
		return invalidDefinition(
			"unsupported discovery schema version %d",
			document.SchemaVersion,
		)
	}

	scenes := make(map[string]SceneDiscoveryProfile, len(document.Scenes))
	aliases := make(map[string]string)
	for _, profile := range document.Scenes {
		definition, found := catalog.scene(profile.SceneID)
		if !found || definition.Status != SceneStatusActive ||
			!validDiscoveryAliases(profile.Aliases) {
			return invalidDefinition("invalid discovery profile for scene %q", profile.SceneID)
		}
		if _, duplicate := scenes[profile.SceneID]; duplicate {
			return invalidDefinition("duplicate discovery profile for scene %q", profile.SceneID)
		}
		for _, alias := range append([]string{definition.Name}, profile.Aliases...) {
			normalized := normalizeDiscoveryText(alias)
			if owner, duplicate := aliases[normalized]; duplicate && owner != profile.SceneID {
				return invalidDefinition("discovery alias %q is shared by scenes %q and %q", alias, owner, profile.SceneID)
			}
			aliases[normalized] = profile.SceneID
		}
		profile.Aliases = append([]string(nil), profile.Aliases...)
		scenes[profile.SceneID] = profile
	}
	active, err := catalog.ListActiveScenes(context.Background())
	if err != nil || len(active) != len(scenes) {
		return invalidDefinition("every active scene must have one discovery profile")
	}

	experiences := make(map[PracticeExperience]ExperienceDiscoveryProfile, len(document.Experiences))
	for _, profile := range document.Experiences {
		definition, found := catalog.scene(profile.DefaultSceneID)
		if !found || definition.Status != SceneStatusActive ||
			definition.Experience != profile.Experience ||
			!validDiscoveryAliases(profile.Aliases) {
			return invalidDefinition("invalid discovery profile for experience %q", profile.Experience)
		}
		option, found := findPracticeOption(definition.PracticeOptions, profile.DefaultPracticeOptionID)
		if !found || (option.Mode != PracticeModeFullSimulation && option.Mode != PracticeModeFullMock) {
			return invalidDefinition("experience %q has invalid default practice option", profile.Experience)
		}
		if _, duplicate := experiences[profile.Experience]; duplicate {
			return invalidDefinition("duplicate discovery profile for experience %q", profile.Experience)
		}
		for _, alias := range profile.Aliases {
			normalized := normalizeDiscoveryText(alias)
			if _, duplicate := aliases[normalized]; duplicate {
				return invalidDefinition("experience discovery alias %q conflicts with a scene alias", alias)
			}
			aliases[normalized] = string(profile.Experience)
		}
		profile.Aliases = append([]string(nil), profile.Aliases...)
		experiences[profile.Experience] = profile
	}
	for _, experience := range []PracticeExperience{
		PracticeExperienceInterview,
		PracticeExperienceIELTSSpeaking,
		PracticeExperienceWorkplace,
		PracticeExperienceLifeAndTravel,
	} {
		if _, found := experiences[experience]; !found {
			return invalidDefinition("experience %q has no discovery profile", experience)
		}
	}
	catalog.sceneDiscovery = scenes
	catalog.experienceDiscovery = experiences
	return nil
}

func validDiscoveryAliases(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeDiscoveryText(value)
		if normalized == "" {
			return false
		}
		if _, duplicate := seen[normalized]; duplicate {
			return false
		}
		seen[normalized] = struct{}{}
	}
	return true
}

func normalizeDiscoveryText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
