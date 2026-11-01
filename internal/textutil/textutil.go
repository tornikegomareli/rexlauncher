// Package textutil holds what the packages that read text from panes share.
package textutil

import (
	"strings"
	"unicode"
)

// Clean drops control characters, so a message cannot move the cursor or change
// the terminal's colors.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		// A joiner or a variation selector makes one emoji of several, and terminals
		// disagree on its width. A line that is wider than counted wraps and breaks the
		// whole picker, so the parts stay separate.
		if unicode.IsControl(r) || r == '‍' || r == '️' {
			return -1
		}
		return r
	}, s)
}

// Lower lowers each rune by itself, so the result has as many runes as the
// input, and a position in one is the same position in the other.
func Lower(s string) string {
	return strings.Map(unicode.ToLower, s)
}

// Entry is one message or screen line of a pane, in the form the search needs.
type Entry struct {
	Text  string
	Lower string
	Label string
	At    string
}
