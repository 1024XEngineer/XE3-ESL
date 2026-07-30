package preparation

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuiltinCatalogExposesAllMVPScenariosAcrossFourFamilies(t *testing.T) {
	catalog := mustBuiltinCatalog(t)

	scenarios := catalog.ListActiveScenarios()
	if len(scenarios) != 31 {
		t.Fatalf("ListActiveScenarios length=%d, want 31", len(scenarios))
	}
	familyCounts := map[ScenarioFamily]int{}
	byID := make(map[string]ScenarioDefinition, len(scenarios))
	for _, scenario := range scenarios {
		familyCounts[scenario.Type]++
		byID[scenario.ID] = scenario
	}
	wantFamilyCounts := map[ScenarioFamily]int{
		ScenarioFamilyInterview: 7,
		ScenarioFamilyExam:      5,
		ScenarioFamilyWorkplace: 8,
		ScenarioFamilyDaily:     11,
	}
	if !reflect.DeepEqual(familyCounts, wantFamilyCounts) {
		t.Fatalf("family counts=%v, want %v", familyCounts, wantFamilyCounts)
	}
	for id, want := range map[string]struct {
		family  ScenarioFamily
		model   ScenarioModel
		version int
	}{
		ProgrammerInterviewScenarioID: {
			ScenarioFamilyInterview,
			ScenarioModelProjectExperienceDeepDive,
			1,
		},
		IELTSSpeakingPart2ScenarioID: {
			ScenarioFamilyExam,
			ScenarioModelIELTSSpeakingPart2,
			1,
		},
		IELTSSpeakingPart1ScenarioID: {
			ScenarioFamilyExam,
			ScenarioModelIELTSSpeakingPart1,
			1,
		},
		IELTSSpeakingPart3ScenarioID: {
			ScenarioFamilyExam,
			ScenarioModelIELTSSpeakingPart3,
			1,
		},
		IELTSSpeakingFullMockScenarioID: {
			ScenarioFamilyExam,
			ScenarioModelIELTSSpeakingFullMock,
			2,
		},
		WorkplaceProgressRiskScenarioID: {
			ScenarioFamilyWorkplace,
			ScenarioModelProgressAndRiskUpdate,
			1,
		},
		DailyHotelCheckinScenarioID: {
			ScenarioFamilyDaily,
			ScenarioModelHotelCheckinAndIssueHandling,
			1,
		},
	} {
		scenario, ok := byID[id]
		if !ok || scenario.Type != want.family ||
			scenario.Model != want.model ||
			scenario.Version != want.version ||
			scenario.Status != ScenarioStatusActive {
			t.Fatalf("scenario %q=%#v, want %#v", id, scenario, want)
		}
	}

	detail, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("GetScenarioDetail: %v", err)
	}
	if detail.ScenarioConfig.ID != BackendEngineerConfigID ||
		detail.ScenarioConfig.Type != ScenarioFamilyInterview ||
		detail.ScenarioConfig.Model !=
			ScenarioModelProjectExperienceDeepDive ||
		detail.ScenarioConfig.Version != 1 {
		t.Fatalf("unexpected scenario config: %#v", detail.ScenarioConfig)
	}
	wantOptions := []string{
		FullSimulationOptionID,
		TechnicalFocusOptionID,
		HRFocusOptionID,
		ProjectFocusOptionID,
		ExecutiveFocusOptionID,
	}
	if got := optionIDs(detail.PracticeOptions); !reflect.DeepEqual(got, wantOptions) {
		t.Fatalf("practice option order=%v, want %v", got, wantOptions)
	}
	if detail.PracticeOptions[0].Type != PracticeOptionFullSimulation ||
		detail.PracticeOptions[0].RoleDefinitionID != "" ||
		detail.PracticeOptions[0].Version != 1 {
		t.Fatalf("invalid full simulation option: %#v", detail.PracticeOptions[0])
	}

	roles, err := catalog.ListRoles(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	wantRoles := []string{
		TechnicalInterviewerRoleID,
		HRInterviewerRoleID,
		ProjectManagerRoleID,
		ExecutiveInterviewerRoleID,
	}
	if got := roleIDs(roles); !reflect.DeepEqual(got, wantRoles) {
		t.Fatalf("role order=%v, want %v", got, wantRoles)
	}
	for index, role := range roles {
		option := detail.PracticeOptions[index+1]
		if option.Type != PracticeOptionFocus ||
			option.RoleDefinitionID != role.ID {
			t.Fatalf("role %q has invalid FOCUS option %#v", role.ID, option)
		}
	}

	for _, scenario := range scenarios {
		detail, err := catalog.GetScenarioDetail(scenario.ID)
		if err != nil {
			t.Fatalf("GetScenarioDetail(%q): %v", scenario.ID, err)
		}
		config := detail.ScenarioConfig
		wantBlueprints := 4
		switch scenario.Model {
		case ScenarioModelIELTSSpeakingFullMock:
			wantBlueprints = 14
		case ScenarioModelIELTSSpeakingPart1:
			wantBlueprints = 8
		case ScenarioModelIELTSSpeakingPart3:
			wantBlueprints = 5
		}
		if config.Type != scenario.Type ||
			config.Model != scenario.Model ||
			config.PromptModel.PublicSceneBrief == "" ||
			config.PromptModel.PracticeGoal == "" ||
			config.PromptModel.UserRole == "" ||
			config.PromptModel.AIRole == "" ||
			config.PromptModel.PersonaSummary == "" ||
			len(config.PromptModel.FocusAreas) == 0 ||
			len(config.PromptModel.TurnBlueprints) != wantBlueprints ||
			config.PromptModel.SuggestedDurationSeconds < 1 {
			t.Fatalf("scenario %q has incomplete prompt model: %#v", scenario.ID, config)
		}
		if scenario.Model == ScenarioModelProjectExperienceDeepDive {
			if config.JobTitle == "" || config.JobDescription == "" {
				t.Fatalf("JD compatibility fields missing: %#v", config)
			}
		} else if config.JobTitle != "" || config.JobDescription != "" {
			t.Fatalf("basic scenario faked job fields: %#v", config)
		}

		roles, err := catalog.ListRoles(scenario.ID)
		if err != nil {
			t.Fatalf("ListRoles(%q): %v", scenario.ID, err)
		}
		if len(roles) == 0 {
			t.Fatalf("scenario %q has no AI role", scenario.ID)
		}
		var fullSimulation bool
		var focus bool
		for _, option := range detail.PracticeOptions {
			fullSimulation = fullSimulation ||
				option.Type == PracticeOptionFullSimulation
			focus = focus || option.Type == PracticeOptionFocus
		}
		if !fullSimulation || !focus {
			t.Fatalf("scenario %q options=%#v", scenario.ID, detail.PracticeOptions)
		}
	}
}

