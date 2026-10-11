package tui

import (
	"strings"
	"unicode"
)

// rotateCase cycles s through lower case → Capitalized → ALL CAPS → lower
// case. It reports false when s has no letters with case.
func rotateCase(s string) (string, bool) {
	upper, lower := strings.ToUpper(s), strings.ToLower(s)
	if upper == lower {
		return s, false
	}
	switch s {
	case upper:
		return lower, true
	case capitalize(lower):
		return upper, true
	}
	return capitalize(lower), true
}

// capitalize upper-cases the first letter, skipping leading punctuation
// such as the apostrophe in 'cause.
func capitalize(s string) string {
	r := []rune(s)
	for i, c := range r {
		if unicode.IsLetter(c) {
			r[i] = unicode.ToUpper(c)
			break
		}
	}
	return string(r)
}

// caseName describes the result of rotateCase for the status line.
func caseName(s string) string {
	switch s {
	case strings.ToUpper(s):
		return "ALL CAPS"
	case strings.ToLower(s):
		return "lower case"
	}
	return "Capitalized"
}
