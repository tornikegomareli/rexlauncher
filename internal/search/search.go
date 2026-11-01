// Package search finds panes by name, by what was said in them, and by their screens,
// and finds actions by name.
package search

import (
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

const maxRecent = 6

// Match is an entry that holds the words of a query.
type Match struct {
	textutil.Entry
	Strong bool // Each word occurs at the start of a word.
}

// Reason is why a pane is a row. The order here is the order of the sections.
type Reason int

const (
	reasonNeeds    Reason = iota // An empty query: its program is blocked or failed.
	reasonFinished               // An empty query: its program finished, and nobody looked yet.
	reasonWorking                // An empty query: its program is at work.
	reasonRecent                 // An empty query: its conversation changed last.
	reasonName                   // Its name holds the words, or an empty query.
	ReasonSkill                  // A skill, and not a pane.
	ReasonLayout                 // A saved layout, or the row that saves one.
	ReasonAction                 // An action, and not a pane.
	reasonSaid                   // Its conversation holds the words.
	reasonScreen                 // Its screen holds the words.
)

// Result is one row of the picker: a skill, an action, or a pane. There is one for each
// pane, because Enter goes to a pane, and cannot go to a message in it.
type Result struct {
	Skill  *skills.Skill
	Action *rex.Action
	Layout *layouts.Layout
	// SaveLayout is the row that saves the session's layout.
	SaveLayout bool
	Pane       rex.Pane
	Reason     Reason
	// Partial says the words only occur inside other words.
	Partial bool
	Named   bool // The name of the pane holds the words.
	// Letters are the positions in the title that a query matches with letters
	// left out, as "tlbr" in "tilebar".
	Letters []int
	Found   []Match // The best first.
	at      string
}

// key tells this row from the others, across searches.
func (r Result) Key() string {
	if r.SaveLayout {
		return "layout-to-save"
	}
	if r.Layout != nil {
		return "layout:" + r.Layout.Name
	}
	if r.Skill != nil {
		return r.Skill.Name
	}
	if r.Action != nil {
		return r.Action.Name
	}
	return r.Pane.Block
}

// IsPane says if the row is a pane, and not a skill, an action or a layout.
func (r Result) IsPane() bool {
	return r.Skill == nil && r.Action == nil && r.Layout == nil && !r.SaveLayout
}

// Matches is the number of messages or lines that hold the words.
func (r Result) Matches() int {
	return len(r.Found)
}

// Heading is the name of the section that the row is in.
func (r Result) Heading() string {
	if r.Partial {
		return "Partial matches"
	}
	switch r.Reason {
	case reasonNeeds:
		return "Needs you"
	case reasonFinished:
		return "Finished"
	case reasonWorking:
		return "Working"
	case reasonRecent:
		return "Recent conversations"
	case reasonName:
		return "Panes"
	case ReasonSkill:
		return "Skills"
	case ReasonLayout:
		return "Layouts"
	case ReasonAction:
		return "Actions"
	case reasonSaid:
		return "Conversations"
	}
	return "Screens"
}

// before says if a row comes before another. Partial matches come after all the
// others, and within a section, a whole name comes before letters left out, and the
// newest comes first.
func (r Result) before(other Result) bool {
	if r.Partial != other.Partial {
		return other.Partial
	}
	if r.Reason != other.Reason {
		return r.Reason < other.Reason
	}
	if (r.Letters == nil) != (other.Letters == nil) {
		return r.Letters == nil
	}
	return r.at > other.at
}

// filter is one tab of the picker.
type Filter struct {
	Name string
	Keep func(r Result, emptyQuery bool) bool
}

var Filters = []Filter{
	{"All", func(Result, bool) bool { return true }},
	{"Needs you", func(r Result, _ bool) bool {
		return r.Pane.Status.Needs() || r.Pane.Status.State == rex.StateDone
	}},
	{"Panes", func(r Result, _ bool) bool { return r.Named }},
	{"Conversations", func(r Result, empty bool) bool {
		return r.Pane.Conversation != nil && (empty || r.Matches() > 0)
	}},
	{"Screens", func(r Result, empty bool) bool {
		return r.Pane.Conversation == nil && (empty || r.Matches() > 0)
	}},
	{"Skills", func(r Result, _ bool) bool { return r.Skill != nil }},
	{"Layouts", func(r Result, _ bool) bool { return r.Layout != nil || r.SaveLayout }},
	{"Actions", func(r Result, _ bool) bool { return r.Action != nil }},
}

const (
	AllTab     = 0
	NeedsTab   = 1
	SkillsTab  = 5
	LayoutsTab = 6
	ActionsTab = 7
)

// search finds panes by name first, then by the words said in their
// conversations, the newest first, then by the text of their screens. All the
// words of the query must be in the same message or line. A pane where they only
// occur inside other words comes after all the others. An empty query lists the
// panes that need the person, then what finished and what is at work, then the
// conversations that changed last, and then the other panes.
func Search(panes []rex.Pane, query string) []Result {
	words := strings.Fields(textutil.Lower(query))
	if len(words) == 0 {
		return listAll(panes)
	}
	var results []Result
	for _, p := range panes {
		name := textutil.Lower(p.SessionLabel + " " + p.WindowLabel + " " + p.Name + " " + ShortHome(p.Cwd) + " " + p.Status.Message)
		named := containsAll(name, words)
		found := matchesOf(p, words)
		letters := lettersOf(textutil.Clean(p.Title()), words) // Positions in the name as drawn.
		if !named && len(found) == 0 && letters == nil {
			continue
		}
		r := Result{Pane: p, Reason: reasonName, Named: named, Found: found}
		strong := named && startsWords(name, words)
		if !strong && len(found) > 0 && (found[0].Strong || !named) {
			strong = found[0].Strong
			r.Reason = reasonScreen
			if p.Conversation != nil {
				r.Reason, r.at = reasonSaid, found[0].At
			}
		}
		r.Partial = !strong
		if letters != nil && !named && (len(found) == 0 || !found[0].Strong) {
			r.Reason, r.Partial, r.Named, r.Letters, r.at = reasonName, false, true, letters, ""
		}
		results = append(results, r)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].before(results[j]) })
	return results
}

