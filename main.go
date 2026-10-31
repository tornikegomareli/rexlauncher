// rex-jump is the program behind the Jump Anywhere plugin for Rex. It finds a
// pane by name, or by the words said in the conversation inside it, and goes there.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-jump:", err)
		waitForEnter()
		os.Exit(1)
	}
}

// waitForEnter keeps an error on the screen, because the floating layer closes
// when this program exits.
func waitForEnter() {
	fmt.Fprintln(os.Stderr, "Press Enter to close.")
	// Not stdin: fzf does not hand it to the command it becomes.
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return
	}
	defer tty.Close()
	bufio.NewReader(tty).ReadString('\n')
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: rex-jump pick [CLIENT]")
	}
	switch command, rest := args[0], args[1:]; {
	case command == "pick" && len(rest) <= 1:
		client := ""
		if len(rest) == 1 {
			client = rest[0]
		}
		return pick(client)
	case command == "search" && len(rest) == 1:
		panes, err := loadSnapshot(os.Getenv(snapshotVariable), needsContent(rest[0]))
		if err != nil {
			return err
		}
		for _, r := range search(panes, rest[0]) {
			fmt.Println(r.row())
		}
		return nil
	case command == "preview" && len(rest) == 4:
		panes, err := loadSnapshot(os.Getenv(snapshotVariable), needsContent(rest[3]))
		if err != nil {
			return err
		}
		line, _ := strconv.Atoi(rest[2])
		fmt.Print(preview(panes, rest[0], rest[1], line, rest[3]))
		return nil
	case command == "go" && len(rest) == 4:
		if err := goTo(rest[0], rest[1], rest[2], rest[3]); err != nil {
			// fzf exits with this program, and pick would report it a second time.
			fmt.Fprintln(os.Stderr, "rex-jump:", err)
			waitForEnter()
		}
		return nil
	}
	return fmt.Errorf("unknown command: %s", strings.Join(args, " "))
}

// pick shows the picker. fzf only draws here: this program does the matching,
// because its results are not all lines of one list.
func pick(client string) error {
	fzf, err := exec.LookPath("fzf")
	if err != nil {
		return fmt.Errorf("this plugin needs fzf. Install it, for example with: brew install fzf")
	}
	if err := checkServer(os.Getenv("REX_SESSION")); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	snapshot, err := os.MkdirTemp("", "rex-jump")
	if err != nil {
		return err
	}
	defer os.RemoveAll(snapshot)

	// The snapshot holds terminal text. Remove it even if the layer is closed.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		<-signals
		os.RemoveAll(snapshot)
		os.Exit(1)
	}()

	index, err := cacheDir()
	if err != nil {
		return err
	}

	me := shellQuote(self)
	cmd := exec.Command(fzf,
		"--disabled", "--ansi", "--layout=reverse", "--no-sort",
		"--delimiter=\t", "--with-nth=6..",
		"--prompt=Jump> ",
		"--header=Pane names and words said in conversations. Enter goes there.",
		"--preview="+me+" preview {3} {4} {5} {q}",
		"--preview-window=right,40%,wrap,<110(down,45%,wrap)",
		"--bind=start:reload("+me+" search {q})",
		"--bind=change:reload("+me+" search {q})",
		"--bind=enter:become("+me+" go {1} {2} {3} "+shellQuote(client)+")",
	)
	// fzf runs its commands with $SHELL, and a person's shell may be slow to start.
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh", snapshotVariable+"="+snapshot)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	// The picker opens at once, and a search waits for what it needs.
	failed := make(chan error, 1)
	go func() {
		pruneIndex(index)
		if err := takeSnapshot(snapshot, os.Getenv("REX_BLOCK"), claudeDir(), index); err != nil {
			failed <- err
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	err = cmd.Wait()
	select {
	case err := <-failed:
		return err
	default:
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 130 {
		return nil // Escape.
	}
	return err
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
		// The client that opened the picker can be gone: "rex do jump_anywhere" is
		// one. Rex then uses the app, if one is connected.
	}
	_, err = runRex(selectArgs...)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
