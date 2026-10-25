# Picker: inua's file chooser

Without the picker, apps that choose a file to open or a place to save
get GTK's file chooser: inua's own apps through `gtk.FileDialog`, other
apps through the `xdg-desktop-portal-gtk` backend. It is GTK3 or GTK4 depending on the
path, does not take inua's look, and works differently from Files:

- typing starts a location entry that completes paths, rather than
  filtering the folder;
- folders you use often are not offered, only "Recent" files and
  bookmarks;
- there is no preview of the file selected;
- each app starts again in its own last folder or in the home folder;
- saving keeps the name field and the folder list apart, so a folder
  cannot be picked by typing its name;
- it runs in another process and its window opens as a tile unless a
  window rule floats it (`config/hypr/inua.conf`).

The picker replaces it with Files' own views in a dialog window, served
to every app through the portal. It aims to be:

- **Files, in a dialog**: the same sidebar, list, grid, filter as you
  type, jump list (Ctrl+P), path entry (Ctrl+L), remote disks and keys,
  so someone who knows one knows the other;
- **quick**: up with the folder in it in well under a second when
  inua-files is running, and about a second when it is not
  (`docs/performance.md`);
- **for every app**: the portal is how Firefox, Chromium, Electron apps,
  Flatpaks and GTK apps under portals ask, so one backend covers them,
  and inua's apps ask the same way.

## How a request arrives

```
app ──► xdg-desktop-portal ──► org.freedesktop.impl.portal.FileChooser
        (org.freedesktop.portal       │  served by inua-files
         .FileChooser)                ▼
                                   picker window ──► URIs back to the app
```

inua-files serves `org.freedesktop.impl.portal.FileChooser` on the
session bus under `org.freedesktop.impl.portal.desktop.inua.files`; the
shell serves inua's other interfaces under `.inua` (`docs/portals.md`).
The process that already serves Files' windows serves the picker too:
the folder reading, icons, thumbnails, drives and remote disks are
already loaded. When it is not running, D-Bus starts it from a service
file, `inua-files -portal`, with no window: its first window is built
hidden and kept for the first folder the user opens.

