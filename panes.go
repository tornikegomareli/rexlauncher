package main

import "time"

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
	// Conversation is the Claude Code session that runs in the pane, if one does.
	Conversation *conversation
	// Text is what the search reads: the conversation, or the scrollback of any
	// other pane. The newest comes first.
	Text []entry
}

// active is the last change of the pane's conversation. It is zero without one.
func (p pane) active() time.Time {
	if p.Conversation == nil {
		return time.Time{}
	}
	return p.Conversation.Active
}

// loadPanes lists the panes, with the names of their conversations. It is enough
// to show the picker.
func loadPanes(self, claude string) ([]pane, error) {
	panes, err := listPanes(self)
	if err != nil {
		return nil, err
	}
	conversations := findConversations(claude)
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

// loadText returns the panes with their text. It can take seconds, if a
// conversation was not indexed before.
func loadText(panes []pane, index string) []pane {
	loaded := append([]pane(nil), panes...)
	forEach(len(loaded), func(i int) {
		p := &loaded[i]
		if p.Conversation != nil {
			if entries, err := p.Conversation.entries(index); err == nil {
				p.Text = entries
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
