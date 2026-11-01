// Package rex is everything rexlauncher asks the rex command: panes, their screens,
// actions, and how to show a pane. It never starts a server, because the picker runs
// inside one.
package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// rexTimeout is how long a rex command can take. Without a limit, one that hangs would
// freeze the preview, or leave the picker reading conversations for ever.
var rexTimeout = 15 * time.Second

// Capture reads what a pane shows now, with its colors.
func Capture(p Pane) (string, error) {
	text, err := Run("capture", "-s", p.Session, "-b", p.Block, "--trim", "--format=vt")
	return ScreenText(string(text)), err
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

// ScreenText keeps the colors of a screen, and turns cursor moves to the right
// into spaces. Nothing else passes, so that a pane cannot move the picker's cursor.
// Each line ends with a reset and starts with the colors that are still on, so that
// each line can be cut alone.
func ScreenText(vt string) string {
	vt = otherEscape.ReplaceAllString(vt, "")
	active := ""
	lines := strings.Split(strings.TrimRight(vt, "\n"), "\n")
	for i, line := range lines {
		var out strings.Builder
		out.WriteString(active)
		end := 0
		for _, at := range controlSequence.FindAllStringSubmatchIndex(line, -1) {
			out.WriteString(textutil.Clean(line[end:at[0]]))
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
		out.WriteString(textutil.Clean(line[end:]) + "\x1b[0m")
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

// Run calls  It never starts a server: the picker runs inside one.
func Run(args ...string) ([]byte, error) {
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

func Call(method string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	out, err := Run("api", "call", method, string(body))
	if err != nil {
		return err
	}
	return json.Unmarshal(out, output)
}
