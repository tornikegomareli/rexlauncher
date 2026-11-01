//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
)

const (
	stayAliveVariable = "REXLAUNCHER_TEST_STAY_ALIVE"
	// promptVariable makes this program act as an agent: it says each line it gets.
	promptVariable = "REXLAUNCHER_TEST_PROMPT"
	// statusVariable is the status that program reports, in the Program Status Protocol.
	statusVariable = "REXLAUNCHER_TEST_STATUS"
)

func userLine(text string) string {
	return `{"type":"user","timestamp":"t","message":{"content":"` + text + `"}}` + "\n"
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// kinds names each row by its section, and its pane.
func kinds(results []search.Result) []string {
	names := map[string]string{"Recent conversations": "recent", "Panes": "pane", "Conversations": "said", "Screens": "screen"}
	var out []string
	for _, r := range results {
		out = append(out, names[r.Heading()]+":"+r.Pane.Block)
	}
	return out
}

func TestMain(m *testing.M) {
	if os.Getenv(promptVariable) != "" {
		if status := os.Getenv(statusVariable); status != "" {
			fmt.Print("\x1b]7501;" + status + "\x1b\\")
		}
		lines := bufio.NewScanner(os.Stdin)
		for lines.Scan() {
			fmt.Printf("sent [%s]\n", lines.Text())
		}
		return
	}
	if os.Getenv(stayAliveVariable) != "" {
		time.Sleep(2 * time.Minute)
		return
	}
	os.Exit(m.Run())
}

// server starts a Rex server of its own, and a rex command on PATH that can only
// reach it. Nothing here touches the Rex the person is using.
func server(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("rex")
	if err != nil {
		t.Skip("Rex is not installed")
	}
	root, err := os.MkdirTemp("", "rj") // Short, because a socket path has a limit.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	socket := "unix://" + filepath.Join(root, "s.sock")
	init := filepath.Join(root, "init.lua")
	os.WriteFile(init, nil, 0o644)

	cmd := exec.Command(real, "server", "run", "--listen", socket, "--state-dir", filepath.Join(root, "state"), "--config", init, "--restore=false")
	cmd.Env = withoutRex(os.Environ())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	})

	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	wrapper := fmt.Sprintf("#!/bin/sh\nunset REX_SERVER REX_SESSION REX_BLOCK REX_CLIENT\nexec %q -S %q \"$@\"\n", real, socket)
	if err := os.WriteFile(filepath.Join(bin, "rex"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := rex.Run("ls"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Rex server did not answer")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var keymap struct {
		Config struct {
			Path string `json:"path"`
		} `json:"config"`
	}
	out, err := rex.Run("keymap", "--json")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(out, &keymap)
	if got, _ := filepath.EvalSymlinks(keymap.Config.Path); !strings.HasSuffix(got, "/init.lua") || !strings.Contains(got, filepath.Base(root)) {
		t.Fatalf("talking to the wrong server: its config is %q", keymap.Config.Path)
	}
}

func withoutRex(env []string) []string {
	var out []string
	for _, entry := range env {
		if !strings.HasPrefix(entry, "REX_") {
			out = append(out, entry)
		}
	}
	return out
}

func newSession(t *testing.T, label string) rex.Pane {
	t.Helper()
	if _, err := rex.Run("new", label); err != nil {
		t.Fatal(err)
	}
	panes, err := rex.ListPanes("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range panes {
		if p.SessionLabel == label {
			return p
		}
	}
	t.Fatalf("no session %s in %+v", label, panes)
	return rex.Pane{}
}

// liveConversation makes what Claude Code leaves on disk, with a process that
// has the pane's block in its environment.
func liveConversation(t *testing.T, claude string, p rex.Pane, id, name string, lines ...string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Not /bin/sleep: macOS hides the environment of its own binaries from ps.
	process := exec.Command(self)
	process.Env = append(os.Environ(), "REX_BLOCK="+p.Block, stayAliveVariable+"=1")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })

	session, _ := json.Marshal(map[string]any{"pid": process.Process.Pid, "sessionId": id, "name": name, "kind": "interactive"})
	os.MkdirAll(filepath.Join(claude, "sessions"), 0o755)
	os.WriteFile(filepath.Join(claude, "sessions", fmt.Sprint(process.Process.Pid)+".json"), session, 0o644)
	project := filepath.Join(claude, "projects", "-some-folder")
	os.MkdirAll(project, 0o755)
	os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(strings.Join(lines, "")), 0o644)
}

