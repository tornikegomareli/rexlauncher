package layouts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/testutil"
)

func cannedView() testutil.Block {
	leaf := func(block string, rect []float64) testutil.Block {
		return testutil.Block{"rect": rect, "block_id": block}
	}
	return testutil.Block{"label": "work", "windows": []testutil.Block{
		{"window_id": "w1", "label": "Window 89abcdef", "layers": []testutil.Block{
			{"kind": "tiled", "layout": testutil.Block{"rect": []float64{0, 0, 1, 1}, "split": testutil.Block{
				"direction": "horizontal", "ratio": 0.6,
				"before": leaf("agent", []float64{0, 0, .6, 1}),
				"after":  leaf("shell", []float64{.6, 0, .4, 1}),
			}}},
			{"kind": "floating", "layout": leaf("picker", []float64{.1, .1, .8, .8})},
		}},
		{"window_id": "w2", "label": "logs", "layers": []testutil.Block{
			{"kind": "tiled", "layout": leaf("tail", []float64{0, 0, 1, 1})},
		}},
	}}
}

func somePanes() []rex.Pane {
	return []rex.Pane{
		{Block: "agent", Name: "Fix it", Cwd: "/code/app", Conversation: &conversations.Conversation{ID: "a3af7ab8-1cc4-5178-92d4-0f28dc79e876"}},
		{Block: "shell", Name: "zsh", Cwd: "/code/app"},
		{Block: "tail", Name: "tail", Cwd: "/var/log"},
	}
}

func TestCaptureKeepsWindowsSplitsFoldersAndConversations(t *testing.T) {
	testutil.FakeRex(t, map[string]any{"session.view": cannedView()})
	got, err := Capture("mine", "s1", somePanes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "mine" || len(got.Windows) != 2 || got.SavedAt.IsZero() {
		t.Fatalf("%+v", got)
	}
	root := got.Windows[0].Root
	if root.Direction != "horizontal" || root.Ratio != 0.6 || root.Before.Pane.Conversation == "" || root.Before.Pane.Cwd != "/code/app" || root.After.Pane.Conversation != "" {
		t.Fatalf("%+v", root)
	}
	if got.Windows[0].Label != "" || got.Windows[1].Label != "logs" {
		t.Fatalf("labels %q %q", got.Windows[0].Label, got.Windows[1].Label)
	}
	if len(got.Panes()) != 3 {
		t.Fatalf("panes %+v", got.Panes())
	}
}

func TestSaveListAndDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "layouts")
	older := Layout{Name: "older", SavedAt: time.Now().Add(-time.Hour), Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a"}}}}}
	newer := Layout{Name: "a/b ../newer", SavedAt: time.Now(), Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/b"}}}}}
	for _, l := range []Layout{older, newer} {
		if err := Save(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Fatalf("files %v: a name must stay one file in the folder", files)
	}
	info, err := os.Stat(filepath.Join(dir, files[0].Name()))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, info.Mode())
	}
	got, err := List(dir)
	if err != nil || len(got) != 2 || got[0].Name != newer.Name || got[1].Name != "older" {
		t.Fatalf("%v %+v", err, got)
	}
	if err := Delete(dir, newer.Name); err != nil {
		t.Fatal(err)
	}
	if got, _ := List(dir); len(got) != 1 || got[0].Name != "older" {
		t.Fatalf("%+v", got)
	}
}

func TestSavingANameAgainReplacesIt(t *testing.T) {
	dir := t.TempDir()
	Save(dir, Layout{Name: "x", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a"}}}}})
	Save(dir, Layout{Name: "x", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/b"}}}}})
	got, err := List(dir)
	if err != nil || len(got) != 1 || got[0].Windows[0].Root.Pane.Cwd != "/b" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestListOfAFolderThatIsNotThereIsEmpty(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAFileThatCannotBeReadIsReportedAndTheOthersStay(t *testing.T) {
	dir := t.TempDir()
	Save(dir, Layout{Name: "good", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a"}}}}})
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600)
	got, err := List(dir)
	if err == nil || !strings.Contains(err.Error(), "bad.json") || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
}

func windowsOf(t *testing.T, spec map[string]any) string {
	t.Helper()
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCreationRebuildsSplitsAndResumesConversations(t *testing.T) {
	testutil.FakeRex(t, map[string]any{"session.view": cannedView()})
	l, err := Capture("mine", "s1", somePanes())
	if err != nil {
		t.Fatal(err)
	}
	got := windowsOf(t, creation(l, "/bin/zsh", nil))
	for _, want := range []string{
		`"label":"mine"`, `"direction":"horizontal"`, `"ratio":0.6`,
		`"cwd":"/code/app"`, `"cwd":"/var/log"`,
		`claude --resume a3af7ab8-1cc4-5178-92d4-0f28dc79e876`,
		`"window_label":"logs"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in %s", want, got)
		}
	}
	if !strings.Contains(got, `"/bin/zsh","-ic"`) {
		t.Errorf("not through the shell the person uses: %s", got)
	}
}

// Two copies of Claude Code on one conversation would write over each other.
func TestAConversationThatIsStillOpenIsNotResumedAgain(t *testing.T) {
	testutil.FakeRex(t, map[string]any{"session.view": cannedView()})
	l, _ := Capture("mine", "s1", somePanes())
	live := map[string]bool{"a3af7ab8-1cc4-5178-92d4-0f28dc79e876": true}
	if got := windowsOf(t, creation(l, "/bin/zsh", live)); strings.Contains(got, "--resume") {
		t.Fatalf("resumed: %s", got)
	}
}

func TestAConversationIDThatIsNotOneRunsNothing(t *testing.T) {
	l := Layout{Name: "x", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a", Conversation: "x; rm -rf ~"}}}}}
	if got := windowsOf(t, creation(l, "/bin/zsh", nil)); strings.Contains(got, "rm -rf") {
		t.Fatalf("ran it: %s", got)
	}
}

func TestRestoreGoesToASessionThatAlreadyHasTheName(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{"session.list": testutil.Block{"sessions": []testutil.Block{{"session_id": "s9", "label": "mine"}}}})
	l := Layout{Name: "mine", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a"}}}}}
	got, err := Restore(l, "/bin/zsh", nil)
	if err != nil || got != "s9" {
		t.Fatalf("%v %q", err, got)
	}
	for _, c := range calls() {
		if strings.Contains(c, "session.create") {
			t.Fatalf("created one anyway: %q", c)
		}
	}
}

func TestRestoreCreatesTheSession(t *testing.T) {
	calls := testutil.FakeRex(t, map[string]any{
		"session.list":   testutil.Block{"sessions": []testutil.Block{}},
		"session.create": testutil.Block{"session_id": "s2"},
	})
	l := Layout{Name: "mine", Windows: []Window{{Root: Node{Pane: &Pane{Cwd: "/a"}}}}}
	got, err := Restore(l, "/bin/zsh", nil)
	if err != nil || got != "s2" {
		t.Fatalf("%v %q %q", err, got, calls())
	}
}
