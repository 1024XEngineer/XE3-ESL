package ielts

import "context"

type QuestionReference struct {
	BankID           string       `json:"bank_id"`
	Part             PracticeMode `json:"part"`
	SourceID         string       `json:"source_id"`
	QuestionPosition int          `json:"question_position"`
}

type ResolvedQuestion struct {
	Reference QuestionReference `json:"reference"`
	Prompt    string            `json:"prompt"`
}

type QuestionResolver interface {
	ResolveQuestion(context.Context, QuestionReference) (ResolvedQuestion, error)
}

func validQuestionReference(reference QuestionReference) bool {
	if !catalogResourceIDPattern.MatchString(reference.BankID) ||
		!catalogResourceIDPattern.MatchString(reference.SourceID) ||
		reference.QuestionPosition < 1 {
		return false
	}
	switch reference.Part {
	case PracticeModePart1, PracticeModePart3:
		return true
	case PracticeModePart2:
		return reference.QuestionPosition == 1
	default:
		return false
	}
}