func TestFindsAPaneByNameByWhatWasSaidAndByItsScreen(t *testing.T) {
	server(t)
	agent := newSession(t, "agent-session")
	shell := newSession(t, "shell-session")
	claude, cache := t.TempDir(), t.TempDir()

	liveConversation(t, claude, agent, "conv-1", "Fix the login",
		userLine("why does zebrafish fail"),
		`{"type":"assistant","timestamp":"t","message":{"content":[{"type":"text","text":"zebrafish needs a lock"}]}}`+"\n",
		`{"type":"user","timestamp":"t","message":{"content":[{"type":"tool_result","content":"toolnoise"}]}}`+"\n",
	)
	if _, err := rex.Run("send", "-s", "shell-session", "echo mongoose-$((40+2))\n"); err != nil {
		t.Fatal(err)
	}

	take := func() []rex.Pane {
		t.Helper()
		panes, err := search.LoadPanes("", "", claude)
		if err != nil {
			t.Fatal(err)
		}
		return search.LoadText(panes, cache)
	}

	// The shell prints after a moment, and the snapshot is fixed when taken.
	var panes []rex.Pane
	deadline := time.Now().Add(20 * time.Second)
	for {
		panes = take()
		if len(search.Search(panes, "mongoose-42")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the screen text was never found")
		}
		time.Sleep(100 * time.Millisecond)
	}

	blocks := func(query string) string { return strings.Join(kinds(search.Search(panes, query)), " ") }
	if got, want := blocks("agent-session"), "pane:"+agent.Block; got != want {
		t.Errorf("by session: %s, want %s", got, want)
	}
	if got, want := blocks("fix the login"), "pane:"+agent.Block; got != want {
		t.Errorf("by conversation name: %s, want %s", got, want)
	}
	if got, want := blocks("zebrafish"), "said:"+agent.Block; got != want {
		t.Errorf("by what was said: %s, want %s", got, want)
	}
	if got, want := blocks("mongoose-42"), "screen:"+shell.Block; got != want {
		t.Errorf("by screen: %s, want %s", got, want)
	}
	if got := blocks("toolnoise"); got != "" {
		t.Errorf("found tool output: %s", got)
	}

	// A new message is found by the next picker.
	project := filepath.Join(claude, "projects", "-some-folder", "conv-1.jsonl")
	appendTo(t, project, userLine("and also narwhal"))
	panes = take()
	if got, want := blocks("narwhal"), "said:"+agent.Block; got != want {
		t.Errorf("after a new message: %s, want %s", got, want)
	}

	// The picker's own pane is not a result, and the others of its window are here.
	if _, err := rex.Run("split", "-s", "agent-session"); err != nil {
		t.Fatal(err)
	}
	listed, err := rex.ListPanes(agent.Block)
	if err != nil {
		t.Fatal(err)
	}
	here := 0
	for _, p := range listed {
		if p.Block == agent.Block {
			t.Errorf("found itself: %+v", p)
		}
		if p.Here {
			here++
			if p.SessionLabel != "agent-session" {
				t.Errorf("here in another session: %+v", p)
			}
		}
	}
	if here != 1 {
		t.Errorf("%d panes here", here)
	}
	panes, err = search.LoadPanes(shell.Block, "", claude)
	if err != nil {
		t.Fatal(err)
	}
	if got := blocks("shell-session"); got != "" {
		t.Errorf("found itself: %s", got)
	}
}

func TestGoToFocusesThePane(t *testing.T) {
	server(t)
	target := newSession(t, "target")
	if _, err := rex.Run("split", "-s", "target"); err != nil {
		t.Fatal(err)
	}
	focused := func() string {
		var view struct {
			Windows []struct {
				Focused string `json:"focused_block_id"`
			} `json:"windows"`
		}
		if err := rex.Call("session.view", map[string]string{"session_id": target.Session}, &view); err != nil {
			t.Fatal(err)
		}
		return view.Windows[0].Focused
	}
	if focused() == target.Block {
		t.Fatal("the split should have taken the focus")
	}
	// With no app connected, showing the session fails. The focus still moved.
	err := rex.GoTo(target.Session, target.Window, target.Block, "")
	if err == nil {
		t.Error("no error, but no app is connected")
	}
	if got := focused(); got != target.Block {
		t.Errorf("focused = %s, want %s", got, target.Block)
	}
}

