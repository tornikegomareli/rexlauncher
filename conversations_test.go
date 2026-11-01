package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractMessage(t *testing.T) {
	tests := []struct {
		name, line string
		want       string
		ok         bool
	}{
		{"a person's words", `{"type":"user","timestamp":"t","message":{"content":"find the flock bug"}}`, "find the flock bug", true},
		{"key order does not matter", `{"parentUuid":"x","message":{"content":"hello"},"timestamp":"t","type":"user"}`, "hello", true},
		{"assistant text", `{"type":"assistant","timestamp":"t","message":{"content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"the answer"}]}}`, "the answer", true},
		{"tool output is left out", `{"type":"user","timestamp":"t","message":{"content":[{"type":"tool_result","content":"noise"}]}}`, "", false},
		{"tool calls are left out", `{"type":"assistant","timestamp":"t","message":{"content":[{"type":"tool_use","input":{"command":"ls"}}]}}`, "", false},
		{"Claude Code's own messages", `{"type":"user","isMeta":true,"message":{"content":"caveat"}}`, "", false},
		{"a compact summary", `{"type":"user","isCompactSummary":true,"message":{"content":"summary"}}`, "", false},
		{"a subagent", `{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"sub"}]}}`, "", false},
		{"a wrapped command", `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`, "", false},
		{"another type naming user inside", `{"type":"api-request","body":{"type":"user","message":{"content":"copy"}}}`, "", false},
		{"not JSON", `{"type":"user",`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := extractMessage([]byte(tt.line))
			if ok != tt.ok || m.Text != tt.want {
				t.Fatalf("got %q, %v; want %q, %v", m.Text, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAMessageStaysOnOneLine(t *testing.T) {
	m := message{Role: "user", At: "t", Text: "one\ntwo\tthree\r\nfour"}
	got := parseMessage(strings.TrimSuffix(m.line(), "\n"))
	if got.Text != "one two three four" || got.Role != "user" || got.At != "t" {
		t.Fatalf("got %+v", got)
	}
}

func userLine(text string) string {
	return `{"type":"user","timestamp":"t","message":{"content":"` + text + `"}}` + "\n"
}

func TestUpdateIndexReadsOnlyWhatIsNew(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(transcript, []byte(userLine("first")), 0o644); err != nil {
		t.Fatal(err)
	}
	index := func() string {
		t.Helper()
		path, err := updateIndex(dir, "id", transcript)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		return string(data)
	}
	if got := index(); got != "user\tt\tfirst\n" {
		t.Fatalf("first: %q", got)
	}
	if got := index(); got != "user\tt\tfirst\n" {
		t.Fatalf("nothing new, but: %q", got)
	}

	// Half a line waits until it is whole.
	whole := userLine("second")
	appendTo(t, transcript, whole[:10])
	if got := index(); got != "user\tt\tfirst\n" {
		t.Fatalf("half a line: %q", got)
	}
	appendTo(t, transcript, whole[10:])
	if got := index(); got != "user\tt\tfirst\nuser\tt\tsecond\n" {
		t.Fatalf("whole line: %q", got)
	}

	// A shorter transcript is a new one.
	if err := os.WriteFile(transcript, []byte(userLine("x")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := index(); got != "user\tt\tx\n" {
		t.Fatalf("replaced: %q", got)
	}
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

func TestAnIndexFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "a.jsonl")
	os.WriteFile(transcript, []byte(userLine("first")), 0o644)
	path, err := updateIndex(dir, "id", transcript)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestPruneIndexKeepsRecentOnes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"old.txt", "old.offset", "new.txt", "new.offset"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600)
	}
	long := time.Now().Add(-2 * indexMaxAge)
	os.Chtimes(filepath.Join(dir, "old.txt"), long, long)

	pruneIndex(dir)

	for name, want := range map[string]bool{"old.txt": false, "old.offset": false, "new.txt": true, "new.offset": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s: exists = %v, want %v", name, err == nil, want)
		}
	}
}

// Closing the picker while it reads conversations ends the process in the middle of
// a write. The index must not keep half a line, or the same messages twice.
func TestAKilledIndexWriteLeavesTheIndexUsable(t *testing.T) {
	const variable = "REXLAUNCHER_TEST_INDEX_DIR"
	if dir := os.Getenv(variable); dir != "" {
		updateIndex(dir, "c", filepath.Join(dir, "t.jsonl"))
		return
	}
	dir := t.TempDir()
	transcript := filepath.Join(dir, "t.jsonl")
	const messages = 150000
	var content strings.Builder
	for i := range messages {
		content.WriteString(userLine(fmt.Sprintf("message number %d with some words in it", i)))
	}
	if err := os.WriteFile(transcript, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, delay := range []time.Duration{30, 60, 90, 120, 180, 240} {
		child := exec.Command(os.Args[0], "-test.run=^TestAKilledIndexWriteLeavesTheIndexUsable$")
		child.Env = append(os.Environ(), variable+"="+dir)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(delay * time.Millisecond)
		child.Process.Kill()
		child.Wait()
	}

	path, err := updateIndex(dir, "c", transcript)
	if err != nil {
		t.Fatal(err)
	}
	lines := readLines(path)
	if len(lines) != messages {
		t.Errorf("%d lines, want %d", len(lines), messages)
	}
	for _, line := range lines {
		if strings.Count(line, "message number") != 1 {
			t.Fatalf("a broken line: %q", line)
		}
	}
}

const stayAliveVariable = "REXLAUNCHER_TEST_STAY_ALIVE"

// TestStayAlive is what liveSession runs as: a process with a pane in its environment.
func TestStayAlive(t *testing.T) {
	if os.Getenv(stayAliveVariable) != "" {
		time.Sleep(time.Minute)
	}
}

// liveSession makes what Claude Code leaves on disk for a session in a pane. It uses
// this program, because macOS hides the environment of its own binaries from ps.
func liveSession(t *testing.T, claude, block, id, kind, name string, messages ...string) {
	t.Helper()
	process := exec.Command(os.Args[0], "-test.run=^TestStayAlive$")
	process.Env = append(os.Environ(), "REX_BLOCK="+block, stayAliveVariable+"=1")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })
	session, err := json.Marshal(map[string]any{"pid": process.Process.Pid, "sessionId": id, "name": name, "kind": kind})
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(claude, "sessions", strconv.Itoa(process.Process.Pid)+".json"): string(session),
		filepath.Join(claude, "projects", "-some-folder", id+".jsonl"):               strings.Join(messages, ""),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindConversationsMatchesSessionsToTheirPanes(t *testing.T) {
	claude := t.TempDir()
	const pane = "block:0000aaaa-0000-0000-0000-000000000001"
	liveSession(t, claude, pane, "conv", "interactive", "Fix it", userLine("hello"))
	found := findConversations(claude)
	c, ok := found[pane]
	if !ok || c.ID != "conv" || c.Name != "Fix it" || c.Active.IsZero() {
		t.Fatalf("got %+v", found)
	}
}

func TestFindConversationsLeavesOutAProcessThatIsGone(t *testing.T) {
	claude := t.TempDir()
	if err := os.MkdirAll(filepath.Join(claude, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone, err := json.Marshal(map[string]any{"pid": 999999, "sessionId": "x", "kind": "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "sessions", "999999.json"), gone, 0o644); err != nil {
		t.Fatal(err)
	}
	if found := findConversations(claude); len(found) != 0 {
		t.Fatalf("got %+v", found)
	}
}

// A one-shot run in a pane must not hide the conversation a person is having there.
func TestAnInteractiveSessionWinsItsPane(t *testing.T) {
	claude := t.TempDir()
	const pane = "block:0000aaaa-0000-0000-0000-000000000002"
	liveSession(t, claude, pane, "talk", "interactive", "The conversation", userLine("hello"))
	liveSession(t, claude, pane, "oneshot", "print", "A one-shot run", userLine("hello"))
	if c := findConversations(claude)[pane]; c.ID != "talk" {
		t.Fatalf("got %+v", c)
	}
}

func TestLoadTextReadsAConversationFromItsPane(t *testing.T) {
	claude, cache := t.TempDir(), t.TempDir()
	const block = "block:0000aaaa-0000-0000-0000-000000000003"
	fakeRex(t, map[string]any{
		"session.list": map[string]any{"sessions": []map[string]any{{"session_id": "s"}}},
		"session.view.s": map[string]any{"label": "work", "windows": []map[string]any{{"window_id": "w", "layers": []map[string]any{
			{"blocks": []map[string]any{{"block_id": block, "label": "shell", "creator_name": "com.superlogical.terminal"}}},
		}}}},
	})
	liveSession(t, claude, block, "conv", "interactive", "Fix it", userLine("zebrafish"))
	panes, err := loadPanes("", claude)
	if err != nil {
		t.Fatal(err)
	}
	if panes[0].Name != "Fix it" || panes[0].Conversation == nil {
		t.Fatalf("got %+v", panes[0])
	}
	panes = loadText(panes, cache)
	if got := search(panes, "zebrafish"); len(got) != 1 || got[0].Reason != reasonSaid {
		t.Fatalf("got %+v", got)
	}
}

func TestAMessageThatStartsWithATagIsAPersonsWords(t *testing.T) {
	for _, text := range []string{"<Button> crashes on tap", "<div> does not center"} {
		if _, ok := extractMessage([]byte(userLine(text))); !ok {
			t.Errorf("%s was left out", text)
		}
	}
	for _, text := range []string{"<task-notification>done</task-notification>", "<local-command-stdout>x</local-command-stdout>", "<bash-input>ls</bash-input>", "<system-reminder>x</system-reminder>"} {
		if _, ok := extractMessage([]byte(userLine(text))); ok {
			t.Errorf("%s was kept", text)
		}
	}
}

func TestPastedTextIsSearchedWithoutItsTag(t *testing.T) {
	if _, ok := extractMessage([]byte(userLine("<pasted_content>the log</pasted_content>"))); !ok {
		t.Fatal("left out")
	}
	if got := plain(`<pasted_content id="2c24">the zebrafish log</pasted_content>`); got != "the zebrafish log" {
		t.Fatalf("got %q", got)
	}
}

func TestTheNewestConversationWinsItsPane(t *testing.T) {
	claude := t.TempDir()
	const pane = "block:0000aaaa-0000-0000-0000-000000000004"
	liveSession(t, claude, pane, "fresh", "interactive", "Fresh", userLine("hello"))
	liveSession(t, claude, pane, "stale", "interactive", "Stale", userLine("hello"))
	old := time.Now().Add(-240 * time.Hour)
	if err := os.Chtimes(filepath.Join(claude, "projects", "-some-folder", "stale.jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
	if c := findConversations(claude)[pane]; c.ID != "fresh" {
		t.Fatalf("got %+v", c)
	}
}

func TestACorruptOffsetStartsAgain(t *testing.T) {
	dir := t.TempDir()
	transcript := writeLines(t, strings.TrimSuffix(userLine("hello"), "\n"))
	for _, content := range []string{"1 -5", "-1 0", "nonsense", "5"} {
		if err := os.WriteFile(filepath.Join(dir, "c.offset"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		path, err := updateIndex(dir, "c", transcript)
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if got := messageEntries(path); len(got) != 1 || got[0].Text != "hello" {
			t.Fatalf("%q: got %+v", content, got)
		}
	}
}
