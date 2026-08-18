package scene

import (
	"context"
	"sort"
)

const catalogManifestSchemaVersion = 1

// PreviewCatalog is the server-authoritative boundary exposed to practice
// preview. The caller may choose a Catalog scene only by its stable ID.
type PreviewCatalog interface {
	ResolvePreviewCatalogSelection(
		context.Context,
		string,
	) (PreviewCatalogSelection, error)
	PreviewCatalogManifest(context.Context) (CatalogManifest, error)
}

// PreviewCatalogSelection materializes the immutable Scene definition and the
// server-owned defaults used when a Catalog scene is selected.
type PreviewCatalogSelection struct {
	Scene          SceneDefinition
	DefaultRoleIDs []string
	DefaultOption  PracticeOption
}

func (selection PreviewCatalogSelection) Valid() bool {
	if validateScene(
		selection.Scene,
		make(map[string]struct{}),
		make(map[string]struct{}),
		make(map[string]struct{}),
	) != nil || selection.Scene.Status != SceneStatusActive ||
		len(selection.Scene.Roles) == 0 ||
		len(selection.DefaultRoleIDs) != 1 ||
		selection.DefaultRoleIDs[0] != selection.Scene.Roles[0].ID {
		return false
	}
	if _, found := findRole(
		selection.Scene.Roles,
		selection.DefaultRoleIDs[0],
	); !found {
		return false
	}
	option, found := findPracticeOption(
		selection.Scene.PracticeOptions,
		selection.DefaultOption.ID,
	)
	return found && option == selection.DefaultOption &&
		(option.Mode == PracticeModeFullSimulation ||
			option.Mode == PracticeModeFullMock)
}

type CatalogManifest struct {
	SchemaVersion int                         `json:"schema_version"`
	Experiences   []CatalogExperienceManifest `json:"experiences"`
	Scenes        []CatalogSceneManifest      `json:"scenes"`
}

type CatalogExperienceManifest struct {
	Experience              PracticeExperience `json:"practice_experience"`
	Aliases                 []string           `json:"aliases"`
	DefaultSceneID          string             `json:"default_scene_id"`
	DefaultPracticeOptionID string             `json:"default_practice_option_id"`
}

type CatalogSceneManifest struct {
	SceneID            string             `json:"scene_id"`
	SceneVersion       int                `json:"scene_version"`
	PracticeExperience PracticeExperience `json:"practice_experience"`
	Name               string             `json:"name"`
	Aliases            []string           `json:"aliases"`
	PublicSceneBrief   string             `json:"public_scene_brief"`
	PracticeGoal       string             `json:"practice_goal"`
}

func (manifest CatalogManifest) Valid() bool {
	if manifest.SchemaVersion != catalogManifestSchemaVersion ||
		len(manifest.Experiences) != len(catalogPracticeExperiences()) ||
		len(manifest.Scenes) == 0 {
		return false
	}
	scenes := make(map[string]CatalogSceneManifest, len(manifest.Scenes))
	previousSceneID := ""
	for _, item := range manifest.Scenes {
		if !validResourceID(item.SceneID) || item.SceneVersion < 1 ||
			!validCatalogManifestExperience(item.PracticeExperience) ||
			!nonBlank(item.Name) || !validDiscoveryPhrases(item.Aliases) ||
			!nonBlank(item.PublicSceneBrief) || !nonBlank(item.PracticeGoal) ||
			(previousSceneID != "" && previousSceneID >= item.SceneID) {
			return false
		}
		if _, duplicate := scenes[item.SceneID]; duplicate {
			return false
		}
		scenes[item.SceneID] = item
		previousSceneID = item.SceneID
	}
	previousExperience := PracticeExperience("")
	for _, item := range manifest.Experiences {
		if !validCatalogManifestExperience(item.Experience) ||
			!validDiscoveryPhrases(item.Aliases) ||
			!validResourceID(item.DefaultSceneID) ||
			!validResourceID(item.DefaultPracticeOptionID) ||
			(previousExperience != "" && previousExperience >= item.Experience) {
			return false
		}
		definition, found := scenes[item.DefaultSceneID]
		if !found || definition.PracticeExperience != item.Experience {
			return false
		}
		previousExperience = item.Experience
	}
	return validCatalogManifestAliases(manifest)
}

func validCatalogManifestAliases(manifest CatalogManifest) bool {
	owners := make(map[string]string)
	for _, item := range manifest.Scenes {
		owner := "scene:" + item.SceneID
		for _, alias := range append([]string{item.Name}, item.Aliases...) {
			normalized := normalizeDiscoveryText(alias)
			if current, duplicate := owners[normalized]; duplicate && current != owner {
				return false
			}
			owners[normalized] = owner
		}
	}
	for _, item := range manifest.Experiences {
		owner := "experience:" + string(item.Experience)
		for _, alias := range item.Aliases {
			normalized := normalizeDiscoveryText(alias)
			if current, duplicate := owners[normalized]; duplicate && current != owner {
				return false
			}
			owners[normalized] = owner
		}
	}
	return true
}