func TestBuiltinCatalogTreatsRolesAsIndependentPerspectives(t *testing.T) {
	catalog := mustBuiltinCatalog(t)
	roles, err := catalog.ListRoles(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	detail, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("GetScenarioDetail: %v", err)
	}

	expectedRoles := []struct {
		id          string
		roleType    string
		displayName string
		focusOption string
	}{
		{
			TechnicalInterviewerRoleID,
			"TECHNICAL_INTERVIEWER",
			"技术面试官",
			TechnicalFocusOptionID,
		},
		{
			HRInterviewerRoleID,
			"HR_INTERVIEWER",
			"招聘专员",
			HRFocusOptionID,
		},
		{
			ProjectManagerRoleID,
			"PROJECT_MANAGER",
			"项目经理",
			ProjectFocusOptionID,
		},
		{
			ExecutiveInterviewerRoleID,
			"EXECUTIVE_INTERVIEWER",
			"用人经理",
			ExecutiveFocusOptionID,
		},
	}
	if len(roles) != len(expectedRoles) {
		t.Fatalf("role count=%d, want %d", len(roles), len(expectedRoles))
	}

	optionsByID := make(map[string]PracticeOptionDefinition, len(detail.PracticeOptions))
	focusCountByRole := make(map[string]int, len(roles))
	for _, option := range detail.PracticeOptions {
		optionsByID[option.ID] = option
		if option.Type == PracticeOptionFocus {
			focusCountByRole[option.RoleDefinitionID]++
		}
	}

	for index, expected := range expectedRoles {
		role := roles[index]
		if role.ID != expected.id ||
			role.Type != expected.roleType ||
			role.DisplayName != expected.displayName ||
			role.Version != 1 {
			t.Fatalf("role[%d]=%#v, want %#v", index, role, expected)
		}

		fullSnapshot, err := catalog.GetCatalogSnapshot(
			ProgrammerInterviewScenarioID,
			1,
			[]string{role.ID},
			FullSimulationOptionID,
			1,
		)
		if err != nil {
			t.Fatalf("FULL_SIMULATION for role %q: %v", role.ID, err)
		}
		if len(fullSnapshot.SelectedRoles) != 1 ||
			fullSnapshot.SelectedRoles[0].ID != role.ID ||
			fullSnapshot.PracticeOption.Type != PracticeOptionFullSimulation ||
			fullSnapshot.PracticeOption.RoleDefinitionID != "" {
			t.Fatalf("invalid FULL_SIMULATION snapshot for %q: %#v", role.ID, fullSnapshot)
		}

		focusOption, ok := optionsByID[expected.focusOption]
		if !ok ||
			focusOption.Type != PracticeOptionFocus ||
			focusOption.RoleDefinitionID != role.ID ||
			focusOption.Version != 1 ||
			focusCountByRole[role.ID] != 1 {
			t.Fatalf("role %q has invalid FOCUS mapping: %#v", role.ID, focusOption)
		}
		focusSnapshot, err := catalog.GetCatalogSnapshot(
			ProgrammerInterviewScenarioID,
			1,
			[]string{role.ID},
			expected.focusOption,
			1,
		)
		if err != nil {
			t.Fatalf("FOCUS for role %q: %v", role.ID, err)
		}
		if focusSnapshot.PracticeOption.RoleDefinitionID != role.ID {
			t.Fatalf("FOCUS snapshot mismatch for %q: %#v", role.ID, focusSnapshot)
		}
	}
}

