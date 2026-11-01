<h1 align="center">Rex Launcher</h1>

<h3 align="center">Find any Rex pane by what was said in it.</h3>

<p align="center">
  <img src=".github/assets/hero.png" width="100%" alt="A Rex window with Rex Launcher open over it. The search for migration finds two Claude Code conversations, and the preview on the right shows the messages that hold the word" />
</p>

Rex Launcher is a [rexd](https://github.com/tornikegomareli/rexd) plugin for [Rex](https://www.superlogical.com/rex). It searches every pane on your server, and the words said in the Claude Code conversations inside them, then goes to the pane you choose.

Type part of a name, or a word you remember from a conversation. The picker looks like this in text:

```
 › zebrafish                                                              1 pane
────────────────────────────────────────────────────────────────────────────────
  All   Panes   Conversations   Screens
 Conversations                                │ The zebrafish migration failed
▌ Fix the checkout  The zebrafish migrati…  2 │ because the lock was held.
                                              │
                                              │ you: Please explain why the
                                              │ zebrafish migration failed
                                              │ ───────────────────────────────
                                              │ Session  shop
                                              │ Folder   ~/Development/shop
                                              │ Matches  2
────────────────────────────────────────────────────────────────────────────────
 ↵ Go to pane   tab Filter   esc Close                                      1/1
```

## Install

```sh
rexd install tornikegomareli/rexlauncher
```

It needs:

- [rexd](https://github.com/tornikegomareli/rexd) and Go, to build the plugin.
- Remote Control, in Rex's Settings under Rex Server. Rex only lets a program outside the app change the session it shows when this is on. Without it, Enter still focuses the pane in its session, and shows Rex's error.

## Keys

The plugin binds `ctrl+alt+shift+l`. To use another key, bind `rexlauncher` in your `init.lua`:

```lua
rex.bind("cmd+shift+k", "rexlauncher")
```

Rex warns about a key bound to an action it does not have, so if you may remove the plugin, fall back to another action:

```lua
local has_launcher = rexd and rexd.plugins["tornikegomareli.rexlauncher"]
rex.bind("cmd+shift+k", has_launcher and "rexlauncher" or "session.switch")
```

| Action | Key |
| --- | --- |
| Search | Type |
| Move in the list | Up, Down, Ctrl+P, Ctrl+N |
| Move by a page, or to the first or last row | PageUp, PageDown, Home, End |
| Go to the selected pane | Enter, or click the selected row |
| Filter: All, Panes, Conversations, Screens | Tab, Shift+Tab |
| Scroll the matches in the preview | Shift+Up, Shift+Down, or the wheel |
| Delete a word, or the whole query | Ctrl+W, Ctrl+U |
| Close | Esc |

The picker has one row for each pane: its name, and the best match in it. With a window of 72 columns or more, the right side has a preview: all the matches of the selected pane, or its live screen with colors if it has no match. The details of the pane are below the preview.

With no query, the picker lists the conversations that changed last, and then the other panes. A pane in the window of the picker is not suggested, because it is behind the picker.

## What it searches

All the words you type must be in the same message or line. Panes come in this order:

| Panes found by | Text | From |
| --- | --- | --- |
| Name | Session, window and pane names, and the folder | Rex |
| Conversation | What you and Claude Code wrote, the newest first | The transcripts in `~/.claude` |
| Screen | The scrollback of the other panes | Rex |

A pane where your words only occur inside other words comes last: `panes` finds "split panes" before "Japanese". A pane name also matches with letters left out: `tlbr` finds `tilebar`.

Tool calls, tool output and thinking are not searched.

Limits:

- It searches only conversations that are running in a pane now.
- Enter goes to the pane. It cannot scroll Claude Code to the message, so the preview shows the message.
- Rex keeps no scrollback for a program on the alternate screen. For other full-screen agents, such as Codex, only the visible screen is searched.

## Your data

Nothing leaves your machine. A transcript is mostly tool output, so the plugin keeps an index with only the text above, in your cache folder (`~/Library/Caches/rexlauncher` on macOS). Only you can read these files. Each one is deleted 30 days after its conversation was last searched. The text of your other panes stays in memory while the picker is open.

## Develop

```sh
git clone https://github.com/tornikegomareli/rexlauncher.git
rexd install ./rexlauncher --link
make test    # unit tests
make e2e     # against a Rex server of its own
```

## License

[MIT License](LICENSE).
