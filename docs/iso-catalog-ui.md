# The install-image list: what it holds and how it is grouped

Status: **implemented and run by hand on the local Hyprland session
(2026-10-18)**. What the screenshots in `docs/evidence/vm-ui-2026-10-18/` and the
session's own record show is what this document asked for, with three deltas
recorded at the end. It follows [iso-download.md](iso-download.md) (the catalog,
the downloader, the checksum and the free-space check).

Read with [inua-vm.md](inua-vm.md); the window's own rules (a section head with a
Phosphor mark, strings through `internal/i18n`, the source named in the section,
"the machines one a row") are what this list should follow too.

## What the download list is today

One flat list, **one row per install medium**, in the `[IMAGE]` section of the
machine's setup page (`ui/vm.blp`, `Box vm_media_rows`; built by `isoRowsOf` and
`showSetupMedia` in `internal/vmui/window.go`).

On this host's osinfo-db (0.20250606) the filter the guide sets — released or
rolling, not past its support date, a URL, a host architecture — leaves:

| | count |
|---|---:|
| operating systems | 73 |
| media rows (what the list shows) | 231 |
| of those, live images | 80 |
| of those, the database gives a size for | 67 |
| of those, no size in the database | 164 |

Each row reads *system · variant*, then *architecture · live image/installer ·
size when the database knows one · how the file already stands here*
(cached, half downloaded, verified), and carries one action button
(Download / Resume / Retry / Use this image). The section head names the source
(`osinfo-db`, or inua's own short list when the package is missing).

The transfer itself — bar, `17 MiB / 40 MiB`, **Cancel** — is a box
(`vm_media_progress`) placed **after** the row box, so it sits at the end of the
list; with 231 rows that is more than a dozen screens below the button that
started the download.

## What that costs (seen in the 2026-10-18 hand-run)

1. **Nothing groups or filters the list.** The order is newest release first, so
   the newest rows of every system are at the top even though the user is looking
   for one system by name ("Debian netinst" is a scroll through 231 rows).
2. **The row repeats the system it belongs to.** `Fedora Linux 42` appears four
   times, `AlmaLinux 10` three times, `openSUSE Leap 15.5` six times: the list is
   medium-shaped where the user thinks system-shaped.
3. **Starting a transfer takes the user away from it.** The bar and Cancel are at
   the end of the list, and the list is rebuilt when a row's state changes, which
   moves the scroll position (observed: the same view could not be returned to
   reliably after a download started or a row errored).
4. **Rows look inconsistent in a way the database causes.** 164 of 231 rows show
   no size, because osinfo-db publishes none for them — that is not a fault to
   fix by probing, but the list should not pretend a size is the norm.
5. Only the machine's name and state stay fixed while the page scrolls; the
   `[GUEST]`, `[INSTALL]` and `[IMAGE]` heads scroll away with the rows.

## Rules this design must keep

From the two guides, unchanged:

- **osinfo-db is the catalog**, read from `/usr/share/osinfo` and
  `~/.local/share/osinfo`; released or rolling, not ended, a URL present, the
  host's architecture, newest release first. No popularity ranking, no mirror
  choice, no size cap.
- **No curated URLs.** A list of ids may be curated (they age slowly); the URL
  always comes from the database.
- **Wording stays honest**: a file with no published checksum says so; a file
  already in the cache says how it got there; nothing is downloaded to decide
  what to show.
- Everything the user reads goes through `internal/i18n` in the six catalogs;
  marks come from the Phosphor set the shell already installs.

## Draft

### 1. Two levels, in the same shape the window already uses

**Recommended** — a short section above the full list: at most eight systems,
from a small built-in list of **osinfo-db ids** (`fedoraproject.org`,
`ubuntu.com`, `debian.org`, `opensuse.org`, `archlinux.org`,
`linuxmint.com`, `freebsd.org`, `alpinelinux.org`). Each id resolves through the
same filter to its newest medium; the row shows the same facts as today. An id
that resolves to nothing is simply left out, so the list cannot rot the way a
list of URLs does.

**All systems** — one **expandable row per operating system**: its mark, name,
and a summary ("3 installers · newest 2026-04"). Expanding it shows that
system's media, one row each, exactly the rows of today (variant, architecture,
live/installer, size when known, local state, one action button). Expanding in
place keeps the single column and the keyboard path; the alternative — the
system in the list, its media in the right-hand panel — is what *machines* do,
but that panel already holds the machine being set up, so two panels would
collide.

### 2. Controls above the list, one line

- a search field (system name or variant);
- a family chooser — Linux · BSD · other — from the OS element's own
  `<family>`/`<distro>`;
