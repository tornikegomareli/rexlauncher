# Jump anywhere

A [rexd](https://github.com/tornikegomareli/rexd) plugin for [Rex](https://www.superlogical.com/rex). It searches every pane on your server, and the words said in the Claude Code conversations inside them, then goes to the pane you choose.

Type part of a name, or a word you remember from a conversation:

```
Jump> zebrafish
  2/2
  Pane names and words said in conversations. Enter goes there.
▌ claude  The zebrafish migration failed because the lock was held.  shop › Fix the checkout
  you  Please explain why the zebrafish migration failed last night  shop › Fix the checkout
```

## Install

```sh
rexd install tornikegomareli/rex-jump
```

It needs:

- [rexd](https://github.com/tornikegomareli/rexd) and Go, to build the plugin.
- [fzf](https://github.com/junegunn/fzf) 0.38 or newer. The picker tells you if it is missing.
- Remote Control, in Rex's Settings under Rex Server. Rex only lets a program outside the app change the session it shows when this is on. Without it, Enter still focuses the pane in its session, and shows Rex's error.

## Keys

The plugin binds `ctrl+alt+shift+j`. To use another key, bind `jump_anywhere` in your `init.lua`:

```lua
rex.bind("cmd+shift+k", "jump_anywhere")
```

Rex warns about a key bound to an action it does not have, so if you may remove the plugin, fall back to another action:

```lua
local has_jump = rexd and rexd.plugins["tornikegomareli.jump"]
rex.bind("cmd+shift+k", has_jump and "jump_anywhere" or "session.switch")
```

In the picker, Enter goes to the selected row, and Esc closes the picker.

## What it searches

All the words you type must be in the same message or line. Results come in this order:

| Results | Text | From |
| --- | --- | --- |
| Panes | Session, window and pane names, and the folder | Rex |
| Conversations | What you and Claude Code wrote, the newest first | The transcripts in `~/.claude` |
| Screens | The scrollback of the other panes | Rex |

Tool calls, tool output and thinking are not searched.

Limits:

- It searches only conversations that are running in a pane now.
- Enter goes to the pane. It cannot scroll Claude Code to the message, so the preview shows the message and the ones around it.
- Rex keeps no scrollback for a program on the alternate screen. For other full-screen agents, such as Codex, only the visible screen is searched.

## Your data

Nothing leaves your machine. A transcript is mostly tool output, so the plugin keeps an index with only the text above, in your cache folder (`~/Library/Caches/rex-jump` on macOS). Only you can read these files. Each one is deleted 30 days after its conversation was last searched. While the picker is open, the text of your panes is in a private temporary folder, which it deletes when it closes.

## Develop

```sh
rexd install ./rex-jump --link
make test    # unit tests
make e2e     # against a Rex server of its own
```

## License

MIT
