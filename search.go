package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	kindPane   = "pane"
	kindSaid   = "said"
	kindScreen = "screen"

	maxInPreview = 20

	// Characters of text around a match. A row keeps few before it, because it is
	// cut at its end, and the match must stay in view.
	rowBefore     = 8
	rowLength     = 200
	previewBefore = 200
	previewLength = 800
)

// entry is one message or screen line of a pane, in the form the search needs.
type entry struct {
	Text  string
	lower string
	Label string
	At    string
}

// messageEntries reads an index, the newest message first. Identical messages
// count once.
func messageEntries(path string) []entry {
	lines := readLines(path)
	seen := map[string]bool{}
	var entries []entry
	for i := len(lines) - 1; i >= 0; i-- {
		message := parseMessage(lines[i])
		text := clean(plain(message.Text))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		e := entry{Text: text, lower: lowerRunes(text), At: message.At}
		if message.Role == "user" {
			e.Label = "you: "
		}
		entries = append(entries, e)
	}
	return entries
}

// screenEntries splits a screen's text, the newest line first.
func screenEntries(text string) []entry {
	lines := strings.Split(text, "\n")
	seen := map[string]bool{}
	var entries []entry
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(clean(lines[i]))
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		entries = append(entries, entry{Text: line, lower: lowerRunes(line)})
	}
	return entries
}

// match is an entry that holds the words of a query.
type match struct {
	entry
	Strong bool // Each word occurs at the start of a word.
}

// result is one row of the picker. There is one for each pane, because Enter
// goes to a pane, and cannot go to a message in it.
type result struct {
	Pane  pane
	Kind  string  // What found the pane.
	Named bool    // The name of the pane holds the words.
	Found []match // The best first.
	rank  int
	at    string
}

// Matches is the number of messages or lines that hold the words.
func (r result) Matches() int {
	return len(r.Found)
}

// Heading is the name of the section that the row is in.
func (r result) Heading() string {
	switch r.rank {
	case 0:
		return "Panes"
	case 1:
		return "Conversations"
	case 2:
		return "Screens"
	}
	return "Partial matches"
}

// search finds panes by name first, then by the words said in their
// conversations, the newest first, then by the text of their screens. All the
// words of the query must be in the same message or line. A pane where they only
// occur inside other words comes after all the others.
func search(panes []pane, query string) []result {
	words := strings.Fields(lowerRunes(query))
	var results []result
	for _, p := range panes {
		name := lowerRunes(p.SessionLabel + " " + p.WindowLabel + " " + p.Name + " " + shortHome(p.Cwd))
		named := containsAll(name, words)
		found := p.matches(words)
		if !named && len(found) == 0 {
			continue
		}
		r := result{Pane: p, Kind: kindPane, Named: named, Found: found}
		strong := named && startsWords(name, words)
		if !strong && len(found) > 0 && (found[0].Strong || !named) {
			strong = found[0].Strong
			r.Kind, r.rank = kindScreen, 2
			if p.Conversation {
				r.Kind, r.rank, r.at = kindSaid, 1, found[0].At
			}
		}
		if !strong {
			r.rank += 3
		}
		results = append(results, r)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].rank != results[j].rank {
			return results[i].rank < results[j].rank
		}
		return results[i].at > results[j].at
	})
	return results
}

// matches returns the best first: those that are strong, and then the newest.
func (p pane) matches(words []string) []match {
	if len(words) == 0 {
		return nil
	}
	var strong, partial []match
	for _, e := range p.Text {
		if !containsAll(e.lower, words) {
			continue
		}
		if startsWords(e.lower, words) {
			strong = append(strong, match{e, true})
		} else {
			partial = append(partial, match{e, false})
		}
	}
	return append(strong, partial...)
}

// title is the name of the pane. Rex can have none for it.
func (p pane) title() string {
	for _, name := range []string{p.Name, p.WindowLabel, p.SessionLabel} {
		if name != "" {
			return name
		}
	}
	return "pane"
}

var (
	imageTag = regexp.MustCompile(`\[Image #\d+\]`)
	heading  = regexp.MustCompile(`(^|\s)#{1,6}\s`)
	markdown = strings.NewReplacer("**", "", "`", "")
)

// plain removes what a conversation's text has only for the terminal that draws it.
func plain(text string) string {
	text = imageTag.ReplaceAllString(text, "")
	text = heading.ReplaceAllString(text, "$1")
	return strings.Join(strings.Fields(markdown.Replace(text)), " ")
}

func containsAll(text string, words []string) bool {
	for _, word := range words {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// lowerRunes lowers each rune by itself, so the result has as many runes as the
// input, and a position in one is the same position in the other.
func lowerRunes(s string) string {
	return strings.Map(unicode.ToLower, s)
}

// clean drops control characters, so a message cannot move the cursor or change
// the terminal's colors.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func runesEqual(a, b []rune) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// find returns the position of a word in runes, preferring one at the start of
// a word, and if it is at such a start. It returns -1 if the word is not there.
func find(text, word []rune) (int, bool) {
	first := -1
	for i := 0; i+len(word) <= len(text); i++ {
		if !runesEqual(text[i:], word) {
			continue
		}
		if i == 0 || !unicode.IsLetter(text[i-1]) && !unicode.IsDigit(text[i-1]) {
			return i, true
		}
		if first < 0 {
			first = i
		}
	}
	return first, false
}

func startsWords(lower string, words []string) bool {
	text := []rune(lower)
	for _, word := range words {
		if _, strong := find(text, []rune(word)); !strong {
			return false
		}
	}
	return true
}

// around returns the part of text near the word. It starts at a whole word if
// one starts between the limit and the match.
func around(text, word string, before, length int) string {
	runes := []rune(text)
	at, _ := find([]rune(lowerRunes(text)), []rune(word))
	start := max(0, at-before)
	if start > 0 {
		for i := start; i < at; i++ {
			if runes[i-1] == ' ' {
				start = i
				break
			}
		}
	}
	end := min(len(runes), start+length)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// marks says for each rune of text if it is part of a word.
func marks(text string, words []string) []bool {
	lower := []rune(lowerRunes(text))
	marked := make([]bool, len(lower))
	for _, word := range words {
		target := []rune(word)
		for i := 0; i+len(target) <= len(lower); i++ {
			if runesEqual(lower[i:], target) {
				for j := range target {
					marked[i+j] = true
				}
			}
		}
	}
	return marked
}

func shortHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + path[len(home):]
	}
	return path
}

// wrap splits text into lines at spaces. A word longer than a line is split.
func wrap(text string, width int) []string {
	return strings.Split(ansi.Wrap(text, width, ""), "\n")
}

// lastLines returns the end of text. A screen's newest text is at its end.
func lastLines(text string, n int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return lines[max(0, len(lines)-n):]
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}
