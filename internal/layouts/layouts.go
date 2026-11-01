// Package layouts saves how a session's windows and panes are arranged, and builds a new
// session from that later.
package layouts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/rex"
)

// Pane is what is needed to start one pane again. Only a folder and a conversation are
// kept: running anything else again could repeat what it did.
type Pane struct {
	Title        string `json:"title,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Conversation string `json:"conversation,omitempty"`
}

// Node is a pane, or a split of two nodes.
type Node struct {
	Pane      *Pane   `json:"pane,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	Before    *Node   `json:"before,omitempty"`
	After     *Node   `json:"after,omitempty"`
}

// Window is one window of a layout.
type Window struct {
	Label string `json:"label,omitempty"`
	Root  Node   `json:"root"`
}

// Layout is a saved session.
type Layout struct {
	Name    string    `json:"name"`
	SavedAt time.Time `json:"saved_at"`
	Windows []Window  `json:"windows"`
}

// Panes lists the panes in order.
func (l Layout) Panes() []Pane {
	var panes []Pane
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Pane != nil {
			panes = append(panes, *n.Pane)
		}
		walk(n.Before)
		walk(n.After)
	}
	for i := range l.Windows {
		walk(&l.Windows[i].Root)
	}
	return panes
}

// Dir is where layouts are kept.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "rexlauncher", "layouts"), nil
}

func fileOf(dir, name string) string {
	return filepath.Join(dir, url.PathEscape(name)+".json") // One file, whatever the name holds.
}

// Save writes a layout. A layout with the same name is replaced.
func Save(dir string, l Layout) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".writing-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // No effect once the rename has happened.
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), fileOf(dir, l.Name))
}

// List returns the layouts, the newest first. It returns those it could read, with an
// error that names any it could not.
func List(dir string) ([]Layout, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var found []Layout
	var failures []error
	for _, path := range paths {
		var l Layout
		data, err := os.ReadFile(path)
		if err == nil {
			err = json.Unmarshal(data, &l)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", filepath.Base(path), err))
			continue
		}
		found = append(found, l)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].SavedAt.After(found[j].SavedAt) })
	return found, errors.Join(failures...)
}

// Delete removes a layout.
func Delete(dir, name string) error {
	return os.Remove(fileOf(dir, name))
}

var defaultWindowLabel = regexp.MustCompile(`^Window [0-9a-f]{8}$`)

// Capture reads how a session is arranged. Floating layers stay out, so the picker does
// not save itself.
func Capture(name, session string, panes []rex.Pane) (Layout, error) {
	type viewNode struct {
		BlockID string `json:"block_id"`
		Split   *struct {
			Direction string          `json:"direction"`
			Ratio     float64         `json:"ratio"`
			Before    json.RawMessage `json:"before"`
			After     json.RawMessage `json:"after"`
		} `json:"split"`
	}
	var view struct {
		Windows []struct {
			Label  string `json:"label"`
			Layers []struct {
				Kind   string          `json:"kind"`
				Layout json.RawMessage `json:"layout"`
			} `json:"layers"`
		} `json:"windows"`
	}
	if err := rex.Call("session.view", map[string]string{"session_id": session}, &view); err != nil {
		return Layout{}, err
	}
	byBlock := map[string]rex.Pane{}
	for _, p := range panes {
		byBlock[p.Block] = p
	}
	var read func(raw json.RawMessage) (Node, error)
	read = func(raw json.RawMessage) (Node, error) {
		var v viewNode
		if err := json.Unmarshal(raw, &v); err != nil {
			return Node{}, err
		}
		if v.Split == nil {
			p := byBlock[v.BlockID]
			pane := &Pane{Title: p.Title(), Cwd: p.Cwd}
			if p.Conversation != nil {
				pane.Conversation = p.Conversation.ID
			}
			return Node{Pane: pane}, nil
		}
		before, err := read(v.Split.Before)
		if err != nil {
			return Node{}, err
		}
		after, err := read(v.Split.After)
		if err != nil {
			return Node{}, err
		}
		return Node{Direction: v.Split.Direction, Ratio: v.Split.Ratio, Before: &before, After: &after}, nil
	}
	l := Layout{Name: name, SavedAt: time.Now()}
	for _, w := range view.Windows {
		for _, layer := range w.Layers {
			if layer.Kind != "tiled" {
				continue
			}
			root, err := read(layer.Layout)
			if err != nil {
				return Layout{}, err
			}
			window := Window{Root: root}
			if !defaultWindowLabel.MatchString(w.Label) {
				window.Label = w.Label
			}
			l.Windows = append(l.Windows, window)
		}
	}
	return l, nil
}

var conversationID = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// creation is the input of session.create. A pane with a conversation resumes it through
// the person's shell, the way they start Claude Code, and leaves a shell when it ends. A
// conversation that is still open is left alone, because two copies of Claude Code on one
// conversation would write over each other.
func creation(l Layout, shell string, live map[string]bool) map[string]any {
	var build func(n Node) map[string]any
	build = func(n Node) map[string]any {
		if n.Pane == nil {
			split := map[string]any{"direction": n.Direction, "before": build(*n.Before), "after": build(*n.After)}
			if n.Ratio > 0 {
				split["ratio"] = n.Ratio
			}
			return map[string]any{"split": split}
		}
		options := map[string]any{"cwd": n.Pane.Cwd}
		if id := n.Pane.Conversation; conversationID.MatchString(id) && !live[id] {
			options["shell"] = "none"
			options["command"] = []string{shell, "-ic", fmt.Sprintf(`claude --resume %s; exec %s -l`, id, shell)}
		}
		return map[string]any{"block": map[string]any{"flavor": "com.superlogical.terminal.shell", "options": options}}
	}
	var windows []map[string]any
	for _, w := range l.Windows {
		window := map[string]any{"layout": build(w.Root)}
		if w.Label != "" {
			window["window_label"] = w.Label
		}
		windows = append(windows, window)
	}
	return map[string]any{"label": l.Name, "initial_windows": windows}
}

// Restore builds a session from a layout and returns its ID. If a session already has the
// layout's name, it returns that one, so a second restore does not start everything twice.
func Restore(l Layout, shell string, live map[string]bool) (string, error) {
	var list struct {
		Sessions []struct {
			ID    string `json:"session_id"`
			Label string `json:"label"`
		} `json:"sessions"`
	}
	if err := rex.Call("session.list", struct{}{}, &list); err != nil {
		return "", err
	}
	for _, s := range list.Sessions {
		if strings.EqualFold(s.Label, l.Name) {
			return s.ID, nil
		}
	}
	var created struct {
		ID string `json:"session_id"`
	}
	if err := rex.Call("session.create", creation(l, shell, live), &created); err != nil {
		return "", err
	}
	return created.ID, nil
}
