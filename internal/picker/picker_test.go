package picker

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

func said(role, text string) conversations.Message {
	return conversations.Message{Role: role, At: "t", Text: text}
}

// talking is a pane with a conversation.
func talking(block, name string, messages ...conversations.Message) rex.Pane {
	return rex.Pane{Block: block, Name: name, Conversation: &conversations.Conversation{}, Text: search.MessageEntries(messages)}
}

// showing is a pane with text on its screen.
func showing(block, name string, text string) rex.Pane {
	return rex.Pane{Block: block, Name: name, Text: search.ScreenEntries(text)}
}

// open returns a picker with its panes and text loaded.
func open(t *testing.T, width, height int, panes ...rex.Pane) *model {
	t.Helper()
	m := &model{
		loadStatuses: func([]rex.Pane) map[string]rex.Status { return nil },
		loadLayouts:  func() ([]layouts.Layout, error) { return nil, nil },
		sessionName:  func([]rex.Pane) string { return "work" },
		loadText:     func(p []rex.Pane) []rex.Pane { return p },
		capture: func(p rex.Pane) (string, error) {
			return "screen of " + p.Block + "\n", nil
		},
	}
	send(m, tea.WindowSizeMsg{Width: width, Height: height})
	send(m, panesMsg{panes: panes})
	return m
}

// send gives a message to the picker, and runs the commands that it starts, except
// for the timer.
func send(m *model, msg tea.Msg) tea.Cmd {
	_, cmd := m.Update(msg)
	runCmd(m, cmd)
	return cmd
}

func runCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(m, c)
		}
		return
	}
	switch msg.(type) {
	case panesMsg, textMsg, captureMsg, statusesMsg, answeredMsg, layoutsMsg, layoutDoneMsg:
		send(m, msg)
	}
}

func typeText(m *model, text string) {
	for _, r := range text {
		send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func press(m *model, key tea.KeyType) {
	send(m, tea.KeyMsg{Type: key})
}

func view(m *model) string {
	return ansi.Strip(m.View())
}

func demoPanes(t *testing.T) []rex.Pane {
	t.Helper()
	first := talking("talk", "drooop-demos", said("user", "our panes project"), said("assistant", "the panes draw well"))
	first.SessionLabel, first.Cwd = "work", "/code/drooop"
	second := showing("shell", "build", "split panes in the log")
	second.SessionLabel, second.Cwd = "ops", "/code/ops"
	third := rex.Pane{Block: "named", Name: "panes-app", SessionLabel: "apps", Cwd: "/code/app"}
	return []rex.Pane{first, second, third}
}

func TestEveryLineFitsTheWindow(t *testing.T) {
	long := showing("b", strings.Repeat("name", 40), strings.Repeat("a word ", 200))
	long.Cwd = "/" + strings.Repeat("folder/", 40)
	for _, width := range []int{20, 40, 71, 72, 100, 200} {
		for _, height := range []int{3, 6, 30} {
			m := open(t, width, height, long, rex.Pane{Block: "c", Name: "გამარჯობა მსოფლიო"})
			typeText(m, "word")
			lines := strings.Split(view(m), "\n")
			if len(lines) > max(height, 7) {
				t.Errorf("%dx%d: %d lines", width, height, len(lines))
			}
			for _, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d: a line of %d: %q", width, height, got, line)
				}
			}
		}
	}
}

func TestARowNamesThePaneAndItsBestMatch(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "draw")
	out := view(m)
	line := out[strings.Index(out, "drooop-demos"):]
	line = line[:strings.Index(line, "\n")]
	if !strings.Contains(line, "panes draw well") {
		t.Fatalf("got %q", line)
	}
	if !strings.Contains(m.View(), "\x1b[0;100;1;33mdraw") {
		t.Fatalf("no mark on the selected row: %q", m.View())
	}
}

func TestSectionsNameWhatFoundTheRows(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	var found []string
	for _, line := range strings.Split(view(m), "\n") {
		for _, heading := range []string{"Panes", "Conversations", "Screens"} {
			if strings.HasPrefix(line, " "+heading+" ") {
				found = append(found, heading)
			}
		}
	}
	if got := strings.Join(found, " "); got != "Panes Conversations Screens" {
		t.Fatalf("got %q in\n%s", got, view(m))
	}
}

func TestPreviewIsOnTheRightAndShowsTheMatches(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "draw")
	for _, line := range strings.Split(view(m), "\n") {
		if strings.Contains(line, "│") && strings.Contains(line, "the panes draw well") {
			if strings.Index(line, "│") > strings.LastIndex(line, "the panes draw well") {
				t.Fatalf("not on the right: %q", line)
			}
			return
		}
	}
	t.Fatalf("got\n%s", view(m))
}

func TestPreviewListsEveryMatchWithItsLabel(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	press(m, tea.KeyDown)
	out := view(m)
	if !strings.Contains(out, "you: our panes project") || !strings.Contains(out, "the panes draw well") {
		t.Fatalf("got\n%s", out)
	}
}

