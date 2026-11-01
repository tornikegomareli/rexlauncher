package picker

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/tornikegomareli/rexlauncher/internal/search"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

const (
	maxInPreview = 20

	// Characters of text around a match. A row keeps few before it, because it is
	// cut at its end, and the match must stay in view.
	rowBefore     = 8
	rowLength     = 200
	previewBefore = 200
	previewLength = 800
)

// span is text in one style.
type span struct{ text, style string }

// paint draws spans on a background, and fills the line to a width.
func paint(spans []span, background string, width int) string {
	var out strings.Builder
	used := 0
	for _, s := range spans {
		text := ansi.Truncate(s.text, width-used, "")
		style := background
		if s.style != "" {
			style += ";" + s.style
		}
		fmt.Fprintf(&out, "\x1b[0;%sm%s", style, text)
		used += ansi.StringWidth(text)
	}
	fmt.Fprintf(&out, "\x1b[0;%sm%s\x1b[0m", background, strings.Repeat(" ", max(width-used, 0)))
	return out.String()
}

// marked returns text in a style, with the words of a query marked.
func marked(text string, words []string, style string) []span {
	return markedRunes(text, search.Marks(text, words), style)
}

// markedRunes returns text in a style, with the runes that are on marked.
func markedRunes(text string, on []bool, style string) []span {
	runes := []rune(text)
	var spans []span
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && on[j] == on[i] {
			j++
		}
		s := style
		if on[i] {
			s = styleHit
		}
		spans = append(spans, span{string(runes[i:j]), s})
		i = j
	}
	return spans
}

func cut(text string, width int) string {
	return ansi.Truncate(text, max(width, 0), "…")
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	width := m.width
	lines := []string{m.headerLine(width), m.ruleLine(width), m.tabsLine(width), ""}
	height := m.bodyHeight()
	if width >= previewMinimumWidth {
		list, preview := m.listLines(m.listWidth(), height), m.previewLines(width-m.listWidth()-3, height)
		for i := range list {
			lines = append(lines, list[i]+"\x1b[2m │ \x1b[0m"+preview[i])
		}
	} else {
		lines = append(lines, m.listLines(width, height)...)
	}
	lines = append(lines, m.ruleLine(width), m.footerLine(width))
	return strings.Join(lines, "\n")
}

func (m *model) ruleLine(width int) string {
	return "\x1b[2m" + strings.Repeat("─", width) + "\x1b[0m"
}

// headerLine is the query with its cursor, and the state of the picker on the right.
func (m *model) headerLine(width int) string {
	state := m.count()
	if !m.textLoaded {
		state = "Reading conversations…"
	}
	room := max(width-ansi.StringWidth(state)-6, 1)
	start := max(m.cursor-room+1, 0)
	before, after := string(m.query[start:m.cursor]), string(m.query[m.cursor:])
	spans := []span{{" › ", styleAccent}, {before, ""}}
	switch {
	case after != "":
		first := []rune(after)
		spans = append(spans, span{string(first[:1]), "7"}, span{cut(string(first[1:]), room-ansi.StringWidth(before)-1), ""})
	case len(m.query) == 0:
		spans = append(spans, span{" ", "7"}, span{cut("Search panes, conversations and actions", room-1), styleDim})
	default:
		spans = append(spans, span{" ", "7"})
	}
	used := 3
	for _, s := range spans[1:] {
		used += ansi.StringWidth(s.text)
	}
	spans = append(spans, span{strings.Repeat(" ", max(width-used-ansi.StringWidth(state)-1, 0)), ""}, span{state, styleDim})
	return paint(spans, "49", width)
}

// count says how many rows there are, in the word for what they are.
func (m *model) count() string {
	rows, noun := m.visible(), "pane"
	for _, r := range rows {
		if r.Action != nil {
			noun = "result"
		}
	}
	if m.filter == search.ActionsTab {
		noun = "action"
	}
	if len(rows) == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", len(rows), noun)
}

func (m *model) tabsLine(width int) string {
	spans := []span{{" ", ""}}
	for i, filter := range search.Filters {
		if i == m.filter {
			spans = append(spans, span{" " + filter.Name + " ", styleSelected + ";" + styleBold})
		} else {
			spans = append(spans, span{" " + filter.Name + " ", styleDim})
		}
		spans = append(spans, span{" ", ""})
	}
	return paint(spans, "49", width)
}

