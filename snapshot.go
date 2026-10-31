package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A snapshot is what the picker knows, fixed when it opens. fzf starts this
// program again for each key press, so the snapshot lives in a folder of files.
// The list of panes comes first, because it is enough to show the picker. The
// text to search can take seconds, if a conversation was not indexed before.
const (
	snapshotVariable = "REX_JUMP_SNAPSHOT"
	panesFile        = "panes.json"
	contentFile      = "content.json"
)

func takeSnapshot(dir, self, claude, index string) error {
	panes, err := listPanes(self)
	if err != nil {
		return err
	}
	conversations := findConversations(claude)
	for i := range panes {
		if c, ok := conversations[panes[i].Block]; ok && c.Name != "" {
			panes[i].Name = c.Name
		}
	}
	if err := writeSnapshot(dir, panesFile, panes); err != nil {
		return err
	}
	forEach(len(panes), func(i int) {
		p := &panes[i]
		if c, ok := conversations[p.Block]; ok {
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
	return writeSnapshot(dir, contentFile, panes)
}

// writeSnapshot replaces the file in one step, so a reader never sees part of it.
func writeSnapshot(dir, name string, panes []pane) error {
	data, err := json.Marshal(panes)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path+".new", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

// needsContent says if a query searches text, and not only the list of panes.
func needsContent(query string) bool {
	return strings.TrimSpace(query) != ""
}

// loadSnapshot waits for the file it needs. It stops when the folder is gone,
// because the picker removes the folder when it closes.
func loadSnapshot(dir string, withContent bool) ([]pane, error) {
	name := panesFile
	if withContent {
		name = contentFile
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			var panes []pane
			return panes, json.Unmarshal(data, &panes)
		}
		if _, err := os.Stat(dir); err != nil {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
