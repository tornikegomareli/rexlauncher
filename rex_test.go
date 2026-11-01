package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRex puts a rex on PATH that answers from canned JSON and records its calls, so
// the code that reads Rex can be tested without a server.
func fakeRex(t *testing.T, answers map[string]any) (calls func() []string) {
	t.Helper()
	dir := t.TempDir()
	for name, answer := range answers {
		data, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "calls")
	// An answer's file is named after the call: the method, then the session or
	// block when it has one, joined with a dot.
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
shift # --autostart=false
if [ "$1" = api ]; then
  key="$3.$(printf '%s' "$4" | sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p')"
  method="$(printf '%s' "$4" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')"
  block="$(printf '%s' "$4" | sed -n 's/.*"block_id":"\([^"]*\)".*/\1/p')"
  [ -n "$block" ] && key="$3.$block.$method"
  for file in "` + dir + `/$key.json" "` + dir + `/$3.json"; do
    if [ -f "$file" ]; then cat "$file"; exit 0; fi
  done
  echo '{}'
  exit 0
fi
# Answering "fail-client" makes a call through a client fail.
if [ "$1" = -C ] && [ -f "` + dir + `/fail-client.json" ]; then echo "no such client" >&2; exit 3; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		data, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

type block = map[string]any

func twoSessions() map[string]any {
	const terminal = "com.superlogical.terminal"
	return map[string]any{
		"session.list": block{"sessions": []block{{"session_id": "s1"}, {"session_id": "s2"}}},
		"session.view.s1": block{"label": "one", "windows": []block{
			{"window_id": "w1", "label": "main", "layers": []block{
				{"blocks": []block{
					{"block_id": "b1", "label": "first", "creator_name": terminal},
					{"block_id": "self", "label": "picker", "creator_name": terminal},
				}},
				{"blocks": []block{{"block_id": "other", "label": "not a terminal", "creator_name": "com.example.notes"}}},
			}},
			{"window_id": "w2", "label": "side", "layers": []block{
				{"blocks": []block{{"block_id": "b2", "label": "second", "creator_name": terminal}}},
			}},
		}},
		"session.view.s2": block{"label": "two", "windows": []block{
			{"window_id": "w3", "label": "", "layers": []block{
				{"blocks": []block{{"block_id": "b3", "label": "third", "creator_name": terminal}}},
			}},
		}},
		"block.method.b1.title": block{"title": "a title"},
		"block.method.b1.pwd":   block{"pwd": "file://host/code/my%20app"},
	}
}

func TestListPanesKeepsTerminalsAndLeavesOutThePicker(t *testing.T) {
	fakeRex(t, twoSessions())
	panes, err := listPanes("self")
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
	fakeRex(t, twoSessions())
	panes, err := listPanes("self")
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
	fakeRex(t, twoSessions())
	panes, err := listPanes("self")
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
	fakeRex(t, twoSessions())
	if err := checkServer("s2"); err != nil {
		t.Errorf("its own session: %v", err)
	}
	if err := checkServer("elsewhere"); err == nil || !strings.Contains(err.Error(), "REX_SERVER") {
		t.Errorf("another session: %v", err)
	}
}

func TestGoToFocusesThenShowsThroughTheClient(t *testing.T) {
	calls := fakeRex(t, nil)
	if err := goTo("s1", "w1", "b1", "client-1"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 || !strings.Contains(got[0], "session.focus_block") || !strings.Contains(got[1], "-C client-1 do session.select session_id=s1 window_id=w1") {
		t.Fatalf("got %q", got)
	}
}

func TestGoToWithoutAClientLetsRexChoose(t *testing.T) {
	calls := fakeRex(t, nil)
	if err := goTo("s1", "w1", "b1", ""); err != nil {
		t.Fatal(err)
	}
	if got := calls(); len(got) != 2 || strings.Contains(got[1], "-C") {
		t.Fatalf("got %q", got)
	}
}

// "rex do rexlauncher" has no client, or the client that opened the picker is gone.
func TestGoToFallsBackWhenTheClientIsGone(t *testing.T) {
	calls := fakeRex(t, map[string]any{"fail-client": true})
	if err := goTo("s1", "w1", "b1", "gone"); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 3 || !strings.Contains(got[1], "-C gone") || strings.Contains(got[2], "-C") {
		t.Fatalf("got %q", got)
	}
}
