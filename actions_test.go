package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func actionNames(results []result) []string {
	var names []string
	for _, r := range results {
		if r.Action != nil {
			names = append(names, r.Action.Name)
		}
	}
	return names
}

var someActions = []action{
	{Name: "pane.split.down", Title: "Split Pane Down", Keywords: "pane split terminal divide", Category: "Pane"},
	{Name: "pane.split.right", Title: "Split Pane Right", Keywords: "pane split terminal divide", Category: "Pane"},
	{Name: "client.theme.change", Title: "Change Theme…", Keywords: "theme colour color scheme", Category: "View"},
	{Name: "client.sidebar.toggle", Title: "Toggle Sidebar", Keywords: "terminal", Category: "View"},
}

// showTab moves to a tab the way a key does, and arranges the screen.
func showTab(m *model, tab int) {
	m.setFilter(tab)
	m.arrange()
}

func withActions(m *model, actions ...action) *model {
	send(m, actionsMsg{actions: actions})
	return m
}

func TestLoadActionsKeepsWhatOneKeyCanRun(t *testing.T) {
	fakeActions(t, `{"actions":[
		{"name":"rexlauncher","title":"Rex Launcher"},
		{"name":"lazygit","title":"LazyGit","category":"Actions"},
		{"name":"com.superlogical.terminal.format"},
		{"name":"com.superlogical.terminal.resize","args":{"required":["columns"]}}
	]}`, `{"actions":[
		{"name":"pane.split.down","title":"Split Pane Down","category":"Pane","keywords":"split"},
		{"name":"pane.split","title":"Split Pane","args":{"required":["direction"]}},
		{"name":"client.theme.change","title":"Change Theme…","args":{"properties":{"name":{}}}},
		{"name":"pane.close","title":"Close Pane"},
		{"name":"session.close","title":"Close Session"}
	]}`)
	actions, err := loadActions("client-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range actions {
		names = append(names, a.Name)
	}
	want := "pane.split.down client.theme.change lazygit"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSearchActionsPutsTitlesBeforeKeywords(t *testing.T) {
	got := actionNames(searchActions(someActions, "terminal"))
	if strings.Join(got, " ") != "pane.split.down pane.split.right client.sidebar.toggle" {
		t.Fatalf("keywords: %q", got)
	}
	got = actionNames(searchActions([]action{
		{Name: "by-keyword", Title: "Reset", Keywords: "theme"},
		{Name: "by-title", Title: "Change Theme…"},
	}, "theme"))
	if strings.Join(got, " ") != "by-title by-keyword" {
		t.Fatalf("order: %q", got)
	}
}

func TestActionsComeAfterPanesAndBeforeConversations(t *testing.T) {
	m := withActions(open(t, 120, 30,
		pane{Block: "named", Session: "s", SessionLabel: "split-work", Name: "shell"},
		pane{Block: "said", Session: "s", SessionLabel: "x", Name: "agent", Conversation: &conversation{}, Text: []entry{{Text: "split the module", lower: "split the module"}}},
	), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("split")})
	var order []string
	for _, r := range m.visible() {
		order = append(order, r.Heading())
	}
	want := "Panes Actions Actions Conversations"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnEmptyQueryShowsActionsOnlyInTheirTab(t *testing.T) {
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	if got := actionNames(m.visible()); got != nil {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, actionsTab)
	if got := actionNames(m.visible()); len(got) != len(someActions) {
		t.Fatalf("Actions tab: %q", got)
	}
}

func TestOtherTabsHaveNoActions(t *testing.T) {
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	for tab := range filters {
		if tab == actionsTab {
			continue
		}
		showTab(m, tab)
		if tab != 0 {
			send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
		}
		if got := actionNames(m.visible()); tab != 0 && got != nil {
			t.Errorf("%s: %q", filters[tab].name, got)
		}
		m.query, m.cursor = nil, 0
		m.refresh(false)
	}
}

func TestTheAllTabLimitsActions(t *testing.T) {
	var many []action
	for _, name := range strings.Fields("a b c d e f g h") {
		many = append(many, action{Name: name, Title: "Session " + name})
	}
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), many...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("session")})
	if got := actionNames(m.visible()); len(got) != maxActionsInAll {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, actionsTab)
	if got := actionNames(m.visible()); len(got) != len(many) {
		t.Fatalf("Actions tab: %q", got)
	}
}

func TestEnterChoosesTheActionAndNoPane(t *testing.T) {
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("theme")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.chosen != nil || m.chosenAction == nil || m.chosenAction.Name != "client.theme.change" {
		t.Fatalf("pane %+v, action %+v", m.chosen, m.chosenAction)
	}
}

func TestTheSelectedActionStaysWhenTextArrives(t *testing.T) {
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	showTab(m, actionsTab)
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	send(m, textMsg{panes: m.panes})
	if r, ok := m.current(); !ok || r.Action == nil || r.Action.Name != "pane.split.right" {
		t.Fatalf("selected %+v", r)
	}
}

func TestPaneSelectionIsNotMovedByActionRows(t *testing.T) {
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("split")})
	if m.selected != 0 {
		t.Fatalf("selected %d", m.selected)
	}
}

func TestAnActionRowDrawsAndNeedsNoScreen(t *testing.T) {
	asked := 0
	m := withActions(open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	m.capture = func(pane) (string, error) { asked++; return "", nil }
	showTab(m, actionsTab)
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	view := m.View()
	for _, want := range []string{"Split Pane Right", "pane.split.right", "Run action"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	if asked != 0 {
		t.Errorf("read a screen %d times for an action", asked)
	}
}

func TestAnActionsErrorIsShownInItsTab(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "b", Session: "s", Name: "shell"})
	send(m, actionsMsg{err: errString("rex actions: no such client")})
	showTab(m, actionsTab)
	if view := m.View(); !strings.Contains(view, "no such client") {
		t.Fatalf("no error in:\n%s", view)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRunActionAppliesToThePaneBehindThePicker(t *testing.T) {
	calls := fakeRex(t, nil)
	if err := runAction("pane.split.down", "s1", "b1", "client-1"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || !strings.Contains(got[0], "session.focus_block") || !strings.Contains(got[1], "-C client-1 do pane.split.down -s s1 -b b1") {
		t.Fatalf("got %q", got)
	}
}

func TestRunActionWithoutAPaneLetsRexChoose(t *testing.T) {
	calls := fakeRex(t, nil)
	if err := runAction("client.sidebar.toggle", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 1 || !strings.HasSuffix(got[0], "do client.sidebar.toggle") {
		t.Fatalf("got %q", got)
	}
}
