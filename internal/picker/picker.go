// Package picker is the screen: the query, the rows and the preview.
package picker

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

const (
	// Below this width, the preview has no room, and the list takes all of it.
	previewMinimumWidth = 72
	refreshInterval     = 600 * time.Millisecond
	// statusEvery is how many refreshes pass between asking the panes what they do.
	statusEvery = 3

	// SGR parameters. They come from the terminal's palette, so the picker follows
	// a light or a dark theme.
	styleBold     = "1"
	styleDim      = "2"
	styleHit      = "1;33"
	styleAccent   = "34"
	styleSelected = "100"
	styleBlocked  = "1;33"
	styleError    = "1;31"
	styleDone     = "32"
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
	// Skills lists what the agent in the pane behind the picker can run.
	Skills func() (skills.Installed, error)
	// Statuses asks the panes what their programs are doing now.
	Statuses func([]rex.Pane) map[string]rex.Status
	// Answer gives an agent what the person told it.
	Answer func(rex.Pane, rex.Answer) error
	// Layouts lists the saved layouts.
	Layouts func() ([]layouts.Layout, error)
	// SaveLayout saves the session under a name.
	SaveLayout func(name string, panes []rex.Pane) error
	// DeleteLayout removes a saved layout.
	DeleteLayout func(name string) error
	// SessionName is what a new layout is called until the person names it.
	SessionName func([]rex.Pane) string
}

// Choice is what the person chose: a pane, an action, a skill, or none of them.
type Choice struct {
	Pane   *rex.Pane
	Action *rex.Action
	Skill  *skills.Skill
	Layout *layouts.Layout
}

// Run shows the picker until the person chooses or closes it.
func Run(sources Sources) (Choice, error) {
	m := &model{loadPanes: sources.Panes, loadText: sources.Text, capture: sources.Screen, loadActions: sources.Actions, loadSkills: sources.Skills,
		loadStatuses: sources.Statuses, answer: sources.Answer, loadLayouts: sources.Layouts,
		saveLayout: sources.SaveLayout, deleteLayout: sources.DeleteLayout, sessionName: sources.SessionName}
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return Choice{}, err
	}
	return Choice{Pane: m.chosen, Action: m.chosenAction, Skill: m.chosenSkill, Layout: m.chosenLayout}, m.err
}

