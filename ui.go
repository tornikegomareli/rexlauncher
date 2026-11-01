package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	// Below this width, the preview has no room, and the list takes all of it.
	previewMinimumWidth = 72
	refreshInterval     = 600 * time.Millisecond
	metadataRows        = 3

	// SGR parameters. They come from the terminal's palette, so the picker follows
	// a light or a dark theme.
	styleBold     = "1"
	styleDim      = "2"
	styleHit      = "1;33"
	styleAccent   = "34"
	styleSelected = "100"
)

// model is the state of the picker.
type model struct {
	loadPanes func() ([]pane, error)
	loadText  func([]pane) []pane
	capture   func(pane) (string, error)

	panes      []pane
	textLoaded bool
	query      []rune
	cursor     int
	filter     int
	rows       []result
	selected   int
	offset     int

	screens    map[string]string
	capturing  bool
	previewTop int
	lines      []listLine

	width, height int

	chosen *pane
	err    error
}

type (
	panesMsg struct {
		panes []pane
		err   error
	}
	textMsg    struct{ panes []pane }
	captureMsg struct {
		block string
		text  string
	}
	tickMsg struct{}
)

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Init() tea.Cmd {
	load := m.loadPanes
	return tea.Batch(tick(), func() tea.Msg {
		panes, err := load()
		return panesMsg{panes, err}
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	m.arrange()
	return next, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case panesMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.panes = msg.panes
		m.refresh(true)
		load, panes := m.loadText, m.panes
		return m, tea.Batch(m.captureSelected(), func() tea.Msg { return textMsg{load(panes)} })
	case textMsg:
		m.panes, m.textLoaded = msg.panes, true
		m.refresh(true)
		return m, m.captureSelected()
	case captureMsg:
		m.capturing = false
		if m.screens == nil {
			m.screens = map[string]string{}
		}
		m.screens[msg.block] = msg.text
		// The selection moved while this ran.
		if r, ok := m.current(); ok && r.Pane.Block != msg.block {
			return m, m.captureSelected()
		}
	case tickMsg:
		return m, tea.Batch(tick(), m.captureSelected())
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m, nil
}

func (m *model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.inPreview(msg.X) {
			m.previewTop = max(m.previewTop-1, 0)
			return m, nil
		}
		return m, m.move(m.selected - 1)
	case tea.MouseButtonWheelDown:
		if m.inPreview(msg.X) {
			m.previewTop++
			return m, nil
		}
		return m, m.move(m.selected + 1)
	case tea.MouseButtonLeft:
		if msg.Y == tabsRow {
			if tab, ok := tabAt(msg.X); ok {
				return m, m.setFilter(tab)
			}
			return m, nil
		}
		row, ok := m.rowAt(msg.Y)
		if m.inPreview(msg.X) || !ok {
			return m, nil
		}
		if row == m.selected {
			return m.key(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return m, m.move(row)
	}
	return m, nil
}

var pastedLines = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := msg.String()
	if msg.Type == tea.KeyRunes && !msg.Alt {
		name = "" // Typed text, which can spell the name of a key.
	}
	switch name {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if r, ok := m.current(); ok {
			m.chosen = &r.Pane
			return m, tea.Quit
		}
		return m, nil
	case "up", "ctrl+p":
		return m, m.move(m.selected - 1)
	case "down", "ctrl+n":
		return m, m.move(m.selected + 1)
	case "pgup":
		return m, m.move(m.selected - max(m.bodyHeight()-1, 1))
	case "pgdown":
		return m, m.move(m.selected + max(m.bodyHeight()-1, 1))
	case "home":
		return m, m.move(0)
	case "end":
		return m, m.move(len(m.visible()) - 1)
	case "shift+up":
		m.previewTop = max(m.previewTop-1, 0)
		return m, nil
	case "shift+down":
		m.previewTop++
		return m, nil
	case "tab":
		return m, m.setFilter((m.filter + 1) % len(filters))
	case "shift+tab":
		return m, m.setFilter((m.filter + len(filters) - 1) % len(filters))
	case "left", "ctrl+b":
		m.cursor = max(m.cursor-1, 0)
		return m, nil
	case "right", "ctrl+f":
		m.cursor = min(m.cursor+1, len(m.query))
		return m, nil
	case "ctrl+a":
		m.cursor = 0
		return m, nil
	case "ctrl+e":
		m.cursor = len(m.query)
		return m, nil
	case "backspace":
		if m.cursor > 0 {
			m.query = append(m.query[:m.cursor-1:m.cursor-1], m.query[m.cursor:]...)
			m.cursor--
		}
	case "delete", "ctrl+d":
		if m.cursor < len(m.query) {
			m.query = append(m.query[:m.cursor:m.cursor], m.query[m.cursor+1:]...)
		}
	case "ctrl+u":
		m.query, m.cursor = nil, 0
	case "ctrl+w":
		start := m.cursor
		for start > 0 && m.query[start-1] == ' ' {
			start--
		}
		for start > 0 && m.query[start-1] != ' ' {
			start--
		}
		m.query = append(m.query[:start:start], m.query[m.cursor:]...)
		m.cursor = start
	default:
		if msg.Alt || (msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace) {
			return m, nil
		}
		// Pasted lines stay separate words.
		text := []rune(clean(pastedLines.Replace(string(msg.Runes))))
		m.query = append(m.query[:m.cursor:m.cursor], append(text, m.query[m.cursor:]...)...)
		m.cursor += len(text)
	}
	m.refresh(false)
	return m, m.captureSelected()
}

// refresh searches again. If the query is the same and only the text changed, the
// selection stays on its pane.
func (m *model) refresh(keepSelection bool) {
	block := ""
	if r, ok := m.current(); ok && keepSelection {
		block = r.Pane.Block
	}
	m.rows = search(m.panes, string(m.query))
	m.selected, m.offset, m.previewTop = 0, 0, 0
	for i, r := range m.visible() {
		if r.Pane.Block == block {
			m.selected = i
		}
	}
}

// visible returns the rows of the current filter.
func (m *model) visible() []result {
	empty := len(m.words()) == 0
	var rows []result
	for _, r := range m.rows {
		if filters[m.filter].keep(r, empty) {
			rows = append(rows, r)
		}
	}
	return rows
}

func (m *model) current() (result, bool) {
	rows := m.visible()
	if m.selected < len(rows) {
		return rows[m.selected], true
	}
	return result{}, false
}

func (m *model) move(to int) tea.Cmd {
	m.selected = max(min(to, len(m.visible())-1), 0)
	m.previewTop = 0
	return m.captureSelected()
}

func (m *model) setFilter(filter int) tea.Cmd {
	m.filter, m.selected, m.offset, m.previewTop = filter, 0, 0, 0
	return m.captureSelected()
}

func (m *model) words() []string {
	return strings.Fields(lowerRunes(string(m.query)))
}

// showsScreen says if the preview of a row is the pane's screen.
func (m *model) showsScreen(r result) bool {
	return len(m.words()) == 0 || r.Matches() == 0
}

// captureSelected reads the screen of the selected pane, if the preview shows it.
// Only one read runs at a time, and a read that ends checks the selection again.
func (m *model) captureSelected() tea.Cmd {
	r, ok := m.current()
	if !ok || m.capturing || !m.showsScreen(r) || m.width < previewMinimumWidth {
		return nil
	}
	m.capturing = true
	capture, p := m.capture, r.Pane
	return func() tea.Msg {
		text, err := capture(p)
		if err != nil {
			text = screenText(err.Error()) // It holds what rex printed.
		}
		return captureMsg{p.Block, text}
	}
}

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
	return markedRunes(text, marks(text, words), style)
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
	state := fmt.Sprintf("%d panes", len(m.visible()))
	if len(m.visible()) == 1 {
		state = "1 pane"
	}
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
		spans = append(spans, span{" ", "7"}, span{cut("Search panes and conversations", room-1), styleDim})
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

func (m *model) tabsLine(width int) string {
	spans := []span{{" ", ""}}
	for i, filter := range filters {
		if i == m.filter {
			spans = append(spans, span{" " + filter.name + " ", styleSelected + ";" + styleBold})
		} else {
			spans = append(spans, span{" " + filter.name + " ", styleDim})
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
		text := "No pane has these words"
		switch {
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
func (m *model) rowLine(r result, width int, selected bool) string {
	words := m.words()
	dim, background, bar := styleDim, "49", " "
	if selected {
		dim, background, bar = "", styleSelected, "▌"
	}
	room := width - 2
	title := cut(clean(r.Pane.title()), room*6/10)
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
		accessory = fmt.Sprint(r.Matches())
	} else if len(words) == 0 && !r.Pane.active().IsZero() {
		accessory = age(r.Pane.active(), time.Now())
	}
	if accessory != "" {
		room -= ansi.StringWidth(accessory) + 1
	}
	if room > 3 {
		spans = append(spans, span{"  ", ""})
		room -= 2
		var best *entry
		text := ""
		switch {
		case len(words) > 0 && r.Matches() > 0:
			best = &r.Found[0].entry
			text = around(best.Text, words[0], rowBefore, rowLength)
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
			text := cut(clean(shortHome(r.Pane.Cwd)), room)
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
	if ok {
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
		lines = append(lines, m.detail("Session", clean(r.Pane.SessionLabel), width))
		if r.Pane.Cwd != "" {
			lines = append(lines, m.detail("Folder", clean(shortHome(r.Pane.Cwd)), width))
		}
		if r.Matches() > 0 {
			lines = append(lines, m.detail("Matches", fmt.Sprint(r.Matches()), width))
		} else if !r.Pane.active().IsZero() {
			lines = append(lines, m.detail("Active", ago(r.Pane.active()), width))
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
func (m *model) screenLines(r result, width, height int) []string {
	var lines []string
	for _, line := range lastLines(m.screens[r.Pane.Block], height) {
		shown := ansi.Truncate(line, width, "")
		lines = append(lines, shown+strings.Repeat(" ", max(width-ansi.StringWidth(shown), 0))+"\x1b[0m")
	}
	return lines
}

// matchLines is each match of the pane, wrapped at words.
func (m *model) matchLines(r result, width int) []string {
	words := m.words()
	var lines []string
	for i, found := range r.Found[:min(maxInPreview, len(r.Found))] {
		if i > 0 {
			lines = append(lines, strings.Repeat(" ", width))
		}
		wrapped := wrap(found.Label+around(found.Text, words[0], previewBefore, previewLength), width)
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
	spans := []span{{" ↵", styleBold}, {" Go to pane   ", styleDim}, {"tab", styleBold}, {" Filter   ", styleDim}, {"esc", styleBold}, {" Close", styleDim}}
	used := 0
	for _, s := range spans {
		used += ansi.StringWidth(s.text)
	}
	if rows := len(m.visible()); rows > 0 {
		position := fmt.Sprintf("%d/%d", m.selected+1, rows)
		spans = append(spans, span{strings.Repeat(" ", max(width-used-ansi.StringWidth(position)-1, 0)) + position, styleDim})
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