// ResolvePreviewCatalogSelection accepts only an active Catalog scene ID. It
// never interprets natural language and never falls back to Custom.
func (catalog *Catalog) ResolvePreviewCatalogSelection(
	ctx context.Context,
	sceneID string,
) (PreviewCatalogSelection, error) {
	if !validResourceID(sceneID) {
		return PreviewCatalogSelection{}, ErrCatalogSelectionInvalid
	}
	definition, err := catalog.GetScene(ctx, sceneID)
	if err != nil {
		return PreviewCatalogSelection{}, err
	}
	selection, valid := previewCatalogSelection(definition)
	if !valid {
		return PreviewCatalogSelection{}, ErrCatalogDefinitionInvalid
	}
	return selection, nil
}

// PreviewCatalogManifest returns the trusted, concise metadata supplied to the
// model before it chooses a stable scene ID.
func (catalog *Catalog) PreviewCatalogManifest(
	ctx context.Context,
) (CatalogManifest, error) {
	definitions, err := catalog.ListActiveScenes(ctx)
	if err != nil {
		return CatalogManifest{}, err
	}
	if len(definitions) == 0 || len(catalog.sceneDiscovery) != len(definitions) {
		return CatalogManifest{}, ErrCatalogDefinitionInvalid
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].ID < definitions[right].ID
	})
	manifest := CatalogManifest{
		SchemaVersion: catalogManifestSchemaVersion,
		Experiences: make(
			[]CatalogExperienceManifest,
			0,
			len(catalog.experienceDiscovery),
		),
		Scenes: make([]CatalogSceneManifest, 0, len(definitions)),
	}
	for _, definition := range definitions {
		profile, found := catalog.sceneDiscovery[definition.ID]
		if !found || profile.SceneID != definition.ID ||
			!validDiscoveryPhrases(profile.Aliases) {
			return CatalogManifest{}, ErrCatalogDefinitionInvalid
		}
		if _, valid := previewCatalogSelection(definition); !valid {
			return CatalogManifest{}, ErrCatalogDefinitionInvalid
		}
		aliases := append([]string(nil), profile.Aliases...)
		sort.Strings(aliases)
		manifest.Scenes = append(manifest.Scenes, CatalogSceneManifest{
			SceneID:            definition.ID,
			SceneVersion:       definition.Version,
			PracticeExperience: definition.Experience,
			Name:               definition.Name,
			Aliases:            aliases,
			PublicSceneBrief:   definition.Prompt.PublicSceneBrief,
			PracticeGoal:       definition.Prompt.PracticeGoal,
		})
	}
	for _, experience := range catalogPracticeExperiences() {
		profile, found := catalog.experienceDiscovery[experience]
		if !found || profile.Experience != experience {
			return CatalogManifest{}, ErrCatalogDefinitionInvalid
		}
		definition, found := catalog.scene(profile.DefaultSceneID)
		if !found || definition.Status != SceneStatusActive ||
			definition.Experience != experience {
			return CatalogManifest{}, ErrCatalogDefinitionInvalid
		}
		option, found := findPracticeOption(
			definition.PracticeOptions,
			profile.DefaultPracticeOptionID,
		)
		if !found || (option.Mode != PracticeModeFullSimulation &&
			option.Mode != PracticeModeFullMock) {
			return CatalogManifest{}, ErrCatalogDefinitionInvalid
		}
		aliases := append([]string(nil), profile.Aliases...)
		sort.Strings(aliases)
		manifest.Experiences = append(
			manifest.Experiences,
			CatalogExperienceManifest{
				Experience:              experience,
				Aliases:                 aliases,
				DefaultSceneID:          profile.DefaultSceneID,
				DefaultPracticeOptionID: profile.DefaultPracticeOptionID,
			},
		)
	}
	sort.Slice(manifest.Experiences, func(left, right int) bool {
		return manifest.Experiences[left].Experience <
			manifest.Experiences[right].Experience
	})
	if !manifest.Valid() {
		return CatalogManifest{}, ErrCatalogDefinitionInvalid
	}
	return manifest, nil
}

func previewCatalogSelection(
	definition SceneDefinition,
) (PreviewCatalogSelection, bool) {
	if definition.Status != SceneStatusActive || len(definition.Roles) == 0 {
		return PreviewCatalogSelection{}, false
	}
	option, found := defaultPreviewPracticeOption(definition.PracticeOptions)
	if !found {
		return PreviewCatalogSelection{}, false
	}
	selection := PreviewCatalogSelection{
		Scene:          cloneScene(definition),
		DefaultRoleIDs: []string{definition.Roles[0].ID},
		DefaultOption:  option,
	}
	return selection, selection.Valid()
}

func defaultPreviewPracticeOption(
	options []PracticeOption,
) (PracticeOption, bool) {
	for _, option := range options {
		if option.Mode == PracticeModeFullSimulation ||
			option.Mode == PracticeModeFullMock {
			return option, true
		}
	}
	return PracticeOption{}, false
}

func catalogPracticeExperiences() []PracticeExperience {
	return []PracticeExperience{
		PracticeExperienceInterview,
		PracticeExperienceIELTSSpeaking,
		PracticeExperienceWorkplace,
		PracticeExperienceLifeAndTravel,
	}
}

func validCatalogManifestExperience(experience PracticeExperience) bool {
	switch experience {
	case PracticeExperienceInterview,
		PracticeExperienceIELTSSpeaking,
		PracticeExperienceWorkplace,
		PracticeExperienceLifeAndTravel:
		return true
	default:
		return false
	}
}
