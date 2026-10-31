//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const stayAliveVariable = "REX_JUMP_TEST_STAY_ALIVE"

func TestMain(m *testing.M) {
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
		if _, err := runRex("ls"); err == nil {
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
	out, err := runRex("keymap", "--json")
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

func newSession(t *testing.T, label string) pane {
	t.Helper()
	if _, err := runRex("new", label); err != nil {
		t.Fatal(err)
	}
	panes, err := listPanes("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range panes {
		if p.SessionLabel == label {
			return p
		}
	}
	t.Fatalf("no session %s in %+v", label, panes)
	return pane{}
}

// liveConversation makes what Claude Code leaves on disk, with a process that
// has the pane's block in its environment.
func liveConversation(t *testing.T, claude string, p pane, id, name string, lines ...string) {
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
	claude, cache, snapshot := t.TempDir(), t.TempDir(), t.TempDir()

	liveConversation(t, claude, agent, "conv-1", "Fix the login",
		userLine("why does zebrafish fail"),
		`{"type":"assistant","timestamp":"t","message":{"content":[{"type":"text","text":"zebrafish needs a lock"}]}}`+"\n",
		`{"type":"user","timestamp":"t","message":{"content":[{"type":"tool_result","content":"toolnoise"}]}}`+"\n",
	)
	if _, err := runRex("send", "-s", "shell-session", "echo mongoose-$((40+2))\n"); err != nil {
		t.Fatal(err)
	}

	take := func() []pane {
		t.Helper()
		if err := takeSnapshot(snapshot, "", claude, cache); err != nil {
			t.Fatal(err)
		}
		panes, err := loadSnapshot(snapshot, true)
		if err != nil {
			t.Fatal(err)
		}
		return panes
	}

	// The shell prints after a moment, and the snapshot is fixed when taken.
	var panes []pane
	deadline := time.Now().Add(20 * time.Second)
	for {
		panes = take()
		if len(search(panes, "mongoose-42")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the screen text was never found")
		}
		time.Sleep(100 * time.Millisecond)
	}

	blocks := func(query string) string { return strings.Join(kinds(search(panes, query)), " ") }
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

	// The picker's own pane is not a result.
	if err := takeSnapshot(snapshot, shell.Block, claude, cache); err != nil {
		t.Fatal(err)
	}
	panes, _ = loadSnapshot(snapshot, true)
	if got := blocks("shell-session"); got != "" {
		t.Errorf("found itself: %s", got)
	}
}

func TestGoToFocusesThePane(t *testing.T) {
	server(t)
	target := newSession(t, "target")
	if _, err := runRex("split", "-s", "target"); err != nil {
		t.Fatal(err)
	}
	focused := func() string {
		var view struct {
			Windows []struct {
				Focused string `json:"focused_block_id"`
			} `json:"windows"`
		}
		if err := callRex("session.view", map[string]string{"session_id": target.Session}, &view); err != nil {
			t.Fatal(err)
		}
		return view.Windows[0].Focused
	}
	if focused() == target.Block {
		t.Fatal("the split should have taken the focus")
	}
	// With no app connected, showing the session fails. The focus still moved.
	err := goTo(target.Session, target.Window, target.Block, "")
	if err == nil {
		t.Error("no error, but no app is connected")
	}
	if got := focused(); got != target.Block {
		t.Errorf("focused = %s, want %s", got, target.Block)
	}
}

func TestRefusesAServerThatDoesNotHoldItsOwnSession(t *testing.T) {
	server(t)
	newSession(t, "only")
	if err := checkServer("session:not-on-this-server"); err == nil {
		t.Fatal("no error for a session the server does not have")
	}
	panes, err := listPanes("")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkServer(panes[0].Session); err != nil {
		t.Fatal(err)
	}
	if err := checkServer(""); err == nil {
		t.Fatal("no error outside of Rex")
	}
}

func TestThePaneListIsReadyBeforeTheirContent(t *testing.T) {
	server(t)
	newSession(t, "only")
	snapshot, cache := t.TempDir(), t.TempDir()
	if err := takeSnapshot(snapshot, "", t.TempDir(), cache); err != nil {
		t.Fatal(err)
	}
	for _, withContent := range []bool{false, true} {
		panes, err := loadSnapshot(snapshot, withContent)
		if err != nil || len(panes) == 0 {
			t.Fatalf("withContent %v: %v, %+v", withContent, err, panes)
		}
		if withContent && panes[0].Screen == "" {
			t.Errorf("no screen in %+v", panes[0])
		}
		if !withContent && panes[0].Screen != "" {
			t.Errorf("a screen in the first list: %+v", panes[0])
		}
	}
}