- an architecture chooser, the host's by default, another architecture behind it;
- two switches, off by default, that widen the guide's rule and say so:
  *show prerelease*, *show end-of-life*.

The section head counts what the controls leave: `12 systems · 31 media` instead
of 231 rows. The source line (`osinfo-db` / inua's own list) stays where it is.

### 3. The transfer leaves the list

The bar, its text and **Cancel** move into a **fixed row at the head of the
`[IMAGE]` section**, outside the scrolling rows, so they are in view the moment a
download starts and stay in view while the user reads something else. The medium
being fetched keeps its own state in the list (its button goes insensitive, and
the row is what the fixed row is talking about: the system and variant in it).

The rows themselves should be **diffed, not rebuilt**: `isoRowsSame` already
exists for that (it compares media, cached, partial, checksum, downloading, err);
the rebuild path that ignores it is what moves the scroll.

### 4. Data the reader needs (additive, no new source)

- `<family>` and `<distro>` from the OS element, as text: the grouping key and
  the family filter.
- `<variant>` id (already read): category of a medium — desktop, server, netinst,
  minimal, cloud, live, rescue — through a small table in code; an id the table
  does not know is left uncategorised rather than guessed.
- Nothing else: no probing, no extra requests, no second list to maintain.

### 5. What it must not grow into

- no curated **URLs**; ids only;
- no popularity ranking and no "recommended mirror";
- no size probing to fill the 164 rows osinfo-db leaves empty (a HEAD per row is
  164 requests for a number the database chose not to publish);
- no claim of a checksum that was never published.

## Files this would touch

- `internal/vm/isocatalog.go` — read `<family>`/`<distro>`; add `Family`,
  `Group`, `Category` to `ISOMedia`; keep `sortISOMedia`'s rule (date first, then
  a stable tiebreak); a `RecommendedIDs` list and its resolution helper.
- `internal/vmui/window.go` — `isoRowsOf` returns systems (with their media)
  instead of one flat list; `showSetupMedia` builds the filter line, the fixed
  transfer row and the expandable rows; keep `isoRowsSame`.
- `ui/vm.blp`, `ui/vm.ui`, `ui/style.css` — the filter line, the fixed transfer
  row, the expander; run `make generate` and commit both.
- `internal/vm/isocatalog_test.go`, `internal/vmui/iso_test.go` — family and
  category reads, grouping, recommended resolution, and the row model.
- `po/inua.pot`, `internal/i18n/locales/*.po` — the new strings (`make i18n`,
  idempotent). System and variant names stay as the database writes them.

## What the hand-run changed (deltas from the draft)

1. **The architecture filter has something to filter.** The draft assumed the
   catalog would keep holding only the x86 media and the chooser would be nearly
   empty. Instead `LoadISOCatalog` now keeps every medium that has a URL and an
   architecture, stamps it with its family (`x86`, `arm64`, `riscv64`, …), and
   the *view* applies the guide's rule: the host's family by default, every
   family when the chooser says so. So the switches for prerelease and ended
   systems and the architecture chooser are all one mechanism (`ISOFilters`),
   and the loader only decides what is readable. Measured on this host's
   osinfo-db: 390 media with a URL, 231 of them x86 that the guide's list shows.
2. **A medium with no variant is named by its file.** A system's own rows used to
   repeat the system's name whenever the database named no variant - AlmaLinux
   10 three times over. Inside a system the row is the variant when there is one
   and the file's own name when there is not.
3. **The transfer stands above the list, not sticky.** The bar, its text and
   Cancel moved from the end of the rows to the head of the `[IMAGE]` section, so
   starting a download no longer scrolls its own progress two hundred rows away.
   It is not literally pinned: that needs the list in a scroller of its own, which
   is the next thing worth doing here.

## Open questions for the maintainer

1. **Recommended**: implemented as a built-in list of *distributions*
   (`internal/vm/isocatalog.go`, `RecommendedISOList`: fedora, ubuntu, debian,
   opensuse, archlinux, freebsd, rocky, alpinelinux), each resolved to the newest
   system the view holds, and skipped when the database has none. A system also
   stays in the "all systems" list it would otherwise be missing from.
2. **A flat mode?** When the search box has text, should the list fall back to
   one row per medium (what is shown today), so a file name can be matched
   directly?
3. **How many categories?** The variant ids are precise but numerous; hand-named
   buckets ("Desktop", "Server", "Minimal") read better and cost i18n strings.
4. **A page of its own?** The guide's page set (setup → provisioning → running)
   has no "choose an image" page. Does the list stay inside the machine's setup
   page, or become a step of New machine (template → image → name)?
