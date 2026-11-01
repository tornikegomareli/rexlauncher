package rex

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/parallel"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// Pane is one terminal in Rex, as it was when the picker opened.
type Pane struct {
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
	// Conversation is the Claude Code session that runs in the Pane, if one does.
	Conversation *conversations.Conversation
	// Text is what the search reads: the conversation, or the scrollback of any
	// other pane. The newest comes first.
	Text []textutil.Entry
}

// Active is the last change of the pane's conversation. It is zero without one.
func (p Pane) Active() time.Time {
	if p.Conversation == nil {
		return time.Time{}
	}
	return p.Conversation.Active
}

// Title is the name of the pane. Rex can have none for it.
func (p Pane) Title() string {
	name := "pane"
	for _, candidate := range []string{p.Name, p.WindowLabel, p.SessionLabel} {
		if candidate != "" {
			name = candidate
			break
		}
	}
	if p.Conversation != nil {
		return name // A conversation has a name of its own.
	}
	// A shell is often just "shell", so where it is tells it from the others.
	for _, place := range []string{p.WindowLabel, p.SessionLabel} {
		if place != "" && place != name {
			name = place + " · " + name
		}
	}
	return name
}

// CheckServer makes sure rex reaches the server this pane is on. A terminal on a
// server started with its own --listen has no REX_SERVER, so rex would call the
// default server, and the picker would go to panes that belong to another.
func CheckServer(session string) error {
	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := Call("session.list", struct{}{}, &list); err != nil {
		return err
	}
	for _, s := range list.Sessions {
		if s.ID == session {
			return nil
		}
	}
	return fmt.Errorf("rex reached a server that does not hold this pane's session (%q). Set REX_SERVER", session)
}

// ListPanes returns every terminal on the server except self, in Rex's order.
func ListPanes(self string) ([]Pane, error) {
	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := Call("session.list", struct{}{}, &list); err != nil {
		return nil, err
	}

	found := make([][]Pane, len(list.Sessions))
	errs := make([]error, len(list.Sessions))
	parallel.ForEach(len(list.Sessions), func(i int) {
		found[i], errs[i] = sessionPanes(list.Sessions[i].ID, self)
	})
	// A session can close while this runs, so one failure only leaves it out. If
	// none can be read, something else is wrong.
	var panes []Pane
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
	parallel.ForEach(len(panes), func(i int) { describePane(&panes[i]) })
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

func sessionPanes(session, self string) ([]Pane, error) {
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
	if err := Call("session.view", map[string]string{"session_id": session}, &view); err != nil {
		return nil, err
	}
	var panes []Pane
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
				panes = append(panes, Pane{
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
func describePane(p *Pane) {
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

func blockMethod(p Pane, method string, output any) error {
	return Call("block.method", map[string]string{
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

// GoTo shows a pane. The server focuses the block in its session, and the app
// then shows that session.
func GoTo(session, window, block, client string) error {
	var ignored map[string]any
	err := Call("session.focus_block", map[string]string{"session_id": session, "block_id": block}, &ignored)
	if err != nil {
		if strings.Contains(err.Error(), "not_found") {
			return errors.New("that pane has closed")
		}
		return err
	}
	selectArgs := []string{"do", "session.select", "session_id=" + session, "window_id=" + window}
	if client != "" {
		if _, err := Run(append([]string{"-C", client}, selectArgs...)...); err == nil {
			return nil
		}
		// The client that opened the picker can be gone: "rex do rexlauncher" is
		// one. Rex then uses the app, if one is connected.
	}
	_, err = Run(selectArgs...)
	return err
}
