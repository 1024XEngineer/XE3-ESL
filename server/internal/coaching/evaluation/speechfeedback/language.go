package speechfeedback

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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
	return projectSpeechFeedbackEnglishText(text).text
}

type speechFeedbackEnglishProjection struct {
	text           string
	originalStarts []int
	originalEnds   []int
}

func projectSpeechFeedbackEnglishText(text string) speechFeedbackEnglishProjection {
	var output strings.Builder
	originalStarts := make([]int, 0, len(text))
	originalEnds := make([]int, 0, len(text))
	needsSpace := false
	spaceStart := 0
	appendRune := func(character rune, originalStart int, originalEnd int) {
		output.WriteRune(character)
		for range utf8.RuneLen(character) {
			originalStarts = append(originalStarts, originalStart)
			originalEnds = append(originalEnds, originalEnd)
		}
	}
	for originalStart, character := range text {
		originalEnd := originalStart + utf8.RuneLen(character)
		allowed := unicode.Is(unicode.Latin, character) &&
			unicode.IsLetter(character)
		allowed = allowed || unicode.IsNumber(character) ||
			(character <= unicode.MaxASCII &&
				(unicode.IsPunct(character) || unicode.IsSpace(character)))
		if !allowed || unicode.IsSpace(character) {
			if output.Len() > 0 && !needsSpace {
				spaceStart = originalStart
			}
			needsSpace = output.Len() > 0
			continue
		}
		if needsSpace {
			appendRune(' ', spaceStart, originalStart)
		}
		appendRune(character, originalStart, originalEnd)
		needsSpace = false
	}
	projected := output.String()
	trimmed := strings.Trim(projected, " \t\r\n,;:!?-.")
	if trimmed == "" {
		return speechFeedbackEnglishProjection{}
	}
	start := strings.Index(projected, trimmed)
	end := start + len(trimmed)
	return speechFeedbackEnglishProjection{
		text:           trimmed,
		originalStarts: originalStarts[start:end],
		originalEnds:   originalEnds[start:end],
	}
}

func (projection speechFeedbackEnglishProjection) excerptRange(
	excerpt string,
	occurrence int,
) (int, int, bool) {
	projectedStart := speechFeedbackExcerptStart(
		projection.text,
		excerpt,
		occurrence,
	)
	projectedEnd := projectedStart + len(excerpt)
	if projectedStart < 0 || projectedEnd > len(projection.originalStarts) ||
		projectedStart >= projectedEnd {
		return 0, 0, false
	}
	return projection.originalStarts[projectedStart],
		projection.originalEnds[projectedEnd-1], true
}

// speechFeedbackOralReferenceText keeps the confirmed transcript immutable and
// only changes the language-assessment projection when ASR punctuation split
// two complete spoken clauses at a natural pause.
func speechFeedbackOralReferenceText(text string) string {
	starts := make([]int, len(text))
	ends := make([]int, len(text))
	for index := range text {
		starts[index] = index
		ends[index] = index + 1
	}
	return projectSpeechFeedbackOralReference(speechFeedbackEnglishProjection{
		text:           text,
		originalStarts: starts,
		originalEnds:   ends,
	}).text
}

func projectSpeechFeedbackOralReference(
	projection speechFeedbackEnglishProjection,
) speechFeedbackEnglishProjection {
	matches := asrPauseBoundaryPattern.FindAllStringSubmatchIndex(
		projection.text,
		-1,
	)
	if len(matches) == 0 {
		return projection
	}
	var normalized strings.Builder
	starts := make([]int, 0, len(projection.text))
	ends := make([]int, 0, len(projection.text))
	appendRange := func(start int, end int) {
		normalized.WriteString(projection.text[start:end])
		starts = append(starts, projection.originalStarts[start:end]...)
		ends = append(ends, projection.originalEnds[start:end]...)
	}
	appendMapped := func(value string, originalStart int, originalEnd int) {
		normalized.WriteString(value)
		for range len(value) {
			starts = append(starts, originalStart)
			ends = append(ends, originalEnd)
		}
	}
	consumed := 0
	for _, match := range matches {
		boundaryStart, boundaryEnd := match[0], match[1]
		conjunctionStart, conjunctionEnd := match[2], match[3]
		if !looksLikeSpokenClause(clauseBefore(projection.text, boundaryStart)) ||
			!looksLikeSpokenClause(clauseAfter(projection.text, boundaryEnd)) {
			continue
		}
		appendRange(consumed, boundaryStart)
		conjunction := strings.ToLower(
			projection.text[conjunctionStart:conjunctionEnd],
		)
		separator := " "
		if conjunction == "and" {
			separator = ", "
		}
		appendMapped(
			separator,
			projection.originalStarts[boundaryStart],
			projection.originalEnds[conjunctionStart-1],
		)
		normalized.WriteString(conjunction)
		starts = append(
			starts,
			projection.originalStarts[conjunctionStart:conjunctionEnd]...,
		)
		ends = append(
			ends,
			projection.originalEnds[conjunctionStart:conjunctionEnd]...,
		)
		appendMapped(
			" ",
			projection.originalStarts[conjunctionEnd],
			projection.originalEnds[boundaryEnd-1],
		)
		consumed = boundaryEnd
	}
	if consumed == 0 {
		return projection
	}
	appendRange(consumed, len(projection.text))
	return speechFeedbackEnglishProjection{
		text:           normalized.String(),
		originalStarts: starts,
		originalEnds:   ends,
	}
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