func TestPreviewShowsTheDetailsOfThePane(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "draw")
	out := view(m)
	for _, want := range []string{"Session  work", "Folder   /code/drooop", "Matches  1"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

func TestPreviewShowsTheScreenWithoutAMatchInText(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	if out := view(m); !strings.Contains(out, "screen of talk") {
		t.Fatalf("empty query: %s", out)
	}
	typeText(m, "panes-app")
	if out := view(m); !strings.Contains(out, "screen of named") {
		t.Fatalf("a name: %s", out)
	}
}

func TestANarrowWindowHasNoPreview(t *testing.T) {
	m := open(t, previewMinimumWidth-1, 30, demoPanes(t)...)
	typeText(m, "draw")
	if out := view(m); strings.Contains(out, "│") || strings.Contains(out, "Session") {
		t.Fatalf("got\n%s", out)
	}
}

func TestTabChangesTheFilter(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	want := map[string][]string{
		"All":           {"named", "talk", "shell"},
		"Panes":         {"named"},
		"Conversations": {"talk"},
		"Screens":       {"shell"},
	}
	for _, f := range search.Filters {
		filter := f.Name
		var got []string
		for _, r := range m.visible() {
			got = append(got, r.Pane.Block)
		}
		if fmt.Sprint(got) != fmt.Sprint(want[filter]) {
			t.Errorf("%s: got %v", filter, got)
		}
		press(m, tea.KeyTab)
	}
	if m.filter != 0 {
		t.Fatalf("filter %d after a full turn", m.filter)
	}
	press(m, tea.KeyShiftTab)
	if search.Filters[m.filter].Name != "Actions" {
		t.Fatalf("filter %s", search.Filters[m.filter].Name)
	}
}

func TestEnterChoosesTheSelectedRow(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	press(m, tea.KeyDown)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.chosen == nil || m.chosen.Block != "shell" {
		t.Fatalf("chose %+v", m.chosen)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the picker did not close")
	}
}

func TestEnterOnNoRowDoesNothing(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "nothing-like-this")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.chosen != nil || cmd != nil {
		t.Fatalf("chose %+v", m.chosen)
	}
	if out := view(m); !strings.Contains(out, "No pane has these words") {
		t.Fatalf("got\n%s", out)
	}
}

func TestEscapeClosesWithoutChoosing(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(tea.QuitMsg); !ok || m.chosen != nil {
		t.Fatalf("chose %+v", m.chosen)
	}
}

func TestEditingTheQuery(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "one two")
	send(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := string(m.query); got != "one " {
		t.Fatalf("after ctrl+w: %q", got)
	}
	press(m, tea.KeyLeft)
	press(m, tea.KeyLeft)
	typeText(m, "X")
	if got := string(m.query); got != "onXe " || m.cursor != 3 {
		t.Fatalf("typed in the middle: %q at %d", got, m.cursor)
	}
	press(m, tea.KeyBackspace)
	press(m, tea.KeyDelete)
	if got := string(m.query); got != "on " {
		t.Fatalf("after backspace and delete: %q", got)
	}
	press(m, tea.KeyCtrlU)
	if len(m.query) != 0 || m.cursor != 0 {
		t.Fatalf("after ctrl+u: %q", string(m.query))
	}
	send(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if got := string(m.query); got != " " {
		t.Fatalf("a space: %q", got)
	}
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("paste\x1b[2Jd"), Paste: true})
	if strings.Contains(string(m.query), "\x1b") {
		t.Fatalf("pasted a control character: %q", string(m.query))
	}
}

func TestTheSelectionStaysInViewWithItsHeading(t *testing.T) {
	var panes []rex.Pane
	for i := range 40 {
		panes = append(panes, rex.Pane{Block: fmt.Sprint(i), Name: fmt.Sprintf("pane-%d", i)})
	}
	m := open(t, 120, 12, panes...)
	for range 30 {
		press(m, tea.KeyDown)
	}
	if out := view(m); !strings.Contains(out, "pane-30") || strings.Contains(out, "pane-5 ") {
		t.Fatalf("got\n%s", out)
	}
	press(m, tea.KeyHome)
	out := view(m)
	if !strings.Contains(out, " Panes") || !strings.Contains(out, "pane-0") {
		t.Fatalf("got\n%s", out)
	}
	press(m, tea.KeyEnd)
	if out := view(m); !strings.Contains(out, "pane-39") || !strings.Contains(out, "40/40") {
		t.Fatalf("got\n%s", out)
	}
}

func TestTextLoadedLaterKeepsTheSelection(t *testing.T) {
	panes := []rex.Pane{{Block: "a", Name: "one"}, {Block: "b", Name: "two"}}
	m := &model{
		loadText: func(p []rex.Pane) []rex.Pane { return nil },
		capture:  func(p rex.Pane) (string, error) { return "", nil },
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(panesMsg{panes: panes})
	if out := view(m); !strings.Contains(out, "Reading conversations…") {
		t.Fatalf("got\n%s", out)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(textMsg{panes})
	if r, ok := m.current(); !ok || r.Pane.Block != "b" {
		t.Fatalf("selected %+v", r)
	}
	if out := view(m); strings.Contains(out, "Reading conversations…") {
		t.Fatalf("got\n%s", out)
	}
}

func TestAFailureToListPanesCloses(t *testing.T) {
	m := &model{}
	_, cmd := m.Update(panesMsg{err: fmt.Errorf("no server")})
	if _, ok := cmd().(tea.QuitMsg); !ok || m.err == nil {
		t.Fatalf("err %v", m.err)
	}
}

func TestAScreenReadOfAnotherPaneReadsTheSelectedOneNext(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	m.capturing = true
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // Does not read, because a read is running.
	_, cmd := m.Update(captureMsg{block: "talk", text: "old"})
	if cmd == nil {
		t.Fatal("no new read")
	}
	if msg := cmd().(captureMsg); msg.block != "shell" {
		t.Fatalf("read %s", msg.block)
	}
}

func TestLettersAreMarkedInTheNameAsDrawn(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "\x07\x07tilebar", SessionLabel: "s"})
	typeText(m, "tlbr")
	out := m.View()
	for _, want := range []string{styleHit + "mt", styleHit + "ml", styleHit + "mb", styleHit + "mr"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in %q", want, out)
		}
	}
}

