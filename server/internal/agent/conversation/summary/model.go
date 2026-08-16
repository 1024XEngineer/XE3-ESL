package summary

import (
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

const (
	MaxItemsPerSection = 6
	MaxItems           = 24
	MaxItemRunes       = 240
	MaxItemBytes       = 960
	MaxContentRunes    = 2400
	MaxSourceMessages  = 200
)

type Content struct {
	CurrentIntents []string `json:"current_intents"`
	Background     []string `json:"background"`
	Progress       []string `json:"progress"`
	Decisions      []string `json:"decisions"`
	OpenQuestions  []string `json:"open_questions"`
	NextSteps      []string `json:"next_steps"`
}

func (content Content) Valid() bool {
	sections := [][]string{
		content.CurrentIntents,
		content.Background,
		content.Progress,
		content.Decisions,
		content.OpenQuestions,
		content.NextSteps,
	}
	items := 0
	runes := 0
	for _, section := range sections {
		if section == nil || len(section) > MaxItemsPerSection {
			return false
		}
		items += len(section)
		for _, item := range section {
			if !validItem(item) {
				return false
			}
			runes += utf8.RuneCountInString(item)
		}
	}
	return items > 0 && items <= MaxItems && runes <= MaxContentRunes
}

func validItem(value string) bool {
	return utf8.ValidString(value) &&
		len(value) <= MaxItemBytes &&
		utf8.RuneCountInString(value) <= MaxItemRunes &&
		!strings.ContainsRune(value, '\x00') &&
		value == strings.TrimSpace(value) &&
		value != ""
}

// State is the one current compression of a Thread. A newer summary replaces
// it; there is deliberately no checkpoint history or provider lineage.
type State struct {
	OwnerID         string
	ThreadID        string
	ThroughSequence int64
	Content         Content
}

func (state State) Valid() bool {
	return conversation.ValidUUID(state.OwnerID) &&
		conversation.ValidUUID(state.ThreadID) &&
		state.ThroughSequence >= 1 && state.Content.Valid()
}
