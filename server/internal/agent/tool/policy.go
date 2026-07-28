package tool

type Policy struct {
	AllowedNames   []string
	AllowWrites    bool
	ConfirmedNames []string
}

// Allows 判断某个工具定义在当前策略下是否允许执行。
func (policy Policy) Allows(definition Definition) bool {
	if !policy.nameAllowed(definition.Name) {
		return false
	}
	if !policy.AllowWrites && !definition.ReadOnly {
		return false
	}
	if definition.Risk == RiskRequiresConfirm &&
		!policy.nameConfirmed(definition.Name) {
		return false
	}
	return true
}

// Select 返回当前 Agent Run 允许暴露的工具定义。
func (policy Policy) Select(registry *Registry) ([]Definition, error) {
	if registry == nil {
		return nil, nil
	}
	if len(policy.AllowedNames) == 0 {
		return selectFromDefinitions(registry.Definitions(), policy), nil
	}
	definitions := make([]Definition, 0, len(policy.AllowedNames))
	for _, name := range policy.AllowedNames {
		tool, ok := registry.Get(name)
		if !ok {
			return nil, ErrUnknownTool
		}
		definition := tool.Definition()
		if !policy.Allows(definition) {
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

// selectFromDefinitions 按写权限过滤工具定义，并保持原有顺序。
func selectFromDefinitions(definitions []Definition, policy Policy) []Definition {
	selected := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if policy.Allows(definition) {
			selected = append(selected, definition)
		}
	}
	return selected
}

func (policy Policy) nameAllowed(name string) bool {
	if len(policy.AllowedNames) == 0 {
		return true
	}
	for _, allowed := range policy.AllowedNames {
		if allowed == name {
			return true
		}
	}
	return false
}

func (policy Policy) nameConfirmed(name string) bool {
	for _, confirmed := range policy.ConfirmedNames {
		if confirmed == name {
			return true
		}
	}
	return false
}