// listLines draws what arrange chose.
func (m *model) listLines(width, height int) []string {
	rows := m.visible()
	lines := make([]string, 0, height)
	if len(rows) == 0 {
		text := "No pane has these words in one message or line"
		switch {
		case m.filter == search.ActionsTab && m.actionsErr != nil:
			text = textutil.Clean(m.actionsErr.Error())
		case m.filter == search.ActionsTab:
			text = "No action has these words"
		case len(m.panes) == 0 && !m.textLoaded:
			text = ""
		case len(m.query) == 0:
			text = "No other panes"
		case !m.textLoaded:
			text = "Reading conversations…"
		}
		lines = append(lines, paint([]span{{"  " + cut(text, width-2), styleDim}}, "49", width))
	}
	for _, line := range m.shown() {
		switch {
		case line.gap:
			lines = append(lines, strings.Repeat(" ", width))
		case line.heading:
			lines = append(lines, paint([]span{{" " + cut(rows[line.row].Heading(), width-1), styleDim}}, "49", width))
		default:
			lines = append(lines, m.rowLine(rows[line.row], width, line.row == m.selected))
		}
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines
}

// rowLine is one row: the name, why it is a result, and the number of matches on
// the right.
func (m *model) rowLine(r search.Result, width int, selected bool) string {
	words := m.words()
	dim, background, bar := styleDim, "49", " "
	if selected {
		dim, background, bar = "", styleSelected, "▌"
	}
	room := width - 2
	if r.Action != nil {
		title := cut(textutil.Clean(r.Action.Title), room)
		spans := append([]span{{bar, styleAccent}, {" ", ""}}, marked(title, words, styleBold)...)
		return paint(append(spans, span{"  " + textutil.Clean(r.Action.Category), dim}), background, width)
	}
	title := cut(textutil.Clean(r.Pane.Title()), room*6/10)
	spans := []span{{bar, styleAccent}, {" ", ""}}
	if r.Letters != nil {
		on := make([]bool, len([]rune(title)))
		for _, at := range r.Letters {
			if at < len(on) {
				on[at] = true
			}
		}
		spans = append(spans, markedRunes(title, on, styleBold)...)
	} else {
		spans = append(spans, marked(title, words, styleBold)...)
	}
	room -= ansi.StringWidth(title)

	accessory := ""
	if r.Matches() > 1 {
		accessory = fmt.Sprintf("%d matches", r.Matches())
	} else if len(words) == 0 && !r.Pane.Active().IsZero() {
		accessory = age(r.Pane.Active(), time.Now())
	}
	if accessory != "" {
		room -= ansi.StringWidth(accessory) + 1
	}
	if room > 3 {
		spans = append(spans, span{"  ", ""})
		room -= 2
		var best *textutil.Entry
		text := ""
		switch {
		case len(words) > 0 && r.Matches() > 0:
			best = &r.Found[0].Entry
			text = search.Around(best.Text, words[0], rowBefore, rowLength)
		case len(words) == 0 && r.Pane.Conversation != nil && len(r.Pane.Text) > 0:
			best = &r.Pane.Text[0]
			text = best.Text
		}
		if best != nil {
			if best.Label != "" && ansi.StringWidth(best.Label) < room {
				spans = append(spans, span{best.Label, dim})
				room -= ansi.StringWidth(best.Label)
			}
			text = cut(text, room)
			spans = append(spans, marked(text, words, dim)...)
			room -= ansi.StringWidth(text)
		} else {
			text := cut(textutil.Clean(search.ShortHome(r.Pane.Cwd)), room)
			spans = append(spans, marked(text, words, dim)...)
			room -= ansi.StringWidth(text)
		}
	}
	if accessory != "" {
		spans = append(spans, span{strings.Repeat(" ", max(room, 0)+1) + accessory, dim})
	}
	return paint(spans, background, width)
}

// previewLines is what the right side shows: the matches of the pane or its screen,
// and the details of the pane below.
func (m *model) previewLines(width, height int) []string {
	r, ok := m.current()
	lines := make([]string, 0, height)
	if ok && r.Action != nil {
		lines = append(lines,
			paint([]span{{cut(textutil.Clean(r.Action.Title), width), styleBold}}, "49", width),
			strings.Repeat(" ", width),
			m.detail("Action", textutil.Clean(r.Action.Name), width),
			m.detail("Category", textutil.Clean(r.Action.Category), width),
		)
	} else if ok {
		room := max(height-metadataRows-1, 0)
		var content []string
		if m.showsScreen(r) {
			content = m.screenLines(r, width, room)
		} else {
			content = m.matchLines(r, width)[m.previewTop:]
		}
		lines = append(lines, content[:min(room, len(content))]...)
		for len(lines) < room {
			lines = append(lines, strings.Repeat(" ", width))
		}
		lines = append(lines, "\x1b[2m"+strings.Repeat("─", width)+"\x1b[0m")
		lines = append(lines, m.detail("Session", textutil.Clean(r.Pane.SessionLabel), width))
		if r.Pane.WindowLabel != "" {
			lines = append(lines, m.detail("Window", textutil.Clean(r.Pane.WindowLabel), width))
		}
		if r.Pane.Cwd != "" {
			lines = append(lines, m.detail("Folder", textutil.Clean(search.ShortHome(r.Pane.Cwd)), width))
		}
		if r.Matches() > 0 {
			lines = append(lines, m.detail("Matches", fmt.Sprint(r.Matches()), width))
		} else if !r.Pane.Active().IsZero() {
			lines = append(lines, m.detail("Active", ago(r.Pane.Active()), width))
		}
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}

func (m *model) detail(label, value string, width int) string {
	return paint([]span{{fmt.Sprintf("%-9s", label), styleDim}, {cut(value, width-9), ""}}, "49", width)
}

// screenLines is the end of the pane's screen.
func (m *model) screenLines(r search.Result, width, height int) []string {
	var lines []string
	for _, line := range lastLines(m.screens[r.Pane.Block], height) {
		shown := ansi.Truncate(line, width, "")
		lines = append(lines, shown+strings.Repeat(" ", max(width-ansi.StringWidth(shown), 0))+"\x1b[0m")
	}
	return lines
}

// matchLines is each match of the pane, wrapped at words.
func (m *model) matchLines(r search.Result, width int) []string {
	words := m.words()
	var lines []string
	for i, found := range r.Found[:min(maxInPreview, len(r.Found))] {
		if i > 0 {
			lines = append(lines, strings.Repeat(" ", width))
		}
		wrapped := wrap(found.Label+search.Around(found.Text, words[0], previewBefore, previewLength), width)
		for j, line := range wrapped {
			var spans []span
			if j == 0 && found.Label != "" {
				spans = append(spans, span{found.Label, styleDim})
				// Wrapping can drop the space after the label, when the first word is long.
				line = strings.TrimPrefix(strings.TrimPrefix(line, strings.TrimSpace(found.Label)), " ")
			}
			lines = append(lines, paint(append(spans, marked(line, words, "")...), "49", width))
		}
	}
	return lines
}

func (m *model) footerLine(width int) string {
	enter := " Go to pane   "
	if r, ok := m.current(); ok && r.Action != nil {
		enter = " Run action   "
	}
	spans := []span{{" ↵", styleBold}, {enter, styleDim}, {"tab", styleBold}, {" Filter   ", styleDim}, {"esc", styleBold}, {" Close", styleDim}}
	used := 0
	for _, s := range spans {
		used += ansi.StringWidth(s.text)
	}
	if rows := len(m.visible()); rows > 0 {
		position := fmt.Sprintf("%d/%d", m.selected+1, rows)
		if gap := width - used - ansi.StringWidth(position) - 1; gap > 0 {
			spans = append(spans, span{strings.Repeat(" ", gap) + position, styleDim})
		}
	}
	return paint(spans, "49", width)
}

func ago(moment time.Time) string {
	since := age(moment, time.Now())
	if since == "now" {
		return since
	}
	return since + " ago"
}

// age is a short form of the time since a moment.
func age(moment, now time.Time) string {
	since := now.Sub(moment)
	switch {
	case since < time.Minute:
		return "now"
	case since < time.Hour:
		return fmt.Sprintf("%dm", int(since.Minutes()))
	case since < 24*time.Hour:
		return fmt.Sprintf("%dh", int(since.Hours()))
	}
	return fmt.Sprintf("%dd", int(since.Hours()/24))
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
