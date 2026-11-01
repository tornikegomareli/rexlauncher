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
	for _, f := range filters {
		filter := f.name
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
	if filters[m.filter].name != "Screens" {
		t.Fatalf("filter %s", filters[m.filter].name)
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

func TestLettersAreMarkedInTheNameAsDrawn(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "a", Name: "\x07\x07tilebar", SessionLabel: "s"})
	typeText(m, "tlbr")
	out := m.View()
	for _, want := range []string{styleHit + "mt", styleHit + "ml", styleHit + "mb", styleHit + "mr"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in %q", want, out)
		}
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
	if filters[m.filter].name != "Screens" {
		t.Fatalf("filter %s", filters[m.filter].name)
	}
}

// Each character of a tab's label, as drawn, must choose that tab.
func TestEveryLetterOfATabChoosesIt(t *testing.T) {
	m := open(t, 120, 30, demoPanes(t)...)
	line := strings.Split(view(m), "\n")[tabsRow]
	for i, f := range filters {
		at := strings.Index(line, f.name)
		for x := at; x < at+len(f.name); x++ {
			click(m, x, tabsRow)
			if m.filter != i {
				t.Fatalf("%s at %d chose %s", f.name, x, filters[m.filter].name)
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
	var lines []string
	for i := range 12 {
		lines = append(lines, said("user", fmt.Sprintf("word number %d", i)))
	}
	m := open(t, 120, 30, talking(t, "a", "x", lines...))
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
	p := talking(t, "a", "x", said("user", "first"), said("assistant", "the last message"))
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
	name := filters[1].name
	filters[1].name = "Terminals"
	defer func() { filters[1].name = name }()
	m := open(t, 120, 30, demoPanes(t)...)
	typeText(m, "panes")
	press(m, tea.KeyTab)
	if rows := m.visible(); len(rows) != 1 || rows[0].Pane.Block != "named" {
		t.Fatalf("got %+v", rows)
	}
}

func TestScreenTextDropsEverySequenceItDoesNotKeep(t *testing.T) {
	for _, c := range []struct{ name, input, want string }{
		{"a character set", "\x1b(Bplain\n", "plain"},
		{"a private mode", "\x1b[>4;2mplain\n", "plain"},
		{"a title ended by ST", "\x1b]0;title\x1b\\plain\n", "plain"},
		{"a link", "\x1b]8;;http://x\x1b\\text\x1b]8;;\x1b\\\n", "text"},
	} {
		if got := ansi.Strip(screenText(c.input)); strings.TrimRight(got, "\n") != c.want {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
	if got := screenText("one\x1b\ntwo\n"); !strings.Contains(got, "one") || strings.Count(got, "\n") != 2 {
		t.Errorf("an escape at the end of a line joined two lines: %q", got)
	}
}

// Lines that only use partial resets never end a color. Reading them must stay cheap,
// because the preview reads the screen again every 600 ms.
func TestScreenTextCarriesABoundedAmountOfColor(t *testing.T) {
	got := screenText(strings.Repeat("\x1b[39mline\x1b[22m\n", 8000))
	if len(got) > 8000*(maxCarriedColor+64) {
		t.Fatalf("%d bytes from 8000 short lines", len(got))
	}
}

func TestALabelIsDrawnOnceBeforeALongWord(t *testing.T) {
	long := "/a/very/long/path/that/fills/a/whole/line/of/the/preview/and/more"
	m := open(t, 100, 30, talking(t, "a", "x", said("user", long+" word")))
	typeText(m, "word")
	if got := strings.Count(previewText(m), "you:"); got != 1 {
		t.Fatalf("%d labels in\n%s", got, previewText(m))
	}
}

func TestAScreenCannotAskForAnyAmountOfMemory(t *testing.T) {
	for _, count := range []string{"200000000", "4611686018427387904"} {
		if got := screenText("\x1b[" + count + "Cx\n"); len(got) > 2*maxCursorMove {
			t.Errorf("%s: %d bytes", count, len(got))
		}
	}
}

func TestAVeryLongColorIsNotPrintedAsText(t *testing.T) {
	got := screenText("\x1b[" + strings.Repeat("1;", 400) + "1mone\ntwo\n")
	if strings.Contains(ansi.Strip(got), ";") {
		t.Fatalf("got %q", ansi.Strip(got))
	}
}

func TestAnErrorInThePreviewCannotMoveTheCursor(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "a", Name: "x"})
	m.capture = func(pane) (string, error) { return "", fmt.Errorf("failed\x1b[2J") }
	m.capturing = false
	send(m, tickMsg{})
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Fatalf("got %q", m.View())
	}
}

func TestAGrownWindowShowsTheWholeList(t *testing.T) {
	var panes []pane
	for i := range 20 {
		panes = append(panes, pane{Block: fmt.Sprint(i), Name: fmt.Sprintf("pane-%d", i)})
	}
	m := open(t, 120, 12, panes...)
	press(m, tea.KeyEnd)
	send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if out := view(m); !strings.Contains(out, "pane-0 ") {
		t.Fatalf("got\n%s", out)
	}
}

func TestARowOfAShellNamesItsSession(t *testing.T) {
	m := open(t, 120, 30, pane{Block: "a", Name: "shell", SessionLabel: "infra"}, pane{Block: "b", Name: "shell", SessionLabel: "logs"})
	out := view(m)
	if !strings.Contains(out, "infra · shell") || !strings.Contains(out, "logs · shell") {
		t.Fatalf("got\n%s", out)
	}
}

func TestAConversationKeepsItsOwnName(t *testing.T) {
	p := talking(t, "a", "Fix the lock", said("user", "hello"))
	p.SessionLabel = "checkout"
	if got := p.title(); got != "Fix the lock" {
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