func TestALetterLeftOutMarksTheOthers(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "tilebar", SessionLabel: "s"})
	typeText(m, "tlbr")
	out := m.View()
	for _, want := range []string{styleHit + "mt", styleHit + "ml", styleHit + "mb", styleHit + "mr"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in %q", want, out)
		}
	}
}

func TestPreviewKeepsTheColorsOfTheScreenAndNothingElse(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "x"})
	send(m, captureMsg{block: "a", text: rex.ScreenText("\x1b[31mred\x1b[0m \x1b[3Cgap\x1b[2J\x1b]0;title\x07\n")})
	out := m.View()
	if !strings.Contains(out, "\x1b[31mred") {
		t.Fatalf("no color in %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "red    gap") {
		t.Fatalf("no gap in %q", ansi.Strip(out))
	}
	for _, unwanted := range []string{"\x1b[2J", "title", "\x07"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("%q in %q", unwanted, out)
		}
	}
}

func TestAColorOfTheScreenStaysInsideThePreview(t *testing.T) {
	long := "\x1b[31m" + strings.Repeat("x", 300) + "\n"
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "x"})
	send(m, captureMsg{block: "a", text: rex.ScreenText(long)})
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "xxx") && !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("no reset at the end of %q", line)
		}
		if got := ansi.StringWidth(line); got > 120 {
			t.Fatalf("a line of %d", got)
		}
	}
}

func TestThePreviewReadsTheScreenAgainOnTheTimer(t *testing.T) {
	reads := 0
	m := &model{
		loadStatuses: func([]rex.Pane) map[string]rex.Status { return nil },
		loadLayouts:  func() ([]layouts.Layout, error) { return nil, nil },
		sessionName:  func([]rex.Pane) string { return "work" },
		loadText:     func(p []rex.Pane) []rex.Pane { return p },
		capture: func(p rex.Pane) (string, error) {
			reads++
			return fmt.Sprintf("read %d", reads), nil
		},
	}
	send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	send(m, panesMsg{panes: []rex.Pane{{Block: "a", Name: "x"}}})
	before := reads
	send(m, tickMsg{})
	if reads != before+1 || !strings.Contains(view(m), fmt.Sprintf("read %d", reads)) {
		t.Fatalf("%d reads, then %d: %s", before, reads, view(m))
	}
}

func click(m *model, x, y int) tea.Cmd {
	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return cmd
}

func wheel(m *model, x, y int, button tea.MouseButton) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button})
}

func lineOf(t *testing.T, m *model, text string) int {
	t.Helper()
	for i, line := range strings.Split(view(m), "\n") {
		if strings.Contains(line, text) {
			return i
		}
	}
	t.Fatalf("no %q in\n%s", text, view(m))
	return 0
}

func TestClickingARowSelectsItAndClickingItAgainGoes(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	click(m, 5, lineOf(t, m, "build"))
	if r, _ := m.current(); r.Pane.Block != "shell" || m.chosen != nil {
		t.Fatalf("selected %s, chose %+v", r.Pane.Block, m.chosen)
	}
	cmd := click(m, 5, lineOf(t, m, "build"))
	if m.chosen == nil || m.chosen.Block != "shell" {
		t.Fatalf("chose %+v", m.chosen)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the picker did not close")
	}
}

func TestClickingAHeadingOrTheEmptyListDoesNothing(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	before := m.selected
	click(m, 5, lineOf(t, m, " Conversations "))
	click(m, 5, 28)
	if m.selected != before || m.chosen != nil {
		t.Fatalf("selected %d, chose %+v", m.selected, m.chosen)
	}
}

func TestClickingAFilterChoosesIt(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	line := strings.Split(view(m), "\n")[2]
	click(m, strings.Index(line, "Screens"), 2)
	if search.Filters[m.filter].Name != "Screens" {
		t.Fatalf("filter %s", search.Filters[m.filter].Name)
	}
}

// Each character of a tab's label, as drawn, must choose that tab.
func TestEveryLetterOfATabChoosesIt(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	line := strings.Split(view(m), "\n")[tabsRow]
	for i, f := range search.Filters {
		at := strings.Index(line, f.Name)
		for x := at; x < at+len(f.Name); x++ {
			click(m, x, tabsRow)
			if m.filter != i {
				t.Fatalf("%s at %d chose %s", f.Name, x, search.Filters[m.filter].Name)
			}
		}
	}
}

func TestDrawingChangesNothing(t *testing.T) {
	m := open(t, 120, 12, demoPanes(t)...)
	typeText(m, "panes")
	press(m, tea.KeyDown)
	before := fmt.Sprint(m.selected, m.offset, m.previewTop, m.lines)
	first := m.View()
	if second := m.View(); first != second || fmt.Sprint(m.selected, m.offset, m.previewTop, m.lines) != before {
		t.Fatal("View changed the picker")
	}
}

func TestTheWheelMovesTheList(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	wheel(m, 5, 10, tea.MouseButtonWheelDown)
	if m.selected != 1 {
		t.Fatalf("selected %d", m.selected)
	}
	wheel(m, 5, 10, tea.MouseButtonWheelUp)
	if m.selected != 0 {
		t.Fatalf("selected %d", m.selected)
	}
}

// previewText is what the right side of the picker shows.
func previewText(m *model) string {
	var out []string
	for _, line := range strings.Split(view(m), "\n") {
		if _, right, ok := strings.Cut(line, "│"); ok {
			out = append(out, right)
		}
	}
	return strings.Join(out, "\n")
}

