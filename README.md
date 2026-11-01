<h1 align="center">Rex Launcher</h1>

<h3 align="center">See which agents need you, answer them, find any pane by what was said in it, and run any Rex action or skill.</h3>

<p align="center">
  <img src=".github/assets/hero.gif" width="100%" alt="Rex Launcher open over a dark Rex window. Over a Claude Code session, it finds another session by a word from its conversation, splits the pane with split r, then finds a skill and puts /code-review in the prompt" />
</p>

Rex Launcher is one search box for everything in [Rex](https://www.superlogical.com/rex). Press a key, type a few letters, and press Enter.

| Type | Enter does this |
| --- | --- |
| Nothing | Lists the agents that need you first. Press `ctrl+r` to answer one without leaving your pane |
| The name of a session, window, pane or folder | Goes to that pane |
| A word you remember from a Claude Code conversation | Goes to the pane with that conversation, and the preview shows the message |
| A word on any pane's screen, such as an error | Goes to that pane |
| An action, such as `split d`, `theme` or `sidebar` | Runs it on the pane you were on |
| A skill of the agent you were in, such as `review` | Puts `/code-review` in that agent's prompt |
| The name of a saved layout | Builds that session again |

It is a [rexd](https://github.com/tornikegomareli/rexd) plugin.

## Install

```sh
rexd install tornikegomareli/rexlauncher
```

If rexd is new to you, run `rexd setup` first.

To remove it, run `rexd remove tornikegomareli.rexlauncher`.

It needs:

- [rexd](https://github.com/tornikegomareli/rexd) and Go, to build the plugin.
- Remote Control, in Rex's Settings under Rex Server. Rex only lets a program outside the app change the session it shows, or perform its actions, when this is on. Without it, Enter on a pane still focuses the pane in its session, and shows Rex's error.

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
| Go to the selected pane, run the selected action, or put the selected skill in the prompt | Enter, or click the selected row |
| Answer the selected agent | Ctrl+R |
| Delete the selected layout | Ctrl+X twice |
| Filter: All, Needs you, Panes, Conversations, Screens, Skills, Layouts, Actions | Tab, Shift+Tab |
| Scroll the preview | Shift+Up, Shift+Down, or the wheel |
| Delete a word, or the whole query | Ctrl+W, Ctrl+U |
| Close | Esc |

The picker has one row for each pane: its name, and the best match in it. With a window of 72 columns or more, the right side has a preview: all the matches of the selected pane, or its live screen with colors if it has no match. The details of the pane are below the preview.

With no query, the picker lists what needs you, what finished, what is at work, the conversations that changed last, and then the other panes. A pane in the window of the picker stays out of the recent conversations, because it is behind the picker.

Press the key again to close the picker.

## Needs you

Rex knows what each program says it is doing, through the [Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status). Claude Code reports it, so a blocked agent no longer looks like an idle one.

| Section | What is in it |
| --- | --- |
| Needs you | Agents that wait for you, with `permission`, `question` or `auth`, and those that failed |
| Finished | Agents that finished and that nobody has looked at yet, up to 6 |
| Working | Agents at work, with their progress if they report it |

Each row has the agent's own message, such as `approve Bash: make deploy`. The list refreshes while the picker is open. The pane you opened the picker from stays out of these sections. The Needs you tab keeps only what needs you and what finished.

### Answer an agent

Select an agent and press `ctrl+r`. The preview shows its live screen, so you see what it asks. The picker stays open afterwards, for the next agent.

| The agent | `ctrl+r` lets you |
| --- | --- |
| Waits on a menu, such as a permission prompt | Press a number to choose. Enter goes to its pane instead |
| Is at rest, or at work | Type text. Enter sends it |
| Wants a password | Nothing. Go to its pane to type it |

An answer is only sent if the agent still shows what you saw. If it moved on, nothing is sent, and the picker says so. A menu takes no text, because Enter after the text would pick its default option.

A menu's numbers are the agent's own, so one of them can do more than the others, such as allowing a command for good. Read them in the preview first.

You can answer a pane that reports `blocked`, or one that holds a Claude Code conversation.

## Layouts

Open the Layouts tab, choose **Save this layout** and name it. Enter on a saved layout builds a new session with its name. Search finds a layout by its name, its panes and its folders.

A layout keeps:

- The windows and their names.
- The splits and their sizes.
- The folder of each pane.
- The conversation of each Claude Code pane, which resumes with `claude --resume`, through your own shell.

It runs nothing else again, because repeating a command could repeat what it did, so every other pane comes back as a shell in its folder. That includes Codex and pi. Floating panes and zoom are not kept.

If a session already has the layout's name, Enter goes to that session instead of starting everything twice. A conversation that is still open in another pane is not resumed, because two copies of Claude Code on one conversation would write over each other.

Saving a name again replaces that layout.

## Actions

<p align="center">
  <img src=".github/assets/actions.png" width="100%" alt="Rex Launcher over a Claude Code session with the search split r. It lists Split Pane Right, Left, Up and Down as actions" />
</p>

You can run Rex's actions without leaving the search box. It has the actions in Rex's palette and those of your `init.lua`:

- Split a pane down, right, left or up.
- Zoom, float or move a pane, and balance the splits.
- Open the theme picker, and toggle the sidebar.
- Start a new tab, window or session.
- Run your own actions, such as a `lazygit` one.

An action runs on the pane you were on when you opened the picker.

With a query, the All tab shows up to 4 actions, below the panes with that name and above the conversations. The Actions tab lists all of them.

It leaves out actions that close or restart something, such as Close Pane, because Enter runs the first row, and a word you type to find a conversation can match their titles. It also leaves out actions that need an argument. Rex's palette has both.

## Skills

<p align="center">
  <img src=".github/assets/skills.png" width="100%" alt="Rex Launcher on its Skills tab with the search rev. It lists /code-review and /security-review, and the preview shows the description, agent, source, file and frontmatter of /code-review" />
</p>

Open the picker from a pane that runs an agent, and the Skills tab lists what that agent can run. The preview shows what the selected skill does, where it comes from, and the rest of its frontmatter.

Enter puts the name in the prompt of the pane you were on, with a space after it, and sends nothing. You can add arguments, and you press Enter yourself. The text goes where the prompt's cursor is, so a draft stays.

| Agent | Enter puts | Where the list comes from |
| --- | --- | --- |
| Claude Code | `/name`, or `/plugin:name` | Your skills and commands in `~/.claude`, those of the project, and the plugins that are on |
| Codex | `$name` | Codex, through `codex app-server`. Skills that are off stay out |
| pi | `/skill:name` | pi, through `pi --mode rpc`, without its extensions |

For Codex and pi, the plugin starts a second copy for a moment to ask. It does not affect the agent in your pane.

With a query, the All tab shows up to 4 skills, below the panes with that name. The Skills tab lists all of them. A pane with no agent has no skills, and the tab says so.

Claude Code has no command that lists its skills, so the plugin reads the same folders. It leaves out skills from your claude.ai account, enterprise skills, and Claude Code's own commands, such as `/clear`.

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
- The search reads what the panes held when the picker opened. The preview of a screen stays live. To search newer output, open it again.
- Enter goes to the pane. It cannot scroll Claude Code to the message, so the preview shows the message.
- Rex keeps no scrollback for a program on the alternate screen. For other full-screen agents, such as Codex, only the visible screen is searched.

## Your data

Nothing leaves your machine. Layouts are in your config folder (`~/Library/Application Support/rexlauncher/layouts` on macOS), and only you can read them. They hold folders, pane titles and conversation IDs. A transcript is mostly tool output, so the plugin keeps an index with only the text above, in your cache folder (`~/Library/Caches/rexlauncher` on macOS). Only you can read these files. Each one is deleted 30 days after its conversation was last searched, when the picker opens. `rexd remove` does not delete them, so to remove the plugin and its index, also delete that folder. The text of your other panes stays in memory while the picker is open.

## Develop

```sh
git clone https://github.com/tornikegomareli/rexlauncher.git
rexd install ./rexlauncher --link
make test    # unit tests
make e2e     # against a Rex server of its own
```

| Folder | What it holds |
| --- | --- |
| `cmd/rexlauncher` | The program |
| `internal/picker` | The screen: the query, the rows and the preview |
| `internal/search` | What finds panes and actions, and their order |
| `internal/rex` | Everything it asks the `rex` command |
| `internal/conversations` | Everything it knows about Claude Code's conversations |
| `internal/skills` | What finds the skills of Claude Code, Codex and pi |
| `internal/layouts` | What saves a session's layout and builds it again |
| `rex/config.lua` | The action and the key, loaded into Rex's config |
| `e2e` | Tests against a real Rex |

## License

[MIT License](LICENSE).
