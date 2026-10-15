# Files: inua's file manager

`inua-files` is a file manager in the light apps' look, made to replace
Nautilus in an inua session. It aims to be:

- **fast**: a folder of 100 000 entries opens in a fraction of a second
  and scrolls without stutter; nothing waits on the disk on the main
  thread;
- **keyboard first**: every action has a key, typing filters, a jump list
  learns the folders you use, and the mouse is never required;
- **quiet**: the shell's colours, hairlines, monospace readouts and
  Phosphor icons; no toolbar full of buttons.

It is a program of its own (`cmd/inua-files`), like `inua-games` and
`inua-assistant`, so `inua-apps` does not grow and the file manager can
be started, and replaced, on its own.

```
inua-files [-ui-dir DIR] [PATH|URI ...]   open windows on folders (files are selected)
inua-files -default                       make inua-files the desktop's file manager
inua-files -default=false                 give the job back (removes what -default wrote)
```

`inua-files trash:` opens the trash; `inua-files sftp://host/dir` mounts
the share first.

## Window

```
┌──────────┬──────────────────────────────────────┬────────────┐
│ Files    │ ‹ › ↑  ~ / Code / inua        [≡][▦] │            │
│ PLACES   ├──────────────────────────────────────┤ preview    │
│ Home     │ NAME ↑              SIZE   MODIFIED  │            │
│ Desktop  │ ▸ cmd           12 items   14:02     │ name       │
│ …        │ ▸ docs           9 items   11:40     │ KIND       │
│ Trash    │   go.mod          3.1 kB   Oct 3     │ SIZE       │
│ DEVICES  │   README.md      14.2 kB   Oct 14    │ MODIFIED   │
│ USB    ⏏ │                                      │ …          │
│ NETWORK  │ /flt · 23 items · 2 sel. · 9 GB free │            │
│          │ Copying photo.jpg ━━━━━━  38 MB/s  ✕ │            │
│ ◉ ▥ ⓘ  ⌘ │                                      │            │
└──────────┴──────────────────────────────────────┴────────────┘
```

| Part | What | Key |
| --- | --- | --- |
| Sidebar (left) | Places, the trash, devices and network shares; at its foot, hidden files, two panes, details and the keys. A right click on an entry: open it in a new tab or window, and remove a bookmark, empty the trash, eject a drive or disconnect a share. | F9 shows or hides it |
| Panes (centre) | One pane, or two side by side (commander style). Each has its own tabs, and each tab its own folder and history; a pane's view, order and selection are its folder's. With two panes, a status square starts each path: filled for the one the keys go to, hollow for the other, whose selection loses the accent. | F3 two panes; Tab the other pane |
| Tabs | A pane's tabs show above it once it has two: the folder's name, a button that closes it (a middle click too). | Ctrl+T, Ctrl+W, Ctrl+Tab |
| Details (right) | The selection: a thumbnail or the first lines of a text, its kind (MIME type), size, modified, opened and changed times, permissions, owner, where it is and where a link points; a folder's size and count, added up on a goroutine. Several selected: how many, and their size. | Space, Ctrl+I, Alt+Enter |
| Path bar | The folder as buttons, one a segment (home as a house); a click beside them, or the pencil, edits it, the next button copies it, and the last opens a terminal there. Ctrl+L turns it into an entry (`~` is home, Tab completes folder names); a file typed opens its folder with it selected; a URI (`sftp://host/dir`, `smb://server/share`) mounts it; `trash:` is the trash. | Ctrl+L, Esc back |
| Status | The count, the hidden count, the selection and its size, the filter typed, free space, and the zoom (− 100% +); a message for a moment after an action. The operation running shows under the panes, with its speed, time left and a cancel button. | |

### Views

The head's text tabs, LIST and GRID (the apps' tabs), choose the view:

