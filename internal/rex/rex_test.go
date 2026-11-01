package rex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/tornikegomareli/rexlauncher/internal/testutil"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// fakeActions puts a rex on PATH that lists actions: the server's, and with -C a client's.
func fakeActions(t *testing.T, server, client string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
shift # --autostart=false
if [ "$1" = -C ]; then cat <<'END'
` + client + `
END
else cat <<'END'
` + server + `
END
fi
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestListPanesKeepsTerminalsAndLeavesOutThePicker(t *testing.T) {
	testutil.FakeRex(t, testutil.TwoSessions())
	panes, err := ListPanes("self")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range panes {
		got = append(got, p.Block)
	}
	if strings.Join(got, " ") != "b1 b2 b3" {
		t.Fatalf("got %v", got)
	}
}

func TestAPaneInThePickersWindowIsHere(t *testing.T) {
	testutil.FakeRex(t, testutil.TwoSessions())
	panes, err := ListPanes("self")
	if err != nil {
		t.Fatal(err)
	}
	here := map[string]bool{}
	for _, p := range panes {
		here[p.Block] = p.Here
	}
	if !here["b1"] || here["b2"] || here["b3"] {
		t.Fatalf("got %v", here)
	}
}

func TestListPanesNamesEachPane(t *testing.T) {
	testutil.FakeRex(t, testutil.TwoSessions())
	panes, err := ListPanes("self")
	if err != nil {
		t.Fatal(err)
	}
	first, second := panes[0], panes[1]
	if first.Name != "a title" || first.Cwd != "/code/my app" || first.SessionLabel != "one" || first.WindowLabel != "main" {
		t.Errorf("first: %+v", first)
	}
	// Without a title, the pane keeps the label of its block.
	if second.Name != "second" || second.Cwd != "" || second.Session != "s1" || second.Window != "w2" {
		t.Errorf("second: %+v", second)
	}
}

func TestCheckServerRefusesAnotherServersSessions(t *testing.T) {
	testutil.FakeRex(t, testutil.TwoSessions())
	if err := CheckServer("s2"); err != nil {
		t.Errorf("its own session: %v", err)
	}
	if err := CheckServer("elsewhere"); err == nil || !strings.Contains(err.Error(), "REX_SERVER") {
		t.Errorf("another session: %v", err)
	}
}

func TestGoToFocusesThenShowsThroughTheClient(t *testing.T) {
	calls := testutil.FakeRex(t, nil)
	if err := GoTo("s1", "w1", "b1", "client-1"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || !strings.Contains(got[0], "session.focus_block") || !strings.Contains(got[1], "-C client-1 do session.select session_id=s1 window_id=w1") {
		t.Fatalf("got %q", got)
	}
}

func TestGoToWithoutAClientLetsRexChoose(t *testing.T) {
	calls := testutil.FakeRex(t, nil)
	if err := GoTo("s1", "w1", "b1", ""); err != nil {
		t.Fatal(err)
	}
	if got := calls(); len(got) != 2 || strings.Contains(got[1], "-C") {
		t.Fatalf("got %q", got)
	}
}

// "rex do rexlauncher" has no client, or the client that opened the picker is gone.
func TestGoToFallsBackWhenTheClientIsGone(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{"fail-client": true})
	if err := GoTo("s1", "w1", "b1", "gone"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 3 || !strings.Contains(got[1], "-C gone") || strings.Contains(got[2], "-C") {
		t.Fatalf("got %q", got)
	}
}

func TestASessionThatClosesWhileListingIsLeftOut(t *testing.T) {
	answers := testutil.TwoSessions()
	answers["session.list"] = testutil.Block{"sessions": []testutil.Block{{"session_id": "s2"}, {"session_id": "gone"}}}
	answers["session.view.gone"] = "FAIL"
	testutil.FakeRex(t, answers)
	panes, err := ListPanes("")
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 || panes[0].Block != "b3" {
		t.Fatalf("got %+v", panes)
	}
}

func TestARexCommandThatHangsEnds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	before := rexTimeout
	rexTimeout = 100 * time.Millisecond
	defer func() { rexTimeout = before }()
	start := time.Now()
	if _, err := Run("capture"); err == nil {
		t.Fatal("no error")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
}

func TestAPaneWhoseShellReportsNoFolderStillHasOne(t *testing.T) {
	answers := testutil.TwoSessions()
	answers["block.method.b3.process"] = testutil.Block{"foreground": testutil.Block{"cwd": "/code/infra"}}
	testutil.FakeRex(t, answers)
	panes, err := ListPanes("self")
	if err != nil {
		t.Fatal(err)
	}
	if got := panes[2]; got.Block != "b3" || got.Cwd != "/code/infra" {
		t.Fatalf("got %+v", got)
	}
	if got := panes[0].Cwd; got != "/code/my app" {
		t.Fatalf("the reported folder comes first: %q", got)
	}
}

func TestGoingToAClosedPaneSaysSoPlainly(t *testing.T) {
	testutil.FakeRex(t, map[string]any{"session.focus_block": "FAIL"})
	err := GoTo("s1", "w1", "b1", "")
	if err == nil || err.Error() != "that pane has closed" {
		t.Fatalf("got %v", err)
	}
}

func TestAWindowNameRexMadeUpIsLeftOut(t *testing.T) {
	for label, want := range map[string]string{"Window 6091e3fd": "", "api": "api", "Window for logs": "Window for logs"} {
		if got := windowName(label); got != want {
			t.Errorf("%q: got %q", label, got)
		}
	}
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
	actions, err := LoadActions("client-1")
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

func TestRunActionAppliesToThePaneBehindThePicker(t *testing.T) {
	calls := testutil.FakeRex(t, nil)
	if err := RunAction("pane.split.down", "s1", "b1", "client-1"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || !strings.Contains(got[0], "session.focus_block") || !strings.Contains(got[1], "-C client-1 do pane.split.down -s s1 -b b1") {
		t.Fatalf("got %q", got)
	}
}

func TestRunActionWithoutAPaneLetsRexChoose(t *testing.T) {
	calls := testutil.FakeRex(t, nil)
	if err := RunAction("client.sidebar.toggle", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 1 || !strings.HasSuffix(got[0], "do client.sidebar.toggle") {
		t.Fatalf("got %q", got)
	}
}

func TestScreenText(t *testing.T) {
	got := ScreenText("\x1b[31mone\ntwo\x1b[0m\nthree\r\n")
	want := "\x1b[31mone\x1b[0m\n\x1b[31mtwo\x1b[0m\x1b[0m\nthree\x1b[0m\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestScreenTextDropsEverySequenceItDoesNotKeep(t *testing.T) {
	for _, c := range []struct{ name, input, want string }{
		{"a character set", "\x1b(Bplain\n", "plain"},
		{"a private mode", "\x1b[>4;2mplain\n", "plain"},
		{"a title ended by ST", "\x1b]0;title\x1b\\plain\n", "plain"},
		{"a link", "\x1b]8;;http://x\x1b\\text\x1b]8;;\x1b\\\n", "text"},
	} {
		if got := ansi.Strip(ScreenText(c.input)); strings.TrimRight(got, "\n") != c.want {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
	if got := ScreenText("one\x1b\ntwo\n"); !strings.Contains(got, "one") || strings.Count(got, "\n") != 2 {
		t.Errorf("an escape at the end of a line joined two lines: %q", got)
	}
}

// Lines that only use partial resets never end a color. Reading them must stay cheap,
// because the preview reads the screen again every 600 ms.
func TestScreenTextCarriesABoundedAmountOfColor(t *testing.T) {
	got := ScreenText(strings.Repeat("\x1b[39mline\x1b[22m\n", 8000))
	if len(got) > 8000*(maxCarriedColor+64) {
		t.Fatalf("%d bytes from 8000 short lines", len(got))
	}
}

func TestAScreenCannotAskForAnyAmountOfMemory(t *testing.T) {
	for _, count := range []string{"200000000", "4611686018427387904"} {
		if got := ScreenText("\x1b[" + count + "Cx\n"); len(got) > 2*maxCursorMove {
			t.Errorf("%s: %d bytes", count, len(got))
		}
	}
}

func TestAVeryLongColorIsNotPrintedAsText(t *testing.T) {
	got := ScreenText("\x1b[" + strings.Repeat("1;", 400) + "1mone\ntwo\n")
	if strings.Contains(ansi.Strip(got), ";") {
		t.Fatalf("got %q", ansi.Strip(got))
	}
}

func TestAJoinedEmojiCannotChangeTheWidthOfALine(t *testing.T) {
	for _, text := range []string{"👨‍👩‍👧 family", "🏳️‍🌈 flag"} {
		if got := textutil.Clean(text); strings.ContainsAny(got, "‍️") {
			t.Errorf("%q kept a joiner: %q", text, got)
		}
		if got := ScreenText(text + "\n"); strings.ContainsAny(got, "‍️") {
			t.Errorf("%q kept a joiner on a screen: %q", text, got)
		}
	}
}

func TestPathFromFileURL(t *testing.T) {
	if got := pathFromFileURL("file://host.local/Users/me/My%20Code"); got != "/Users/me/My Code" {
		t.Fatalf("got %q", got)
	}
	if got := pathFromFileURL(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestForegroundNamesTheProgramInAPane(t *testing.T) {
	testutil.FakeRex(t, map[string]any{
		"block.method.b1.process": testutil.Block{"foreground": testutil.Block{
			"name": "2.1.316", "path": "/v/2.1.316", "invoked_path": "/bin/claude", "argv0": "claude", "cwd": "/code",
		}},
	})
	got, err := Foreground("s1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Argv0 != "claude" || got.Path != "/bin/claude" || got.Cwd != "/code" {
		t.Fatalf("%+v", got)
	}
}

func TestForegroundWithoutAProcessIsEmpty(t *testing.T) {
	testutil.FakeRex(t, map[string]any{"block.method.b1.process": testutil.Block{"last_exit": 0}})
	got, err := Foreground("s1", "b1")
	if err != nil || got != (Program{}) {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestInsertTypesIntoThePaneBehindThePickerWithoutEnter(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{})
	if err := Insert("s1", "b1", "/review "); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 {
		t.Fatalf("calls %q", got)
	}
	if !strings.Contains(got[0], "session.focus_block") {
		t.Errorf("first call %q", got[0])
	}
	// The log of calls loses the last space. The end-to-end test sees it.
	if got[1] != "--autostart=false send -s s1 -b b1 /review" {
		t.Errorf("second call %q", got[1])
	}
}

func statusOf(t *testing.T, records ...testutil.Block) Status {
	t.Helper()
	testutil.FakeRex(t, map[string]any{"block.method.b1.program_status": testutil.Block{"records": records}})
	got, err := ReadStatus("s1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReadStatusKeepsWhatAnAgentNeeds(t *testing.T) {
	got := statusOf(t, testutil.Block{"state": "blocked", "kind": "permission", "app": "claude-code", "msg": "Allow Bash?", "progress": 40, "updated_at": "2026-11-01T10:00:00Z"})
	if got.State != StateBlocked || got.Kind != "permission" || got.Message != "Allow Bash?" || got.App != "claude-code" || got.Progress == nil || *got.Progress != 40 {
		t.Fatalf("%+v", got)
	}
	if got.At.IsZero() {
		t.Fatal("no time")
	}
}

func TestReadStatusPicksTheMostUrgentRecord(t *testing.T) {
	got := statusOf(t,
		testutil.Block{"state": "working", "updated_at": "2026-11-01T12:00:00Z"},
		testutil.Block{"state": "blocked", "kind": "question", "updated_at": "2026-11-01T10:00:00Z"},
		testutil.Block{"state": "done", "updated_at": "2026-11-01T13:00:00Z"},
	)
	if got.State != StateBlocked {
		t.Fatalf("%+v", got)
	}
}

func TestReadStatusPicksTheNewestOfEquallyUrgentRecords(t *testing.T) {
	got := statusOf(t,
		testutil.Block{"state": "done", "msg": "old", "updated_at": "2026-11-01T10:00:00Z"},
		testutil.Block{"state": "done", "msg": "new", "updated_at": "2026-11-01T12:00:00Z"},
	)
	if got.Message != "new" {
		t.Fatalf("%+v", got)
	}
}

func TestAnUnknownStateCountsAsIdleAndAnUnknownKindAsNone(t *testing.T) {
	got := statusOf(t, testutil.Block{"state": "dancing", "kind": "riddle"})
	if got.State != StateIdle || got.Kind != "" {
		t.Fatalf("%+v", got)
	}
}

func TestAPaneWithNoRecordsHasNoStatus(t *testing.T) {
	if got := statusOf(t); got.State != "" {
		t.Fatalf("%+v", got)
	}
}

func TestSendAnswerPastesThenPressesEnter(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{})
	if err := SendAnswer("s1", "b1", "use postgres"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || !strings.Contains(got[0], `"type":"paste"`) || !strings.Contains(got[0], `use postgres`) {
		t.Fatalf("calls %q", got)
	}
	if !strings.HasSuffix(got[1], "send-key -s s1 -b b1 enter") {
		t.Fatalf("second call %q", got[1])
	}
}

func TestPressKeySendsOneKeyAndNoEnter(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{})
	if err := PressKey("s1", "b1", "2"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 1 || !strings.HasSuffix(got[0], "send-key -s s1 -b b1 2") {
		t.Fatalf("calls %q", got)
	}
}

func answerSeen(t *testing.T, now testutil.Block, answer Answer) (error, []string) {
	t.Helper()
	calls := testutil.FakeRex(t, map[string]any{"block.method.b1.program_status": testutil.Block{"records": []testutil.Block{now}}})
	err := Deliver("s1", "b1", answer)
	return err, calls()
}

func sent(calls []string, what string) bool {
	for _, c := range calls {
		if strings.Contains(c, what) {
			return true
		}
	}
	return false
}

func TestDeliverPressesTheKeyIfTheAgentStillWaitsForThatAnswer(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2026-11-01T10:00:00Z")
	err, calls := answerSeen(t, testutil.Block{"state": "blocked", "kind": "permission", "msg": "Allow?", "updated_at": "2026-11-01T10:00:00Z"},
		Answer{Key: "1", Seen: Status{State: StateBlocked, Kind: "permission", Message: "Allow?", At: at}})
	if err != nil || !sent(calls, "send-key -s s1 -b b1 1") {
		t.Fatalf("%v %q", err, calls)
	}
}

func TestDeliverRefusesIfTheAgentMovedOn(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2026-11-01T10:00:00Z")
	seen := Status{State: StateBlocked, Kind: "permission", Message: "Allow Read?", At: at}
	for name, now := range map[string]testutil.Block{
		"another question":     {"state": "blocked", "kind": "permission", "msg": "Allow rm?", "updated_at": "2026-11-01T10:00:00Z"},
		"the same words later": {"state": "blocked", "kind": "permission", "msg": "Allow Read?", "updated_at": "2026-11-01T10:00:09Z"},
		"not blocked now":      {"state": "working", "updated_at": "2026-11-01T10:00:00Z"},
	} {
		err, calls := answerSeen(t, now, Answer{Key: "1", Seen: seen})
		if err == nil || sent(calls, "send-key") {
			t.Errorf("%s: %v %q", name, err, calls)
		}
	}
}

// Text followed by Enter would pick the default option of a menu.
func TestDeliverNeverSendsTextToABlockedAgent(t *testing.T) {
	err, calls := answerSeen(t, testutil.Block{"state": "blocked", "kind": "question", "msg": "Which?"},
		Answer{Text: "hello", Seen: Status{State: StateBlocked, Kind: "question", Message: "Which?"}})
	if err == nil || sent(calls, "paste") || sent(calls, "send-key") {
		t.Fatalf("%v %q", err, calls)
	}
}

func TestDeliverRefusesAPasswordPrompt(t *testing.T) {
	err, calls := answerSeen(t, testutil.Block{"state": "blocked", "kind": "auth"},
		Answer{Key: "1", Seen: Status{State: StateBlocked, Kind: "auth"}})
	if err == nil || sent(calls, "send-key") {
		t.Fatalf("%v %q", err, calls)
	}
}

func TestDeliverSendsTextToAnAgentAtRest(t *testing.T) {
	err, calls := answerSeen(t, testutil.Block{"state": "done", "msg": "ok"},
		Answer{Text: "next step", Seen: Status{State: StateDone, Message: "ok"}})
	if err != nil || !sent(calls, "next step") || !sent(calls, "send-key -s s1 -b b1 enter") {
		t.Fatalf("%v %q", err, calls)
	}
}