func TestCatalogSnapshotUsesExactVersionsAndReturnsCopies(t *testing.T) {
	catalog := mustBuiltinCatalog(t)

	snapshot, err := catalog.GetCatalogSnapshot(
		ProgrammerInterviewScenarioID,
		1,
		[]string{TechnicalInterviewerRoleID},
		TechnicalFocusOptionID,
		1,
	)
	if err != nil {
		t.Fatalf("GetCatalogSnapshot: %v", err)
	}
	if snapshot.SelectedRoles[0].ID != TechnicalInterviewerRoleID ||
		snapshot.PracticeOption.RoleDefinitionID != TechnicalInterviewerRoleID {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}

	snapshot.ScenarioConfig.PromptModel.FocusAreas[0] = "mutated"
	snapshot.SelectedRoles[0].FocusAreas[0] = "mutated"
	snapshot.SelectedRoles[0].DisplayName = "mutated"
	snapshot.PracticeOption.DisplayName = "mutated"

	reloaded, err := catalog.GetCatalogSnapshot(
		ProgrammerInterviewScenarioID,
		1,
		[]string{TechnicalInterviewerRoleID},
		TechnicalFocusOptionID,
		1,
	)
	if err != nil {
		t.Fatalf("reload snapshot: %v", err)
	}
	if reloaded.ScenarioConfig.PromptModel.FocusAreas[0] == "mutated" ||
		reloaded.SelectedRoles[0].FocusAreas[0] == "mutated" ||
		reloaded.SelectedRoles[0].DisplayName == "mutated" ||
		reloaded.PracticeOption.DisplayName == "mutated" {
		t.Fatalf("snapshot mutation changed catalog: %#v", reloaded)
	}

	tests := []struct {
		name      string
		scenario  string
		version   int
		roles     []string
		option    string
		optionVer int
		want      error
	}{
		{"unknown scenario", "unknown", 1, []string{TechnicalInterviewerRoleID}, TechnicalFocusOptionID, 1, ErrScenarioDefinitionNotFound},
		{"unknown scenario version", ProgrammerInterviewScenarioID, 2, []string{TechnicalInterviewerRoleID}, TechnicalFocusOptionID, 1, ErrScenarioDefinitionNotFound},
		{"unknown role", ProgrammerInterviewScenarioID, 1, []string{"unknown"}, TechnicalFocusOptionID, 1, ErrRoleDefinitionNotFound},
		{"empty roles", ProgrammerInterviewScenarioID, 1, nil, TechnicalFocusOptionID, 1, ErrCatalogSelectionInvalid},
		{"duplicate roles", ProgrammerInterviewScenarioID, 1, []string{TechnicalInterviewerRoleID, TechnicalInterviewerRoleID}, FullSimulationOptionID, 1, ErrCatalogSelectionInvalid},
		{"unknown option", ProgrammerInterviewScenarioID, 1, []string{TechnicalInterviewerRoleID}, "unknown", 1, ErrPracticeOptionNotFound},
		{"unknown option version", ProgrammerInterviewScenarioID, 1, []string{TechnicalInterviewerRoleID}, TechnicalFocusOptionID, 2, ErrPracticeOptionNotFound},
		{"focus role mismatch", ProgrammerInterviewScenarioID, 1, []string{HRInterviewerRoleID}, TechnicalFocusOptionID, 1, ErrCatalogSelectionInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := catalog.GetCatalogSnapshot(
				test.scenario,
				test.version,
				test.roles,
				test.option,
				test.optionVer,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestCatalogReadMethodsDoNotExposeMutableSlices(t *testing.T) {
	catalog := mustBuiltinCatalog(t)

	firstDetail, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("GetScenarioDetail: %v", err)
	}
	firstRoles, err := catalog.ListRoles(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	firstDetail.ScenarioConfig.PromptModel.FocusAreas[0] = "mutated"
	firstDetail.PracticeOptions[0].DisplayName = "mutated"
	firstRoles[0].FocusAreas[0] = "mutated"

	secondDetail, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("second GetScenarioDetail: %v", err)
	}
	secondRoles, err := catalog.ListRoles(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("second ListRoles: %v", err)
	}
	if secondDetail.ScenarioConfig.PromptModel.FocusAreas[0] == "mutated" ||
		secondDetail.PracticeOptions[0].DisplayName == "mutated" ||
		secondRoles[0].FocusAreas[0] == "mutated" {
		t.Fatal("catalog returned shared mutable data")
	}
}

func TestInactiveScenarioIsNotPublic(t *testing.T) {
	definition := programmerInterviewCatalogDefinition()
	definition.definition.Status = ScenarioStatusInactive
	catalog, err := newCatalog([]catalogScenario{definition})
	if err != nil {
		t.Fatalf("newCatalog: %v", err)
	}

	if scenarios := catalog.ListActiveScenarios(); len(scenarios) != 0 {
		t.Fatalf("inactive scenario was listed: %#v", scenarios)
	}
	if _, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID); !errors.Is(err, ErrScenarioDefinitionNotFound) {
		t.Fatalf("GetScenarioDetail error=%v", err)
	}
	if _, err := catalog.ListRoles(ProgrammerInterviewScenarioID); !errors.Is(err, ErrScenarioDefinitionNotFound) {
		t.Fatalf("ListRoles error=%v", err)
	}
}

func TestInactiveScenarioRemainsAvailableForPinnedSnapshot(t *testing.T) {
	definition := programmerInterviewCatalogDefinition()
	definition.definition.Status = ScenarioStatusInactive
	catalog, err := newCatalog([]catalogScenario{definition})
	if err != nil {
		t.Fatalf("newCatalog: %v", err)
	}

	snapshot, err := catalog.GetCatalogSnapshot(
		ProgrammerInterviewScenarioID,
		1,
		[]string{TechnicalInterviewerRoleID},
		TechnicalFocusOptionID,
		1,
	)
	if err != nil {
		t.Fatalf("GetCatalogSnapshot: %v", err)
	}
	if snapshot.ScenarioDefinition.Status != ScenarioStatusInactive ||
		snapshot.SelectedRoles[0].ID != TechnicalInterviewerRoleID ||
		snapshot.PracticeOption.ID != TechnicalFocusOptionID {
		t.Fatalf("unexpected inactive snapshot: %#v", snapshot)
	}
}

func TestCatalogConstructionRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*catalogScenario)
	}{
		{"zero scenario version", func(value *catalogScenario) {
			value.definition.Version = 0
		}},
		{"invalid family model pair", func(value *catalogScenario) {
			value.definition.Model = ScenarioModelIELTSSpeakingPart2
			value.config.Model = ScenarioModelIELTSSpeakingPart2
		}},
		{"config belongs to another scenario", func(value *catalogScenario) {
			value.config.ScenarioDefinitionID = "scn_other"
		}},
		{"config model differs from definition", func(value *catalogScenario) {
			value.config.Model = ScenarioModelIELTSSpeakingPart2
		}},
		{"missing public scene brief", func(value *catalogScenario) {
			value.config.PromptModel.PublicSceneBrief = ""
		}},
		{"missing turn blueprints", func(value *catalogScenario) {
			value.config.PromptModel.TurnBlueprints = nil
		}},
		{"duplicate config focus area", func(value *catalogScenario) {
			value.config.PromptModel.FocusAreas = []string{"one", "one"}
		}},
		{"invalid role type", func(value *catalogScenario) {
			value.roles[0].Type = "hr-interviewer"
		}},
		{"duplicate role ID", func(value *catalogScenario) {
			value.roles[1].ID = value.roles[0].ID
		}},
		{"FULL_SIMULATION references role", func(value *catalogScenario) {
			value.practiceOptions[0].RoleDefinitionID = value.roles[0].ID
		}},
		{"FOCUS references unknown role", func(value *catalogScenario) {
			value.practiceOptions[1].RoleDefinitionID = "unknown"
		}},
		{"role lacks FOCUS option", func(value *catalogScenario) {
			value.practiceOptions = append(
				[]PracticeOptionDefinition(nil),
				value.practiceOptions[:len(value.practiceOptions)-1]...,
			)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := programmerInterviewCatalogDefinition()
			test.mutate(&definition)
			if _, err := newCatalog([]catalogScenario{definition}); !errors.Is(err, ErrCatalogDefinitionInvalid) {
				t.Fatalf("newCatalog error=%v, want ErrCatalogDefinitionInvalid", err)
			}
		})
	}

	duplicate := programmerInterviewCatalogDefinition()
	if _, err := newCatalog([]catalogScenario{duplicate, duplicate}); !errors.Is(err, ErrCatalogDefinitionInvalid) {
		t.Fatalf("duplicate scenario error=%v", err)
	}

	other := alternateCatalogDefinition()
	other.config.ID = duplicate.config.ID
	if _, err := newCatalog([]catalogScenario{duplicate, other}); !errors.Is(err, ErrCatalogDefinitionInvalid) {
		t.Fatalf("duplicate config error=%v", err)
	}
}