func TestTheWheelScrollsTheMatchesInThePreview(t *testing.T) {
	var lines []conversations.Message
	for i := range 12 {
		lines = append(lines, said("user", fmt.Sprintf("word number %d", i)))
	}
	m := open(t, 120, 30, talking("a", "x", lines...))
	typeText(m, "word")
	if !strings.Contains(previewText(m), "word number 11") {
		t.Fatalf("got\n%s", view(m))
	}
	wheel(m, 100, 10, tea.MouseButtonWheelDown)
	if m.selected != 0 || strings.Contains(previewText(m), "word number 11") {
		t.Fatalf("selected %d:\n%s", m.selected, view(m))
	}
	wheel(m, 100, 10, tea.MouseButtonWheelUp)
	if !strings.Contains(previewText(m), "word number 11") {
		t.Fatalf("got\n%s", view(m))
	}
	for range 100 {
		wheel(m, 100, 10, tea.MouseButtonWheelDown)
	}
	if !strings.Contains(previewText(m), "word number 0") {
		t.Fatalf("scrolled past the end:\n%s", view(m))
	}
}

func TestALiveScreenDoesNotScroll(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	before := view(m)
	wheel(m, 100, 10, tea.MouseButtonWheelDown)
	press(m, tea.KeyShiftDown)
	if m.previewTop != 0 || view(m) != before {
		t.Fatalf("preview at %d:\n%s", m.previewTop, view(m))
	}
}

func TestAnEmptyQueryShowsWhatEachConversationSaidLast(t *testing.T) {
	p := talking("a", "x", said("user", "first"), said("assistant", "the last message"))
	p.Conversation.Active, p.Cwd = time.Now().Add(-5*time.Minute), "/code/x"
	m := open(t, 120, 30, p)
	out := view(m)
	for _, want := range []string{"Recent conversations", "the last message", "5m", "Active   5m ago", "Folder   /code/x"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

func TestTheBackgroundOfTheSelectedRowDoesNotStop(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	if out := m.View(); strings.Contains(out, ";m") {
		t.Fatalf("an empty parameter resets the colors: %q", out)
	}
}

// Fast typing can arrive as one message, which must not read as a key's name.
func TestTypedWordsAreNotKeys(t *testing.T) {
	m := open(t, 100, 30, demoPanes(t)...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
	if string(m.query) != "tab" || m.filter != 0 {
		t.Errorf("query %q, filter %d", string(m.query), m.filter)
	}
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	if m.chosen != nil || string(m.query) != "tabenter" {
		t.Errorf("chosen %v, query %q", m.chosen, string(m.query))
	}
}

// A tab's name is only a label: what it keeps must not depend on it.
func TestRenamingATabKeepsWhatItShows(t *testing.T) {
	name := search.Filters[2].Name
	search.Filters[2].Name = "Terminals"
	defer func() { search.Filters[2].Name = name }()
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	press(m, tea.KeyTab)
	press(m, tea.KeyTab)
	if rows := m.visible(); len(rows) != 1 || rows[0].Pane.Block != "named" {
		t.Fatalf("got %+v", rows)
	}
}

func TestALabelIsDrawnOnceBeforeALongWord(t *testing.T) {
	long := "/a/very/long/path/that/fills/a/whole/line/of/the/preview/and/more"
	m := open(t, 100, 30, talking("a", "x", said("user", long+" word")))
	typeText(m, "word")
	if got := strings.Count(previewText(m), "you:"); got != 1 {
		t.Fatalf("%d labels in\n%s", got, previewText(m))
	}
}

func TestAnErrorInThePreviewCannotMoveTheCursor(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "x"})
	m.capture = func(rex.Pane) (string, error) { return "", fmt.Errorf("failed\x1b[2J") }
	m.capturing = false
	send(m, tickMsg{})
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Fatalf("got %q", m.View())
	}
}

func TestAGrownWindowShowsTheWholeList(t *testing.T) {
	var panes []rex.Pane
	for i := range 20 {
		panes = append(panes, rex.Pane{Block: fmt.Sprint(i), Name: fmt.Sprintf("pane-%d", i)})
	}
	m := open(t, 120, 12, panes...)
	press(m, tea.KeyEnd)
	send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if out := view(m); !strings.Contains(out, "pane-0 ") {
		t.Fatalf("got\n%s", out)
	}
}

func TestARowOfAShellNamesItsSession(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Name: "shell", SessionLabel: "infra"}, rex.Pane{Block: "b", Name: "shell", SessionLabel: "logs"})
	out := view(m)
	if !strings.Contains(out, "infra · shell") || !strings.Contains(out, "logs · shell") {
		t.Fatalf("got\n%s", out)
	}
}

func TestAConversationKeepsItsOwnName(t *testing.T) {
	p := talking("a", "Fix the lock", said("user", "hello"))
	p.SessionLabel = "checkout"
	if got := p.Title(); got != "Fix the lock" {
		t.Fatalf("got %q", got)
	}
}

func TestPastedLinesStaySeparateWords(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("error: foo\nline two\r\nend"), Paste: true})
	if got := string(m.query); got != "error: foo line two end" {
		t.Fatalf("got %q", got)
	}
}

func TestWordsInDifferentMessagesExplainTheEmptyList(t *testing.T) {
	m := open(t, 120, 30, talking("a", "x", said("user", "one"), said("user", "two")))
	typeText(m, "one two")
	if out := view(m); !strings.Contains(out, "in one message or line") {
		t.Fatalf("got\n%s", out)
	}
}

func TestANarrowFooterDropsThePosition(t *testing.T) {
	m := open(t, 40, 30, demoPanes(t)...)
	lines := strings.Split(view(m), "\n")
	if footer := lines[len(lines)-1]; !strings.Contains(footer, "Close") || strings.Contains(footer, "/") {
		t.Fatalf("got %q", footer)
	}
}

