package search

import (
	"regexp"
	"strings"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/parallel"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// LoadPanes lists the panes, with the names of their conversations. It is enough
// to show the picker.
func LoadPanes(self, claude string) ([]rex.Pane, error) {
	panes, err := rex.ListPanes(self)
	if err != nil {
		return nil, err
	}
	conversations := conversations.Find(claude)
	for i := range panes {
		if c, ok := conversations[panes[i].Block]; ok {
			panes[i].Conversation = &c
			if c.Name != "" {
				panes[i].Name = c.Name
			}
		}
	}
	return panes, nil
}

// LoadText returns the panes with their text. It can take seconds, if a
// conversation was not indexed before.
func LoadText(panes []rex.Pane, index string) []rex.Pane {
	loaded := append([]rex.Pane(nil), panes...)
	parallel.ForEach(len(loaded), func(i int) {
		p := &loaded[i]
		if p.Conversation != nil {
			if messages, err := p.Conversation.Messages(index); err == nil {
				p.Text = MessageEntries(messages)
				return
			}
		}
		// Rex keeps a terminal's scrollback, except for a program that draws on
		// the alternate screen. Claude Code does, so its index comes first.
		if text, err := rex.Run("capture", "-s", p.Session, "-b", p.Block, "--trim", "--unwrap"); err == nil {
			p.Text = ScreenEntries(string(text))
		}
	})
	return loaded
}

// ScreenEntries splits a screen's text, the newest line first.
func ScreenEntries(text string) []textutil.Entry {
	lines := strings.Split(text, "\n")
	seen := map[string]bool{}
	var entries []textutil.Entry
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(textutil.Clean(lines[i]))
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		entries = append(entries, textutil.Entry{Text: line, Lower: textutil.Lower(line)})
	}
	return entries
}

// plain removes what a conversation's text has only for the terminal that draws it.
func plain(text string) string {
	text = imageTag.ReplaceAllString(text, "")
	text = heading.ReplaceAllString(text, "$1")
	return strings.Join(strings.Fields(markdown.Replace(text)), " ")
}

var (
	imageTag = regexp.MustCompile(`\[Image #\d+\]|</?pasted_content[^>]*>`)
	heading  = regexp.MustCompile(`(^|\s)#{1,6}\s`)
	markdown = strings.NewReplacer("**", "", "`", "")
)

// MessageEntries returns what a search reads in messages, the newest first. Identical
// messages count once.
func MessageEntries(messages []conversations.Message) []textutil.Entry {
	seen := map[string]bool{}
	var entries []textutil.Entry
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		text := textutil.Clean(plain(message.Text))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		e := textutil.Entry{Text: text, Lower: textutil.Lower(text), At: message.At}
		if message.Role == "user" {
			e.Label = "you: "
		}
		entries = append(entries, e)
	}
	return entries
}
