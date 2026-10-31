package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// A snapshot is what the picker knows, fixed when it opens. fzf starts this
// program again for each key press, so the snapshot lives in a folder of files.
const snapshotVariable = "REX_JUMP_SNAPSHOT"

func takeSnapshot(dir, self, claude, index string) error {
	panes, err := listPanes(self)
	if err != nil {
		return err
	}
	conversations := findConversations(claude)
	forEach(len(panes), func(i int) {
		p := &panes[i]
		if c, ok := conversations[p.Block]; ok {
			if c.Name != "" {
				p.Name = c.Name
			}
			if path, err := updateIndex(index, c.ID, c.Transcript); err == nil {
				p.Index = path
				return
			}
		}
		// Rex keeps a terminal's scrollback, except for a program that draws on
		// the alternate screen. Claude Code does, so its index comes first.
		text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim", "--unwrap")
		if err != nil {
			return
		}
		path := filepath.Join(dir, fmt.Sprintf("screen-%d.txt", i))
		if os.WriteFile(path, text, 0o600) == nil {
			p.Screen = path
		}
	})
	data, err := json.Marshal(panes)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "panes.json"), data, 0o600)
}

func loadSnapshot(dir string) ([]pane, error) {
	data, err := os.ReadFile(filepath.Join(dir, "panes.json"))
	if err != nil {
		return nil, err
	}
	var panes []pane
	return panes, json.Unmarshal(data, &panes)
}