// model is the state of the picker.
type model struct {
	loadPanes func() ([]rex.Pane, error)
	loadText  func([]rex.Pane) []rex.Pane
	capture   func(rex.Pane) (string, error)
	// loadActions lists what Rex can do.
	loadActions func() ([]rex.Action, error)
	loadSkills  func() (skills.Installed, error)
	// loadStatuses asks the panes what their programs are doing now.
	loadStatuses func([]rex.Pane) map[string]rex.Status
	answer       func(rex.Pane, rex.Answer) error
	sessionName  func([]rex.Pane) string

	ticks         int
	readingStatus bool
	// answering is the agent the person is answering, while they do.
	answering    *rex.Pane
	answerText   []rune
	answerCursor int
	// notice says what just happened, until the next key.
	notice string

	loadLayouts  func() ([]layouts.Layout, error)
	saveLayout   func(string, []rex.Pane) error
	deleteLayout func(string) error
	saved        []layouts.Layout
	savedErr     error
	// naming says the person is naming a layout, in the same line as an answer.
	naming bool
	// deleting is the layout that the next ctrl+x deletes.
	deleting string

	actions      []rex.Action
	actionsErr   error
	installed    skills.Installed
	skillsLoaded bool
	skillsErr    error
	panes        []rex.Pane
	textLoaded   bool
	query        []rune
	cursor       int
	filter       int
	rows         []search.Result
	selected     int
	offset       int

	screens    map[string]string
	capturing  bool
	previewTop int
	lines      []listLine

	width, height int

	chosen       *rex.Pane
	chosenAction *rex.Action
	chosenSkill  *skills.Skill
	chosenLayout *layouts.Layout
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
	skillsMsg struct {
		installed skills.Installed
		err       error
	}
	statusesMsg struct{ statuses map[string]rex.Status }
	answeredMsg struct {
		pane rex.Pane
		err  error
	}
	layoutsMsg struct {
		saved []layouts.Layout
		err   error
	}
	layoutDoneMsg struct {
		notice string
		err    error
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
	load, loadActions, loadSkills := m.loadPanes, m.loadActions, m.loadSkills
	return tea.Batch(tick(), m.readLayouts(), func() tea.Msg {
		panes, err := load()
		return panesMsg{panes, err}
	}, func() tea.Msg {
		actions, err := loadActions()
		return actionsMsg{actions, err}
	}, func() tea.Msg {
		installed, err := loadSkills()
		return skillsMsg{installed, err}
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
	case skillsMsg:
		m.installed, m.skillsErr, m.skillsLoaded = msg.installed, msg.err, true
		m.refresh(true)
	case textMsg:
		// The statuses may be newer than the panes this was reading.
		statuses := map[string]rex.Status{}
		for _, p := range m.panes {
			statuses[p.Block] = p.Status
		}
		m.panes, m.textLoaded = msg.panes, true
		m.applyStatuses(statuses)
		m.refresh(true)
		return m, m.captureSelected()
	case layoutsMsg:
		m.saved, m.savedErr = msg.saved, msg.err
		m.refresh(true)
	case layoutDoneMsg:
		m.notice = msg.notice
		if msg.err != nil {
			m.notice = "Failed: " + textutil.Clean(msg.err.Error())
		}
		return m, m.readLayouts()
	case statusesMsg:
		m.readingStatus = false
		m.applyStatuses(msg.statuses)
		m.refresh(true)
	case answeredMsg:
		m.notice = "Sent to " + textutil.Clean(msg.pane.Title())
		if msg.err != nil {
			m.notice = "Not sent: " + textutil.Clean(msg.err.Error())
		}
		return m, m.readStatuses()
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
		m.ticks++
		if m.ticks%statusEvery == 0 {
			return m, tea.Batch(tick(), m.captureSelected(), m.readStatuses())
		}
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
	m.notice = ""
	deleting := m.deleting
	m.deleting = ""
	if m.answering != nil {
		return m.answerKey(msg)
	}
	if m.naming {
		return m.nameKey(msg)
	}
	name := keyName(msg)
	switch name {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if r, ok := m.current(); ok {
			switch {
			case r.SaveLayout:
				m.startNaming()
				return m, nil
			case r.Layout != nil:
				m.chosenLayout = r.Layout
			case r.Skill != nil:
				m.chosenSkill = r.Skill
			case r.Action != nil:
				m.chosenAction = r.Action
			default:
				m.chosen = &r.Pane
			}
			return m, tea.Quit
		}
		return m, nil
	case "ctrl+r":
		return m, m.startAnswer()
	case "ctrl+x":
		return m, m.deleteSelected(deleting)
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
	default:
		if !editText(&m.query, &m.cursor, name, msg) {
			return m, nil
		}
	}
	m.refresh(false)
	return m, m.captureSelected()
}

// keyName is the name of a key. Typed text has none, because it can spell one.
func keyName(msg tea.KeyMsg) string {
	if msg.Type == tea.KeyRunes && !msg.Alt {
		return ""
	}
	return msg.String()
}

// editText applies a key to a line of text, and says if the text changed.
func editText(text *[]rune, cursor *int, name string, msg tea.KeyMsg) bool {
	switch name {
	case "left", "ctrl+b":
		*cursor = max(*cursor-1, 0)
		return false
	case "right", "ctrl+f":
		*cursor = min(*cursor+1, len(*text))
		return false
	case "ctrl+a":
		*cursor = 0
		return false
	case "ctrl+e":
		*cursor = len(*text)
		return false
	case "backspace":
		if *cursor > 0 {
			*text = append((*text)[:*cursor-1:*cursor-1], (*text)[*cursor:]...)
			*cursor--
		}
	case "delete", "ctrl+d":
		if *cursor < len(*text) {
			*text = append((*text)[:*cursor:*cursor], (*text)[*cursor+1:]...)
		}
	case "ctrl+u":
		*text, *cursor = nil, 0
	case "ctrl+w":
		start := *cursor
		for start > 0 && (*text)[start-1] == ' ' {
			start--
		}
		for start > 0 && (*text)[start-1] != ' ' {
			start--
		}
		*text = append((*text)[:start:start], (*text)[*cursor:]...)
		*cursor = start
	default:
		if msg.Alt || (msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace) {
			return false
		}
		// Pasted lines stay separate words.
		typed := []rune(textutil.Clean(pastedLines.Replace(string(msg.Runes))))
		*text = append((*text)[:*cursor:*cursor], append(typed, (*text)[*cursor:]...)...)
		*cursor += len(typed)
	}
	return true
}

// startAnswer begins an answer to the selected pane, if it is an agent that can take one.
func (m *model) startAnswer() tea.Cmd {
	r, ok := m.current()
	if !ok || !r.IsPane() {
		return nil
	}
	p := r.Pane
	switch {
	case p.Status.Kind == "auth":
		m.notice = "It wants a password. Press Enter to go to its pane and type it there"
	case p.Status.State != rex.StateBlocked && p.Conversation == nil:
		m.notice = "Only an agent can take an answer here"
	default:
		m.answering, m.answerText, m.answerCursor = &p, nil, 0
		m.previewTop = 0
		return m.captureSelected()
	}
	return nil
}

// startNaming begins naming a layout, with the session's name to start from.
func (m *model) startNaming() {
	name := []rune(m.sessionName(m.panes))
	m.naming, m.answerText, m.answerCursor = true, name, len(name)
}

func (m *model) nameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := keyName(msg)
	switch name {
	case "esc", "ctrl+c":
		m.naming = false
		return m, nil
	case "enter":
		text := strings.TrimSpace(string(m.answerText))
		if text == "" {
			return m, nil
		}
		m.naming = false
		save, panes := m.saveLayout, m.panes
		replacing := false
		for _, l := range m.saved {
			replacing = replacing || l.Name == text
		}
		return m, func() tea.Msg {
			notice := "Saved " + text
			if replacing {
				notice = "Replaced " + text
			}
			return layoutDoneMsg{notice, save(text, panes)}
		}
	}
	editText(&m.answerText, &m.answerCursor, name, msg)
	return m, nil
}

// deleteSelected deletes the selected layout on the second press, because it cannot be
// brought back.
func (m *model) deleteSelected(asked string) tea.Cmd {
	r, ok := m.current()
	if !ok || r.Layout == nil {
		return nil
	}
	name := r.Layout.Name
	if asked != name {
		m.deleting = name
		m.notice = "Press ctrl+x again to delete " + textutil.Clean(name)
		return nil
	}
	remove := m.deleteLayout
	return func() tea.Msg { return layoutDoneMsg{"Deleted " + name, remove(name)} }
}

func (m *model) readLayouts() tea.Cmd {
	load := m.loadLayouts
	return func() tea.Msg {
		saved, err := load()
		return layoutsMsg{saved, err}
	}
}

// onMenu says if the agent waits on a menu, which only takes a number. Text and then
// Enter would pick its default option.
func (m *model) onMenu() bool {
	return m.answering.Status.State == rex.StateBlocked
}

func (m *model) answerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := keyName(msg)
	switch name {
	case "esc", "ctrl+c":
		m.answering = nil
		return m, m.captureSelected()
	case "enter":
		if m.onMenu() {
			m.chosen, m.answering = m.answering, nil // Only its pane can take this.
			return m, tea.Quit
		}
		if len(m.answerText) == 0 {
			return m, nil
		}
		return m, m.send(rex.Answer{Text: string(m.answerText), Seen: m.answering.Status})
	}
	if m.onMenu() {
		if text := string(msg.Runes); msg.Type == tea.KeyRunes && !msg.Alt && len(text) == 1 && text[0] >= '1' && text[0] <= '9' {
			return m, m.send(rex.Answer{Key: text, Seen: m.answering.Status})
		}
		return m, nil
	}
	editText(&m.answerText, &m.answerCursor, name, msg)
	return m, nil
}

// send ends the answer and delivers it. The picker stays open for the next agent.
func (m *model) send(a rex.Answer) tea.Cmd {
	answer, p := m.answer, *m.answering
	m.answering = nil
	return func() tea.Msg {
		return answeredMsg{p, answer(p, a)}
	}
}

// readStatuses asks the panes what they are doing. Only one read runs at a time.
func (m *model) readStatuses() tea.Cmd {
	if m.readingStatus || len(m.panes) == 0 {
		return nil
	}
	m.readingStatus = true
	load, panes := m.loadStatuses, m.panes
	return func() tea.Msg { return statusesMsg{load(panes)} }
}

func (m *model) applyStatuses(statuses map[string]rex.Status) {
	for i := range m.panes {
		if s, ok := statuses[m.panes[i].Block]; ok {
			m.panes[i].Status = s
		}
	}
}

// refresh searches again. If the query is the same and only the text changed, the
// selection stays on its row.
func (m *model) refresh(keepSelection bool) {
	keep := ""
	if r, ok := m.current(); ok && keepSelection {
		keep = r.Key()
	}
	m.rows = search.All(m.panes, m.installed.Skills, m.saved, m.actions, string(m.query))
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
	actions, installed, layouts := 0, 0, 0
	for _, r := range m.rows {
		// The All tab shows a few of these, once there is a query. The others show none.
		if r.Action != nil && m.filter != search.ActionsTab {
			if m.filter != search.AllTab || empty || actions == search.MaxActionsInAll {
				continue
			}
			actions++
		}
		if r.Skill != nil && m.filter != search.SkillsTab {
			if m.filter != search.AllTab || empty || installed == search.MaxSkillsInAll {
				continue
			}
			installed++
		}
		if (r.Layout != nil || r.SaveLayout) && m.filter != search.LayoutsTab {
			if m.filter != search.AllTab || empty || layouts == search.MaxLayoutsInAll {
				continue
			}
			layouts++
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
	if !r.IsPane() {
		return false
	}
	// To answer an agent, the person must see what it asks.
	return m.answering != nil || len(m.words()) == 0 || r.Matches() == 0
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
