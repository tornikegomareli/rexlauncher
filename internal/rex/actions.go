package rex

import (
	"encoding/json"
	"fmt"
)

// Action is something Rex can do with one key, such as splitting a pane.
type Action struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Keywords string `json:"keywords"`
	Args     struct {
		Required []string `json:"required"`
	} `json:"args"`
}

// notListed are actions that close or restart something. Enter runs the first row, and
// a word typed to find a conversation can match their titles. Rex's palette has them.
var notListed = map[string]bool{
	"rexlauncher":           true,
	"pane.close":            true,
	"session.close":         true,
	"client.close":          true,
	"client.window.close":   true,
	"client.tab.close":      true,
	"client.host.remove":    true,
	"client.server.restart": true,
	"client.update.accept":  true,
}

// LoadActions lists what the client offers, then what the server offers. Without a
// client, there are only the server's.
func LoadActions(client string) ([]Action, error) {
	var actions []Action
	if client != "" {
		found, err := readActions("-C", client, "actions", "--json")
		if err != nil {
			return nil, err
		}
		actions = found
	}
	found, err := readActions("actions", "--json")
	if err != nil {
		return nil, err
	}
	return append(actions, found...), nil
}

func readActions(args ...string) ([]Action, error) {
	out, err := Run(args...)
	if err != nil {
		return nil, err
	}
	var list struct {
		Actions []Action `json:"actions"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("rex actions: %w", err)
	}
	var kept []Action
	for _, a := range list.Actions {
		if a.Title != "" && len(a.Args.Required) == 0 && !notListed[a.Name] {
			kept = append(kept, a)
		}
	}
	return kept, nil
}

// RunAction runs an action on the pane behind the picker. The floating layer holds
// the focus until the picker exits, so the pane gets it back first.
func RunAction(name, session, block, client string) error {
	var args []string
	if client != "" {
		args = append(args, "-C", client)
	}
	args = append(args, "do", name)
	if block != "" {
		var ignored map[string]any
		if err := Call("session.focus_block", map[string]string{"session_id": session, "block_id": block}, &ignored); err != nil {
			return err
		}
		args = append(args, "-s", session, "-b", block)
	}
	_, err := Run(args...)
	return err
}
