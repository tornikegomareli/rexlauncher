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
	// Status is what the program in the pane says it is doing.
	Status Status
	// From says if this is the pane the picker opened from.
	From bool
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

// ReadStatuses asks every pane what its program is doing, by block. A pane that closed
// is left out.
func ReadStatuses(panes []Pane) map[string]Status {
	found := make([]Status, len(panes))
	read := make([]bool, len(panes))
	parallel.ForEach(len(panes), func(i int) {
		status, err := ReadStatus(panes[i].Session, panes[i].Block)
		found[i], read[i] = status, err == nil
	})
	statuses := map[string]Status{}
	for i := range panes {
		if read[i] {
			statuses[panes[i].Block] = found[i]
		}
	}
	return statuses
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
	if status, err := ReadStatus(p.Session, p.Block); err == nil {
		p.Status = status
	}
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

// Program is what runs in the front of a pane.
type Program struct {
	Argv0 string
	Path  string // The path it was started by.
	Cwd   string
}

// Foreground names what runs in the front of a pane. A pane with nothing running has
// no name.
func Foreground(session, block string) (Program, error) {
	var process struct {
		Foreground struct {
			Argv0       string `json:"argv0"`
			InvokedPath string `json:"invoked_path"`
			Cwd         string `json:"cwd"`
		} `json:"foreground"`
	}
	if err := blockMethod(Pane{Session: session, Block: block}, "process", &process); err != nil {
		return Program{}, err
	}
	f := process.Foreground
	return Program{Argv0: f.Argv0, Path: f.InvokedPath, Cwd: f.Cwd}, nil
}

// Insert types text in a pane and leaves it for the person to finish, so it never sends
// Enter. The floating layer holds the focus until the picker exits, so the pane gets it
// back first.
func Insert(session, block, text string) error {
	var ignored map[string]any
	if err := Call("session.focus_block", map[string]string{"session_id": session, "block_id": block}, &ignored); err != nil {
		return err
	}
	_, err := Run("send", "-s", session, "-b", block, text)
	return err
}

// What a program reports it is doing, from the Program Status Protocol.
const (
	StateIdle    = "idle"
	StateWorking = "working"
	StateDone    = "done"
	StateBlocked = "blocked" // Waiting for the person.
	StateError   = "error"
)

// Status is what the program in a pane says it is doing. A pane whose program reports
// nothing has none, and its State is empty.
type Status struct {
	State string
	// Kind is what a blocked program waits for: permission, question or auth.
	Kind     string
	App      string
	Message  string
	Progress *int
	At       time.Time
}

// statusRank orders states by how much they need the person.
var statusRank = map[string]int{StateIdle: 0, StateDone: 1, StateWorking: 2, StateError: 3, StateBlocked: 4}

// ReadStatus returns the most urgent of what a pane reports, and of equals the newest.
// The protocol says to treat a state or kind it does not name as idle or as none.
func ReadStatus(session, block string) (Status, error) {
	var reply struct {
		Records []struct {
			State     string    `json:"state"`
			Kind      string    `json:"kind"`
			App       string    `json:"effective_app"`
			RawApp    string    `json:"app"`
			Msg       string    `json:"msg"`
			Progress  *int      `json:"progress"`
			UpdatedAt time.Time `json:"updated_at"`
		} `json:"records"`
	}
	if err := blockMethod(Pane{Session: session, Block: block}, "program_status", &reply); err != nil {
		return Status{}, err
	}
	var best Status
	for _, r := range reply.Records {
		s := Status{State: r.State, Kind: r.Kind, App: r.App, Message: r.Msg, Progress: r.Progress, At: r.UpdatedAt}
		if s.App == "" {
			s.App = r.RawApp
		}
		if _, known := statusRank[s.State]; !known {
			s.State = StateIdle
		}
		if s.Kind != "permission" && s.Kind != "question" && s.Kind != "auth" {
			s.Kind = ""
		}
		if best.State == "" || statusRank[s.State] > statusRank[best.State] ||
			statusRank[s.State] == statusRank[best.State] && s.At.After(best.At) {
			best = s
		}
	}
	return best, nil
}

// Needs says if the program waits for the person, or failed.
func (s Status) Needs() bool {
	return s.State == StateBlocked || s.State == StateError
}

// Answer is what a person tells an agent from the picker: one key for a menu, or text for
// its prompt. Seen is the status the person was looking at.
type Answer struct {
	Key  string
	Text string
	Seen Status
}

// Deliver gives an agent an answer, if it still waits for that answer. The person chose
// from what the screen showed, and the agent may have moved on since: a key for one
// permission prompt must not land on the next one. Text never goes to a blocked agent,
// because the Enter after it would pick a menu's default option, and a password prompt
// gets nothing, because a password does not belong in a search box.
func Deliver(session, block string, a Answer) error {
	now, err := ReadStatus(session, block)
	if err != nil {
		return err
	}
	if now.State != a.Seen.State || now.Kind != a.Seen.Kind || now.Message != a.Seen.Message || !now.At.Equal(a.Seen.At) {
		return errors.New("the agent moved on, so nothing was sent")
	}
	if now.Kind == "auth" {
		return errors.New("it wants a password, so go to its pane to type it")
	}
	if a.Key != "" {
		return PressKey(session, block, a.Key)
	}
	if now.State == StateBlocked {
		return errors.New("it waits on a menu, so nothing was sent")
	}
	return SendAnswer(session, block, a.Text)
}

// SendAnswer puts text in a pane's prompt and sends it. A paste keeps a newline in the
// text from acting as Enter.
func SendAnswer(session, block, text string) error {
	var ignored map[string]any
	err := Call("block.event", map[string]any{
		"session_id": session,
		"block_id":   block,
		"event":      map[string]any{"type": "paste", "payload": map[string]string{"text": text}},
	}, &ignored)
	if err != nil {
		return err
	}
	return PressKey(session, block, "enter")
}

// PressKey sends one key to a pane.
func PressKey(session, block, key string) error {
	_, err := Run("send-key", "-s", session, "-b", block, key)
	return err
}
