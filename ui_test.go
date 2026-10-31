package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// open returns a picker with its panes and text loaded.
func open(t *testing.T, width, height int, panes ...pane) *model {
	t.Helper()
	m := &model{
		loadText: func(p []pane) []pane { return p },
		capture: func(p pane) (string, error) {
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
	case panesMsg, textMsg, captureMsg:
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

func demoPanes(t *testing.T) []pane {
	t.Helper()
	first := talking(t, "talk", "drooop-demos", said("user", "our panes project"), said("assistant", "the panes draw well"))
	first.SessionLabel, first.Cwd = "work", "/code/drooop"
	second := showing("shell", "build", "split panes in the log")
	second.SessionLabel, second.Cwd = "ops", "/code/ops"
	third := pane{Block: "named", Name: "panes-app", SessionLabel: "apps", Cwd: "/code/app"}
	return []pane{first, second, third}
}

func TestEveryLineFitsTheWindow(t *testing.T) {
	long := showing("b", strings.Repeat("name", 40), strings.Repeat("a word ", 200))
	long.Cwd = "/" + strings.Repeat("folder/", 40)
	for _, width := range []int{20, 40, 71, 72, 100, 200} {
		for _, height := range []int{3, 6, 30} {
			m := open(t, width, height, long, pane{Block: "c", Name: "გამარჯობა მსოფლიო"})
			typeText(m, "word")
			lines := strings.Split(view(m), "\n")
			if len(lines) > max(height, 6) {
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
	for _, filter := range filters {
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
	if filters[m.filter] != "Screens" {
		t.Fatalf("filter %s", filters[m.filter])
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
	var panes []pane
	for i := range 40 {
		panes = append(panes, pane{Block: fmt.Sprint(i), Name: fmt.Sprintf("pane-%d", i)})
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
	panes := []pane{{Block: "a", Name: "one"}, {Block: "b", Name: "two"}}
	m := &model{
		loadText: func(p []pane) []pane { return nil },
		capture:  func(p pane) (string, error) { return "", nil },
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

func TestALetterLeftOutMarksTheOthers(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "a", Name: "tilebar", SessionLabel: "s"})
	typeText(m, "tlbr")
	out := m.View()
	for _, want := range []string{styleHit + "mt", styleHit + "ml", styleHit + "mb", styleHit + "mr"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in %q", want, out)
		}
	}
}

func TestPreviewKeepsTheColorsOfTheScreenAndNothingElse(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "a", Name: "x"})
	send(m, captureMsg{block: "a", text: screenText("\x1b[31mred\x1b[0m \x1b[3Cgap\x1b[2J\x1b]0;title\x07\n")})
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
	m := open(t, 120, 30, pane{Block: "a", Name: "x"})
	send(m, captureMsg{block: "a", text: screenText(long)})
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "xxx") && !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("no reset at the end of %q", line)
		}
		if got := ansi.StringWidth(line); got > 120 {
			t.Fatalf("a line of %d", got)
		}
	}
}

func TestScreenText(t *testing.T) {
	got := screenText("\x1b[31mone\ntwo\x1b[0m\nthree\r\n")
	want := "\x1b[31mone\x1b[0m\n\x1b[31mtwo\x1b[0m\x1b[0m\nthree\x1b[0m\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestThePreviewReadsTheScreenAgainOnTheTimer(t *testing.T) {
	reads := 0
	m := &model{
		loadText: func(p []pane) []pane { return p },
		capture: func(p pane) (string, error) {
			reads++
			return fmt.Sprintf("read %d", reads), nil
		},
	}
	send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	send(m, panesMsg{panes: []pane{{Block: "a", Name: "x"}}})
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
	if filters[m.filter] != "Screens" {
		t.Fatalf("filter %s", filters[m.filter])
	}
}

func TestTheWheelMovesTheListOrThePreview(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	wheel(m, 5, 10, tea.MouseButtonWheelDown)
	if m.selected != 1 {
		t.Fatalf("selected %d", m.selected)
	}
	wheel(m, 100, 10, tea.MouseButtonWheelDown)
	if m.selected != 1 || m.previewTop != 1 {
		t.Fatalf("selected %d, preview at %d", m.selected, m.previewTop)
	}
	wheel(m, 100, 10, tea.MouseButtonWheelUp)
	wheel(m, 5, 10, tea.MouseButtonWheelUp)
	if m.selected != 0 || m.previewTop != 0 {
		t.Fatalf("selected %d, preview at %d", m.selected, m.previewTop)
	}
}

func TestAnEmptyQueryShowsWhatEachConversationSaidLast(t *testing.T) {
	p := talking(t, "a", "x", said("user", "first"), said("assistant", "the last message"))
	p.Active, p.Cwd = time.Now().Add(-5*time.Minute), "/code/x"
	m := open(t, 120, 30, p)
	out := view(m)
	for _, want := range []string{"Recent conversations", "the last message", "5m", "Active   5m ago", "Folder   /code/x"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