func TestCatalogSortsScenariosRolesAndOptions(t *testing.T) {
	first := programmerInterviewCatalogDefinition()
	first.definition.DisplayOrder = 20
	reverseRoles(first.roles)
	reversePracticeOptions(first.practiceOptions)

	second := alternateCatalogDefinition()
	second.definition.DisplayOrder = 10
	catalog, err := newCatalog([]catalogScenario{first, second})
	if err != nil {
		t.Fatalf("newCatalog: %v", err)
	}

	scenarios := catalog.ListActiveScenarios()
	if got := []string{scenarios[0].ID, scenarios[1].ID}; !reflect.DeepEqual(
		got,
		[]string{second.definition.ID, first.definition.ID},
	) {
		t.Fatalf("scenario order=%v", got)
	}
	roles, err := catalog.ListRoles(first.definition.ID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if got := roleIDs(roles); !reflect.DeepEqual(got, []string{
		TechnicalInterviewerRoleID,
		HRInterviewerRoleID,
		ProjectManagerRoleID,
		ExecutiveInterviewerRoleID,
	}) {
		t.Fatalf("role order=%v", got)
	}
	detail, err := catalog.GetScenarioDetail(first.definition.ID)
	if err != nil {
		t.Fatalf("GetScenarioDetail: %v", err)
	}
	if got := optionIDs(detail.PracticeOptions); !reflect.DeepEqual(got, []string{
		FullSimulationOptionID,
		TechnicalFocusOptionID,
		HRFocusOptionID,
		ProjectFocusOptionID,
		ExecutiveFocusOptionID,
	}) {
		t.Fatalf("option order=%v", got)
	}
}

func TestCatalogCopiesConstructorInput(t *testing.T) {
	definition := programmerInterviewCatalogDefinition()
	catalog, err := newCatalog([]catalogScenario{definition})
	if err != nil {
		t.Fatalf("newCatalog: %v", err)
	}
	definition.config.PromptModel.FocusAreas[0] = "mutated"
	definition.roles[0].FocusAreas[0] = "mutated"
	definition.practiceOptions[0].DisplayName = "mutated"

	detail, err := catalog.GetScenarioDetail(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("GetScenarioDetail: %v", err)
	}
	roles, err := catalog.ListRoles(ProgrammerInterviewScenarioID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if detail.ScenarioConfig.PromptModel.FocusAreas[0] == "mutated" ||
		detail.PracticeOptions[0].DisplayName == "mutated" ||
		roles[0].FocusAreas[0] == "mutated" {
		t.Fatal("constructor retained mutable input")
	}
}

func mustBuiltinCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("NewBuiltinCatalog: %v", err)
	}
	return catalog
}

