package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const maxConcurrent = 8

// pane is one terminal in Rex, as it was when the picker opened.
type pane struct {
	Session      string `json:"session"`
	SessionLabel string `json:"session_label"`
	Window       string `json:"window"`
	WindowLabel  string `json:"window_label"`
	Block        string `json:"block"`
	Name         string `json:"name"`
	Cwd          string `json:"cwd"`
	// Index is the path of the conversation's index, for a Claude Code pane.
	Index string `json:"index,omitempty"`
	// Screen is the path of the captured text, for any other pane.
	Screen string `json:"screen,omitempty"`
}

// runRex calls rex. It never starts a server: the picker runs inside one.
func runRex(args ...string) ([]byte, error) {
	path, err := exec.LookPath("rex")
	if err != nil {
		// A terminal in Rex names the folder even if PATH lost it.
		path = filepath.Join(os.Getenv("REX_BIN_DIR"), "rex")
	}
	cmd := exec.Command(path, append([]string{"--autostart=false"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rex %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func callRex(method string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	out, err := runRex("api", "call", method, string(body))
	if err != nil {
		return err
	}
	return json.Unmarshal(out, output)
}

// forEach runs work for 0..n-1, a few at a time.
func forEach(n int, work func(i int)) {
	limit := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			work(i)
		}()
	}
	wg.Wait()
}

// checkServer makes sure rex reaches the server this pane is on. A terminal on a
// server started with its own --listen has no REX_SERVER, so rex would call the
// default server, and the picker would go to panes that belong to another.
func checkServer(session string) error {
	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := callRex("session.list", struct{}{}, &list); err != nil {
		return err
	}
	for _, s := range list.Sessions {
		if s.ID == session {
			return nil
		}
	}
	return fmt.Errorf("rex reached a server that does not hold this pane's session (%q). Set REX_SERVER", session)
}

// listPanes returns every terminal on the server except self, in Rex's order.
func listPanes(self string) ([]pane, error) {
	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := callRex("session.list", struct{}{}, &list); err != nil {
		return nil, err
	}

	found := make([][]pane, len(list.Sessions))
	errs := make([]error, len(list.Sessions))
	forEach(len(list.Sessions), func(i int) {
		found[i], errs[i] = sessionPanes(list.Sessions[i].ID, self)
	})
	var panes []pane
	for i := range found {
		if errs[i] != nil {
			return nil, errs[i]
		}
		panes = append(panes, found[i]...)
	}
	forEach(len(panes), func(i int) { describePane(&panes[i]) })
	return panes, nil
}

func sessionPanes(session, self string) ([]pane, error) {
	var view struct {
		Label   string `json:"label"`
		Windows []struct {
			ID     string `json:"window_id"`
			Label  string `json:"label"`
			Layers []struct {
				Blocks []struct {
					ID      string `json:"block_id"`
					Label   string `json:"label"`
					Creator string `json:"creator_name"`
				} `json:"blocks"`
			} `json:"layers"`
		} `json:"windows"`
	}
	if err := callRex("session.view", map[string]string{"session_id": session}, &view); err != nil {
		return nil, err
	}
	var panes []pane
	for _, window := range view.Windows {
		for _, layer := range window.Layers {
			for _, block := range layer.Blocks {
				if block.ID == self || block.Creator != "com.superlogical.terminal" {
					continue
				}
				panes = append(panes, pane{
					Session:      session,
					SessionLabel: view.Label,
					Window:       window.ID,
					WindowLabel:  window.Label,
					Block:        block.ID,
					Name:         block.Label,
				})
			}
		}
	}
	return panes, nil
}

// describePane adds the terminal's title and folder. A pane can close at any
// moment, so a failure only leaves these out.
func describePane(p *pane) {
	var title struct {
		Title string `json:"title"`
	}
	if blockMethod(*p, "title", &title) == nil && title.Title != "" {
		p.Name = title.Title
	}
	var pwd struct {
		Pwd string `json:"pwd"`
	}
	if blockMethod(*p, "pwd", &pwd) == nil {
		p.Cwd = pathFromFileURL(pwd.Pwd)
	}
}

func blockMethod(p pane, method string, output any) error {
	return callRex("block.method", map[string]string{
		"session_id": p.Session,
		"block_id":   p.Block,
		"method":     method,
	}, output)
}

func pathFromFileURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" {
		return value
	}
	return parsed.Path
}

// conversation is a live Claude Code session.
type conversation struct {
	ID         string
	Name       string
	Transcript string
}

var rexBlockVariable = regexp.MustCompile(`\bREX_BLOCK=(block:[0-9a-f-]+)`)

// findConversations returns the live Claude Code sessions by the Rex block they
// run in. Each one has a file named after its process, and the process's
// environment names its block.
func findConversations(claudeDir string) map[string]conversation {
	files, _ := filepath.Glob(filepath.Join(claudeDir, "sessions", "*.json"))
	blocks := make([]string, len(files))
	found := make([]conversation, len(files))
	forEach(len(files), func(i int) {
		data, err := os.ReadFile(files[i])
		if err != nil {
			return
		}
		var session struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			Name      string `json:"name"`
		}
		if json.Unmarshal(data, &session) != nil || session.PID == 0 || session.SessionID == "" {
			return
		}
		env, err := exec.Command("ps", "eww", "-p", fmt.Sprint(session.PID)).Output()
		if err != nil {
			return // The process is gone, and Claude Code left its file behind.
		}
		match := rexBlockVariable.FindSubmatch(env)
		if match == nil {
			return
		}
		// A session's folder comes from where it started, so look in all of them.
		transcripts, _ := filepath.Glob(filepath.Join(claudeDir, "projects", "*", session.SessionID+".jsonl"))
		if len(transcripts) == 0 {
			return
		}
		blocks[i] = string(match[1])
		found[i] = conversation{ID: session.SessionID, Name: session.Name, Transcript: transcripts[0]}
	})
	conversations := map[string]conversation{}
	for i, block := range blocks {
		if block != "" {
			conversations[block] = found[i]
		}
	}
	return conversations
}

func claudeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "rex-jump")
	return dir, os.MkdirAll(dir, 0o700)
}
