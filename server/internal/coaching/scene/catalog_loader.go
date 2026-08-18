package scene

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const builtinCatalogSchemaVersion = 1

//go:embed catalog.v1.json
var builtinCatalogData []byte

type catalogDocument struct {
	SchemaVersion int               `json:"schema_version"`
	Scenes        []SceneDefinition `json:"scenes"`
}

// NewBuiltinCatalog loads the versioned Scene content shipped with the server.
func NewBuiltinCatalog(
	policyValidator EvaluationPolicyReferenceValidator,
) (*Catalog, error) {
	catalog, err := LoadCatalog(bytes.NewReader(builtinCatalogData), policyValidator)
	if err != nil {
		return nil, err
	}
	if err := loadBuiltinDiscovery(catalog); err != nil {
		return nil, fmt.Errorf("load built-in Scene discovery: %w", err)
	}
	return catalog, nil
}

// LoadCatalog decodes and validates one complete Scene catalog document.
func LoadCatalog(
	reader io.Reader,
	policyValidator EvaluationPolicyReferenceValidator,
) (*Catalog, error) {
	if reader == nil {
		return nil, invalidDefinition("catalog document is required")
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document catalogDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, invalidDefinition("decode catalog: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, invalidDefinition("catalog contains trailing JSON content")
	}
	if document.SchemaVersion != builtinCatalogSchemaVersion {
		return nil, invalidDefinition(
			"unsupported catalog schema version %d",
			document.SchemaVersion,
		)
	}
	for sceneIndex := range document.Scenes {
		definition := &document.Scenes[sceneIndex]
		definition.DisplayOrder = sceneIndex + 1
		for roleIndex := range definition.Roles {
			definition.Roles[roleIndex].DisplayOrder = roleIndex + 1
		}
		for optionIndex := range definition.PracticeOptions {
			definition.PracticeOptions[optionIndex].DisplayOrder = optionIndex + 1
		}
	}
	catalog, err := NewCatalog(document.Scenes, policyValidator)
	if err != nil {
		return nil, fmt.Errorf("load built-in Scene catalog: %w", err)
	}
	return catalog, nil
}
