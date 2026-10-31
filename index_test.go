package main

import (
	"os"
	"path/filepath"
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