func TestARowSaysWhatItsNumberCounts(t *testing.T) {
	m := open(t, 120, 30, talking("a", "x", said("user", "word one"), said("user", "word two")))
	typeText(m, "word")
	if out := view(m); !strings.Contains(out, "2 matches") {
		t.Fatalf("got\n%s", out)
	}
}

func TestARowOfAShellNamesItsWindow(t *testing.T) {
	p := rex.Pane{Block: "a", Name: "shell", SessionLabel: "svc", WindowLabel: "api"}
	m := open(t, 120, 30, p)
	out := view(m)
	if !strings.Contains(out, "svc · api · shell") || !strings.Contains(out, "Window   api") {
		t.Fatalf("got\n%s", out)
	}
}

func TestWrap(t *testing.T) {
	if got := strings.Join(wrap("one two three", 7), "|"); got != "one two|three" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(wrap("abcdefgh", 3), "|"); got != "abc|def|gh" {
		t.Fatalf("a long word: %q", got)
	}
	if got := strings.Join(wrap("ა ბ გ", 3), "|"); got != "ა ბ|გ" {
		t.Fatalf("multi-byte text: %q", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := strings.Join(lastLines("a\nb\nc\n", 2), "|"); got != "b|c" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(lastLines("a\n", 5), "|"); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestAge(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{time.Second, "now"}, {5 * time.Minute, "5m"}, {3 * time.Hour, "3h"}, {50 * time.Hour, "2d"}} {
		if got := age(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v: got %q", c.ago, got)
		}
	}
}

func actionNames(results []search.Result) []string {
	var names []string
	for _, r := range results {
		if r.Action != nil {
			names = append(names, r.Action.Name)
		}
	}
	return names
}

var someActions = []rex.Action{
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

func withActions(m *model, actions ...rex.Action) *model {
	send(m, actionsMsg{actions: actions})
	return m
}

func TestActionsComeAfterPanesAndBeforeConversations(t *testing.T) {
	m := withActions(open(t, 120, 30,
		rex.Pane{Block: "named", Session: "s", SessionLabel: "split-work", Name: "shell"},
		rex.Pane{Block: "said", Session: "s", SessionLabel: "x", Name: "agent", Conversation: &conversations.Conversation{}, Text: []textutil.Entry{{Text: "split the module", Lower: "split the module"}}},
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
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	if got := actionNames(m.visible()); got != nil {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, search.ActionsTab)
	if got := actionNames(m.visible()); len(got) != len(someActions) {
		t.Fatalf("Actions tab: %q", got)
	}
}

func TestOtherTabsHaveNoActions(t *testing.T) {
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	for tab := range search.Filters {
		if tab == search.ActionsTab {
			continue
		}
		showTab(m, tab)
		if tab != 0 {
			send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
		}
		if got := actionNames(m.visible()); tab != 0 && got != nil {
			t.Errorf("%s: %q", search.Filters[tab].Name, got)
		}
		m.query, m.cursor = nil, 0
		m.refresh(false)
	}
}

func TestTheAllTabLimitsActions(t *testing.T) {
	var many []rex.Action
	for _, name := range strings.Fields("a b c d e f g h") {
		many = append(many, rex.Action{Name: name, Title: "Session " + name})
	}
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), many...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("session")})
	if got := actionNames(m.visible()); len(got) != search.MaxActionsInAll {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, search.ActionsTab)
	if got := actionNames(m.visible()); len(got) != len(many) {
		t.Fatalf("Actions tab: %q", got)
	}
}

func TestEnterChoosesTheActionAndNoPane(t *testing.T) {
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("theme")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.chosen != nil || m.chosenAction == nil || m.chosenAction.Name != "client.theme.change" {
		t.Fatalf("pane %+v, action %+v", m.chosen, m.chosenAction)
	}
}

func TestTheSelectedActionStaysWhenTextArrives(t *testing.T) {
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	showTab(m, search.ActionsTab)
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	send(m, textMsg{panes: m.panes})
	if r, ok := m.current(); !ok || r.Action == nil || r.Action.Name != "pane.split.right" {
		t.Fatalf("selected %+v", r)
	}
}

func TestPaneSelectionIsNotMovedByActionRows(t *testing.T) {
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("split")})
	if m.selected != 0 {
		t.Fatalf("selected %d", m.selected)
	}
}

