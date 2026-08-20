package speechfeedback

import (
	"regexp"
	"strings"
	"unicode"
)

var asrPauseBoundaryPattern = regexp.MustCompile(`(?i)\.\s+(because|and)\s+`)

type speechFeedbackLanguage string

const (
	speechFeedbackLanguageEnglish   speechFeedbackLanguage = "ENGLISH"
	speechFeedbackLanguageMixed     speechFeedbackLanguage = "MIXED"
	speechFeedbackLanguageChinese   speechFeedbackLanguage = "CHINESE"
	speechFeedbackLanguageUncertain speechFeedbackLanguage = "UNCERTAIN"
)

func classifySpeechFeedbackLanguage(text string) speechFeedbackLanguage {
	var hasLatinLetter, hasHanLetter, hasOtherLetter bool
	for _, character := range strings.TrimSpace(text) {
		switch {
		case unicode.Is(unicode.Han, character):
			hasHanLetter = true
		case unicode.IsLetter(character):
			if !unicode.Is(unicode.Latin, character) {
				hasOtherLetter = true
				continue
			}
			hasLatinLetter = true
		}
	}
	if hasOtherLetter {
		return speechFeedbackLanguageUncertain
	}
	if hasLatinLetter && hasHanLetter {
		return speechFeedbackLanguageMixed
	}
	if hasLatinLetter {
		return speechFeedbackLanguageEnglish
	}
	if hasHanLetter {
		return speechFeedbackLanguageChinese
	}
	return speechFeedbackLanguageUncertain
}

func speechFeedbackHasAssessableEnglish(text string) bool {
	language := classifySpeechFeedbackLanguage(text)
	return language == speechFeedbackLanguageEnglish ||
		language == speechFeedbackLanguageMixed
}

// speechFeedbackEnglishReferenceText keeps only the English-addressable part
// of a confirmed mixed-language transcript. The original transcript remains
// the immutable text evidence; this projection is used only as the ISE paper.
func speechFeedbackEnglishReferenceText(text string) string {
	var output strings.Builder
	needsSpace := false
	for _, character := range strings.TrimSpace(text) {
		allowed := unicode.Is(unicode.Latin, character) &&
			unicode.IsLetter(character)
		allowed = allowed || unicode.IsNumber(character) ||
			(character <= unicode.MaxASCII &&
				(unicode.IsPunct(character) || unicode.IsSpace(character)))
		if !allowed {
			needsSpace = output.Len() > 0
			continue
		}
		if needsSpace && !unicode.IsSpace(character) {
			output.WriteByte(' ')
		}
		output.WriteRune(character)
		needsSpace = false
	}
	projected := strings.Join(strings.Fields(output.String()), " ")
	return strings.Trim(projected, " \t\r\n,;:!?-.")
}

// speechFeedbackOralReferenceText keeps the confirmed transcript immutable and
// only changes the language-assessment projection when ASR punctuation split
// two complete spoken clauses at a natural pause.
func speechFeedbackOralReferenceText(text string) string {
	matches := asrPauseBoundaryPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var normalized strings.Builder
	consumed := 0
	for _, match := range matches {
		boundaryStart, boundaryEnd := match[0], match[1]
		conjunctionStart, conjunctionEnd := match[2], match[3]
		if !looksLikeSpokenClause(clauseBefore(text, boundaryStart)) ||
			!looksLikeSpokenClause(clauseAfter(text, boundaryEnd)) {
			continue
		}
		normalized.WriteString(text[consumed:boundaryStart])
		conjunction := strings.ToLower(text[conjunctionStart:conjunctionEnd])
		if conjunction == "and" {
			normalized.WriteString(", ")
		} else {
			normalized.WriteByte(' ')
		}
		normalized.WriteString(conjunction)
		normalized.WriteByte(' ')
		consumed = boundaryEnd
	}
	if consumed == 0 {
		return text
	}
	normalized.WriteString(text[consumed:])
	return normalized.String()
}

func clauseBefore(text string, boundary int) string {
	start := strings.LastIndexAny(text[:boundary], ".!?") + 1
	return strings.TrimSpace(text[start:boundary])
}

func clauseAfter(text string, boundary int) string {
	end := strings.IndexAny(text[boundary:], ".!?")
	if end < 0 {
		end = len(text)
	} else {
		end += boundary
	}
	return strings.TrimSpace(text[boundary:end])
}

func looksLikeSpokenClause(text string) bool {
	// discipline: stay conservative; add new clause shapes only with ASR fixtures.
	words := strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !unicode.IsLetter(character) && character != '\''
	})
	for len(words) > 0 && (words[0] == "because" || words[0] == "and") {
		words = words[1:]
	}
	if len(words) < 2 {
		return false
	}
	if isPersonalSubject(words[0]) {
		// discipline: reject valid two-word clauses rather than hide an
		// incomplete predicate such as "it is" or "I need".
		return len(words) >= 3
	}
	for index, word := range words[1:] {
		if isFiniteAuxiliary(word) {
			return index+2 < len(words)
		}
	}
	return false
}

func isPersonalSubject(word string) bool {
	switch word {
	case "i", "you", "he", "she", "it", "we", "they", "there":
		return true
	default:
		return false
	}
}

func isFiniteAuxiliary(word string) bool {
	switch word {
	case "am", "is", "are", "was", "were", "have", "has", "had", "do", "does", "did",
		"can", "could", "will", "would", "shall", "should", "may", "might", "must":
		return true
	default:
		return false
	}
}

func speechFeedbackAcousticCategory(
	text string,
) AcousticAssessmentCategory {
	words := 0
	inWord := false
	for _, character := range strings.TrimSpace(text) {
		isWord := unicode.IsLetter(character) || unicode.IsNumber(character)
		if isWord && !inWord {
			words++
		}
		inWord = isWord
	}
	if words == 1 {
		return AcousticCategoryReadWord
	}
	return AcousticCategoryReadSentence
}
