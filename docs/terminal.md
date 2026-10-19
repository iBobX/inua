# Terminal: inua-term

`inua-term` is inua's terminal, made for running many agents at once and
knowing at a glance which of them needs you. Its window has three
columns: the workspaces (a folder each), the shown workspace's sessions
with their panes, and the shown session's terminals, tiled.

```
inua-term                        open the window (the last workspace shown)
inua-term --workspace DIR        open or show the workspace for a folder
inua-term -e COMMAND [ARGS…]     run a command in a session of its own ("Commands")
inua-term agents [--json]        the agents in every pane
inua-term report …               an agent telling inua-term what it does (below)
inua-term integration remove claude|codex|all
                                 take out the hooks an earlier inua-term added
```

It is in the app drawer, under Tools. It can be the desktop's terminal:
Settings → Default Apps writes `xdg-terminals.list`, and its desktop
entry takes `-e` as xdg-terminal-exec expects.

## The first start

With no `term.toml` yet, a guide comes first, before the first pane
starts. Three steps, each a list beside a preview:

- **Your shell**: the login shell (`$SHELL`), or another of those
  installed (`/etc/shells`, one path each, nologin left out). The
  preview shows its prompt.
- **Terminal colours**: the window theme's, or a scheme of its own
  (Tokyo Night, Catppuccin Mocha and Latte, Dracula, Gruvbox Dark and
  Light, Nord, One Dark, Rosé Pine, Solarized Dark and Light), each with
  a line on its look; the preview is a few commands in it and its 16
  colours.
- **Window theme**: inua's, or one of inua's others for this window
  alone, whatever inua's is. The window takes it at once.

Skip, Escape or closing the guide keeps what was chosen so far; every
choice is in the settings after.

## How it works

The terminals belong to a **host**, `inua-term --host`, which the window
starts when none runs and which ends with its last pane. Closing the
window, or the window crashing, leaves every program running; opening
it again finds them, their recent output replayed. The window is a client
of the host over `$XDG_RUNTIME_DIR/inua/term.sock` (this user only); the
host keeps the workspaces, sessions and layouts, so every window shows
the same.

The terminal engine is [libghostty-vt](https://github.com/ghostty-org/ghostty),
Ghostty's terminal core, as a library: the screen, the scrollback, the
keyboard (with the kitty keyboard protocol Claude Code and Codex use to
tell Shift+Enter from Enter), the mouse, paste, search and the shell's
prompt marks. inua loads it with purego, so inua-term builds without cgo.
It is `libinua-vt.so`, libghostty-vt at a pinned commit linked with
inua's small shim (`internal/term/vt/shim`): releases have it in
`lib/inua/`; from source, `make term-engine` builds it (it needs git and
Zig 0.16). inua-term looks for it beside the program, in `../lib/inua`
from it, or at `$INUA_VT_LIB`.

## The window

- **Workspaces** (column 1): at its top, where this is (`user@host`,
  then the system, the kernel, the shell new panes start and the
  uptime), then the search with the new workspace button. Each
  workspace is a name, its
  folder, and what its agents add up to: a square filled in warn while
  one waits for you, in the accent while one works, hollow when quiet,
  and a badge counting those that wait. The search finds workspaces by
  name, folder or pane title.
- **Sessions** (column 2): at its top, the shown workspace's name and
  the new session button; then its sessions, each a section of its
  panes. A pane's row shows its agent's mark and title, its state and
  how long it has been in it; an agent waiting for you shows what it
  asks. A shell shows the command running in it. An agent's mark takes
  its own colour (Claude Code's orange, Gemini's blue…) while it works
  or waits for you, and goes quiet after.
- **Terminals** (column 3): the shown session's panes, tiled, each with a
  head (state, mark, title, folder). Drag a gap to resize, drag a head
  onto another pane to swap them.

A window narrower than 1040 px folds column 2, narrower than 720 px
column 1 as well; Ctrl+Shift+S and Ctrl+Shift+B show them anyway.

## Agents

inua-term knows Claude Code, Codex, Gemini CLI, opencode, Aider, Amp,
Cursor Agent, Copilot CLI, Goose and Qwen Code by their process, and what
they are doing (working, waiting for you, done, idle) from what they
report, from what they show at the bottom of their screen, and from the
notifications they send through the terminal. A manifest of each says
how; yours in `~/.config/inua/term/agents/*.toml` add agents or replace
the built-in ones:

```toml
type = "claude"
name = "Claude Code"
icon = "agent-claude"            # ui/icons/agent-claude-symbolic.svg
color = "#d97757"                # the mark's while it works or asks; none for the accent
process = ["claude"]
resume = "claude --continue"     # what a restored pane puts back

[[state]]
state = "blocked"
lines = 12                       # the last twelve rows with text
any = ["Do you want to proceed?", "❯ 1. Yes"]

[[state]]
state = "working"
lines = 6
any = ["esc to interrupt"]
```

A wrapper can name its agent with `INUA_TERM_AGENT=<type>`.

**Nothing added to the agents.** inua-term writes no hooks, skills or
settings of the agents': it knows them by their process and their
screen alone. An earlier inua-term added hooks to Claude Code's
`~/.claude/settings.json` and Codex's `~/.codex/config.toml`;
`inua-term integration remove all` takes them out (the file before is
kept as `.bak`). A program of your own may still report through
`inua-term report`, which does nothing outside an inua-term pane. The
host takes a report only from a process running in the pane, its
terminal the pane's: Codex runs its `notify` from a daemon that left the
pane it started in, and those reports once landed on that pane whatever
it ran.

```
inua-term report --agent mybot --state working --title "Rename the flag"
inua-term report --state blocked --summary "Allow rm -rf build?"
inua-term report --state done
```

When an agent waits for you or finishes in a pane you are not looking at,
a notification says so; its *Show* brings the window to that pane.
Ctrl+Shift+J goes to the next agent that needs you, in any workspace;
Ctrl+Shift+A lists every agent.

## What it remembers

While the host runs nothing needs remembering. When it ends (a reboot, a
logout), it keeps the sessions you used (typed in, or ran something in),
their layouts, and each pane's folder, title and **last command**: what
was typed at the prompt and not run, else the command running, else the
one run last. The next host starts a shell in each pane's folder, in the
same layout, and puts the command back at the prompt **not run**; the
pane says RESTORED until you type. An agent with a resume command comes
back as it (`claude --continue`). A pane whose folder is gone starts in
its workspace's; a session with neither there yet (a network share or a
disk mounted later) waits for it, still kept, and comes back within a
few seconds of the folder appearing. A session that fails to start is
left out whole, its panes started closed again. Commands matching the `forget`
patterns (`*password*`, `*token=*`, `*secret*`) are never kept, nor is
anything typed at a program's own prompt (a password for sudo or ssh).
The file is `~/.local/state/inua/term.json` (mode 0600).

