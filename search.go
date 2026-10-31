package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	maxInPreview = 20

	// Characters of text around a match. A row keeps few before it, because fzf
	// cuts a row at its end, and the match must stay in view.
	rowBefore     = 20
	rowLength     = 200
	previewBefore = 200
	previewLength = 800

	kindPane   = "pane"
	kindSaid   = "said"
	kindScreen = "screen"

	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	hit   = "\x1b[0;1;33m" // Starts with a reset, because a match stays bright in a dim row.
	reset = "\x1b[0m"
)

// result is one row of the picker. There is one for each pane, because Enter
// goes to a pane, and cannot go to a message in it.
type result struct {
	Pane    pane
	Kind    string // What found the pane.
	Matches int    // Messages or lines that hold the words.
	Display string
	rank    int
	at      string
}

// row is what fzf reads. It shows the display, and gives the other fields back
// to the preview and to the jump.
func (r result) row() string {
	return strings.Join([]string{r.Pane.Session, r.Pane.Window, r.Pane.Block, r.Display}, "\t")
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
		r := result{Pane: p, Kind: kindPane, Matches: len(found)}
		strong := named && startsWords(name, words)
		if !strong && len(found) > 0 && (found[0].Strong || !named) {
			strong = found[0].Strong
			r.Kind = kindScreen
			r.rank = 2
			if p.Index != "" {
				r.Kind, r.rank, r.at = kindSaid, 1, found[0].At
			}
		}
		if !strong {
			r.rank += 3
		}
		r.Display = p.display(words, found)
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

// match is one message or line of a pane that holds the words of a query.
type match struct {
	Text   string
	Label  string
	At     string
	Strong bool // Each word occurs at the start of a word.
}

// matches returns the best first: those that are strong, and then the newest.
func (p pane) matches(words []string) []match {
	path := p.Index
	if path == "" {
		path = p.Screen
	}
	if path == "" || len(words) == 0 {
		return nil
	}
	lines := readLines(path)
	seen := map[string]bool{}
	var strong, partial []match
	for i := len(lines) - 1; i >= 0; i-- {
		// Most lines do not match, so do the cheap check first.
		if !containsAll(lowerRunes(lines[i]), words) {
			continue
		}
		m := match{Text: strings.TrimSpace(clean(lines[i]))}
		if p.Index != "" {
			message := parseMessage(lines[i])
			m = match{Text: clean(plain(message.Text)), At: message.At}
			if message.Role == "user" {
				m.Label = "you: "
			}
		}
		lower := lowerRunes(m.Text)
		if seen[m.Text] || !containsAll(lower, words) {
			continue
		}
		seen[m.Text] = true
		if m.Strong = startsWords(lower, words); m.Strong {
			strong = append(strong, m)
		} else {
			partial = append(partial, m)
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

// display is two lines: where Enter goes, and why this pane is a result.
func (p pane) display(words []string, found []match) string {
	title := clean(p.title())
	first := styled(title, words, bold)
	if session := clean(p.SessionLabel); session != "" && !strings.EqualFold(session, title) {
		first += dim + " · " + reset + styled(session, words, dim)
	}
	if len(found) > 1 {
		first += dim + fmt.Sprintf("  %d matches", len(found)) + reset
	}
	if len(found) == 0 {
		return first + "\n" + styled(clean(shortHome(p.Cwd)), words, dim)
	}
	label := ""
	if found[0].Label != "" {
		label = dim + found[0].Label + reset
	}
	return first + "\n" + label + styled(around(found[0].Text, words[0], rowBefore, rowLength), words, dim)
}

// styled draws text in a style, with the words marked.
func styled(text string, words []string, style string) string {
	return style + strings.ReplaceAll(highlight(text, words), reset, reset+style) + reset
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

// find returns the position of a word in runes, preferring one at the start of
// a word, and if it is at such a start. It returns -1 if the word is not there.
func find(text, word []rune) (int, bool) {
	first := -1
	for i := 0; i+len(word) <= len(text); i++ {
		if string(text[i:i+len(word)]) != string(word) {
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

// highlight marks each place a word occurs.
func highlight(text string, words []string) string {
	runes := []rune(text)
	lower := []rune(lowerRunes(text))
	marked := make([]bool, len(runes))
	for _, word := range words {
		target := []rune(word)
		for i := 0; i+len(target) <= len(lower); i++ {
			if string(lower[i:i+len(target)]) == word {
				for j := range target {
					marked[i+j] = true
				}
			}
		}
	}
	var out strings.Builder
	on := false
	for i, r := range runes {
		if marked[i] != on {
			on = marked[i]
			if on {
				out.WriteString(hit)
			} else {
				out.WriteString(reset)
			}
		}
		out.WriteRune(r)
	}
	if on {
		out.WriteString(reset)
	}
	return out.String()
}

func shortHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + path[len(home):]
	}
	return path
}

// preview prints what the side of the picker shows for a row. The row already
// names the pane, so this is its matches, or its screen if it has none.
func preview(panes []pane, block, query string, width, height int) string {
	var p pane
	for _, candidate := range panes {
		if candidate.Block == block {
			p = candidate
		}
	}
	if p.Block == "" {
		return "" // No row is selected.
	}
	words := strings.Fields(lowerRunes(query))
	found := p.matches(words)
	if len(found) == 0 {
		text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim")
		if err != nil {
			return err.Error()
		}
		lines := strings.Split(lastLines(string(text), height), "\n")
		for i := range lines {
			lines[i] = clean(lines[i])
		}
		return strings.Join(lines, "\n")
	}
	var parts []string
	for _, m := range found[:min(maxInPreview, len(found))] {
		lines := wrap(m.Label+around(m.Text, words[0], previewBefore, previewLength), width)
		lines[0] = strings.TrimPrefix(lines[0], m.Label)
		for i := range lines {
			lines[i] = highlight(lines[i], words)
		}
		if m.Label != "" {
			lines[0] = dim + m.Label + reset + lines[0]
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

// wrap splits text into lines at spaces. A word longer than a line is split.
func wrap(text string, width int) []string {
	var lines []string
	var line []rune
	for _, field := range strings.Fields(text) {
		word := []rune(field)
		if len(line) > 0 && len(line)+1+len(word) > width {
			lines = append(lines, string(line))
			line = nil
		}
		if len(line) > 0 {
			line = append(line, ' ')
		}
		for len(word) > width {
			lines = append(lines, string(word[:width]))
			word = word[width:]
		}
		line = append(line, word...)
	}
	return append(lines, string(line))
}

// lastLines returns the end of text. A screen's newest text is at its end.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}