| Method | Options read | Results |
| --- | --- | --- |
| `OpenFile` | `accept_label`, `modal`, `multiple`, `directory`, `filters`, `current_filter`, `choices`, `current_folder` | `uris`, `choices`, `current_filter`, `writable` |
| `SaveFile` | `accept_label`, `modal`, `filters`, `current_filter`, `choices`, `current_name`, `current_folder`, `current_file` | `uris`, `choices`, `current_filter` |
| `SaveFiles` | `accept_label`, `modal`, `choices`, `current_folder`, `files` | `uris` (the folder chosen, each file's name in it), `choices` |

Each call exports `org.freedesktop.impl.portal.Request` at its `handle`.
`Close` on that object closes the window and answers 2 (ended). The
answer is 0 with a choice, 1 when cancelled and 2 on failure. The call
returns when the window closes, so the service handles each request on a
goroutine of its own and the window on the main thread
(`gtkx.Post`/`gtkx.Call`).

The frontend (`xdg-desktop-portal`) checks the app's sandbox, gives
Flatpaks document-portal paths for the URIs we return and remembers
nothing for us. The backend sees only plain paths.

### Being the backend

`inua-files -picker` makes the picker the session's file chooser and app
chooser, and `-picker=false` takes them back, as `-default` does for the
file manager: it is `inua portals enable FileChooser AppChooser`
(`docs/portals.md`), which writes `inua-files.portal`, the config lines
and the D-Bus service file, and restarts `xdg-desktop-portal` in an inua
session.

### inua's own apps

`gtk.FileDialog` goes through the portal only inside a sandbox, or when
`GDK_DEBUG=portals` is set as GTK starts. With FileChooser on, inua's
apps and the shell set it before GTK opens its display and take it out
of the environment after (`gtkx.ForcePortals`), so their twenty-odd file
dialogs (Viewer, Music, Mail, the VM window, Workshop, Settings…) get the
picker without a change, and the programs they start do not inherit it.
Without the picker, they keep GTK's dialog.

## Window

```
┌──────────┬──────────────────────────────────────┬────────────┐
│ Firefox  │ ‹ › ↑  ~ / Downloads                 │            │
│ PLACES   ├──────────────────────────────────────┤ preview    │
│ Home     │ NAME ↑              SIZE   MODIFIED  │            │
│ Desktop  │ ▸ invoices       4 items   Oct 12    │ name       │
│ Downloads│   scan-0412.pdf   1.2 MB   14:02     │ KIND       │
│ …        │   photo.jpg       3.4 MB   11:40     │ SIZE       │
│ DEVICES  │                                      │            │
│ REMOTE   │                                      │            │
├──────────┴──────────────────────────────────────┴────────────┤
│ Open a file   [PDF documents ▾]          Cancel   [ Open ]   │
└──────────────────────────────────────────────────────────────┘
```

Saving puts the name in the footer, as an entry before the buttons:

```
│ Save as [scan-0412|.pdf         ]  [PDF ▾]   Cancel  [ Save ] │
```

- The title is the request's title, else *Open a file*, *Open files*,
  *Choose a folder* or *Save*. The sidebar's head names the app that
  asked (its desktop entry's name, from `app_id`), so it is clear who
  will get the file.
- One pane, no tabs, no second pane. The sidebar and the details
  (preview) are those of Files and remember their own state in the
  picker, apart from Files'.
- The footer: the title, the filter (when the app gave filters), the
  app's choices (a check button for a boolean, a dropdown for a list),
  Cancel, and the action: the app's `accept_label`, else *Open*,
  *Choose* or *Save*.
- Files the filter leaves out are not listed; folders always are.
- Errors (a folder that cannot be read, a name that cannot be saved)
  show under the footer, in `danger`, as in `design/ui.md`.

### Where it opens

The window floats over the window that asked: `parent_window` is
`wayland:HANDLE`, an xdg-foreign handle, which GDK takes as the picker's
parent (`gtkx.SetForeignParent`), so Hyprland places it as that app's
dialog. Its class is `org.freedesktop.impl.portal.desktop.inua.files`,
which the window rule for the portal's dialogs in
`config/hypr/inua.conf` floats and centres when there is no parent (an
X11 app's `x11:XID`). `modal` is not enforced across processes.

### The folder it starts in

1. `current_file`'s folder, else `current_folder`, when the app gives
   one;
2. the folder last chosen for this app (`app_id`; kept in
   `~/.local/share/inua/picker.json`);
3. for Save, Downloads; for Open, the folder last chosen by any app;
4. home.

The folder chosen also goes into Files' jump list, so Ctrl+P in either
learns from both.

## Keys

As Files, except for these:

| Key | Does |
| --- | --- |
| Enter | A folder: open it. A file: choose it (with several selected, all of them when `multiple`) |
| Ctrl+Enter | Choose what is selected, a folder too; with nothing selected, the folder shown (choosing a folder, saving several) |
| Letters | Filter this folder (when saving, the name entry has the focus: Tab goes to the list, and typing there filters) |
| Ctrl+L | A path; a folder typed is opened, a file selected in its folder |
| Ctrl+P | Jump to a folder used before |
| Ctrl+Shift+N | New folder, here (what an app's "save in a new folder" needs) |
| F2 | Rename |
| Ctrl+F | Search below this folder |
| Alt+F | The filter's list |
| Esc | Clear the filter or the search, then cancel |

Moving to the trash, copy and paste, operations between panes, tabs,
Open With and Tidy are left out: the picker chooses, it does not
manage. The list's menu has only what the keys above do.

## Saving

- The entry starts with `current_name` (else the file's name in
  `current_file`), with the name without its extension selected, as F2
  in Files.
- A path in the entry (`~/x/notes.md`, `../a.txt`) saves there; a folder
  that does not exist is an error under the footer, not made.
- Clicking a file puts its name in the entry; a folder is opened.
- Changing the filter changes the extension in the entry to the
  filter's first glob (`*.png` → `.png`) when the name has one of the
  old filter's extensions.
- A name that exists is asked about in a card over the window, as Files'
  conflicts are: *Replace "notes.md"?*, with Cancel focused and
  *Replace* in `danger`. The app writes the file; the picker only
  answers with its URI.
- A folder that cannot be written (a read-only drive, another user's
  folder) greys out *Save* and says why under the footer.
- `SaveFiles`: no entry; the folder selected, else the one shown, is
  chosen, and the names already in it are asked about once for all of
  them.

## Filters

A portal filter is a name and a list of patterns: globs (`*.jpg`) and
MIME types (`image/*`, `application/pdf`). A file matches when its name
matches a glob, ignoring case, or its type (GIO's content type, already
read for icons, from the name, once for each extension) is or derives
from a MIME type (`g_content_type_is_a`). Choosing a folder lists no
files. The status line counts the files left out apart from the hidden
ones (*"3 not of this kind"*).

## Architecture

| Package | What |
| --- | --- |
| `internal/apps/files/pick` | The request without GTK: options read from the portal's variants, filters and matching, the folder it starts in, the remembered folders, the save name (extensions, paths typed, conflicts), the answer |
| `internal/apps/files/chooser` | `org.freedesktop.impl.portal.FileChooser` and `AppChooser`: serving them (the requests through `internal/portal`) and handing each to the UI, the answer back as the portal's results |
| `internal/filesui` | The picker window: `window` in a pick mode (one pane, the footer, no operations), `picker.go` for the footer, the keys and the answer; `appchooser.go` for the app chooser |
| `internal/gtkx` | The parent from another program (`SetForeignParent`), the window's own class (`SetAppID`), `ForcePortals` |
| `cmd/inua-files` | Serving the portal names, `-picker`, `-portal` |
| `ui/files.blp`, `ui/style.css` | The footer (`picker_foot`) |

The process: as before, it ends with its last window, but a picker holds
it, and it stays 10 minutes after its last picker so the next is quick.
Started by D-Bus, it goes 10 minutes after its start with nothing asked.
Start-up is yet to be measured, as `docs/performance.md` says.

## Tests

- `pick`: options from variants (each method's, missing ones, wrong
  types); filters (globs and case, MIME types and their parents,
  folders always shown); the folder it starts in, in each case of the
  order above; save names (the extension kept when renamed, changed with
  the filter, paths typed, a missing folder, a name taken); the answer's
  URIs (spaces and non-ASCII escaped).
- `chooser`: a client on a private bus (`internal/dbustest`) calls
  each method with a fake picker: the answer's URIs, choices and filter,
  a cancel, `Close` while a request is open, and the app chooser with a
  new list while it is open.
- By hand, on GTK's Broadway backend with a fake HOME and a private bus
  (`inua-files -portal`, requests sent with `gdbus`): opening pictures
  through a filter with the app's choices, a click and Enter; saving with
  the stem typed over, and a name taken asking *Replace*; choosing a
  folder with Ctrl+Enter; `Close` from the frontend closing the window;
  the app chooser's list, its update and finding by typing.
- Not yet run: in a real inua session, with Firefox, Chromium, a Flatpak
  app and inua's own dialogs; a remote disk as the place; the linger.

## Open questions

- Firefox uses the portal for its file chooser only with
  `widget.use-xdg-desktop-portal.file-picker = 1` outside Flatpak. Do we
  set it, document it, or leave it?
- Chromium and Electron decide by the portal's version; check which
  versions of each ask it on 26.04.
- Should the preview be on by default in the picker, where seeing an
  image before choosing it matters more than in Files?
