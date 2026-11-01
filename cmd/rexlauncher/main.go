// rexlauncher is the program behind the Rex Launcher plugin for Rex. It finds a
// pane by name, or by the words said in the conversation inside it, and goes there.
package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/picker"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/search"
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

// pick shows the picker, and then goes to the pane or runs the action that the person
// chose. Block is the pane that was focused when the picker opened.
func pick(client, block string) error {
	session := os.Getenv("REX_SESSION")
	if err := rex.CheckServer(session); err != nil {
		return err
	}
	index, err := conversations.CacheDir()
	if err != nil {
		return err
	}
	choice, err := picker.Run(picker.Sources{
		Panes: func() ([]rex.Pane, error) {
			return search.LoadPanes(os.Getenv("REX_BLOCK"), conversations.ClaudeDir())
		},
		Text: func(panes []rex.Pane) []rex.Pane {
			conversations.Prune(index)
			return search.LoadText(panes, index)
		},
		Screen:  rex.Capture,
		Actions: func() ([]rex.Action, error) { return rex.LoadActions(client) },
	})
	if err != nil {
		return err
	}
	switch {
	case choice.Action != nil:
		return rex.RunAction(choice.Action.Name, session, block, client)
	case choice.Pane != nil:
		return rex.GoTo(choice.Pane.Session, choice.Pane.Window, choice.Pane.Block, client)
	}
	return nil
}
