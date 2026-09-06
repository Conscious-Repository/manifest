package server

import (
	"strings"
	"unicode"
)

// capTaskComment preserves formatting and UTF-8, preferring the last sentence
// ending within the ceiling. With no sentence ending, cut at a whole word.
// Sentence detection is deliberately conservative: terminal punctuation on a
// whitespace-delimited word (optionally followed by quotes/closing brackets).
func capTaskComment(phase, text string) string {
	if !isTaskCommentPhase(phase) {
		return text
	}
	words, wordEnd, sentenceEnd := 0, 0, 0
	inWord := false
	for i, r := range text {
		if !unicode.IsSpace(r) {
			if !inWord {
				if words == taskCommentMaxWords {
					if sentenceEnd > 0 {
						wordEnd = sentenceEnd
					}
					return strings.TrimSpace(text[:wordEnd]) + "…"
				}
				words++
				inWord = true
			}
			continue
		}
		if inWord {
			wordEnd = i
			end := strings.TrimRight(text[:i], "\"'”’)]}")
			if strings.HasSuffix(end, ".") || strings.HasSuffix(end, "!") || strings.HasSuffix(end, "?") {
				sentenceEnd = i
			}
			inWord = false
		}
	}
	return text
}
