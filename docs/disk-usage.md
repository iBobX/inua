# Disk Usage

`inua-disk-usage` shows what takes the room on a disk, as a map in the
manner of DaisyDisk, and knows the folders a developer's and an agent's
work leaves behind: dependencies, build output, toolchains and package caches; coding
agents' conversations, worktrees and old versions; AI models downloaded. For
the item chosen it says whether it can go, by a route of checks, asks the
assistant when no check settles it, explains what it is, and deletes it.

```
inua-disk-usage [FOLDER]         open the window, scanning FOLDER if given
inua-disk-usage --scan FOLDER    the largest items in FOLDER and the folders
                [--json] [--top 15]  a rule knows
inua-disk-usage --why PATH       whether PATH can be deleted, check by check
```

## The window

```
┌───────────────┬──────────────────────────────────┬───────────────────┐
│ Disk Usage    │ ■ [HOME]   518 GB in 5.8M files  │ ■ [ITEM]    [DEV] │
│ PLACES        │ ~ › Codework › inua              │ go-build          │
│ ⌂ Home        │            ╭─────╮               │ ~/.cache/go-build │
│ ▭ System      │          ╭─┤ map ├─╮             │ ■ [CAN IT GO]     │
│ ▢ Choose…     │            ╰─────╯               │ Safe to delete    │
│ FOUND         │ ■ .claude/              3.5 GB   │ □ Part of the sy… │
│ </> Developer │ ■ bin/                  405 MB   │ ■ A kind inua kn… │
│ ◫ Agents, AI  │ ■ dist/                  97 MB   │ go clean -cache   │
│ ⌫ Caches      │                                  │ ■ [WHAT IS IT]    │
└───────────────┴──────────────────────────────────┴───────────────────┘
```

**Places**: the home, each disk mounted (the root, then the others by
mount point; not the one the home is on, which Home stands for), and any
folder chosen with *Choose a folder…* or given on the command line. A scan
stays on its place's filesystem, as `du -x` does.

**The map** is a sunburst: the folder in view is the disc in the middle,
what is in it the first ring, sized by its share of the turn, what is in
those the next ring, five rings out. A slice is coloured by what a rule
knows it as: the developer's in the accent, the agents' and AI's in `warn`,
caches in `ok`, the rest in `t2`; pointing at one names it in the middle.
A click on a folder opens it, on the middle goes up a folder (so do
Backspace and Alt+Up), and the trail above the map goes to any folder on
the way. Under the map, the list of what is in the folder, largest first.

**Before a scan** the map is the disk itself: the room used as a band, the
free room faint, and how much is free in the middle. **While scanning**,
the folders of the place land in the first ring as each is measured, in
their kind's colour and by their share of what is counted so far, and a
sweep of cells turns through the rings outside; under the map, the files
and bytes counted and the folder being read.

**Found**: Developer, Agents and AI, Caches; each lists the folders of its
kind every scan found, largest first, with whether they can go and the
total that can.

**The item chosen**, in the panel on the right: its size, files and last
change; *[CAN IT GO]*, the route; *[DELETE]*, Move to Trash or Delete for
good (asked once more, in place); *[WHAT IS IT]*, the assistant's
explanation. What is kept cannot be deleted from here; the place scanned
itself neither. What goes wrong is said under what it is about.

## Can it go

`Decide` (internal/apps/disk) takes a path through checks in turn, and the
first that settles it ends the route; each check shows in the panel with
what it found.

1. **Part of the system, or one of your main folders?** `/usr`, `/etc`,
   `/var` (but its caches) and the like; the home, `~/.config`,
   `~/.local/share`, `~/.cache` themselves, keys (`~/.ssh`, `~/.gnupg`,
   pass), the XDG folders (Documents, Pictures …). **Keep.**
2. **Yours to delete?** The folder it is in is writable by you; if not,
   only root could, so **Keep**, with the proper command when a rule knows
   one (`sudo paccache -r`).
3. **Kept by git?** A project's `.git` is **Keep**; files git tracks are
   **Keep** (delete them with git, so the project knows); a repository
   itself says what would be lost (changes not committed, commits not
   pushed) and goes on; what the project ignores goes on, noted.
4. **A kind inua knows?** A rule's verdict: **Safe** (made or downloaded
   again when needed), **Review** (yours to decide: it may hold your work),
   or **Keep**, with why and the proper way to clean it.

When no check settles it, *Ask the assistant* sends the facts to the
assistant's decision route: the profile for quick choices (Settings → AI,
*Decision*) chooses Safe, Review or Keep with Mister Morph's Evaluate; with
no such profile, the profile that answers is asked for the word and a
reason. Its answer shows under the route, as the assistant's, never as a
check's.

The facts are the path, the size, the files counted, the last change, the
route's findings and the names and sizes of the largest items in it: never
what the files hold. *Explain* sends the same.

## The rules

`internal/apps/disk/rules.go` lists them, the more particular first. A
rule names a folder anywhere (`node_modules`), one under the home
(`.cache/go-build`, `.cache/*` for any program's cache), or an absolute
path (`/var/cache/pacman/pkg`), and may want a file beside it (Rust's
`target` beside `Cargo.toml`) or in it (`.venv` holding `pyvenv.cfg`).

| Kind | Examples |
| --- | --- |
| Developer | `node_modules`, npm, Yarn, pnpm, Bun and Deno caches, Next.js, Nuxt, SvelteKit, Turborepo and Parcel builds; Go's build cache and modules; Rust: `target` (also wherever `CARGO_TARGET_DIR` puts it, by its `CACHEDIR.TAG`), Cargo's registry and git checkouts, programs in `~/.cargo/bin`, toolchains, rustup's downloads, sccache and rust-analyzer caches; Python environments, bytecode and tool caches, pip, uv, Poetry, Conda; Gradle, Maven, Android SDK and emulators; Zig, ccache, Bazel, JetBrains, VS Code; Podman's and Docker's storage (Keep: prune them with their commands); `.git` (Keep) |
| Agents and AI | Claude Code's conversations, file history, logs, plugins and installed versions; agents' worktrees (`.claude/worktrees`, `.codex/worktrees`); Codex's conversations, logs and installed versions; Gemini CLI, Cursor, Aider, Continue, Windsurf; npx packages; Ollama, LM Studio, Hugging Face and PyTorch models; Playwright and Puppeteer browsers; inua's assistant |
| Caches | the trash; pacman, APT and DNF packages; Flatpak apps (Keep); the journal (Keep); any folder in `~/.cache`; any folder its program marked with `CACHEDIR.TAG` |

An agent's own folder (`~/.claude`, `~/.codex`) lists its known parts,
and itself only when none is found.

## Measuring

A scan reads folders on as many threads as there are CPUs, four each;
sizes are what files take on disk (their blocks), each hard link once. Files
under 1 MB are counted together in each folder (*N smaller files*), so a
home of millions of files stays a small tree. A home of 5.8 million files
and 518 GB took 14 s on an SSD.

## Code

| Where | What |
| --- | --- |
| `internal/apps/disk` | The scan, the rules, `Decide`, the facts and prompts for the assistant, the map's layout, drawing and hit-testing, deleting; no GTK |
| `internal/diskui`, `ui/disk.blp` | The window |
| `cmd/inua-disk-usage` | The program: the window, `--scan`, `--why` |
