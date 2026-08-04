package run

type Configuration struct {
	Provider           string
	Model              string
	MaxOutputTokens    int
	MaxInputCharacters int
}

func (configuration Configuration) Valid() bool {
	return ValidProviderID(configuration.Provider) &&
		ValidModelID(configuration.Model) &&
		configuration.MaxOutputTokens > 0 &&
		configuration.MaxOutputTokens <= MaxBudget &&
		configuration.MaxInputCharacters >= 5000 &&
		configuration.MaxInputCharacters <= MaxBudget
}

func ValidConfiguration(configuration Configuration) bool {
	return configuration.Valid()
}