This uses the shell's prompt marks (OSC 133). inua-term injects small
scripts that send them for bash (`--rcfile`, which then loads your
`~/.bashrc`), zsh (`ZDOTDIR`, handing back to yours) and fish
(`XDG_DATA_DIRS`); `integration = false` in `term.toml` turns that off.

## Keys

The keys are Ghostty's on Linux, a workspace standing for Ghostty's
window, a session for its tab and a pane for its split; inua-term's own
actions are Ctrl+Shift chords. Everything else goes to the program.
Ctrl+Shift+/ shows them all, Ctrl+Shift+P finds any action by name, and
the settings change them. A `term.toml` of the first version has the keys
it kept at that version's defaults moved to these.

| Keys | Action |
|---|---|
| Ctrl+Shift+T / Ctrl+Shift+N | New session / new workspace |
| Ctrl+Shift+O / Ctrl+Shift+E | Split right / down |
| Ctrl+Shift+W | Close the pane (asks if its agent is working) |
| Ctrl+Alt+←↑→↓ | Focus the pane in that direction |
| Ctrl+Shift+Super+←↑→↓ | Resize the focused pane |
| Ctrl+Shift+X, then an arrow | Swap with the pane in that direction |
| Ctrl+Shift+Enter / Ctrl+Shift+Super+= / Ctrl+Shift+L | Zoom / equalize / next layout |
| Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PgDn / Ctrl+PgUp | Next / previous session |
| Alt+1…8 / Alt+9 | Session 1…8 / the last session |
| Ctrl+Shift+1…9 | Workspace 1…9 |
| Ctrl+Shift+J / Ctrl+Shift+A | Next agent that needs you / every agent |
| Ctrl+Shift+B / Ctrl+Shift+S | Fold the workspaces / the sessions |
| Ctrl+Shift+C / Ctrl+Shift+V | Copy / paste |
| Shift+Insert, middle click | Paste the primary selection |
| Ctrl+Shift+F | Search the scrollback |
| Ctrl+Shift+Space | Copy mode: h j k l w b 0 $ g G, v and V to select, y to copy |
| Shift+PgUp / Shift+PgDn, Shift+Home / Shift+End | Scroll |
| Ctrl+Shift+PgUp / Ctrl+Shift+PgDn | Previous / next prompt |
| Ctrl+Shift+R | Rename the session |
| Ctrl+, | Settings |
| Ctrl+= / Ctrl+- / Ctrl+0 | Font larger / smaller / as set |

## Copy and paste

Selecting copies (a setting); the primary selection follows the
selection either way. Double-click selects a word, triple-click a line;
with a program tracking the mouse (htop, vim with `mouse=a`), hold Shift
to select. Pasted text goes in as the program asked for it (bracketed
paste); text that could run commands where the program did not ask (a
line break, the end of a bracketed paste) shows a sheet first: Paste,
Paste as one line, or Cancel. Files on the clipboard paste as their
paths, quoted for the shell; an image with no text saves to
`$XDG_RUNTIME_DIR/inua/term-paste/` and pastes its path. Programs may set
the clipboard through OSC 52 (agents over SSH do); reading it is not
offered. Ctrl+click opens a link, an OSC 8 one or a URL in the text.

## Settings

`~/.config/inua/term.toml`, written by the guide and the settings
(Ctrl+,) with what each setting means: the window's theme (`look.theme`,
`""` for inua's) and the terminals' colours (`look.scheme`, `""` for the
theme's), the font and its size, the cursor, copy on select, scrollback,
the host's replay size, the shell new panes start (`shell.program`, `""`
for `$SHELL`) and its integration, agents' notifications, remembering
commands and what never to keep, and the keys by action (`""` frees one
for the program). The theme, the colours and the shell take hold at
once; the icons and the text size follow inua's `config.toml`.

## Not yet

- Images in the terminal (the kitty graphics protocol, sixel).
- inua's clipboard history in the terminal (the shell's clipboard panel
  has it).
- Reading the clipboard through OSC 52, allowed per workspace, and
  notifications turned off per workspace.
- A client in the browser: nothing in the host stands in its way.
