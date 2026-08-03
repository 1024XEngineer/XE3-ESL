package context

import "regexp"

const maxBudget = 1_000_000

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var modelPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

type AssembleCommand struct {
	RunID              string
	OwnerID            string
	ThreadID           string
	InputMessageID     string
	Provider           string
	Model              string
	MaxOutputTokens    int
	MaxInputCharacters int
}

func (command AssembleCommand) Valid() bool {
	return uuidPattern.MatchString(command.RunID) &&
		uuidPattern.MatchString(command.OwnerID) &&
		uuidPattern.MatchString(command.ThreadID) &&
		uuidPattern.MatchString(command.InputMessageID) &&
		providerPattern.MatchString(command.Provider) &&
		modelPattern.MatchString(command.Model) &&
		command.MaxOutputTokens > 0 &&
		command.MaxOutputTokens <= maxBudget &&
		command.MaxInputCharacters >= 5000 &&
		command.MaxInputCharacters <= maxBudget
}
