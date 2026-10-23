# UI and UX

How inua looks and behaves: the shell's surfaces, the settings window and
the light apps (Files, Remote Desktop, Mobile Link, GPG, Pass and the
rest). The rules are written down here; `ui/style.css` and the themes in
`ui/themes/` are where they are kept. When the two disagree, the code
wins, and this file is corrected.

## Principles

- **Restraint and empty space.** Few colours, one accent per surface, and
  that accent only for what is current, selected or on. No glows, no
  gradients, no drop shadows on controls.
- **Hairlines, not boxes.** Sections are separated by a 1 px line, not
  framed or filled. Corners are 2 px (controls), 3–4 px (cards, menus).
- **Two voices of type.** Running text in a sans; readouts, labels and
  mode names in a monospace, small and tracked wide.
- **Status is a square.** 7 px, filled when on, hollow when off.
- **Keyboard first.** Every action has a key; the keys are listed where
  the user can find them (Files' Ctrl+?, the launcher's foot).
- **Say what happened, where it happened.** An error or a result shows
  beside the control or the thing it is about, never in a distant line
  (see [Feedback](#feedback)).
- **Real content in the first frame.** No placeholder numbers; see
  `docs/performance.md`.

## Colour

Colours are tokens only. A theme (`ui/themes/<name>.css`, chosen by
`style.theme`) defines them; `ui/style.css` uses the names and never a
colour of its own. A user's theme overrides only the tokens it names.

| Token | Use |
| --- | --- |
| `app` | The ground of windows and pages |
| `panel` | The index column of two-column windows; side panels |
| `surface`, `surface2` | Controls: entries, outlined buttons, cells |
| `t0`, `t1`, `t2` | Text: primary, secondary, tertiary (hints, kickers, disabled) |
| `line` | Hairlines and control borders |
| `accent` | Current, selected, on; the main action's fill |
| `primary` | The light apps' `.primary` button fill |
| `danger`, `warn`, `ok` | Errors and destructive actions; notices; good states |
| `field` | The wallpaper's texture |
| `sb`, `sbaccent` | The bar |

The few places that tell kinds apart by colour use the three hues the
themes have besides the text: Disk Usage's map draws the developer's
folders in `accent`, the agents' and AI's in `warn` and caches in `ok`,
the rest in `t2`, with a legend of squares under it.

Themes: `yoru` (the default, dark), `washi` and `aojashin` (light),
`suimen`, `rinko`, `bunko` and `denko` (dark).

Tints of the accent, used the same way everywhere:

| State | Fill | Border |
| --- | --- | --- |
| Hover | accent at 5–9% | accent at 20% (when the item has a border) |
| Selected, checked | accent at 9–16%, or `mix(surface, primary, 0.1)` in an index | accent at 30–38% |
| Focused entry | — | accent, with a 3 px ring at 14% |

## Type

| Family | Where |
| --- | --- |
| Zen Kaku Gothic New (Noto Sans CJK after it) | Running text, 13–14 px |
| IBM Plex Sans | Titles: a window's index title (20 px), index entries |
| Geist Mono (IBM Plex Mono, JetBrains Mono) | Readouts, kickers, section labels, tags, keycaps, numbers, paths, PINs; tabular figures |

Recurring sizes and treatments:

- **Kicker** (`.kicker`): 10.5 px, upper case, tracked 0.16 em, `t2`.
  Group names in an index: THIS COMPUTER, MACHINES.
- **Section label** (`.section-label`): mono 11 px, weight 600, tracked
  0.12 em, in brackets: `[STREAM]`.
- **Tag** (`.tag`): a mode name, mono 11 px, upper case, in the accent:
  `[APPS]`.
- **Label / sub** (`.label`, `.sub`): a row's text, 13 px `t1`, and the
  line under it, 12 px `t2`.
- **Readout** (`.readout`): mono 11.5–12 px, `t1`.

`style.text_scale` (80–150%) scales every `font-size` in `ui/style.css`
as it loads; spacing and icons keep their sizes, so layouts must not
depend on text being a fixed size.

## Marks

The status mark (`.mark`) is a 7 px square: hollow (`t2` border) when off,
filled with the accent when on. It starts a section head, a launcher head,
a pane's path (Files, with two panes) and a list of states.

The section mark (`.settings .section-head .mark`) is the same 7 px
square, filled with `mix(t0, accent, 0.68)`, 1 px corners.

In a layout a mark is an **empty `Box`**, never a `Label`: an empty label
is as tall as a line of text, so the "square" draws as a bar.

The one place colours leave the tokens is a service's face in Mail: its
logo on the brand's own colour (`internal/apps/mail/brands.go`, from
Simple Icons), white or black on it, and on a dark theme a brand near
black on white instead (GitHub). The rules are made for the theme in use
(`mail.BrandCSS`), not written in `ui/style.css`.

```blueprint
Box {
  styles ["mark"]
  valign: center;
}
```

## Windows

### The settings language

The settings window and the light apps share one language (their windows
carry `.settings`):

```
┌───────────────────┬──────────────────────────────────────────────┐
│ Title             │ ■ [SECTION]                     meta  [ctl]  │
│ KICKER            │ ──────────────────────────────────────────── │
│ ▣ Item       sub  │ Label                              control   │
│ ▣ Item       sub  │ sub line                                     │
│ KICKER         ↻  │ Label                              control   │
│ ▣ Item       sub  │                                              │
│ + Add…            │ ■ [SECTION]                                  │
└───────────────────┴──────────────────────────────────────────────┘
```

- **Index** (left, `panel`, a hairline on its right): the title (IBM Plex
  Sans 20 px), kickers for groups, items 40–44 px tall with an icon, a
  title and a sub line. Hover: accent at 6% with a 20% border; selected:
  `mix(surface, primary, 0.1)` with a 30% accent border.
- **Page** (right, `app`, padding 18 px 32 px): sections of rows.
- **Section**: a head (44 px; the mark, the `[LABEL]`, an optional meta
  text or control at the end) on a hairline, then the body. No frame, no
  fill. 22 px between sections.
- **Row** (`.row`, at least 44 px, 12 px above and below): the text on the
  left (label, sub line), the control at the end, centred. A **field**
  (`.field`) puts its label above a wide control.
- **Rows in a list** (`.rows`): hairlines between them; a whole row can be
  a button (`.row-button`), tinted on hover.

### Light apps

A light app is one of these: a window of its own with an index and pages
(Mobile Link, Clock, Stocks), a single page with text tabs (GPG,
Tailscale), or a library with a panel (Virtual machines, Remote Desktop).

A **library** shows what the user runs or connects to as tiles on
`panel`: a card (`surface`, a `line` border, 4 px corners) with its icon
and a mark at the top (filled while it runs or is connected), then its
name and a sub line; hover and selected as an index item. The chosen one's
page is a panel at the right (`app`, about 520 px), with a head (its mark,
name and state) and sections as a page. New or Add is a button in the
library's head and asks in a dialog, never in the panel. With nothing yet,
the library says so and the panel is hidden. The layout is the app's
Blueprint in the `library` classes (`.library`, `.library-tiles`,
`.library-panel`, `.panel-head`); the tiles are `appui.NewLibraryTile`.

They follow `docs/performance.md` for start-up.

### Top bar panels

What a top bar button opens (the notification center, the clipboard
history, passwords, the control center, the agenda) is a card below the
bar, over a full-screen backdrop that closes it on a click.

- **A drop shadow.** Every such card casts `0 8px 28px` at 32% black
  (`.center .card`), and a notification banner a lighter one. It is the
  one place a shadow is allowed: a surface floating over windows, not a
  control. Nothing may clip it: a `GtkScrolledWindow` around the card
  does, and so do the edges of a window only as big as the card; scroll
  inside the card, and give such a window margins as wide as the shadow.
- **Full height slides in from the side.** A card that runs from the bar
  to the screen's foot (the notification center, the clipboard history)
  is in a `GtkRevealer` that slides in from the right edge when it opens
  and back out however it closes (`revealMS`); the window goes once it is
  out. The control center and the agenda, as tall as their content,
  slide down from the bar the same way.

## Controls

All controls are 34 px tall where they sit in a row, framed by a 1 px
`line` border with 2 px corners, on `surface`.

| Control | Look | Class |
| --- | --- | --- |
| Button | Outlined: `surface`, `line` border, `t0` text; hover: 6% accent, 38% accent border | `outlined` |
| The main action | The outlined shape filled with the accent, text in `app`; hover: the accent mixed 14% toward `t0`; disabled at 45% | `suggested-action` |
| A light app's main action | Filled with `primary`, text in `app` | `primary` |
| Destructive | Outlined, text in `danger`; or filled with `danger` in a confirming dialog | `outlined danger`, `destructive-action` |
| Icon button | Borderless, 28 px square (20 px in tight rows), `t1`; hover: 9% accent | `plain` |
| Entry | `surface`, `line` border; focus: accent border and a 3 px ring at 14% | — |
| Number | An entry's box with the digits in mono; − and + as flat 32 px cells behind hairlines | `spinbutton` (styled for every `.settings` window) |
| Drop-down | The outlined button's shape, at least 150 px; its list on `app` without GTK's padding | `dropdown` |
| Switch | 36 × 18 px, 2 px corners: a 14% accent tint off, the accent on | `switch` |
| Segmented | Options in one hairline frame; the chosen one tinted with the accent (16%) in the accent's colour | `segmented` |
| Keycap | Mono 11.5 px in a hairline box | `keycap` |

One main action per page, at most: the accent fill is that page's one
accent. The other buttons beside it are outlined.

Every window that uses `suggested-action` gives it the full rule (shape,
fill, hover, disabled); GTK's default is a rounded bright button that
belongs to no theme of inua's.

## Feedback

### Errors where they happen

An error shows beside what it is about, in `danger`, 12 px, wrapped,
selectable (so it can be copied), and hidden while there is none:

- a section's error under that section's rows;
- an action on an item (a machine, a device) on that item's page, kept
  per item, so moving to another item and back still shows it;
- a form's error under the form, in its dialog (Remote Desktop's *Add a
  machine*).