| View | What | Key |
| --- | --- | --- |
| List | Rows: icon and name, then the columns chosen: size (a folder's item count), modified, created, opened, kind, permissions, owner, group (size, modified and kind at first). A column head sorts by it, again reverses it; a right click on the heads, or the button at their end, turns columns on and off, for every folder. The heads and the rows share fixed widths, so they line up. | Ctrl+1 |
| Grid | Icons, thumbnails for images, names below. | Ctrl+2 |

Both are GTK's recycling `ListView` and `GridView`: only the rows on
screen have widgets, so the size of a folder does not matter. Each folder
remembers its view and order (kept in `~/.local/share/inua/files.json`,
the last 500 folders); a new folder takes the last one used. The zoom
(Ctrl++, Ctrl+−, Ctrl+0, Ctrl with the wheel, or the status line's
buttons) goes from 80% to 150%: rows, text and icons, in every window.

Order: by name (natural: `file2` before `file10`, case folded), size,
modified or kind; folders first. Hidden files (dot files and those named in
a folder's `.hidden`) show with Ctrl+H.

Thumbnails follow the freedesktop thumbnail standard
(`~/.cache/thumbnails/large`, `Thumb::URI` and `Thumb::MTime`), so the
ones Nautilus made are used and the ones made here serve other apps too.
JPEG, PNG, GIF, WebP, BMP and TIFF are decoded in Go (JPEGs at a reduced
size through libjpeg-turbo when it is installed), three at a time, and
turned by their EXIF orientation.

## Keys

Moving and opening

| Key | Does |
| --- | --- |
| ↑ ↓ ← → / Home End / PgUp PgDn | Move (the arrows step through the grid's cells) |
| Enter | Open: a folder in this pane, a file with its app; several selected, all of them |
| Shift+Enter | Open with… (choose the app) |
| Middle click | A folder in a new tab, behind this one |
| Backspace, Alt+↑ | The parent folder, with the folder left selected |
| Alt+← / Alt+→ | Back / forward |
| Alt+Home | Home |
| Alt+1 … Alt+9 | The sidebar's first nine entries |
| Ctrl+L | Type a path or a server |
| Ctrl+P | Jump: the folders you used, by frecency (as zoxide), fuzzy matched |
| Ctrl+F | Search below this folder: names, fuzzy, results as they are found, the best first when done; Esc leaves |
| Letters | Filter this folder as you type (fuzzy; the folder keeps its order and the best match is selected); Backspace takes a letter off, Esc clears |

Selection and changes

| Key | Does |
| --- | --- |
| Shift+arrows, Ctrl+click, Shift+click, a rubber band | Select |
| Ctrl+A / Ctrl+Shift+A / Ctrl+Shift+I | Select all / none / invert |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste, interchanged with other file managers (`x-special/gnome-copied-files`, `text/uri-list`) |
| Ctrl+Shift+C | Copy the paths as text (nothing selected: the folder's) |
| F2 | Rename (the name without its extension is selected) |
| Delete | Move to the trash (in the trash: delete for good) |
| Shift+Delete | Delete for good (asks first) |
| Ctrl+Shift+N / Ctrl+Alt+N | New folder / new empty file |
| Ctrl+Z | Undo the last operations, one at a time |
| F5 / F6 | Copy / move the selection to the other pane (one pane: F5 reloads) |
| Ctrl+D | Bookmark this folder, or remove its bookmark |
| Ctrl+Alt+T | A terminal here (`xdg-terminal-exec`, else `$TERMINAL`, else kitty) |
| Menu, Shift+F10, right click | The selection's menu (or the folder's) |

Window

| Key | Does |
| --- | --- |
| Ctrl+T / Ctrl+W | New tab (here) / close the tab; the last one closes the window |
| Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PgDn / Ctrl+PgUp | Next / previous tab |
| Ctrl+N / Ctrl+Q | New window / close it |
| Ctrl+1 / Ctrl+2 | List / grid |
| Ctrl+H | Hidden files |
| F3 / Tab | Two panes / the other pane |
| F9 / Space, Ctrl+I | Sidebar / details |
| Ctrl+R | Reload (folders follow their changes by themselves; this is for network folders) |
| Ctrl++ / Ctrl+− / Ctrl+0, Ctrl+wheel | Bigger / smaller / normal |
| Ctrl+? | The keys |

In a card (rename, a name taken, a question): Enter does what is
highlighted, Esc cancels, and a question's buttons take their first
letter (R replace, S skip, K keep both, M merge).

## Places

The sidebar is the shell's index column, as Pass and Tailscale have:

- **Places**: Home, then the XDG user folders that exist
  (`~/.config/user-dirs.dirs`: Desktop, Documents, Downloads, Music,
  Pictures, Videos), then the bookmarks. Bookmarks are GTK's
  (`~/.config/gtk-3.0/bookmarks`), the list GTK's file chooser and
  Nautilus show; one that repeats a place above is not listed twice.
  Ctrl+D adds and removes them.
- **Trash**: the freedesktop trash (below).
- **Devices**: the computer's root; then drives and partitions with a
  filesystem, from UDisks2 (`org.freedesktop.UDisks2` on the system bus):
  USB sticks, SD cards, other disks' partitions, LUKS volumes, removable
  ones first and each drive's together. Each shows its label (else its
  drive and size) and, mounted, its free space. Clicking an unmounted one
  mounts it (`Filesystem.Mount`; polkit asks in inua's dialog when it needs
  a password) and opens it; a locked LUKS volume asks for its passphrase
  first (`Encrypted.Unlock`, then the filesystem inside is mounted). The
  eject button moves the panes out of it, unmounts every filesystem of the
  drive, locks its containers, then ejects the media or powers the drive
  off, so it is safe to pull out; when something has a file open on it,
  the status line says so. Drives come and go as they are plugged
  (UDisks2's signals); a new removable one is announced in the status line.
  The system's own volumes (the root, swap, what UDisks2 hints to ignore)
  are left out. Other mounted filesystems (sshfs, NFS, rclone, a disk
  mounted by fstab), from `/proc/self/mountinfo`, follow; the mount table
  is followed through its `POLLPRI` mark.
- **Network**: shares mounted through GVfs (`smb://`, `sftp://`, `ftp://`,
  `dav(s)://`, `nfs://`, `afp://`, phones over MTP), with a disconnect
  button, and *Connect to a server…*: a URI, with the ones used before
  offered. GIO mounts it (`g_file_mount_enclosing_volume`), and GTK's
  mount dialog asks for a password when the server wants one (with
  whether to remember it). A share is browsed through GVfs's FUSE folder
  (`/run/user/UID/gvfs`), so everything in the app works on it as on a
  local folder. GVfs's volume monitor says when shares come and go.

## The trash

The freedesktop Trash specification: an item goes to the home trash
(`~/.local/share/Trash`) when it is on that filesystem, else to the trash
at the top of its own mount (`.Trash/UID` when the administrator made
one, else `.Trash-UID`); `info/NAME.trashinfo` records where it was and
when, so other file managers can restore it too. Names are reserved by
making the info file first, exclusively.

The trash shows every trash there is, newest first, each item by the name
it had, where it came from and when it was trashed. Restore puts items
back (making their folder if it went; refusing when something took the
name), Delete removes them for good, and *Empty Trash* empties every
trash after asking.

## Operations

Copying, moving, trashing, deleting, renaming and making run on a
goroutine, one queue a window, so the window can be used meanwhile. One
that takes more than a quarter second shows under the panes: what it is
doing, a bar, the speed and the time left, and a cancel button. A window
closed while its operations run hides until they end, and the app stays
until then.

- Copying moves each 16 MB chunk with the kernel's `copy_file_range` (a
  reflink on Btrfs and XFS), keeps modes and times, copies links as links,
  and writes each file to a temporary name beside it first, so a cancelled
  or failed copy leaves no half file.
- Moving within a filesystem is a rename (`RENAME_NOREPLACE`, so nothing
  is overwritten by a race); across filesystems it is a copy, then the
  removal of what was copied.
- A name that is taken asks: *Replace*, *Skip*, *Keep both* (`name
  (2).ext`), or for folders *Merge*, which asks again inside; *Do the same
  for the rest* answers the others of that kind.
- Pasting or dropping something into the folder it came from makes a copy
  beside it: `name (copy).ext`, then `name (copy 2).ext`.
- One item failing does not stop the rest; what could not be done is
  listed at the end.
- Undo puts back what can be put back: a rename or a move (moved back), a
  trashing (restored), something made or copied (trashed). Deleting for
  good cannot be undone and asks first.
- Drag and drop: drag items to another pane, onto a folder, or to another
  app (as `text/uri-list`); drop files from other apps. Within a
  filesystem a drop moves, across filesystems it copies; Ctrl copies,
  Shift moves.
- Cut files are dimmed until they are pasted; after a cut and paste the
  clipboard is spent, as in other file managers. Files another app put on
  the clipboard are read with `wl-paste`.

## Opening files

A file opens with the app chosen for its type: its MIME type, guessed
from its name and first bytes; the choice is the desktop's, kept by GIO
in `~/.config/mimeapps.list`, so the launcher, browsers and other file
managers agree. A type with no app chosen asks. *Open with…*
(Shift+Enter, or the menu) is the app's own card: the apps for the type,
the one chosen first, the last used next, each with its icon; typing
finds any other app. *Always open … with it*, on at first, keeps the
choice for the type; off, the app is only put first next time. Several
files open together, one app for each type. The app starts from its
desktop entry with the files in its `Exec` (`%f`, `%F`, `%u`, `%U` as the
Desktop Entry specification says; `internal/desktop`), in the files'
folder; a file is opened by its type, never run, so scripts open in the
editor.

## Terminal

The head's terminal button, Ctrl+Alt+T and the folder's menu open a
terminal in the folder: `xdg-terminal-exec --dir=FOLDER` (the terminal
chosen for the desktop), else `$TERMINAL` started there, else kitty with
`--directory`. The folder is passed rather than only started in, as a
terminal that serves its windows from one process (GNOME Terminal) would
open in its own.

## Window

Files is a normal window, tiled as other apps are: the light apps float,
and `inua.conf` (written by inua) tiles Files after them
(`windowrule inua-files … tile = yes`).

## Instance and D-Bus

One process serves every window. It owns `io.github.lyricat.Inua.Files`
on the session bus and asks for `org.freedesktop.FileManager1`, queued
behind a file manager that holds it (Nautilus), so it takes over when that
one leaves. Browsers ("Show in folder"), the screenshot drawer, Viewer and
the settings window call it:

| Method | Does |
| --- | --- |
| `ShowFolders(uris, startup_id)` | A window on each folder |
| `ShowItems(uris, startup_id)` | A window on each folder of the items, with them selected |
| `ShowItemProperties(uris, startup_id)` | As ShowItems, with the details shown |

`inua-files PATH` started while it runs hands the paths to the running
process, through that interface on its own name, and leaves; so a new
window opens at once. The process ends with its last window.

`inua-files -default` makes it the desktop's file manager:

- `xdg-mime default io.github.lyricat.Inua.Files.desktop inode/directory`;
- a D-Bus service file for `org.freedesktop.FileManager1` in
  `~/.local/share/dbus-1/services`, which outranks Nautilus's in
  `/usr/share`, so "Show in folder" starts inua-files when it is not
  running.

`-default=false` removes the service file and gives `inode/directory`
back to Nautilus when it is installed. The sample `hyprland.conf` binds
Super+E to `$files`, which is `inua-files`, and the default dock pins
Files.

## Architecture

As the other light apps: the logic without GTK, tested; the view thin.

| Package | What |
| --- | --- |
| `internal/apps/files` | Entries (`ReadDir`: names, then `statx`, which gives the creation time too, on up to eight goroutines for big folders; links' targets; `.hidden`), natural order without allocating, fuzzy matching and the filter, history, the jump list (frecency), settings, places (XDG user folders, GTK bookmarks read and written), mounts (`mountinfo`, `statfs`, GVfs's folder names, the table followed), the trash, operations (copy, move, link, trash, restore, delete, rename, make; conflicts, progress, cancelling, undo), the clipboard's formats, searching below a folder on several goroutines, watching a folder (inotify, a burst of changes merged into one within 80 ms, at most 600 ms late), thumbnails |
| `internal/apps/files/disks` | UDisks2: volumes and their drives, mounting, unmounting, unlocking, locking, ejecting and powering off, followed by its signals |
| `internal/apps/files/fm1` | `org.freedesktop.FileManager1`: serving it, and handing paths to a running instance |
| `internal/filesui` | The windows: sidebar, panes and their tabs, list (its columns) and grid, details, path bar, cards (Open With among them), menus, drag and drop, the clipboard, operations' progress, zoom, GIO for apps by type and mounting shares |
| `cmd/inua-files` | Arguments, handing over to a running instance, `-default` |
| `ui/files.blp`, `ui/filespane.blp`, `ui/style.css` | The window, a pane, and their look (`window.files`) |

**The views' model.** A pane's entries live in Go, in the folder's order.
The views' model is a `GtkStringList` of the names shown, under a
`GtkMultiSelection`; a row binds to the entry at its position, so binding
is a slice index and a few labels. A reload puts in the model only the
stretch that changed (the common start and end stay), so the view keeps
its place, and the selection is put back by name. A thumbnail or a
folder's count arriving changes the row's widgets directly, never the
model, so the keyboard's place does not move.

**Signals.** purego makes a native trampoline for every Go function
connected, never freed and at most 2000. The file manager runs all
session and opens windows again and again, so its windows connect no
closures: one handler of each kind serves the process (`hub.go`) and
finds the Go function by the object that emitted the signal; a window
forgets its registrations when it closes.

**Threads.** Reading a folder, watching it, searching, counting,
thumbnails, the details' facts, drives and every operation run on
goroutines and come back with `gtkx.Post`; a pane drops a result meant for
a folder it has left (each load has a generation).

**Speed**, measured with `go test -bench` in `internal/apps/files` on a
16-thread machine, warm cache: reading and sorting a folder of 20 000
files, 21 ms (so about 0.1 s for 100 000); filtering 100 000 names by
three letters as one types, 7 ms.

## Tests

`internal/apps/files` is tested in temporary folders: natural order,
sorting, scoring and filtering, reading folders (links, broken links,
`.hidden`, the parallel path), history and the jump list, places and
bookmarks, `mountinfo` and GVfs's names, the trash (trashing, the info
file, restoring, emptying), every operation with its conflicts, undo,
cancelling, the clipboard's formats, watching, searching, thumbnails.
`disks` runs against a fake UDisks2 and `fm1` against a second process
on a private bus (`internal/dbustest`). The window was checked by hand in
a nested Hyprland: moving and opening by keys, the filter, two panes,
the grid's thumbnails, details, rename, a new folder, the menu, copy and
paste, a conflict, search, the location entry, the trash, undo and the
keys.

## Not yet

In order: renaming many files with a pattern; thumbnails of videos
and PDFs through the system's thumbnailers; reordering, renaming and
dropping on the sidebar's bookmarks (removing them is in their menu); asking a share's password in the
app's own card rather than GTK's dialog; redo; archives (compress,
extract); finding servers on the network (`network:///`, mDNS) rather than
typing an address; a Miller-columns view. Recent and starred files are
left out on purpose: the jump list covers folders, and the launcher finds
files.