func TestAnActionRowDrawsAndNeedsNoScreen(t *testing.T) {
	asked := 0
	m := withActions(open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"}), someActions...)
	m.capture = func(rex.Pane) (string, error) { asked++; return "", nil }
	showTab(m, search.ActionsTab)
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
	m := open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"})
	send(m, actionsMsg{err: errString("rex actions: no such client")})
	showTab(m, search.ActionsTab)
	if view := m.View(); !strings.Contains(view, "no such client") {
		t.Fatalf("no error in:\n%s", view)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

var someSkills = skills.Installed{Agent: "Claude Code", Skills: []skills.Skill{
	{Name: "/grilling", Description: "Ask hard questions about a plan until it holds.", Source: "Personal", Path: "/home/.claude/skills/grilling/SKILL.md", Fields: []skills.Field{{Label: "version", Value: "1.2"}}},
	{Name: "/tools:review", Description: "Review the current diff for bugs.", Source: "Plugin tools", Path: "/plugins/tools/skills/review/SKILL.md"},
}}

func withSkills(m *model, installed skills.Installed) *model {
	send(m, skillsMsg{installed: installed})
	return m
}

func aShell(t *testing.T) *model {
	t.Helper()
	return open(t, 120, 30, rex.Pane{Block: "b", Session: "s", Name: "shell"})
}

func skillNames(rows []search.Result) string {
	var names []string
	for _, r := range rows {
		if r.Skill != nil {
			names = append(names, r.Skill.Name)
		}
	}
	return strings.Join(names, " ")
}

func TestAnEmptyQueryShowsSkillsOnlyInTheirTab(t *testing.T) {
	m := withSkills(aShell(t), someSkills)
	if got := skillNames(m.visible()); got != "" {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, search.SkillsTab)
	if got := skillNames(m.visible()); got != "/grilling /tools:review" {
		t.Fatalf("Skills tab: %q", got)
	}
	if view := m.View(); !strings.Contains(view, "2 skills") {
		t.Fatalf("no count in:\n%s", view)
	}
}

func TestOtherTabsHaveNoSkills(t *testing.T) {
	m := withSkills(aShell(t), someSkills)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	for tab := range search.Filters {
		showTab(m, tab)
		if got := skillNames(m.visible()); (tab == search.AllTab || tab == search.SkillsTab) != (got != "") {
			t.Errorf("%s: %q", search.Filters[tab].Name, got)
		}
	}
}

func TestTheAllTabLimitsSkills(t *testing.T) {
	var many []skills.Skill
	for _, name := range strings.Fields("a b c d e f g") {
		many = append(many, skills.Skill{Name: "/review-" + name})
	}
	m := withSkills(aShell(t), skills.Installed{Agent: "pi", Skills: many})
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("review")})
	if got := skillNames(m.visible()); len(strings.Fields(got)) != search.MaxSkillsInAll {
		t.Fatalf("All tab: %q", got)
	}
	showTab(m, search.SkillsTab)
	if got := skillNames(m.visible()); len(strings.Fields(got)) != len(many) {
		t.Fatalf("Skills tab: %q", got)
	}
}

func TestEnterChoosesTheSkillAndNothingElse(t *testing.T) {
	m := withActions(withSkills(aShell(t), someSkills), someActions...)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("grilling")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.chosen != nil || m.chosenAction != nil || m.chosenSkill == nil || m.chosenSkill.Name != "/grilling" {
		t.Fatalf("pane %+v, action %+v, skill %+v", m.chosen, m.chosenAction, m.chosenSkill)
	}
}

func TestTheSkillPreviewShowsItsDescriptionAndDetails(t *testing.T) {
	asked := 0
	m := withSkills(aShell(t), someSkills)
	m.capture = func(rex.Pane) (string, error) { asked++; return "", nil }
	showTab(m, search.SkillsTab)
	view := ansi.Strip(m.View())
	for _, want := range []string{"Ask hard questions about a plan", "Agent", "Claude Code", "Source", "Personal", "/home/.claude/skills/grilling/SKILL.md", "version", "1.2", "Put in prompt"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	if asked != 0 {
		t.Errorf("read a screen %d times for a skill", asked)
	}
}

func TestALongSkillPreviewWrapsAndScrolls(t *testing.T) {
	long := strings.Repeat("words that wrap ", 80) + "theend"
	m := withSkills(open(t, 100, 14, rex.Pane{Block: "b", Session: "s", Name: "shell"}), skills.Installed{Agent: "pi", Skills: []skills.Skill{{Name: "/long", Description: long}}})
	showTab(m, search.SkillsTab)
	if strings.Contains(ansi.Strip(m.View()), "theend") {
		t.Fatal("the end of the text fits, so this tests nothing")
	}
	for range 40 {
		send(m, tea.KeyMsg{Type: tea.KeyShiftDown})
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "theend") {
		t.Fatalf("no end of text in:\n%s", view)
	}
}

func TestTheSkillsTabSaysWhyItIsEmpty(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(*model)
		want string
	}{
		{"reading", func(*model) {}, "Reading skills"},
		{"no agent", func(m *model) { send(m, skillsMsg{}) }, "No agent runs in this pane"},
		{"none installed", func(m *model) { send(m, skillsMsg{installed: skills.Installed{Agent: "Codex"}}) }, "Codex has no skills"},
		{"error", func(m *model) { send(m, skillsMsg{err: errString("codex did not list its skills")}) }, "codex did not list its skills"},
	} {
		m := aShell(t)
		c.prep(m)
		showTab(m, search.SkillsTab)
		if view := ansi.Strip(m.View()); !strings.Contains(view, c.want) {
			t.Errorf("%s: no %q in:\n%s", c.name, c.want, view)
		}
	}
	m := withSkills(aShell(t), someSkills)
	showTab(m, search.SkillsTab)
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzzz")})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "No skill has these words") {
		t.Errorf("no match:\n%s", view)
	}
}

func TestSkillTextCannotMoveThePickersCursor(t *testing.T) {
	m := withSkills(aShell(t), skills.Installed{Agent: "pi", Skills: []skills.Skill{{Name: "/evil", Description: "bad\x1b[2Jtext", Path: "/x", Source: "Personal", Fields: []skills.Field{{Label: "k\x1b[H", Value: "v\x1b[H"}}}}})
	showTab(m, search.SkillsTab)
	if view := m.View(); strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x1b[H") {
		t.Fatalf("escape in:\n%q", view)
	}
}

func blockedAgent(block, kind string) rex.Pane {
	return rex.Pane{Block: block, Session: "s", Name: block, Conversation: &conversations.Conversation{}, Status: rex.Status{State: rex.StateBlocked, Kind: kind, Message: "Allow Bash?"}}
}