func roleIDs(roles []RoleDefinition) []string {
	result := make([]string, len(roles))
	for index, role := range roles {
		result[index] = role.ID
	}
	return result
}

func optionIDs(options []PracticeOptionDefinition) []string {
	result := make([]string, len(options))
	for index, option := range options {
		result[index] = option.ID
	}
	return result
}

func alternateCatalogDefinition() catalogScenario {
	result := programmerInterviewCatalogDefinition()
	result.definition.ID = "scn_alternate_interview"
	result.config.ID = "scfg_alternate_engineer"
	result.config.ScenarioDefinitionID = result.definition.ID

	roleIDs := make(map[string]string, len(result.roles))
	for index := range result.roles {
		previousID := result.roles[index].ID
		result.roles[index].ID = previousID + "_alternate"
		result.roles[index].ScenarioDefinitionID = result.definition.ID
		roleIDs[previousID] = result.roles[index].ID
	}
	for index := range result.practiceOptions {
		result.practiceOptions[index].ID += "_alternate"
		result.practiceOptions[index].ScenarioDefinitionID = result.definition.ID
		if result.practiceOptions[index].RoleDefinitionID != "" {
			result.practiceOptions[index].RoleDefinitionID =
				roleIDs[result.practiceOptions[index].RoleDefinitionID]
		}
	}
	return result
}

func reverseRoles(values []RoleDefinition) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reversePracticeOptions(values []PracticeOptionDefinition) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
