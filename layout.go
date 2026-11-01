package main

// Where things are on the screen. The drawing and the mouse both read this, so they
// cannot disagree.
const (
	tabsRow    = 2
	listTop    = 4
	chromeRows = 6 // The lines around the list: header, rule, tabs, gap, rule, footer.
)

// tabSpan is where a tab's label and its padding sit on the tabs line.
func tabSpan(index int) (from, to int) {
	from = 1
	for _, f := range filters[:index] {
		from += len(f.name) + 3 // Its padding, and the space between tabs.
	}
	return from, from + len(filters[index].name) + 2
}

func tabAt(x int) (int, bool) {
	for i := range filters {
		if from, to := tabSpan(i); x >= from && x < to {
			return i, true
		}
	}
	return 0, false
}

func (m *model) listWidth() int {
	if m.width < previewMinimumWidth {
		return m.width
	}
	return m.width * 58 / 100
}

func (m *model) bodyHeight() int {
	return max(m.height-chromeRows, 1)
}

func (m *model) inPreview(x int) bool {
	return m.width >= previewMinimumWidth && x >= m.listWidth()
}

// listLine is what one line of the list holds: a row, the heading of a row, or a gap.
type listLine struct {
	row     int
	heading bool
	gap     bool
}

// arrange works out what the screen shows now. Update calls it after each message,
// so View only reads, and the mouse sees what was drawn.
func (m *model) arrange() {
	rows := m.visible()
	m.lines = m.lines[:0]
	lineOf := make([]int, len(rows))
	heading := ""
	for i, r := range rows {
		if m.filter == 0 && r.Heading() != heading { // The tab with every section.
			if heading != "" {
				m.lines = append(m.lines, listLine{gap: true})
			}
			heading = r.Heading()
			m.lines = append(m.lines, listLine{row: i, heading: true})
		}
		lineOf[i] = len(m.lines)
		m.lines = append(m.lines, listLine{row: i})
	}
	if m.selected < len(rows) {
		at := lineOf[m.selected]
		if at > 0 && m.lines[at-1].heading {
			at-- // Keep the heading of a section with its first row.
		}
		m.offset = min(m.offset, at, max(len(m.lines)-m.bodyHeight(), 0))
		if lineOf[m.selected] >= m.offset+m.bodyHeight() {
			m.offset = lineOf[m.selected] - m.bodyHeight() + 1
		}
	}
	if r, ok := m.current(); ok && !m.showsScreen(r) && m.width >= previewMinimumWidth {
		lines := len(m.matchLines(r, m.width-m.listWidth()-3))
		m.previewTop = min(m.previewTop, max(lines-1, 0))
	} else {
		m.previewTop = 0 // A live screen does not scroll.
	}
}

// shown is the part of the list in view.
func (m *model) shown() []listLine {
	return m.lines[min(m.offset, len(m.lines)):min(m.offset+m.bodyHeight(), len(m.lines))]
}

// rowAt returns the row on a line of the screen.
func (m *model) rowAt(y int) (int, bool) {
	shown := m.shown()
	at := y - listTop
	if at < 0 || at >= len(shown) || shown[at].heading || shown[at].gap {
		return 0, false
	}
	return shown[at].row, true
}
