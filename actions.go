package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxActionsInAll keeps actions from pushing conversations out of view when a word,
// such as "session", is in many titles. The Actions tab lists them all.
const maxActionsInAll = 4

// action is something Rex can do with one key, such as splitting a pane.
type action struct {
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

// loadActions lists what the client offers, then what the server offers. Without a
// client, there are only the server's.
func loadActions(client string) ([]action, error) {
	var actions []action
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

func readActions(args ...string) ([]action, error) {
	out, err := runRex(args...)
	if err != nil {
		return nil, err
	}
	var list struct {
		Actions []action `json:"actions"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("rex actions: %w", err)
	}
	var kept []action
	for _, a := range list.Actions {
		if a.Title != "" && len(a.Args.Required) == 0 && !notListed[a.Name] {
			kept = append(kept, a)
		}
	}
	return kept, nil
}

// searchActions returns the actions with the words in their titles, then those with
// them in their keywords. An empty query returns them all.
func searchActions(actions []action, query string) []result {
	words := strings.Fields(lowerRunes(query))
	var titled, other []result
	for i := range actions {
		a := &actions[i]
		r := result{Action: a, Reason: reasonAction}
		switch title := lowerRunes(a.Title); {
		case containsAll(title, words):
			titled = append(titled, r)
		case containsAll(title+" "+lowerRunes(a.Keywords), words):
			other = append(other, r)
		}
	}
	return append(titled, other...)
}

// runAction runs an action on the pane behind the picker. The floating layer holds
// the focus until the picker exits, so the pane gets it back first.
func runAction(name, session, block, client string) error {
	var args []string
	if client != "" {
		args = append(args, "-C", client)
	}
	args = append(args, "do", name)
	if block != "" {
		var ignored map[string]any
		if err := callRex("session.focus_block", map[string]string{"session_id": session, "block_id": block}, &ignored); err != nil {
			return err
		}
		args = append(args, "-s", session, "-b", block)
	}
	_, err := runRex(args...)
	return err
}
