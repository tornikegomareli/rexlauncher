// Package picker is the screen: the query, the rows and the preview.
package picker

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

const (
	// Below this width, the preview has no room, and the list takes all of it.
	previewMinimumWidth = 72
	refreshInterval     = 600 * time.Millisecond
	metadataRows        = 4

	// SGR parameters. They come from the terminal's palette, so the picker follows
	// a light or a dark theme.
	styleBold     = "1"
	styleDim      = "2"
	styleHit      = "1;33"
	styleAccent   = "34"
	styleSelected = "100"
)

// Sources are where the picker gets what it shows.
type Sources struct {
	// Panes lists the panes. It must be fast, because the picker waits for it.
	Panes func() ([]rex.Pane, error)
	// Text adds what the search reads to the panes. It can take seconds.
	Text func([]rex.Pane) []rex.Pane
	// Screen reads what a pane shows now, with its colors.
	Screen func(rex.Pane) (string, error)
	// Actions lists what Rex can do.
	Actions func() ([]rex.Action, error)
}

// Choice is what the person chose: a pane, an action, or neither.
type Choice struct {
	Pane   *rex.Pane
	Action *rex.Action
}

// Run shows the picker until the person chooses or closes it.
func Run(sources Sources) (Choice, error) {
	m := &model{loadPanes: sources.Panes, loadText: sources.Text, capture: sources.Screen, loadActions: sources.Actions}
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return Choice{}, err
	}
	return Choice{Pane: m.chosen, Action: m.chosenAction}, m.err
}

// model is the state of the picker.
type model struct {
	loadPanes func() ([]rex.Pane, error)
	loadText  func([]rex.Pane) []rex.Pane
	capture   func(rex.Pane) (string, error)
	// loadActions lists what Rex can do.
	loadActions func() ([]rex.Action, error)

	actions    []rex.Action
	actionsErr error
	panes      []rex.Pane
	textLoaded bool
	query      []rune
	cursor     int
	filter     int
	rows       []search.Result
	selected   int
	offset     int

	screens    map[string]string
	capturing  bool
	previewTop int
	lines      []listLine

	width, height int

	chosen       *rex.Pane
	chosenAction *rex.Action
	err          error
}

type (
	panesMsg struct {
		panes []rex.Pane
		err   error
	}
	actionsMsg struct {
		actions []rex.Action
		err     error
	}
	textMsg    struct{ panes []rex.Pane }
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
	load, loadActions := m.loadPanes, m.loadActions
	return tea.Batch(tick(), func() tea.Msg {
		panes, err := load()
		return panesMsg{panes, err}
	}, func() tea.Msg {
		actions, err := loadActions()
		return actionsMsg{actions, err}
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
	case actionsMsg:
		m.actions, m.actionsErr = msg.actions, msg.err
		m.refresh(true)
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
			if r.Action != nil {
				m.chosenAction = r.Action
			} else {
				m.chosen = &r.Pane
			}
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
		return m, m.setFilter((m.filter + 1) % len(search.Filters))
	case "shift+tab":
		return m, m.setFilter((m.filter + len(search.Filters) - 1) % len(search.Filters))
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
		text := []rune(textutil.Clean(pastedLines.Replace(string(msg.Runes))))
		m.query = append(m.query[:m.cursor:m.cursor], append(text, m.query[m.cursor:]...)...)
		m.cursor += len(text)
	}
	m.refresh(false)
	return m, m.captureSelected()
}

// refresh searches again. If the query is the same and only the text changed, the
// selection stays on its row.
func (m *model) refresh(keepSelection bool) {
	keep := ""
	if r, ok := m.current(); ok && keepSelection {
		keep = r.Key()
	}
	m.rows = search.All(m.panes, m.actions, string(m.query))
	m.selected, m.offset, m.previewTop = 0, 0, 0
	for i, r := range m.visible() {
		if r.Key() == keep {
			m.selected = i
		}
	}
}

// visible returns the rows of the current filter.
func (m *model) visible() []search.Result {
	empty := len(m.words()) == 0
	var rows []search.Result
	actions := 0
	for _, r := range m.rows {
		if r.Action != nil && m.filter != search.ActionsTab {
			// The All tab shows a few, once there is a query. The others show none.
			if m.filter != search.AllTab || empty || actions == search.MaxActionsInAll {
				continue
			}
			actions++
		}
		if search.Filters[m.filter].Keep(r, empty) {
			rows = append(rows, r)
		}
	}
	return rows
}

func (m *model) current() (search.Result, bool) {
	rows := m.visible()
	if m.selected < len(rows) {
		return rows[m.selected], true
	}
	return search.Result{}, false
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
	return strings.Fields(textutil.Lower(string(m.query)))
}

// showsScreen says if the preview of a row is the pane's screen.
func (m *model) showsScreen(r search.Result) bool {
	return r.Action == nil && (len(m.words()) == 0 || r.Matches() == 0)
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
			text = rex.ScreenText(err.Error()) // It holds what rex printed.
		}
		return captureMsg{p.Block, text}
	}
}
