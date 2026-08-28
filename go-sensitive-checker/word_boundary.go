package main

import "unicode"

func hasLatinWordBoundary(text, word string, start, end int) bool {
	wordRunes := []rune(word)
	if len(wordRunes) == 0 {
		return false
	}
	for _, r := range wordRunes {
		if !(unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) || r == '_') {
			return true
		}
	}
	runes := []rune(text)
	if start < 0 || end > len(runes) || start >= end {
		return false
	}
	isToken := func(r rune) bool {
		return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) || r == '_'
	}
	return (start == 0 || !isToken(runes[start-1])) && (end == len(runes) || !isToken(runes[end]))
}