func listAll(panes []rex.Pane) []Result {
	results := make([]Result, len(panes))
	for i, p := range panes {
		r := Result{Pane: p, Reason: reasonName, Named: true}
		at := p.Status.At.UTC().Format(time.RFC3339Nano)
		switch {
		case p.From:
			// It is behind the picker, and the person just came from it.
		case p.Status.Needs():
			r.Reason, r.at = reasonNeeds, at
		case p.Status.State == rex.StateDone:
			r.Reason, r.at = reasonFinished, at
		case p.Status.State == rex.StateWorking:
			r.Reason, r.at = reasonWorking, at
		case !p.Active().IsZero() && !p.Here:
			r.Reason, r.at = reasonRecent, p.Active().UTC().Format(time.RFC3339Nano)
		}
		results[i] = r
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].before(results[j]) })
	// Only what needs the person has no limit. Too old for its section, a pane joins the panes.
	shown := map[Reason]int{}
	for i := range results {
		if r := &results[i]; r.Reason == reasonFinished || r.Reason == reasonRecent {
			if shown[r.Reason]++; shown[r.Reason] > maxRecent {
				r.Reason = reasonName
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Reason < results[j].Reason })
	return results
}

// matches returns the best first: those that are strong, and then the newest.
func matchesOf(p rex.Pane, words []string) []Match {
	if len(words) == 0 {
		return nil
	}
	var strong, partial []Match
	for _, e := range p.Text {
		if !containsAll(e.Lower, words) {
			continue
		}
		if startsWords(e.Lower, words) {
			strong = append(strong, Match{e, true})
		} else {
			partial = append(partial, Match{e, false})
		}
	}
	return append(strong, partial...)
}

// lettersOf finds the letters of one word in a name, in order. They must start at
// the start of a word and stay close, so that a word does not match any long name
// by accident.
func lettersOf(name string, words []string) []int {
	if len(words) != 1 || len([]rune(words[0])) < 2 {
		return nil
	}
	word, text := []rune(words[0]), []rune(textutil.Lower(name))
	for start := range text {
		if text[start] != word[0] || start > 0 && (unicode.IsLetter(text[start-1]) || unicode.IsDigit(text[start-1])) {
			continue
		}
		at, positions := start, []int{}
		for _, r := range word {
			for at < len(text) && text[at] != r {
				at++
			}
			if at == len(text) {
				break
			}
			positions = append(positions, at)
			at++
		}
		if len(positions) == len(word) && at-start <= 2*len(word) {
			return positions
		}
	}
	return nil
}

func containsAll(text string, words []string) bool {
	for _, word := range words {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
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
func Around(text, word string, before, length int) string {
	runes := []rune(text)
	at, _ := find([]rune(textutil.Lower(text)), []rune(word))
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
func Marks(text string, words []string) []bool {
	lower := []rune(textutil.Lower(text))
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

func ShortHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + path[len(home):]
	}
	return path
}