func TestABlockedAgentLooksDifferentFromAnIdleOne(t *testing.T) {
	m := open(t, 120, 30, blockedAgent("waits", "permission"), rex.Pane{Block: "rest", Session: "s", Name: "rest", Status: rex.Status{State: rex.StateIdle}})
	view := ansi.Strip(m.View())
	for _, want := range []string{"Needs you", "permission", "Allow Bash?"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	if r, _ := m.current(); r.Pane.Block != "waits" {
		t.Fatalf("selected %+v", r.Pane.Block)
	}
}

func TestTheNeedsYouTabKeepsOnlyWhatNeedsThePerson(t *testing.T) {
	m := open(t, 120, 30, blockedAgent("waits", "question"),
		rex.Pane{Block: "shell", Session: "s", Name: "shell"},
		rex.Pane{Block: "busy", Session: "s", Name: "busy", Status: rex.Status{State: rex.StateWorking}},
		rex.Pane{Block: "done", Session: "s", Name: "done", Status: rex.Status{State: rex.StateDone}})
	showTab(m, search.NeedsTab)
	var got []string
	for _, r := range m.visible() {
		got = append(got, r.Pane.Block)
	}
	if strings.Join(got, " ") != "waits done" {
		t.Fatalf("got %q", got)
	}
	m = open(t, 120, 30, rex.Pane{Block: "shell", Session: "s", Name: "shell"})
	showTab(m, search.NeedsTab)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Nothing needs you") {
		t.Fatalf("no empty state in:\n%s", view)
	}
}

func TestAStatusThatChangesMovesTheRow(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Session: "s", Name: "a"}, rex.Pane{Block: "b", Session: "s", Name: "b"})
	send(m, statusesMsg{map[string]rex.Status{"b": {State: rex.StateBlocked, Kind: "permission"}}})
	if rows := m.visible(); rows[0].Pane.Block != "b" || rows[0].Heading() != "Needs you" {
		t.Fatalf("got %+v", rows[0])
	}
	send(m, statusesMsg{map[string]rex.Status{"b": {State: rex.StateIdle}}})
	if rows := m.visible(); rows[0].Heading() == "Needs you" {
		t.Fatalf("got %+v", rows[0])
	}
}

func TestTextArrivingLaterKeepsTheNewestStatus(t *testing.T) {
	m := open(t, 120, 30, rex.Pane{Block: "a", Session: "s", Name: "a"})
	older := append([]rex.Pane(nil), m.panes...)
	send(m, statusesMsg{map[string]rex.Status{"a": {State: rex.StateBlocked}}})
	send(m, textMsg{panes: older})
	if m.panes[0].Status.State != rex.StateBlocked {
		t.Fatalf("got %+v", m.panes[0].Status)
	}
}

