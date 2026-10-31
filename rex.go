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
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxConcurrent = 8

// pane is one terminal in Rex, as it was when the picker opened.
type pane struct {
	Session      string
	SessionLabel string
	Window       string
	WindowLabel  string
	Block        string
	Name         string
	Cwd          string
	// Here says if the pane is in the window of the picker. It is behind the picker,
	// so it is not worth suggesting.
	Here bool
	// Conversation says if a Claude Code session runs in the pane.
	Conversation bool
	// Active is the last change of the conversation.
	Active time.Time
	// Text is what the search reads: the conversation, or the scrollback of any
	// other pane. The newest comes first.
	Text []entry
}

// loadPanes lists the panes, with the names of their conversations. It is enough
// to show the picker.
func loadPanes(self, claude string) ([]pane, map[string]conversation, error) {
	panes, err := listPanes(self)
	if err != nil {
		return nil, nil, err
	}
	conversations := findConversations(claude)
	for i := range panes {
		if c, ok := conversations[panes[i].Block]; ok {
			panes[i].Conversation, panes[i].Active = true, c.Active
			if c.Name != "" {
				panes[i].Name = c.Name
			}
		}
	}
	return panes, conversations, nil
}

// loadText returns the panes with their text. It can take seconds, if a
// conversation was not indexed before.
func loadText(panes []pane, conversations map[string]conversation, index string) []pane {
	loaded := append([]pane(nil), panes...)
	forEach(len(loaded), func(i int) {
		p := &loaded[i]
		if c, ok := conversations[p.Block]; ok {
			if path, err := updateIndex(index, c.ID, c.Transcript); err == nil {
				p.Text = messageEntries(path)
				return
			}
		}
		// Rex keeps a terminal's scrollback, except for a program that draws on
		// the alternate screen. Claude Code does, so its index comes first.
		if text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim", "--unwrap"); err == nil {
			p.Text = screenEntries(string(text))
		}
	})
	return loaded
}

// captureScreen reads what a pane shows now, with its colors.
func captureScreen(p pane) (string, error) {
	text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim", "--format=vt")
	return screenText(string(text)), err
}

var (
	controlSequence = regexp.MustCompile(`\x1b\[([0-9;:?]*)([ -/]*[@-~])`)
	otherEscape     = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\\\)?|\x1b[^\[]`)
)

// screenText keeps the colors of a screen, and turns cursor moves to the right
// into spaces. Nothing else passes, so that a pane cannot move the picker's cursor.
// Each line ends with a reset and starts with the colors that are still on, so that
// each line can be cut alone.
func screenText(vt string) string {
	vt = otherEscape.ReplaceAllString(vt, "")
	active := ""
	lines := strings.Split(strings.TrimRight(vt, "\n"), "\n")
	for i, line := range lines {
		var out strings.Builder
		out.WriteString(active)
		end := 0
		for _, at := range controlSequence.FindAllStringSubmatchIndex(line, -1) {
			out.WriteString(clean(line[end:at[0]]))
			end = at[1]
			parameters, kind := line[at[2]:at[3]], line[at[4]:at[5]]
			switch kind {
			case "m":
				if parameters == "" || parameters == "0" {
					active = ""
				} else {
					active += line[at[0]:at[1]]
				}
				out.WriteString(line[at[0]:at[1]])
			case "C":
				count, err := strconv.Atoi(parameters)
				if err != nil {
					count = 1
				}
				out.WriteString(strings.Repeat(" ", count))
			}
		}
		out.WriteString(clean(line[end:]) + "\x1b[0m")
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n") + "\n"
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
		here := false
		first := len(panes)
		for _, layer := range window.Layers {
			for _, block := range layer.Blocks {
				if block.ID == self {
					here = true
				}
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
		for i := first; i < len(panes); i++ {
			panes[i].Here = here
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
	Active     time.Time
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
		if info, err := os.Stat(transcripts[0]); err == nil {
			found[i].Active = info.ModTime()
		}
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
