package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxPerPane = 20
	maxResults = 300
	snippet    = 80 // Characters of text shown around a match.

	kindPane   = "pane"
	kindSaid   = "said"
	kindScreen = "screen"

	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	hit   = "\x1b[1;33m"
	reset = "\x1b[0m"
)

// result is one row of the picker.
type result struct {
	Pane    pane
	Kind    string
	Line    int // 1-based, in the pane's index or screen file.
	Display string
}

// row is what fzf reads. It shows the display, and gives the other fields back
// to the preview and to the jump.
func (r result) row() string {
	return strings.Join([]string{r.Pane.Session, r.Pane.Window, r.Pane.Block, r.Kind, strconv.Itoa(r.Line), r.Display}, "\t")
}

// search finds panes by name first, then the words said in their conversations,
// the newest first, then the text of their screens. All the words of the query
// must be in the same message or line.
func search(panes []pane, query string) []result {
	words := strings.Fields(lowerRunes(query))
	var results []result
	for _, p := range panes {
		if containsAll(lowerRunes(p.place()+" "+p.Cwd), words) {
			results = append(results, result{Pane: p, Kind: kindPane, Display: p.display(words)})
		}
	}
	if len(words) == 0 {
		return results
	}
	for _, p := range panes {
		if p.Index != "" {
			results = append(results, searchFile(p, p.Index, kindSaid, words)...)
		}
	}
	for _, p := range panes {
		if p.Screen != "" {
			results = append(results, searchFile(p, p.Screen, kindScreen, words)...)
		}
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func (p pane) place() string {
	return p.SessionLabel + " › " + p.WindowLabel + " › " + p.Name
}

func (p pane) display(words []string) string {
	line := highlight(clean(p.place()), words)
	if p.Cwd != "" {
		line += "  " + dim + clean(shortHome(p.Cwd)) + reset
	}
	return line
}

func searchFile(p pane, path, kind string, words []string) []result {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var results []result
	// From the end, because the newest text is the most likely target.
	for i := len(lines) - 1; i >= 0 && len(results) < maxPerPane; i-- {
		text, label := lines[i], ""
		if kind == kindSaid {
			m := parseMessage(lines[i])
			text, label = m.Text, roleLabel(m.Role)+"  "
		}
		if !containsAll(lowerRunes(text), words) {
			continue
		}
		// The text comes first, because fzf cuts a long row at its end.
		display := label + highlight(around(clean(text), words[0]), words) + "  " + dim + clean(p.SessionLabel+" › "+p.Name) + reset
		results = append(results, result{Pane: p, Kind: kind, Line: i + 1, Display: display})
	}
	return results
}

func roleLabel(role string) string {
	if role == "user" {
		return "you"
	}
	return "claude"
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

// around returns the part of text near the word.
func around(text, word string) string {
	runes := []rune(text)
	if len(runes) <= snippet {
		return text
	}
	start := 0
	if at := strings.Index(lowerRunes(text), word); at >= 0 {
		start = len([]rune(text[:at])) - snippet/3
	}
	start = max(0, min(start, len(runes)-snippet))
	out := string(runes[start : start+snippet])
	if start > 0 {
		out = "…" + out
	}
	if start+snippet < len(runes) {
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

// preview prints what the right side of the picker shows for a row.
func preview(panes []pane, block, kind string, line int, query string) string {
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
	head := bold + clean(p.place()) + reset + "\n" + dim + clean(shortHome(p.Cwd)) + reset + "\n\n"
	switch kind {
	case kindSaid:
		return head + previewMessages(p.Index, line, words)
	case kindScreen:
		return head + previewScreen(p.Screen, line, words)
	}
	text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim")
	if err != nil {
		return head + err.Error()
	}
	lines := strings.Split(string(text), "\n")
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return head + strings.Join(lines, "\n")
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func previewMessages(path string, line int, words []string) string {
	lines := readLines(path)
	var out strings.Builder
	for i := max(1, line-2); i <= min(len(lines), line+2); i++ {
		m := parseMessage(lines[i-1])
		text := clean(m.Text)
		if i == line {
			text = highlight(text, words)
		}
		fmt.Fprintf(&out, "%s%s%s\n%s\n\n", bold, roleLabel(m.Role), reset, text)
	}
	return out.String()
}

func previewScreen(path string, line int, words []string) string {
	lines := readLines(path)
	var out strings.Builder
	for i := max(1, line-15); i <= min(len(lines), line+15); i++ {
		text := clean(lines[i-1])
		if i == line {
			text = highlight(text, words)
		}
		out.WriteString(text + "\n")
	}
	return out.String()
}
