package main

import (
	"bytes"
	"context"
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

// rexTimeout is how long a rex command can take. Without a limit, one that hangs would
// freeze the preview, or leave the picker reading conversations for ever.
var rexTimeout = 15 * time.Second

// captureScreen reads what a pane shows now, with its colors.
func captureScreen(p pane) (string, error) {
	text, err := runRex("capture", "-s", p.Session, "-b", p.Block, "--trim", "--format=vt")
	return screenText(string(text)), err
}

var (
	controlSequence = regexp.MustCompile(`\x1b\[([0-9;:?<=>]*)([ -/]*[@-~])`)
	// A string such as a title or a link, a choice of character set, and the rest.
	otherEscape = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)?|\x1b[ -/][0-~]|\x1b[^\[\n]`)
)

// maxCarriedColor bounds the colors carried from line to line. A program that only
// uses partial resets, such as 39m, would otherwise add to them on every line, and
// the preview reads the screen again and again.
const maxCarriedColor = 256

// maxCursorMove bounds the gap one cursor move can make. No screen is this wide, and
// a pane's text decides the number.
const maxCursorMove = 4096

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
				if strings.ContainsAny(parameters, "<=>?") {
					continue // A private mode, and not a color.
				}
				if parameters == "" || parameters == "0" {
					active = ""
				} else {
					active = lastColors(active + line[at[0]:at[1]])
				}
				out.WriteString(line[at[0]:at[1]])
			case "C":
				count, err := strconv.Atoi(parameters)
				if err != nil {
					count = 1
				}
				out.WriteString(strings.Repeat(" ", min(count, maxCursorMove)))
			}
		}
		out.WriteString(clean(line[end:]) + "\x1b[0m")
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n") + "\n"
}

// lastColors keeps the newest colors, starting at a whole sequence.
func lastColors(colors string) string {
	if len(colors) <= maxCarriedColor {
		return colors
	}
	rest := colors[len(colors)-maxCarriedColor:]
	at := strings.IndexByte(rest, 0x1b)
	if at < 0 {
		return "" // One sequence longer than the limit. Half of it would print as text.
	}
	return rest[at:]
}

// runRex calls rex. It never starts a server: the picker runs inside one.
func runRex(args ...string) ([]byte, error) {
	path, err := exec.LookPath("rex")
	if err != nil {
		// A terminal in Rex names the folder even if PATH lost it.
		path = filepath.Join(os.Getenv("REX_BIN_DIR"), "rex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), rexTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, append([]string{"--autostart=false"}, args...)...)
	cmd.WaitDelay = time.Second // Do not wait for what it started, once it was ended.
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
	// A session can close while this runs, so one failure only leaves it out. If
	// none can be read, something else is wrong.
	var panes []pane
	var failure error
	read := 0
	for i := range found {
		if errs[i] != nil {
			failure = errs[i]
			continue
		}
		read++
		panes = append(panes, found[i]...)
	}
	if read == 0 && failure != nil {
		return nil, failure
	}
	forEach(len(panes), func(i int) { describePane(&panes[i]) })
	return panes, nil
}

var defaultWindowLabel = regexp.MustCompile(`^Window [0-9a-f]{8}$`)

// windowName is the name a person gave a window. Rex makes one up for the others, and
// it tells nobody anything.
func windowName(label string) string {
	if defaultWindowLabel.MatchString(label) {
		return ""
	}
	return label
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
					WindowLabel:  windowName(window.Label),
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
	if p.Cwd != "" {
		return
	}
	// Only a shell set up for it reports its folder. Rex can also ask the system.
	var process struct {
		Foreground struct {
			Cwd string `json:"cwd"`
		} `json:"foreground"`
	}
	if blockMethod(*p, "process", &process) == nil {
		p.Cwd = process.Foreground.Cwd
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