// answerable returns a picker on a pane, and what reaches it.
func answerable(t *testing.T, p rex.Pane) (*model, *[]rex.Answer) {
	t.Helper()
	m := open(t, 120, 30, p)
	var got []rex.Answer
	m.answer = func(_ rex.Pane, a rex.Answer) error { got = append(got, a); return nil }
	send(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	return m, &got
}

func TestANumberAnswersAMenuAndThePickerStaysOpen(t *testing.T) {
	m, got := answerable(t, blockedAgent("a", "permission"))
	if m.answering == nil {
		t.Fatal("not answering")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Press a number") || !strings.Contains(view, "Choose") {
		t.Fatalf("no menu hint in:\n%s", view)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	runCmd(m, cmd)
	if len(*got) != 1 || (*got)[0].Key != "2" || (*got)[0].Seen.Message != "Allow Bash?" {
		t.Fatalf("got %+v", *got)
	}
	if m.answering != nil || m.chosen != nil || !strings.Contains(ansi.Strip(m.View()), "Sent to a") {
		t.Fatalf("answering %v, chosen %v:\n%s", m.answering, m.chosen, ansi.Strip(m.View()))
	}
}

func TestAMenuTakesNoText(t *testing.T) {
	m, got := answerable(t, blockedAgent("a", "question"))
	for _, text := range []string{"y", "yes", "0", "22"} {
		send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	}
	if len(*got) != 0 || m.answering == nil {
		t.Fatalf("got %+v", *got)
	}
}

func TestEnterOnAMenuGoesToThePaneAndAnswersNothing(t *testing.T) {
	m, got := answerable(t, blockedAgent("a", "permission"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.chosen == nil || m.chosen.Block != "a" || len(*got) != 0 {
		t.Fatalf("chosen %+v, got %+v", m.chosen, *got)
	}
}

func TestEscLeavesAnAnswerAndKeepsThePickerOpen(t *testing.T) {
	m, got := answerable(t, blockedAgent("a", "permission"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.answering != nil || len(*got) != 0 {
		t.Fatalf("answering %+v, got %+v", m.answering, *got)
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("the picker closed")
		}
	}
}

func TestAPasswordPromptCannotBeAnswered(t *testing.T) {
	m, _ := answerable(t, blockedAgent("a", "auth"))
	if m.answering != nil || !strings.Contains(ansi.Strip(m.View()), "password") {
		t.Fatalf("answering %+v:\n%s", m.answering, ansi.Strip(m.View()))
	}
}

func TestAShellCannotBeAnswered(t *testing.T) {
	m, _ := answerable(t, rex.Pane{Block: "a", Session: "s", Name: "shell"})
	if m.answering != nil || !strings.Contains(ansi.Strip(m.View()), "Only an agent") {
		t.Fatalf("answering %+v:\n%s", m.answering, ansi.Strip(m.View()))
	}
}

func TestTextGoesToAnAgentAtRest(t *testing.T) {
	p := rex.Pane{Block: "a", Session: "s", Name: "a", Conversation: &conversations.Conversation{}, Status: rex.Status{State: rex.StateDone, Message: "ok"}}
	m, got := answerable(t, p)
	send(m, tea.KeyMsg{Type: tea.KeyEnter}) // Nothing to send yet.
	if len(*got) != 0 {
		t.Fatalf("got %+v", *got)
	}
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("next step")})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Answer a") || !strings.Contains(view, "next step") {
		t.Fatalf("no text in:\n%s", view)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	if len(*got) != 1 || (*got)[0].Text != "next step" || (*got)[0].Seen.State != rex.StateDone {
		t.Fatalf("got %+v", *got)
	}
}

func TestAFailedAnswerSaysWhy(t *testing.T) {
	m, _ := answerable(t, blockedAgent("a", "permission"))
	m.answer = func(rex.Pane, rex.Answer) error { return errString("the agent moved on") }
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	runCmd(m, cmd)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Not sent: the agent moved on") {
		t.Fatalf("no reason in:\n%s", view)
	}
}

func TestWhileAnsweringThePreviewShowsTheScreenEvenWithMatches(t *testing.T) {
	p := blockedAgent("a", "permission")
	p.Text = []textutil.Entry{{Text: "a word", Lower: "a word"}}
	m := open(t, 120, 30, p)
	typeText(m, "word")
	r, _ := m.current()
	if m.showsScreen(r) {
		t.Fatal("matches should show before an answer starts")
	}
	send(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	r, _ = m.current()
	if !m.showsScreen(r) {
		t.Fatal("no screen while answering")
	}
}

func someLayouts() []layouts.Layout {
	return []layouts.Layout{{Name: "morning", SavedAt: time.Now().Add(-time.Hour), Windows: []layouts.Window{{Root: layouts.Node{
		Direction: "horizontal", Before: &layouts.Node{Pane: &layouts.Pane{Title: "Fix the login", Cwd: "/code/app", Conversation: "abc"}},
		After: &layouts.Node{Pane: &layouts.Pane{Title: "zsh", Cwd: "/code/app"}},
	}}}}}
}

func withLayouts(m *model) *model {
	send(m, layoutsMsg{saved: someLayouts()})
	return m
}

func TestTheLayoutsTabListsSavedOnesAndTheRowThatSaves(t *testing.T) {
	m := withLayouts(aShell(t))
	showTab(m, search.LayoutsTab)
	rows := m.visible()
	if len(rows) != 2 || !rows[0].SaveLayout || rows[1].Layout.Name != "morning" {
		t.Fatalf("got %+v", rows)
	}
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	view := ansi.Strip(m.View())
	for _, want := range []string{"morning", "2 panes", "Fix the login  resumes", "Restore", "Delete"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
}

func TestAnEmptyQueryShowsLayoutsOnlyInTheirTab(t *testing.T) {
	m := withLayouts(aShell(t))
	for _, r := range m.visible() {
		if !r.IsPane() {
			t.Fatalf("got %+v", r)
		}
	}
	typeText(m, "morning")
	found := false
	for _, r := range m.visible() {
		found = found || r.Layout != nil
	}
	if !found {
		t.Fatal("no layout in the All tab")
	}
}

func TestSavingAsksForANameAndSavesThePanes(t *testing.T) {
	m := withLayouts(aShell(t))
	var name string
	var panes int
	m.saveLayout = func(n string, p []rex.Pane) error { name, panes = n, len(p); return nil }
	showTab(m, search.LayoutsTab)
	send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.naming {
		t.Fatal("not naming")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Name this layout") || !strings.Contains(view, "work") {
		t.Fatalf("no prompt in:\n%s", view)
	}
	for range 4 {
		send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	send(m, tea.KeyMsg{Type: tea.KeyEnter}) // An empty name saves nothing.
	if name != "" || !m.naming {
		t.Fatalf("saved %q", name)
	}
	send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("evening")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	if name != "evening" || panes != 1 || m.naming {
		t.Fatalf("saved %q with %d panes", name, panes)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Saved evening") {
		t.Fatalf("no notice in:\n%s", view)
	}
}

func TestSavingANameThatExistsSaysItReplacedIt(t *testing.T) {
	m := withLayouts(aShell(t))
	m.saveLayout = func(string, []rex.Pane) error { return nil }
	showTab(m, search.LayoutsTab)
	send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.answerText, m.answerCursor = []rune("morning"), 7
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Replaced morning") {
		t.Fatalf("no notice in:\n%s", view)
	}
}

func TestEnterOnALayoutChoosesIt(t *testing.T) {
	m := withLayouts(aShell(t))
	showTab(m, search.LayoutsTab)
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.chosenLayout == nil || m.chosenLayout.Name != "morning" || m.chosen != nil {
		t.Fatalf("layout %+v, pane %+v", m.chosenLayout, m.chosen)
	}
}

func TestDeletingALayoutNeedsTwoPresses(t *testing.T) {
	m := withLayouts(aShell(t))
	var gone []string
	m.deleteLayout = func(n string) error { gone = append(gone, n); return nil }
	showTab(m, search.LayoutsTab)
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if len(gone) != 0 || !strings.Contains(ansi.Strip(m.View()), "Press ctrl+x again") {
		t.Fatalf("gone %v:\n%s", gone, ansi.Strip(m.View()))
	}
	send(m, tea.KeyMsg{Type: tea.KeyUp}) // Any other key cancels it.
	send(m, tea.KeyMsg{Type: tea.KeyDown})
	send(m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if len(gone) != 0 {
		t.Fatalf("gone %v", gone)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	runCmd(m, cmd)
	if len(gone) != 1 || gone[0] != "morning" {
		t.Fatalf("gone %v", gone)
	}
}

func TestLayoutsThatCannotBeReadAreReported(t *testing.T) {
	m := aShell(t)
	send(m, layoutsMsg{err: errString("bad.json: broken")})
	showTab(m, search.LayoutsTab)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "bad.json") {
		t.Fatalf("no error in:\n%s", view)
	}
}
