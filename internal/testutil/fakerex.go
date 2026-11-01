package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeRex puts a rex on PATH that answers from canned JSON and records its calls, so
// the code that reads Rex can be tested without a server.
func FakeRex(t *testing.T, answers map[string]any) (calls func() []string) {
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
    if [ -f "$file" ]; then
      if grep -q '^"FAIL"$' "$file"; then echo "failed (status=404 code=session_not_found)" >&2; exit 1; fi
      cat "$file"; exit 0
    fi
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

// Block is one object in a canned answer.
type Block = map[string]any

// TwoSessions is the answer of a server with two sessions.
func TwoSessions() map[string]any {
	const terminal = "com.superlogical.terminal"
	return map[string]any{
		"session.list": Block{"sessions": []Block{{"session_id": "s1"}, {"session_id": "s2"}}},
		"session.view.s1": Block{"label": "one", "windows": []Block{
			{"window_id": "w1", "label": "main", "layers": []Block{
				{"blocks": []Block{
					{"block_id": "b1", "label": "first", "creator_name": terminal},
					{"block_id": "self", "label": "picker", "creator_name": terminal},
				}},
				{"blocks": []Block{{"block_id": "other", "label": "not a terminal", "creator_name": "com.example.notes"}}},
			}},
			{"window_id": "w2", "label": "side", "layers": []Block{
				{"blocks": []Block{{"block_id": "b2", "label": "second", "creator_name": terminal}}},
			}},
		}},
		"session.view.s2": Block{"label": "two", "windows": []Block{
			{"window_id": "w3", "label": "", "layers": []Block{
				{"blocks": []Block{{"block_id": "b3", "label": "third", "creator_name": terminal}}},
			}},
		}},
		"block.method.b1.title": Block{"title": "a title"},
		"block.method.b1.pwd":   Block{"pwd": "file://host/code/my%20app"},
	}
}