// An action in the config sees the pane it was run on, and the picker lists it.
func TestRunActionRunsOnThePaneBehindThePicker(t *testing.T) {
	server(t)
	target := newSession(t, "target")
	if _, err := rex.Run("split", "-s", "target"); err != nil {
		t.Fatal(err)
	}
	var keymap struct {
		Config struct {
			Path string `json:"path"`
		} `json:"config"`
	}
	out, err := rex.Run("keymap", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &keymap); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(t.TempDir(), "seen")
	config := fmt.Sprintf(`rex.action{ name = "note_block", title = "Note Block", run = function(ctx)
  local file = io.open(%q, "w")
  file:write(ctx.block_id)
  file:close()
end }`, seen)
	if err := os.WriteFile(keymap.Config.Path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("config", "reload"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	actions, err := rex.LoadActions("")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, a := range actions {
		listed[a.Name] = true
	}
	if !listed["note_block"] || !listed["com.superlogical.terminal.clear"] {
		t.Fatalf("listed %v", listed)
	}
	if err := rex.RunAction("note_block", target.Session, target.Block, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != target.Block {
		t.Errorf("ran on %s, want %s", got, target.Block)
	}
}

func TestRefusesAServerThatDoesNotHoldItsOwnSession(t *testing.T) {
	server(t)
	newSession(t, "only")
	if err := rex.CheckServer("session:not-on-this-server"); err == nil {
		t.Fatal("no error for a session the server does not have")
	}
	panes, err := rex.ListPanes("")
	if err != nil {
		t.Fatal(err)
	}
	if err := rex.CheckServer(panes[0].Session); err != nil {
		t.Fatal(err)
	}
	if err := rex.CheckServer(""); err == nil {
		t.Fatal("no error outside of Rex")
	}
}

func TestCaptureScreenKeepsColors(t *testing.T) {
	server(t)
	p := newSession(t, "colors")
	if _, err := rex.Run("send", "-s", "colors", `printf '\033[31mmongoose-%d\033[0m\n' 42`+"\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		text, err := rex.Capture(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, "31m") && strings.Contains(text, "mongoose-42") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no color in %q", text)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The action is Lua, so only a real server can run it. Pressing the key while the
// picker is open must close it, and not stack another.
func TestTheActionOpensThePickerAndClosesIt(t *testing.T) {
	server(t)
	p := newSession(t, "work")
	dir := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "bin", "rexlauncher"), "../cmd/rexlauncher").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var keymap struct {
		Config struct {
			Path string `json:"path"`
		} `json:"config"`
	}
	out, err := rex.Run("keymap", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &keymap); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join("..", "rex", "config.lua"))
	if err != nil {
		t.Fatal(err)
	}
	loader := fmt.Sprintf("rexd = { plugin = { dir = %q } }\n%s", dir, config)
	if err := os.WriteFile(keymap.Config.Path, []byte(loader), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("config", "reload"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	floating := func() int {
		t.Helper()
		var view struct {
			Windows []struct {
				Layers []struct {
					Kind string `json:"kind"`
				} `json:"layers"`
			} `json:"windows"`
		}
		if err := rex.Call("session.view", map[string]string{"session_id": p.Session}, &view); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, window := range view.Windows {
			for _, layer := range window.Layers {
				if layer.Kind == "floating" {
					count++
				}
			}
		}
		return count
	}
	press := func() {
		t.Helper()
		if out, err := rex.Run("do", "rexlauncher", "-s", "work"); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		time.Sleep(300 * time.Millisecond)
	}
	press()
	if got := floating(); got != 1 {
		t.Fatalf("after the first press: %d floating layers", got)
	}
	press()
	if got := floating(); got != 0 {
		t.Fatalf("after the second press: %d floating layers", got)
	}
	press()
	if got := floating(); got != 1 {
		t.Fatalf("after the third press: %d floating layers", got)
	}
	// The picker can close by itself. The next press must open it, and not only try
	// to close what is gone. On a server with its own socket the program reports that
	// it cannot reach it, and waits for Enter.
	panes, err := rex.ListPanes("")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("send-key", "-s", "work", "-b", panes[len(panes)-1].Block, "enter"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for floating() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("after it closed: %d floating layers", floating())
		}
		time.Sleep(100 * time.Millisecond)
	}
	press()
	if got := floating(); got != 1 {
		t.Fatalf("after a press once it closed itself: %d floating layers", got)
	}
}

// "rex do rexlauncher" with no session has no session in its context.
func TestTheActionWorksWithNoSession(t *testing.T) {
	server(t)
	newSession(t, "work")
	var keymap struct {
		Config struct {
			Path string `json:"path"`
		} `json:"config"`
	}
	out, err := rex.Run("keymap", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &keymap); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join("..", "rex", "config.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keymap.Config.Path, []byte(fmt.Sprintf("rexd = { plugin = { dir = %q } }\n%s", t.TempDir(), config)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("config", "reload"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if out, err := rex.Run("do", "rexlauncher"); err != nil && strings.Contains(err.Error(), "index is nil") {
		t.Fatalf("%v: %s", err, out)
	}
}

// A real picker in a split, over a pane that runs a program named claude, with a
// real skill on disk. Enter must leave the skill in that pane, with no Enter after it.
func TestEnterPutsASkillInThePromptOfTheAgentBehindThePicker(t *testing.T) {
	claude := t.TempDir()
	skill := filepath.Join(claude, "skills", "grilling", "SKILL.md")
	os.MkdirAll(filepath.Dir(skill), 0o755)
	os.WriteFile(skill, []byte("---\nname: grilling\ndescription: Ask hard questions.\n---\n"), 0o644)
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	server(t)

	dir := t.TempDir()
	picker := filepath.Join(dir, "rexlauncher")
	if out, err := exec.Command("go", "build", "-o", picker, "../cmd/rexlauncher").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	agent := filepath.Join(dir, "claude")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, program, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("new", "work", "--shell=none", "--keep-open", "--", "/usr/bin/env", promptVariable+"=1", agent); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	panes, err := rex.ListPanes("")
	if err != nil || len(panes) != 1 {
		t.Fatalf("%v: %+v", err, panes)
	}
	target := panes[0]
	started := time.Now().Add(5 * time.Second)
	for {
		program, err := rex.Foreground(target.Session, target.Block)
		if err == nil && filepath.Base(program.Argv0) == "claude" {
			break
		}
		if time.Now().After(started) {
			t.Fatalf("%v: %+v", err, program)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if out, err := rex.Run("split", "-s", "work", "-b", target.Block, "--shell=none", "--", "/usr/bin/env", "PATH="+os.Getenv("PATH"), picker, "pick", "", target.Block); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	panes, err = rex.ListPanes("")
	if err != nil || len(panes) != 2 {
		t.Fatalf("%v: %+v", err, panes)
	}
	block := panes[1].Block
	if block == target.Block {
		block = panes[0].Block
	}
	captureOf := func(b string) string {
		out, err := rex.Run("capture", "-s", "work", "-b", b, "--trim")
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	waitFor := func(b, want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(captureOf(b), want) {
			if time.Now().After(deadline) {
				t.Fatalf("no %q in:\n%s", want, captureOf(b))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitFor(block, "Search panes")
	if _, err := rex.Run("send", "-s", "work", "-b", block, "grilling"); err != nil {
		t.Fatal(err)
	}
	waitFor(block, "/grilling")
	waitFor(block, "Put in prompt")
	if _, err := rex.Run("send-key", "-s", "work", "-b", block, "enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(target.Block, "/grilling")
	time.Sleep(300 * time.Millisecond)
	if screen := captureOf(target.Block); strings.Contains(screen, "sent [") {
		t.Fatalf("the skill was sent with Enter:\n%s", screen)
	}
	// Enter from the person ends the line, so the agent shows all that the prompt held.
	if _, err := rex.Run("send-key", "-s", "work", "-b", target.Block, "enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(target.Block, "sent [/grilling ]")
}

// standIn starts a program in a session that says each line it gets and reports a status.
func standIn(t *testing.T, label, status string) rex.Pane {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("new", label, "--shell=none", "--keep-open", "--", "/usr/bin/env", promptVariable+"=1", statusVariable+"="+status, self); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	panes, err := rex.ListPanes("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range panes {
		if p.SessionLabel == label {
			return p
		}
	}
	t.Fatalf("no pane in %s", label)
	return rex.Pane{}
}

func waitForScreen(t *testing.T, p rex.Pane, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, err := rex.Run("capture", "-s", p.Session, "-b", p.Block, "--trim")
		if err == nil && strings.Contains(string(out), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q in:\n%s", want, out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForStatus(t *testing.T, p rex.Pane, state string) rex.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, err := rex.ReadStatus(p.Session, p.Block)
		if err == nil && status.State == state {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("%v: status %+v", err, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// "Allow Bash?" in base64, as the protocol carries text.
const blockedReport = "state=blocked:kind=permission:app=stand-in:msg=QWxsb3cgQmFzaD8="

func TestRexReportsAStatusAndAKeyReachesAMenu(t *testing.T) {
	server(t)
	p := standIn(t, "agent", blockedReport)
	seen := waitForStatus(t, p, rex.StateBlocked)
	if seen.Kind != "permission" || seen.Message != "Allow Bash?" || seen.App != "stand-in" || seen.At.IsZero() {
		t.Fatalf("%+v", seen)
	}
	if err := rex.Deliver(p.Session, p.Block, rex.Answer{Key: "2", Seen: seen}); err != nil {
		t.Fatal(err)
	}
	waitForScreen(t, p, "2")
	if err := rex.Deliver(p.Session, p.Block, rex.Answer{Text: "hello", Seen: seen}); err == nil {
		t.Fatal("text went to a menu")
	}
}

func TestAnAnswerReachesAnAgentAtRest(t *testing.T) {
	server(t)
	p := standIn(t, "agent", "state=done:app=stand-in")
	seen := waitForStatus(t, p, rex.StateDone)
	if err := rex.Deliver(p.Session, p.Block, rex.Answer{Text: "next step", Seen: seen}); err != nil {
		t.Fatal(err)
	}
	waitForScreen(t, p, "sent [next step]")
}

func TestAnAnswerIsRefusedOnceTheAgentMovedOn(t *testing.T) {
	server(t)
	p := standIn(t, "agent", blockedReport)
	seen := waitForStatus(t, p, rex.StateBlocked)
	seen.Message = "Allow something else?"
	if err := rex.Deliver(p.Session, p.Block, rex.Answer{Key: "1", Seen: seen}); err == nil {
		t.Fatal("a key went to a prompt the person did not see")
	}
	out, err := rex.Run("capture", "-s", p.Session, "-b", p.Block, "--trim")
	if err != nil || strings.Contains(string(out), "1") {
		t.Fatalf("%v: pane holds %q", err, out)
	}
}

// A real session with a split is saved, closed and built again. A pane with a conversation
// starts a program named claude, through the shell, with --resume.
func TestALayoutComesBackWithItsSplitsFoldersAndConversation(t *testing.T) {
	bin := t.TempDir()
	marks := filepath.Join(t.TempDir(), "resumed")
	script := "#!/bin/sh\necho \"$*\" > \"" + marks + "\"\npwd >> \"" + marks + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// A shell of its own: a login profile on this machine could put another claude first.
	shell := filepath.Join(bin, "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nPATH=\""+bin+":$PATH\" exec /bin/sh -c \"$2\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	server(t)
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("new", "work", "-c", folder); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if out, err := rex.Run("split", "-s", "work", "--split=below", "-c", folder); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	time.Sleep(500 * time.Millisecond)
	panes, err := rex.ListPanes("")
	if err != nil || len(panes) != 2 {
		t.Fatalf("%v: %+v", err, panes)
	}
	const id = "a3af7ab8-1cc4-5178-92d4-0f28dc79e876"
	panes[0].Conversation = &conversations.Conversation{ID: id}
	saved, err := layouts.Capture("work", panes[0].Session, panes)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := layouts.Save(dir, saved); err != nil {
		t.Fatal(err)
	}
	if out, err := rex.Run("kill", "work"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	list, err := layouts.List(dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v: %+v", err, list)
	}

	session, err := layouts.Restore(list[0], shell, nil)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Windows []struct {
			Layers []struct {
				Layout struct {
					Split *struct {
						Direction string `json:"direction"`
					} `json:"split"`
				} `json:"layout"`
				Blocks []struct {
					ID string `json:"block_id"`
				} `json:"blocks"`
			} `json:"layers"`
		} `json:"windows"`
	}
	if err := rex.Call("session.view", map[string]string{"session_id": session}, &view); err != nil {
		t.Fatal(err)
	}
	layer := view.Windows[0].Layers[0]
	if layer.Layout.Split == nil || len(layer.Blocks) != 2 {
		t.Fatalf("got %+v", view)
	}
	if original := saved.Windows[0].Root.Direction; layer.Layout.Split.Direction != original {
		t.Fatalf("direction %q, was %q", layer.Layout.Split.Direction, original)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(marks)
		if err == nil && strings.Contains(string(data), "--resume "+id) && strings.Contains(string(data), folder) {
			break
		}
		if time.Now().After(deadline) {
			screen, _ := rex.Run("capture", "-s", session, "-b", layer.Blocks[0].ID, "--trim")
			t.Fatalf("%v: %q, and the pane shows:\n%s", err, data, screen)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// A second restore goes to the session, and does not start everything twice.
	again, err := layouts.Restore(list[0], shell, nil)
	if err != nil || again != session {
		t.Fatalf("%v: %q, was %q", err, again, session)
	}
}
