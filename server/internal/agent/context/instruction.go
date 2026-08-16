package context

import (
	"strings"
	"unicode/utf8"
)

const maxInstructionCharacters = 16 * 1024

// Instruction is the product-owned behavior injected into the generic Agent
// context assembler.
type Instruction struct {
	Version string
	Content string
}

func (instruction Instruction) Valid() bool {
	return instruction.Version == strings.TrimSpace(instruction.Version) &&
		instruction.Version != "" &&
		instruction.Content == strings.TrimSpace(instruction.Content) &&
		instruction.Content != "" &&
		utf8.RuneCountInString(instruction.Content) <= maxInstructionCharacters
}

// Render lets a static Instruction satisfy InstructionProvider directly.
func (instruction Instruction) Render() Instruction {
	return instruction
}

type InstructionProvider interface {
	Render() Instruction
}