It is cleared when the user tries again. A message that would vanish with
its row (a device that stopped waiting to pair) stays in its section until
then. There is no window-wide status line for errors.

Remote Desktop's window follows this. The settings window and Mobile Link
still say their errors in a status line at the foot of the index
(`.settings .status`); they are to move to the same rule.

### Other feedback

- **A notice** (`.warn`): a hairline in `warn`, no fill.
- **A flash**: a result for a moment in a status line that belongs to the
  thing (Files' pane status, 4 s, in the accent).
- **Empty**: say why there is nothing (`.empty`, `t2`): "This folder is
  empty.", "No device is paired yet.", not a blank page.
- **Waiting**: say what is being waited for ("Asking…", "Starting…") in
  the place the result will show.
- **Before content**: a page that has nothing to show yet shows what it
  knows already, not a blank: Disk Usage's map is the disk's used and free
  room before a scan.
- **Working**: a long job shows its result arriving, in the place it will
  stay, with counters in mono: Disk Usage's map fills ring by ring as
  folders are measured, while a sweep of square-ended cells, stepped in
  opacity (no gradient), turns through the rings outside.
- **Asking first**: what cannot be undone (deleting for good, a command
  that deletes) is asked in a dialog over the window: the question as its
  title, the thing in mono, a line on what follows, Cancel with the focus
  and the action beside it (filled with `danger` for what is lost for
  good). What can be undone (to the trash) is done at once.
- **What a failure needs**: when something is missing (a program, a
  permission), say what and give the command that fixes it, selectable.

## Reordering

A list whose order is the user's is put in order by dragging its items:
the dock's pinned apps, inua-term's workspaces (column 1). It goes the
same way in both:

- **The item lifts.** A still of it follows the pointer; its own place
  stays open as a gap, so nothing moves when the drag begins.
- **The others make room.** Under the pointer, the gap opens where the
  item would land: before the item whose first half the pointer is over,
  or after the last. The items slide aside, a step every 16 ms of 30% of
  the way left (no fixed duration), not jump.
- **The drop puts it there**, at once, before whatever keeps the order
  (the settings, inua-term's host) has answered: the list never shows the
  old order again on the way.
- **Anywhere else** (outside the list, or Escape), the item goes back
  and the gap closes. Only where it means something does a drop outside
  do more: a pinned app dropped off the dock is unpinned.
- **A click is still a click.** The drag begins past GTK's threshold, so
  pressing an item to open or show it is unchanged.
- **A filtered list stays put.** While a search narrows the list, its
  items do not drag: the places between them are not the list's.

## Icons

Phosphor's regular icons (MIT), copied by `scripts/phosphor-icons.sh` into
`ui/icons/` under the freedesktop name the code asks for. Another source
only when Phosphor has nothing fitting. Files and other apps' icons come
from the desktop's icon theme; everything else from the Phosphor set.

The inua logo, its app icons and the favicons are in `design/logo/`,
drawn by `design/logo/build.py`; see `design/logo/README.md`.

## Words

All text the user reads is translatable (`docs/i18n.md`). Plain words:
what happened and what to do, in the user's terms ("Paired, and ready.",
"It does not answer: it is off, or not sharing its desktop."). Name the
thing, not the mechanism; give the command when the user has to run one.

## Checking a window

Look at a window before calling a visual change done, and compare it with
one that is right (GPG and the settings window are the references).
Without a window on the desktop, GTK's Broadway backend draws an app into
a web page that a headless browser can capture:

```sh
gtk4-broadwayd :7 &
GDK_BACKEND=broadway BROADWAY_DISPLAY=:7 dbus-run-session -- bin/inua-remote -ui-dir ui &
google-chrome --headless=new --disable-gpu --window-size=1300,900 \
  --virtual-time-budget=4000 --screenshot=remote.png http://127.0.0.1:8087/
```

`dbus-run-session` keeps the copy from handing over to the app already
open on the desktop; `-ui-dir ui` reads the layouts and styles from disk,
so a style change needs no rebuild. The private bus also means the copy
cannot see services on the session bus (Mobile Link shows "starting").

## Known differences

- The settings window and Mobile Link report errors in the index's
  status line, not where they happen.
- `suggested-action` is styled per window (GPG, Pass, Files, Workshop,
  Remote Desktop, Mobile Link, Disk Usage, Mail, the VM manager) with the same rule; one
  shared `.settings button.suggested-action` would replace them.
- Two fills for a main action exist: the accent (`suggested-action`) and
  `primary` (some light apps and games). New windows use the accent.
