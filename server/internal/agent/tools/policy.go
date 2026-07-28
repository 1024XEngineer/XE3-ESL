package tools

type Policy struct {
	AllowedNames []string
	AllowWrites  bool
}

func (policy Policy) Select(registry *Registry) ([]Definition, error) {
	if registry == nil {
		return nil, nil
	}
	if len(policy.AllowedNames) == 0 {
		return selectFromDefinitions(registry.Definitions(), policy.AllowWrites), nil
	}
	definitions := make([]Definition, 0, len(policy.AllowedNames))
	for _, name := range policy.AllowedNames {
		tool, ok := registry.Get(name)
		if !ok {
			return nil, ErrUnknownTool
		}
		definition := tool.Definition()
		if !policy.AllowWrites && !definition.ReadOnly {
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func selectFromDefinitions(definitions []Definition, allowWrites bool) []Definition {
	selected := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if allowWrites || definition.ReadOnly {
			selected = append(selected, definition)
		}
	}
	return selected
}
