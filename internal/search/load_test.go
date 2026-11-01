package search

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/testutil"
)

const stayAliveVariable = "REXLAUNCHER_TEST_STAY_ALIVE"

// TestStayAlive is what liveSession runs as: a process with a pane in its environment.
func TestStayAlive(t *testing.T) {
	if os.Getenv(stayAliveVariable) != "" {
		time.Sleep(time.Minute)
	}
}

func userLine(text string) string {
	return `{"type":"user","timestamp":"t","message":{"content":"` + text + `"}}` + "\n"
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

func TestLoadTextReadsAConversationFromItsPane(t *testing.T) {
	claude, cache := t.TempDir(), t.TempDir()
	const block = "block:0000aaaa-0000-0000-0000-000000000003"
	testutil.FakeRex(t, map[string]any{
		"session.list": map[string]any{"sessions": []map[string]any{{"session_id": "s"}}},
		"session.view.s": map[string]any{"label": "work", "windows": []map[string]any{{"window_id": "w", "layers": []map[string]any{
			{"blocks": []map[string]any{{"block_id": block, "label": "shell", "creator_name": "com.superlogical.terminal"}}},
		}}}},
	})
	liveSession(t, claude, block, "conv", "interactive", "Fix it", userLine("zebrafish"))
	panes, err := LoadPanes("", claude)
	if err != nil {
		t.Fatal(err)
	}
	if panes[0].Name != "Fix it" || panes[0].Conversation == nil {
		t.Fatalf("got %+v", panes[0])
	}
	panes = LoadText(panes, cache)
	if got := Search(panes, "zebrafish"); len(got) != 1 || got[0].Reason != reasonSaid {
		t.Fatalf("got %+v", got)
	}
}

func TestPastedTextIsSearchedWithoutItsTag(t *testing.T) {
	if got := plain(`<pasted_content id="2c24">the zebrafish log</pasted_content>`); got != "the zebrafish log" {
		t.Fatalf("got %q", got)
	}
}
