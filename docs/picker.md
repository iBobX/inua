# Picker: inua's file chooser

*A design, not yet built.*

Apps that choose a file to open or a place to save get GTK's file
chooser. inua's own apps (Viewer, Music, the VM window, Workshop) show
it through `gtk.FileDialog`, and other apps get it through the
`xdg-desktop-portal-gtk` backend. It is GTK3 or GTK4 depending on the
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
session bus under `org.freedesktop.impl.portal.desktop.inua`. The process
that already serves Files' windows serves the picker too: the folder
reading, icons, thumbnails, drives and remote disks are already loaded.
When it is not running, D-Bus starts it from a service file, with no
window.

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

`inua-files -picker` makes the picker the session's file chooser, and
`-picker=false` takes it back, as `-default` does for the file manager.
It writes:

- `inua.portal` (`DBusName=org.freedesktop.impl.portal.desktop.inua`,
  `Interfaces=org.freedesktop.impl.portal.FileChooser`) where
  `xdg-desktop-portal` reads backends;
- `~/.config/xdg-desktop-portal/hyprland-portals.conf`, which replaces
  the system's `hyprland-portals.conf`, so it repeats its default:

  ```ini
  [preferred]
  default=hyprland;gtk
  org.freedesktop.impl.portal.FileChooser=inua;gtk
  ```

  The `gtk` after `inua` keeps file choosers working when inua-files is
  not installed;
- a D-Bus service file for the name in `~/.local/share/dbus-1/services`;

and then restarts `xdg-desktop-portal` (`systemctl --user`), which reads
its backends only at start. `scripts/install.sh` asks whether to run it,
as it does for the keyring.

### inua's own apps

`gtk.FileDialog` only goes through the portal inside a sandbox, so
inua's apps would keep GTK's dialog. They ask the portal instead,
through `internal/picker`, which has no GTK: it calls
`org.freedesktop.portal.FileChooser` with godbus, waits for the
`Response` signal on the request and returns the paths. The apps call it
on a goroutine and bring the answer back with `gtkx.Post`. When there is
no portal, they fall back to `gtk.FileDialog`, so they still work outside
an inua session. The five calls today:

| App | Asks |
| --- | --- |
| Viewer | One file to open (images, PDFs, …) |
| Music | Files or a folder to add |
| VM | A Windows ISO |
| Workshop | An app's folder; files a block asks for |

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

The window floats, centred over the window that asked. `parent_window`
is `wayland:HANDLE` (xdg-foreign) or `x11:XID`, which Hyprland cannot
turn into a window for us, so the picker takes Hyprland's active window
when the request arrives (the app the user has just clicked in), and
places itself on that window's workspace and monitor through
`internal/hypr`. The window rule in `config/hypr/inua.conf` gets the
picker's class, so it floats from its first frame. `modal` cannot be
enforced across processes; the picker stays above its parent instead.

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
| Ctrl+L | A path; a file typed is chosen, a folder opened |
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
- `SaveFiles`: no entry; the footer lists how many files, and a name
  already in the chosen folder is asked about once for all of them.

## Filters

A portal filter is a name and a list of patterns: globs (`*.jpg`) and
MIME types (`image/*`, `application/pdf`). A file matches when its name
matches a glob, ignoring case, or its type (GIO's content type, already
read for icons) is or derives from a MIME type. Matching runs with the
folder's reading, on the pane's goroutines, so a big folder does not wait
on it.

## Architecture

| Package | What |
| --- | --- |
| `internal/apps/files/pick` | The request without GTK: options read from the portal's variants, filters and matching, the folder it starts in, the remembered folders, the save name (extensions, paths typed, conflicts), the answer |
| `internal/apps/files/portal` | `org.freedesktop.impl.portal.FileChooser` and `Request`: serving them and handing each request to the UI, as `fm1` does |
| `internal/picker` | The client for inua's apps: the portal call, else `gtk.FileDialog` |
| `internal/filesui` | The picker window: `window` in a pick mode (one pane, the footer, no operations), `picker.go` for the footer and the answer |
| `cmd/inua-files` | Owning the portal name, `-picker` |
| `ui/files.blp`, `ui/style.css` | The footer (`window.files.picker`) |

The process: as now, it ends with its last window; started by the
portal, it waits 10 minutes after its last picker before it ends, so the
next request is fast. To be measured, as `docs/performance.md` says.

## Tests

- `pick`: options from variants (each method's, missing ones, wrong
  types); filters (globs and case, MIME types and their parents,
  folders always shown); the folder it starts in, in each case of the
  order above; save names (the extension kept when renamed, changed with
  the filter, paths typed, a missing folder, a name taken); the answer's
  URIs (spaces and non-ASCII escaped).
- `portal`: a second process on a private bus (`internal/dbustest`)
  calls each method, cancels, and calls `Close` while a request is open.
- `picker`: against a fake frontend on a private bus: an answer, a
  cancel, no portal.
- By hand, in a nested Hyprland: Firefox's and Chromium's open and save,
  a Flatpak app, a GTK app with `GDK_DEBUG=portals`, each of inua's five
  dialogs, a remote disk as the place, two requests at once, the app
  closed while its picker is open.

## Open questions

- Does Ubuntu 26.04's `xdg-desktop-portal` read backends from
  `~/.local/share/xdg-desktop-portal/portals`, or only from
  `/usr/share`? If only there, `-picker` needs sudo for one file, and
  the install script places it.
- Firefox uses the portal for its file chooser only with
  `widget.use-xdg-desktop-portal.file-picker = 1` outside Flatpak. Do we
  set it, document it, or leave it?
- Chromium and Electron decide by the portal's version; check which
  versions of each ask it on 26.04.
- Should the preview be on by default in the picker, where seeing an
  image before choosing it matters more than in Files?
