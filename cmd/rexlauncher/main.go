// rexlauncher is the program behind the Rex Launcher plugin for Rex. It finds a
// pane by name, or by the words said in the conversation inside it, and goes there.
package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/picker"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rexlauncher:", err)
		waitForEnter()
		os.Exit(1)
	}
}

// waitForEnter keeps an error on the screen, because the floating layer closes
// when this program exits.
func waitForEnter() {
	fmt.Fprintln(os.Stderr, "Press Enter to close.")
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return
	}
	defer tty.Close()
	bufio.NewReader(tty).ReadString('\n')
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "pick":
		return pick("", "")
	case len(args) == 2 && args[0] == "pick":
		return pick(args[1], "")
	case len(args) == 3 && args[0] == "pick":
		return pick(args[1], args[2])
	}
	return fmt.Errorf("usage: rexlauncher pick [CLIENT [BLOCK]]")
}

// pick shows the picker, and then goes to the pane, runs the action or fills in the skill
// that the person chose. Block is the pane that was focused when the picker opened.
func pick(client, block string) error {
	session := os.Getenv("REX_SESSION")
	if err := rex.CheckServer(session); err != nil {
		return err
	}
	index, err := conversations.CacheDir()
	if err != nil {
		return err
	}
	saved, err := layouts.Dir()
	if err != nil {
		return err
	}
	choice, err := picker.Run(picker.Sources{
		Panes: func() ([]rex.Pane, error) {
			return search.LoadPanes(os.Getenv("REX_BLOCK"), block, conversations.ClaudeDir())
		},
		Text: func(panes []rex.Pane) []rex.Pane {
			conversations.Prune(index)
			return search.LoadText(panes, index)
		},
		Screen:   rex.Capture,
		Actions:  func() ([]rex.Action, error) { return rex.LoadActions(client) },
		Skills:   func() (skills.Installed, error) { return loadSkills(session, block) },
		Statuses: rex.ReadStatuses,
		Answer: func(p rex.Pane, a rex.Answer) error {
			return rex.Deliver(p.Session, p.Block, a)
		},
		Layouts: func() ([]layouts.Layout, error) { return layouts.List(saved) },
		SaveLayout: func(name string, panes []rex.Pane) error {
			l, err := layouts.Capture(name, session, panes)
			if err != nil {
				return err
			}
			return layouts.Save(saved, l)
		},
		DeleteLayout: func(name string) error { return layouts.Delete(saved, name) },
		SessionName: func(panes []rex.Pane) string {
			for _, p := range panes {
				if p.Session == session {
					return p.SessionLabel
				}
			}
			return ""
		},
	})
	if err != nil {
		return err
	}
	switch {
	case choice.Layout != nil:
		return restore(*choice.Layout, client)
	case choice.Skill != nil:
		return rex.Insert(session, block, choice.Skill.Name+" ") // The space ends the name, and arguments can follow.
	case choice.Action != nil:
		return rex.RunAction(choice.Action.Name, session, block, client)
	case choice.Pane != nil:
		return rex.GoTo(choice.Pane.Session, choice.Pane.Window, choice.Pane.Block, client)
	}
	return nil
}

// loadSkills lists the skills of the agent in the pane behind the picker. A picker opened
// with no pane has no agent.
func loadSkills(session, block string) (skills.Installed, error) {
	if block == "" {
		return skills.Installed{}, nil
	}
	program, err := rex.Foreground(session, block)
	if err != nil {
		return skills.Installed{}, err
	}
	return skills.Load(skills.Program{Argv0: program.Argv0, Path: program.Path, Cwd: program.Cwd}, conversations.ClaudeDir())
}

// restore builds a session from a layout and shows it.
func restore(l layouts.Layout, client string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	live := map[string]bool{}
	for _, c := range conversations.Find(conversations.ClaudeDir()) {
		live[c.ID] = true
	}
	id, err := layouts.Restore(l, shell, live)
	if err != nil {
		return err
	}
	return rex.ShowSession(id, client)
}
