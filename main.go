// rexlauncher is the program behind the Rex Launcher plugin for Rex. It finds a
// pane by name, or by the words said in the conversation inside it, and goes there.
package main

import (
	"bufio"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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
		return pick("")
	case len(args) == 2 && args[0] == "pick":
		return pick(args[1])
	}
	return fmt.Errorf("usage: rexlauncher pick [CLIENT]")
}

// pick shows the picker, and goes to the pane that the person chose.
func pick(client string) error {
	if err := checkServer(os.Getenv("REX_SESSION")); err != nil {
		return err
	}
	index, err := cacheDir()
	if err != nil {
		return err
	}
	var conversations map[string]conversation
	picker := &model{
		loadPanes: func() ([]pane, error) {
			panes, found, err := loadPanes(os.Getenv("REX_BLOCK"), claudeDir())
			conversations = found
			return panes, err
		},
		loadText: func(panes []pane) []pane {
			pruneIndex(index)
			return loadText(panes, conversations, index)
		},
		capture: captureScreen,
	}
	if _, err := tea.NewProgram(picker, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return err
	}
	if picker.err != nil {
		return picker.err
	}
	if picker.chosen == nil {
		return nil
	}
	return goTo(picker.chosen.Session, picker.chosen.Window, picker.chosen.Block, client)
}

// goTo shows a pane. The server focuses the block in its session, and the app
// then shows that session.
func goTo(session, window, block, client string) error {
	var ignored map[string]any
	err := callRex("session.focus_block", map[string]string{"session_id": session, "block_id": block}, &ignored)
	if err != nil {
		return err
	}
	selectArgs := []string{"do", "session.select", "session_id=" + session, "window_id=" + window}
	if client != "" {
		if _, err := runRex(append([]string{"-C", client}, selectArgs...)...); err == nil {
			return nil
		}
		// The client that opened the picker can be gone: "rex do rexlauncher" is
		// one. Rex then uses the app, if one is connected.
	}
	_, err = runRex(selectArgs...)
	return err
}
